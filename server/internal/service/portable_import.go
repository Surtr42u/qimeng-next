// 旧版便携备份合并导入（预览 + 应用）。
//
// 输入是旧应用导出的单个 qimeng_backup v2 JSON。导入只把记录合并到已经扫描
// 完成的媒体库：不触碰文件系统、不新建空库、不删除既有行。
//
// 实库核对（2026-11-21 导出，8,241 个媒体）确定匹配顺序：
//  1. 旧库名必须在目标实例唯一命中，然后 (libraryName, relPath) 精确匹配；
//  2. 目标库恰好带着同一 assetId（只接受同一库内的同 id，跨库不认领）；
//  3. 同一目标库内 fileName 唯一。
//
// 两个路径都按 / 归一、区分大小写、保留空格（如 "1  图集" 的双空格）再比较。
// 库名缺失/多义或同一库内同名多行一律跳过，绝不跨库按文件名猜测，也绝不按
// 排序取最早行。未命中的旧库名按名称汇总回传，便于核对。
//
// 注意：真实旧导出的 open/play 事件没有 seconds 键、多数事件没有 day 键，因此
// 这里使用独立的宽容 DTO，不复用 migration.DecodeV2 对事件必填键的假设。
package service

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"qimeng.local/next/server/internal/analytics"
	"qimeng.local/next/server/internal/authorlist"
	legacy "qimeng.local/next/server/internal/migration"
	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/store"
)

const (
	// PortableImportMaxBytes 与旧应用导出端点一致：单文件全量 JSON 随库规模
	// 增长，64 MiB 可容纳数十万文件级库。HTTP 边界直接引用本常量。
	PortableImportMaxBytes = 64 << 20

	portableAppIdentifier = "com.qimeng.media"
	portableFormat        = "qimeng_backup"
	portableSchemaVersion = 2

	// 回执写在既有迁移来源表里，不新增表：
	// input_id = portable-v2:<sha256>, kind = portable-v2/import。
	portableReceiptPrefix = "portable-v2:"
	portableReceiptKind   = "portable-v2/import"

	// 旧导出没有安装标识，但点赞基线、时间轴标记和 TXT 片段的身份必须跨多次
	// 导出保持稳定，否则新导出会重复插入。这里用固定来源名；同一目标只对应
	// 一个旧库来源，真正的逐次来源仍由回执的 sha256 记录。
	portableLegacySource = "portable-v2-legacy"

	// 与旧版 v2 校验一致的长度预算。
	portableMaxAuthorProfile = 256 << 10
	portableMaxAvatarBase64  = 2796204
)

// ---------- 旧版 JSON DTO（宽容缺省键） ----------

type portableBackup struct {
	Format           string             `json:"format"`
	SchemaVersion    int                `json:"schemaVersion"`
	AppIdentifier    string             `json:"appIdentifier"`
	ExportedAtMillis int64              `json:"exportedAtMillis"`
	Data             portableBackupData `json:"data"`
}

type portableBackupData struct {
	MediaFiles        []portableMedia     `json:"mediaFiles"`
	ViewEvents        []portableEvent     `json:"viewEvents"`
	Authors           []portableAuthor    `json:"authors"`
	AuthorMediaRefs   []portableAuthorRef `json:"authorMediaRefs"`
	Tags              []portableTag       `json:"tags"`
	MediaTagRefs      []portableTagRef    `json:"mediaTagRefs"`
	Likes             []portableLike      `json:"likes"`
	Favorites         []string            `json:"favorites"`
	FollowedAuthorIDs []string            `json:"followedAuthorIds"`
	TimelineTags      []portableTimeline  `json:"timelineTags"`
	TxtFragments      []portableFragment  `json:"txtFragments"`
	AppPrefs          json.RawMessage     `json:"appPrefs"`
	Settings          []portableSetting   `json:"settings"`

	// 不导入但需要计数的旧段。真实导出恒为空数组或派生汇总。
	MediaStats      []json.RawMessage `json:"mediaStats"`
	DailyBrowse     []json.RawMessage `json:"dailyBrowse"`
	History         []json.RawMessage `json:"history"`
	AuthorMasterTxt *string           `json:"authorMasterTxt"`
	AlbumRules      []json.RawMessage `json:"albumRules"`
	CosWorks        []json.RawMessage `json:"cosWorks"`
	ScanSources     []json.RawMessage `json:"scanSources"`
}

type portableMedia struct {
	AssetID             string `json:"assetId"`
	LibraryName         string `json:"libraryName"`
	RelPath             string `json:"relPath"`
	RecordKey           string `json:"recordKey"`
	FileName            string `json:"fileName"`
	MediaType           string `json:"mediaType"`
	SizeBytes           int64  `json:"sizeBytes"`
	ModifiedAtMillis    int64  `json:"modifiedAtMillis"`
	IndexedAtMillis     int64  `json:"indexedAtMillis"`
	TagsUpdatedAtMillis *int64 `json:"tagsUpdatedAtMillis"`
	IsCOSFile           bool   `json:"isCosFile"`
}

// portableEvent 的 Seconds 可缺省：真实导出的 open/play 事件不写该键。
type portableEvent struct {
	EventID   string `json:"eventId"`
	RecordKey string `json:"recordKey"`
	Kind      string `json:"kind"`
	SessionID string `json:"sessionId"`
	StartedAt string `json:"startedAt"`
	Seconds   *int64 `json:"seconds"`
	Day       string `json:"day"`
}

type portableAuthor struct {
	AuthorID        string  `json:"authorId"`
	DisplayName     string  `json:"displayName"`
	AvatarData      string  `json:"avatarData"`
	ProfileJSON     string  `json:"profileJson"`
	CreatedAtMillis int64   `json:"createdAtMillis"`
	UpdatedAtMillis int64   `json:"updatedAtMillis"`
	Origin          *string `json:"origin"`
}

type portableAuthorRef struct {
	AuthorID        string  `json:"authorId"`
	RecordKey       string  `json:"recordKey"`
	FileName        string  `json:"fileName"`
	CreatedAtMillis *int64  `json:"createdAtMillis"`
	Origin          *string `json:"origin"`
}

type portableTag struct {
	Name            string `json:"name"`
	CreatedAtMillis *int64 `json:"createdAtMillis"`
}

type portableTagRef struct {
	RecordKey       string  `json:"recordKey"`
	TagName         string  `json:"tagName"`
	CreatedAtMillis *int64  `json:"createdAtMillis"`
	Origin          *string `json:"origin"`
}

type portableLike struct {
	RecordKey    string  `json:"recordKey"`
	LikeCount    int64   `json:"likeCount"`
	LastLikeDate *string `json:"lastLikeDate"`
}

type portableTimeline struct {
	RecordKey       string `json:"recordKey"`
	FileName        string `json:"fileName"`
	TimeMillis      int64  `json:"timeMillis"`
	Name            string `json:"name"`
	Color           string `json:"color"`
	CreatedAtMillis int64  `json:"createdAtMillis"`
}

type portableFragment struct {
	Filename         string `json:"filename"`
	Content          string `json:"content"`
	ImportedAtMillis int64  `json:"importedAtMillis"`
}

type portableSetting struct {
	Key             string `json:"key"`
	Value           string `json:"value"`
	UpdatedAtMillis int64  `json:"updatedAtMillis"`
}

func (m portableMedia) legacy() legacy.Media {
	return legacy.Media{
		RecordKey: m.RecordKey, FileName: m.FileName, AssetID: m.AssetID,
		LibraryName: m.LibraryName, RelativePath: portablePath(m.RelPath), MediaType: m.MediaType,
		SizeBytes: m.SizeBytes, ModifiedAtMillis: m.ModifiedAtMillis, IndexedAtMillis: m.IndexedAtMillis,
		TagsUpdatedAtMillis: m.TagsUpdatedAtMillis, IsCOS: m.IsCOSFile,
	}
}

func (e portableEvent) legacy() legacy.Event {
	return legacy.Event{EventID: e.EventID, RecordKey: e.RecordKey, Kind: e.Kind, SessionID: e.SessionID,
		StartedAt: e.StartedAt, Seconds: e.Seconds, Day: e.Day}
}

func (a portableAuthor) legacy() legacy.Author {
	return legacy.Author{AuthorID: a.AuthorID, DisplayName: a.DisplayName, CreatedAtMillis: a.CreatedAtMillis,
		UpdatedAtMillis: a.UpdatedAtMillis, Origin: a.Origin, ProfileJSON: a.ProfileJSON, AvatarData: a.AvatarData}
}

func (l portableLike) legacy() legacy.Like {
	return legacy.Like{RecordKey: l.RecordKey, LikeCount: l.LikeCount, LastLikeDate: l.LastLikeDate}
}

func (t portableTimeline) legacy() legacy.Timeline {
	return legacy.Timeline{RecordKey: t.RecordKey, FileName: t.FileName, TimeMillis: t.TimeMillis,
		Name: t.Name, Color: t.Color, CreatedAtMillis: t.CreatedAtMillis}
}

func (f portableFragment) legacy() legacy.Fragment {
	return legacy.Fragment{Filename: f.Filename, Content: f.Content, ImportedAtMillis: f.ImportedAtMillis}
}

func portablePath(value string) string { return strings.ReplaceAll(value, "\\", "/") }

// ---------- 报告 ----------

