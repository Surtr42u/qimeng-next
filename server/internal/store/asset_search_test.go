package store

import (
	"database/sql"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// oldAssetSearchText is the pre-0048 per-row formula. Keeping it in the test
// pins the contract: the materialised text must stay equal to what the browse
// predicate used to compute, including the taxonomy fallbacks.
const oldAssetSearchText = `(a.file_name||char(10)||substr(a.relative_path,1,length(a.relative_path)-length(a.file_name))||char(10)||COALESCE((SELECT group_concat(tag,char(10)) FROM asset_tag_names WHERE asset_id=a.id),'')||char(10)||COALESCE((SELECT group_concat(au.name||' '||COALESCE(json_extract(au.data,'$.aliases'),''),char(10)) FROM asset_authors aa JOIN authors au ON au.id=aa.author_id WHERE aa.asset_id=a.id),'')||char(10)||COALESCE((SELECT group_concat(s.name,char(10)) FROM asset_source_memberships x JOIN sources s ON s.id=x.source_id WHERE x.asset_id=a.id),json_extract(a.data,'$.source'),'')||char(10)||COALESCE((SELECT group_concat(c.name,char(10)) FROM asset_characters x JOIN characters c ON c.id=x.character_id WHERE x.asset_id=a.id),json_extract(a.data,'$.characters'),''))`

func TestAssetSearchTriggersMatchSearchTextView(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "search.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.DB.Close()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.DB.Exec(query, args...); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
	}
	exec(`INSERT INTO libraries(id,name,kind,data) VALUES('lib-a','A','regular','{}'),('lib-b','B','regular','{}')`)
	exec(`INSERT INTO authors(id,name,following,data) VALUES('au-a','Alpha',0,'{"aliases":["Old Alias"]}')`)
	exec(`INSERT INTO tags(id,name,revision,deleted,created_at,updated_at) VALUES
		('tag-a','Art',1,0,'2025-01-01','2025-01-01'),('tag-b','Draft',1,0,'2025-01-01','2025-01-01')`)
	exec(`INSERT INTO sources(id,name,data) VALUES('src-a','Pixiv','{}'),('src-b','Other','{}')`)
	exec(`INSERT INTO characters(id,source_id,name,data) VALUES('ch-a','src-a','Hero','{}')`)

	insertAsset := func(id, library, relative, file string) {
		t.Helper()
		exec(`INSERT INTO asset_records(id,author_id,revision,created_at,updated_at,data)
			VALUES(?,'au-a',1,'2025-01-01T00:00:00.000Z','2025-01-01T00:00:00.000Z','{}')`, id)
		exec(`INSERT INTO asset_content_versions(asset_id,version,hash,size,media_type,mime_type,width,height,duration_ms,probe_state)
			VALUES(?,1,?,1,'image','image/png',1,1,NULL,'ok')`, id, strings.Repeat("a", 64))
		exec(`INSERT INTO asset_locations(asset_id,library_id,relative_path,file_name,current_version,trashed,storage_layout)
			VALUES(?,?,?,?,1,0,2)`, id, library, relative, file)
	}
	insertAsset("asset-a", "lib-a", "Artist/Work 1/Alpha 1.png", "Alpha 1.png")
	insertAsset("asset-b", "lib-b", "Artist/Work 2/Beta 2.png", "Beta 2.png")
	// asset-fallback never gets a relationship row: both searchable dimensions
	// must come from asset_taxonomy_state.
	insertAsset("asset-fallback", "lib-a", "Fallback/Only.png", "Only.png")
	exec(`INSERT INTO asset_authors(asset_id,author_id,ordinal) VALUES('asset-a','au-a',0)`)
	exec(`INSERT INTO asset_tags(asset_id,tag_id,ordinal) VALUES('asset-a','tag-a',0)`)
	exec(`INSERT INTO asset_sources(asset_id,source_id,ordinal) VALUES('asset-a','src-a',0)`)
	exec(`INSERT INTO asset_characters(asset_id,character_id,ordinal) VALUES('asset-a','ch-a',0)`)
	exec(`INSERT INTO asset_taxonomy_state(asset_id,source_text,character_text,derived,candidates)
		VALUES('asset-b','Fallback Source','["Fallback Hero"]',0,'[]')`)
	exec(`INSERT INTO asset_taxonomy_state(asset_id,source_text,character_text,derived,candidates)
		VALUES('asset-fallback','Fallback Only Source','["Fallback Only Hero"]',0,'[]')`)
	assertAssetSearchMatchesView(t, db.DB)

	step := func(_ string, mutate func()) {
		t.Helper()
		mutate()
		assertAssetSearchMatchesView(t, db.DB)
	}
	step("asset create", func() {
		insertAsset("asset-c", "lib-a", "Artist/Work 3/Gamma 3.png", "Gamma 3.png")
		exec(`INSERT INTO asset_authors(asset_id,author_id,ordinal) VALUES('asset-c','au-a',0)`)
		exec(`INSERT INTO asset_tags(asset_id,tag_id,ordinal) VALUES('asset-c','tag-a',0)`)
	})
	step("asset rename", func() {
		exec(`UPDATE asset_locations SET relative_path='Artist/Work 9/Renamed 9.png',file_name='Renamed 9.png'
			WHERE asset_id='asset-a'`)
	})
	step("asset move", func() {
		exec(`UPDATE asset_locations SET library_id='lib-b' WHERE asset_id='asset-a'`)
	})
	step("tag rename", func() {
		exec(`UPDATE tags SET name='Illustration',revision=revision+1 WHERE id='tag-a'`)
	})
	step("tag soft delete", func() {
		exec(`UPDATE tags SET deleted=1,revision=revision+1 WHERE id='tag-a'`)
	})
	step("tag restore and attach", func() {
		exec(`UPDATE tags SET deleted=0,revision=revision+1 WHERE id='tag-a'`)
		exec(`INSERT INTO asset_tags(asset_id,tag_id,ordinal) VALUES('asset-b','tag-b',0)`)
	})
	step("tag detach", func() {
		exec(`DELETE FROM asset_tags WHERE asset_id='asset-b' AND tag_id='tag-b'`)
	})
	step("author attach", func() {
		exec(`INSERT INTO asset_authors(asset_id,author_id,ordinal) VALUES('asset-b','au-a',0)`)
	})
	step("author detach", func() {
		exec(`DELETE FROM asset_authors WHERE asset_id='asset-b' AND author_id='au-a'`)
	})
	step("author rename", func() {
		exec(`UPDATE authors SET name='Renamed Author' WHERE id='au-a'`)
	})
	step("author alias", func() {
		exec(`UPDATE authors SET data='{"aliases":["New Alias","作者别名"]}' WHERE id='au-a'`)
	})
	step("character rename", func() {
		exec(`UPDATE characters SET name='Renamed Hero' WHERE id='ch-a'`)
	})
	step("character reparent", func() {
		exec(`UPDATE characters SET source_id='src-b' WHERE id='ch-a'`)
	})
	step("character attach", func() {
		exec(`INSERT INTO asset_characters(asset_id,character_id,ordinal) VALUES('asset-b','ch-a',0)`)
	})
	step("character detach", func() {
		exec(`DELETE FROM asset_characters WHERE asset_id='asset-b' AND character_id='ch-a'`)
	})
	step("source rename", func() {
		exec(`UPDATE sources SET name='Renamed Source' WHERE id='src-a'`)
	})
	step("source attach", func() {
		exec(`INSERT INTO asset_sources(asset_id,source_id,ordinal) VALUES('asset-b','src-a',0)`)
	})
	step("source detach", func() {
		exec(`DELETE FROM asset_sources WHERE asset_id='asset-b' AND source_id='src-a'`)
	})
	step("taxonomy text", func() {
		exec(`UPDATE asset_taxonomy_state SET source_text='New Fallback',character_text='["New Fallback Hero"]'
			WHERE asset_id='asset-b'`)
	})
	step("asset delete", func() {
		exec(`DELETE FROM asset_records WHERE id='asset-b'`)
	})
}

