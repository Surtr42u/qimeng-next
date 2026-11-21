import type {
  PortableImportReport,
  PortableMediaSample,
} from "../data/portable-import";
import { count } from "./format";

export interface PortableReportRow {
  key: string;
  label: string;
  value: string;
  note?: string;
}

function n(value: number) {
  return count(value) ?? "0";
}

function percent(value: number, total: number) {
  return total > 0 ? Math.round((value * 100) / total) : 0;
}

function notes(parts: Array<string | undefined | false>) {
  return parts.filter((part): part is string => Boolean(part)).join(" · ");
}

export function mediaMatchRows(report: PortableImportReport): PortableReportRow[] {
  const media = report.media;
  return [
    { key: "total", label: "旧版目录条目", value: n(media.total) },
    {
      key: "libraryPath",
      label: "按库名 + 相对路径匹配",
      value: n(media.matchedByLibraryPath),
    },
    { key: "assetId", label: "按 assetId 匹配", value: n(media.matchedByAssetId) },
    {
      key: "fileName",
      label: "按库内唯一文件名匹配",
      value: n(media.matchedByFileName),
    },
    {
      key: "matched",
      label: "已匹配",
      value: n(media.matched),
      note: `${percent(media.matched, media.total)}%`,
    },
    { key: "ambiguous", label: "歧义（跳过）", value: n(media.ambiguous) },
    { key: "unmatched", label: "未匹配（跳过）", value: n(media.unmatched) },
    {
      key: "duplicates",
      label: "多个条目指向同一媒体（跳过）",
      value: n(media.duplicateTargets),
    },
  ];
}

