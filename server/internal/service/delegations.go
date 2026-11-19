package service

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/store"
)

type DelegationPolicy struct {
	Label             string   `json:"label"`
	LibraryIDs        []string `json:"libraryIds"`
	AuthorIDs         []string `json:"authorIds"`
	AllAuthors        bool     `json:"allAuthors"`
	AllowedFields     []string `json:"allowedFields"`
	CandidateBlocks   []string `json:"candidateBlocks"`
	AllowClear        bool     `json:"allowClear"`
	CanIngest         bool     `json:"canIngest"`
	CanResearch       bool     `json:"canResearch"`
	CanLookupImages   bool     `json:"canLookupImages,omitempty"`
	ResearchProviders []string `json:"researchProviders,omitempty"`
	CanResearchAssets bool     `json:"canResearchAssets,omitempty"`
	AssetFields       []string `json:"assetFields,omitempty"`
	CanOrganize       bool     `json:"canOrganize"`
	CanRecycleSources bool     `json:"canRecycleSources"`
	MaxResearchCalls  int      `json:"maxResearchCalls"`
	MaxUpdates        int      `json:"maxUpdates"`
	MaxTasksPerDay    int      `json:"maxTasksPerDay"`
	ValidDays         int      `json:"validDays"`
	MaxCallsPerTask   int      `json:"maxCallsPerTask"`
	TaskSeconds       int      `json:"taskSeconds"`
}
type Delegation struct {
	DelegationPolicy
	ID        string    `json:"id"`
	State     string    `json:"state"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
}
type DelegationCreated struct {
	Delegation Delegation      `json:"delegation"`
	Credential model.Principal `json:"credential"`
}
type DelegatedTaskStart struct {
	Kind                     string   `json:"kind"`
	Label                    string   `json:"label"`
	LibraryID                string   `json:"libraryId"`
	BatchID                  string   `json:"batchId"`
	ReadyManifestFingerprint *string  `json:"readyManifestFingerprint,omitempty"`
	Mode                     string   `json:"mode"`
	MaxResearchCalls         *int     `json:"maxResearchCalls"`
	ResearchProviders        []string `json:"researchProviders,omitzero"`
	AllowImageLookup         bool     `json:"allowImageLookup,omitempty"`
	AuthorID                 string   `json:"authorId"`
	AssetID                  string   `json:"assetId"`
	ExpectedContentVersion   int64    `json:"expectedContentVersion"`
	AssetFields              []string `json:"assetFields"`
	ExpectedRevision         int64    `json:"expectedRevision"`
	AllowedFields            []string `json:"allowedFields"`
	CandidateBlocks          []string `json:"candidateBlocks"`
	AllowClear               bool     `json:"allowClear"`
	MaxUpdates               int      `json:"maxUpdates"`
	CredentialSecret         string   `json:"credentialSecret"`
}
type DelegatedTask struct {
	TaskID     string                `json:"taskId"`
	Kind       string                `json:"kind"`
	Batch      *model.Batch          `json:"batch,omitempty"`
	AuthorTask *AuthorTask           `json:"authorTask,omitempty"`
	AssetTask  *AssetMaintenanceTask `json:"assetTask,omitempty"`
	Credential model.Principal       `json:"credential"`
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
func withinStrings(values, allowed []string) bool {
	for _, value := range values {
		if !containsString(allowed, value) {
			return false
		}
	}
	return true
}

func (s *Service) delegation(ctx context.Context, id string) (Delegation, error) {
	value, e := store.Get[Delegation](ctx, s.DB.DB, "SELECT data FROM ai_delegations WHERE id=?", id)
	return value, Normalize(e)
}
func (s *Service) validateDelegation(ctx context.Context, p model.Principal) error {
	if p.ParentDelegationID == "" {
		return nil
	}
	d, e := s.delegation(ctx, p.ParentDelegationID)
	if e != nil {
		return e
	}
	if d.State != "active" || !d.ExpiresAt.After(time.Now()) {
		return Fail(401, "delegation_inactive", "AI长期授权已撤销或到期。")
	}
	return nil
}
func (s *Service) CurrentDelegation(ctx context.Context, p model.Principal) (Delegation, error) {
	if !p.Has("delegate") || p.ParentDelegationID == "" {
		return Delegation{}, Fail(403, "delegation_required", "需要首次配置签发的执行器凭据。")
	}
	if e := s.validateDelegation(ctx, p); e != nil {
		return Delegation{}, e
	}
	return s.delegation(ctx, p.ParentDelegationID)
}
func (s *Service) Delegations(ctx context.Context, p model.Principal, state, cursor string, count int) (model.Page[Delegation], error) {
	result := model.Page[Delegation]{Items: []Delegation{}}
	if e := Authorize(p, "admin", ""); e != nil {
		return result, e
	}
	if state == "" {
		state = "active"
	}
	switch state {
	case "active", "expired", "revoked", "all":
	default:
		return result, Fail(400, "invalid_state", "授权状态无效。")
	}
	n, e := limited(count, 50)
	if e != nil {
		return result, e
	}
	filter := "delegations:" + state
	after, e := readSeek(cursor, filter, "created")
	if e != nil {
		return result, e
	}
	now := time.Now().UTC()
	values, e := store.List[Delegation](ctx, s.DB.DB, `SELECT data FROM ai_delegations WHERE (?='all' OR CASE WHEN state='active' AND julianday(expires_at)<=julianday(?) THEN 'expired' ELSE state END=?)
 AND (?='' OR json_extract(data,'$.createdAt')<? OR (json_extract(data,'$.createdAt')=? AND id>?)) ORDER BY json_extract(data,'$.createdAt') DESC,id LIMIT ?`, state, now.Format(time.RFC3339Nano), state, after.ID, after.Value, after.Value, after.ID, n+1)
	if e != nil {
		return result, e
	}
	if len(values) > n {
		values = values[:n]
		last := values[n-1]
		result.NextCursor = makeSeek(filter, "created", last.CreatedAt.Format(time.RFC3339Nano), last.ID)
	}
	for i := range values {
		if values[i].State == "active" && !values[i].ExpiresAt.After(now) {
			values[i].State = "expired"
		}
	}
	result.Items = values
	return result, nil
}
func (s *Service) CreateDelegation(ctx context.Context, w *Write, in DelegationPolicy) (DelegationCreated, error) {
	var result DelegationCreated
	if e := Authorize(w.Principal, "admin", ""); e != nil {
		return result, e
	}
	if in.MaxCallsPerTask == 0 {
		in.MaxCallsPerTask = 100
	}
	if in.TaskSeconds == 0 {
		in.TaskSeconds = 3600
	}
	if strings.TrimSpace(in.Label) == "" || len(in.Label) > 80 || in.MaxResearchCalls < 0 || in.MaxResearchCalls > 100 || in.MaxUpdates < 1 || in.MaxUpdates > 10 || in.MaxTasksPerDay < 1 || in.MaxTasksPerDay > 200 || in.ValidDays < 1 || in.ValidDays > 365 {
		return result, Fail(400, "invalid_delegation", "授权名称、预算或有效期无效。")
	}
	if in.MaxCallsPerTask < 10 || in.MaxCallsPerTask > 10000 || in.TaskSeconds < 60 || in.TaskSeconds > 86400 || !in.CanResearch && in.MaxResearchCalls != 0 {
		return result, Fail(400, "invalid_delegation_budget", "任务预算与授权动作不一致。")
	}
	var e error
	if in.LibraryIDs, e = cleanStrings(in.LibraryIDs, 100, 128); e != nil {
		return result, e
	}
	if in.AuthorIDs, e = cleanStrings(in.AuthorIDs, 100, 128); e != nil {
		return result, e
	}
	if in.AllowedFields, e = cleanStrings(in.AllowedFields, 4, 32); e != nil {
		return result, e
	}
	if in.CandidateBlocks, e = cleanStrings(in.CandidateBlocks, 3, 32); e != nil {
		return result, e
	}
	if !withinStrings(in.AllowedFields, []string{"name", "biography", "homepage", "aliases"}) || !withinStrings(in.CandidateBlocks, []string{"x", "basic", "accounts"}) {
		return result, Fail(400, "invalid_fields", "授权资料字段或资料块无效。")
	}
	if !in.CanResearch && len(in.CandidateBlocks) > 0 {
		return result, Fail(400, "research_required", "提交资料候选需要明确研究权限。")
	}
	if in.CanRecycleSources && !in.CanIngest {
		return result, Fail(400, "intake_scope_required", "来源回收须独立授权并绑定允许的接收库。")
	}
	if e = validateAssetPolicy(&in); e != nil {
		return result, e
	}
	if e = validateResearchProviders(in.ResearchProviders, in.CanResearch, in.MaxResearchCalls); e != nil {
		return result, e
	}
	if in.CanLookupImages && (!in.CanResearch || in.MaxResearchCalls < 1 || !in.CanIngest && !in.CanResearchAssets) {
		return result, Fail(400, "image_lookup_permission_invalid", "单帧外发须授权接收或作品预览，并授予研究权限与正数预算。")
	}
	if len(in.LibraryIDs) == 0 && (in.CanIngest || in.CanOrganize) {
		return result, Fail(400, "library_required", "入库整理须明确选择允许的库。")
	}
	if !in.AllAuthors && len(in.AuthorIDs) == 0 && (len(in.AllowedFields) > 0 || len(in.CandidateBlocks) > 0) {
		return result, Fail(400, "author_scope_required", "资料维护须指定作者或明确选择全部作者。")
	}
	for _, id := range in.LibraryIDs {
		if _, e = s.library(ctx, id); e != nil {
			return result, e
		}
	}
	for _, id := range in.AuthorIDs {
		a, err := s.Author(ctx, id)
		if err != nil {
			return result, err
		}
		if a.Placeholder {
			return result, Fail(409, "placeholder_protected", "占位作者不能授予资料维护权限。")
		}
	}
	var count int
	if e = s.DB.DB.QueryRowContext(ctx, "SELECT count(*) FROM ai_delegations WHERE state='active' AND julianday(expires_at)>julianday(?)", time.Now().UTC().Format(time.RFC3339Nano)).Scan(&count); e != nil {
		return result, e
	}
	if count >= 32 {
		return result, Fail(409, "delegation_limit", "有效AI授权已达到32个，请先撤销不用的授权。")
	}
	now := time.Now().UTC()
	d := Delegation{DelegationPolicy: in, ID: ID(), State: "active", CreatedAt: now, ExpiresAt: now.AddDate(0, 0, in.ValidDays)}
	p := model.Principal{ID: d.ID, Label: in.Label, Scopes: []string{"read", "delegate"}, ParentDelegationID: d.ID, Generation: 1, ExpiresAt: d.ExpiresAt}
	secret := token()
	raw, e := store.Encode(d)
	if e != nil {
		return result, e
	}
	q, e := w.Q()
	if e != nil {
		return result, e
	}
	if _, e = q.ExecContext(ctx, "INSERT INTO ai_delegations VALUES(?,?,?,?)", d.ID, d.State, d.ExpiresAt.Format(time.RFC3339Nano), raw); e != nil {
		return result, e
	}
	if e = saveDelegatedCredential(ctx, q, p, secret); e != nil {
		return result, e
	}
	p.Token = secret
	return DelegationCreated{d, p}, nil
}
func (s *Service) RevokeDelegation(ctx context.Context, w *Write, id string) error {
	if e := Authorize(w.Principal, "admin", ""); e != nil {
		return e
	}
	d, e := s.delegation(ctx, id)
	if e != nil {
		return e
	}
	d.State = "revoked"
	raw, e := store.Encode(d)
	if e != nil {
		return e
	}
	q, e := w.Q()
	if e != nil {
		return e
	}
	if _, e = q.ExecContext(ctx, "UPDATE ai_delegations SET state='revoked',data=? WHERE id=?", raw, id); e != nil {
		return e
	}
	_, e = q.ExecContext(ctx, "DELETE FROM credentials WHERE id=? OR json_extract(data,'$.parentDelegationId')=?", id, id)
	if e == nil {
		w.after = append(w.after, func() error { s.researchGateway.cancel("", id); return nil })
	}
	return e
}

func validExecutorSecret(value string) bool {
	decoded, e := base64.RawURLEncoding.DecodeString(value)
	return e == nil && len(decoded) == 32 && len(value) == 43
}
func saveDelegatedCredential(ctx context.Context, q store.Querier, p model.Principal, secret string) error {
	raw, e := store.Encode(p)
	if e != nil {
		return e
	}
	_, e = q.ExecContext(ctx, `INSERT INTO credentials(id,token_hash,expires_at,data) VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET token_hash=excluded.token_hash,expires_at=excluded.expires_at,data=excluded.data`, p.ID, digest([]byte(secret)), p.ExpiresAt.Format(time.RFC3339Nano), raw)
	return e
}
func limitExpiry(d Delegation, duration time.Duration) time.Time {
	expiry := time.Now().UTC().Add(duration)
	if d.ExpiresAt.Before(expiry) {
		return d.ExpiresAt
	}
	return expiry
}

func (s *Service) StartDelegatedTask(ctx context.Context, w *Write, in DelegatedTaskStart) (DelegatedTask, error) {
	var result DelegatedTask
	d, e := s.CurrentDelegation(ctx, w.Principal)
	if e != nil {
		return result, e
	}
	if !validExecutorSecret(in.CredentialSecret) || strings.TrimSpace(in.Label) == "" || len(in.Label) > 80 {
		return result, Fail(400, "invalid_executor", "执行器凭据或任务名称无效。")
	}
	today := time.Now().Format("2006-01-02")
	if e = s.checkDailyTaskLimit(ctx, d, today); e != nil {
		return result, e
	}
	if in.ReadyManifestFingerprint != nil {
		hash := *in.ReadyManifestFingerprint
		if in.Kind != "intake" || in.BatchID == "" || in.Mode != "intelligent" || len(hash) != 64 || strings.Trim(hash, "0123456789abcdef") != "" {
			return result, Fail(400, "invalid_queue_claim", "自动认领须明确指定已提交的智能批次与清单摘要。")
		}
	}
	result.Kind = in.Kind
	providers := d.ResearchProviders
	if in.ResearchProviders != nil {
		providers = in.ResearchProviders
	}
	if !withinStrings(providers, d.ResearchProviders) {
		return result, Fail(403, "research_provider_denied", "该研究来源未在首次授权中选择。")
	}
	researchLimit := d.MaxResearchCalls
	if in.MaxResearchCalls != nil {
		researchLimit = *in.MaxResearchCalls
	}
	if researchLimit < 0 || researchLimit > d.MaxResearchCalls {
		return result, Fail(403, "research_budget_denied", "任务研究预算超出长期授权。")
	}
	p := model.Principal{ID: w.ID("delegated-credential"), Label: in.Label, Scopes: []string{"read"}, ParentDelegationID: d.ID, Generation: 1, ExpiresAt: limitExpiry(d, 24*time.Hour)}
	isNewBatch := false
	switch in.Kind {
	case "intake":
		if !d.CanIngest || !containsString(d.LibraryIDs, in.LibraryID) {
			return result, Fail(403, "library_scope_denied", "首次配置未授权此库的接收任务。")
		}
		mode := in.Mode
		if mode == "" {
			mode = "intelligent"
		}
		if mode == "intelligent" && !d.CanOrganize {
			return result, Fail(403, "organize_scope_denied", "智能整理需要明确整理权限。")
		}
		var b model.Batch
		if in.BatchID != "" {
			b, e = s.Batch(ctx, in.BatchID)
			if e != nil {
				return result, e
			}
			if b.LibraryID != in.LibraryID || b.Mode != mode || in.ReadyManifestFingerprint == nil && b.MaxResearchCalls > researchLimit {
				return result, Fail(409, "batch_not_claimable", "批次范围、预算或状态不符合本授权。")
			}
			researchLimit = min(researchLimit, b.MaxResearchCalls)
			var exists int
			e = s.DB.DB.QueryRowContext(ctx, "SELECT count(*) FROM delegated_tasks WHERE task_id=?", b.ID).Scan(&exists)
			if e != nil {
				return result, e
			}
			if exists != 0 {
				return result, Fail(409, "task_already_claimed", "任务已被认领，请用原任务ID接续。")
			}
			claimable := b.State == "waiting_executor" || b.State == "receiving"
			if in.ReadyManifestFingerprint != nil {
				e = s.DB.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM ready_intake_batches WHERE batch_id=? AND manifest_fingerprint=?)`, b.ID, *in.ReadyManifestFingerprint).Scan(&claimable)
				p.IntakeResearchLimit = &researchLimit
			} else if b.State == "partial" {
				e = s.DB.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM batch_inputs i LEFT JOIN uploads u ON u.id=i.upload_id
 WHERE i.batch_id=? AND i.cancelled=0 AND i.rejection_code='' AND (i.upload_id IS NULL OR u.state IN ('receiving','staged')))`, b.ID).Scan(&claimable)
			}
			if e != nil {
				return result, e
			}
			if !claimable {
				return result, Fail(409, "batch_not_claimable", "批次没有可接手的待处理输入，或所选清单已变化。")
			}
		} else {
			b, e = s.prepareBatch(ctx, w.ID("batch"), model.Batch{LibraryID: in.LibraryID, Mode: mode, Label: in.Label, MaxResearchCalls: researchLimit})
			if e != nil {
				return result, e
			}
			isNewBatch = true
		}
		result.TaskID = b.ID
		result.Batch = &b
		p.BatchID = b.ID
		p.Scopes = append(p.Scopes, "ingest")
		if d.CanResearch && (p.IntakeResearchLimit == nil || researchLimit > 0) {
			p.Scopes = append(p.Scopes, "research")
		}
		if d.CanOrganize {
			p.Scopes = append(p.Scopes, "organize")
		}
		if d.CanRecycleSources {
			p.Scopes = append(p.Scopes, "source.cleanup")
		}
	case "author":
		if !d.AllAuthors && !containsString(d.AuthorIDs, in.AuthorID) {
			return result, Fail(403, "author_scope_denied", "首次配置未授权此作者。")
		}
		if !withinStrings(in.AllowedFields, d.AllowedFields) || !withinStrings(in.CandidateBlocks, d.CandidateBlocks) || in.AllowClear && !d.AllowClear {
			return result, Fail(403, "field_scope_denied", "任务资料范围或清空权限超出长期授权。")
		}
		updates := in.MaxUpdates
		if updates == 0 {
			updates = d.MaxUpdates
		}
		if updates > d.MaxUpdates {
			return result, Fail(403, "update_budget_denied", "任务写入次数超出长期授权。")
		}
		fields := in.AllowedFields
		if fields == nil {
			fields = []string{}
		}
		task, err := s.prepareAuthorTask(ctx, w.ID("author-task"), AuthorTaskCreate{AuthorID: in.AuthorID, ExpectedRevision: in.ExpectedRevision, AllowedFields: fields, CandidateBlocks: in.CandidateBlocks, AllowClear: in.AllowClear, MaxUpdates: updates, ExpiresInSeconds: 3600})
		if err != nil {
			return result, err
		}
		if d.ExpiresAt.Before(task.ExpiresAt) {
			task.ExpiresAt = d.ExpiresAt
		}
		result.TaskID = task.ID
		result.AuthorTask = &task
		p.AuthorTaskID = task.ID
		p.ExpiresAt = task.ExpiresAt
		if len(task.AllowedFields) > 0 {
			p.Scopes = append(p.Scopes, "author.profile.write")
		}
		if len(task.CandidateBlocks) > 0 {
			p.Scopes = append(p.Scopes, "research")
		}
	case "asset":
		task, err := s.prepareAssetMaintenance(ctx, w.ID("asset-task"), d, in)
		if err != nil {
			return result, err
		}
		result.TaskID = task.ID
		result.AssetTask = &task
		p.AssetTaskID = task.ID
		p.ExpiresAt = task.ExpiresAt
		if task.Research {
			p.Scopes = append(p.Scopes, "research")
		}
		if len(task.AssetFields) > 0 {
			p.Scopes = append(p.Scopes, "asset.metadata.write")
		}
	default:
		return result, Fail(400, "invalid_task_kind", "任务类别必须是 intake、author 或 asset。")
	}
	if e = validateResearchProviders(in.ResearchProviders, d.CanResearch, researchLimit); e != nil {
		return result, e
	}
	q, e := w.Q()
	if e != nil {
		return result, e
	}
	if result.Kind == "intake" && !isNewBatch && in.ReadyManifestFingerprint == nil {
		if _, e = q.ExecContext(ctx, "DELETE FROM credentials WHERE json_extract(data,'$.batchId')=?", result.TaskID); e != nil {
			return result, e
		}
	}
	if isNewBatch {
		if e = store.SaveBatch(ctx, q, *result.Batch); e != nil {
			return result, e
		}
	}
	if result.AuthorTask != nil {
		if e = saveAuthorTask(ctx, q, *result.AuthorTask); e != nil {
			return result, e
		}
	}
	if result.AssetTask != nil {
		if e = saveAssetMaintenance(ctx, q, *result.AssetTask); e != nil {
			return result, e
		}
	}
	if e = saveDelegatedCredential(ctx, q, p, in.CredentialSecret); e != nil {
		return result, e
	}
	if !p.Has("research") || researchLimit == 0 {
		providers = []string{}
	}
	if in.AllowImageLookup && (!d.CanLookupImages || in.Kind == "author" || !p.Has("research") || researchLimit <= 0) {
		return result, Fail(403, "image_lookup_denied", "单帧外发须属于获准的媒体任务，并具有研究预算。")
	}
	if providers == nil {
		providers = []string{}
	}
	encoded, e := store.Encode(providers)
	if e != nil {
		return result, e
	}
	if _, e = q.ExecContext(ctx, "INSERT INTO delegated_tasks(task_id,delegation_id,credential_id,kind,created_day,max_calls,deadline,research_providers,provider_limit,allow_image_lookup) VALUES(?,?,?,?,?,?,?,?,?,?)", result.TaskID, d.ID, p.ID, result.Kind, today, d.MaxCallsPerTask, limitExpiry(d, time.Duration(d.TaskSeconds)*time.Second).Format(time.RFC3339Nano), encoded, researchLimit, in.AllowImageLookup); e != nil {
		return result, e
	}
	result.Credential = p
	return result, nil
}

func (s *Service) ResumeDelegatedTask(ctx context.Context, w *Write, id, secret string) (DelegatedTask, error) {
	var result DelegatedTask
	d, e := s.CurrentDelegation(ctx, w.Principal)
	if e != nil {
		return result, e
	}
	if !validExecutorSecret(secret) {
		return result, Fail(400, "invalid_executor", "执行器凭据无效。")
	}
	var parent, credentialID string
	e = s.DB.DB.QueryRowContext(ctx, "SELECT delegation_id,credential_id,kind FROM delegated_tasks WHERE task_id=?", id).Scan(&parent, &credentialID, &result.Kind)
	if e != nil {
		return result, Normalize(e)
	}
	if parent != d.ID {
		return result, Fail(403, "task_scope_denied", "任务不属于该长期授权。")
	}
	p, e := store.Get[model.Principal](ctx, s.DB.DB, "SELECT data FROM credentials WHERE id=?", credentialID)
	if errors.Is(e, sql.ErrNoRows) {
		return result, Fail(409, "task_credential_revoked", "此任务凭据已单独撤销，不能自动恢复。")
	}
	if e != nil {
		return result, e
	}
	switch result.Kind {
	case "intake":
		b, err := s.Batch(ctx, id)
		if err != nil {
			return result, err
		}
		if b.State == "cancelled" {
			return result, Fail(409, "task_cancelled", "任务已取消。")
		}
		result.Batch = &b
		p.ExpiresAt = limitExpiry(d, 24*time.Hour)
	case "author":
		task, err := s.authorTask(ctx, id)
		if err != nil {
			return result, err
		}
		if task.State != "authorized" || !task.ExpiresAt.After(time.Now()) {
			return result, Fail(409, "author_task_stale", "资料任务已完成、取消或到期，请按当前资料修订创建新任务。")
		}
		result.AuthorTask = &task
		p.ExpiresAt = task.ExpiresAt
	case "asset":
		task, err := s.assetMaintenance(ctx, id)
		if err != nil {
			return result, err
		}
		if !task.ExpiresAt.After(time.Now()) {
			return result, Fail(409, "asset_task_expired", "作品任务已到期。")
		}
		result.AssetTask = &task
		p.ExpiresAt = task.ExpiresAt
	default:
		return result, Fail(409, "task_kind_invalid", "任务记录无效。")
	}
	p.Generation++
	q, e := w.Q()
	if e != nil {
		return result, e
	}
	if e = saveDelegatedCredential(ctx, q, p, secret); e != nil {
		return result, e
	}
	w.after = append(w.after, func() error { s.researchGateway.cancel(credentialID, ""); return nil })
	result.TaskID = id
	result.Credential = p
	return result, nil
}

type DelegatedBudget struct {
	Calls    int       `json:"calls"`
	MaxCalls int       `json:"maxCalls"`
	Deadline time.Time `json:"deadline"`
}

func (s *Service) DelegatedTaskBudget(ctx context.Context, p model.Principal, id string) (DelegatedBudget, error) {
	var result DelegatedBudget
	var parent, text string
	e := s.DB.DB.QueryRowContext(ctx, "SELECT delegation_id,calls,max_calls,deadline FROM delegated_tasks WHERE task_id=?", id).Scan(&parent, &result.Calls, &result.MaxCalls, &text)
	if e != nil {
		return result, Normalize(e)
	}
	if !p.Has("admin") && parent != p.ParentDelegationID {
		return result, Fail(403, "task_scope_denied", "预算不属于当前授权。")
	}
	result.Deadline, e = time.Parse(time.RFC3339Nano, text)
	return result, e
}

type DelegatedBudgetUpdate struct {
	MaxCalls         int `json:"maxCalls"`
	ExpiresInSeconds int `json:"expiresInSeconds"`
}

func (s *Service) RenewDelegatedBudget(ctx context.Context, w *Write, id string, in DelegatedBudgetUpdate) (DelegatedBudget, error) {
	if e := Authorize(w.Principal, "admin", ""); e != nil {
		return DelegatedBudget{}, e
	}
	budget, e := s.DelegatedTaskBudget(ctx, w.Principal, id)
	if e != nil {
		return budget, e
	}
	deadline := time.Now().UTC().Add(time.Duration(in.ExpiresInSeconds) * time.Second)
	if in.MaxCalls < budget.MaxCalls || in.MaxCalls > 10000 || in.ExpiresInSeconds < 60 || in.ExpiresInSeconds > 86400 || deadline.Before(budget.Deadline) {
		return budget, Fail(400, "invalid_budget", "续期只能提高上限及截止时间，不能清零已消费次数。")
	}
	q, e := w.Q()
	if e != nil {
		return budget, e
	}
	_, e = q.ExecContext(ctx, "UPDATE delegated_tasks SET max_calls=?,deadline=? WHERE task_id=?", in.MaxCalls, deadline.Format(time.RFC3339Nano), id)
	budget.MaxCalls = in.MaxCalls
	budget.Deadline = deadline
	return budget, e
}
func (s *Service) ReserveDelegatedCall(ctx context.Context, w *Write, id string) (DelegatedBudget, error) {
	var budget DelegatedBudget
	var credentialID, text string
	if w.Principal.ParentDelegationID == "" || w.Principal.BatchID != id && w.Principal.AuthorTaskID != id && w.Principal.AssetTaskID != id {
		return budget, Fail(403, "task_scope_denied", "预算只属于当前受限任务凭据。")
	}
	e := s.DB.DB.QueryRowContext(ctx, "SELECT credential_id,calls,max_calls,deadline FROM delegated_tasks WHERE task_id=?", id).Scan(&credentialID, &budget.Calls, &budget.MaxCalls, &text)
	if e != nil {
		return budget, Normalize(e)
	}
	if credentialID != w.Principal.ID {
		return budget, Fail(403, "task_scope_denied", "执行器未持有该任务凭据。")
	}
	budget.Deadline, e = time.Parse(time.RFC3339Nano, text)
	if e != nil {
		return budget, e
	}
	if budget.Calls >= budget.MaxCalls || !budget.Deadline.After(time.Now()) {
		return budget, Fail(429, "task_budget_exhausted", "任务预算耗尽，服务端进度保留；由可信端核对后决定续期。")
	}
	q, e := w.Q()
	if e != nil {
		return budget, e
	}
	_, e = q.ExecContext(ctx, "UPDATE delegated_tasks SET calls=calls+1 WHERE task_id=?", id)
	budget.Calls++
	return budget, e
}
