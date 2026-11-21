package service

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"qimeng.local/next/server/internal/domain"
	"qimeng.local/next/server/internal/store"
)

// TestAssetSearchSQLAgreesWithSearchToken runs the real generated predicate
// against a corpus and compares every candidate with domain.SearchToken.
func TestAssetSearchSQLAgreesWithSearchToken(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "search.sqlite"))
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
	exec(`INSERT INTO libraries(id,name,kind,data) VALUES('lib','Library','regular','{}')`)
	exec(`INSERT INTO authors(id,name,following,data) VALUES('au','Author',0,'{}')`)
	corpus := []string{
		"朱诺 11 (2).png",
		"朱诺 110.png",
		"朱诺 2011.png",
		"11 (2).png",
		"作品 12 中文",
		"汉字角色",
		"a*b-c:d(e)f",
		`quote " and * and - and : and ( and )`,
		`50%_\real`,
		"【2】 (3) [4] （5）",
		"x11y",
		"111",
		"119",
		"（11）.png",
		"【11】.png",
		"[11].png",
		"start 11",
		"11 end",
		"alpha beta",
		"ABC",
		"a(b)c",
		"a[b]c",
		"p_q%r",
		`back\slash`,
		"你好世界",
		"作品",
		"字",
		"22 2 222",
		"0 00 000",
		"(0).png",
		"a]b",
		"a【b】c",
	}
	for i, text := range corpus {
		id := fmt.Sprintf("corpus-%03d", i)
		exec(`INSERT INTO asset_records(id,author_id,revision,created_at,updated_at,data)
			VALUES(?,'au',1,'2025-01-01T00:00:00.000Z','2025-01-01T00:00:00.000Z','{}')`, id)
		exec(`INSERT INTO asset_content_versions(asset_id,version,hash,size,media_type,mime_type,width,height,duration_ms,probe_state)
			VALUES(?,1,?,1,'image','image/png',1,1,NULL,'ok')`, id, strings.Repeat("b", 64))
		exec(`INSERT INTO asset_locations(asset_id,library_id,relative_path,file_name,current_version,trashed,storage_layout)
			VALUES(?,'lib',?,?,1,0,2)`, id, id+".png", id+".png")
		exec(`UPDATE asset_search SET text=lower(?),brackets=replace(replace(replace(replace(replace(replace(
			lower(?),'（','('),'[','('),'【','('),'）',')'),']',')'),'】',')') WHERE asset_id=?`, text, text, id)
	}

	tokens := []string{
		"11", "1", "2", "22", "0", "作品", "字", "你好", "ab", "AB", "cjk",
		`"`, "*", "-", ":", "(", ")", "%", "_", `\`, "【】", "（）", "[2]", "（2）", "(2)", "【2】",
		"(2", "2）", "[abc]", "?", "a[", "a]",
	}
	for _, token := range tokens {
		want := []string{}
		for i, text := range corpus {
			if domain.SearchToken(text, token) {
				want = append(want, fmt.Sprintf("corpus-%03d", i))
			}
		}
		sort.Strings(want)
		match, args := assetSearchMatches(token)
		rows, err := db.DB.Query(`SELECT a.id FROM assets a WHERE `+match+` ORDER BY a.id`, args...)
		if err != nil {
			t.Fatalf("token %q: %v", token, err)
		}
		got := []string{}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			got = append(got, id)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		rows.Close()
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Fatalf("token %q\ngot  %v\nwant %v", token, got, want)
		}

		kind, folded, _ := domain.SearchTokenPattern(token)
		probe, _ := assetSearchProbe(token)
		if !domain.SearchTokenDigits(folded) && strings.Contains(probe, "GLOB") {
			t.Fatalf("token %q (kind %v, folded %q) reached the GLOB branch", token, kind, folded)
		}
	}
}