// PortableMediaSample 是歧义、未匹配或多对一样本；最多保留 20 条。
// libraryName / relativePath 原样带回；destination 列出目标侧候选，便于核对。
type PortableMediaSample struct {
	RecordKey    string   `json:"recordKey"`
	LibraryName  string   `json:"libraryName,omitempty"`
	RelativePath string   `json:"relativePath,omitempty"`
	FileName     string   `json:"fileName"`
	Reason       string   `json:"reason"`
	Candidates   int      `json:"candidates,omitempty"`
	Destination  []string `json:"destination,omitempty"`
}

// PortableLibraryMiss 按旧库名汇总未解析的条目：同名库缺失或多义都不会猜。
type PortableLibraryMiss struct {
	LibraryName string `json:"libraryName"`
	Reason      string `json:"reason"`
	Count       int    `json:"count"`
}

type PortableImportSamples struct {
	Ambiguous        []PortableMediaSample `json:"ambiguous,omitempty"`
	Unmatched        []PortableMediaSample `json:"unmatched,omitempty"`
	DuplicateTargets []PortableMediaSample `json:"duplicateTargets,omitempty"`
}

type PortableMediaReport struct {
	Total                int                   `json:"total"`
	Matched              int                   `json:"matched"`
	MatchedByLibraryPath int                   `json:"matchedByLibraryPath"`
	MatchedByAssetID     int                   `json:"matchedByAssetId"`
	MatchedByFileName    int                   `json:"matchedByFileName"`
	Ambiguous            int                   `json:"ambiguous"`
	Unmatched            int                   `json:"unmatched"`
	DuplicateTargets     int                   `json:"duplicateTargets"`
	MissingLibraries     []PortableLibraryMiss `json:"missingLibraries,omitempty"`
}

type PortableAuthorReport struct {
	Total                    int `json:"total"`
	Created                  int `json:"created"`
	Merged                   int `json:"merged"`
	Placeholder              int `json:"placeholder"`
	Ambiguous                int `json:"ambiguous"`
	Skipped                  int `json:"skipped"`
	ProfilesPrepared         int `json:"profilesPrepared"`
	ProfilesSkipped          int `json:"profilesSkipped"`
	AvatarsPrepared          int `json:"avatarsPrepared"`
	ProfileBlocksRestored    int `json:"profileBlocksRestored"`
	ProfileCandidatesCreated int `json:"profileCandidatesCreated"`
	AvatarsRestored          int `json:"avatarsRestored"`
	ProfileIssues            int `json:"profileIssues"`
}

type PortableTagReport struct {
	VocabularyTotal       int `json:"vocabularyTotal"`
	VocabularyCreated     int `json:"vocabularyCreated"`
	VocabularyExisting    int `json:"vocabularyExisting"`
	VocabularyUnavailable int `json:"vocabularyUnavailable"`
	LinksAdded            int `json:"linksAdded"`
	LinksPresent          int `json:"linksPresent"`
	LinksSkipped          int `json:"linksSkipped"`
	AssetsChanged         int `json:"assetsChanged"`
}

type PortableAssetAuthorReport struct {
	Total              int `json:"total"`
	LinksAdded         int `json:"linksAdded"`
	LinksPresent       int `json:"linksPresent"`
	SkippedUnmatched   int `json:"skippedUnmatched"`
	SkippedAuthor      int `json:"skippedAuthor"`
	SkippedCos         int `json:"skippedCos"`
	SkippedPlaceholder int `json:"skippedPlaceholder"`
	AssetsChanged      int `json:"assetsChanged"`
}

type PortableFavoriteReport struct {
	Total            int `json:"total"`
	Applied          int `json:"applied"`
	Already          int `json:"already"`
	SkippedLocal     int `json:"skippedLocal"`
	SkippedUnmatched int `json:"skippedUnmatched"`
}

type PortableLikeReport struct {
	Total            int `json:"total"`
	Applied          int `json:"applied"`
	Already          int `json:"already"`
	SkippedConflict  int `json:"skippedConflict"`
	SkippedNoLikes   int `json:"skippedNoLikes"`
	SkippedUnmatched int `json:"skippedUnmatched"`
}

type PortableEventReport struct {
	Total            int   `json:"total"`
	Imported         int   `json:"imported"`
	Already          int   `json:"already"`
	SkippedUnmatched int   `json:"skippedUnmatched"`
	SkippedConflict  int   `json:"skippedConflict"`
	SkippedInvalid   int   `json:"skippedInvalid"`
	Views            int64 `json:"views"`
	Plays            int64 `json:"plays"`
	DwellSeconds     int64 `json:"dwellSeconds"`
}

type PortableTimelineReport struct {
	Total                 int `json:"total"`
	Imported              int `json:"imported"`
	Already               int `json:"already"`
	SkippedNotVideo       int `json:"skippedNotVideo"`
	SkippedUnmatched      int `json:"skippedUnmatched"`
	SkippedContentChanged int `json:"skippedContentChanged"`
}

type PortableFollowReport struct {
	Total          int `json:"total"`
	Marked         int `json:"marked"`
	Already        int `json:"already"`
	SkippedLocal   int `json:"skippedLocal"`
	SkippedUnknown int `json:"skippedUnknown"`
}

type PortableTxtReport struct {
	Fragments         int      `json:"fragments"`
	FragmentsImported int      `json:"fragmentsImported"`
	FragmentsExisting int      `json:"fragmentsExisting"`
	ListsActivated    int      `json:"listsActivated"`
	Issues            []string `json:"issues,omitempty"`
}

type PortablePreferenceReport struct {
	Present  bool `json:"present"`
	Imported bool `json:"imported"`
	Kept     bool `json:"kept"`
}

type PortableSkippedSection struct {
	Section string `json:"section"`
	Count   int    `json:"count"`
	Reason  string `json:"reason"`
}

// PortableImportReport 是预览与应用共用的结果。dryRun 为真时所有写入计数
// 表示“将写入”；应用后表示实际写入。alreadyImported 为真时来自首次回执。
type PortableImportReport struct {
	DryRun           bool                      `json:"dryRun"`
	AlreadyImported  bool                      `json:"alreadyImported"`
	Changed          bool                      `json:"changed"`
	SourceDigest     string                    `json:"sourceDigest"`
	Format           string                    `json:"format"`
	SchemaVersion    int                       `json:"schemaVersion"`
	AppIdentifier    string                    `json:"appIdentifier"`
	ExportedAtMillis int64                     `json:"exportedAtMillis,omitempty"`
	Media            PortableMediaReport       `json:"media"`
	Authors          PortableAuthorReport      `json:"authors"`
	Tags             PortableTagReport         `json:"tags"`
	AssetAuthors     PortableAssetAuthorReport `json:"assetAuthors"`
	Favorites        PortableFavoriteReport    `json:"favorites"`
	Likes            PortableLikeReport        `json:"likes"`
	Events           PortableEventReport       `json:"events"`
	Timeline         PortableTimelineReport    `json:"timeline"`
	Follows          PortableFollowReport      `json:"follows"`
	Txt              PortableTxtReport         `json:"txt"`
	Preferences      PortablePreferenceReport  `json:"preferences"`
	Skipped          []PortableSkippedSection  `json:"skipped"`
	Samples          PortableImportSamples     `json:"samples,omitempty"`
	Warnings         []string                  `json:"warnings,omitempty"`
}

// ---------- 计划 ----------

type portableAssetRow struct {
	ID           string
	LibraryID    string
	FileName     string
	RelativePath string
	LibraryName  string
	LibraryKind  string
}

type portableLibraryRow struct {
	ID   string
	Name string
	Kind string
}

type portableMatch struct {
	Row   portableAssetRow
	Key   string
	Media portableMedia
}

type portableAuthorPlan struct {
	Source      portableAuthor
	Local       model.Author
	Create      bool
	Placeholder bool
	SkipReason  string
}

type portableProfilePlan struct {
	SourceAuthorID string
	LocalAuthorID  string
	AuthorName     string
	ArchiveID      string
	Prepared       *ImportedAuthorProfile
}

type portableTagPlan struct {
	Name        string
	ID          string
	Create      bool
	Unavailable bool
}

type portableFavoritePlan struct {
	RecordKey string
	AssetID   string
}

type portableLikePlan struct {
	RecordKey string
	AssetID   string
	Action    string
	Like      portableLike
	Media     portableMedia
}

type portableEventPlan struct {
	Event   portableEvent
	AssetID string
	Action  string
}

type portableTimelinePlan struct {
	Mark    portableTimeline
	AssetID string
	Action  string
	ID      string
}

type portableFollowPlan struct {
	SourceAuthorID string
	AuthorID       string
	Action         string
}

type portableAuthorWork struct {
	AssetID  string
	AuthorID string
	FileName string
}

type portableImportPlan struct {
	report         PortableImportReport
	input          portableBackup
	sourceID       string
	sourceDigest   string
	matches        map[string]portableMatch
	mediaByKey     map[string]portableMedia
	assetMedia     map[string]portableMedia
	assets         map[string]model.Asset
	authors        []*portableAuthorPlan
	authorBySource map[string]*portableAuthorPlan
	localAuthors   map[string]model.Author
	profiles       []portableProfilePlan
	tags           map[string]*portableTagPlan
	tagExisting    map[string]map[string]bool
	tagIncoming    map[string][]string
	authorExisting map[string][]string
	authorIncoming map[string][]string
	// 旧作者关联到的新版库类型与库 id，用于决定 COS 目录作者落在哪种身份。
	authorAssetKinds  map[string]map[string]bool
	authorLibraries   map[string][]string
	authorDirectories map[string][]string
	favoritePlans     []portableFavoritePlan
	likePlans         []portableLikePlan
	eventPlans        []portableEventPlan
	timelinePlans     []portableTimelinePlan
	followPlans       []portableFollowPlan
	linkPlans         []portableAuthorWork
	weights           *analytics.Weights
	fragments         []portableFragment
	warnings          []string
}

