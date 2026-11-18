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
	return map[string]any{"state": "committed", "operation": publicResearchOperation, "idempotencyKey": key, "status": 200, "response": receipt}, nil
}

func (s *Service) QueryResearchProvider(ctx context.Context, p model.Principal, name, key string, in PublicResearchInput) (Result, error) {
	if !s.Config.ResearchEnabled {
		return Result{}, Fail(403, "research_provider_disabled", "此实例未开启外部公开研究，查询未发出。")
	}
	if len(key) < 8 || len(key) > 128 {
		return Result{}, Fail(400, "idempotency_required", "公开查证须使用稳定的查询身份。")
	}
	lookup, err := researchprovider.Build(name, in.Operation, in.Target, in.Limit)
	if err != nil {
		return Result{}, Fail(400, "invalid_research_query", "来源、操作、公开标识或查询上限无效。")
	}
	in.Target = lookup.Target
	in.Limit = lookup.Limit
	input, err := json.Marshal(in)
	if err != nil {
		return Result{}, err
	}
	fingerprint := digest(append([]byte(name+"\x00"), input...))
	keyHash := digest([]byte(key))
	var record PublicResearchResult
	var dispatched bool
	var outbound context.Context
	var cancel context.CancelFunc
	reserved, err := s.Run(ctx, p, "reservePublicResearch", "", nil, func(w *Write) (any, int, error) {
		g, e := s.providerCredential(ctx, w.Principal)
		if e != nil {
			return nil, 0, e
		}
		var oldHash, raw string
		e = s.DB.DB.QueryRowContext(ctx, "SELECT input_hash,data FROM research_provider_calls WHERE task_id=? AND request_key=?", g.taskID, keyHash).Scan(&oldHash, &raw)
		if e == nil {
			var previous storedPublicResearchResult
			if e = json.Unmarshal([]byte(raw), &previous); e != nil {
				return nil, 0, e
			}
			if !previous.belongsTo(w.Principal) {
				return nil, 0, Fail(409, "research_receipt_from_previous_grant", "此键已属于早先授权的查询，当前凭据不能复用。")
			}
			if oldHash != fingerprint {
				return nil, 0, Fail(409, "idempotency_conflict", "此查询身份已用于不同内容。")
			}
			record = previous.PublicResearchResult
			status := 200
			if record.State == "pending" {
				status = 202
			}
			return record, status, nil
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return nil, 0, e
		}
		if !containsString(g.allowed, name) {
			return nil, 0, Fail(403, "research_provider_denied", "当前任务未明确获准将查询发给此来源。")
		}
		g, e = s.activeProviderSubject(ctx, w.Principal, g)
		if e != nil {
			return nil, 0, e
		}
		if e = s.publicResearchSubject(ctx, w.Principal, &g, in); e != nil {
			return nil, 0, e
		}
		if g.researchLeft <= 0 || g.callsLeft <= 0 {
			return nil, 0, Fail(429, "research_budget_exhausted", "此任务的研究或总调用额度已用尽。")
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
		record = PublicResearchResult{ID: ID(), TaskID: g.taskID, SubjectID: g.subjectID, SubjectHash: g.subjectHash, SubjectRevision: g.revision, ContentVersion: g.version, Provider: name, Operation: in.Operation,
			State: "pending", EvidenceKind: lookup.EvidenceKind, Items: []researchprovider.Item{}, StartedAt: now}
		if deadline := now.Add(10 * time.Second); deadline.Before(g.deadline) {
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
	record.State, record.Items = s.researchGateway.fetch(outbound, lookup)
	if record.Items == nil {
		record.Items = []researchprovider.Item{}
	}
	finishCtx, stopFinish := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopFinish()
	var accessErr error
	result, err := s.Run(finishCtx, syncOwner(), "finishPublicResearch", "", nil, func(w *Write) (any, int, error) {
		g, e := s.providerGrant(finishCtx, p)
		if e == nil && !containsString(g.allowed, name) {
			e = Fail(403, "research_provider_denied", "此来源授权已失效。")
		}
		if e == nil {
			e = s.publicResearchSubject(finishCtx, p, &g, in)
		}
		if e == nil && (record.SubjectHash != g.subjectHash || record.SubjectRevision != g.revision || record.ContentVersion != g.version) {
			e = Fail(409, "research_subject_changed", "查证期间对象已被修改，请重新核对。")
		}
		if e != nil {
			accessErr = e
			record.State = "interrupted"
			record.Items = []researchprovider.Item{}
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
