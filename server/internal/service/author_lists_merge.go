package service

import (
	"context"
	"strings"
	"time"

	"qimeng.local/next/server/internal/authorlist"
	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/store"
)

// Move declared works and their stable matches before deleting the source
// identity. A fresh filename lookup alone would lose previously renamed files.
func mergeAuthorListBlocks(ctx context.Context, q store.Querier, source, target model.Author) error {
	lists, e := store.List[authorListRecord](ctx, q, `SELECT l.data FROM author_lists l WHERE EXISTS(SELECT 1 FROM author_list_members m WHERE m.list_id=l.id AND m.author_id IN (?,?)) ORDER BY l.position`, source.ID, target.ID)
	if e != nil {
		return e
	}
	firstSource, home := -1, -1
	combined := authorlist.Block{}
	for i, list := range lists {
		for _, b := range list.Blocks {
			if b.AuthorID == target.ID && home < 0 {
				home = i
			}
			if b.AuthorID != source.ID {
				continue
			}
			if firstSource < 0 {
				firstSource = i
			}
			combined.Names = appendUnique(combined.Names, b.Declaration.Names...)
			combined.Sources = appendUnique(combined.Sources, b.Declaration.Sources...)
			combined.Works = appendUnique(combined.Works, b.Declaration.Works...)
		}
	}
	if firstSource < 0 {
		return nil
	}
	if home < 0 {
		home = firstSource
		declaration, e := textDeclaration(target)
		if e != nil {
			return e
		}
		lists[home].Blocks = append(lists[home].Blocks, authorListBlock{AuthorID: target.ID, Declaration: declaration})
	}
	destination := lists[home].ID
	// Source rules are removed by saveAuthorList. Copy matches while both sets
	// of rule bindings still exist, using the final combined destination rule.
	for i := range lists[home].Blocks {
		b := &lists[home].Blocks[i]
		if b.AuthorID != target.ID {
			continue
		}
		b.Declaration.Names = appendUnique(b.Declaration.Names, combined.Names...)
		if _, e = authorlist.Names(strings.Join(b.Declaration.Names, " / ")); e != nil {
			return Fail(409, "author_alias_limit", "合并后的别名无法安全导出，请先精简别名。")
		}
		b.Declaration.Header = ""
		b.Declaration.Sources = appendUnique(b.Declaration.Sources, combined.Sources...)
		b.Declaration.Works = appendUnique(b.Declaration.Works, combined.Works...)
		break
	}
	if e = saveAuthorList(ctx, q, &lists[home]); e != nil {
		return e
	}
	if _, e = q.ExecContext(ctx, `INSERT OR IGNORE INTO author_list_matches(list_id,asset_id,author_id,work) SELECT ?,asset_id,?,work FROM author_list_matches WHERE author_id=?`, destination, target.ID, source.ID); e != nil {
		return e
	}
	if _, e = q.ExecContext(ctx, `INSERT INTO asset_author_overrides(asset_id,author_id,removed,list_id)
  SELECT asset_id,?,removed,CASE removed WHEN 0 THEN ? ELSE '' END FROM asset_author_overrides WHERE author_id=?
  ON CONFLICT(asset_id,author_id) DO UPDATE SET removed=0,list_id=excluded.list_id WHERE asset_author_overrides.removed=1 AND excluded.removed=0`, target.ID, destination, source.ID); e != nil {
		return e
	}
	for i := range lists {
		list := &lists[i]
		kept := make([]authorListBlock, 0, len(list.Blocks))
		for _, block := range list.Blocks {
			if block.AuthorID != source.ID {
				kept = append(kept, block)
			}
		}
		if len(kept) == len(list.Blocks) && i != home {
			continue
		}
		list.Blocks = kept
		list.Revision++
		list.UpdatedAt = time.Now().UTC()
		if e = saveAuthorList(ctx, q, list); e != nil {
			return e
		}
	}
	_, e = q.ExecContext(ctx, "INSERT OR IGNORE INTO author_source_overrides(author_id) SELECT ? WHERE EXISTS(SELECT 1 FROM author_source_overrides WHERE author_id=?)", target.ID, source.ID)
	return e
}
