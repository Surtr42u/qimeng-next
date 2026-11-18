package service

import (
	"context"
	"database/sql"
	"errors"
	"path"
	"strings"
	"time"

	"qimeng.local/next/server/internal/authorlist"
	"qimeng.local/next/server/internal/domain"
	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/store"
)

func listAuthorIDs(blocks []authorListBlock) []string {
	ids := []string{}
	seen := map[string]bool{}
	for _, b := range blocks {
		if !seen[b.AuthorID] {
			seen[b.AuthorID] = true
			ids = append(ids, b.AuthorID)
		}
	}
	return ids
}

func bindTextDeclarations(ctx context.Context, q store.Querier, id func(string) string, doc authorlist.Document) ([]authorListBlock, int, error) {
	blocks := make([]authorListBlock, 0, len(doc.Blocks))
	created := 0
	for _, b := range doc.Blocks {
		key := authorlist.IdentityKey(b.Names[0])
		a, found, e := lookupRegularAuthor(ctx, q, key)
		if e != nil {
			return nil, 0, e
		}
		if !found {
			a = newTextAuthor(id("text-author:"+key), b.Names, time.Now().UTC())
			if e = saveNewRegularAuthor(ctx, q, a); e != nil {
				return nil, 0, e
			}
			created++
		}
		if _, e = authorSourceTerms(b.Sources); e != nil {
			return nil, 0, e
		}
		blocks = append(blocks, authorListBlock{AuthorID: a.ID, Declaration: b})
	}
	return blocks, created, nil
}

func protectListEntries(ctx context.Context, q store.Querier, list *authorListRecord, resolution string) error {
	rows, e := q.QueryContext(ctx, `SELECT o.author_id,au.name,a.id,a.file_name FROM asset_author_overrides o
  JOIN assets a ON a.id=o.asset_id JOIN authors au ON au.id=o.author_id WHERE o.list_id=? AND o.removed=0 ORDER BY o.author_id,a.id`, list.ID)
	if e != nil {
		return e
	}
	defer rows.Close()
	accepted := map[string]map[string]bool{}
	for _, b := range list.Blocks {
		if accepted[b.AuthorID] == nil {
			accepted[b.AuthorID] = map[string]bool{}
		}
		for _, work := range b.Declaration.Works {
			accepted[b.AuthorID][work] = true
		}
	}
	missing := []AuthorListMissingWork{}
	for rows.Next() {
		var item AuthorListMissingWork
		if e = rows.Scan(&item.AuthorID, &item.AuthorName, &item.AssetID, &item.FileName); e != nil {
			return e
		}
		if !accepted[item.AuthorID][item.FileName] {
			missing = append(missing, item)
		}
	}
	if e = rows.Err(); e != nil {
		return e
	}
	rows.Close()
	if len(missing) == 0 {
		return nil
	}
	if resolution == "" {
		return &Error{Status: 409, Code: "author_list_missing_entries", Message: "重导清单缺少已手工挂靠的作品，请选择保留或移除。", CurrentRevision: list.Revision, Details: map[string]any{"missing": missing[:min(len(missing), 100)], "totalMissing": len(missing), "truncated": len(missing) > 100}}
	}
	for _, item := range missing {
		if resolution == "remove" {
			// The owner is removing this list's entry. Another list's independent
			// declaration may still match this file.
			if _, e = q.ExecContext(ctx, "DELETE FROM asset_author_overrides WHERE asset_id=? AND author_id=? AND list_id=?", item.AssetID, item.AuthorID, list.ID); e != nil {
				return e
			}
			continue
		}
		if accepted[item.AuthorID] == nil {
			a, e := store.Get[model.Author](ctx, q, "SELECT data FROM authors WHERE id=?", item.AuthorID)
			if e != nil {
				return e
			}
			b, e := textDeclaration(a)
			if e != nil {
				return e
			}
			list.Blocks = append(list.Blocks, authorListBlock{AuthorID: a.ID, Declaration: b})
			accepted[a.ID] = map[string]bool{}
		}
		if !accepted[item.AuthorID][item.FileName] {
			addListWork(list, item.AuthorID, item.FileName)
			accepted[item.AuthorID][item.FileName] = true
		}
	}
	return nil
}

func (s *Service) ImportAuthorList(ctx context.Context, w *Write, in AuthorListInput) (AuthorListResult, error) {
	return s.importAuthorList(ctx, w, in, w.ID)
}

