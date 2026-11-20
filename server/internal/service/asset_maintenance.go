package service

import (
	"context"
	"path"
	"strings"
	"time"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/store"
)

var maintenanceFields = []string{"authors", "taxonomy", "tags", "rename"}

type AssetMaintenanceTask struct {
	ID               string     `json:"id"`
	State            string     `json:"state,omitempty"`
	AssetID          string     `json:"assetId"`
	LibraryID        string     `json:"libraryId"`
	ContentVersion   int64      `json:"contentVersion"`
	Hash             string     `json:"hash"`
	ExpectedRevision int64      `json:"expectedRevision"`
	AssetFields      []string   `json:"assetFields"`
	ManageFiles      bool       `json:"manageFiles,omitempty"`
	ReplaceExisting  bool       `json:"replaceExisting,omitempty"`
	Research         bool       `json:"research"`
	UsedUpdates      int        `json:"usedUpdates"`
	MaxUpdates       int        `json:"maxUpdates"`
	CreatedAt        *time.Time `json:"createdAt,omitempty"`
	ExpiresAt        time.Time  `json:"expiresAt"`
}

func validateAssetPolicy(d *DelegationPolicy) error {
	if d.AssetFields != nil {
		fields, err := cleanStrings(d.AssetFields, 4, 32)
		if err != nil {
			return err
		}
		if !withinStrings(fields, maintenanceFields) {
			return Fail(400, "unknown_asset_field", "作品维护权限包含未知字段。")
		}
		d.AssetFields = fields
	}
	if d.CanResearchAssets && !d.CanResearch {
		return Fail(400, "research_required", "已有作品研究需要同时开启研究权限。")
	}
	if d.CanReplaceAssetMetadata && !containsString(d.AssetFields, "tags") {
		return Fail(400, "asset_field_required", "替换已有作品标签须同时选择标签字段。")
	}
	if (d.CanResearchAssets || len(d.AssetFields) > 0 || d.CanManageAssetFiles) && len(d.LibraryIDs) == 0 {
		return Fail(400, "library_required", "已有作品维护须明确选择可操作的库。")
	}
	return nil
}
func (s *Service) prepareAssetMaintenance(ctx context.Context, id string, d Delegation, in DelegatedTaskStart) (AssetMaintenanceTask, error) {
	var t AssetMaintenanceTask
	a, err := s.Asset(ctx, in.AssetID)
	if err != nil {
		return t, err
	}
	if (in.LibraryID != "" && in.LibraryID != a.LibraryID) || !containsString(d.LibraryIDs, a.LibraryID) {
		return t, Fail(403, "library_scope_denied", "当前授权未包含作品所在库。")
	}
	if in.ManageFiles && !d.CanManageAssetFiles {
		return t, Fail(403, "asset_files_denied", "首次配置未授权回收站与移动。")
	}
	allowed := []string{}
	if in.AssetFields != nil {
		allowed, err = cleanStrings(in.AssetFields, 4, 32)
		if err != nil {
			return t, err
		}
	}
	if !withinStrings(allowed, d.AssetFields) || (len(allowed) == 0 && !d.CanResearchAssets && !in.ManageFiles) {
		return t, Fail(403, "asset_scope_denied", "此作品任务超出了首次配置的权限。")
	}
	lib, err := s.library(ctx, a.LibraryID)
	if err != nil {
		return t, err
	}
	// A restore task is created for an asset that is already in the trash. Other
	// tasks still refuse trashed media, and metadata writes re-check that state.
	// File-only tasks follow the app's trash/restore behaviour for a disabled library.
	if (!lib.Enabled && !in.ManageFiles) || (a.Trashed && !in.ManageFiles) {
		return t, Fail(409, "asset_unavailable", "库已停用或作品已进入回收站。")
	}
	if a.Revision != in.ExpectedRevision || a.ContentVersion != in.ExpectedContentVersion {
		return t, Conflict(a.Revision)
	}
	updates := in.MaxUpdates
	if updates == 0 {
		updates = d.MaxUpdates
	}
	if updates < 1 || updates > d.MaxUpdates {
		return t, Fail(403, "update_budget_denied", "作品修改次数超出授权。")
	}
	now := time.Now().UTC()
	return AssetMaintenanceTask{ID: id, State: "authorized", AssetID: a.ID, LibraryID: a.LibraryID, ContentVersion: a.ContentVersion, Hash: a.Hash, ExpectedRevision: a.Revision, AssetFields: allowed, ManageFiles: in.ManageFiles, ReplaceExisting: d.CanReplaceAssetMetadata, Research: d.CanResearchAssets, MaxUpdates: updates, CreatedAt: &now, ExpiresAt: limitExpiry(d, time.Duration(d.TaskSeconds)*time.Second)}, nil
}
func (s *Service) assetMaintenance(ctx context.Context, id string) (AssetMaintenanceTask, error) {
	t, err := store.Get[AssetMaintenanceTask](ctx, s.DB.DB, "SELECT data FROM asset_maintenance_tasks WHERE id=?", id)
	return t, Normalize(err)
}
func saveAssetMaintenance(ctx context.Context, q store.Querier, t AssetMaintenanceTask) error {
	raw, err := store.Encode(t)
	if err != nil {
		return err
	}
	_, err = q.ExecContext(ctx, "INSERT INTO asset_maintenance_tasks(id,asset_id,data) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET data=excluded.data", t.ID, t.AssetID, raw)
	return err
}
func (s *Service) AssetTaskContext(ctx context.Context, p model.Principal, id string) (AssetMaintenanceTask, error) {
	if !p.Has("admin") && (p.AssetTaskID != id || id == "") {
		return AssetMaintenanceTask{}, Fail(403, "asset_task_scope_denied", "当前凭据未绑定这个作品任务。")
	}
	return s.assetMaintenance(ctx, id)
}
func (s *Service) activeAssetTask(ctx context.Context, p model.Principal, id string) (AssetMaintenanceTask, model.Asset, error) {
	return s.activeAssetTaskState(ctx, p, id, false, false)
}
func (s *Service) activeAssetTaskState(ctx context.Context, p model.Principal, id string, allowTrashed, allowDisabledLibrary bool) (AssetMaintenanceTask, model.Asset, error) {
	var a model.Asset
	t, err := s.AssetTaskContext(ctx, p, id)
	if err != nil {
		return t, a, err
	}
	if t.State == "revoked" {
		return t, a, Fail(409, "asset_task_revoked", "此作品任务的授权已撤销，历史记录和研究依据仍可查看。")
	}
	if !t.ExpiresAt.After(time.Now()) {
		return t, a, Fail(409, "asset_task_expired", "作品维护任务已到期。")
	}
	a, err = s.Asset(ctx, t.AssetID)
	if err != nil {
		return t, a, err
	}
	lib, err := s.library(ctx, a.LibraryID)
	if err != nil {
		return t, a, err
	}
	if (!lib.Enabled && !allowDisabledLibrary) || (a.Trashed && !allowTrashed) || a.LibraryID != t.LibraryID || a.ContentVersion != t.ContentVersion || a.Hash != t.Hash {
		return t, a, Fail(409, "asset_version_changed", "作品或其来源范围已变化，请重新核对。")
	}
	return t, a, nil
}
func protectHumanField(ctx context.Context, q store.Querier, id, field string) error {
	_, err := q.ExecContext(ctx, "INSERT OR IGNORE INTO asset_human_fields(asset_id,field) VALUES(?,?)", id, field)
	return err
}
func (s *Service) allowAssetField(ctx context.Context, t AssetMaintenanceTask, field string) error {
	if !containsString(t.AssetFields, field) {
		return Fail(403, "asset_field_denied", "此任务未授权修改该作品字段。")
	}
	var locked bool
	if err := s.DB.DB.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM asset_human_fields WHERE asset_id=? AND field=?)", t.AssetID, field).Scan(&locked); err != nil {
		return err
	}
	if locked {
		return Fail(409, "human_field_protected", "该字段有人工修改或清空记录，请保留现有意图。")
	}
	return nil
}