func portableSourceID(sourceDigest string) string { return portableReceiptPrefix + sourceDigest }

func portableErrorCode(err error) string {
	var app *Error
	if errors.As(err, &app) {
		return app.Code
	}
	return ""
}

func portableTime(ms int64, fallback time.Time) time.Time {
	if ms <= 0 {
		return fallback
	}
	value := time.UnixMilli(ms).UTC()
	if value.After(fallback) {
		return fallback
	}
	return value
}

func portableMillisTime(ms *int64, now time.Time) *time.Time {
	if ms == nil || *ms <= 0 {
		return nil
	}
	value := time.UnixMilli(*ms).UTC()
	if value.After(now) {
		return nil
	}
	return &value
}

func portableSample(f portableMedia, reason string, candidates int, destination []string) PortableMediaSample {
	return PortableMediaSample{RecordKey: f.RecordKey, LibraryName: f.LibraryName, RelativePath: f.RelPath,
		FileName: f.FileName, Reason: reason, Candidates: candidates, Destination: destination}
}

func appendPortableSample(samples *[]PortableMediaSample, value PortableMediaSample) {
	if len(*samples) < 20 {
		*samples = append(*samples, value)
	}
}

func portableRowLabels(rows []portableAssetRow) []string {
	result := []string{}
	for index, row := range rows {
		if index >= 3 {
			break
		}
		result = append(result, row.LibraryName+" / "+row.RelativePath)
	}
	return result
}

func (p *portableImportPlan) warn(format string, args ...any) {
	msg := format
	if len(args) > 0 {
		msg = fmt.Sprintf(format, args...)
	}
	for _, existing := range p.warnings {
		if existing == msg {
			return
		}
	}
	if len(p.warnings) < 60 {
		p.warnings = append(p.warnings, msg)
	}
}

// ---------- 解析与校验 ----------

func invalidPortable(message string) error {
	return Fail(400, "invalid_portable_import", message)
}

func decodePortableBackup(raw []byte) (portableBackup, error) {
	var input portableBackup
	if len(raw) == 0 {
		return input, Fail(400, "portable_import_empty", "请选择旧版导出的 JSON 文件。")
	}
	if len(raw) > PortableImportMaxBytes {
		return input, Fail(413, "portable_import_too_large", "导入文件超出 64 MiB，请确认导出完整后重试。")
	}
	if e := json.Unmarshal(raw, &input); e != nil {
		if strings.Contains(e.Error(), "unexpected end of JSON") {
			return input, Fail(400, "invalid_json", "导入文件不是完整的 JSON（可能被截断）；请重新选择旧版导出的文件。")
		}
		return input, invalidPortable("导入文件的 JSON 结构不符合旧版 v2 备份：" + e.Error())
	}
	if input.Format != portableFormat {
		return input, Fail(400, "unsupported_portable_format", "仅支持旧版绮梦导出的 qimeng_backup 文件。")
	}
	if input.SchemaVersion != portableSchemaVersion {
		return input, Fail(400, "unsupported_schema_version", "仅支持 schemaVersion=2 的旧版备份。")
	}
	if input.AppIdentifier != portableAppIdentifier {
		return input, Fail(400, "unsupported_app_identifier", "文件不是旧版绮梦导出（appIdentifier 不匹配）。")
	}
	if e := validatePortableBackup(input); e != nil {
		return input, e
	}
	return input, nil
}

// validatePortableBackup 只校验导入实际使用和引用的形状；seconds/day 可以缺省，
// 因为真实旧导出对 open/play 事件不写 seconds、多数事件不写 day。
func validatePortableBackup(input portableBackup) error {
	if input.Data.ViewEvents == nil {
		return invalidPortable("导入文件缺少 viewEvents 段（旧版 v2 要求该段存在，可为空数组）。")
	}
	media := map[string]bool{}
	for index, item := range input.Data.MediaFiles {
		field := fmt.Sprintf("data.mediaFiles[%d]", index)
		if item.RecordKey == "" || len(item.RecordKey) > 4096 || media[item.RecordKey] {
			return invalidPortable(field + " 的 recordKey 缺失或重复。")
		}
		if item.FileName == "" || len(item.FileName) > 1024 || item.SizeBytes < 0 {
			return invalidPortable(field + " 的文件名或大小无效。")
		}
		if item.MediaType != "image" && item.MediaType != "animated_image" && item.MediaType != "video" {
			return invalidPortable(field + " 的媒体类型无效。")
		}
		media[item.RecordKey] = true
	}
	events := map[string]bool{}
	for index, event := range input.Data.ViewEvents {
		field := fmt.Sprintf("data.viewEvents[%d]", index)
		if event.EventID == "" || len(event.EventID) > 4096 || events[event.EventID] || !media[event.RecordKey] {
			return invalidPortable(field + " 的事件身份缺失、重复或引用了未知媒体。")
		}
		if event.Kind != "open" && event.Kind != "play" && event.Kind != "dwell" {
			return invalidPortable(field + " 的事件类型无效。")
		}
		if _, e := time.Parse(time.RFC3339Nano, event.StartedAt); e != nil {
			return invalidPortable(field + " 的原始事件时刻无效。")
		}
		if event.Seconds != nil && *event.Seconds < 0 {
			return invalidPortable(field + " 的事件秒数不得为负。")
		}
		if event.Day != "" {
			if _, e := time.Parse("2006-01-02", event.Day); e != nil {
				return invalidPortable(field + " 的日键无效。")
			}
		}
		events[event.EventID] = true
	}
	authors := map[string]bool{}
	for index, author := range input.Data.Authors {
		field := fmt.Sprintf("data.authors[%d]", index)
		if author.AuthorID == "" || authors[author.AuthorID] || strings.TrimSpace(author.DisplayName) == "" {
			return invalidPortable(field + " 的作者身份缺失、重复或名称为空。")
		}
		if len(author.ProfileJSON) > portableMaxAuthorProfile || len(author.AvatarData) > portableMaxAvatarBase64 {
			return invalidPortable(field + " 的资料或头像超过 v2 上限。")
		}
		if author.ProfileJSON != "" {
			var profile map[string]json.RawMessage
			if json.Unmarshal([]byte(author.ProfileJSON), &profile) != nil || profile == nil {
				return invalidPortable(field + " 的 profileJson 必须是 JSON 对象。")
			}
		}
		if author.AvatarData != "" && author.ProfileJSON == "" {
			return invalidPortable(field + " 的离线头像缺少作者资料。")
		}
		authors[author.AuthorID] = true
	}
	for _, ref := range input.Data.AuthorMediaRefs {
		if !media[ref.RecordKey] || !authors[ref.AuthorID] {
			return invalidPortable("data.authorMediaRefs 存在未解析的媒体或作者引用。")
		}
	}
	for _, ref := range input.Data.MediaTagRefs {
		name := strings.TrimSpace(ref.TagName)
		if !media[ref.RecordKey] || name == "" || len(name) > 100 || strings.ContainsRune(name, '\x00') {
			return invalidPortable("data.mediaTagRefs 存在未解析的媒体或无效标签名。")
		}
	}
	for _, tag := range input.Data.Tags {
		name := strings.TrimSpace(tag.Name)
		if name == "" || len(name) > 100 || strings.ContainsRune(name, '\x00') {
			return invalidPortable("data.tags 存在无效标签名。")
		}
	}
	for _, record := range input.Data.Favorites {
		if !media[record] {
			return invalidPortable("data.favorites 存在未解析的媒体引用。")
		}
	}
	for _, id := range input.Data.FollowedAuthorIDs {
		if !authors[id] {
			return invalidPortable("data.followedAuthorIds 存在未解析的作者引用。")
		}
	}
	likeKeys := map[string]bool{}
	for _, like := range input.Data.Likes {
		if !media[like.RecordKey] || like.LikeCount < 0 || likeKeys[like.RecordKey] {
			return invalidPortable("data.likes 存在重复引用或无效累计次数。")
		}
		likeKeys[like.RecordKey] = true
		if like.LastLikeDate != nil && *like.LastLikeDate != "" {
			if _, e := time.Parse("2006-01-02", *like.LastLikeDate); e != nil {
				return invalidPortable("data.likes 的最后点赞日无效。")
			}
		}
	}
	for _, mark := range input.Data.TimelineTags {
		if !media[mark.RecordKey] || mark.TimeMillis < 0 || strings.TrimSpace(mark.Name) == "" || len(mark.Name) > 200 {
			return invalidPortable("data.timelineTags 存在无效引用、时间或名称。")
		}
	}
	for _, fragment := range input.Data.TxtFragments {
		if fragment.Filename == "" || len(fragment.Filename) > 512 || strings.ContainsAny(fragment.Filename, "/\\:\x00") || len(fragment.Content) > 8<<20 {
			return invalidPortable("data.txtFragments 存在无效文件名或超过大小上限。")
		}
	}
	return nil
}

// ---------- 入口 ----------