func (s *Service) importAuthorList(ctx context.Context, w *Write, in AuthorListInput, id func(string) string) (AuthorListResult, error) {
	result := AuthorListResult{PreservedSources: []string{}, Issues: []authorlist.Issue{}}
	if e := Authorize(w.Principal, "admin", ""); e != nil {
		return result, e
	}
	if !domain.ValidName(in.Name) || !strings.EqualFold(".txt", path.Ext(in.Name)) || in.ExpectedRevision == nil || *in.ExpectedRevision < 0 {
		return result, Fail(400, "invalid_author_list", "须提供有效的TXT文件名与预期修订号。")
	}
	if in.ConflictResolution != "" && in.ConflictResolution != "keep" && in.ConflictResolution != "remove" {
		return result, Fail(400, "invalid_resolution", "清单冲突处理方式无效。")
	}
	doc, e := authorlist.Parse(in.Content)
	if e != nil {
		return result, Fail(400, "invalid_author_list", e.Error())
	}
	q, e := w.Q()
	if e != nil {
		return result, e
	}
	list, e := store.Get[authorListRecord](ctx, q, "SELECT data FROM author_lists WHERE name=?", in.Name)
	exists := e == nil
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return result, e
	}
	if *in.ExpectedRevision != list.Revision {
		return result, Conflict(list.Revision)
	}
	oldAuthors := listAuthorIDs(list.Blocks)
	blocks, created, e := bindTextDeclarations(ctx, q, id, doc)
	if e != nil {
		return result, e
	}
	result.Format = doc.Format
	result.Issues = doc.Issues
	result.AuthorsCreated = created
	if e = archiveAuthorListInput(ctx, q, id("author-list-input"), "manual", in.Name, in.Content, time.Now().UTC()); e != nil {
		return result, e
	}
	if doc.Format == "names" {
		return result, nil
	}
	if !exists {
		list, e = nextAuthorList(ctx, q, id("author-list"), in.Name)
		if e != nil {
			return result, e
		}
	}
	list.Blocks = blocks
	if e = protectListEntries(ctx, q, &list, in.ConflictResolution); e != nil {
		return result, e
	}
	if exists {
		list.Revision++
	}
	list.UpdatedAt = time.Now().UTC()
	list.InputHash = digest([]byte(in.Content))
	if e = saveAuthorList(ctx, q, &list); e != nil {
		return result, e
	}
	result.PreservedSources, e = reconcileImportedSources(ctx, q, appendUnique(oldAuthors, listAuthorIDs(list.Blocks)...))
	if e != nil {
		return result, e
	}
	rebuilt, e := s.rebuildAuthorLists(ctx, q)
	if e != nil {
		return result, e
	}
	result.List = &list.AuthorList
	result.ChangedAssets = rebuilt.ChangedAssets
	result.MatchedAssets = rebuilt.MatchedAssets
	return result, nil
}

func (s *Service) RebuildAuthorLists(ctx context.Context, w *Write) (AuthorListResult, error) {
	if e := Authorize(w.Principal, "admin", ""); e != nil {
		return AuthorListResult{}, e
	}
	q, e := w.Q()
	if e != nil {
		return AuthorListResult{}, e
	}
	return s.rebuildAuthorLists(ctx, q)
}

func (s *Service) DeleteAuthorList(ctx context.Context, w *Write, id string, revision int64) error {
	if e := Authorize(w.Principal, "admin", ""); e != nil {
		return e
	}
	q, e := w.Q()
	if e != nil {
		return e
	}
	list, e := authorListRecordAt(ctx, q, id)
	if e != nil {
		return Normalize(e)
	}
	if list.Revision != revision {
		return Conflict(list.Revision)
	}
	if _, e = q.ExecContext(ctx, "DELETE FROM asset_author_overrides WHERE list_id=?", id); e != nil {
		return e
	}
	if _, e = q.ExecContext(ctx, "DELETE FROM author_lists WHERE id=?", id); e != nil {
		return e
	}
	if _, e = q.ExecContext(ctx, `INSERT INTO author_list_tombstones(name,revision) VALUES(?,?) ON CONFLICT(name) DO UPDATE SET revision=excluded.revision`, list.Name, list.Revision); e != nil {
		return e
	}
	if _, e = reconcileImportedSources(ctx, q, listAuthorIDs(list.Blocks)); e != nil {
		return e
	}
	_, e = s.rebuildAuthorLists(ctx, q)
	return e
}
