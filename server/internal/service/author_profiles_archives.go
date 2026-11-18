package service

import (
	"context"
	"encoding/json"
	"reflect"
	"time"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/store"
)

func archiveDeletedAuthorProfile(ctx context.Context, q store.Querier, authorID, archiveID string) error {
	_, err := archiveAuthorProfile(ctx, q, authorID, "", archiveID, "delete")
	return err
}

// A target's existing block includes an explicitly cleared block. Only absent
// blocks can inherit source data; the complete source remains in the archive.
func mergeAuthorProfiles(ctx context.Context, q store.Querier, sourceID, targetID, archiveID string) ([]string, error) {
	archive, err := archiveAuthorProfile(ctx, q, sourceID, targetID, archiveID, "merge")
	if err != nil {
		return nil, err
	}
	for _, source := range archive.Blocks {
		target, err := profileBlock(ctx, q, targetID, source.Block)
		if err != nil {
			return nil, err
		}
		if target.Revision != 0 {
			continue
		}
		source.AuthorID = targetID
		source.Revision = 1
		source.Source = "merged"
		source.CandidateID = ""
		now := time.Now().UTC()
		source.UpdatedAt = &now
		if err = saveProfileBlock(ctx, q, source); err != nil {
			return nil, err
		}
	}
	if archive.Subscription != nil {
		current, err := savedAuthorUpdates(ctx, q, targetID)
		if err != nil {
			return nil, err
		}
		if current.Revision == 0 {
			inherited := *archive.Subscription
			inherited.AuthorID = targetID
			inherited.Revision = 1
			account, err := profileBlock(ctx, q, targetID, "accounts")
			if err != nil {
				return nil, err
			}
			if profileString(account.Values.IdentityStatus) != "verified" {
				inherited.TrackUpdates = false
				inherited.CheckedAt = nil
				inherited.Items = []AuthorUpdateEntry{}
			}
			if err = saveAuthorUpdates(ctx, q, inherited); err != nil {
				return nil, err
			}
		}
	}
	return archive.Conflicts, nil
}

func archiveAuthorProfile(ctx context.Context, q store.Querier, authorID, targetID, archiveID, reason string) (ProfileArchive, error) {
	archive := ProfileArchive{ProfileArchiveSummary: ProfileArchiveSummary{ID: archiveID, AuthorID: authorID, TargetAuthorID: targetID, Reason: reason, CreatedAt: time.Now().UTC(), Conflicts: []string{}}, Blocks: []AuthorProfileBlock{}}
	author, err := store.Get[model.Author](ctx, q, "SELECT data FROM authors WHERE id=?", authorID)
	if err != nil {
		return archive, err
	}
	archive.AuthorName = author.Name
	subscription, err := savedAuthorUpdates(ctx, q, authorID)
	if err != nil {
		return archive, err
	}
	if subscription.Revision > 0 {
		archive.Subscription = &subscription
		if targetID != "" {
			target, err := savedAuthorUpdates(ctx, q, targetID)
			if err != nil {
				return archive, err
			}
			if target.Revision > 0 && (target.TrackUpdates != subscription.TrackUpdates || target.FeedURL != subscription.FeedURL) {
				archive.Conflicts = append(archive.Conflicts, "profile.subscription")
			}
		}
	}
	archive.Blocks, err = store.List[AuthorProfileBlock](ctx, q, "SELECT data FROM author_profile_blocks WHERE author_id=? ORDER BY block", authorID)
	if err != nil {
		return archive, err
	}
	archive.BlockCount = len(archive.Blocks)
	if err = q.QueryRowContext(ctx, "SELECT COUNT(*) FROM author_profile_candidates WHERE author_id=? AND archive_id=''", authorID).Scan(&archive.CandidateCount); err != nil {
		return archive, err
	}
	if targetID != "" {
		for _, source := range archive.Blocks {
			target, err := profileBlock(ctx, q, targetID, source.Block)
			if err != nil {
				return archive, err
			}
			if target.Revision > 0 && (!reflect.DeepEqual(target.Values, source.Values) || !reflect.DeepEqual(target.Evidence, source.Evidence) || !reflect.DeepEqual(target.Avatar, source.Avatar)) {
				archive.Conflicts = append(archive.Conflicts, "profile."+source.Block)
			}
		}
	}
	if err = saveProfileArchive(ctx, q, archive); err != nil {
		return archive, err
	}
	// JSON updates preserve every candidate field without an unbounded in-memory read.
	_, err = q.ExecContext(ctx, "UPDATE author_profile_candidates SET archive_id=?,data=json_set(data,'$.archiveId',?) WHERE author_id=? AND archive_id=''", archiveID, archiveID, authorID)
	return archive, err
}

