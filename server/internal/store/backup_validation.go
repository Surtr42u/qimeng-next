package store

import (
	"context"
	"database/sql"
	"errors"
	"sort"
)

// ValidateBackupSchema checks both the recorded migrations and the actual
// SQLite objects. A copied migration ledger alone cannot authorize added
// triggers/views or changed columns in an externally supplied database.
func ValidateBackupSchema(ctx context.Context, db *sql.DB) error {
	var objectType string
	if e := db.QueryRowContext(ctx, "SELECT type FROM sqlite_schema WHERE name='schema_migrations'").Scan(&objectType); e != nil {
		return e
	}
	if objectType != "table" {
		return errors.New("backup migration ledger is not a table")
	}
	type record struct {
		version    int
		name, hash string
	}
	records := []record{}
	rows, e := db.QueryContext(ctx, "SELECT version,name,sha256 FROM schema_migrations ORDER BY version")
	if e != nil {
		return e
	}
	for rows.Next() {
		var r record
		if e = rows.Scan(&r.version, &r.name, &r.hash); e != nil {
			rows.Close()
			return e
		}
		records = append(records, r)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	if len(records) == 0 || len(records) > 10000 {
		return errors.New("invalid backup migration ledger")
	}
	steps, e := readSchemaMigrations(migrations)
	if e != nil {
		return e
	}
	known := make(map[int]schemaMigration, len(steps))
	for _, step := range steps {
		known[step.version] = step
	}
	expected, e := sql.Open(sqliteDriverName, ":memory:")
	if e != nil {
		return e
	}
	defer expected.Close()
	expected.SetMaxOpenConns(1)
	if _, e = expected.ExecContext(ctx, migrationLedgerSQL); e != nil {
		return e
	}
	for _, r := range records {
		step, ok := known[r.version]
		if !ok || step.name != r.name {
			return errors.New("backup requires an unknown migration")
		}
		if step.checksum != r.hash {
			return errors.New("backup migration checksum mismatch")
		}
		if _, e = expected.ExecContext(ctx, step.sql); e != nil {
			return e
		}
	}
	objects := func(source *sql.DB) ([]string, error) {
		rows, e := source.QueryContext(ctx, "SELECT type,name,tbl_name,coalesce(sql,'') FROM sqlite_schema ORDER BY type,name")
		if e != nil {
			return nil, e
		}
		defer rows.Close()
		values := []string{}
		for rows.Next() {
			var kind, name, table, definition string
			if e = rows.Scan(&kind, &name, &table, &definition); e != nil {
				return nil, e
			}
			values = append(values, kind+"\x00"+name+"\x00"+table+"\x00"+definition)
		}
		return values, rows.Err()
	}
	want, e := objects(expected)
	if e != nil {
		return e
	}
	actual, e := objects(db)
	if e != nil {
		return e
	}
	sort.Strings(want)
	sort.Strings(actual)
	if len(want) != len(actual) {
		return errors.New("backup database schema has unexpected objects")
	}
	for i := range want {
		if want[i] != actual[i] {
			return errors.New("backup database schema differs from migrations")
		}
	}
	var integrity string
	if e = db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); e != nil {
		return e
	}
	if integrity != "ok" {
		return errors.New("backup database integrity check failed")
	}
	foreign, e := db.QueryContext(ctx, "PRAGMA foreign_key_check")
	if e != nil {
		return e
	}
	defer foreign.Close()
	if foreign.Next() {
		return errors.New("backup database foreign keys are inconsistent")
	}
	return foreign.Err()
}