// 只列出本次确实涉及或会写入的记录类，避免整页零值。
export function writeRows(report: PortableImportReport): PortableReportRow[] {
  const rows: PortableReportRow[] = [];
  const authors = report.authors;
  if (authors.total > 0 || authors.profilesPrepared > 0) {
    rows.push({
      key: "authors",
      label: "作者",
      value: `${n(authors.created)} 新建 · ${n(authors.merged)} 合并`,
      note: notes([
        authors.profilesPrepared > 0
          ? report.dryRun
            ? `${n(authors.profilesPrepared)} 份资料待处理`
            : `${n(authors.profilesPrepared)} 份资料已核对`
          : undefined,
        authors.profilesSkipped > 0 ? `${n(authors.profilesSkipped)} 份资料跳过` : undefined,
        authors.placeholder > 0 ? `${n(authors.placeholder)} 位其他搁置` : undefined,
        authors.profileBlocksRestored > 0 ? `${n(authors.profileBlocksRestored)} 块写入` : undefined,
        authors.profileCandidatesCreated > 0 ? `${n(authors.profileCandidatesCreated)} 条候选` : undefined,
        authors.avatarsRestored > 0 ? `${n(authors.avatarsRestored)} 个头像` : undefined,
      ]),
    });
  }
  const tags = report.tags;
  if (tags.vocabularyTotal > 0 || tags.linksAdded > 0) {
    rows.push({
      key: "tags",
      label: "标签",
      value: `${n(tags.vocabularyCreated)} 新词条 · ${n(tags.linksAdded)} 个关联`,
      note: notes([
        tags.linksPresent > 0 ? `${n(tags.linksPresent)} 个关联已有` : undefined,
        tags.vocabularyExisting > 0 ? `${n(tags.vocabularyExisting)} 个词条已存在` : undefined,
        tags.vocabularyUnavailable > 0 ? `${n(tags.vocabularyUnavailable)} 个词条已删除，跳过` : undefined,
      ]),
    });
  }
  const links = report.assetAuthors;
  if (links.total > 0) {
    rows.push({
      key: "assetAuthors",
      label: "作者与文件关联",
      value: `${n(links.linksAdded)} 个关联`,
      note: notes([
        links.linksPresent > 0 ? `${n(links.linksPresent)} 个已有` : undefined,
        links.skippedUnmatched > 0 ? `${n(links.skippedUnmatched)} 个文件未匹配` : undefined,
        links.skippedCos > 0 ? `${n(links.skippedCos)} 个 COS 目录关联跳过` : undefined,
        links.skippedPlaceholder > 0 ? `${n(links.skippedPlaceholder)} 个其他搁置跳过` : undefined,
      ]),
    });
  }
  const favorites = report.favorites;
  if (favorites.total > 0) {
    rows.push({
      key: "favorites",
      label: "收藏",
      value: `${n(favorites.applied)} 个写入`,
      note: notes([
        favorites.already > 0 ? `${n(favorites.already)} 个已收藏` : undefined,
        favorites.skippedLocal > 0 ? `${n(favorites.skippedLocal)} 个保留新版取消` : undefined,
        favorites.skippedUnmatched > 0 ? `${n(favorites.skippedUnmatched)} 个未匹配` : undefined,
      ]),
    });
  }
  const likes = report.likes;
  if (likes.total > 0) {
    rows.push({
      key: "likes",
      label: "累计点赞",
      value: `${n(likes.applied)} 条写入`,
      note: notes([
        likes.already > 0 ? `${n(likes.already)} 条已有` : undefined,
        likes.skippedConflict > 0 ? `${n(likes.skippedConflict)} 条与新版投票重叠，跳过` : undefined,
        likes.skippedNoLikes > 0 ? `${n(likes.skippedNoLikes)} 条无点赞` : undefined,
        likes.skippedUnmatched > 0 ? `${n(likes.skippedUnmatched)} 条未匹配` : undefined,
      ]),
    });
  }
  const events = report.events;
  if (events.total > 0) {
    rows.push({
      key: "events",
      label: "浏览事件",
      value: `${n(events.imported)} 条写入`,
      note: notes([
        events.views > 0 ? `${n(events.views)} 次浏览` : undefined,
        events.plays > 0 ? `${n(events.plays)} 次播放` : undefined,
        events.dwellSeconds > 0 ? `${n(events.dwellSeconds)} 秒停留` : undefined,
        events.already > 0 ? `${n(events.already)} 条已有` : undefined,
        events.skippedConflict > 0 ? `${n(events.skippedConflict)} 条冲突跳过` : undefined,
        events.skippedInvalid > 0 ? `${n(events.skippedInvalid)} 条无效` : undefined,
        events.skippedUnmatched > 0 ? `${n(events.skippedUnmatched)} 条未匹配` : undefined,
      ]),
    });
  }
  const timeline = report.timeline;
  if (timeline.total > 0) {
    rows.push({
      key: "timeline",
      label: "时间轴标记",
      value: `${n(timeline.imported)} 个写入`,
      note: notes([
        timeline.already > 0 ? `${n(timeline.already)} 个已有` : undefined,
        timeline.skippedNotVideo > 0 ? `${n(timeline.skippedNotVideo)} 个非视频` : undefined,
        timeline.skippedContentChanged > 0 ? `${n(timeline.skippedContentChanged)} 个原件已替换` : undefined,
        timeline.skippedUnmatched > 0 ? `${n(timeline.skippedUnmatched)} 个未匹配` : undefined,
      ]),
    });
  }
  const follows = report.follows;
  if (follows.total > 0) {
    rows.push({
      key: "follows",
      label: "关注",
      value: `${n(follows.marked)} 位写入`,
      note: notes([
        follows.already > 0 ? `${n(follows.already)} 位已关注` : undefined,
        follows.skippedLocal > 0 ? `${n(follows.skippedLocal)} 位保留新版取消` : undefined,
        follows.skippedUnknown > 0 ? `${n(follows.skippedUnknown)} 位未对应` : undefined,
      ]),
    });
  }
  const txt = report.txt;
  if (txt.fragments > 0) {
    rows.push({
      key: "txt",
      label: "作者 TXT",
      value: `${n(txt.fragmentsImported)} 个片段归档`,
      note: notes([
        txt.fragmentsExisting > 0 ? `${n(txt.fragmentsExisting)} 个已有` : undefined,
        txt.listsActivated > 0 ? `${n(txt.listsActivated)} 份清单激活` : undefined,
      ]),
    });
  }
  if (report.preferences.present) {
    rows.push({
      key: "preferences",
      label: "推荐偏好",
      value: report.preferences.imported
        ? "迁入"
        : report.preferences.kept
          ? "保留新版"
          : "无变化",
    });
  }
  return rows;
}