func saveProfileArchive(ctx context.Context, q store.Querier, archive ProfileArchive) error {
	raw, err := store.Encode(archive)
	if err != nil {
		return err
	}
	_, err = q.ExecContext(ctx, "INSERT INTO author_profile_archives(id,author_id,target_author_id,reason,created_at,data) VALUES(?,?,?,?,?,?)", archive.ID, archive.AuthorID, archive.TargetAuthorID, archive.Reason, dateKey(archive.CreatedAt), raw)
	if err != nil {
		return err
	}
	for _, block := range archive.Blocks {
		if block.Avatar == nil {
			continue
		}
		if _, err = q.ExecContext(ctx, "INSERT OR IGNORE INTO author_profile_archive_avatars(archive_id,avatar_id) VALUES(?,?)", archive.ID, block.Avatar.ID); err != nil {
			return err
		}
	}
	return nil
}

// PreserveImportedAuthorProfile keeps opaque imported evidence recoverable. It
// does not infer old field meanings or turn external text into confirmed data.
func PreserveImportedAuthorProfile(ctx context.Context, q store.Querier, authorID, name, archiveID string, raw json.RawMessage) error {
	var original map[string]json.RawMessage
	if len(raw) > 1<<20 || json.Unmarshal(raw, &original) != nil || original == nil {
		return Fail(400, "invalid_imported_profile", "导入资料必须是有界 JSON 对象。")
	}
	archive := ProfileArchive{ProfileArchiveSummary: ProfileArchiveSummary{ID: archiveID, AuthorID: authorID, AuthorName: name, Reason: "import", CreatedAt: time.Now().UTC(), Conflicts: []string{}}, Blocks: []AuthorProfileBlock{}, OriginalImport: raw}
	return saveProfileArchive(ctx, q, archive)
}

func (s *Service) AuthorProfileArchives(ctx context.Context, authorID, cursor string, limit int) (model.Page[ProfileArchiveSummary], error) {
	page := model.Page[ProfileArchiveSummary]{Items: []ProfileArchiveSummary{}}
	n, err := limited(limit, 20)
	if err != nil {
		return page, err
	}
	filter := "profile-archives:" + authorID
	seek, err := readSeek(cursor, filter, "created_desc")
	if err != nil {
		return page, err
	}
	query := "SELECT json_remove(data,'$.blocks','$.originalImport') FROM author_profile_archives WHERE (author_id=? OR target_author_id=?)"
	args := []any{authorID, authorID}
	if seek.ID != "" {
		query += " AND (created_at<? OR (created_at=? AND id<?))"
		args = append(args, seek.Value, seek.Value, seek.ID)
	}
	query += " ORDER BY created_at DESC,id DESC LIMIT ?"
	args = append(args, n+1)
	page.Items, err = store.List[ProfileArchiveSummary](ctx, s.DB.DB, query, args...)
	if err != nil {
		return page, err
	}
	if len(page.Items) > n {
		last := page.Items[n-1]
		page.Items = page.Items[:n]
		page.NextCursor = makeSeek(filter, "created_desc", dateKey(last.CreatedAt), last.ID)
	}
	return page, nil
}

func (s *Service) AuthorProfileArchive(ctx context.Context, authorID, id string) (ProfileArchive, error) {
	archive, err := store.Get[ProfileArchive](ctx, s.DB.DB, "SELECT data FROM author_profile_archives WHERE id=? AND (author_id=? OR target_author_id=?)", id, authorID, authorID)
	return archive, Normalize(err)
}
