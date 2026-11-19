package phonerelink

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"strings"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/service"
	"qimeng.local/next/server/internal/store"
)

func present(ctx context.Context, db store.Querier, query string, args ...any) (bool, error) {
	var yes bool
	err := db.QueryRowContext(ctx, query, args...).Scan(&yes)
	return yes, err
}

func tables(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx, "SELECT name FROM main.sqlite_schema WHERE type='table' ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

// Both catalogs have already passed the native schema gate, including their
// actual SQLite objects. Each durable table has a key. EXCEPT in both directions
// compares every field, including historical IDs, blobs and origin records.
func sameTable(ctx context.Context, db *sql.DB, other, name string) error {
	t := `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
	local := "SELECT * FROM main." + t
	remote := "SELECT * FROM " + other + "." + t
	different, err := present(ctx, db, "SELECT (SELECT count(*) FROM main."+t+")!=(SELECT count(*) FROM "+other+"."+t+") OR EXISTS("+local+" EXCEPT "+remote+") OR EXISTS("+remote+" EXCEPT "+local+")")
	if err != nil {
		return err
	}
	if different {
		return refusal("catalog_differs_from_verified_restore")
	}
	return nil
}

const installedSchema = 45

func catalogVersion(ctx context.Context, db *sql.DB) (int, error) {
	var version int
	err := db.QueryRowContext(ctx, "SELECT max(version) FROM schema_migrations").Scan(&version)
	return version, err
}

func sealedVersion(ctx context.Context, name string) (int, error) {
	db, err := store.OpenDatabase(name, store.SnapshotDatabase)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	if err := store.ValidateBackupSchema(ctx, db); err != nil {
		return 0, errors.Join(refusal("incompatible_or_damaged_catalog"), err)
	}
	return catalogVersion(ctx, db)
}

func requireInstalledCatalog(ctx context.Context, name string) error {
	version, err := sealedVersion(ctx, name)
	if err != nil {
		return err
	}
	if version != installedSchema {
		return refusal("installed_schema_45_required")
	}
	return nil
}

// The released 0044 converter and restore used the same native reset policy.
// Compare their complete tables before the production 0045 migration creates
// its new relations. Only the extracted working snapshot is changed.
func matchSchema44(ctx context.Context, snapshot, restored string, receipt service.RestoreResult) error {
	db, err := store.OpenDatabase(snapshot, store.CatalogDatabase)
	if err != nil {
		return err
	}
	version, err := catalogVersion(ctx, db)
	if err == nil && version != 44 {
		err = refusal("archive_and_restored_schema_differ")
	}
	if err == nil {
		err = matchRestored(ctx, db, restored, receipt)
	}
	return errors.Join(err, db.Close())
}

// Re-run the official catalog restore on the independently verified archive
// snapshot. A supplied receipt or a matching media list cannot vouch for
// edits to unconnected events, preferences, history or the identity map.
func matchRestored(ctx context.Context, db *sql.DB, restoredName string, receipt service.RestoreResult) error {
	pending, err := present(ctx, db, `SELECT EXISTS(SELECT 1 FROM device_apply_tasks WHERE state IN ('pending','claimed'))
		OR EXISTS(SELECT 1 FROM research_provider_calls WHERE state='pending')`)
	if err != nil {
		return err
	}
	if pending {
		// Restoring these in-flight records creates a new cancellation time.
		return refusal("native_archive_has_inflight_device_or_research_work")
	}
	if err := service.ResetRestoredCatalog(ctx, db, receipt.InstanceID); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, "ATTACH DATABASE ? AS restored", store.FileURI(restoredName)+"?mode=ro&immutable=1"); err != nil {
		return err
	}
	names, err := tables(ctx, db)
	if err != nil {
		return err
	}
	for _, name := range names {
		if err := sameTable(ctx, db, "restored", name); err != nil {
			return err
		}
	}
	_, err = db.ExecContext(ctx, "DETACH DATABASE restored")
	return err
}

func isInstanceID(s string) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == 16 && strings.ToLower(s) == s
}

func defaultAuthor(ctx context.Context, db *sql.DB) error {
	var raw string
	if err := db.QueryRowContext(ctx, "SELECT data FROM phone.authors WHERE id='unknown' AND name='其他搁置' AND following=0").Scan(&raw); err != nil {
		return errors.Join(refusal("target_default_author_changed"), err)
	}
	var a model.Author
	d := json.NewDecoder(strings.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&a); err != nil || a.CreatedAt.IsZero() || a.UpdatedAt.IsZero() {
		return refusal("target_default_author_changed")
	}
	want := model.Author{ID: "unknown", Name: "其他搁置", Aliases: []string{}, Revision: 1, Placeholder: true,
		FieldStatus: map[string]string{"identity": "missing", "biography": "missing", "homepage": "missing"}, CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt}
	if !reflect.DeepEqual(a, want) {
		return refusal("target_default_author_changed")
	}
	return nil
}

func checkTarget(ctx context.Context, db *sql.DB, targetName string, receipt service.RestoreResult) (string, error) {
	if _, err := db.ExecContext(ctx, "ATTACH DATABASE ? AS phone", store.FileURI(targetName)+"?mode=ro&immutable=1"); err != nil {
		return "", err
	}
	names, err := tables(ctx, db)
	if err != nil {
		return "", err
	}
	// All other current tables, including queues, stats, diagnostics and
	// favorites, must be empty. The placeholder and migration-only rows have
	// narrowly defined contents. Default workers may have finished their pass.
	for _, table := range names {
		var check string
		switch table {
		case "schema_version", "schema_migrations":
			if err := sameTable(ctx, db, "phone", table); err != nil {
				return "", errors.Join(refusal("target_migration_history_differs"), err)
			}
			continue
		case "authors":
			check = "SELECT count(*)!=1 FROM phone.authors"
		case "author_lookup_keys":
			check = "SELECT count(*)!=1 OR count(*) FILTER(WHERE identity_key='其他搁置' AND author_id='unknown')!=1 FROM phone.author_lookup_keys"
		case "settings":
			check = `SELECT EXISTS(SELECT 1 FROM phone.settings WHERE key NOT IN
				('instance_id','operational_settings','recommendation_preferences','acquisition_vocabulary'))`
		case "credentials":
			// A scoped grant may name an object from another catalog. Only
			// standalone target credentials can safely survive this operation.
			check = `SELECT EXISTS(SELECT 1 FROM phone.credentials WHERE json_extract(data,'$.id') IS NOT id
				OR length(token_hash)!=64 OR token_hash GLOB '*[^0-9a-f]*' OR json_type(data,'$.token') IS NOT NULL
				OR coalesce(json_extract(data,'$.batchId'),'')!='' OR coalesce(json_extract(data,'$.authorTaskId'),'')!=''
				OR coalesce(json_extract(data,'$.assetTaskId'),'')!='' OR coalesce(json_extract(data,'$.deviceId'),'')!=''
				OR coalesce(json_extract(data,'$.parentDelegationId'),'')!='' OR coalesce(json_extract(data,'$.generation'),0)!=0)`
		case "backup_business_state":
			check = "SELECT count(*)!=1 OR count(*) FILTER(WHERE id=1 AND revision>=1)!=1 FROM phone.backup_business_state"
		case "recommendation_epoch":
			check = "SELECT count(*)!=1 OR count(*) FILTER(WHERE id=1 AND generation=1)!=1 FROM phone.recommendation_epoch"
		case "source_rule_settings":
			check = `SELECT count(*)!=1 OR count(*) FILTER(WHERE id=1 AND revision=1 AND layer='{"groups":[],"stopWords":[]}'
				AND engine_version=1 AND run_state IN ('pending','completed') AND cursor='' AND processed=0 AND matched=0 AND ambiguous=0 AND error_code='')!=1
				FROM phone.source_rule_settings`
		default:
			check = `SELECT EXISTS(SELECT 1 FROM phone."` + strings.ReplaceAll(table, `"`, `""`) + `")`
		}
		nonempty, err := present(ctx, db, check)
		if err != nil {
			return "", err
		}
		if nonempty {
			return "", refusal("target_has_business_or_nondefault_state")
		}
	}
	if err := defaultAuthor(ctx, db); err != nil {
		return "", err
	}
	var id string
	if err := db.QueryRowContext(ctx, "SELECT value FROM phone.settings WHERE key='instance_id'").Scan(&id); err != nil {
		return "", err
	}
	if !isInstanceID(id) || id == receipt.InstanceID || id == receipt.SourceInstanceID {
		return "", refusal("target_instance_identity_not_independent")
	}
	conflict, err := present(ctx, db, `SELECT EXISTS(SELECT 1 FROM phone.settings p JOIN main.settings s ON p.key=s.key
		WHERE p.key!='instance_id' AND p.value IS NOT s.value)`)
	if err != nil {
		return "", err
	}
	if conflict {
		return "", refusal("target_setting_conflicts_with_source")
	}
	return id, nil
}