func portableImportReceipt(ctx context.Context, q store.Querier, inputID, sourceDigest string) (PortableImportReport, bool, error) {
	var stored string
	e := q.QueryRowContext(ctx, "SELECT original FROM migration_relation_origins WHERE input_id=? AND kind=? AND identity=?",
		inputID, portableReceiptKind, sourceDigest).Scan(&stored)
	if errors.Is(e, sql.ErrNoRows) {
		return PortableImportReport{}, false, nil
	}
	if e != nil {
		return PortableImportReport{}, false, e
	}
	var report PortableImportReport
	if e = json.Unmarshal([]byte(stored), &report); e != nil {
		return report, false, Fail(409, "portable_import_receipt_invalid", "此文件的导入回执无法读取；未重复写入，请核对后再试。")
	}
	return report, true, nil
}

// PreviewPortableImport 只读：返回匹配与预计写入，不产生任何行。
func (s *Service) PreviewPortableImport(ctx context.Context, p model.Principal, raw []byte) (PortableImportReport, error) {
	var report PortableImportReport
	if e := Authorize(p, "admin", ""); e != nil {
		return report, e
	}
	input, e := decodePortableBackup(raw)
	if e != nil {
		return report, e
	}
	sourceDigest := digest(raw)
	inputID := portableSourceID(sourceDigest)
	if stored, found, e := portableImportReceipt(ctx, s.DB.DB, inputID, sourceDigest); e != nil {
		return report, e
	} else if found {
		stored.DryRun = true
		stored.AlreadyImported = true
		stored.Changed = false
		stored.Warnings = append(stored.Warnings, "此文件此前已导入，预览不会再写入任何数据。")
		return stored, nil
	}
	tx, e := s.DB.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return report, e
	}
	defer tx.Rollback()
	plan, e := s.buildPortablePlan(ctx, tx, input, sourceDigest)
	if e != nil {
		return report, e
	}
	plan.report.DryRun = true
	plan.report.AlreadyImported = false
	plan.report.Changed = plan.wouldChange()
	plan.report.Warnings = plan.warnings
	if len(plan.input.Data.MediaFiles) > 0 && plan.report.Media.Matched*100 < plan.report.Media.Total*95 {
		plan.report.Warnings = append(plan.report.Warnings,
			fmt.Sprintf("匹配率低于 95%%（%d/%d）。请核对库名与相对路径是否与目标实例完全一致；下方样本列出了旧侧库名、相对路径和目标候选。",
				plan.report.Media.Matched, plan.report.Media.Total))
	}
	plan.report.Warnings = append(plan.report.Warnings, "这是预览，不会写入任何数据。")
	return plan.report, tx.Commit()
}

// ApplyPortableImport 在调用方事务内合并。相同摘要的文件第二次调用直接返回
// 首次回执，不产生重复关联或修订。
func (s *Service) ApplyPortableImport(ctx context.Context, w *Write, raw []byte) (PortableImportReport, error) {
	var report PortableImportReport
	if e := Authorize(w.Principal, "admin", ""); e != nil {
		return report, e
	}
	input, e := decodePortableBackup(raw)
	if e != nil {
		return report, e
	}
	sourceDigest := digest(raw)
	inputID := portableSourceID(sourceDigest)
	q, e := w.Q()
	if e != nil {
		return report, e
	}
	if stored, found, e := portableImportReceipt(ctx, q, inputID, sourceDigest); e != nil {
		return report, e
	} else if found {
		stored.DryRun = false
		stored.AlreadyImported = true
		stored.Changed = false
		stored.Warnings = append(stored.Warnings, "此文件此前已导入，本次没有产生新的写入。")
		return stored, nil
	}
	// 先落回执占位：若后续任何一段失败，事务回滚不会留下“已导入”的假回执。
	if _, e = q.ExecContext(ctx, "INSERT INTO migration_relation_origins(input_id,kind,identity,original) VALUES(?,?,?,?)",
		inputID, portableReceiptKind, sourceDigest, "{}"); e != nil {
		return report, e
	}
	plan, e := s.buildPortablePlan(ctx, q, input, sourceDigest)
	if e != nil {
		return report, e
	}
	if e = s.applyPortablePlan(ctx, q, w, plan); e != nil {
		return report, e
	}
	if len(plan.input.Data.MediaFiles) > 0 && plan.report.Media.Matched*100 < plan.report.Media.Total*95 {
		plan.warn("匹配率低于 95%%（%d/%d）。请核对库名与相对路径是否与目标实例完全一致。",
			plan.report.Media.Matched, plan.report.Media.Total)
	}
	plan.report.DryRun = false
	plan.report.AlreadyImported = false
	plan.report.Changed = plan.didChange()
	plan.report.Warnings = plan.warnings
	body, e := store.Encode(plan.report)
	if e != nil {
		return report, e
	}
	if _, e = q.ExecContext(ctx, "UPDATE migration_relation_origins SET original=? WHERE input_id=? AND kind=? AND identity=?",
		string(body), inputID, portableReceiptKind, sourceDigest); e != nil {
		return report, e
	}
	w.libraryChanged()
	return plan.report, nil
}

// ---------- 计划构建 ----------

func (s *Service) buildPortablePlan(ctx context.Context, q store.Querier, input portableBackup, sourceDigest string) (*portableImportPlan, error) {
	now := time.Now().UTC()
	plan := &portableImportPlan{
		input:             input,
		sourceID:          portableLegacySource,
		sourceDigest:      sourceDigest,
		matches:           map[string]portableMatch{},
		mediaByKey:        map[string]portableMedia{},
		assetMedia:        map[string]portableMedia{},
		assets:            map[string]model.Asset{},
		authorBySource:    map[string]*portableAuthorPlan{},
		localAuthors:      map[string]model.Author{},
		tags:              map[string]*portableTagPlan{},
		tagExisting:       map[string]map[string]bool{},
		tagIncoming:       map[string][]string{},
		authorExisting:    map[string][]string{},
		authorIncoming:    map[string][]string{},
		authorAssetKinds:  map[string]map[string]bool{},
		authorLibraries:   map[string][]string{},
		authorDirectories: map[string][]string{},
		fragments:         input.Data.TxtFragments,
	}
	plan.report = PortableImportReport{
		DryRun:           true,
		SourceDigest:     sourceDigest,
		Format:           input.Format,
		SchemaVersion:    input.SchemaVersion,
		AppIdentifier:    input.AppIdentifier,
		ExportedAtMillis: input.ExportedAtMillis,
		Skipped:          []PortableSkippedSection{},
	}
	for _, media := range input.Data.MediaFiles {
		plan.mediaByKey[media.RecordKey] = media
	}
	matches, mediaReport, ambiguous, unmatched, duplicates, e := matchPortableMedia(ctx, q, input.Data.MediaFiles)
	if e != nil {
		return nil, e
	}
	plan.matches = matches
	plan.report.Media = mediaReport
	plan.report.Samples = PortableImportSamples{Ambiguous: ambiguous, Unmatched: unmatched, DuplicateTargets: duplicates}
	ids := make([]string, 0, len(matches))
	for _, match := range matches {
		ids = appendUnique(ids, match.Row.ID)
		plan.assetMedia[match.Row.ID] = match.Media
	}
	if plan.assets, e = loadPortableAssets(ctx, q, ids); e != nil {
		return nil, e
	}
	for _, ref := range input.Data.AuthorMediaRefs {
		match, ok := plan.matches[ref.RecordKey]
		if !ok {
			continue
		}
		if plan.authorAssetKinds[ref.AuthorID] == nil {
			plan.authorAssetKinds[ref.AuthorID] = map[string]bool{}
		}
		plan.authorAssetKinds[ref.AuthorID][match.Row.LibraryKind] = true
		plan.authorLibraries[ref.AuthorID] = appendUnique(plan.authorLibraries[ref.AuthorID], match.Row.LibraryID)
		if match.Row.LibraryKind == "cos" {
			if parts := strings.Split(strings.TrimPrefix(match.Row.RelativePath, "/"), "/"); len(parts) > 1 && parts[0] != "" {
				plan.authorDirectories[ref.AuthorID] = appendUnique(plan.authorDirectories[ref.AuthorID], parts[0])
			}
		}
	}
	if e = s.planPortableAuthors(ctx, q, plan, now); e != nil {
		return nil, e
	}
	if plan.report.Tags, e = planPortableTags(ctx, q, &plan.tags, input.Data.Tags, input.Data.MediaTagRefs); e != nil {
		return nil, e
	}
	if e = s.planPortableAuthorLinks(ctx, q, plan); e != nil {
		return nil, e
	}
	if e = s.planPortableTagsOnAssets(ctx, q, plan); e != nil {
		return nil, e
	}
	if e = s.planPortableFavorites(ctx, plan); e != nil {
		return nil, e
	}
	if e = s.planPortableLikes(ctx, q, plan); e != nil {
		return nil, e
	}
	if e = s.planPortableEvents(ctx, q, plan); e != nil {
		return nil, e
	}
	if e = s.planPortableTimeline(ctx, q, plan); e != nil {
		return nil, e
	}
	if e = s.planPortableFollows(ctx, plan); e != nil {
		return nil, e
	}
	if e = planPortableTxt(ctx, q, plan); e != nil {
		return nil, e
	}
	if e = planPortablePreferences(ctx, q, plan); e != nil {
		return nil, e
	}
	planPortableStaticSections(input, plan)
	plan.collectWarnings()
	return plan, nil
}

