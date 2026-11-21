import { describe, expect, it } from "vitest";
import {
  createServerEventParser,
  refreshTargets,
} from "../src/data/server-events-core";

describe("server event parser", () => {
  it("parses one frame with event, data and id", () => {
    const parse = createServerEventParser();
    expect(parse('event: library.changed\ndata: null\nid: 1\n\n')).toEqual([
      { event: "library.changed", data: "null", id: "1" },
    ]);
  });

  it("joins multi-line data and ignores comments and heartbeats", () => {
    const parse = createServerEventParser();
    expect(parse(": ping\n\nevent: scan.progress\ndata: {\"a\":1}\ndata: tail\n\n")).toEqual([
      { event: "scan.progress", data: '{"a":1}\ntail' },
    ]);
  });

  it("keeps a partial line and a partial frame across chunks", () => {
    const parse = createServerEventParser();
    expect(parse("event: favorite.chan")).toEqual([]);
    expect(parse("ged\nda")).toEqual([]);
    expect(parse('ta: {"assetId":"a1"}\nid: 7\n')).toEqual([]);
    expect(parse("\n")).toEqual([
      { event: "favorite.changed", data: '{"assetId":"a1"}', id: "7" },
    ]);
  });

  it("returns several frames from one chunk and tolerates CRLF", () => {
    const parse = createServerEventParser();
    expect(parse("retry: 3000\r\n\r\nevent: hello\r\ndata: {\"version\":\"0.1.0\"}\r\nid: 0\r\n\r\n")).toEqual([
      { event: "message", data: "", retry: 3000 },
      { event: "hello", data: '{"version":"0.1.0"}', id: "0" },
    ]);
  });

  it("ignores invalid retry values", () => {
    const parse = createServerEventParser();
    expect(parse("retry: nope\nevent: x\ndata: y\n\n")).toEqual([
      { event: "x", data: "y" },
    ]);
  });
});

describe("event to refresh mapping", () => {
  it("maps known topics to their data family", () => {
    expect(refreshTargets("library.changed")).toEqual(["library"]);
    expect(refreshTargets("upload.done")).toEqual(["library"]);
    expect(refreshTargets("favorite.changed")).toEqual(["favorite"]);
    expect(refreshTargets("like.changed")).toEqual(["like"]);
  });

  it("ignores hello and unknown future topics", () => {
    expect(refreshTargets("hello")).toEqual([]);
    expect(refreshTargets("scan.progress")).toEqual([]);
    expect(refreshTargets("future.topic")).toEqual([]);
  });
});
