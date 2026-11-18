package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/researchprovider"
	"qimeng.local/next/server/internal/store"
)

func validateResearchProviders(values []string, allowed bool, limit int) error {
	if len(values) > 0 && (!allowed || limit <= 0) {
		return Fail(400, "research_provider_permission_required", "选择公开来源须同时允许研究，并授予正数研究预算。")
	}
	seen := map[string]bool{}
	for _, v := range values {
		if !researchprovider.Known(v) || seen[v] {
			return Fail(400, "invalid_research_provider", "研究来源未知或重复。")
		}
		seen[v] = true
	}
	return nil
}

type providerGrant struct {
	taskID, kind, subjectID, subjectHash, libraryID string
	revision, version                               int64
	knownAuthor                                     bool
	allowed                                         []string
	researchLeft                                    int
	callsLeft                                       int
	deadline                                        time.Time
	batch                                           *model.Batch
}

func activeProviderTaskID(p model.Principal) string {
	for _, id := range []string{p.BatchID, p.AuthorTaskID, p.AssetTaskID} {
		if id != "" {
			return id
		}
	}
	return ""
}

// A restored intake batch may be claimed under a different delegation. The
// task ID alone cannot establish who issued a lookup before the restore.
type storedPublicResearchResult struct {
	PublicResearchResult
	CredentialID string `json:"receiptCredentialId"`
	DelegationID string `json:"receiptDelegationId"`
}

func (r storedPublicResearchResult) belongsTo(p model.Principal) bool {
	return r.TaskID == activeProviderTaskID(p) && r.CredentialID == p.ID && r.DelegationID == p.ParentDelegationID && r.CredentialID != "" && r.DelegationID != ""
}

func (s *Service) providerCredential(ctx context.Context, p model.Principal) (providerGrant, error) {
	g := providerGrant{taskID: activeProviderTaskID(p)}
	if p.ParentDelegationID == "" || g.taskID == "" || p.Has("admin") || !p.Has("research") {
		return g, Fail(403, "research_task_required", "外部研究需要首次授权所签出的当前任务凭据。")
	}
	var current model.Principal
	var parent Delegation
	var credential, delegation, providers, deadline string
	var used, limit int
	err := s.DB.DB.QueryRowContext(ctx, `SELECT c.data,d.data,t.kind,t.research_providers,t.provider_calls,t.provider_limit,
	 t.max_calls-t.calls,t.deadline FROM delegated_tasks t JOIN credentials c ON c.id=t.credential_id
	 JOIN ai_delegations d ON d.id=t.delegation_id WHERE t.task_id=? AND c.id=? AND d.id=?`, g.taskID, p.ID, p.ParentDelegationID).
		Scan(&credential, &delegation, &g.kind, &providers, &used, &limit, &g.callsLeft, &deadline)
	if errors.Is(err, sql.ErrNoRows) {
		return g, Fail(403, "research_task_revoked", "公开研究任务凭据已撤销或不属于此任务。")
	}
	if err != nil {
		return g, err
	}
	if err = json.Unmarshal([]byte(credential), &current); err != nil {
		return g, err
	}
	if err = json.Unmarshal([]byte(delegation), &parent); err != nil {
		return g, err
	}
	if err = json.Unmarshal([]byte(providers), &g.allowed); err != nil {
		return g, err
	}
	g.deadline, err = time.Parse(time.RFC3339Nano, deadline)
	if err != nil {
		return g, err
	}
	if current.Generation != p.Generation || !current.Has("research") || current.ParentDelegationID != parent.ID || activeProviderTaskID(current) != g.taskID {
		return g, Fail(401, "lease_changed", "任务已由新执行器接续。")
	}
	if !parent.CanResearch || parent.State != "active" || !current.ExpiresAt.After(time.Now()) || !parent.ExpiresAt.After(time.Now()) {
		return g, Fail(403, "research_task_inactive", "公开研究授权或任务时限已结束。")
	}
	if current.ExpiresAt.Before(g.deadline) {
		g.deadline = current.ExpiresAt
	}
	if parent.ExpiresAt.Before(g.deadline) {
		g.deadline = parent.ExpiresAt
	}
	g.allowed = slices.DeleteFunc(g.allowed, func(name string) bool { return !containsString(parent.ResearchProviders, name) })
	g.researchLeft = limit - used
	return g, nil
}

