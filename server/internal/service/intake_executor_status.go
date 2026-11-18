package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/store"
)

type BatchExecutorReport struct {
	Generation             int64  `json:"generation"`
	BatchRevision          int64  `json:"batchRevision"`
	ExpectedStatusRevision int64  `json:"expectedStatusRevision"`
	State                  string `json:"state"`
	Reason                 string `json:"reason,omitempty"`
}

// A lease proves a claim, not a live host. Only a current worker report
// carries an observation time or a reason. Polling this projection is read-only.
func (s *Service) batchExecutorStatus(ctx context.Context, q store.Querier, b model.Batch) (*model.BatchExecutorStatus, error) {
	if b.Mode != "intelligent" || b.State == "completed" || b.State == "cancelled" {
		return nil, nil
	}
	if b.State == "partial" {
		var unfinished bool
		err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM batch_inputs i LEFT JOIN uploads u ON u.id=i.upload_id
 WHERE i.batch_id=? AND i.cancelled=0 AND i.rejection_code='' AND (i.upload_id IS NULL OR u.state IN ('receiving','staged')))`, b.ID).Scan(&unfinished)
		if err != nil || !unfinished {
			return nil, err
		}
	}
	status := model.BatchExecutorStatus{BatchRevision: b.Revision}
	var credential, previous, grantState, grantExpiry string
	err := q.QueryRowContext(ctx, `SELECT coalesce(c.data,''),coalesce(o.data,''),d.state,d.expires_at
 FROM delegated_tasks t JOIN ai_delegations d ON d.id=t.delegation_id LEFT JOIN credentials c ON c.id=t.credential_id
 LEFT JOIN intake_executor_observations o ON o.task_id=t.task_id WHERE t.task_id=? AND t.kind='intake'`, b.ID).
		Scan(&credential, &previous, &grantState, &grantExpiry)
	if errors.Is(err, sql.ErrNoRows) {
		var configured, ready bool
		err = q.QueryRowContext(ctx, `SELECT ? OR EXISTS(SELECT 1 FROM ai_delegations d
 WHERE d.state='active' AND julianday(d.expires_at)>julianday('now')
 AND json_extract(d.data,'$.canIngest')=1 AND json_extract(d.data,'$.canOrganize')=1
 AND EXISTS(SELECT 1 FROM json_each(d.data,'$.libraryIds') WHERE value=?)),
 EXISTS(SELECT 1 FROM ready_intake_batches WHERE batch_id=?)`, s.Config.AIConfigured, b.LibraryID, b.ID).Scan(&configured, &ready)
		if err != nil {
			return nil, err
		}
		switch {
		case !configured:
			status.State, status.Source = "not_configured", "deployment"
		case ready:
			status.State, status.Source = "awaiting_claim", "queue"
		default:
			status.State, status.Source = "not_ready", "batch"
		}
		return &status, nil
	}
	if err != nil {
		return nil, err
	}
	var last model.BatchExecutorStatus
	if previous != "" {
		if err := json.Unmarshal([]byte(previous), &last); err != nil {
			return nil, err
		}
		status.Revision = last.Revision
	}
	grantEnd, err := time.Parse(time.RFC3339Nano, grantExpiry)
	if err != nil {
		return nil, err
	}
	var principal model.Principal
	if credential != "" {
		if err := json.Unmarshal([]byte(credential), &principal); err != nil {
			return nil, err
		}
	}
	status.Source, status.Generation = "lease", principal.Generation
	now := time.Now()
	if grantState != "active" || !grantEnd.After(now) || !principal.ExpiresAt.After(now) {
		status.State = "lease_inactive"
	} else if previous != "" && last.Generation == principal.Generation && last.BatchRevision == b.Revision {
		return &last, nil
	} else {
		status.State = "claimed"
	}
	return &status, nil
}

func (s *Service) ReportIntakeExecutorStatus(ctx context.Context, w *Write, id string, in BatchExecutorReport) (*model.BatchExecutorStatus, error) {
	p := w.Principal
	if p.ParentDelegationID == "" || p.BatchID != id || !p.Has("ingest") || !p.Has("organize") {
		return nil, Fail(403, "executor_task_scope_denied", "只有已认领该智能批次的受限执行器可报告停等状态。")
	}
	reasons := []string{"host_unavailable", "permission_required", "budget_exhausted", "needs_attention", "operation_uncertain"}
	if (in.State != "paused" || !slices.Contains(reasons, in.Reason)) && (in.State != "claimed" || in.Reason != "") {
		return nil, Fail(400, "invalid_executor_report", "暂停时须给出已声明的原因，恢复时省略原因。")
	}
	if p.Generation != in.Generation {
		return nil, Fail(409, "lease_changed", "执行租约已换代，请回读当前状态。")
	}
	q, err := w.Q()
	if err != nil {
		return nil, err
	}
	var bound bool
	if err = q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM delegated_tasks WHERE task_id=? AND delegation_id=?
 AND credential_id=? AND kind='intake')`, id, p.ParentDelegationID, p.ID).Scan(&bound); err != nil {
		return nil, err
	}
	if !bound {
		return nil, Fail(403, "executor_task_scope_denied", "当前凭据未绑定该批次的执行租约。")
	}
	b, err := store.Get[model.Batch](ctx, q, "SELECT data FROM batches WHERE id=?", id)
	if err != nil {
		return nil, Normalize(err)
	}
	if b.Revision != in.BatchRevision {
		return nil, Conflict(b.Revision)
	}
	current, err := s.batchExecutorStatus(ctx, q, b)
	if err != nil {
		return nil, err
	}
	if current == nil || current.State == "lease_inactive" || current.Generation != p.Generation {
		return nil, Fail(409, "executor_task_inactive", "批次已结束或当前租约失效。")
	}
	if current.Revision != in.ExpectedStatusRevision {
		return nil, Fail(409, "executor_status_changed", "执行器的停等状态已经改变，请回读批次。")
	}
	if current.State == in.State && current.Reason == in.Reason {
		return nil, Fail(409, "executor_status_unchanged", "已经是此状态，请沿原回执读取。")
	}
	now := time.Now().UTC()
	status := &model.BatchExecutorStatus{State: in.State, Source: "worker", Revision: current.Revision + 1,
		BatchRevision: b.Revision, Generation: p.Generation, Reason: in.Reason, ReportedAt: &now}
	raw, err := store.Encode(status)
	if err != nil {
		return nil, err
	}
	_, err = q.ExecContext(ctx, `INSERT INTO intake_executor_observations(task_id,data) VALUES(?,?)
 ON CONFLICT(task_id) DO UPDATE SET data=excluded.data`, id, raw)
	return status, err
}