// ---------- 媒体匹配 ----------

// matchPortableMedia 固定匹配顺序：
//  1. 旧库名必须在目标实例唯一命中，然后 (libraryName, relPath) 精确匹配；
//  2. 目标库恰好带着同一 assetId（只接受同一库内的同 id，跨库不认领）；
//  3. 同一目标库内 fileName 唯一。
//
// 两个路径都按 / 归一、区分大小写、保留空格再比较。库名缺失、库名多义或同一
// 库内同名多行都跳过，绝不跨库按文件名猜测，也绝不按排序取最早行。
func matchPortableMedia(ctx context.Context, q store.Querier, files []portableMedia) (map[string]portableMatch, PortableMediaReport, []PortableMediaSample, []PortableMediaSample, []PortableMediaSample, error) {
	matches := map[string]portableMatch{}
	report := PortableMediaReport{Total: len(files)}
	var ambiguous, unmatched, duplicates []PortableMediaSample
	rows, e := q.QueryContext(ctx, `SELECT a.id,a.library_id,a.file_name,a.relative_path,l.name,l.kind
 FROM assets a JOIN libraries l ON l.id=a.library_id WHERE a.trashed=0`)
	if e != nil {
		return nil, report, nil, nil, nil, e
	}
	byID := map[string]portableAssetRow{}
	byLibraryPath := map[string][]portableAssetRow{}
	byLibraryName := map[string][]portableAssetRow{}
	for rows.Next() {
		var row portableAssetRow
		if e = rows.Scan(&row.ID, &row.LibraryID, &row.FileName, &row.RelativePath, &row.LibraryName, &row.LibraryKind); e != nil {
			rows.Close()
			return nil, report, nil, nil, nil, e
		}
		row.RelativePath = portablePath(row.RelativePath)
		byID[row.ID] = row
		byLibraryPath[row.LibraryID+"\x00"+row.RelativePath] = append(byLibraryPath[row.LibraryID+"\x00"+row.RelativePath], row)
		byLibraryName[row.LibraryID+"\x00"+row.FileName] = append(byLibraryName[row.LibraryID+"\x00"+row.FileName], row)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, report, nil, nil, nil, e
	}
	libraryRows, e := q.QueryContext(ctx, "SELECT id,name,kind FROM libraries")
	if e != nil {
		return nil, report, nil, nil, nil, e
	}
	libraryByName := map[string][]portableLibraryRow{}
	for libraryRows.Next() {
		var row portableLibraryRow
		if e = libraryRows.Scan(&row.ID, &row.Name, &row.Kind); e != nil {
			libraryRows.Close()
			return nil, report, nil, nil, nil, e
		}
		libraryByName[row.Name] = append(libraryByName[row.Name], row)
	}
	e = libraryRows.Err()
	libraryRows.Close()
	if e != nil {
		return nil, report, nil, nil, nil, e
	}
	misses := map[string]*PortableLibraryMiss{}
	miss := func(name, reason string) {
		label := name
		if label == "" {
			label = "（未提供库名）"
		}
		key := label + "\x00" + reason
		value := misses[key]
		if value == nil {
			value = &PortableLibraryMiss{LibraryName: label, Reason: reason}
			misses[key] = value
		}
		value.Count++
	}
	for _, file := range files {
		if file.LibraryName == "" {
			// 没有库名时不跨库按文件名猜测；稳定 assetId 仍可作为唯一认领证据。
			if file.AssetID != "" {
				if found, ok := byID[file.AssetID]; ok {
					matches[file.RecordKey] = portableMatch{Row: found, Key: "assetId", Media: file}
					continue
				}
			}
			report.Unmatched++
			miss("", "library_name_missing")
			appendPortableSample(&unmatched, portableSample(file, "library_name_missing", 0, nil))
			continue
		}
		libraries := libraryByName[file.LibraryName]
		switch len(libraries) {
		case 0:
			report.Unmatched++
			miss(file.LibraryName, "library_not_found")
			appendPortableSample(&unmatched, portableSample(file, "library_not_found", 0, nil))
			continue
		case 1:
		default:
			report.Ambiguous++
			miss(file.LibraryName, "library_name_ambiguous")
			appendPortableSample(&ambiguous, portableSample(file, "library_name_ambiguous", len(libraries), nil))
			continue
		}
		libraryID := libraries[0].ID
		if file.RelPath != "" {
			pathRows := byLibraryPath[libraryID+"\x00"+portablePath(file.RelPath)]
			if len(pathRows) == 1 {
				matches[file.RecordKey] = portableMatch{Row: pathRows[0], Key: "libraryPath", Media: file}
				continue
			}
			if len(pathRows) > 1 {
				report.Ambiguous++
				appendPortableSample(&ambiguous, portableSample(file, "multiple_paths_in_library", len(pathRows), portableRowLabels(pathRows)))
				continue
			}
		}
		if file.AssetID != "" {
			if found, ok := byID[file.AssetID]; ok && found.LibraryID == libraryID {
				matches[file.RecordKey] = portableMatch{Row: found, Key: "assetId", Media: file}
				continue
			}
		}
		candidates := byLibraryName[libraryID+"\x00"+file.FileName]
		switch len(candidates) {
		case 1:
			matches[file.RecordKey] = portableMatch{Row: candidates[0], Key: "fileName", Media: file}
		case 0:
			reason := "file_name_not_found"
			if file.RelPath != "" {
				reason = "path_and_name_not_found"
			}
			report.Unmatched++
			appendPortableSample(&unmatched, portableSample(file, reason, 0, nil))
		default:
			report.Ambiguous++
			appendPortableSample(&ambiguous, portableSample(file, "multiple_names_in_library", len(candidates), portableRowLabels(candidates)))
		}
	}
	// 一个目标被多个旧条目指向时全部跳过：宁可少写，也不串文件。
	targets := map[string]int{}
	for _, match := range matches {
		targets[match.Row.ID]++
	}
	for recordKey, match := range matches {
		if targets[match.Row.ID] > 1 {
			delete(matches, recordKey)
			report.DuplicateTargets++
			appendPortableSample(&duplicates, portableSample(match.Media, "multiple_sources_one_target", targets[match.Row.ID],
				[]string{match.Row.LibraryName + " / " + match.Row.RelativePath}))
		}
	}
	report.Matched = len(matches)
	for _, match := range matches {
		switch match.Key {
		case "libraryPath":
			report.MatchedByLibraryPath++
		case "assetId":
			report.MatchedByAssetID++
		case "fileName":
			report.MatchedByFileName++
		}
	}
	report.MissingLibraries = make([]PortableLibraryMiss, 0, len(misses))
	for _, value := range misses {
		report.MissingLibraries = append(report.MissingLibraries, *value)
	}
	sort.Slice(report.MissingLibraries, func(i, j int) bool {
		if report.MissingLibraries[i].LibraryName != report.MissingLibraries[j].LibraryName {
			return report.MissingLibraries[i].LibraryName < report.MissingLibraries[j].LibraryName
		}
		return report.MissingLibraries[i].Reason < report.MissingLibraries[j].Reason
	})
	return matches, report, ambiguous, unmatched, duplicates, nil
}

func loadPortableAssets(ctx context.Context, q store.Querier, ids []string) (map[string]model.Asset, error) {
	result := map[string]model.Asset{}
	if len(ids) == 0 {
		return result, nil
	}
	raw, e := json.Marshal(ids)
	if e != nil {
		return result, e
	}
	values, e := store.List[model.Asset](ctx, q, "SELECT data FROM assets WHERE id IN (SELECT value FROM json_each(?))", string(raw))
	if e != nil {
		return result, e
	}
	for _, value := range values {
		result[value.ID] = value
	}
	return result, nil
}

// ---------- 作者 ----------

func portableRawRegularAuthor(id, name string, now time.Time) model.Author {
	return model.Author{ID: id, Kind: "regular", Name: name, Aliases: []string{}, Revision: 1,
		FieldStatus: map[string]string{"identity": "confirmed", "biography": "missing", "homepage": "missing"},
		CreatedAt:   now, UpdatedAt: now}
}

func portableDirectoryAuthor(name, libraryID string, now time.Time) model.Author {
	return model.Author{ID: digest([]byte("cos-directory:" + libraryID + ":" + name))[:32], Kind: "cos",
		Name: name, Aliases: []string{}, Revision: 1,
		FieldStatus: map[string]string{"identity": "candidate", "biography": "missing", "homepage": "missing"},
		CreatedAt:   now, UpdatedAt: now}
}