// checkFileWrite is the file-change counterpart of checkWrite. It requires the
// task's explicit file opt-in and the task credential's separate scope.
func (t AssetMaintenanceTask) checkFileWrite(p model.Principal, a model.Asset, revision int64) error {
	if !t.ManageFiles || !p.Has("asset.files.write") {
		return Fail(403, "asset_files_denied", "此任务未授权回收站与移动。")
	}
	if a.Revision != t.ExpectedRevision || a.Revision != revision {
		return Conflict(a.Revision)
	}
	if t.UsedUpdates >= t.MaxUpdates {
		return Fail(409, "asset_updates_exhausted", "此任务的修改次数已用完。")
	}
	return nil
}

type AssetTaskTagsUpdate struct {
	ExpectedRevision int64                      `json:"expectedRevision"`
	Tags             []string                   `json:"tags"`
	Evidence         []AssetMaintenanceEvidence `json:"evidence"`
}

// ReplaceAssetTaskTags rewrites the whole tag set. Filling only empty tags is
// the default; replacing existing or human-edited tags needs the separate
// delegation flag recorded on the task at creation.
func (s *Service) ReplaceAssetTaskTags(ctx context.Context, w *Write, id string, in AssetTaskTagsUpdate) (AssetMaintenanceTask, error) {
	t, a, err := s.activeAssetTask(ctx, w.Principal, id)
	if err != nil {
		return t, err
	}
	if err = t.checkWrite(w.Principal, a, in.ExpectedRevision); err != nil {
		return t, err
	}
	if t.ReplaceExisting && !w.Principal.Has("asset.tags.replace") {
		return t, Fail(403, "asset_write_denied", "此任务未获整体替换标签的独立许可。")
	}
	if !containsString(t.AssetFields, "tags") {
		return t, Fail(403, "asset_field_denied", "此任务未授权修改该作品字段。")
	}
	if len(in.Tags) == 0 {
		return t, Fail(400, "asset_tags_required", "替换标签须至少保留一个标签；清空标签请在影库中由本人操作。")
	}
	if err = checkAssetEvidence([]string{"tags"}, in.Evidence); err != nil {
		return t, err
	}
	if !t.ReplaceExisting {
		if err = s.allowAssetField(ctx, t, "tags"); err != nil {
			return t, err
		}
		if len(a.Tags) > 0 || a.TagSetSource != "" {
			return t, Fail(409, "asset_field_occupied", "该字段已填写或曾被清空，本任务只补全缺失资料。")
		}
	}
	q, err := w.Q()
	if err != nil {
		return t, err
	}
	a.Tags, err = validateAssetTags(ctx, q, in.Tags)
	if err != nil {
		return t, err
	}
	a.TagsRevision++
	a.TagSetSource = "ai"
	a.Revision++
	a.UpdatedAt = time.Now().UTC()
	a.TagSetUpdatedAt = &a.UpdatedAt
	if err = store.SaveAssetTags(ctx, q, a); err != nil {
		return t, err
	}
	raw, err := store.Encode(in.Evidence)
	if err != nil {
		return t, err
	}
	if _, err = q.ExecContext(ctx, "INSERT INTO asset_maintenance_evidence(task_id,revision,data) VALUES(?,?,?)", t.ID, a.Revision, raw); err != nil {
		return t, err
	}
	t.ExpectedRevision = a.Revision
	t.UsedUpdates++
	return t, saveAssetMaintenance(ctx, q, t)
}

