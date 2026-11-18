package store

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strconv"
	"strings"
)

type schemaMigration struct {
	version  int
	name     string
	sql      string
	checksum string
}

func applySchema(db *sql.DB, source fs.FS) error {
	steps, err := readSchemaMigrations(source)
	if err != nil {
		return err
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY, name TEXT NOT NULL, sha256 TEXT NOT NULL
	)`); err != nil {
		return err
	}
	// An older executable must reject a future or divergent database before
	// running even one pending migration from its own build.
	applied, err := verifyMigrationHistory(db, steps)
	if err != nil {
		return err
	}
	for _, step := range steps {
		if !applied[step.version] {
			if err := step.apply(db); err != nil {
				return fmt.Errorf("apply migration %s: %w", step.name, err)
			}
		}
	}
	return nil
}

func readSchemaMigrations(source fs.FS) ([]schemaMigration, error) {
	files, err := fs.ReadDir(source, "migrations")
	if err != nil {
		return nil, err
	}
	steps := make([]schemaMigration, 0, len(files))
	seen := make(map[int]bool)
	for _, file := range files {
		name := file.Name()
		if file.IsDir() || path.Ext(name) != ".sql" {
			continue
		}
		prefix, _, ok := strings.Cut(name, "_")
		version, err := strconv.Atoi(prefix)
		if !ok || err != nil || version < 1 || seen[version] {
			return nil, fmt.Errorf("invalid or duplicate migration version in %s", name)
		}
		body, err := fs.ReadFile(source, path.Join("migrations", name))
		if err != nil {
			return nil, err
		}
		seen[version] = true
		steps = append(steps, schemaMigration{
			version: version, name: name, sql: string(body),
			checksum: fmt.Sprintf("%x", sha256.Sum256(body)),
		})
	}
	sort.Slice(steps, func(i, j int) bool { return steps[i].version < steps[j].version })
	return steps, nil
}

func verifyMigrationHistory(db *sql.DB, steps []schemaMigration) (map[int]bool, error) {
	checksums := make(map[int]string, len(steps))
	for _, step := range steps {
		checksums[step.version] = step.checksum
	}
	rows, err := db.Query("SELECT version, sha256 FROM schema_migrations ORDER BY version")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	applied := make(map[int]bool)
	for rows.Next() {
		var version int
		var checksum string
		if err := rows.Scan(&version, &checksum); err != nil {
			return nil, err
		}
		expected, found := checksums[version]
		if !found {
			return nil, fmt.Errorf("database schema %d requires a newer server", version)
		}
		if checksum != expected {
			return nil, fmt.Errorf("migration %d checksum changed", version)
		}
		applied[version] = true
	}
	return applied, rows.Err()
}

func (step schemaMigration) apply(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(step.sql); err != nil {
		return err
	}
	if _, err := tx.Exec("INSERT INTO schema_migrations(version, name, sha256) VALUES(?, ?, ?)",
		step.version, step.name, step.checksum); err != nil {
		return err
	}
	return tx.Commit()
}
