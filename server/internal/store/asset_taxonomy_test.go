package store

import (
	"context"
	"path/filepath"
	"slices"
	"testing"
)

func TestTaxonomyUpgradeKeepsMembershipOrderUnboundTextAndDeliberateAbsence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prior.sqlite")
	db, err := OpenDatabase(path, CatalogDatabase)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(migrationLedgerSQL)
	steps, err := readSchemaMigrations(migrations)
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range steps {
		if step.version >= 40 {
			break
		}
		if err := step.apply(db); err != nil {
			t.Fatal(err)
		}
	}
	exec("INSERT INTO libraries(id,name,kind,data) VALUES('lib','Library','regular','{}')")
	exec("INSERT INTO authors(id,name,data) VALUES('owner','Owner','{}')")
	exec("INSERT INTO sources(id,name,data) VALUES('a','Alpha','{}'),('z','Zulu','{}'),('m','Middle','{}'),('ghost','Ghost','{}')")
	exec("INSERT INTO characters(id,source_id,name,data) VALUES('a-role','a','Alpha role','{}'),('z-role','z','Zulu role','{}')")
	for id, raw := range map[string]string{
		"bound":   `{"source":"stale","characters":["old"],"sourceIds":["z","ghost","a"],"characterIds":["z-role","a-role"]}`,
		"unbound": `{"source":"Zulu","characters":["Zulu role"],"sourceIds":[],"characterIds":[]}`,
		"cleared": `{"source":"Ghost","characters":["old"],"sourceIds":["ghost"],"characterIds":["z-role"]}`,
	} {
		exec("INSERT INTO asset_records(id,author_id,revision,created_at,updated_at,data) VALUES(?,?,1,'2026-01-01','2026-01-01',?)", id, "owner", raw)
		exec("INSERT INTO asset_content_versions(asset_id,version,size,media_type,mime_type) VALUES(?,1,4,'image','image/png')", id)
		exec("INSERT INTO asset_locations(asset_id,library_id,relative_path,file_name,current_version,trashed,storage_layout) VALUES(?,'lib',?,?,1,0,1)", id, id+".png", id+".png")
	}
	exec("INSERT INTO asset_sources(asset_id,source_id) VALUES('bound','a'),('bound','z'),('bound','m')")
	exec("INSERT INTO asset_characters(asset_id,character_id) VALUES('bound','a-role'),('bound','z-role')")
	exec("INSERT INTO asset_human_fields(asset_id,field) VALUES('cleared','taxonomy')")
	exec(`INSERT INTO file_intents(id,kind,state,data) VALUES('pending-taxonomy','move','prepared','{"After":{"id":"bound"}}')`)
	if err := applySchema(db, migrations); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = OpenDatabase(path, CatalogDatabase)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	type selection struct {
		Source                             string
		SourceIDs, CharacterIDs, Characters []string
	}
	read := func(id string) selection {
		t.Helper()
		result, err := Get[selection](ctx, db, "SELECT data FROM assets WHERE id=?", id)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	bound := read("bound")
	if !slices.Equal(bound.SourceIDs, []string{"z", "a", "m"}) || bound.Source != "Zulu+Alpha+Middle" ||
		!slices.Equal(bound.CharacterIDs, []string{"z-role", "a-role"}) || !slices.Equal(bound.Characters, []string{"Zulu role", "Alpha role"}) {
		t.Fatal("migration changed membership or invented order", bound)
	}
	text := read("unbound")
	if text.Source != "Zulu" || !slices.Equal(text.Characters, []string{"Zulu role"}) || len(text.SourceIDs)+len(text.CharacterIDs) != 0 {
		t.Fatal("legacy text became an identity", text)
	}
	cleared := read("cleared")
	if cleared.Source != "" || len(cleared.SourceIDs)+len(cleared.CharacterIDs)+len(cleared.Characters) != 0 {
		t.Fatal("an obsolete cached identity was resurrected", cleared)
	}
	var clean bool
	if err := db.QueryRow(`SELECT NOT EXISTS(SELECT 1 FROM asset_records r,json_each(r.data) j WHERE j.key IN ('source','sourceIds','characters','characterIds'))
		AND EXISTS(SELECT 1 FROM asset_human_fields WHERE asset_id='cleared' AND field='taxonomy')`).Scan(&clean); err != nil || !clean {
		t.Fatal("duplicate facts or lost human protection", clean, err)
	}
	if _, err := db.Exec("UPDATE asset_records SET revision=2 WHERE id='bound'"); err == nil {
		t.Fatal("upgrade lost the active file intent guard")
	}
}