type AssetTaskFileUpdate struct {
	ExpectedRevision int64  `json:"expectedRevision"`
	RelativePath     string `json:"relativePath,omitempty"`
}

// TrashAssetForTask reuses the same file intent and trash layout as the app.
func (s *Service) TrashAssetForTask(ctx context.Context, w *Write, id string, in AssetTaskFileUpdate) (model.Asset, error) {
	t, a, err := s.activeAssetTaskState(ctx, w.Principal, id, false, true)
	if err != nil {
		return a, err
	}
	if err = t.checkFileWrite(w.Principal, a, in.ExpectedRevision); err != nil {
		return a, err
	}
	// Read outside w: opening the write transaction here would make prepareFile
	// reject the intent, because a durable intent must precede that transaction.
	if err = s.ensureAssetNotUnderProtection(ctx, a.ID); err != nil {
		return a, err
	}
	old := a
	a.Trashed = true
	a.StorageLayoutVersion = 2
	a.Revision++
	a.UpdatedAt = time.Now().UTC()
	t.ExpectedRevision = a.Revision
	t.UsedUpdates++
	return s.runFile(ctx, w, fileIntent{ID: w.ID("file"), Kind: "trash", Source: assetPath(old), Target: assetPath(a), Before: &old, After: a, MaintenanceTask: &t})
}

