package service

import (
	"context"
	"time"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/store"
)

type ReadyIntakeBatch struct {
	BatchID             string `json:"batchId"`
	LibraryID           string `json:"libraryId"`
	ManifestFingerprint string `json:"manifestFingerprint"`
	MaxResearchCalls    int    `json:"maxResearchCalls"`
}

type ReadyIntakeBatches struct {
	Items []ReadyIntakeBatch `json:"items"`
}

const readyIntakeList = `SELECT b.batch_id,b.library_id,b.manifest_fingerprint,b.max_research_calls
 FROM json_each(?) allowed CROSS JOIN ready_intake_batches b
 WHERE b.library_id=allowed.value AND b.max_research_calls<=? ORDER BY b.created_at,b.batch_id LIMIT 16`

func (s *Service) checkDailyTaskLimit(ctx context.Context, d Delegation, day string) error {
	var count int
	if err := s.DB.DB.QueryRowContext(ctx, "SELECT count(*) FROM delegated_tasks WHERE delegation_id=? AND created_day=?", d.ID, day).Scan(&count); err != nil {
		return err
	}
	if count >= d.MaxTasksPerDay {
		return Fail(429, "delegation_daily_limit", "已达到首次配置的每日任务上限。")
	}
	return nil
}

func (s *Service) ReadyIntakeBatches(ctx context.Context, p model.Principal) (ReadyIntakeBatches, error) {
	page := ReadyIntakeBatches{Items: []ReadyIntakeBatch{}}
	d, err := s.CurrentDelegation(ctx, p)
	if err != nil {
		return page, err
	}
	if !d.CanIngest || !d.CanOrganize {
		return page, Fail(403, "organize_scope_denied", "发现待处理智能批次需要长期授权中的接收和整理权限。")
	}
	if err = s.checkDailyTaskLimit(ctx, d, time.Now().Format("2006-01-02")); err != nil {
		return page, err
	}
	libs, err := store.Encode(d.LibraryIDs)
	if err != nil {
		return page, err
	}
	rows, err := s.DB.DB.QueryContext(ctx, readyIntakeList, libs, d.MaxResearchCalls)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		var item ReadyIntakeBatch
		if err = rows.Scan(&item.BatchID, &item.LibraryID, &item.ManifestFingerprint, &item.MaxResearchCalls); err != nil {
			return page, err
		}
		page.Items = append(page.Items, item)
	}
	return page, rows.Err()
}
