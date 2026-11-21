import { describe, expect, it } from "vitest";
import type { PortableImportReport } from "../src/data/portable-import";
import {
  issueLabel,
  libraryMissReason,
  mediaMatchRows,
  mediaReasonLabel,
  reportHeadline,
  skippedSectionLabel,
  skippedSectionReason,
  writeRows,
} from "../src/lib/portable-import";

function report(overrides: Partial<PortableImportReport> = {}): PortableImportReport {
  return {
    dryRun: true,
    alreadyImported: false,
    changed: true,
    sourceDigest: "d".repeat(64),
    format: "qimeng_backup",
    schemaVersion: 2,
    appIdentifier: "com.qimeng.media",
    media: {
      total: 8,
      matched: 4,
      matchedByLibraryPath: 3,
      matchedByAssetId: 1,
      matchedByFileName: 0,
      ambiguous: 1,
      unmatched: 1,
      duplicateTargets: 2,
    },
    authors: {
      total: 2,
      created: 1,
      merged: 1,
      placeholder: 0,
      ambiguous: 0,
      skipped: 0,
      profilesPrepared: 1,
      profilesSkipped: 0,
      avatarsPrepared: 1,
      profileBlocksRestored: 0,
      profileCandidatesCreated: 0,
      avatarsRestored: 0,
      profileIssues: 0,
    },
    tags: {
      vocabularyTotal: 2,
      vocabularyCreated: 1,
      vocabularyExisting: 1,
      vocabularyUnavailable: 0,
      linksAdded: 2,
      linksPresent: 0,
      linksSkipped: 0,
      assetsChanged: 2,
    },
    assetAuthors: {
      total: 2,
      linksAdded: 2,
      linksPresent: 0,
      skippedUnmatched: 0,
      skippedAuthor: 0,
      skippedCos: 0,
      skippedPlaceholder: 0,
      assetsChanged: 2,
    },
    favorites: { total: 1, applied: 1, already: 0, skippedLocal: 0, skippedUnmatched: 0 },
    likes: { total: 1, applied: 1, already: 0, skippedConflict: 0, skippedNoLikes: 0, skippedUnmatched: 0 },
    events: {
      total: 2,
      imported: 2,
      already: 0,
      skippedUnmatched: 0,
      skippedConflict: 0,
      skippedInvalid: 0,
      views: 1,
      plays: 0,
      dwellSeconds: 13,
    },
    timeline: {
      total: 1,
      imported: 1,
      already: 0,
      skippedNotVideo: 0,
      skippedUnmatched: 0,
      skippedContentChanged: 0,
    },
    follows: { total: 1, marked: 1, already: 0, skippedLocal: 0, skippedUnknown: 0 },
    txt: { fragments: 1, fragmentsImported: 1, fragmentsExisting: 0, listsActivated: 0 },
    preferences: { present: true, imported: true, kept: false },
    skipped: [
      { section: "mediaStats", count: 3, reason: "aggregate_derived_from_events" },
      { section: "history", count: 5, reason: "derived_from_view_events" },
    ],
    samples: {
      unmatched: [
        {
          recordKey: "missing.png",
          libraryName: "1  图集",
          relativePath: "missing.png",
          fileName: "missing.png",
          reason: "library_not_found",
        },
      ],
    },
    warnings: ["测试提示"],
    ...overrides,
  };
}

describe("portable import report mapping", () => {
  it("reports the fixed match order and the matched ratio", () => {
    const rows = mediaMatchRows(report());
    const byKey = new Map(rows.map((row) => [row.key, row] as const));
    expect(byKey.get("libraryPath")?.label).toContain("库名 + 相对路径");
    expect(byKey.get("libraryPath")?.value).toBe("3");
    expect(byKey.get("assetId")?.value).toBe("1");
    expect(byKey.get("fileName")?.value).toBe("0");
    expect(byKey.get("matched")?.note).toBe("50%");
    expect(byKey.get("ambiguous")?.value).toBe("1");
    expect(byKey.get("unmatched")?.value).toBe("1");
    expect(byKey.get("duplicates")?.value).toBe("2");
  });

  it("lists only the record classes present in the document", () => {
    const rows = writeRows(report());
    const byKey = new Map(rows.map((row) => [row.key, row] as const));
    expect(byKey.get("authors")?.value).toContain("1 新建");
    expect(byKey.get("authors")?.note).toContain("1 份资料");
    expect(byKey.get("tags")?.value).toContain("2 个关联");
    expect(byKey.get("events")?.note).toContain("13 秒停留");
    expect(byKey.get("preferences")?.value).toBe("迁入");
    expect(rows.some((row) => row.key === "timeline")).toBe(true);
  });

  it("distinguishes preview, applied, unchanged and already imported", () => {
    expect(reportHeadline(report())).toContain("预览完成");
    expect(reportHeadline(report({ dryRun: false }))).toContain("导入完成");
    expect(reportHeadline(report({ dryRun: false, changed: false }))).toContain("没有需要写入");
    expect(reportHeadline(report({ alreadyImported: true }))).toContain("此前已导入");
  });

  it("maps skip reasons and section names for display", () => {
    expect(mediaReasonLabel("library_not_found")).toContain("没有这个库名");
    expect(mediaReasonLabel("multiple_names_in_library")).toContain("多个同名文件");
    expect(libraryMissReason("library_name_ambiguous")).toBe("多个同名库");
    expect(skippedSectionLabel("mediaStats")).toBe("旧版统计汇总");
    expect(skippedSectionReason("aggregate_derived_from_events")).toContain("浏览事件");
    expect(issueLabel("txt_fragment_archived")).toContain("归档");
  });
});
