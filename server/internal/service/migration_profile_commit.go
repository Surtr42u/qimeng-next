package service

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"qimeng.local/next/server/internal/store"
)

func verifyImportedBlockPlan(ctx context.Context, q store.Querier, author V2MigrationAuthor, profile *ImportedAuthorProfile) error {
	current, err := inspectImportedBlocks(ctx, q, author.AuthorID, profile)
	if err != nil {
		return err
	}
	want, err := json.Marshal(author.ProfileBlocks)
	if err != nil {
		return err
	}
	actual, err := json.Marshal(current)
	if err != nil {
		return err
	}
	if !bytes.Equal(want, actual) {
		return Fail(409, "migration_profile_changed", "预览后作者资料、头像或订阅已变化，请核对新计划。")
	}
	return nil
}

func replacementArchive(ctx context.Context, q store.Querier, author V2MigrationAuthor, id string, profile *ImportedAuthorProfile, now time.Time) (ProfileArchive, bool, error) {
	read := func(key string) (ProfileArchive, error) {
		return store.Get[ProfileArchive](ctx, q, "SELECT data FROM author_profile_archives WHERE id=?", key)
	}
	a, err := read(id)
	if err == nil && a.AuthorID != author.AuthorID {
		id = digest([]byte(id + ":target:" + author.AuthorID))[:32]
		a, err = read(id)
	}
	if err == nil {
		return a, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return a, false, err
	}
	a, err = archiveImportedProfile(ctx, q, author.AuthorID, author.Name, id, profile, now)
	return a, true, err
}

func applyImportedBlockPlan(ctx context.Context, q store.Querier, author V2MigrationAuthor, profile *ImportedAuthorProfile, archiveID string, plan *V2MigrationPlan) (bool, error) {
	now := time.Now().UTC()
	archive, fresh, err := replacementArchive(ctx, q, author, archiveID, profile, now)
	if err != nil {
		return false, err
	}
	var partial bool
	result := ProfileImportResult{}
	if len(author.ProfileBlocks) != len(archive.Blocks) {
		return false, Fail(409, "migration_profile_changed", "源资料归档与已确认计划不一致。")
	}
	for i, d := range author.ProfileBlocks {
		source := archive.Blocks[i]
		if source.Block != d.Block {
			return false, Fail(409, "migration_profile_changed", "源资料块与已确认计划不一致。")
		}
		current, err := profileBlock(ctx, q, author.AuthorID, d.Block)
		if err != nil {
			return false, err
		}
		switch d.Action {
		case "protected", "candidate":
			partial = true
			if fresh && author.AuthorID != "unknown" {
				if err := importProfileCandidate(ctx, q, author.AuthorID, archive.ID+":main", source, current, &result); err != nil {
					return false, err
				}
			}
			continue
		case "keep":
			continue
		}
		oldAvatar := current.Avatar
		if slices.Contains(d.Fields, "avatar") {
			if d.IncomingAvatarSHA256 != "" && (source.Avatar == nil || source.Avatar.ContentHash != d.IncomingAvatarSHA256) {
				return false, Fail(409, "migration_profile_changed", "离线头像与本次计划的内容摘要不一致。")
			}
			current.Avatar = source.Avatar
		}
		current.Revision++
		current.UpdatedAt = &now
		current.Source = "imported"
		current.CandidateID = ""
		current.Values = d.IncomingValues
		current.Evidence = append([]ProfileEvidence{}, d.IncomingEvidence...)
		if err := saveProfileBlock(ctx, q, current); err != nil {
			return false, err
		}
		if oldAvatar != nil && (current.Avatar == nil || oldAvatar.ID != current.Avatar.ID) {
			if err := pruneProfileAvatar(ctx, q, oldAvatar.ID); err != nil {
				return false, err
			}
		}
		if d.Action == "replace" {
			plan.Counts["profileBlocksReplaced"]++
		} else {
			plan.Counts["profileBlocksRestored"]++
		}
		if slices.Contains(d.Fields, "avatar") && current.Avatar != nil {
			plan.Counts["avatarsRestored"]++
		}
	}
	if fresh && author.AuthorID != "unknown" {
		if err := preserveNestedImportCandidate(ctx, q, author.AuthorID, archive.ID, profile, &result); err != nil {
			return false, err
		}
	}
	plan.Counts["profileCandidatesPreserved"] += int64(len(result.CandidateBlocks))
	for _, issue := range result.Issues {
		migrationIssue(plan, issue)
	}
	return partial, nil
}
