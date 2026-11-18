import { beforeAll, describe, expect, it } from "vitest";
import ts from "typescript";
import { resolve } from "node:path";
import { presentationFindings } from "./presentation-boundary";

const roots = ["src/app", "src/pages", "src/components", "../ui/react"];
const views = roots.flatMap(dir => ts.sys.readDirectory(resolve(dir), [".ts", ".tsx"]));
const examples = new Map([
  [resolve("tests/.boundary-bridge.ts"), 'export { api as forwarded } from "../src/data/client";'],
  [resolve("tests/.boundary-allowed.tsx"), [
    'import { type Schema } from "../src/data/client";',
    'import { useAssets } from "../src/data/media";',
    'declare const src: string, receive: (s: string) => void;',
    'const thumb = new Image(); thumb.src = src;',
    'const login = <input type="password" onChange={e => receive(e.target.value)} />;',
    'const video = <video src={src} preload="metadata" />;',
    'const fixture = { fetch: () => "local definition" }; fixture.fetch();',
  ].join("\n")],
  [resolve("tests/.boundary-forbidden.tsx"), [
    'import { forwarded as client } from "./.boundary-bridge";',
    'const load = () => import("../src/data/client");',
    'const { fetch: send } = window;',
    'navigator.sendBeacon("/unexpected");',
    'const io = globalThis["XMLHttpRequest"];',
    'const cookie = document.cookie;',
    'const cache = window.localStorage;',
    'const ambient = import.meta.env.VITE_API_TOKEN;',
    'declare const request: import("../src/data/client").Schema<"SessionCreate">;',
    'const key = request.token;',
  ].join("\n")],
]);
let program: ts.Program;

beforeAll(() => {
  const configPath = resolve("tsconfig.json");
  const config = ts.readConfigFile(configPath, ts.sys.readFile);
  const { options } = ts.parseJsonConfigFileContent(config.config, ts.sys, process.cwd());
  const host = ts.createCompilerHost(options);
  const originalRead = host.readFile.bind(host);
  const originalExists = host.fileExists.bind(host);
  const originalSource = host.getSourceFile.bind(host);
  host.fileExists = file => examples.has(resolve(file)) || originalExists(file);
  host.readFile = file => examples.get(resolve(file)) ?? originalRead(file);
  host.getSourceFile = (file, ...args) => {
    const source = examples.get(resolve(file));
    return source === undefined ? originalSource(file, ...args) : ts.createSourceFile(file, source, ts.ScriptTarget.Latest, true);
  };
  program = ts.createProgram([...views, ...examples.keys()], options, host);
}, 30000);

describe("presentation boundaries", () => {
  it("keeps application transport, browser credential storage and runtime SDKs outside views", () => {
    const violations = views.flatMap(file => presentationFindings(program, program.getSourceFile(file)!));
    expect(violations).toEqual([]);
  }, 30000);
  it("allows typed hooks, media preloading and explicit login, and catches indirect transport and credential access", () => {
    const check = (name: string) => presentationFindings(program, program.getSourceFile(resolve("tests/.boundary-" + name + ".tsx"))!);
    expect(check("allowed")).toEqual([]);
    expect(check("forbidden").map(f => f.line)).toEqual([1, 2, 3, 4, 5, 6, 7, 8, 10]);
  });
});
