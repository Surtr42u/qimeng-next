package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"qimeng.local/next/server/internal/model"
)

// ReplayBeforeInspection checks a completed, principal-bound operation before
// acquiring media verification resources. A miss does not reserve a key.
// Run checks the same receipt again under the mutation lock before committing.
func (s *Service) ReplayBeforeInspection(ctx context.Context, p model.Principal, scope, operation, key string, input []byte) (Result, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, err := s.currentWritePrincipal(ctx, p)
	if err != nil {
		return Result{}, false, err
	}
	if scope != "" {
		if err = Authorize(current, scope, ""); err != nil {
			return Result{}, false, err
		}
	}
	return s.matchingReceipt(ctx, current.ID, operation, key, digest(input))
}

func (s *Service) matchingReceipt(ctx context.Context, principalID, operation, key, hash string) (Result, bool, error) {
	if key == "" {
		return Result{}, false, nil
	}
	if len(key) < 8 || len(key) > 128 {
		return Result{}, false, Fail(400, "invalid_idempotency_key", "幂等键长度应为 8–128。")
	}
	var previous string
	var result Result
	err := s.DB.DB.QueryRowContext(ctx, "SELECT input_hash,response,status FROM idempotency WHERE principal_id=? AND operation=? AND key=?",
		principalID, operation, key).Scan(&previous, &result.Body, &result.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return Result{}, false, nil
	}
	if err != nil {
		return Result{}, false, err
	}
	if previous != hash {
		return Result{}, false, Fail(409, "idempotency_conflict", "同一幂等键的输入已经改变。")
	}
	return result, true, nil
}

// Operation never exposes another principal's receipt. A key may be reused for
// distinct operations; callers must select the operation rather than guess.
func (s *Service) Operation(ctx context.Context, p model.Principal, key, operation string) (map[string]any, error) {
	var research *PublicResearchResult
	providerOperation := operation == publicResearchOperation || operation == imageResearchOperation
	if providerOperation || operation == "" && p.ParentDelegationID != "" && p.Has("research") && activeProviderTaskID(p) != "" {
		var err error
		research, err = s.researchOperationByKey(ctx, p, key)
		if err != nil {
			return nil, err
		}
		if providerOperation {
			if research != nil && research.receiptOperation() != operation {
				research = nil
			}
			return researchOperationResult(key, research)
		}
	}
	rows, e := s.DB.DB.QueryContext(ctx, "SELECT operation,response,status FROM idempotency WHERE principal_id=? AND key=? AND (?='' OR operation=?) ORDER BY operation LIMIT 2", p.ID, key, operation, operation)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	type receipt struct {
		op     string
		body   []byte
		status int
	}
	items := []receipt{}
	for rows.Next() {
		var v receipt
		if e = rows.Scan(&v.op, &v.body, &v.status); e != nil {
			return nil, e
		}
		items = append(items, v)
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	if len(items) == 0 {
		return researchOperationResult(key, research)
	}
	if len(items) > 1 || research != nil {
		return nil, Fail(409, "operation_ambiguous", "该键用于多个操作，请同时指定 operation。")
	}
	v := items[0]
	var body any
	if len(v.body) > 0 {
		if e = json.Unmarshal(v.body, &body); e != nil {
			return nil, e
		}
	}
	return map[string]any{"state": "committed", "operation": v.op, "idempotencyKey": key, "status": v.status, "response": body}, nil
}
