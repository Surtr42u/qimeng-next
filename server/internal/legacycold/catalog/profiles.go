package catalog

import (
	"database/sql"
	"encoding/json"
	"net/mail"
	"strconv"
	"time"

	"qimeng.local/next/server/internal/service"
	"qimeng.local/next/server/internal/store"
)

type coldProfile struct {
	AuthorID     string   `json:"authorId"`
	Status       string   `json:"status"`
	Revision     int64    `json:"revision"`
	Biography    string   `json:"biography"`
	Homepage     string   `json:"homepageUrl"`
	Evidence     []string `json:"evidenceUrls"`
	FeedURL      string   `json:"feedUrl"`
	TrackUpdates *bool    `json:"trackUpdates"`
	LastChecked  string   `json:"lastCheckedAt"`
	Updates      []struct {
		ID, Title, URL string
		PublishedAt    string `json:"publishedAt"`
	} `json:"updates"`
}

func (i *importer) importProfiles() error {
	return i.each(`SELECT substr(key,17),updated_at,CASE WHEN length(CAST(value AS BLOB))<=3145728 THEN value END
		FROM kv_settings WHERE substr(key,1,16)='creator_profile:' ORDER BY key`, func(rows *sql.Rows) error {
		var id, updated string
		var raw []byte
		if err := rows.Scan(&id, &updated, &raw); err != nil {
			return err
		}
		a, exists := i.authors[id]
		if !exists || len(raw) == 0 {
			return sourceProblem("profile_owner_or_size_invalid")
		}
		portable, decisions, err := i.inspectJSON(raw, "creator_profile")
		if err != nil {
			return err
		}
		var p coldProfile
		var fields map[string]json.RawMessage
		if err := decode(portable, &p); err != nil {
			return err
		}
		if err := decode(portable, &fields); err != nil {
			return err
		}
		if p.AuthorID != id || p.Revision < 0 || (p.Status == "verified" && (p.Homepage == "" || len(p.Evidence) == 0)) {
			return sourceProblem("profile_identity_or_status_invalid")
		}
		at, err := stamp(updated)
		if err != nil {
			return err
		}
		var avatar []byte
		if img := fields["avatarData"]; img != nil {
			if json.Unmarshal(img, &avatar) != nil {
				return sourceProblem("avatar_encoding_invalid")
			}
			delete(fields, "avatarData")
		}
		body, err := json.Marshal(fields)
		if err != nil {
			return err
		}
		prepared, err := service.PrepareImportedAuthorProfile(i.ctx, id, body, avatar)
		if err != nil {
			return err
		}
		archiveID := stableID(i.sourceID, "profile", id)
		out, err := service.RestoreColdProfile(i.ctx, i.target, a.ID, a.Name, archiveID, prepared, at)
		if err != nil {
			return err
		}
		fact := map[string]any{"authorId": id, "updatedAt": updated, "archiveId": archiveID, "value": portable, "projection": out}
		if len(decisions) > 0 {
			source := map[string]string{"sourceId": i.sourceID, "table": "kv_settings", "key": "creator_profile:" + id}
			fact["valueDisposition"], fact["sealedSource"], fact["fields"] = "portable_query_redacted", source, decisions
			for _, decision := range decisions {
				if err := i.origin("profile-text-disposition", []string{id, decision.Field},
					map[string]any{"authorId": id, "sealedSource": source, "decision": decision}); err != nil {
					return err
				}
			}
		}
		if err := i.origin("author-profile", []string{id}, fact); err != nil {
			return err
		}
		if a.Placeholder {
			return nil
		}
		if p.Status == "verified" {
			a.Homepage, a.Biography = p.Homepage, p.Biography
			a.FieldStatus["identity"] = "confirmed"
		} else if p.Status == "candidate" {
			a.FieldStatus["identity"] = "candidate"
		}
		for field, value := range map[string]string{"homepage": p.Homepage, "biography": p.Biography} {
			if value != "" {
				if p.Status == "verified" {
					a.FieldStatus[field] = "confirmed"
				} else {
					a.FieldStatus[field] = "candidate"
				}
			}
		}
		a.UpdatedAt = at
		value, err := store.Encode(a)
		if err != nil {
			return err
		}
		if err := i.exec("UPDATE authors SET data=? WHERE id=?", value, a.ID); err != nil {
			return err
		}
		if p.TrackUpdates == nil && p.FeedURL == "" && len(p.Updates) == 0 {
			return nil
		}
		sub := service.AuthorUpdates{AuthorID: a.ID, Revision: 1, FeedURL: p.FeedURL,
			TrackUpdates: p.TrackUpdates != nil && *p.TrackUpdates, Items: []service.AuthorUpdateEntry{}}
		if sub.TrackUpdates && p.LastChecked != "" {
			when, err := stamp(p.LastChecked)
			if err != nil {
				return err
			}
			sub.CheckedAt = &when
		}
		for n, item := range p.Updates {
			entry := service.AuthorUpdateEntry{ID: item.ID, Title: item.Title, URL: item.URL}
			if item.PublishedAt != "" {
				when, err := time.Parse(time.RFC3339Nano, item.PublishedAt)
				if err != nil {
					when, err = mail.ParseDate(item.PublishedAt)
				}
				if err == nil {
					entry.PublishedAt = &when
				} else if err := i.origin("unparsed-feed-date", []string{id, strconv.Itoa(n)},
					map[string]any{"authorId": id, "entry": n, "publishedAt": item.PublishedAt}); err != nil {
					return err
				}
			}
			sub.Items = append(sub.Items, entry)
		}
		return service.SaveColdSubscription(i.ctx, i.target, sub)
	})
}

func (i *importer) importProfileFloors() error {
	return i.each(`SELECT substr(k.key,26),k.value,k.updated_at,EXISTS(SELECT 1 FROM kv_settings p
		WHERE p.key='creator_profile:'||substr(k.key,26)) FROM kv_settings k
		WHERE substr(k.key,1,25)='creator_profile_revision:' ORDER BY k.key`, func(rows *sql.Rows) error {
		var id, value, updated string
		var profileExists bool
		if err := rows.Scan(&id, &value, &updated, &profileExists); err != nil {
			return err
		}
		if err := i.portableText(id); err != nil {
			return err
		}
		floor, err := strconv.ParseInt(value, 10, 64)
		if id == "" || err != nil || floor < 0 {
			return sourceProblem("profile_floor_invalid")
		}
		at, err := stamp(updated)
		if err != nil {
			return err
		}
		if err := i.origin("profile-revision-floor", []string{id}, map[string]any{"authorId": id, "value": value, "updatedAt": updated}); err != nil {
			return err
		}
		if a, live := i.authors[id]; live {
			if floor == 0 || profileExists || a.Placeholder {
				return nil
			}
			return service.RestoreColdEmptyProfile(i.ctx, i.target, a.ID, floor, at)
		}
		targetID := i.nativeAuthorID(id)
		if err := i.exec("INSERT INTO migration_author_aliases(source_id,source_author_id,author_id) VALUES(?,?,?)", i.sourceID, id, targetID); err != nil {
			return err
		}
		return i.exec("INSERT INTO author_tombstones(id,revision,deleted_at) VALUES(?,?,?)", targetID, floor, updated)
	})
}
