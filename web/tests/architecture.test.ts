import { describe, expect, it } from "vitest";
import ts from "typescript";
import { readFileSync, readdirSync } from "node:fs";
import { resolve } from "node:path";

function sources(directory: string): string[] {
  return readdirSync(directory, { withFileTypes: true }).flatMap((entry) =>
    entry.isDirectory()
      ? sources(resolve(directory, entry.name))
      : /\.(ts|tsx)$/.test(entry.name)
        ? [resolve(directory, entry.name)]
        : [],
  );
}
describe("presentation boundaries", () => {
  it("pages and visual components do not request the network or import runtime SDK values", () => {
    const violations: string[] = [];
    for (const file of [
      ...sources(resolve("src/pages")),
      ...sources(resolve("src/components")),
      ...sources(resolve("../ui/react")),
    ]) {
      const tree = ts.createSourceFile(
        file,
        readFileSync(file, "utf8"),
        ts.ScriptTarget.Latest,
        true,
        ts.ScriptKind.TSX,
      );
      const visit = (node: ts.Node) => {
        if (
          ts.isImportDeclaration(node) &&
          ts.isStringLiteral(node.moduleSpecifier)
        ) {
          const name = node.moduleSpecifier.text;
          if (
            (name.includes("/api/") || name.endsWith("/data/client")) &&
            !node.importClause?.isTypeOnly
          )
            violations.push(`${file}: runtime API import`);
        }
        if (
          ts.isCallExpression(node) &&
          ts.isIdentifier(node.expression) &&
          ["fetch", "XMLHttpRequest", "WebSocket", "EventSource"].includes(
            node.expression.text,
          )
        )
          violations.push(`${file}: network call`);
        if (
          ts.isNewExpression(node) &&
          ts.isIdentifier(node.expression) &&
          ["XMLHttpRequest", "WebSocket", "EventSource"].includes(
            node.expression.text,
          )
        )
          violations.push(`${file}: network constructor`);
        ts.forEachChild(node, visit);
      };
      visit(tree);
    }
    expect(violations).toEqual([]);
  });
  it("the reusable visual package has no application or data dependency", () => {
    for (const file of sources(resolve("../ui/react"))) {
      const source = readFileSync(file, "utf8");
      expect(source).not.toMatch(
        /from ['"][^'"]*(?:web\/|data\/|api\/|react-router)/,
      );
    }
  });
});
