package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/researchprovider"
	"qimeng.local/next/server/internal/store"
)

const publicResearchOperation = "queryResearchProvider"
const imageResearchOperation = "lookupResearchImage"

func (r PublicResearchResult) receiptOperation() string {
	if r.Provider == "lens" {
		return imageResearchOperation
	}
	return publicResearchOperation
}

func (s *Service) researchOperationByKey(ctx context.Context, p model.Principal, key string) (*PublicResearchResult, error) {
	g, err := s.providerCredential(ctx, p)
	if err != nil {
		return nil, err
	}
	receipt, err := store.Get[storedPublicResearchResult](ctx, s.DB.DB,
		"SELECT data FROM research_provider_calls WHERE task_id=? AND request_key=?", g.taskID, digest([]byte(key)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !receipt.belongsTo(p) {
		return nil, nil
	}
	return &receipt.PublicResearchResult, nil
}

func researchOperationResult(key string, receipt *PublicResearchResult) (map[string]any, error) {
	if receipt == nil {
		return map[string]any{"state": "not_recorded", "idempotencyKey": key}, nil
	}
	if receipt.State == "pending" {
		return nil, Fail(409, "research_query_pending", "公开查询正在处理，尚无终态回执。")
	}
	return map[string]any{"state": "committed", "operation": receipt.receiptOperation(), "idempotencyKey": key, "status": 200, "response": receipt}, nil
}

func (s *Service) QueryResearchProvider(ctx context.Context, p model.Principal, name, key string, in PublicResearchInput) (Result, error) {
	if !s.Config.ResearchEnabled {
		return Result{}, Fail(403, "research_provider_disabled", "此实例未开启外部公开研究，查询未发出。")
	}
	lookup, err := researchprovider.Build(name, in.Operation, in.Target, in.Limit)
	if err != nil {
		return Result{}, Fail(400, "invalid_research_query", "来源、操作、公开标识或查询上限无效。")
	}
	in.Target = lookup.Target
	in.Limit = lookup.Limit
	return s.runResearchLookup(ctx, p, key, researchLookup{provider: lookup, subject: in}, in)
}

type researchLookup struct {
	provider researchprovider.Query
	subject  PublicResearchInput
	image    *ResearchImageLookup
}

func (r researchLookup) allowed(g providerGrant) bool {
	if r.image != nil {
		return g.imageLookup
	}
	return containsString(g.allowed, r.provider.Name)
}

func (s *Service) bindResearchLookup(ctx context.Context, p model.Principal, g *providerGrant, lookup researchLookup) error {
	if lookup.image != nil {
		i := lookup.image
		if g.kind == "asset" && (i.AssetID != g.subjectID || i.ContentVersion != g.version) || g.kind == "intake" && i.UploadID == "" {
			return Fail(409, "research_subject_mismatch", "帧所指的上传或作品内容版本不属于当前任务。")
		}
	}
	return s.publicResearchSubject(ctx, p, g, lookup.subject)
}

func (s *Service) researchAdmission(ctx context.Context, p model.Principal, lookup researchLookup, keyHash, fingerprint string) (providerGrant, *PublicResearchResult, error) {
	g, err := s.providerCredential(ctx, p)
	if err != nil {
		return g, nil, err
	}
	var oldHash, raw string
	err = s.DB.DB.QueryRowContext(ctx, "SELECT input_hash,data FROM research_provider_calls WHERE task_id=? AND request_key=?", g.taskID, keyHash).Scan(&oldHash, &raw)
	if err == nil {
		var previous storedPublicResearchResult
		if err = json.Unmarshal([]byte(raw), &previous); err != nil {
			return g, nil, err
		}
		if !previous.belongsTo(p) {
			return g, nil, Fail(409, "research_receipt_from_previous_grant", "此键已属于早先授权的查询，当前凭据不能复用。")
		}
		if oldHash != fingerprint {
			return g, nil, Fail(409, "idempotency_conflict", "此查询身份已用于不同内容。")
		}
		return g, &previous.PublicResearchResult, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return g, nil, err
	}
	if lookup.image != nil && s.researchGateway.lens == nil {
		return g, nil, Fail(403, "image_lookup_disabled", "管理员尚未配置单帧查证服务。")
	}
	if !lookup.allowed(g) {
		return g, nil, Fail(403, "research_provider_denied", "当前任务未明确获准将查询发给此来源。")
	}
	if g, err = s.activeProviderSubject(ctx, p, g); err != nil {
		return g, nil, err
	}
	if err = s.bindResearchLookup(ctx, p, &g, lookup); err != nil {
		return g, nil, err
	}
	if g.researchLeft <= 0 || g.callsLeft <= 0 {
		return g, nil, Fail(429, "research_budget_exhausted", "此任务的研究或总调用额度已用尽。")
	}
	return g, nil, nil
}

func (r PublicResearchResult) statusCode() int {
	if r.State == "pending" {
		return 202
	}
	return 200
}

// Check the receipt and task before opening a derived frame. Admission runs
// again under the commit lock after the cache I/O, before charging or sending.
func (s *Service) inspectImageLookup(ctx context.Context, p model.Principal, lookup researchLookup, keyHash, fingerprint string) (*PublicResearchResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.currentWritePrincipal(ctx, p)
	if err != nil {
		return nil, err
	}
	_, previous, err := s.researchAdmission(ctx, p, lookup, keyHash, fingerprint)
	return previous, err
}

func (s *Service) runResearchLookup(ctx context.Context, p model.Principal, key string, lookup researchLookup, body any) (Result, error) {
	if len(key) < 8 || len(key) > 128 {
		return Result{}, Fail(400, "idempotency_required", "公开查证须使用稳定的查询身份。")
	}
	input, err := json.Marshal(body)
	if err != nil {
		return Result{}, err
	}
	name := lookup.provider.Name
	fingerprint := digest(append([]byte(name+"\x00"), input...))
	keyHash := digest([]byte(key))
	var artifact ResearchArtifact
	var frame []byte
	if lookup.image != nil {
		previous, e := s.inspectImageLookup(ctx, p, lookup, keyHash, fingerprint)
		if e != nil {
			return Result{}, e
		}
		if previous != nil {
			raw, e := json.Marshal(previous)
			return Result{Body: raw, Status: previous.statusCode()}, e
		}
		select {
		case s.researchGateway.frameReads <- struct{}{}:
			defer func() { <-s.researchGateway.frameReads }()
		default:
			return Result{}, Fail(429, "research_provider_busy", "已有两次单帧查证在执行。")
		}
		artifact, frame, err = s.ResearchArtifact(ctx, p, lookup.image.FrameID)
		if err != nil {
			return Result{}, err
		}
		if !lookup.image.matches(artifact, frame) {
			return Result{}, Fail(409, "research_frame_mismatch", "衍生帧、摘要或来源版本不匹配。")
		}
	}
	var record PublicResearchResult
	var dispatched bool
	var outbound context.Context
	var cancel context.CancelFunc
	reserved, err := s.Run(ctx, p, "reservePublicResearch", "", nil, func(w *Write) (any, int, error) {
		g, previous, e := s.researchAdmission(ctx, w.Principal, lookup, keyHash, fingerprint)
		if e != nil {
			return nil, 0, e
		}
		if previous != nil {
			return previous, previous.statusCode(), nil
		}
		if lookup.image != nil && (artifact.SourceSHA256 != g.subjectHash || !artifact.ExpiresAt.After(time.Now())) {
			return nil, 0, Fail(409, "research_frame_mismatch", "查证帧已过期或其来源内容已变化。")
		}
		now := time.Now().UTC()
		var recent int
		if e = s.DB.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM research_provider_calls WHERE provider=? AND started_at>?", name, dateKey(now.Add(-time.Minute))).Scan(&recent); e != nil {
			return nil, 0, e
		}
		if recent >= 20 {
			return nil, 0, Fail(429, "research_provider_rate_limited", "此来源最近一分钟已发出20次查询。")
		}
		select {
		case s.researchGateway.slots <- struct{}{}:
		default:
			return nil, 0, Fail(429, "research_provider_busy", "已有两次外部查证在执行。")
		}
		dispatched = true
		record = PublicResearchResult{ID: ID(), TaskID: g.taskID, SubjectID: g.subjectID, SubjectHash: g.subjectHash, SubjectRevision: g.revision, ContentVersion: g.version, Provider: name, Operation: lookup.subject.Operation,
			State: "pending", EvidenceKind: lookup.provider.EvidenceKind, Items: []researchprovider.Item{}, StartedAt: now}
		timeout := 10 * time.Second
		if lookup.image != nil {
			record.FrameID, record.FrameSHA256 = artifact.ID, artifact.SHA256
			timeout = 95 * time.Second
		}
		if deadline := now.Add(timeout); deadline.Before(g.deadline) {
			g.deadline = deadline
		}
		outbound, cancel = context.WithDeadline(ctx, g.deadline)
		s.researchGateway.register(record.ID, providerFlight{credential: p.ID, delegation: p.ParentDelegationID, task: g.taskID, library: g.libraryID, stop: cancel})
		q, e := w.Q()
		if e != nil {
			return nil, 0, e
		}
		if _, e = q.ExecContext(ctx, "UPDATE delegated_tasks SET calls=calls+1,provider_calls=provider_calls+1 WHERE task_id=?", g.taskID); e != nil {
			return nil, 0, e
		}
		if b := g.batch; b != nil {
			b.ResearchCalls++
			b.Revision++
			b.UpdatedAt = now
			if e = store.SaveBatch(ctx, q, *b); e != nil {
				return nil, 0, e
			}
		}
		body, e := store.Encode(storedPublicResearchResult{record, p.ID, p.ParentDelegationID})
		if e != nil {
			return nil, 0, e
		}
		_, e = q.ExecContext(ctx, `INSERT INTO research_provider_calls(id,task_id,subject_id,subject_hash,provider,request_key,input_hash,state,started_at,data)
		 VALUES(?,?,?,?,?,?,?,'pending',?,?)`, record.ID, g.taskID, g.subjectID, g.subjectHash, name, keyHash, fingerprint, dateKey(now), body)
		return record, 202, e
	})
	if dispatched {
		defer func() { cancel(); s.researchGateway.finish(record.ID); <-s.researchGateway.slots }()
	}
	if err != nil || !dispatched {
		return reserved, err
	}
	stop := context.AfterFunc(s.workCtx, cancel)
	defer stop()
	if lookup.image != nil {
		record.State, record.Items, record.ImageHints = s.researchGateway.lens.lookup(outbound, frame, record.FrameSHA256)
	} else {
		record.State, record.Items = s.researchGateway.fetch(outbound, lookup.provider)
	}
	if record.Items == nil {
		record.Items = []researchprovider.Item{}
	}
	finishCtx, stopFinish := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopFinish()
	var accessErr error
	result, err := s.Run(finishCtx, syncOwner(), "finishPublicResearch", "", nil, func(w *Write) (any, int, error) {
		g, e := s.providerGrant(finishCtx, p)
		if e == nil && !lookup.allowed(g) {
			e = Fail(403, "research_provider_denied", "此来源授权已失效。")
		}
		if e == nil {
			e = s.bindResearchLookup(finishCtx, p, &g, lookup)
		}
		if e == nil && (record.SubjectHash != g.subjectHash || record.SubjectRevision != g.revision || record.ContentVersion != g.version) {
			e = Fail(409, "research_subject_changed", "查证期间对象已被修改，请重新核对。")
		}
		if e != nil {
			accessErr = e
			record.State = "interrupted"
			record.Items = []researchprovider.Item{}
			record.ImageHints = nil
		}
		now := time.Now().UTC()
		record.FinishedAt = &now
		raw, e := store.Encode(storedPublicResearchResult{record, p.ID, p.ParentDelegationID})
		if e != nil {
			return nil, 0, e
		}
		q, e := w.Q()
		if e != nil {
			return nil, 0, e
		}
		_, e = q.ExecContext(finishCtx, "UPDATE research_provider_calls SET state=?,data=? WHERE id=? AND state='pending'", record.State, raw, record.ID)
		return record, 200, e
	})
	if err != nil {
		return Result{}, err
	}
	if accessErr != nil {
		return Result{}, accessErr
	}
	return result, nil
}
