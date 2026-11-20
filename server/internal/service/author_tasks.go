package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/store"
)

type AuthorTaskCreate struct {
	AuthorID         string   `json:"authorId"`
	ExpectedRevision int64    `json:"expectedRevision"`
	AllowedFields    []string `json:"allowedFields"`
	CandidateBlocks  []string `json:"candidateBlocks"`
	AllowClear       bool     `json:"allowClear"`
	MaxUpdates       int      `json:"maxUpdates"`
	ExpiresInSeconds int      `json:"expiresInSeconds"`
}
type AuthorTask struct {
	ID               string    `json:"id"`
	AuthorID         string    `json:"authorId"`
	ExpectedRevision int64     `json:"expectedRevision"`
	AllowedFields    []string  `json:"allowedFields"`
	CandidateBlocks  []string  `json:"candidateBlocks"`
	AllowClear       bool      `json:"allowClear"`
	MaxUpdates       int       `json:"maxUpdates"`
	UsedUpdates      int       `json:"usedUpdates"`
	State            string    `json:"state"`
	ExpiresAt        time.Time `json:"expiresAt"`
	CreatedAt        time.Time `json:"createdAt"`
}

func (s *Service) authorTask(ctx context.Context, id string) (AuthorTask, error) {
	task, e := store.Get[AuthorTask](ctx, s.DB.DB, "SELECT data FROM author_tasks WHERE id=?", id)
	return task, Normalize(e)
}
func (s *Service) CreateAuthorTask(ctx context.Context, w *Write, in AuthorTaskCreate) (AuthorTask, error) {
	if e := Authorize(w.Principal, "admin", ""); e != nil {
		return AuthorTask{}, e
	}
	result, e := s.prepareAuthorTask(ctx, w.ID("author-task"), in)
	if e != nil {
		return result, e
	}
	q, e := w.Q()
	if e != nil {
		return result, e
	}
	return result, saveAuthorTask(ctx, q, result)
}

func saveAuthorTask(ctx context.Context, q store.Querier, result AuthorTask) error {
	raw, e := store.Encode(result)
	if e != nil {
		return e
	}
	_, e = q.ExecContext(ctx, "INSERT INTO author_tasks VALUES(?,?,?,?,?)", result.ID, result.AuthorID, result.State, result.ExpiresAt.Format(time.RFC3339Nano), raw)
	return e
}

func (s *Service) prepareAuthorTask(ctx context.Context, id string, in AuthorTaskCreate) (AuthorTask, error) {
	var result AuthorTask
	author, e := s.Author(ctx, in.AuthorID)
	if e != nil {
		return result, e
	}
	if author.Placeholder {
		return result, Fail(409, "placeholder_protected", "占位作者不能用于资料维护。")
	}
	if author.Revision != in.ExpectedRevision {
		return result, Conflict(author.Revision)
	}
	fields, e := cleanStrings(in.AllowedFields, 4, 32)
	if e != nil {
		return result, e
	}
	if in.CandidateBlocks == nil {
		in.CandidateBlocks = []string{}
	}
	blocks, e := cleanStrings(in.CandidateBlocks, 3, 32)
	if e != nil {
		return result, e
	}
	if len(fields)+len(blocks) == 0 {
		return result, Fail(400, "fields_required", "至少授权一个资料字段或候选资料块。")
	}
	for _, block := range blocks {
		if block != "x" && block != "basic" && block != "accounts" {
			return result, Fail(400, "invalid_blocks", "候选资料块无效。")
		}
	}
	for _, field := range fields {
		switch field {
		case "name", "biography", "homepage", "aliases":
		default:
			return result, Fail(400, "invalid_fields", "不支持的资料字段。")
		}
	}
	for _, field := range fields {
		if author.Kind == "cos" && field == "name" {
			return result, Fail(400, "cos_name_is_directory_identity", "COS名称由目录确定；可授权basic候选资料块维护公开名称。")
		}
	}
	if in.MaxUpdates == 0 {
		in.MaxUpdates = 1
	}
	if in.ExpiresInSeconds == 0 {
		in.ExpiresInSeconds = 3600
	}
	if in.MaxUpdates < 1 || in.ExpiresInSeconds < 60 || in.ExpiresInSeconds > 86400 {
		return result, Fail(400, "invalid_budget", "维护次数或有效期超出允许范围。")
	}
	now := time.Now().UTC()
	result = AuthorTask{ID: id, AuthorID: author.ID, ExpectedRevision: author.Revision,
		AllowedFields: fields, CandidateBlocks: blocks, AllowClear: in.AllowClear, MaxUpdates: in.MaxUpdates, State: "authorized", CreatedAt: now, ExpiresAt: now.Add(time.Duration(in.ExpiresInSeconds) * time.Second)}
	return result, nil
}

func (s *Service) AuthorTaskContext(ctx context.Context, p model.Principal, id string) (map[string]any, error) {
	if !p.Has("admin") && p.AuthorTaskID != id {
		return nil, Fail(403, "task_scope_denied", "凭据不属于此资料维护任务。")
	}
	task, e := s.authorTask(ctx, id)
	if e != nil {
		return nil, e
	}
	author, e := s.Author(ctx, task.AuthorID)
	if e != nil {
		var missing *Error
		if !errors.As(e, &missing) || missing.Status != 404 || task.State == "authorized" {
			return nil, e
		}
		author, e = store.Get[model.Author](ctx, s.DB.DB, "SELECT data FROM author_archives WHERE author_id=? ORDER BY created_at DESC,id DESC LIMIT 1", task.AuthorID)
		if e != nil {
			return nil, Normalize(e)
		}
	}
	actions := []string{}
	if task.State == "authorized" && task.ExpiresAt.After(time.Now()) && task.ExpectedRevision == author.Revision {
		if len(task.AllowedFields) > 0 {
			actions = append(actions, "author.profile.update")
		}
		if len(task.CandidateBlocks) > 0 {
			actions = append(actions, "author.profile.candidate.submit")
		}
	}
	return map[string]any{"task": effectiveAuthorTask(task, time.Now()), "author": author, "nextActions": actions}, nil
}

