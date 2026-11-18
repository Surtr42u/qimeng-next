package service

import (
	"context"
	"slices"
	"strings"
	"time"
	"unicode"

	"qimeng.local/next/server/internal/authorlist"
	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/store"
)

func authorSourceTerms(values []string) ([]string, error) {
	terms, e := cleanStrings(append([]string{}, values...), 100, 2000)
	if e != nil {
		return nil, e
	}
	for _, term := range terms {
		if strings.ContainsFunc(term, unicode.IsControl) || strings.Contains(term, "  ") {
			return nil, Fail(400, "invalid_author_source", "作者来源词不能包含控制字符或清单分隔符。")
		}
	}
	return terms, nil
}

// Owner edits and upload additions both bind sources to the first declared
// block. A later TXT import cannot revert an explicit edit, including clearing.
func (s *Service) projectAuthorSources(ctx context.Context, q store.Querier, a model.Author) error {
	if a.Kind != "regular" || a.Placeholder {
		return nil
	}
	list, e := s.authorTextHome(ctx, q, a)
	if e != nil {
		return e
	}
	for i := range list.Blocks {
		block := &list.Blocks[i]
		if block.AuthorID != a.ID {
			continue
		}
		if !slices.Equal(block.Declaration.Sources, a.AcquisitionSources) {
			block.Declaration.Sources = append([]string{}, a.AcquisitionSources...)
			list.Revision++
			list.UpdatedAt = time.Now().UTC()
			if e = saveAuthorList(ctx, q, &list); e != nil {
				return e
			}
		}
		break
	}
	_, e = q.ExecContext(ctx, "INSERT OR IGNORE INTO author_source_overrides(author_id) VALUES(?)", a.ID)
	return e
}

func saveAuthorSources(ctx context.Context, q store.Querier, a *model.Author, values []string) error {
	if slices.Equal(a.AcquisitionSources, values) {
		return nil
	}
	a.AcquisitionSources = values
	a.Revision++
	a.UpdatedAt = time.Now().UTC()
	raw, e := store.Encode(values)
	if e != nil {
		return e
	}
	_, e = q.ExecContext(ctx, `UPDATE authors SET data=json_set(data,'$.acquisitionSources',json(?),'$.revision',?,'$.updatedAt',?) WHERE id=?`, raw, a.Revision, dateKey(a.UpdatedAt), a.ID)
	return e
}

func (s *Service) appendUploadAuthorSources(ctx context.Context, q store.Querier, a model.Asset, u model.Upload) error {
	if len(u.AuthorAcquisitionSources) == 0 {
		return nil
	}
	for _, id := range u.AuthorIDs {
		if id == "unknown" || !slices.Contains(a.AuthorIDs, id) {
			continue
		}
		author, e := store.Get[model.Author](ctx, q, "SELECT data FROM authors WHERE id=?", id)
		if e != nil {
			return e
		}
		if author.Kind != "regular" || author.Placeholder {
			continue
		}
		values := appendUnique(append([]string{}, author.AcquisitionSources...), u.AuthorAcquisitionSources...)
		if len(values) > 100 {
			return Fail(409, "acquisition_sources_limit", "所选作者的获取渠道超过100项，请先核对。")
		}
		if e = saveAuthorSources(ctx, q, &author, values); e != nil {
			return e
		}
		if e = s.projectAuthorSources(ctx, q, author); e != nil {
			return e
		}
	}
	return nil
}

func reconcileImportedSources(ctx context.Context, q store.Querier, ids []string) ([]string, error) {
	kept := []string{}
	for _, id := range ids {
		var explicit bool
		if e := q.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM author_source_overrides WHERE author_id=?)", id).Scan(&explicit); e != nil {
			return nil, e
		}
		if explicit {
			kept = append(kept, id)
			continue
		}
		author, e := store.Get[model.Author](ctx, q, "SELECT data FROM authors WHERE id=?", id)
		if e != nil {
			return nil, e
		}
		if author.Placeholder {
			continue
		}
		blocks, e := store.List[authorlist.Block](ctx, q, `SELECT json_extract(l.data,'$.blocks['||m.ordinal||'].declaration')
   FROM author_list_members m JOIN author_lists l ON l.id=m.list_id WHERE m.author_id=? ORDER BY l.position,m.ordinal`, id)
		if e != nil {
			return nil, e
		}
		values := []string{}
		for _, b := range blocks {
			values = appendUnique(values, b.Sources...)
		}
		if len(values) > 100 {
			return nil, Fail(409, "acquisition_sources_limit", "跨清单合并后作者获取渠道超过100项。")
		}
		if e = saveAuthorSources(ctx, q, &author, values); e != nil {
			return nil, e
		}
	}
	return kept, nil
}
