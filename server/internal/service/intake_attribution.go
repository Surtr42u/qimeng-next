package service

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/store"
)

func (s *Service) checkPlanResearch(ctx context.Context, p model.Principal, u model.Upload, authors []model.Author) error {
	r, err := store.Get[model.Research](ctx, s.DB.DB, "SELECT data FROM research WHERE upload_id=?", u.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if r.Outcome == "conflict" {
		return &Error{Status: 409, Code: "research_conflict", Message: "此项证据冲突，须由用户解决后再准备计划。", Details: map[string]any{"uploadId": u.ID}}
	}
	if p.Has("admin") || authors[0].Placeholder || u.AuthorID != "" && u.AuthorID != "unknown" {
		return nil
	}
	unverified := &Error{Status: 409, Code: "research_author_unverified", Message: "此输入缺少支持所选实名作者的证据，可保留未知作者入库；其他输入可继续。", Details: map[string]any{"uploadId": u.ID}}
	if r.Outcome != "candidates" || r.UploadID != u.ID || r.ExpectedHash != u.Hash || len(r.Candidates) == 0 {
		return unverified
	}
	// When only already-charged gateway receipts are cited, an unverified
	// search/Lens link is not an independent source for confirming a creator.
	var records map[string]bool
	if r.CallsUsed == 0 {
		receipts, e := store.Encode(r.ProviderQueryIDs)
		if e != nil {
			return e
		}
		evidence, e := store.List[PublicResearchResult](ctx, s.DB.DB, `SELECT data FROM research_provider_calls WHERE id IN (SELECT value FROM json_each(?))
		 AND task_id=? AND subject_id=? AND subject_hash=? AND state='ok' AND json_extract(data,'$.evidenceKind')='platform_record'`, receipts, u.BatchID, u.ID, u.Hash)
		if e != nil {
			return e
		}
		records = map[string]bool{}
		for _, receipt := range evidence {
			for _, item := range receipt.Items {
				records[item.URL] = true
				for _, url := range item.Sources {
					records[url] = true
				}
			}
		}
	}
	supported := map[string]bool{}
	for _, candidate := range r.Candidates {
		if candidate.AuthorID != "" && validURL(candidate.SourceURL) && strings.TrimSpace(candidate.Evidence) != "" && (records == nil || records[candidate.SourceURL]) {
			supported[candidate.AuthorID] = true
		}
	}
	for _, author := range authors {
		if !supported[author.ID] {
			return unverified
		}
	}
	return nil
}
