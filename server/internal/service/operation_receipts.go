package service

import (
	"context"
	"encoding/json"

	"qimeng.local/next/server/internal/model"
)

// Operation never exposes another principal's receipt. A key may be reused for
// distinct operations; callers must select the operation rather than guess.
func (s *Service) Operation(ctx context.Context, p model.Principal, key, operation string) (map[string]any, error) {
	var research *PublicResearchResult
	if operation == publicResearchOperation || operation == "" && p.ParentDelegationID != "" && p.Has("research") && activeProviderTaskID(p) != "" {
		var err error
		research, err = s.researchOperationByKey(ctx, p, key)
		if err != nil {
			return nil, err
		}
		if operation == publicResearchOperation {
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