// resolvePortableRegularAuthor 先按 TXT 身份键找，再按规范显示名精确找；两者
// 都不成立才新建。名称不能被 TXT 语法表示时仍保留原始显示名，避免丢掉旧作者。
func resolvePortableRegularAuthor(ctx context.Context, q store.Querier, source portableAuthor, now time.Time, staged map[string]model.Author) (model.Author, bool, string, error) {
	name := strings.TrimSpace(source.DisplayName)
	if name == "" || utf8.RuneCountInString(name) > 200 || strings.ContainsRune(name, '\x00') {
		return model.Author{}, false, "invalid_author_name", nil
	}
	names, nameErr := authorlist.Names(name)
	stageKey := "name:" + name
	if nameErr == nil {
		stageKey = authorlist.IdentityKey(names[0])
		if existing, ok := staged[stageKey]; ok {
			return existing, false, "", nil
		}
		found, ok, e := lookupRegularAuthor(ctx, q, stageKey)
		if e != nil {
			if portableErrorCode(e) == "ambiguous_author" {
				return model.Author{}, false, "ambiguous_author", nil
			}
			return model.Author{}, false, "", e
		}
		if ok {
			if found.Placeholder {
				return model.Author{}, false, "placeholder", nil
			}
			staged[stageKey] = found
			return found, false, "", nil
		}
	}
	if existing, ok := staged[stageKey]; ok {
		return existing, false, "", nil
	}
	// 旧显示名可能带 (备注) 或别名分隔；新版保存的是规范名。两种写法都查，
	// 避免同一作者被重复创建。
	matchNames := []string{name}
	if nameErr == nil {
		if normalized := strings.Join(names, " / "); normalized != name {
			matchNames = append(matchNames, normalized)
		}
	}
	placeholders := make([]string, len(matchNames))
	args := make([]any, len(matchNames))
	for index, value := range matchNames {
		placeholders[index] = "?"
		args[index] = value
	}
	matches, e := store.List[model.Author](ctx, q, "SELECT data FROM authors WHERE name IN ("+strings.Join(placeholders, ",")+") AND json_extract(data,'$.kind')='regular' ORDER BY id LIMIT 3", args...)
	if e != nil {
		return model.Author{}, false, "", e
	}
	switch len(matches) {
	case 1:
		staged[stageKey] = matches[0]
		return matches[0], false, "", nil
	case 0:
		id := digest([]byte("portable-v2-author:" + source.AuthorID))[:32]
		var created model.Author
		if nameErr == nil {
			created = newTextAuthor(id, names, portableTime(source.CreatedAtMillis, now))
		} else {
			created = portableRawRegularAuthor(id, name, portableTime(source.CreatedAtMillis, now))
		}
		staged[stageKey] = created
		return created, true, "", nil
	default:
		return model.Author{}, false, "ambiguous_author", nil
	}
}

func (s *Service) planPortableAuthors(ctx context.Context, q store.Querier, plan *portableImportPlan, now time.Time) error {
	report := &plan.report.Authors
	report.Total = len(plan.input.Data.Authors)
	unknown, e := store.Get[model.Author](ctx, q, "SELECT data FROM authors WHERE id='unknown'")
	if e != nil {
		return e
	}
	staged := map[string]model.Author{}
	for _, source := range plan.input.Data.Authors {
		author := &portableAuthorPlan{Source: source}
		name := strings.TrimSpace(source.DisplayName)
		kind := v2AuthorKind(source.legacy())
		if source.AuthorID == "unknown" || name == unknown.Name {
			author.Local = unknown
			author.Placeholder = true
			report.Placeholder++
			plan.authors = append(plan.authors, author)
			plan.authorBySource[source.AuthorID] = author
			continue
		}
		resolved := false
		if kind == "cos" {
			// 目标库是 COS 时优先目录身份；目标是普通库或没有库信息时退回常规作者，
			// 否则会丢掉旧 COS 目录作者在普通媒体库里的关联。
			preferCos := len(plan.authorAssetKinds[source.AuthorID]) > 0
			for libraryKind := range plan.authorAssetKinds[source.AuthorID] {
				if libraryKind != "cos" {
					preferCos = false
				}
			}
			if preferCos {
				// 新版的 COS 目录作者以库内第一段目录名为身份；旧作者显示名可能
				// 大小写不同，因此优先用命中媒体的目录段查找/创建。
				directories := plan.authorDirectories[source.AuthorID]
				cosName := name
				if len(directories) == 1 {
					cosName = directories[0]
				}
				matches, e := store.List[model.Author](ctx, q, "SELECT data FROM authors WHERE name=? AND json_extract(data,'$.kind')='cos' ORDER BY id LIMIT 2", cosName)
				if e != nil {
					return e
				}
				switch len(matches) {
				case 1:
					author.Local = matches[0]
					report.Merged++
					resolved = true
				case 0:
					if libraries := plan.authorLibraries[source.AuthorID]; len(libraries) == 1 && len(directories) == 1 {
						author.Local = portableDirectoryAuthor(cosName, libraries[0], portableTime(source.CreatedAtMillis, now))
						author.Create = true
						report.Created++
						resolved = true
					}
				default:
					author.SkipReason = "ambiguous_author"
					report.Ambiguous++
					resolved = true
				}
			}
		}
		if !resolved {
			local, created, reason, e := resolvePortableRegularAuthor(ctx, q, source, now, staged)
			if e != nil {
				return e
			}
			if reason == "placeholder" {
				author.Local = unknown
				author.Placeholder = true
				report.Placeholder++
			} else if reason != "" {
				author.SkipReason = reason
				if reason == "ambiguous_author" {
					report.Ambiguous++
				} else {
					report.Skipped++
				}
			} else {
				author.Local = local
				author.Create = created
				if created {
					report.Created++
				} else {
					report.Merged++
				}
			}
		}
		plan.authors = append(plan.authors, author)
		plan.authorBySource[source.AuthorID] = author
		if author.SkipReason != "" || author.Placeholder {
			continue
		}
		plan.localAuthors[author.Local.ID] = author.Local
		if source.ProfileJSON == "" {
			continue
		}
		avatar := []byte{}
		if source.AvatarData != "" {
			decoded, decodeErr := base64.StdEncoding.DecodeString(source.AvatarData)
			if decodeErr != nil {
				plan.warn("作者 %s 的离线头像不是有效 Base64，资料按无头像迁入。", name)
			} else {
				avatar = decoded
			}
		}
		prepared, prepareErr := PrepareImportedAuthorProfile(ctx, source.AuthorID, json.RawMessage(source.ProfileJSON), avatar)
		if prepareErr != nil && len(avatar) > 0 {
			// 单个头像超限或损坏不应丢掉整份作者资料；去掉头像再试一次。
			avatarErr := prepareErr
			prepared, prepareErr = PrepareImportedAuthorProfile(ctx, source.AuthorID, json.RawMessage(source.ProfileJSON), nil)
			if prepareErr == nil {
				plan.warn("作者 %s 的头像无法迁入（%s），资料已按无头像导入。", name, portableErrorCode(avatarErr))
			}
		}
		if prepareErr != nil {
			report.ProfilesSkipped++
			plan.warn("作者 %s 的资料无法迁入（%s），作者与文件关联仍保留。", name, portableErrorCode(prepareErr))
			continue
		}
		report.ProfilesPrepared++
		if prepared.avatar != nil {
			report.AvatarsPrepared++
		}
		archiveID := digest([]byte("portable-v2-profile:" + source.AuthorID + "\x00" + digest(prepared.original)))[:32]
		plan.profiles = append(plan.profiles, portableProfilePlan{
			SourceAuthorID: source.AuthorID, LocalAuthorID: author.Local.ID,
			AuthorName: author.Local.Name, ArchiveID: archiveID, Prepared: prepared,
		})
	}
	return nil
}

// ---------- 标签 ----------

func planPortableTags(ctx context.Context, q store.Querier, result *map[string]*portableTagPlan, vocabulary []portableTag, refs []portableTagRef) (PortableTagReport, error) {
	report := PortableTagReport{}
	names := map[string]bool{}
	for _, tag := range vocabulary {
		names[strings.TrimSpace(tag.Name)] = true
	}
	for _, ref := range refs {
		names[strings.TrimSpace(ref.TagName)] = true
	}
	for name := range names {
		if name == "" {
			continue
		}
		plan := &portableTagPlan{Name: name}
		var id string
		var deleted int
		e := q.QueryRowContext(ctx, "SELECT id,deleted FROM tags WHERE name=? ORDER BY deleted LIMIT 1", name).Scan(&id, &deleted)
		switch {
		case e == nil && deleted == 0:
			plan.ID = id
			report.VocabularyExisting++
		case e == nil:
			plan.Unavailable = true
			report.VocabularyUnavailable++
		case errors.Is(e, sql.ErrNoRows):
			var retired bool
			if e = q.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM retired_tag_names WHERE name=?)", name).Scan(&retired); e != nil {
				return report, e
			}
			if retired {
				plan.Unavailable = true
				report.VocabularyUnavailable++
			} else {
				plan.Create = true
				report.VocabularyCreated++
			}
		default:
			return report, e
		}
		(*result)[name] = plan
		report.VocabularyTotal++
	}
	return report, nil
}

func loadPortableAssetAuthors(ctx context.Context, q store.Querier, ids []string) (map[string][]string, error) {
	result := map[string][]string{}
	if len(ids) == 0 {
		return result, nil
	}
	raw, e := json.Marshal(ids)
	if e != nil {
		return result, e
	}
	rows, e := q.QueryContext(ctx, `SELECT asset_id,author_id FROM asset_authors
 WHERE asset_id IN (SELECT value FROM json_each(?)) ORDER BY asset_id,ordinal,author_id`, string(raw))
	if e != nil {
		return result, e
	}
	defer rows.Close()
	for rows.Next() {
		var asset, author string
		if e = rows.Scan(&asset, &author); e != nil {
			return result, e
		}
		result[asset] = appendUnique(result[asset], author)
	}
	return result, rows.Err()
}

