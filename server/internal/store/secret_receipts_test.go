package store

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// Recreate only synthetic pre-fix rows to check that a historical one-time
// issuance cannot be replayed, without revoking an unrelated user's device.
func TestRevokeExposedIssuancesPreservesUnrelatedReceipts(t *testing.T) {
	db, err := OpenDatabase(filepath.Join(t.TempDir(), "old-catalog.sqlite"), CatalogDatabase)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	apply := func(file string) {
		t.Helper()
		raw, err := migrations.ReadFile("migrations/" + file)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(string(raw)); err != nil {
			t.Fatal(err)
		}
	}
	files, err := migrations.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range files {
		if entry.Name() >= "0020" {
			break
		}
		apply(entry.Name())
	}
	exec := func(query string, args ...any) sql.Result {
		t.Helper()
		result, err := db.Exec(query, args...)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	for _, grant := range []string{"exposed", "safe"} {
		exec("INSERT INTO ai_delegations VALUES(?, 'active','2099-01-01','{\"state\":\"active\"}')", grant)
		exec("INSERT INTO credentials VALUES(?,?,'2099-01-01','{}')", grant, grant+"-digest")
		exec("INSERT INTO credentials VALUES(?,?,'2099-01-01',json_object('parentDelegationId',?))", grant+"-child", grant+"-child-digest", grant)
	}
	exec("INSERT INTO credentials VALUES('device','devdigest','2099-01-01','{}')")
	base := "INSERT INTO idempotency VALUES('owner',?,'synthetic-key','digest',?,201)"
	exec(base, "POST /api/v1/credentials", []byte(`{"id":"device","token":"fixture"}`))
	exec(base, "POST /api/v1/ai/delegations", []byte(`{"credential":{"id":"exposed","token":"fixture"}}`))
	exec(base, "POST /api/v1/libraries", []byte(`{"id":"library"}`))
	apply("0020_single_use_secrets.sql")
	check := func(query, expected string) {
		t.Helper()
		var actual string
		if err := db.QueryRow(query).Scan(&actual); err != nil || actual != expected {
			t.Fatalf("unexpected migrated record: %q %v", actual, err)
		}
	}
	check("SELECT group_concat(id,',') FROM (SELECT id FROM credentials ORDER BY id)", "safe,safe-child")
	check("SELECT state||':'||json_extract(data,'$.state') FROM ai_delegations WHERE id='exposed'", "revoked:revoked")
	check("SELECT state FROM ai_delegations WHERE id='safe'", "active")
	check("SELECT group_concat(operation,',') FROM idempotency", "POST /api/v1/libraries")
}
