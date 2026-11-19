package catalog

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"qimeng.local/next/server/internal/analytics"
	"qimeng.local/next/server/internal/service"
	"qimeng.local/next/server/internal/sourcerules"
	"qimeng.local/next/server/internal/store"
)

type setting struct {
	Raw json.RawMessage
	At  string
}

func decode(raw json.RawMessage, target any) error {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, target) != nil {
		return errors.New("cold catalog: invalid business setting")
	}
	return nil
}

func (i *importer) readSettings() error {
	// The value of an unknown or host-only setting never crosses this query.
	// Profiles are streamed separately because their offline avatars can be large.
	return i.each(`SELECT key,updated_at,CASE WHEN key IN (
		'recommend_prefs','client_config','backup_schedule','custom_sources','custom_source_groups',
		'custom_stop_words','author_source_vocabulary','imported_txt_sources','imported_txt_upload_entries','legacy_import_batch'
		) THEN value END FROM kv_settings
		WHERE substr(key,1,16)!='creator_profile:' AND substr(key,1,25)!='creator_profile_revision:' ORDER BY key`, func(rows *sql.Rows) error {
		var key, at string
		var raw []byte
		if err := rows.Scan(&key, &at, &raw); err != nil {
			return err
		}
		if _, err := stamp(at); err != nil {
			return err
		}
		if raw == nil {
			switch key {
			case "client_logs", "author_mirror", "library_revision", "enrichment_engine_version":
				return i.origin("excluded-setting", []string{key}, map[string]string{"key": key, "reason": "previous_host_state"})
			default:
				return errors.New("cold catalog: unrecognized v26 setting; value withheld")
			}
		}
		if key == "legacy_import_batch" {
			if err := i.portableText(string(raw)); err != nil {
				return err
			}
			return i.origin("archived-setting", []string{key}, map[string]string{"key": key, "updatedAt": at, "value": string(raw)})
		}
		if err := i.portableJSON(raw, false); err != nil {
			return err
		}
		i.settings[key] = setting{raw, at}
		kind := "setting"
		if key == "backup_schedule" {
			kind = "archived-setting" // Native backup targets use independent ownership and configuration.
		}
		return i.origin(kind, []string{key}, map[string]any{"key": key, "updatedAt": at, "value": json.RawMessage(raw)})
	})
}

func (i *importer) saveSetting(key string, v any) error {
	raw, err := store.Encode(v)
	if err != nil {
		return err
	}
	return i.exec("INSERT INTO settings(key,value) VALUES(?,?)", key, raw)
}

func (i *importer) importSettings() error {
	if v, ok := i.settings["recommend_prefs"]; ok {
		// v26 fills missing and null dimensions with these same documented defaults.
		weights := analytics.DefaultWeights
		if err := decode(v.Raw, &weights); err != nil || !weights.Valid() {
			return errors.New("cold catalog: invalid recommendation weights")
		}
		if err := i.saveSetting("recommendation_preferences", service.RecommendationPreferences{Revision: 1, Weights: weights}); err != nil {
			return err
		}
	}
	if v, ok := i.settings["client_config"]; ok {
		var cfg service.SettingsUpdate
		if err := decode(v.Raw, &cfg); err != nil || cfg.Scan == nil || cfg.Scan.Workers == nil || cfg.Scan.ThumbEdge == nil ||
			cfg.Upload == nil || cfg.Upload.AutoAccept == nil || *cfg.Scan.Workers < 0 || *cfg.Scan.Workers > 32 ||
			*cfg.Scan.ThumbEdge < 200 || *cfg.Scan.ThumbEdge > 1600 {
			return errors.New("cold catalog: invalid scan or upload settings")
		}
		native := service.Settings{Revision: 1, Scan: service.ScanSettings{Workers: *cfg.Scan.Workers, ThumbEdge: *cfg.Scan.ThumbEdge},
			Upload: service.UploadSettings{AutoAccept: *cfg.Upload.AutoAccept}}
		if err := i.saveSetting("operational_settings", native); err != nil {
			return err
		}
	}
	if v, ok := i.settings["author_source_vocabulary"]; ok {
		var words []string
		if err := decode(v.Raw, &words); err != nil {
			return err
		}
		if err := i.saveSetting("acquisition_vocabulary", service.AcquisitionVocabulary{Revision: 1, Sources: words}); err != nil {
			return err
		}
	}
	return i.importRules()
}

func (i *importer) importRules() error {
	layer := sourcerules.Layer{Groups: []sourcerules.Group{}, StopWords: []string{}}
	groups, hasGroups := i.settings["custom_source_groups"]
	names, hasNames := i.settings["custom_sources"]
	words, hasWords := i.settings["custom_stop_words"]
	if !hasGroups && !hasNames && !hasWords {
		return nil
	}
	// Birth time of a newly materialized native term is the first observed
	// time in its evidence, never a claim about an older entity creation.
	if hasGroups {
		if err := decode(groups.Raw, &layer.Groups); err != nil {
			return err
		}
	}
	known := map[string]bool{}
	for n := range layer.Groups {
		g := &layer.Groups[n]
		known[g.Canonical] = true
		if g.Characters == nil {
			g.Characters = []sourcerules.Character{}
		}
		if g.Variants == nil {
			g.Variants = []string{}
		}
		for j := range g.Characters {
			if g.Characters[j].Aliases == nil {
				g.Characters[j].Aliases = []string{}
			}
		}
	}
	if hasNames {
		var bare []string
		if err := decode(names.Raw, &bare); err != nil {
			return err
		}
		for _, name := range bare {
			if !known[name] {
				layer.Groups = append(layer.Groups, sourcerules.Group{Canonical: name, Variants: []string{}, Characters: []sourcerules.Character{}})
			}
		}
	}
	if hasWords {
		if err := decode(words.Raw, &layer.StopWords); err != nil {
			return err
		}
	}
	normalized, err := sourcerules.Normalize(layer)
	if err != nil {
		return errors.New("cold catalog: source dictionary exceeds native rule contract")
	}
	raw, err := store.Encode(normalized)
	if err != nil {
		return err
	}
	if err := i.exec("UPDATE source_rule_settings SET layer=?,revision=revision+1,run_state='pending' WHERE id=1", raw); err != nil {
		return err
	}
	for _, g := range normalized.Groups {
		observed := names.At
		if known[g.Canonical] {
			observed = groups.At
		}
		at, err := stamp(observed)
		if err != nil {
			return err
		}
		parent, err := i.term("", g.Canonical, g.Variants, at)
		if err != nil {
			return err
		}
		for _, c := range g.Characters {
			if _, err := i.term(parent, c.Canonical, c.Aliases, at); err != nil {
				return err
			}
		}
	}
	return nil
}

func (i *importer) term(parent, name string, aliases []string, at time.Time) (string, error) {
	id := stableID(i.sourceID, "taxonomy", parent, name)
	base := service.Source{ID: id, Name: name, Aliases: append([]string{}, aliases...), Revision: 1, CreatedAt: at, UpdatedAt: at}
	if parent == "" {
		raw, err := store.Encode(base)
		if err != nil {
			return "", err
		}
		return id, i.exec("INSERT INTO sources(id,name,data) VALUES(?,?,?) ON CONFLICT(id) DO NOTHING", id, name, raw)
	}
	raw, err := store.Encode(service.Character{Source: base, SourceID: parent})
	if err != nil {
		return "", err
	}
	return id, i.exec("INSERT INTO characters(id,source_id,name,data) VALUES(?,?,?,?) ON CONFLICT(id) DO NOTHING", id, parent, name, raw)
}