func (s *Service) providerGrant(ctx context.Context, p model.Principal) (providerGrant, error) {
	g, err := s.providerCredential(ctx, p)
	if err != nil {
		return g, err
	}
	return s.activeProviderSubject(ctx, p, g)
}

// Only a new external call or an in-flight result needs a still-active subject.
// A current task credential can reconcile its own prior receipts after closure.
func (s *Service) activeProviderSubject(ctx context.Context, p model.Principal, g providerGrant) (providerGrant, error) {
	if !g.deadline.After(time.Now()) {
		return g, Fail(403, "research_task_inactive", "公开研究任务时限已结束。")
	}
	switch g.kind {
	case "intake":
		b, err := s.Batch(ctx, g.taskID)
		if err != nil {
			return g, err
		}
		if b.State == "completed" || b.State == "cancelled" {
			return g, Fail(409, "batch_closed", "接收批次已结束。")
		}
		l, err := s.library(ctx, b.LibraryID)
		if err != nil {
			return g, err
		}
		if !l.Enabled {
			return g, Fail(409, "library_disabled", "媒体库已停用。")
		}
		g.researchLeft = min(g.researchLeft, b.MaxResearchCalls-b.ResearchCalls)
		g.batch = &b
		g.libraryID = b.LibraryID
	case "asset":
		t, a, err := s.activeAssetTask(ctx, p, g.taskID)
		if err != nil {
			return g, err
		}
		if !t.Research {
			return g, Fail(403, "research_task_required", "作品任务未授权研究。")
		}
		if t.ExpectedRevision != a.Revision {
			return g, Conflict(a.Revision)
		}
		g.subjectID, g.subjectHash, g.revision, g.version, g.libraryID = a.ID, a.Hash, a.Revision, a.ContentVersion, a.LibraryID
		g.knownAuthor = a.AuthorID != "" && a.AuthorID != "unknown"
	case "author":
		t, err := s.authorTask(ctx, g.taskID)
		if err != nil {
			return g, err
		}
		if t.State != "authorized" || !t.ExpiresAt.After(time.Now()) {
			return g, Fail(409, "author_task_stale", "作者任务已结束。")
		}
		a, err := s.Author(ctx, t.AuthorID)
		if err != nil {
			return g, err
		}
		if a.Revision != t.ExpectedRevision {
			return g, Conflict(a.Revision)
		}
		g.subjectID, g.revision = a.ID, a.Revision
	default:
		return g, Fail(403, "research_task_required", "无可用的公开研究任务。")
	}
	return g, nil
}
func (s *Service) publicResearchSubject(ctx context.Context, p model.Principal, g *providerGrant, in PublicResearchInput) error {
	authorLookup := in.Operation == "author" || in.Operation == "artist"
	if g.kind != "intake" {
		if in.UploadID != "" || in.ExpectedHash != "" {
			return Fail(400, "research_subject_mismatch", "此任务已经绑定单一作者或作品。")
		}
		if authorLookup && g.knownAuthor {
			return Fail(409, "research_author_known", "作品已有选定作者，请用对应作者的资料维护任务查证其资料。")
		}
		return nil
	}
	if in.UploadID == "" || in.ExpectedHash == "" {
		return Fail(400, "research_input_required", "批次查证须指定原始上传和已核验 SHA-256。")
	}
	u, err := s.Upload(ctx, p, in.UploadID)
	if err != nil {
		return err
	}
	if u.BatchID != g.taskID || u.State != "staged" || u.Hash != in.ExpectedHash {
		return Fail(409, "research_input_changed", "查询不属于本任务的当前待整理输入。")
	}
	if authorLookup && u.AuthorID != "" && u.AuthorID != "unknown" {
		return Fail(409, "research_author_known", "此上传已有选定作者，请复用现有身份；查证作者资料须使用对应的作者任务。")
	}
	g.subjectID, g.subjectHash = u.ID, u.Hash
	return nil
}
func (s *Service) ResearchProviderContext(ctx context.Context, p model.Principal) (PublicResearchContext, error) {
	v := PublicResearchContext{Enabled: s.Config.ResearchEnabled, Providers: researchprovider.Matrix(), AllowedProviders: []string{}}
	if p.ParentDelegationID == "" {
		return v, nil
	}
	if p.Has("delegate") {
		d, e := s.CurrentDelegation(ctx, p)
		if e != nil {
			return v, e
		}
		v.AllowedProviders = append(v.AllowedProviders, d.ResearchProviders...)
		return v, nil
	}
	if !p.Has("research") {
		return v, nil
	}
	g, e := s.providerGrant(ctx, p)
	if e != nil {
		return v, e
	}
	v.TaskID = g.taskID
	v.AllowedProviders = append(v.AllowedProviders, g.allowed...)
	v.RemainingResearchCalls = &g.researchLeft
	rows, e := s.DB.DB.QueryContext(ctx, `SELECT id,subject_id,provider,json_extract(data,'$.operation'),state FROM research_provider_calls WHERE task_id=? AND json_extract(data,'$.receiptCredentialId')=? AND json_extract(data,'$.receiptDelegationId')=? ORDER BY started_at DESC,id DESC LIMIT 100`, g.taskID, p.ID, p.ParentDelegationID)
	if e != nil {
		return v, e
	}
	defer rows.Close()
	for rows.Next() {
		var item PublicResearchRef
		if e = rows.Scan(&item.ID, &item.SubjectID, &item.Provider, &item.Operation, &item.State); e != nil {
			return v, e
		}
		v.Queries = append(v.Queries, item)
	}
	return v, rows.Err()
}
func (s *Service) PublicResearchResult(ctx context.Context, p model.Principal, id string) (PublicResearchResult, error) {
	v, e := store.Get[storedPublicResearchResult](ctx, s.DB.DB, "SELECT data FROM research_provider_calls WHERE id=?", id)
	if e != nil {
		return PublicResearchResult{}, Normalize(e)
	}
	if p.Has("admin") {
		return v.PublicResearchResult, nil
	}
	if _, err := s.providerCredential(ctx, p); err != nil {
		return PublicResearchResult{}, err
	}
	if !v.belongsTo(p) {
		return PublicResearchResult{}, Fail(403, "research_task_required", "查询回执只属于绑定任务。")
	}
	return v.PublicResearchResult, nil
}
func (s *Service) checkProviderEvidence(ctx context.Context, batch string, u model.Upload, in model.Research) error {
	if len(in.ProviderQueryIDs) == 0 {
		return nil
	}
	if len(in.ProviderQueryIDs) > 20 {
		return Fail(400, "invalid_research_receipts", "每次最多关联20个查询回执。")
	}
	if in.Outcome == "candidates" && len(in.Candidates) == 0 {
		return Fail(400, "invalid_evidence", "候选结果须包含实际候选和出处。")
	}
	seen := map[string]bool{}
	urls := map[string]bool{}
	for _, id := range in.ProviderQueryIDs {
		if seen[id] {
			return Fail(400, "invalid_research_receipts", "重复的研究查询回执。")
		}
		seen[id] = true
		v, e := store.Get[PublicResearchResult](ctx, s.DB.DB, `SELECT data FROM research_provider_calls WHERE id=? AND task_id=? AND subject_id=? AND subject_hash=? AND state!='pending'`, id, batch, u.ID, u.Hash)
		if errors.Is(e, sql.ErrNoRows) {
			return Fail(409, "research_evidence_mismatch", "研究回执不属于这个输入版本或尚未结束。")
		}
		if e != nil {
			return e
		}
		if v.State == "ok" {
			for _, item := range v.Items {
				urls[item.URL] = true
				for _, source := range item.Sources {
					urls[source] = true
				}
			}
		}
	}
	if in.CallsUsed == 0 {
		for _, candidate := range in.Candidates {
			if !urls[candidate.SourceURL] {
				return Fail(409, "research_evidence_mismatch", "候选出处须来自本次引用的实际查询结果。")
			}
		}
	}
	return nil
}
