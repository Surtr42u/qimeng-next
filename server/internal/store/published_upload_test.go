package store

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestLegacyUploadVersionsRequireOriginalPublicationEvidence(t *testing.T) {
	db, e := OpenDatabase(filepath.Join(t.TempDir(), "synthetic.sqlite"), CatalogDatabase)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	files, e := migrations.ReadDir("migrations")
	if e != nil {
		t.Fatal(e)
	}
	for _, entry := range files {
		if entry.Name() >= "0019" {
			break
		}
		raw, e := migrations.ReadFile("migrations/" + entry.Name())
		if e != nil {
			t.Fatal(e)
		}
		if _, e = db.Exec(string(raw)); e != nil {
			t.Fatal(e)
		}
	}
	for _, id := range []string{"intent", "receipt", "late", "absent", "replaced"} {
		extra := ""
		if id == "replaced" {
			extra = `,"replaceAssetId":"asset-replaced","expectedContentVersion":1`
		}
		record := fmt.Sprintf(`{"id":%q,"assetId":"asset-%s","hash":"h","size":7%s}`, id, id, extra)
		if _, e = db.Exec("INSERT INTO uploads(id,batch_id,state,data) VALUES(?,'batch','ingested',?)", id, record); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = db.Exec(`INSERT INTO file_intents(id,kind,state,data) VALUES('f','ingest','done','{"UploadID":"intent","After":{"id":"asset-intent","contentVersion":1,"hash":"h","size":7}}')`); e != nil {
		t.Fatal(e)
	}
	for _, entry := range []struct {
		id      string
		version int
	}{{"receipt", 1}, {"late", 3}, {"replaced", 2}} {
		body := fmt.Sprintf(`{"upload":{"id":%q,"state":"ingested"},"asset":{"id":"asset-%s","contentVersion":%d,"hash":"h","size":7}}`, entry.id, entry.id, entry.version)
		if _, e = db.Exec("INSERT INTO idempotency VALUES('owner',?,'operation','request',?,200)", "POST /api/v1/uploads/"+entry.id+"/complete", []byte(body)); e != nil {
			t.Fatal(e)
		}
	}
	raw, e := migrations.ReadFile("migrations/0019_library_index.sql")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(string(raw)); e != nil {
		t.Fatal(e)
	}
	var facts string
	if e = db.QueryRow(`SELECT group_concat(id||'='||COALESCE(json_extract(data,'$.publishedContentVersion'),0),',') FROM (SELECT id,data FROM uploads ORDER BY id)`).Scan(&facts); e != nil {
		t.Fatal(e)
	}
	if strings.TrimSpace(facts) != "absent=0,intent=1,late=0,receipt=1,replaced=2" {
		t.Fatal("legacy proof was guessed or lost", facts)
	}
}