// RestoreAssetForTask lands the file wherever the app's restore would place it.
func (s *Service) RestoreAssetForTask(ctx context.Context, w *Write, id string, in AssetTaskFileUpdate) (model.Asset, error) {
	t, a, err := s.activeAssetTaskState(ctx, w.Principal, id, true, true)
	if err != nil {
		return a, err
	}
	if err = t.checkFileWrite(w.Principal, a, in.ExpectedRevision); err != nil {
		return a, err
	}
	if !a.Trashed {
		return a, Fail(409, "asset_not_trashed", "作品不在回收站。")
	}
	old := a
	a.Trashed = false
	target, err := s.availablePath(ctx, a.LibraryID, a.RelativePath)
	if err != nil {
		return a, err
	}
	a.RelativePath = target
	a.FileName = path.Base(target)
	a.Revision++
	a.UpdatedAt = time.Now().UTC()
	t.ExpectedRevision = a.Revision
	t.UsedUpdates++
	return s.runFile(ctx, w, fileIntent{ID: w.ID("file"), Kind: "restore", Source: assetPath(old), Target: assetPath(a), Before: &old, After: a, MaintenanceTask: &t})
}

// MoveAssetForTask moves inside the same library; checkPath rejects collisions
// and the library-relative target cannot cross into another library.
func (s *Service) MoveAssetForTask(ctx context.Context, w *Write, id, target string, revision int64) (model.Asset, error) {
	t, a, err := s.activeAssetTaskState(ctx, w.Principal, id, false, true)
	if err != nil {
		return a, err
	}
	if err = t.checkFileWrite(w.Principal, a, revision); err != nil {
		return a, err
	}
	return s.moveAsset(ctx, w, a, target, revision, &t)
}

type AssetMaintenanceEvidence struct {
	Field       string `json:"field"`
	URL         string `json:"url"`
	Observation string `json:"observation"`
}
type AssetMaintenanceRecord struct {
	TaskID   string                     `json:"taskId"`
	Revision int64                      `json:"revision"`
	Evidence []AssetMaintenanceEvidence `json:"evidence"`
}

func (s *Service) AssetTaskEvidence(ctx context.Context, p model.Principal, id, cursor string, count int) (model.Page[AssetMaintenanceRecord], error) {
	var page model.Page[AssetMaintenanceRecord]
	t, err := s.AssetTaskContext(ctx, p, id)
	if err != nil {
		return page, err
	}
	n, err := limited(count, 20)
	if err != nil {
		return page, err
	}
	filter := "asset-evidence:" + t.ID
	offset, err := pageOffset(cursor, filter)
	if err != nil {
		return page, err
	}
	items, err := store.List[AssetMaintenanceRecord](ctx, s.DB.DB, `SELECT json_object('taskId',e.task_id,'revision',e.revision,'evidence',json(e.data))
	 FROM asset_maintenance_evidence e JOIN asset_maintenance_tasks t ON e.task_id=t.id WHERE t.asset_id=? AND json_extract(t.data,'$.contentVersion')=?
	 ORDER BY e.revision,e.task_id LIMIT ? OFFSET ?`, t.AssetID, t.ContentVersion, n+1, offset)
	if err != nil {
		return page, err
	}
	return paged(items, n, offset, filter), nil
}