func loadPortableAssetTags(ctx context.Context, q store.Querier, ids []string) (map[string][]string, error) {
	result := map[string][]string{}
	if len(ids) == 0 {
		return result, nil
	}
	raw, e := json.Marshal(ids)
	if e != nil {
		return result, e
	}
	rows, e := q.QueryContext(ctx, `SELECT asset_id,tag FROM asset_tag_names
 WHERE asset_id IN (SELECT value FROM json_each(?)) ORDER BY asset_id,ordinal,tag`, string(raw))
	if e != nil {
		return result, e
	}
	defer rows.Close()
	for rows.Next() {
		var asset, tag string
		if e = rows.Scan(&asset, &tag); e != nil {
			return result, e
		}
		result[asset] = appendUnique(result[asset], tag)
	}
	return result, rows.Err()
}

func (s *Service) planPortableAuthorLinks(ctx context.Context, q store.Querier, plan *portableImportPlan) error {
	ids := make([]string, 0, len(plan.assets))
	for id := range plan.assets {
		ids = append(ids, id)
	}
	existing, e := loadPortableAssetAuthors(ctx, q, ids)
	if e != nil {
		return e
	}
	plan.authorExisting = existing
	report := &plan.report.AssetAuthors
	report.Total = len(plan.input.Data.AuthorMediaRefs)
	changedAssets := map[string]bool{}
	for _, ref := range plan.input.Data.AuthorMediaRefs {
		match, ok := plan.matches[ref.RecordKey]
		if !ok {
			report.SkippedUnmatched++
			continue
		}
		author, ok := plan.authorBySource[ref.AuthorID]
		switch {
		case !ok:
			report.SkippedAuthor++
			continue
		case author.Placeholder:
			report.SkippedPlaceholder++
			continue
		case author.SkipReason != "":
			report.SkippedAuthor++
			continue
		case author.Local.Kind == "cos" && match.Row.LibraryKind != "cos":
			// COS 目录身份只能挂在 COS 库；普通库用上文的常规作者回退。
			report.SkippedCos++
			continue
		}
		if containsString(existing[match.Row.ID], author.Local.ID) || containsString(plan.authorIncoming[match.Row.ID], author.Local.ID) {
			report.LinksPresent++
			continue
		}
		plan.authorIncoming[match.Row.ID] = append(plan.authorIncoming[match.Row.ID], author.Local.ID)
		plan.linkPlans = append(plan.linkPlans, portableAuthorWork{AssetID: match.Row.ID, AuthorID: author.Local.ID, FileName: match.Row.FileName})
		report.LinksAdded++
		changedAssets[match.Row.ID] = true
	}
	report.AssetsChanged = len(changedAssets)
	return nil
}

func (s *Service) planPortableTagsOnAssets(ctx context.Context, q store.Querier, plan *portableImportPlan) error {
	ids := make([]string, 0, len(plan.assets))
	for id := range plan.assets {
		ids = append(ids, id)
	}
	existing, e := loadPortableAssetTags(ctx, q, ids)
	if e != nil {
		return e
	}
	plan.tagExisting = map[string]map[string]bool{}
	for asset, names := range existing {
		set := map[string]bool{}
		for _, name := range names {
			set[name] = true
		}
		plan.tagExisting[asset] = set
	}
	report := &plan.report.Tags
	changedAssets := map[string]bool{}
	for _, ref := range plan.input.Data.MediaTagRefs {
		match, ok := plan.matches[ref.RecordKey]
		if !ok {
			report.LinksSkipped++
			continue
		}
		name := strings.TrimSpace(ref.TagName)
		tag := plan.tags[name]
		if tag == nil || tag.Unavailable {
			report.LinksSkipped++
			continue
		}
		if plan.tagExisting[match.Row.ID][name] || containsString(plan.tagIncoming[match.Row.ID], name) {
			report.LinksPresent++
			continue
		}
		plan.tagIncoming[match.Row.ID] = append(plan.tagIncoming[match.Row.ID], name)
		report.LinksAdded++
		changedAssets[match.Row.ID] = true
	}
	report.AssetsChanged = len(changedAssets)
	return nil
}

// ---------- 收藏 / 点赞 ----------

func (s *Service) planPortableFavorites(ctx context.Context, plan *portableImportPlan) error {
	report := &plan.report.Favorites
	report.Total = len(plan.input.Data.Favorites)
	seen := map[string]bool{}
	for _, recordKey := range plan.input.Data.Favorites {
		match, ok := plan.matches[recordKey]
		if !ok {
			report.SkippedUnmatched++
			continue
		}
		if seen[recordKey] {
			report.Already++
			continue
		}
		seen[recordKey] = true
		asset := plan.assets[match.Row.ID]
		switch {
		case asset.Favorite:
			report.Already++
		case asset.FavoriteUpdatedAt != nil:
			// 本机有过明确收藏决定且当前为未收藏，保留本机选择。
			report.SkippedLocal++
		default:
			report.Applied++
			plan.favoritePlans = append(plan.favoritePlans, portableFavoritePlan{RecordKey: recordKey, AssetID: match.Row.ID})
		}
	}
	return nil
}

func (s *Service) planPortableLikes(ctx context.Context, q store.Querier, plan *portableImportPlan) error {
	report := &plan.report.Likes
	report.Total = len(plan.input.Data.Likes)
	for _, like := range plan.input.Data.Likes {
		match, ok := plan.matches[like.RecordKey]
		if !ok {
			report.SkippedUnmatched++
			continue
		}
		if like.LikeCount == 0 {
			report.SkippedNoLikes++
			continue
		}
		media := plan.mediaByKey[like.RecordKey]
		action, _, e := decideLegacyLikes(ctx, q, match.Row.ID, plan.sourceID, likeSourceKey(media.legacy()), like.legacy())
		if e != nil {
			return e
		}
		switch action {
		case "apply":
			report.Applied++
		case "keep":
			report.Already++
		default:
			report.SkippedConflict++
		}
		plan.likePlans = append(plan.likePlans, portableLikePlan{RecordKey: like.RecordKey, AssetID: match.Row.ID, Action: action, Like: like, Media: media})
	}
	return nil
}

// ---------- 事件 ----------

func (s *Service) planPortableEvents(ctx context.Context, q store.Querier, plan *portableImportPlan) error {
	report := &plan.report.Events
	report.Total = len(plan.input.Data.ViewEvents)
	ids := []string{}
	accepted := []portableEvent{}
	for _, event := range plan.input.Data.ViewEvents {
		if event.EventID == "" || len(event.EventID) > 128 || len(event.SessionID) > 128 {
			report.SkippedInvalid++
			continue
		}
		if _, ok := plan.matches[event.RecordKey]; !ok {
			report.SkippedUnmatched++
			continue
		}
		accepted = append(accepted, event)
		ids = append(ids, event.EventID)
	}
	existing := map[string]model.Event{}
	if len(ids) > 0 {
		raw, e := json.Marshal(ids)
		if e != nil {
			return e
		}
		values, e := store.List[model.Event](ctx, q, "SELECT data FROM events WHERE event_id IN (SELECT value FROM json_each(?))", string(raw))
		if e != nil {
			return e
		}
		for _, value := range values {
			existing[value.EventID] = value
		}
	}
	for _, event := range accepted {
		assetID := plan.matches[event.RecordKey].Row.ID
		if old, ok := existing[event.EventID]; ok {
			if portableEventEqual(old, assetID, event) {
				report.Already++
			} else {
				report.SkippedConflict++
			}
			continue
		}
		report.Imported++
		switch event.Kind {
		case "open":
			report.Views++
		case "play":
			report.Plays++
		case "dwell":
			if event.Seconds != nil {
				report.DwellSeconds += *event.Seconds
			}
		}
		plan.eventPlans = append(plan.eventPlans, portableEventPlan{Event: event, AssetID: assetID, Action: "imported"})
	}
	return nil
}

func portableEventEqual(old model.Event, assetID string, event portableEvent) bool {
	occurred, e := time.Parse(time.RFC3339Nano, event.StartedAt)
	if e != nil {
		return false
	}
	kind := event.Kind
	if kind == "open" {
		kind = "view"
	}
	seconds := float64(0)
	if event.Seconds != nil {
		seconds = float64(*event.Seconds)
	}
	return old.AssetID == assetID && old.Kind == kind && old.SessionID == event.SessionID &&
		old.Seconds == seconds && old.ContentVersion == 0 && old.OccurredAt != nil && old.OccurredAt.Equal(occurred)
}

// ---------- 时间轴 / 关注 / TXT / 偏好 ----------

func (s *Service) planPortableTimeline(ctx context.Context, q store.Querier, plan *portableImportPlan) error {
	report := &plan.report.Timeline
	report.Total = len(plan.input.Data.TimelineTags)
	ids := []string{}
	planned := map[string]bool{}
	for _, mark := range plan.input.Data.TimelineTags {
		match, ok := plan.matches[mark.RecordKey]
		if !ok {
			report.SkippedUnmatched++
			continue
		}
		asset := plan.assets[match.Row.ID]
		if asset.MediaType != "video" {
			report.SkippedNotVideo++
			continue
		}
		// 新扫描的原件是 1；只有被替换过的原件（>1）才不映射旧时间点标记。
		if asset.ContentVersion > 1 {
			report.SkippedContentChanged++
			continue
		}
		identity, _ := json.Marshal(struct {
			Source, Asset string
			Position      int64
			Name, Color   string
		}{plan.sourceID, asset.ID, mark.TimeMillis, mark.Name, mark.Color})
		id := digest(identity)[:32]
		if planned[id] {
			report.Already++
			continue
		}
		planned[id] = true
		ids = append(ids, id)
		plan.timelinePlans = append(plan.timelinePlans, portableTimelinePlan{Mark: mark, AssetID: asset.ID, Action: "imported", ID: id})
	}
	if len(ids) > 0 {
		raw, e := json.Marshal(ids)
		if e != nil {
			return e
		}
		rows, e := q.QueryContext(ctx, "SELECT id FROM timeline_marks WHERE id IN (SELECT value FROM json_each(?))", string(raw))
		if e != nil {
			return e
		}
		existing := map[string]bool{}
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				rows.Close()
				return e
			}
			existing[id] = true
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		for index := range plan.timelinePlans {
			if existing[plan.timelinePlans[index].ID] {
				plan.timelinePlans[index].Action = "already"
			}
		}
	}
	for _, item := range plan.timelinePlans {
		if item.Action == "imported" {
			report.Imported++
		} else {
			report.Already++
		}
	}
	return nil
}