func (s *Service) CancelAuthorTask(ctx context.Context, w *Write, id string) error {
	if e := Authorize(w.Principal, "admin", ""); e != nil {
		return e
	}
	task, e := s.authorTask(ctx, id)
	if e != nil {
		return e
	}
	if task.State != "authorized" {
		return nil
	}
	task.State = "cancelled"
	raw, e := store.Encode(task)
	if e != nil {
		return e
	}
	q, e := w.Q()
	if e != nil {
		return e
	}
	_, e = q.ExecContext(ctx, "UPDATE author_tasks SET state=?,data=? WHERE id=?", task.State, raw, id)
	if e == nil {
		w.after = append(w.after, func() error { s.researchGateway.cancelSubject(id, ""); return nil })
	}
	return e
}

// authorizeAuthorUpdate only accepts a trusted task bound to this author and version.
// It does not promote model-provided confirmation flags or create new authority.
func (s *Service) authorizeAuthorUpdate(ctx context.Context, p model.Principal, id string, in AuthorUpdate) (*AuthorTask, error) {
	if p.Has("admin") {
		return nil, nil
	}
	if !p.Has("author.profile.write") || p.AuthorTaskID == "" {
		return nil, Fail(403, "scope_denied", "未授予作者资料维护权限。")
	}
	task, e := s.authorTask(ctx, p.AuthorTaskID)
	if e != nil {
		return nil, e
	}
	if task.AuthorID != id || task.ExpectedRevision != in.ExpectedRevision || task.State != "authorized" || !task.ExpiresAt.After(time.Now()) || task.UsedUpdates >= task.MaxUpdates {
		return nil, Fail(409, "author_task_stale", "维护对象、版本、状态或有效期已变化。")
	}
	allowed := map[string]bool{}
	for _, field := range task.AllowedFields {
		allowed[field] = true
	}
	present := map[string]bool{"name": in.Name != nil, "biography": in.Biography != nil, "homepage": in.Homepage != nil, "aliases": in.Aliases != nil, "following": in.Following != nil}
	count := 0
	for field, exists := range present {
		if !exists {
			continue
		}
		count++
		if !allowed[field] {
			return nil, Fail(403, "field_scope_denied", "本任务未授权修改该字段。")
		}
	}
	if count == 0 {
		return nil, Fail(400, "changes_required", "必须提交授权范围内的具体字段变更。")
	}
	if !task.AllowClear {
		for _, value := range []*string{in.Name, in.Biography, in.Homepage} {
			if value != nil && strings.TrimSpace(*value) == "" {
				return nil, Fail(403, "clear_not_authorized", "任务未授权清空资料。")
			}
		}
		if in.Aliases != nil && len(*in.Aliases) == 0 {
			return nil, Fail(403, "clear_not_authorized", "任务未授权清空别名。")
		}
	}
	return &task, nil
}

func finishAuthorTask(ctx context.Context, q store.Querier, task *AuthorTask, revision int64) error {
	if task == nil {
		return nil
	}
	task.UsedUpdates++
	task.ExpectedRevision = revision
	if task.UsedUpdates >= task.MaxUpdates {
		task.State = "completed"
	}
	raw, e := store.Encode(task)
	if e != nil {
		return e
	}
	_, e = q.ExecContext(ctx, "UPDATE author_tasks SET state=?,data=? WHERE id=?", task.State, raw, task.ID)
	return e
}

// AuthorCandidateGrant binds candidate submission to an explicitly delegated
// author and block. The block's independent revision is checked by its use case.
func (s *Service) AuthorCandidateGrant(ctx context.Context, p model.Principal, authorID, block string) (*AuthorTask, error) {
	if p.Has("admin") {
		return nil, nil
	}
	if !p.Has("research") || p.AuthorTaskID == "" {
		return nil, Fail(403, "scope_denied", "缺少作者候选研究授权。")
	}
	task, e := s.authorTask(ctx, p.AuthorTaskID)
	if e != nil {
		return nil, e
	}
	author, e := s.Author(ctx, authorID)
	if e != nil {
		return nil, e
	}
	if task.AuthorID != authorID || task.ExpectedRevision != author.Revision || task.State != "authorized" || !task.ExpiresAt.After(time.Now()) || task.UsedUpdates >= task.MaxUpdates {
		return nil, Fail(409, "author_task_stale", "作者候选任务已失效。")
	}
	for _, allowed := range task.CandidateBlocks {
		if allowed == block {
			return &task, nil
		}
	}
	return nil, Fail(403, "block_scope_denied", "任务未授权研究此资料块。")
}

// ConsumeAuthorCandidate must use the same transaction as candidate persistence.
func ConsumeAuthorCandidate(ctx context.Context, q store.Querier, task *AuthorTask) error {
	if task == nil {
		return nil
	}
	return finishAuthorTask(ctx, q, task, task.ExpectedRevision)
}
