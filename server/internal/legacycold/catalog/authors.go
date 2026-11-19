package catalog

import (
	"database/sql"
	"errors"
	"strings"

	"qimeng.local/next/server/internal/authorlist"
	"qimeng.local/next/server/internal/domain"
	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/service"
	"qimeng.local/next/server/internal/store"
)

func (i *importer) nativeAuthorID(id string) string {
	// Only the canonical v26 placeholder maps to the native singleton. A
	// person literally named "unknown", or an opaque non-route-safe ID, keeps
	// a separate stable identity via the explicit source alias.
	if id == "unknown" || !domain.ValidName(id) {
		return "cold-" + stableID(i.sourceID, "author", id)
	}
	return id
}

func (i *importer) importAuthors() error {
	return i.each("SELECT id,display_name,type,created_at,followed,origin FROM authors ORDER BY id", func(rows *sql.Rows) error {
		var id, name, kind, created, origin string
		var followed int
		if err := rows.Scan(&id, &name, &kind, &created, &followed, &origin); err != nil {
			return err
		}
		if id == "" || strings.TrimSpace(name) == "" || (kind != "cos" && kind != "regular") || (followed != 0 && followed != 1) {
			return errors.New("cold catalog: invalid source author")
		}
		for _, v := range []string{id, name, origin} {
			if err := i.portableText(v); err != nil {
				return err
			}
		}
		at, err := stamp(created)
		if err != nil {
			return err
		}
		aliases := []string{}
		if kind == "regular" {
			if names, err := authorlist.Names(name); err == nil {
				aliases = names
			}
		}
		state := "confirmed"
		if kind == "cos" || origin == "scanner" {
			state = "candidate"
		}
		a := model.Author{ID: i.nativeAuthorID(id), Kind: kind, Name: name, Aliases: aliases, Following: followed == 1,
			AcquisitionSources: []string{}, Revision: 1, CreatedAt: at, UpdatedAt: at,
			FieldStatus: map[string]string{"identity": state, "homepage": "missing", "biography": "missing"}}
		if id == "其他搁置" && kind == "regular" && len(aliases) > 0 && aliases[0] == "其他搁置" {
			a, err = store.Get[model.Author](i.ctx, i.target, "SELECT data FROM authors WHERE id='unknown'")
			if err != nil {
				return err
			}
			if !a.Placeholder {
				return errors.New("cold catalog: missing native placeholder")
			}
			a.Following = followed == 1
			if err := i.exec("UPDATE authors SET following=?,data=json_set(data,'$.following',json(?)) WHERE id='unknown'",
				a.Following, []string{"false", "true"}[followed]); err != nil {
				return err
			}
		} else if err := service.InsertColdAuthor(i.ctx, i.target, a); err != nil {
			return err
		}
		i.authors[id] = a
		if err := i.exec("INSERT INTO migration_author_aliases(source_id,source_author_id,author_id) VALUES(?,?,?)", i.sourceID, id, a.ID); err != nil {
			return err
		}
		return i.origin("author", []string{id}, map[string]any{"id": id, "displayName": name, "type": kind, "followed": followed,
			"origin": origin, "createdAt": created, "targetId": a.ID})
	})
}

func (i *importer) importTags() error {
	return i.each("SELECT id,name,created_at FROM tags ORDER BY id", func(rows *sql.Rows) error {
		var id, name, created string
		if err := rows.Scan(&id, &name, &created); err != nil {
			return err
		}
		if id == "" || strings.TrimSpace(name) == "" {
			return errors.New("cold catalog: invalid source tag")
		}
		for _, v := range []string{id, name} {
			if err := i.portableText(v); err != nil {
				return err
			}
		}
		if _, err := stamp(created); err != nil {
			return err
		}
		return i.exec("INSERT INTO tags(id,name,revision,created_at,updated_at) VALUES(?,?,1,?,?)", id, name, created, created)
	})
}