type AssetMaintenanceUpdate struct {
	ExpectedRevision int64                      `json:"expectedRevision"`
	AuthorIDs        *[]string                  `json:"authorIds"`
	SourceIDs        *[]string                  `json:"sourceIds"`
	CharacterIDs     *[]string                  `json:"characterIds"`
	Tags             *[]string                  `json:"tags"`
	Evidence         []AssetMaintenanceEvidence `json:"evidence"`
}

func (t AssetMaintenanceTask) checkWrite(p model.Principal, a model.Asset, revision int64) error {
	if !p.Has("asset.metadata.write") {
		return Fail(403, "asset_write_denied", "未授予作品资料写入权限。")
	}
	if a.Revision != t.ExpectedRevision || a.Revision != revision {
		return Conflict(a.Revision)
	}
	if t.UsedUpdates >= t.MaxUpdates {
		return Fail(409, "asset_updates_exhausted", "此任务的修改次数已用完。")
	}
	return nil
}
func checkAssetEvidence(fields []string, evidence []AssetMaintenanceEvidence) error {
	if len(fields) == 0 || len(evidence) < 1 || len(evidence) > 12 {
		return Fail(400, "asset_evidence_required", "请提交实际字段、来源及核对依据。")
	}
	present := map[string]bool{}
	for _, e := range evidence {
		if !containsString(fields, e.Field) || e.URL == "" || !validURL(e.URL) || len(e.URL) > 2000 || strings.TrimSpace(e.Observation) == "" || len(e.Observation) > 2000 {
			return Fail(400, "invalid_asset_evidence", "每条证据须对应本次字段且包含有效出处与观察。")
		}
		present[e.Field] = true
	}
	for _, field := range fields {
		if !present[field] {
			return Fail(400, "asset_evidence_required", "有字段缺少自己的核对依据。")
		}
	}
	return nil
}
func (s *Service) ApplyAssetMaintenance(ctx context.Context, w *Write, id string, in AssetMaintenanceUpdate) (AssetMaintenanceTask, error) {
	t, a, err := s.activeAssetTask(ctx, w.Principal, id)
	if err != nil {
		return t, err
	}
	if err = t.checkWrite(w.Principal, a, in.ExpectedRevision); err != nil {
		return t, err
	}
	fields := []string{}
	if in.AuthorIDs != nil {
		fields = append(fields, "authors")
	}
	if in.SourceIDs != nil || in.CharacterIDs != nil {
		fields = append(fields, "taxonomy")
	}
	if in.Tags != nil {
		fields = append(fields, "tags")
	}
	if err = checkAssetEvidence(fields, in.Evidence); err != nil {
		return t, err
	}
	for _, field := range fields {
		if err = s.allowAssetField(ctx, t, field); err != nil {
			return t, err
		}
	}
	notMissing := func() error {
		return Fail(409, "asset_field_occupied", "该字段已填写或曾被清空，本任务只补全缺失资料。")
	}
	q, err := w.Q()
	if err != nil {
		return t, err
	}
	var taxonomy *store.TaxonomyEdits
	if in.AuthorIDs != nil && (len(*in.AuthorIDs) == 0 || a.AuthorID != "unknown" || containsString(*in.AuthorIDs, "unknown")) {
		return t, notMissing()
	}
	if in.SourceIDs != nil || in.CharacterIDs != nil {
		if in.SourceIDs != nil {
			if len(*in.SourceIDs) == 0 || len(a.SourceIDs) > 0 || a.Source != "" {
				return t, notMissing()
			}
		}
		if in.CharacterIDs != nil {
			if len(*in.CharacterIDs) == 0 || len(a.CharacterIDs) > 0 || len(a.Characters) > 0 {
				return t, notMissing()
			}
		}
		previous := append([]string(nil), a.SourceIDs...)
		if taxonomy, err = editAssetTaxonomy(ctx, q, &a, AssetUpdate{SourceIDs: in.SourceIDs, CharacterIDs: in.CharacterIDs}); err != nil {
			return t, err
		}
		if len(previous) > 0 && !withinStrings(a.SourceIDs, previous) {
			return t, notMissing()
		}
	}
	if in.Tags != nil {
		if len(*in.Tags) == 0 || len(a.Tags) > 0 || a.TagSetSource != "" {
			return t, notMissing()
		}
		a.Tags, err = validateAssetTags(ctx, q, *in.Tags)
		if err != nil {
			return t, err
		}
		a.TagsRevision++
		a.TagSetSource = "ai"
	}
	previousAuthors := a.AuthorIDs
	if in.AuthorIDs != nil {
		if err = s.replaceAssetAuthors(ctx, w, q, &a, AssetUpdate{AuthorIDs: in.AuthorIDs, ExpectedAuthorsRevision: a.AuthorsRevision}); err != nil {
			return t, err
		}
	}
	a.Revision++
	a.UpdatedAt = time.Now().UTC()
	if in.Tags != nil {
		a.TagSetUpdatedAt = &a.UpdatedAt
	}
	if in.Tags != nil {
		if err = store.SaveAssetTags(ctx, q, a); err != nil {
			return t, err
		}
	}
	if in.AuthorIDs != nil || in.SourceIDs != nil || in.CharacterIDs != nil {
		if err = store.SaveAssetEdits(ctx, q, a, in.AuthorIDs != nil, taxonomy); err != nil {
			return t, err
		}
	}
	if in.AuthorIDs != nil {
		if err = s.recordExplicitAuthorSet(ctx, q, previousAuthors, a); err != nil {
			return t, err
		}
	}
	raw, err := store.Encode(in.Evidence)
	if err != nil {
		return t, err
	}
	if _, err = q.ExecContext(ctx, "INSERT INTO asset_maintenance_evidence(task_id,revision,data) VALUES(?,?,?)", t.ID, a.Revision, raw); err != nil {
		return t, err
	}
	t.ExpectedRevision = a.Revision
	t.UsedUpdates++
	return t, saveAssetMaintenance(ctx, q, t)
}

