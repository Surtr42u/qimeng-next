package legacycold

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"regexp"
	"strconv"
	"strings"
)

type TableCount struct {
	Table       string `json:"table"`
	Rows        int64  `json:"rows"`
	Disposition string `json:"disposition"`
}

type OriginCount struct {
	Kind string `json:"kind"`
	Rows int64  `json:"rows"`
}

type Report struct {
	Format           string        `json:"format"`
	State            string        `json:"state"`
	Stage            string        `json:"stage"`
	SourceID         string        `json:"sourceId,omitempty"`
	RootSelection    string        `json:"rootSelectionSHA256,omitempty"`
	SourceTimezone   string        `json:"sourceTimezone"`
	Tables           []TableCount  `json:"tables"`
	Origins          []OriginCount `json:"relationOrigins,omitempty"`
	Libraries        int           `json:"libraries"`
	LiveAssets       int           `json:"liveAssets"`
	TrashedAssets    int           `json:"trashedAssets"`
	OriginalBytes    int64         `json:"originalBytes"`
	OtherFiles       int64         `json:"unarchivedNonMediaFiles"`
	ExcludedEntries  int64         `json:"excludedOldScannerEntries"`
	NonMediaHandling string        `json:"nonMediaHandling"`
	Problem          *issue        `json:"problem,omitempty"`
}

var tables = map[string]string{
	"schema_migrations":       "validated_v26_ledger",
	"libraries":               "native_without_old_absolute_roots",
	"assets":                  "native_current_original_and_source_evidence",
	"asset_characters":        "native_and_source_evidence",
	"tags":                    "native_and_source_evidence",
	"asset_tags":              "native_and_source_evidence",
	"authors":                 "native_and_source_evidence",
	"asset_authors":           "native_and_source_evidence",
	"view_events":             "native_and_source_evidence",
	"portable_event_identity": "source_identity_evidence",
	"likes":                   "native_and_source_evidence",
	"favorites":               "native_and_source_evidence",
	"timeline_tags":           "native_and_source_evidence",
	"daily_shown":             "native_and_source_evidence",
	"asset_daily_stats":       "source_evidence_derived_totals",
	"kv_settings":             "per_key_projection_or_sealed_source",
	"upload_receipts":         "allowlisted_history_full_record_in_sealed_source",
	"maintenance_jobs":        "allowlisted_history_full_record_in_sealed_source",
	"maintenance_steps":       "allowlisted_history_full_record_in_sealed_source",
	"maintenance_artifacts":   "allowlisted_history_full_record_in_sealed_source",
	"device_apply_tasks":      "allowlisted_history_full_record_in_sealed_source",
	"device_apply_segments":   "allowlisted_history_full_record_in_sealed_source",
	"ai_audit":                "allowlisted_history_full_record_in_sealed_source",
	"users":                   "old_authority_excluded",
	"auth_sessions":           "old_authority_excluded",
	"ai_clients":              "old_authority_excluded",
	"trash_items":             "retired_table_must_be_empty_trash_on_disk",
	"assets_fts":              "derived_search_index",
}

var derived = map[string]bool{
	"sqlite_sequence": true, "sqlite_stat1": true, "sqlite_stat4": true,
	"assets_fts_data": true, "assets_fts_idx": true, "assets_fts_content": true,
	"assets_fts_docsize": true, "assets_fts_config": true,
}

var plainTable = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,95}$`)

func reportTable(name string) string {
	if plainTable.MatchString(name) {
		return name
	}
	sum := sha256.Sum256([]byte(name))
	return "sha256-" + hex.EncodeToString(sum[:])
}

func inventoryTables(ctx context.Context, db *sql.DB, report *Report) error {
	rows, err := db.QueryContext(ctx, `SELECT name,type FROM pragma_table_list
		WHERE schema='main' AND type!='view' AND name!='sqlite_schema' ORDER BY name`)
	if err != nil {
		return failed("source_schema_unreadable")
	}
	defer rows.Close()
	seen := map[string]bool{}
	var blocked error
	for rows.Next() {
		var name, kind string
		if err = rows.Scan(&name, &kind); err != nil {
			return err
		}
		if len(seen) >= 1024 {
			return failed("too_many_source_tables")
		}
		seen[name] = true
		entry := TableCount{Table: reportTable(name), Disposition: tables[name]}
		if derived[name] {
			entry.Disposition = "derived_index_or_sqlite_bookkeeping"
		}
		if kind == "virtual" && name != "assets_fts" {
			entry.Rows, entry.Disposition = -1, "unsupported_virtual_table"
			blocked = locate(failed("unsupported_virtual_table"), entry.Table, 0, "")
		} else {
			query := `SELECT count(*) FROM "` + strings.ReplaceAll(name, `"`, `""`) + `"`
			if err = db.QueryRowContext(ctx, query).Scan(&entry.Rows); err != nil {
				return locate(failed("source_count_failed"), entry.Table, 0, "")
			}
			if entry.Disposition == "" {
				entry.Disposition = "unrecognized_empty"
				if entry.Rows != 0 {
					entry.Disposition = "unrecognized_nonempty"
					blocked = locate(failed("unknown_nonempty_table"), entry.Table, 0, "")
				}
			}
			if name == "trash_items" && entry.Rows != 0 {
				blocked = locate(failed("unsupported_retired_trash_rows"), name, 0, "")
			}
		}
		report.Tables = append(report.Tables, entry)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	for name := range tables {
		if !seen[name] {
			return locate(failed("required_v26_table_missing"), name, 0, "")
		}
	}
	if blocked != nil {
		return blocked
	}
	var integrity string
	if err = db.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&integrity); err != nil || integrity != "ok" {
		return failed("source_integrity_check_failed")
	}
	var orphan bool
	if err = db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM pragma_foreign_key_check)").Scan(&orphan); err != nil || orphan {
		return failed("source_foreign_key_check_failed")
	}
	return nil
}

