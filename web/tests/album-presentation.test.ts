import { describe, expect, it } from "vitest";
import type { Asset } from "../src/data/media";
import { albumGroups } from "../src/lib/album-presentation";

describe("album date presentation", () => {
  it("uses the request offset and authoritative totals across page boundaries", () => {
    const first = { id: "first", fileDate: "2026-01-01T17:00:00Z" } as Asset;
    const second = {
      id: "second",
      fileDate: "2026-01-01T23:00:00Z",
    } as Asset;
    const counts = [{ date: "2026-01-02", fileCount: 100 }];
    expect(
      albumGroups([first], counts, 480, Date.parse("2026-01-02T02:00:00Z"))[0],
    ).toMatchObject({ key: "2026-01-02", label: "今天", total: 100 });
    expect(albumGroups([first, second], counts, 480)[0]?.total).toBe(100);
  });
  it("leaves unknown dates and missing totals unknown instead of using creation time or loaded count", () => {
    const unknown = {
      id: "unknown",
      createdAt: "2026-01-01T00:00:00Z",
    } as Asset;
    const known = { id: "known", fileDate: "2026-01-01T00:00:00Z" } as Asset;
    const groups = albumGroups([unknown, known], undefined, 0);
    expect(groups.map((group) => group.key)).toEqual(["2026-01-01", ""]);
    expect(groups.every((group) => group.total === undefined)).toBe(true);
    expect(albumGroups([unknown], [], 0, Date.now(), 42)[0]).toMatchObject({ key: "", label: "", total: 42 });
  });
});