type AssetTaskRename struct {
	ExpectedRevision     int64              `json:"expectedRevision"`
	NamespaceFingerprint string             `json:"namespaceFingerprint"`
	Target               string             `json:"target"`
	Preview              NamingPreviewInput `json:"preview"`
}

func (s *Service) RenameAssetForTask(ctx context.Context, w *Write, id string, in AssetTaskRename) (model.Asset, error) {
	t, a, err := s.activeAssetTask(ctx, w.Principal, id)
	if err != nil {
		return a, err
	}
	if err = t.checkWrite(w.Principal, a, in.ExpectedRevision); err != nil {
		return a, err
	}
	if err = s.allowAssetField(ctx, t, "rename"); err != nil {
		return a, err
	}
	if len(in.Preview.Items) != 1 || in.Preview.AssetID != "" && in.Preview.AssetID != a.ID || path.Dir(in.Target) != path.Dir(a.RelativePath) {
		return a, Fail(400, "asset_rename_scope", "改名仅适用于当前作品与目录。")
	}
	if in.Preview.Intent == "rename_exact" {
		in.Preview.AssetID = a.ID
		in.Preview.ExpectedRevision = a.Revision
	}
	if in.Preview.Intent != "rename_exact" && in.Preview.Intent != "new_work" {
		return a, Fail(400, "asset_rename_intent", "现有单项改名请选择规范续号或精确名称。")
	}
	preview, err := s.previewNamesLocked(ctx, a.LibraryID, in.Preview)
	if err != nil {
		return a, err
	}
	if len(preview.Items) != 1 || preview.NamespaceFingerprint != in.NamespaceFingerprint || preview.Items[0].RelativePath != in.Target {
		return a, Fail(409, "naming_namespace_changed", "目录命名已变化，请重新读取 Go 命名预览。")
	}
	return s.moveAsset(ctx, w, a, in.Target, in.ExpectedRevision, &t)
}