// Operational ledgers carry capabilities and private host paths. Preserve only
// typed historical facts. Parent rowids locate the complete record in exactly
// the sealed source identified by its digest, including after cross-instance restore.
var history = []struct{ table, fields string }{
	{"ai_audit", `'method',CASE WHEN method IN ('GET','HEAD','POST','PUT','PATCH','DELETE','OPTIONS') THEN method END,
		'httpStatus',cast(status_code AS integer),'at',strftime('%Y-%m-%dT%H:%M:%fZ',created_at)`},
	{"upload_receipts", `'assetRow',(SELECT rowid FROM assets a WHERE a.asset_id=upload_receipts.asset_id),
		'bytes',cast(size_bytes AS integer),'completedAt',cast(completed_at AS integer)`},
	{"maintenance_jobs", `'kind',CASE WHEN kind IN ('asset_research','author_cleanup','backup_export','custom') THEN kind END,
		'status',CASE WHEN status IN ('queued','inventory','research','planned','applying','verifying','waiting_evidence','waiting_authorization','waiting_device','paused','partial','completed','failed','cancelled') THEN status END,
		'createdAt',strftime('%Y-%m-%dT%H:%M:%fZ',created_at),'finishedAt',strftime('%Y-%m-%dT%H:%M:%fZ',finished_at)`},
	{"maintenance_steps", `'jobRow',(SELECT rowid FROM maintenance_jobs j WHERE j.id=maintenance_steps.job_id),
		'status',CASE WHEN status IN ('pending','running','completed','failed','skipped') THEN status END,'attempt',cast(attempt AS integer),
		'startedAt',strftime('%Y-%m-%dT%H:%M:%fZ',started_at),'finishedAt',strftime('%Y-%m-%dT%H:%M:%fZ',finished_at)`},
	{"maintenance_artifacts", `'jobRow',(SELECT rowid FROM maintenance_jobs j WHERE j.id=maintenance_artifacts.job_id),
		'bytes',cast(bytes AS integer),'createdAt',strftime('%Y-%m-%dT%H:%M:%fZ',created_at)`},
	{"device_apply_tasks", `'protocolVersion',cast(protocol_version AS integer),
		'status',CASE WHEN status IN ('pending','claimed','applied','partial','failed','cancelled','expired') THEN status END,
		'createdAt',strftime('%Y-%m-%dT%H:%M:%fZ',created_at),'finishedAt',strftime('%Y-%m-%dT%H:%M:%fZ',finished_at)`},
	{"device_apply_segments", `'taskRow',(SELECT rowid FROM device_apply_tasks t WHERE t.id=device_apply_segments.task_id),
		'kind',CASE WHEN kind IN ('masterTxt','v2Backup') THEN kind END,'bytes',cast(bytes AS integer)`},
}

func importHistory(ctx context.Context, source *sql.DB, target *sql.Tx, sourceID string) error {
	for _, projection := range history {
		rows, err := source.QueryContext(ctx, "SELECT rowid,json_object("+projection.fields+") FROM "+projection.table+" ORDER BY rowid")
		if err != nil {
			return locate(failed("history_query_failed"), projection.table, 0, "")
		}
		err = func() error {
			defer rows.Close()
			for rows.Next() {
				var row int64
				var facts string
				if err := rows.Scan(&row, &facts); err != nil {
					return err
				}
				_, err := target.ExecContext(ctx, `INSERT INTO migration_relation_origins(input_id,kind,identity,original)
					VALUES(?,?,?,?)`, sourceID, "cold26/history/"+projection.table, strconv.FormatInt(row, 10), facts)
				if err != nil {
					return err
				}
			}
			return rows.Err()
		}()
		if err != nil {
			return locate(failed("history_projection_failed"), projection.table, 0, "")
		}
	}
	return nil
}
