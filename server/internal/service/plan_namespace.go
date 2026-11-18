package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path"
	"sort"

	"qimeng.local/next/server/internal/domain"
	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/store"
)

func planDirectory(name string) string {
	parent := path.Dir(name)
	if parent == "." {
		return ""
	}
	return parent
}

// Hash the occupancy the allocator used and the objects this input refers to.
// Unrelated catalog edits, likes and seeks do not change these inputs.
func planItemScope(ctx context.Context, q store.Querier, library string, item model.PlanItem, names []string, ownPaths, ownAssets map[string]bool) (string, error) {
	h := sha256.New()
	enc := json.NewEncoder(h)
	dir := planDirectory(item.RelativePath)
	class := domain.NumberClass(item.FileName)
	_ = enc.Encode([]string{library, dir, item.NamingFamily, class})
	var collection model.Collection
	if item.CollectionID != "" {
		var err error
		collection, err = store.Get[model.Collection](ctx, q, "SELECT data FROM collections WHERE id=?", item.CollectionID)
		if err != nil {
			return "", planReferenceError(err)
		}
		_ = enc.Encode([]any{collection.ID, collection.LibraryID, collection.Name, collection.Kind, collection.Revision})
		if err = hashCollectionMembers(ctx, q, collection, ownAssets, enc); err != nil {
			return "", err
		}
	}
	relevant := []string{}
	planned, named := domain.ParseWork(item.FileName)
	occupied := map[int]bool{}
	pages := []string{}
	for _, name := range names {
		if ownPaths[path.Join(dir, name)] || domain.NumberClass(name) != class {
			continue
		}
		work, ok := domain.ParseWork(name)
		if ok && work.Family == item.NamingFamily {
			if named && work.Number <= planned.Number {
				occupied[work.Number] = true
			}
			if item.Page > 0 && named && work.Number == planned.Number {
				pages = append(pages, name)
			}
		}
		relevant = append(relevant, name)
	}
	if collection.Kind == "short_drama" {
		_ = enc.Encode(domain.NextChapter(item.NamingFamily, relevant, 0))
	} else {
		next, width, sep := domain.Allocate(item.NamingFamily, class, relevant)
		_ = enc.Encode([]any{next, width, sep})
		_ = enc.Encode(occupied)
		sort.Strings(pages)
		_ = enc.Encode(pages)
	}
	for _, id := range item.AuthorIDs {
		var name, kind string
		if err := q.QueryRowContext(ctx, "SELECT name,coalesce(json_extract(data,'$.kind'),'regular') FROM authors WHERE id=?", id).Scan(&name, &kind); err != nil {
			return "", planReferenceError(err)
		}
		_ = enc.Encode([]string{"author", id, name, kind})
	}
	for _, id := range item.SourceIDs {
		var name string
		if err := q.QueryRowContext(ctx, "SELECT name FROM sources WHERE id=?", id).Scan(&name); err != nil {
			return "", planReferenceError(err)
		}
		_ = enc.Encode([]string{"source", id, name})
	}
	for _, id := range item.CharacterIDs {
		var name, source string
		if err := q.QueryRowContext(ctx, "SELECT name,source_id FROM characters WHERE id=?", id).Scan(&name, &source); err != nil {
			return "", planReferenceError(err)
		}
		_ = enc.Encode([]string{"character", id, source, name})
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func planReferenceError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return Fail(409, "namespace_changed", "计划引用的作者、出处、角色或集合已移除，请重新核对。")
	}
	return err
}

func hashCollectionMembers(ctx context.Context, q store.Querier, c model.Collection, own map[string]bool, enc *json.Encoder) error {
	rows, err := q.QueryContext(ctx, `SELECT m.asset_id,l.relative_path,coalesce(m.position,m.reported_position,0)
		FROM collection_members m JOIN asset_locations l ON l.asset_id=m.asset_id
		WHERE m.collection_id=? AND (l.trashed=0 OR ?) ORDER BY m.asset_id`, c.ID, c.Kind == "short_drama")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, relative string
		var page int
		if err = rows.Scan(&id, &relative, &page); err != nil {
			return err
		}
		if !own[id] {
			_ = enc.Encode([]any{id, relative, page})
		}
	}
	return rows.Err()
}
