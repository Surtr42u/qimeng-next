package service

import (
	"context"
	"time"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/store"
)

type AuthorMergeInput struct {
	SourceID               string `json:"sourceId"`
	TargetID               string `json:"targetId"`
	ExpectedSourceRevision int64  `json:"expectedSourceRevision"`
	ExpectedTargetRevision int64  `json:"expectedTargetRevision"`
}
type AuthorMergeResult struct {
	Author             model.Author `json:"author"`
	AffectedAssets     int          `json:"affectedAssets"`
	PreservedConflicts []string     `json:"preservedConflicts"`
}

func (s *Service) archiveAuthor(ctx context.Context, q store.Querier, w *Write, a model.Author, reason string) error {
	raw, e := store.Encode(a)
	if e != nil {
		return e
	}
	_, e = q.ExecContext(ctx, "INSERT INTO author_archives(id,author_id,reason,created_at,data) VALUES(?,?,?,?,?)", w.ID("author-archive"), a.ID, reason, time.Now().UTC().Format(time.RFC3339Nano), raw)
	return e
}
func replaceAuthor(ctx context.Context, q store.Querier, a *model.Asset, source, target string) error {
	ids := []string{}
	for _, id := range a.AuthorIDs {
		if id == source {
			id = target
		}
		if id != "" && id != "unknown" {
			ids = appendUnique(ids, id)
		}
	}
	if len(ids) == 0 {
		ids = []string{"unknown"}
	}
	authors := []model.Author{}
	for _, id := range ids {
		author, e := store.Get[model.Author](ctx, q, "SELECT data FROM authors WHERE id=?", id)
		if e != nil {
			return e
		}
		authors = append(authors, author)
	}
	setAuthors(a, authors)
	a.AuthorsRevision++
	a.Revision++
	a.UpdatedAt = time.Now().UTC()
	return store.SaveAssetEdits(ctx, q, *a, true, nil)
}
func (s *Service) DeleteAuthor(ctx context.Context, w *Write, id string, revision int64) error {
	if e := Authorize(w.Principal, "admin", ""); e != nil {
		return e
	}
	author, e := s.Author(ctx, id)
	if e != nil {
		return e
	}
	if author.Placeholder || author.Kind == "cos" {
		return Fail(409, "author_delete_protected", "占位作者不能删除；COS 作者需通过对应目录管理。")
	}
	if author.Revision != revision {
		return Conflict(author.Revision)
	}
	q, e := w.Q()
	if e != nil {
		return e
	}
	if e = s.archiveAuthor(ctx, q, w, author, "delete"); e != nil {
		return e
	}
	if e = archiveDeletedAuthorProfile(ctx, q, id, w.ID("author-archive")); e != nil {
		return e
	}
	if e = touchAuthorListMetadata(ctx, q, author, false, true); e != nil {
		return e
	}
	for {
		assets, e := store.List[model.Asset](ctx, q, "SELECT a.data FROM assets a JOIN asset_authors x ON x.asset_id=a.id WHERE x.author_id=? ORDER BY a.id LIMIT 200", id)
		if e != nil {
			return e
		}
		if len(assets) == 0 {
			break
		}
		for _, a := range assets {
			if e = replaceAuthor(ctx, q, &a, id, ""); e != nil {
				return e
			}
		}
	}
	if _, e = q.ExecContext(ctx, "INSERT INTO author_tombstones(id,revision,deleted_at) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET revision=MAX(revision,excluded.revision),deleted_at=excluded.deleted_at", id, author.Revision+1, time.Now().UTC().Format(time.RFC3339Nano)); e != nil {
		return e
	}
	_, e = q.ExecContext(ctx, "DELETE FROM authors WHERE id=?", id)
	if e == nil {
		w.libraryChanged()
	}
	return e
}