export function reportHeadline(report: PortableImportReport): string {
  if (report.alreadyImported) {
    return "此文件此前已导入，重复提交不会再写；下面是首次回执。";
  }
  if (report.dryRun) {
    return report.changed
      ? `预览完成：${n(report.media.matched)}/${n(report.media.total)} 条旧记录匹配，确认后会写入下列记录。`
      : "预览完成：没有需要写入的新记录。";
  }
  return report.changed
    ? "导入完成：旧记录已合并到当前媒体库。"
    : "导入完成：没有需要写入的新记录。";
}

export function mediaReasonLabel(reason: string): string {
  switch (reason) {
    case "library_name_missing":
      return "缺少库名";
    case "library_not_found":
      return "目标实例没有这个库名";
    case "library_name_ambiguous":
      return "目标实例有多个同名库";
    case "multiple_paths_in_library":
      return "同一库内路径重复";
    case "file_name_not_found":
      return "库内没有这个文件名";
    case "path_and_name_not_found":
      return "路径和文件名都没有对上";
    case "multiple_names_in_library":
      return "同一库内多个同名文件";
    case "multiple_sources_one_target":
      return "多个旧条目指向同一媒体";
    default:
      return reason;
  }
}

export function libraryMissReason(reason: string): string {
  switch (reason) {
    case "library_not_found":
      return "没有同名库";
    case "library_name_ambiguous":
      return "多个同名库";
    case "library_name_missing":
      return "缺少库名";
    default:
      return reason;
  }
}

export function skippedSectionLabel(section: string): string {
  switch (section) {
    case "mediaStats":
      return "旧版统计汇总";
    case "dailyBrowse":
      return "按天浏览";
    case "history":
      return "浏览历史";
    case "authorMasterTxt":
      return "作者主表 TXT";
    case "settings":
      return "旧版运行时设置";
    case "scanSources":
      return "旧版扫描目录";
    case "albumRules":
      return "相册规则";
    case "cosWorks":
      return "COS 作品目录";
    default:
      return section;
  }
}

export function skippedSectionReason(reason: string): string {
  switch (reason) {
    case "aggregate_derived_from_events":
      return "新版按浏览事件重建，不重复导入";
    case "derived_from_view_events":
      return "由浏览事件回放，不单独导入";
    case "master_txt_has_no_target":
      return "新版没有对应主表，未导入";
    case "runtime_settings_not_activated":
      return "运行时设置需在新版重新核对";
    case "scan_roots_not_registered":
      return "扫描目录以新版库配置为准";
    case "no_equivalent_capability":
      return "新版没有对应能力";
    case "derived_from_directories":
      return "由目录扫描派生";
    default:
      return reason;
  }
}

export function issueLabel(code: string): string {
  switch (code) {
    case "txt_fragment_archived":
      return "片段已归档（不是编号清单格式，未激活清单）";
    case "existing_txt_list_kept":
      return "同名清单已存在，保留本机内容";
    case "deleted_txt_list_kept":
      return "同名清单已被删除，未复活";
    case "txt_fragment_owner_or_source_unresolved":
      return "部分作者或出处无法对应，片段仅归档";
    case "line_outside_section":
      return "片段中有行不在任何区块内";
    default:
      return code;
  }
}

export function sampleLibraryLabel(sample: PortableMediaSample) {
  return sample.libraryName || "（未提供库名）";
}
