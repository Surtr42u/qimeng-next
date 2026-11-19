package catalog

import (
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"qimeng.local/next/server/internal/authorlist"
	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/service"
	"qimeng.local/next/server/internal/store"
)

type link struct {
	Key    string  `json:"key"`
	Name   string  `json:"name,omitempty"`
	Origin string  `json:"origin"`
	At     *string `json:"createdAt"`
}

func (i *importer) preserveLinks(assetID, kind string, encoded []byte) ([]link, error) {
	var refs []link
	if err := json.Unmarshal(encoded, &refs); err != nil {
		return nil, err
	}
	for _, ref := range refs {
		for _, v := range []string{ref.Key, ref.Name, ref.Origin} {
			if err := i.portableText(v); err != nil {
				return nil, err
			}
		}
		if ref.At != nil {
			if _, err := stamp(*ref.At); err != nil {
				return nil, err
			}
		}
		if err := i.origin(kind, []string{assetID, ref.Key}, map[string]any{"assetId": assetID, "relation": ref}); err != nil {
			return nil, err
		}
	}
	return refs, nil
}

func (i *importer) importAssets() error {
	// Read at most one asset's memberships at a time. All source reads use
	// the same cursor so the cold connection can have a one-connection pool.
	err := i.each(`SELECT a.asset_id,a.source,a.cos_work,a.tag_set_updated_at,l.kind,
		(SELECT json_group_array(json_object('key',x.author_id,'origin',x.origin,'createdAt',x.created_at) ORDER BY x.author_id)
		 FROM asset_authors x WHERE x.asset_id=a.asset_id),
		(SELECT json_group_array(json_object('key',x.tag_id,'name',t.name,'origin',x.origin,'createdAt',x.created_at)
		 ORDER BY x.created_at DESC,t.name,x.tag_id) FROM asset_tags x JOIN tags t ON t.id=x.tag_id WHERE x.asset_id=a.asset_id),
		(SELECT json_group_array(json_object('key',x.character_name,'origin',x.origin,'createdAt',x.created_at)
		 ORDER BY x.character_name) FROM asset_characters x WHERE x.asset_id=a.asset_id)
		FROM assets a JOIN libraries l ON l.id=a.library_id ORDER BY a.asset_id`, func(rows *sql.Rows) error {
		var assetID, tagSet, libraryKind string
		var source, cosWork sql.NullString
		var authors, tags, characters []byte
		if err := rows.Scan(&assetID, &source, &cosWork, &tagSet, &libraryKind, &authors, &tags, &characters); err != nil {
			return err
		}
		a, err := store.Get[model.Asset](i.ctx, i.target, "SELECT data FROM assets WHERE id=?", assetID)
		if err != nil {
			return err
		}
		if a.AuthorID != "unknown" {
			return errors.New("cold catalog: target already has asset author decisions")
		}
		authorRefs, err := i.preserveLinks(assetID, "asset-author", authors)
		if err != nil {
			return err
		}
		tagRefs, err := i.preserveLinks(assetID, "asset-tag", tags)
		if err != nil {
			return err
		}
		charRefs, err := i.preserveLinks(assetID, "asset-character", characters)
		if err != nil {
			return err
		}
		if err := i.assetAuthors(&a, authorRefs, libraryKind, cosWork); err != nil {
			return err
		}
		if err := i.assetTags(&a, tagRefs, tagSet); err != nil {
			return err
		}
		if err := i.assetTaxonomy(&a, source, charRefs); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	for key, entries := range i.protected {
		for _, entry := range entries {
			if !entry.matched {
				if err := i.origin("unbound-protected-work", []string{entry.list, key.author, key.work},
					map[string]string{"listId": entry.list, "authorId": key.author, "work": key.work}); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (i *importer) assetTags(a *model.Asset, refs []link, tagSet string) error {
	if len(refs) == 0 && tagSet == "" {
		return nil
	}
	a.Tags = make([]string, 0, len(refs))
	protected := tagSet != ""
	for _, ref := range refs {
		a.Tags = append(a.Tags, ref.Name)
		protected = protected || ref.Origin == "client"
	}
	if tagSet != "" {
		when, err := stamp(tagSet)
		if err != nil {
			return err
		}
		a.TagSetUpdatedAt = &when
		if err := i.origin("tag-set", []string{a.ID}, map[string]string{"assetId": a.ID, "updatedAt": tagSet}); err != nil {
			return err
		}
	}
	a.TagSetSource = "cold26"
	a.TagsRevision = max(1, a.TagsRevision)
	if err := store.SaveAssetTags(i.ctx, i.target, *a); err != nil {
		return err
	}
	if protected {
		return i.protect(a.ID, "tags")
	}
	return nil
}

func (i *importer) protect(id, field string) error {
	return i.exec("INSERT INTO asset_human_fields(asset_id,field) VALUES(?,?) ON CONFLICT DO NOTHING", id, field)
}

func (i *importer) assetAuthors(a *model.Asset, refs []link, libraryKind string, work sql.NullString) error {
	active := map[string]link{}
	ids := []string{}
	placeholder := false
	manual := false
	for _, ref := range refs {
		author, ok := i.authors[ref.Key]
		if !ok {
			return errors.New("cold catalog: missing source author identity")
		}
		manual = manual || ref.Origin == "client"
		if author.Placeholder {
			placeholder = true
		} else {
			ids = append(ids, author.ID)
			active[author.ID] = ref
		}
	}
	if len(ids) > 20 {
		return errors.New("cold catalog: source author set exceeds native relation budget")
	}
	directoryID, err := i.cosPlacement(a, active, libraryKind, work)
	if err != nil {
		return err
	}
	if directoryID != "" {
		p := slices.Index(ids, directoryID)
		ids = append([]string{directoryID}, slices.Delete(ids, p, p+1)...)
	}
	if len(ids) > 0 && placeholder {
		if err := i.origin("mixed-placeholder", []string{a.ID}, map[string]string{"assetId": a.ID, "decision": "retain_real_owners"}); err != nil {
			return err
		}
	}
	if manual {
		if err := i.protect(a.ID, "authors"); err != nil {
			return err
		}
	}
	// Only the cold relationship table establishes membership. Existing TXT
	// rules are bound to those exact relations without re-running inference.
	if err := i.bindAssetRules(a, active); err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	a.AuthorID, a.AuthorIDs, a.AuthorStatus = ids[0], ids, "confirmed"
	a.AuthorsRevision = max(1, a.AuthorsRevision)
	return store.SaveAssetEdits(i.ctx, i.target, *a, true, nil)
}

type rule struct{ list, author, work string }

func (i *importer) bindAssetRules(a *model.Asset, active map[string]link) error {
	keys := authorlist.Keys(a.FileName)
	rows, err := i.target.QueryContext(i.ctx, `SELECT list_id,author_id,work FROM author_list_rules
		WHERE (match_key=? AND (extension='' OR extension=?)) OR (pages=1 AND ?!='' AND match_key=?) ORDER BY list_id,ordinal`,
		keys.Stem, keys.Extension, keys.PageBase, keys.PageBase)
	if err != nil {
		return err
	}
	var rules []rule
	for rows.Next() {
		var r rule
		if err := rows.Scan(&r.list, &r.author, &r.work); err != nil {
			rows.Close()
			return err
		}
		rules = append(rules, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	matched := map[string]bool{}
	for _, r := range rules {
		if r.author == "unknown" {
			continue
		}
		if _, ok := active[r.author]; !ok {
			// A text match with no actual attribution is not an attribution.
			if err := i.exec(`INSERT INTO asset_author_overrides(asset_id,author_id,removed) VALUES(?,?,1) ON CONFLICT DO NOTHING`, a.ID, r.author); err != nil {
				return err
			}
			continue
		}
		if err := i.exec("INSERT INTO author_list_matches(list_id,asset_id,author_id,work) VALUES(?,?,?,?)", r.list, a.ID, r.author, r.work); err != nil {
			return err
		}
		matched[r.author] = true
	}
	for id, ref := range active {
		if i.authors[ref.Key].Kind != "regular" {
			continue
		}
		home := ""
		for _, entry := range i.protected[protectedKey{id, a.FileName}] {
			if home != "" && home != entry.list {
				return errors.New("cold catalog: one protected work has multiple owning lists")
			}
			home = entry.list
			entry.matched = true
		}
		if home != "" || ref.Origin == "client" || !matched[id] {
			if err := i.exec("INSERT INTO asset_author_overrides(asset_id,author_id,removed,list_id) VALUES(?,?,0,?)", a.ID, id, home); err != nil {
				return err
			}
		}
	}
	return nil
}

func (i *importer) cosPlacement(a *model.Asset, active map[string]link, kind string, oldWork sql.NullString) (string, error) {
	parts := strings.Split(a.RelativePath, "/")
	id := ""
	if kind == "cos" && len(parts) >= 2 {
		expected := "cos_" + authorlist.IdentityKey(parts[0])
		nameMatch := ""
		matches := 0
		for target, ref := range active {
			author := i.authors[ref.Key]
			if author.Kind == "cos" && author.Name == parts[0] {
				if ref.Key == expected {
					id = target
				}
				nameMatch = target
				matches++
			}
		}
		if id == "" && matches == 1 {
			// A single already-linked COS identity explicitly owns this directory
			// name even if its historical ID was opaque.
			id = nameMatch
		}
		if id == "" && len(active) > 0 {
			if err := i.origin("unbound-cos-directory", []string{a.ID}, map[string]string{"assetId": a.ID, "directory": parts[0]}); err != nil {
				return "", err
			}
		}
	}
	if oldWork.Valid {
		if err := i.portableText(oldWork.String); err != nil {
			return "", err
		}
		if id != "" && len(parts) >= 3 && oldWork.String != "" && parts[1] == oldWork.String {
			directory := strings.Join(parts[:2], "/")
			work := model.CosWork{ID: stableID(i.sourceID, "cos-work", a.LibraryID, directory), LibraryID: a.LibraryID,
				RelativeDirectory: directory, Name: oldWork.String, AuthorID: id, Revision: 1, CreatedAt: a.CreatedAt}
			if err := service.InsertColdCOSWork(i.ctx, i.target, work); err != nil {
				return "", err
			}
			a.CosWorkID, a.CosWorkName = work.ID, work.Name
		} else if err := i.origin("unbound-cos-work", []string{a.ID}, map[string]any{"assetId": a.ID, "cosWork": oldWork.String}); err != nil {
			return "", err
		}
	}
	if id != "" {
		if err := i.exec(`UPDATE asset_records SET cos_work_id=?,data=json_set(data,'$.cosDirectoryAuthorId',?,
			'$.cosWorkId',?,'$.cosWorkName',?) WHERE id=?`, a.CosWorkID, id, a.CosWorkID, a.CosWorkName, a.ID); err != nil {
			return "", err
		}
	}
	return id, nil
}

func (i *importer) assetTaxonomy(a *model.Asset, src sql.NullString, refs []link) error {
	if !src.Valid && len(refs) == 0 {
		return nil
	}
	if err := i.portableText(src.String); err != nil {
		return err
	}
	a.Source, a.SourceIDs, a.Characters, a.CharacterIDs = src.String, []string{}, []string{}, []string{}
	if src.String != "" {
		for _, name := range strings.Split(src.String, "+") {
			if name == "" {
				return errors.New("cold catalog: ambiguous empty component in source name")
			}
			id, err := i.term("", name, nil, a.UpdatedAt)
			if err != nil {
				return err
			}
			a.SourceIDs = append(a.SourceIDs, id)
		}
	}
	if src.Valid {
		if err := i.origin("asset-source", []string{a.ID}, map[string]string{"assetId": a.ID, "source": src.String}); err != nil {
			return err
		}
	}
	for _, ref := range refs {
		a.Characters = append(a.Characters, ref.Key)
		if len(a.SourceIDs) == 1 {
			id, err := i.term(a.SourceIDs[0], ref.Key, nil, a.UpdatedAt)
			if err != nil {
				return err
			}
			a.CharacterIDs = append(a.CharacterIDs, id)
		}
	}
	if len(refs) > 0 && len(a.SourceIDs) != 1 {
		if err := i.origin("unbound-character-parents", []string{a.ID}, map[string]any{"assetId": a.ID, "characters": a.Characters, "source": a.Source}); err != nil {
			return err
		}
	}
	return store.SaveAssetTaxonomy(i.ctx, i.target, *a, store.CompleteTaxonomy(*a))
}