func (s *Service) planPortableFollows(ctx context.Context, plan *portableImportPlan) error {
	report := &plan.report.Follows
	report.Total = len(plan.input.Data.FollowedAuthorIDs)
	seen := map[string]bool{}
	for _, sourceID := range plan.input.Data.FollowedAuthorIDs {
		author, ok := plan.authorBySource[sourceID]
		if !ok || author.Placeholder || author.SkipReason != "" {
			report.SkippedUnknown++
			continue
		}
		if seen[author.Local.ID] {
			report.Already++
			continue
		}
		seen[author.Local.ID] = true
		switch {
		case author.Local.Following:
			report.Already++
		case author.Local.FollowingUpdatedAt != nil:
			report.SkippedLocal++
		default:
			report.Marked++
			plan.followPlans = append(plan.followPlans, portableFollowPlan{SourceAuthorID: sourceID, AuthorID: author.Local.ID, Action: "marked"})
		}
	}
	return nil
}

func planPortableTxt(ctx context.Context, q store.Querier, plan *portableImportPlan) error {
	report := &plan.report.Txt
	report.Fragments = len(plan.input.Data.TxtFragments)
	ids := []string{}
	planned := map[string]bool{}
	for _, fragment := range plan.input.Data.TxtFragments {
		hash := digest([]byte(fragment.Content))
		id := digest([]byte(plan.sourceID + "\x00" + fragment.Filename + "\x00" + hash))[:32]
		if planned[id] {
			report.FragmentsExisting++
			continue
		}
		planned[id] = true
		ids = append(ids, id)
	}
	if len(ids) > 0 {
		raw, e := json.Marshal(ids)
		if e != nil {
			return e
		}
		rows, e := q.QueryContext(ctx, "SELECT id FROM author_txt_fragments WHERE id IN (SELECT value FROM json_each(?))", string(raw))
		if e != nil {
			return e
		}
		existing := map[string]bool{}
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				rows.Close()
				return e
			}
			existing[id] = true
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		for id := range planned {
			if existing[id] {
				report.FragmentsExisting++
			} else {
				report.FragmentsImported++
			}
		}
	}
	return nil
}

func planPortablePreferences(ctx context.Context, q store.Querier, plan *portableImportPlan) error {
	weights, e := importedWeights(plan.input.Data.AppPrefs)
	if e != nil {
		return e
	}
	if weights == nil {
		return nil
	}
	plan.weights = weights
	plan.report.Preferences.Present = true
	var existing string
	e = q.QueryRowContext(ctx, "SELECT value FROM settings WHERE key='recommendation_preferences'").Scan(&existing)
	switch {
	case errors.Is(e, sql.ErrNoRows):
		plan.report.Preferences.Imported = true
	case e != nil:
		return e
	default:
		plan.report.Preferences.Kept = true
	}
	return nil
}

func planPortableStaticSections(input portableBackup, plan *portableImportPlan) {
	add := func(section string, count int, reason string) {
		if count > 0 {
			plan.report.Skipped = append(plan.report.Skipped, PortableSkippedSection{Section: section, Count: count, Reason: reason})
		}
	}
	add("mediaStats", len(input.Data.MediaStats), "aggregate_derived_from_events")
	add("dailyBrowse", len(input.Data.DailyBrowse), "aggregate_derived_from_events")
	add("history", len(input.Data.History), "derived_from_view_events")
	if input.Data.AuthorMasterTxt != nil && strings.TrimSpace(*input.Data.AuthorMasterTxt) != "" {
		add("authorMasterTxt", 1, "master_txt_has_no_target")
	}
	add("settings", len(input.Data.Settings), "runtime_settings_not_activated")
	add("scanSources", len(input.Data.ScanSources), "scan_roots_not_registered")
	add("albumRules", len(input.Data.AlbumRules), "no_equivalent_capability")
	add("cosWorks", len(input.Data.CosWorks), "derived_from_directories")
}

func (p *portableImportPlan) collectWarnings() {
	r := p.report
	if r.Media.Unmatched > 0 {
		p.warn("%d 条旧记录未匹配到当前媒体库，相关作者、标签、收藏、点赞和事件已跳过。", r.Media.Unmatched)
	}
	if r.Media.Ambiguous > 0 {
		p.warn("%d 条旧记录存在多个候选或库名多义，未按顺序猜测，相关记录已跳过。", r.Media.Ambiguous)
	}
	if r.Media.DuplicateTargets > 0 {
		p.warn("%d 条旧记录指向同一个新版媒体，已全部跳过以免串文件。", r.Media.DuplicateTargets)
	}
	if len(r.Media.MissingLibraries) > 0 {
		p.warn("目标实例没有同名库或多义库；请在媒体库页核对库名，未命中的旧库名已列在报告中。")
	}
	if r.Authors.Ambiguous > 0 {
		p.warn("%d 位旧作者对应多个新版同名身份，已跳过；请在作者页核对后手动合并。", r.Authors.Ambiguous)
	}
	if r.Authors.ProfilesSkipped > 0 {
		p.warn("%d 位作者的资料无法迁入（多为超长或缺少来源），作者与文件关联仍保留。", r.Authors.ProfilesSkipped)
	}
	if r.AssetAuthors.SkippedCos > 0 {
		p.warn("%d 条 COS 目录作者关联落在非 COS 库，已跳过。", r.AssetAuthors.SkippedCos)
	}
	if r.Favorites.SkippedLocal > 0 {
		p.warn("%d 个旧收藏与新版明确取消收藏的状态冲突，已保留新版选择。", r.Favorites.SkippedLocal)
	}
	if r.Likes.SkippedConflict > 0 {
		p.warn("%d 条旧累计点赞与新版现有投票边界重叠，已保留新版并跳过。", r.Likes.SkippedConflict)
	}
	if r.Events.SkippedConflict > 0 {
		p.warn("%d 条旧事件身份与新版现有事件内容不一致，已跳过。", r.Events.SkippedConflict)
	}
	if r.Follows.SkippedLocal > 0 {
		p.warn("%d 个旧关注与新版明确取消关注的状态冲突，已保留新版选择。", r.Follows.SkippedLocal)
	}
	if len(r.Skipped) > 0 {
		p.warn("旧版统计汇总、按天浏览和浏览历史不单独导入；新版浏览统计由 viewEvents 事件回放重建。")
	}
	for _, section := range r.Skipped {
		switch section.Section {
		case "authorMasterTxt":
			p.warn("作者主表 TXT 是派生汇总，未导入；原始 TXT 片段已按作者清单归档。")
		case "settings":
			p.warn("旧版运行时设置不导入，请在新版设置中重新核对。")
		case "scanSources":
			p.warn("旧版扫描目录不导入；请核对当前已扫描媒体库的根目录。")
		case "albumRules":
			p.warn("旧版相册规则在新版没有对应能力，已忽略。")
		case "cosWorks":
			p.warn("COS 作品目录由目录扫描派生，未逐条导入。")
		}
	}
	if r.Txt.FragmentsImported > 0 {
		p.warn("%d 个 TXT 片段将归档，并在作者身份可解析时激活为作者清单。", r.Txt.FragmentsImported)
	}
	if r.Preferences.Kept {
		p.warn("新版已有推荐偏好，旧偏好未覆盖。")
	}
}

func (p *portableImportPlan) wouldChange() bool {
	r := p.report
	return r.Authors.Created > 0 || r.Authors.ProfilesPrepared > 0 ||
		r.Tags.VocabularyCreated > 0 || r.Tags.LinksAdded > 0 ||
		r.AssetAuthors.LinksAdded > 0 || r.Favorites.Applied > 0 || r.Likes.Applied > 0 ||
		r.Events.Imported > 0 || r.Timeline.Imported > 0 || r.Follows.Marked > 0 ||
		r.Txt.FragmentsImported > 0 || r.Preferences.Imported
}

func (p *portableImportPlan) didChange() bool {
	r := p.report
	return r.Authors.Created > 0 || r.Authors.ProfileBlocksRestored > 0 || r.Authors.ProfileCandidatesCreated > 0 || r.Authors.AvatarsRestored > 0 ||
		r.Tags.VocabularyCreated > 0 || r.Tags.LinksAdded > 0 ||
		r.AssetAuthors.LinksAdded > 0 || r.Favorites.Applied > 0 || r.Likes.Applied > 0 ||
		r.Events.Imported > 0 || r.Timeline.Imported > 0 || r.Follows.Marked > 0 ||
		r.Txt.FragmentsImported > 0 || r.Preferences.Imported
}
