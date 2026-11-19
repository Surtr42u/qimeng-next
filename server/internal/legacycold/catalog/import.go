// Package catalog projects the catalog and owner decisions of a cold v26
// database into the new instance's transaction. It never opens media or a network.
package catalog

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/store"
)

type importer struct {
	ctx       context.Context
	source    *sql.DB
	target    *sql.Tx
	sourceID  string
	roots     []string
	authors   map[string]model.Author
	settings  map[string]setting
	protected map[protectedKey][]*protectedWork
}

// Import requires an immutable, read-only source, and a fresh native target
// containing the libraries, verified asset versions and native unknown author.
// The caller owns the transaction and rolls it back on any error.
func Import(ctx context.Context, source *sql.DB, target *sql.Tx, sourceID string) error {
	if sourceID == "" {
		return diagnostic("source_identity", sourceProblem("source_identity_missing"))
	}
	i := &importer{ctx: ctx, source: source, target: target, sourceID: sourceID,
		authors: make(map[string]model.Author), settings: make(map[string]setting)}
	for _, step := range []struct {
		name string
		run  func() error
	}{
		{"libraries", i.loadRoots}, {"authors", i.importAuthors}, {"tags", i.importTags},
		{"kv_settings", i.readSettings}, {"user_preferences", i.importSettings},
		{"author_lists", i.importLists}, {"asset_relations", i.importAssets},
		{"creator_profiles", i.importProfiles}, {"profile_revisions", i.importProfileFloors},
	} {
		if err := step.run(); err != nil {
			return diagnostic(step.name, err)
		}
	}
	return nil
}

func (i *importer) each(query string, read func(*sql.Rows) error) error {
	rows, err := i.source.QueryContext(i.ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := read(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (i *importer) exec(statement string, args ...any) error {
	_, err := i.target.ExecContext(i.ctx, statement, args...)
	return err
}

// The JSON tuple makes boundaries unambiguous even for opaque v26 IDs.
func stableID(parts ...string) string {
	raw, _ := json.Marshal(parts)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:16])
}

func (i *importer) origin(kind string, identity []string, fact any) error {
	raw, err := store.Encode(fact)
	if err != nil {
		return err
	}
	return i.exec(`INSERT INTO migration_relation_origins(input_id,kind,identity,original) VALUES(?,?,?,?)`,
		i.sourceID, "cold26/"+kind, stableID(identity...), raw)
}

func (i *importer) loadRoots() error {
	return i.each("SELECT root_path FROM libraries", func(rows *sql.Rows) error {
		var root string
		if err := rows.Scan(&root); err != nil {
			return err
		}
		if root = strings.TrimRight(strings.ReplaceAll(strings.ToLower(root), "\\", "/"), "/"); len(root) > 1 {
			i.roots = append(i.roots, root)
		}
		return nil
	})
}

func stamp(value string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, sourceProblem("source_timestamp_invalid")
	}
	return t.UTC(), nil
}