// delegateAuthorMerge is reachable only with the standing credential issued
// with author.merge; task credentials and unrelated authors stay denied.
func (s *Service) delegateAuthorMerge(ctx context.Context, p model.Principal, in AuthorMergeInput) error {
	if !p.Has("author.merge") || p.ParentDelegationID == "" || p.BatchID != "" || p.AuthorTaskID != "" || p.AssetTaskID != "" {
		return Fail(403, "author_merge_denied", "合并作者须由首次配置单独授权的长期凭据发起。")
	}
	if e := s.validateDelegation(ctx, p); e != nil {
		return e
	}
	d, e := s.delegation(ctx, p.ParentDelegationID)
	if e != nil {
		return e
	}
	if !d.CanMergeAuthors {
		return Fail(403, "author_merge_denied", "首次配置未授权合并作者。")
	}
	if !d.AllAuthors && (!containsString(d.AuthorIDs, in.SourceID) || !containsString(d.AuthorIDs, in.TargetID)) {
		return Fail(403, "author_scope_denied", "源或目标作者不在首次授权的范围内。")
	}
	return nil
}
func (s *Service) MergeAuthors(ctx context.Context, w *Write, in AuthorMergeInput) (AuthorMergeResult, error) {
	result := AuthorMergeResult{PreservedConflicts: []string{}}
	if e := Authorize(w.Principal, "admin", ""); e != nil {
		if e = s.delegateAuthorMerge(ctx, w.Principal, in); e != nil {
			return result, e
		}
	}
	if in.SourceID == in.TargetID {
		return result, Fail(400, "same_author", "合并的源与目标必须不同。")
	}
	source, e := s.Author(ctx, in.SourceID)
	if e != nil {
		return result, e
	}
	target, e := s.Author(ctx, in.TargetID)
	if e != nil {
		return result, e
	}
	if source.Placeholder || target.Placeholder || source.Kind != "regular" || target.Kind != "regular" {
		return result, Fail(400, "merge_regular_only", "仅明确的常规作者身份可以合并。")
	}
	if source.Revision != in.ExpectedSourceRevision {
		return result, Conflict(source.Revision)
	}
	if target.Revision != in.ExpectedTargetRevision {
		return result, Conflict(target.Revision)
	}
	if target.FieldStatus["biography"] == "missing" && target.Biography == "" {
		target.Biography = source.Biography
		target.FieldStatus["biography"] = source.FieldStatus["biography"]
	} else if source.Biography != "" && source.Biography != target.Biography {
		result.PreservedConflicts = append(result.PreservedConflicts, "biography")
	}
	if target.FieldStatus["homepage"] == "missing" && target.Homepage == "" {
		target.Homepage = source.Homepage
		target.FieldStatus["homepage"] = source.FieldStatus["homepage"]
	} else if source.Homepage != "" && source.Homepage != target.Homepage {
		result.PreservedConflicts = append(result.PreservedConflicts, "homepage")
	}
	target.AcquisitionSources = appendUnique(target.AcquisitionSources, source.AcquisitionSources...)
	if len(target.AcquisitionSources) > 100 {
		return result, Fail(409, "acquisition_sources_limit", "合并后的获取渠道超过上限，请先核对。")
	}
	target.Aliases = appendUnique(target.Aliases, source.Name)
	target.Aliases = appendUnique(target.Aliases, source.Aliases...)
	if len(target.Aliases) > 100 {
		return result, Fail(409, "alias_limit", "合并后的别名超过上限，请先核对别名。")
	}
	target.Following = target.Following || source.Following
	target.Revision++
	target.UpdatedAt = time.Now().UTC()
	q, e := w.Q()
	if e != nil {
		return result, e
	}
	if e = s.archiveAuthor(ctx, q, w, source, "merge:"+target.ID); e != nil {
		return result, e
	}
	raw, e := store.Encode(target)
	if e != nil {
		return result, e
	}
	if _, e = q.ExecContext(ctx, "UPDATE authors SET following=?,data=? WHERE id=?", target.Following, raw, target.ID); e != nil {
		return result, e
	}
	profileConflicts, e := mergeAuthorProfiles(ctx, q, source.ID, target.ID, w.ID("author-archive"))
	if e != nil {
		return result, e
	}
	result.PreservedConflicts = append(result.PreservedConflicts, profileConflicts...)
	if e = mergeAuthorListBlocks(ctx, q, source, target); e != nil {
		return result, e
	}
	for {
		assets, e := store.List[model.Asset](ctx, q, "SELECT a.data FROM assets a JOIN asset_authors x ON x.asset_id=a.id WHERE x.author_id=? ORDER BY a.id LIMIT 200", source.ID)
		if e != nil {
			return result, e
		}
		if len(assets) == 0 {
			break
		}
		for _, a := range assets {
			if e = replaceAuthor(ctx, q, &a, source.ID, target.ID); e != nil {
				return result, e
			}
			result.AffectedAssets++
		}
	}
	if _, e = q.ExecContext(ctx, "INSERT INTO author_tombstones(id,revision,deleted_at) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET revision=MAX(revision,excluded.revision),deleted_at=excluded.deleted_at", source.ID, source.Revision+1, time.Now().UTC().Format(time.RFC3339Nano)); e != nil {
		return result, e
	}
	if _, e = q.ExecContext(ctx, "DELETE FROM authors WHERE id=?", source.ID); e != nil {
		return result, e
	}
	if e = q.QueryRowContext(ctx, "SELECT COUNT(DISTINCT a.id),COALESCE(sum(st.views),0) FROM asset_authors aa JOIN assets a ON a.id=aa.asset_id LEFT JOIN asset_daily_stats st ON st.asset_id=a.id WHERE aa.author_id=? AND a.trashed=0", target.ID).Scan(&target.FileCount, &target.ViewCount); e != nil {
		return result, e
	}
	raw, e = store.Encode(target)
	if e != nil {
		return result, e
	}
	if _, e = q.ExecContext(ctx, "UPDATE authors SET data=? WHERE id=?", raw, target.ID); e != nil {
		return result, e
	}
	result.Author = target
	w.libraryChanged()
	return result, nil
}
