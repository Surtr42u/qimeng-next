import ts from "typescript";
import { relative, resolve } from "node:path";

const slash = (path: string) => path.replaceAll("\\", "/");
const app = slash(resolve("src")) + "/";
const shared = slash(resolve("../ui/react")) + "/";
const transport = new Set(["fetch", "XMLHttpRequest", "WebSocket", "EventSource", "WebTransport", "sendBeacon"]);
const storage = new Set(["cookie", "cookieStore", "localStorage", "sessionStorage", "indexedDB", "credentials"]);
const secret = /(?:^|_)(?:TOKEN|PASSWORD|SECRET|CREDENTIALS?|API_KEY)(?:$|_)/i;

export type BoundaryFinding = { file: string; line: number; reason: string };

export function presentationFindings(program: ts.Program, source: ts.SourceFile): BoundaryFinding[] {
  const checker = program.getTypeChecker();
  const findings: BoundaryFinding[] = [];
  const pure = slash(source.fileName).startsWith(shared);
  const report = (node: ts.Node, reason: string) => {
    const line = source.getLineAndCharacterOfPosition(node.getStart(source)).line + 1;
    if (!findings.some(f => f.line === line && f.reason === reason)) findings.push({ file: slash(relative(process.cwd(), source.fileName)), line, reason });
  };
  const origin = (symbol: ts.Symbol | undefined) =>
    symbol?.flags && (symbol.flags & ts.SymbolFlags.Alias) ? checker.getAliasedSymbol(symbol) : symbol;
  const paths = (symbol: ts.Symbol | undefined) => origin(symbol)?.declarations?.map(d => slash(d.getSourceFile().fileName)) ?? [];
  const rawClient = (path: string) => path.startsWith(app + "api/") || path === app + "data/client.ts" || path.includes("/node_modules/openapi-fetch/");
  const runtimeBinding = (el: ts.ImportSpecifier | ts.ExportSpecifier) => !el.isTypeOnly;
  const modules = (node: ts.Node, spec: ts.StringLiteralLike, runtime: boolean, bindings: readonly ts.Node[] = [], all = false) => {
    const module = checker.getSymbolAtLocation(spec);
    const files = paths(module);
    if (!files.length) {
      const resolved = ts.resolveModuleName(spec.text, source.fileName, program.getCompilerOptions(), ts.sys).resolvedModule;
      if (resolved) files.push(slash(resolved.resolvedFileName));
    }
    if (pure && (files.some(p => p.startsWith(app)) || /^(?:react-router(?:-dom)?|@tanstack\/react-query|openapi-fetch)(?:\/|$)/.test(spec.text))) {
      report(node, "共用展示包依赖了应用、路由或数据层");
    }
    if (!runtime) return;
    const imported = bindings.map(binding => checker.getSymbolAtLocation(binding));
    if (all && module) imported.push(...checker.getExportsOfModule(module).filter(s => (origin(s)!.flags & ts.SymbolFlags.Value) !== 0));
    if (spec.text === "openapi-fetch" || files.some(rawClient) || imported.some(s => paths(s).some(rawClient))) {
      report(node, "展示层取得了底层 SDK 或 HTTP 客户端");
    }
  };
  const checkBrowser = (node: ts.Node, name: string, symbol: ts.Symbol | undefined) => {
    if (!symbol?.declarations?.some(d => program.isSourceFileDefaultLibrary(d.getSourceFile()))) return;
    if (transport.has(name)) report(node, "展示层直接使用网络传输");
    if (storage.has(name)) report(node, "展示层直接访问浏览器凭据或持久化存储");
  };
  const prop = (node: ts.Node, owner: ts.Expression, name: string) => {
    if (!transport.has(name) && !storage.has(name) && name !== "token" && name !== "password" && !secret.test(name)) return;
    const symbol = checker.getPropertyOfType(checker.getTypeAtLocation(owner), name);
    checkBrowser(node, name, symbol);
    if ((name === "token" || name === "password") && paths(symbol).some(p => p.startsWith(app + "api/"))) {
      report(node, "展示层直接读取协议凭据");
    }
    if (secret.test(name) && ts.isPropertyAccessExpression(owner) && owner.name.text === "env" &&
      (ts.isMetaProperty(owner.expression) || ts.isIdentifier(owner.expression) && owner.expression.text === "process")) {
      report(node, "展示层直接读取凭据环境变量");
    }
  };
  const visit = (node: ts.Node) => {
    if (ts.isImportDeclaration(node) && ts.isStringLiteralLike(node.moduleSpecifier)) {
      const c = node.importClause, b = c?.namedBindings;
      const named = b && ts.isNamedImports(b) ? b.elements.filter(runtimeBinding).map(e => e.name) : [];
      const runtime = !c || !c.isTypeOnly && (!!c.name || !b || !ts.isNamedImports(b) || b.elements.length === 0 || named.length > 0);
      modules(node, node.moduleSpecifier, runtime, c?.name ? [c.name, ...named] : named, !!b && ts.isNamespaceImport(b));
      return;
    }
    if (ts.isExportDeclaration(node) && node.moduleSpecifier && ts.isStringLiteralLike(node.moduleSpecifier)) {
      const named = node.exportClause && ts.isNamedExports(node.exportClause) ? node.exportClause.elements.filter(runtimeBinding).map(e => e.name) : undefined;
      modules(node, node.moduleSpecifier, !node.isTypeOnly && (!named || named.length > 0), named, !named);
      return;
    }
    if (ts.isTypeNode(node)) return;
    if (ts.isCallExpression(node) && (node.expression.kind === ts.SyntaxKind.ImportKeyword || ts.isIdentifier(node.expression) && node.expression.text === "require")) {
      const spec = node.arguments[0];
      if (spec && ts.isStringLiteralLike(spec)) modules(node, spec, true, [], true);
    }
    if (ts.isPropertyAccessExpression(node)) prop(node, node.expression, node.name.text);
    else if (ts.isElementAccessExpression(node) && ts.isStringLiteralLike(node.argumentExpression)) prop(node, node.expression, node.argumentExpression.text);
    else if (ts.isBindingElement(node) && ts.isObjectBindingPattern(node.parent)) {
      const owner = node.parent.parent;
      const key = node.propertyName ?? node.name;
      if (ts.isVariableDeclaration(owner) && owner.initializer && (ts.isIdentifier(key) || ts.isStringLiteralLike(key))) prop(node, owner.initializer, key.text);
    } else if (ts.isIdentifier(node) && (transport.has(node.text) || storage.has(node.text))) {
      checkBrowser(node, node.text, checker.getSymbolAtLocation(node));
    }
    ts.forEachChild(node, visit);
  };
  visit(source);
  return findings;
}
