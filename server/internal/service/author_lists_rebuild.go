package service

import (
	"context"
	"encoding/json"
	"time"

	"qimeng.local/next/server/internal/authorlist"
	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/store"
)

const maxAuthorListMatches = 1000000
const authorListAssetRows = " FROM assets a WHERE a.id NOT IN " + selectedImageDonors

type textMatch struct{ ListID, AssetID, AuthorID, Work string }
type textRule struct{ ListID, AuthorID, Work string }

func sameAuthorSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	set := map[string]bool{}
	for _, id := range a {
		set[id] = true
	}
	for _, id := range b {
		if !set[id] {
			return false
		}
	}
	return true
}
func preferredAuthorOrder(current, desired []string) []string {
	set := map[string]bool{}
	for _, id := range desired {
		set[id] = true
	}
	out := []string{}
	for _, id := range current {
		if set[id] {
			out = appendUnique(out, id)
		}
	}
	for _, id := range desired {
		out = appendUnique(out, id)
	}
	if len(out) == 0 {
		out = append(out, "unknown")
	}
	return out
}

// Rebuild compiles declarations once and joins them with a bounded filename
// index. Existing matches are stable identity bindings while their declaration
// remains present, so renaming a file does not sever its creator relationship.
func (s *Service) rebuildAuthorLists(ctx context.Context, q store.Querier) (AuthorListResult, error) {
	result := AuthorListResult{Format: "blocks", PreservedSources: []string{}, Issues: []authorlist.Issue{}}
	var count int
	if e := q.QueryRowContext(ctx, "SELECT count(*)"+authorListAssetRows).Scan(&count); e != nil {
		return result, e
	}
	if count > maxAuthorListAssets {
		return result, Fail(422, "author_list_asset_budget", "作者重建超过当前索引资源预算，未截断或修改关联。")
	}
	kinds := map[string]string{}
	rows, e := q.QueryContext(ctx, "SELECT id,COALESCE(json_extract(data,'$.kind'),'') FROM authors")
	if e != nil {
		return result, e
	}
	for rows.Next() {
		var id, kind string
		if e = rows.Scan(&id, &kind); e != nil {
			rows.Close()
			return result, e
		}
		kinds[id] = kind
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return result, e
	}
	current := map[string][]string{}
	rows, e = q.QueryContext(ctx, "SELECT asset_id,author_id FROM asset_authors WHERE asset_id NOT IN "+selectedImageDonors+" ORDER BY asset_id,author_id")
	if e != nil {
		return result, e
	}
	for rows.Next() {
		var asset, author string
		if e = rows.Scan(&asset, &author); e != nil {
			rows.Close()
			return result, e
		}
		current[asset] = append(current[asset], author)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return result, e
	}
	files := []authorlist.File{}
	assetIDs := []string{}
	fixed := map[string][]string{}
	manualEmpty := map[string]bool{}
	exists := map[string]bool{}
	rows, e = q.QueryContext(ctx, `SELECT a.id,a.file_name,coalesce(json_extract(a.data,'$.cosDirectoryAuthorId'),''),
		EXISTS(SELECT 1 FROM asset_human_fields f WHERE f.asset_id=a.id AND f.field='authors')`+authorListAssetRows+" ORDER BY a.id")
	if e != nil {
		return result, e
	}
	for rows.Next() {
		var id, name, directoryAuthor string
		var cleared bool
		if e = rows.Scan(&id, &name, &directoryAuthor, &cleared); e != nil {
			rows.Close()
			return result, e
		}
		exists[id] = true
		assetIDs = append(assetIDs, id)
		for _, author := range current[id] {
			if kinds[author] == "cos" {
				fixed[id] = appendUnique(fixed[id], author)
			} else if author != "unknown" {
				cleared = false
			}
		}
		if cleared {
			manualEmpty[id] = true
		}
		if kinds[directoryAuthor] == "cos" {
			fixed[id] = appendUnique(fixed[id], directoryAuthor)
		}
		if len(fixed[id]) == 0 {
			files = append(files, authorlist.File{ID: id, Name: name})
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return result, e
	}
	index := authorlist.BuildIndex(files)
	rules := []textRule{}
	rows, e = q.QueryContext(ctx, "SELECT r.list_id,r.author_id,r.work FROM author_list_rules r JOIN author_lists l ON l.id=r.list_id ORDER BY l.position,r.ordinal,r.author_id,r.work")
	if e != nil {
		return result, e
	}
	for rows.Next() {
		var rule textRule
		if e = rows.Scan(&rule.ListID, &rule.AuthorID, &rule.Work); e != nil {
			rows.Close()
			return result, e
		}
		rules = append(rules, rule)
		if len(rules) > maxAuthorListMatches {
			rows.Close()
			return result, Fail(422, "author_list_rule_budget", "作者作品规则超过资源预算，未修改关联。")
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return result, e
	}
	matches := []textMatch{}
	seen := map[textMatch]bool{}
	add := func(value textMatch) error {
		if seen[value] {
			return nil
		}
		if len(matches) >= maxAuthorListMatches {
			return Fail(422, "author_list_match_budget", "匹配关系超过资源预算，未截断或修改关联。")
		}
		seen[value] = true
		matches = append(matches, value)
		return nil
	}
	rows, e = q.QueryContext(ctx, "SELECT m.list_id,m.asset_id,m.author_id,m.work FROM author_list_matches m JOIN author_list_rules r ON r.list_id=m.list_id AND r.author_id=m.author_id AND r.work=m.work ORDER BY m.list_id,m.author_id,m.work,m.asset_id")
	if e != nil {
		return result, e
	}
	for rows.Next() {
		var match textMatch
		if e = rows.Scan(&match.ListID, &match.AssetID, &match.AuthorID, &match.Work); e != nil {
			rows.Close()
			return result, e
		}
		if exists[match.AssetID] {
			if e = add(match); e != nil {
				rows.Close()
				return result, e
			}
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return result, e
	}
	for _, rule := range rules {
		for _, file := range index.Match(rule.Work) {
			if e = add(textMatch{rule.ListID, file.ID, rule.AuthorID, rule.Work}); e != nil {
				return result, e
			}
		}
	}
	positive := map[string][]string{}
	removed := map[string]map[string]bool{}
	rows, e = q.QueryContext(ctx, "SELECT asset_id,author_id,removed FROM asset_author_overrides WHERE asset_id NOT IN "+selectedImageDonors+" ORDER BY asset_id,author_id")
	if e != nil {
		return result, e
	}
	for rows.Next() {
		var asset, author string
		var denied bool
		if e = rows.Scan(&asset, &author, &denied); e != nil {
			rows.Close()
			return result, e
		}
		if denied {
			if removed[asset] == nil {
				removed[asset] = map[string]bool{}
			}
			removed[asset][author] = true
		} else {
			positive[asset] = appendUnique(positive[asset], author)
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return result, e
	}
	desired := map[string][]string{}
	for _, id := range assetIDs {
		desired[id] = appendUnique(append([]string{}, fixed[id]...), positive[id]...)
	}
	matchedAssets := map[string]bool{}
	for _, match := range matches {
		if !manualEmpty[match.AssetID] && !removed[match.AssetID][match.AuthorID] && match.AuthorID != "unknown" {
			desired[match.AssetID] = appendUnique(desired[match.AssetID], match.AuthorID)
			matchedAssets[match.AssetID] = true
		}
	}
	for _, id := range assetIDs {
		if len(desired[id]) > 20 {
			return result, Fail(409, "too_many_authors", "清单会使同一资产关联超过20位作者，请先核对作品规则。")
		}
	}
	if _, e = q.ExecContext(ctx, "DELETE FROM author_list_matches"); e != nil {
		return result, e
	}
	for _, match := range matches {
		if _, e = q.ExecContext(ctx, "INSERT INTO author_list_matches(list_id,asset_id,author_id,work) VALUES(?,?,?,?)", match.ListID, match.AssetID, match.AuthorID, match.Work); e != nil {
			return result, e
		}
	}
	cache := map[string]model.Author{}
	for _, id := range assetIDs {
		wanted := preferredAuthorOrder(current[id], desired[id])
		if sameAuthorSet(current[id], wanted) {
			continue
		}
		asset, e := store.Get[model.Asset](ctx, q, "SELECT data FROM assets WHERE id=?", id)
		if e != nil {
			return result, e
		}
		wanted = preferredAuthorOrder(asset.AuthorIDs, desired[id])
		authors := []model.Author{}
		for _, authorID := range wanted {
			author, ok := cache[authorID]
			if !ok {
				author, e = store.Get[model.Author](ctx, q, "SELECT data FROM authors WHERE id=?", authorID)
				if e != nil {
					return result, e
				}
				cache[authorID] = author
			}
			authors = append(authors, author)
		}
		setAuthors(&asset, authors)
		asset.AuthorsRevision++
		asset.Revision++
		asset.UpdatedAt = time.Now().UTC()
		if e = store.SaveAssetEdits(ctx, q, asset, true, nil); e != nil {
			return result, e
		}
		result.ChangedAssets++
	}
	result.MatchedAssets = len(matchedAssets)
	return result, nil
}

// The single-file path queries precompiled rules by the same pure match keys.
// Upload/scan do not rebuild or scan every stored TXT for each new file.
func (s *Service) matchAuthorListsForAsset(ctx context.Context, q store.Querier, a *model.Asset, newRegularScan bool) error {
	var cos int
	var manualEmpty bool
	if !newRegularScan {
		if e := q.QueryRowContext(ctx, `SELECT count(*) FILTER(WHERE json_extract(au.data,'$.kind')='cos'),
			count(*) FILTER(WHERE au.id!='unknown' AND coalesce(json_extract(au.data,'$.kind'),'')!='cos')=0 AND
			EXISTS(SELECT 1 FROM asset_human_fields WHERE asset_id=?1 AND field='authors')
			FROM asset_authors x JOIN authors au ON au.id=x.author_id WHERE x.asset_id=?1`, a.ID).Scan(&cos, &manualEmpty); e != nil {
			return e
		}
	}
	if cos == 0 {
		keys := authorlist.Keys(a.FileName)
		rows, e := q.QueryContext(ctx, "SELECT r.list_id,r.author_id,r.work FROM author_list_rules r JOIN author_lists l ON l.id=r.list_id WHERE (r.match_key=? AND (r.extension='' OR r.extension=?)) OR (r.pages=1 AND ?!='' AND r.match_key=?) ORDER BY l.position,r.ordinal", keys.Stem, keys.Extension, keys.PageBase, keys.PageBase)
		if e != nil {
			return e
		}
		matches := []textMatch{}
		for rows.Next() {
			var value textMatch
			value.AssetID = a.ID
			if e = rows.Scan(&value.ListID, &value.AuthorID, &value.Work); e != nil {
				rows.Close()
				return e
			}
			matches = append(matches, value)
			if len(matches) > 10000 {
				rows.Close()
				return Fail(422, "author_list_match_budget", "此文件命中的作者规则超出预算。")
			}
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		// A newly inserted regular scan asset starts with the unknown author.
		// Prior overrides and identity-bound matches require an existing asset.
		if newRegularScan && len(matches) == 0 {
			return nil
		}
		for _, match := range matches {
			if _, e = q.ExecContext(ctx, "INSERT OR IGNORE INTO author_list_matches(list_id,asset_id,author_id,work) VALUES(?,?,?,?)", match.ListID, match.AssetID, match.AuthorID, match.Work); e != nil {
				return e
			}
		}
	}
	if manualEmpty {
		// Keep the matching declaration as evidence without changing the
		// owner's explicitly cleared regular-author set or its fixed COS identity.
		return nil
	}
	rows, e := q.QueryContext(ctx, `WITH ids AS (
 SELECT x.author_id FROM asset_authors x JOIN authors c ON c.id=x.author_id WHERE x.asset_id=? AND json_extract(c.data,'$.kind')='cos'
 UNION SELECT author_id FROM asset_author_overrides WHERE asset_id=? AND removed=0
 UNION SELECT m.author_id FROM author_list_matches m WHERE m.asset_id=? AND m.author_id!='unknown'
 AND NOT EXISTS(SELECT 1 FROM asset_author_overrides o WHERE o.asset_id=m.asset_id AND o.author_id=m.author_id AND o.removed=1)
 ) SELECT au.data FROM ids JOIN authors au ON au.id=ids.author_id ORDER BY au.id`, a.ID, a.ID, a.ID)
	if e != nil {
		return e
	}
	byID := map[string]model.Author{}
	desired := []string{}
	for rows.Next() {
		var raw string
		var author model.Author
		if e = rows.Scan(&raw); e != nil {
			rows.Close()
			return e
		}
		if e = json.Unmarshal([]byte(raw), &author); e != nil {
			rows.Close()
			return e
		}
		byID[author.ID] = author
		desired = append(desired, author.ID)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	if len(desired) > 20 {
		return Fail(409, "too_many_authors", "此文件命中超过20位作者，请核对清单。")
	}
	wanted := preferredAuthorOrder(a.AuthorIDs, desired)
	if sameAuthorSet(a.AuthorIDs, wanted) {
		return nil
	}
	authors := []model.Author{}
	for _, id := range wanted {
		author, ok := byID[id]
		if !ok {
			author, e = store.Get[model.Author](ctx, q, "SELECT data FROM authors WHERE id=?", id)
			if e != nil {
				return e
			}
		}
		authors = append(authors, author)
	}
	setAuthors(a, authors)
	a.AuthorsRevision++
	a.Revision++
	a.UpdatedAt = time.Now().UTC()
	return store.SaveAssetEdits(ctx, q, *a, true, nil)
}
