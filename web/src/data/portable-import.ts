import { ApiError } from "../lib/errors";

// 与 server/internal/service/portable_import.go 的 PortableImportMaxBytes 双写。
export const PORTABLE_IMPORT_MAX_BYTES = 64 * 1024 * 1024;

export interface PortableMediaSample {
  recordKey: string;
  libraryName?: string;
  relativePath?: string;
  fileName: string;
  reason: string;
  candidates?: number;
  destination?: string[];
}

export interface PortableLibraryMiss {
  libraryName: string;
  reason: string;
  count: number;
}

export interface PortableImportSamples {
  ambiguous?: PortableMediaSample[];
  unmatched?: PortableMediaSample[];
  duplicateTargets?: PortableMediaSample[];
}

export interface PortableMediaReport {
  total: number;
  matched: number;
  matchedByLibraryPath: number;
  matchedByAssetId: number;
  matchedByFileName: number;
  ambiguous: number;
  unmatched: number;
  duplicateTargets: number;
  missingLibraries?: PortableLibraryMiss[];
}

export interface PortableAuthorReport {
  total: number;
  created: number;
  merged: number;
  placeholder: number;
  ambiguous: number;
  skipped: number;
  profilesPrepared: number;
  profilesSkipped: number;
  avatarsPrepared: number;
  profileBlocksRestored: number;
  profileCandidatesCreated: number;
  avatarsRestored: number;
  profileIssues: number;
}

export interface PortableTagReport {
  vocabularyTotal: number;
  vocabularyCreated: number;
  vocabularyExisting: number;
  vocabularyUnavailable: number;
  linksAdded: number;
  linksPresent: number;
  linksSkipped: number;
  assetsChanged: number;
}

export interface PortableAssetAuthorReport {
  total: number;
  linksAdded: number;
  linksPresent: number;
  skippedUnmatched: number;
  skippedAuthor: number;
  skippedCos: number;
  skippedPlaceholder: number;
  assetsChanged: number;
}

export interface PortableFavoriteReport {
  total: number;
  applied: number;
  already: number;
  skippedLocal: number;
  skippedUnmatched: number;
}

export interface PortableLikeReport {
  total: number;
  applied: number;
  already: number;
  skippedConflict: number;
  skippedNoLikes: number;
  skippedUnmatched: number;
}

export interface PortableEventReport {
  total: number;
  imported: number;
  already: number;
  skippedUnmatched: number;
  skippedConflict: number;
  skippedInvalid: number;
  views: number;
  plays: number;
  dwellSeconds: number;
}

export interface PortableTimelineReport {
  total: number;
  imported: number;
  already: number;
  skippedNotVideo: number;
  skippedUnmatched: number;
  skippedContentChanged: number;
}

export interface PortableFollowReport {
  total: number;
  marked: number;
  already: number;
  skippedLocal: number;
  skippedUnknown: number;
}

export interface PortableTxtReport {
  fragments: number;
  fragmentsImported: number;
  fragmentsExisting: number;
  listsActivated: number;
  issues?: string[];
}

export interface PortablePreferenceReport {
  present: boolean;
  imported: boolean;
  kept: boolean;
}

export interface PortableSkippedSection {
  section: string;
  count: number;
  reason: string;
}

export interface PortableImportReport {
  dryRun: boolean;
  alreadyImported: boolean;
  changed: boolean;
  sourceDigest: string;
  format: string;
  schemaVersion: number;
  appIdentifier: string;
  exportedAtMillis?: number;
  media: PortableMediaReport;
  authors: PortableAuthorReport;
  tags: PortableTagReport;
  assetAuthors: PortableAssetAuthorReport;
  favorites: PortableFavoriteReport;
  likes: PortableLikeReport;
  events: PortableEventReport;
  timeline: PortableTimelineReport;
  follows: PortableFollowReport;
  txt: PortableTxtReport;
  preferences: PortablePreferenceReport;
  skipped: PortableSkippedSection[];
  samples?: PortableImportSamples;
  warnings?: string[];
}

// 这条路由暂未写入 api/openapi.yaml：生成 SDK 由 CI 产出且禁止手改，因此按
// 仓库既有先例直接 fetch。OpenAPI 补齐后再改为数据层调用生成客户端。
export async function postPortableImport(
  file: File,
  dryRun: boolean,
  signal?: AbortSignal,
): Promise<PortableImportReport> {
  if (file.size > PORTABLE_IMPORT_MAX_BYTES) {
    throw new ApiError(413, "portable_import_too_large", "导入文件超过 64 MiB，请确认导出完整后重试。");
  }
  let response: Response;
  try {
    response = await fetch(
      `/api/v1/migration/portable/imports${dryRun ? "?dryRun=true" : ""}`,
      {
        method: "POST",
        body: file,
        credentials: "same-origin",
        headers: { "Content-Type": "application/json" },
        signal,
      },
    );
  } catch (error) {
    if (error instanceof DOMException && error.name === "AbortError") throw error;
    throw new ApiError(
      0,
      "network_unavailable",
      "无法连接影库。请检查网络与服务器；本次操作没有收到成功确认。",
    );
  }
  if (response.status === 401) window.dispatchEvent(new Event("qimeng-session-expired"));
  let body: unknown;
  try {
    body = await response.json();
  } catch {
    body = undefined;
  }
  if (!response.ok) {
    const error = body as
      | { code?: string; message?: string; currentRevision?: number; details?: Record<string, unknown> }
      | undefined;
    throw new ApiError(
      response.status,
      error?.code ?? "http_error",
      error?.message ?? `服务器返回 ${response.status}`,
      error?.currentRevision,
      error?.details,
    );
  }
  return body as PortableImportReport;
}