// assertAssetSearchMatchesView recomputes every asset from the view, compares
// the stored row, rejects extra rows, and rechecks the old per-row formula.
func assertAssetSearchMatchesView(t *testing.T, db *sql.DB) {
	t.Helper()
	view := []string{}
	rows, err := db.Query(`SELECT asset_id FROM asset_search_source ORDER BY asset_id`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		view = append(view, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		t.Fatal(err)
	}
	rows.Close()
	for _, id := range view {
		var text, brackets, wantText, wantBrackets string
		if err := db.QueryRow(`SELECT text,brackets FROM asset_search_source WHERE asset_id=?`, id).Scan(&wantText, &wantBrackets); err != nil {
			t.Fatalf("asset_search_source row for %s: %v", id, err)
		}
		if err := db.QueryRow(`SELECT text,brackets FROM asset_search WHERE asset_id=?`, id).Scan(&text, &brackets); err != nil {
			t.Fatalf("asset_search row for %s: %v", id, err)
		}
		if text != wantText || brackets != wantBrackets {
			t.Fatalf("asset_search row for %s differs from asset_search_source\ngot  %q %q\nwant %q %q",
				id, text, brackets, wantText, wantBrackets)
		}
	}
	var extra int
	if err := db.QueryRow(`SELECT count(*) FROM asset_search WHERE asset_id NOT IN (SELECT asset_id FROM asset_search_source)`).Scan(&extra); err != nil {
		t.Fatal(err)
	}
	if extra != 0 {
		t.Fatalf("asset_search has %d rows outside asset_search_source", extra)
	}

	rows, err = db.Query(`SELECT a.id,lower(` + oldAssetSearchText + `) FROM assets a ORDER BY a.id`)
	if err != nil {
		t.Fatal(err)
	}
	formula := map[string]string{}
	for rows.Next() {
		var id, text string
		if err := rows.Scan(&id, &text); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		formula[id] = text
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		t.Fatal(err)
	}
	rows.Close()
	for id, want := range formula {
		var text string
		if err := db.QueryRow(`SELECT text FROM asset_search WHERE asset_id=?`, id).Scan(&text); err != nil {
			t.Fatalf("asset_search row for %s: %v", id, err)
		}
		if id == "asset-fallback" {
			if text != want {
				t.Fatalf("fallback asset text differs from the old per-row formula\ngot  %q\nwant %q", text, want)
			}
			continue
		}
		// group_concat has no ORDER BY in either path; ordering across
		// dimensions is not part of the matching contract, but the line
		// multiset is.
		if !sameLines(text, want) {
			t.Fatalf("asset_search text lines for %s differ from the old per-row formula\ngot  %q\nwant %q", id, text, want)
		}
	}
}

func sameLines(a, b string) bool {
	left := strings.Split(a, "\n")
	right := strings.Split(b, "\n")
	sort.Strings(left)
	sort.Strings(right)
	return strings.Join(left, "\n") == strings.Join(right, "\n")
}
