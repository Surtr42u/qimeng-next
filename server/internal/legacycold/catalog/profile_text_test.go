package catalog

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestColdCatalogPublicArchiveURLRequiresPublicNestedIdentity(t *testing.T) {
	i := importer{roots: []string{"/synthetic-mount"}}
	for _, link := range []string{
		"https://mirror.example/record/https://artist.example/page.png",
		"https://mirror.example/record/http%3A%2F%2Fartist.example%2Ffolder%2Fpage.png",
		"https://mirror.example/record/https%3A%2F%2Fartist.example%2Fpage%3Fview%3Done",
	} {
		text := "see " + link + " next"
		got, changes, err := i.inspectText(text, true)
		if err != nil || changes != 0 || got != text || i.portableText(text) != nil {
			t.Fatal("public archived URL failed byte-preserving readback")
		}
	}
	for _, link := range []string{
		"https://mirror.example/r/https://localhost/a", "https://mirror.example/r/https%3A//127.0.0.1/a",
		"https://mirror.example/r/https://0177.0.0.1/a", "https://mirror.example/r/https://u@artist.example/a",
		"https://mirror.example/r/https%3A%2F%2Fartist.example%2Fa%3FX-Amz-Signature%3Dfixture",
		"https://mirror.example/r/https://artist.example/C%3A%2Ffixture", "https://mirror.example/r/C%3a%5cfixture",
		"https://mirror.example/r/https%253A%252F%252Flocalhost/a", "https://mirror.example/%2Fprivate-host/share",
		"https://mirror.example/r/https://artist.example/synthetic-mount/item", "https://mirror.example/%2Fsynthetic-mount/item",
		"https://mirror.example/r/https://artist.example/a?flag=keep%26access_token%3Dfixture",
		"https://mirror.example/a#access_token=fixture", "https://mirror.example/a?si%2567=fixture",
		"plain text C:/synthetic/file", "plain text /home/fixture/.ssh/example", "plain text token=fixture",
	} {
		if _, _, err := i.inspectText(link, true); !errors.Is(err, errPrivate) {
			t.Fatal("unsafe or ambiguous URL was accepted")
		}
	}
}

func TestColdCatalogRedactsOnlyDocumentedBiographyQueryFields(t *testing.T) {
	i := importer{}
	marker := rand.Text()
	for _, input := range []string{
		`{"homepageUrl":"https://artist.example/?token=%s"}`,
		`{"homepageUrl":"https://artist.example)"}`,
		`{"candidate":{"evidenceUrls":["https://artist.example)"]}}`,
		`{"avatarUrl":"https://artist.example/?token=%s"}`,
		`{"remark":"https://artist.example/?token=%s"}`,
		`{"xProfile":{"url":"https://artist.example/?token=%s"}}`,
		`{"updates":[{"title":"https://artist.example/?token=%s"}]}`,
		`{"other":{"biography":"https://artist.example/?token=%s"}}`,
		`{"candidate":[{"biography":"https://artist.example/?token=%s"}]}`,
		`{"biography":"direct access_token=%s"}`,
		`{"biography":"access_token=https://artist.example/credential"}`,
		`{"biography":"https://u@artist.example/a?token=%s"}`,
		`{"other":"https://artist.example/?token=%s","other":"safe"}`,
	} {
		raw := strings.ReplaceAll(input, "%s", marker)
		if _, _, err := i.inspectJSON([]byte(raw), true); err == nil {
			t.Fatal("non-descriptive, ambiguous or non-public credential accepted")
		}
	}
	raw := []byte("{ \"biography\" : \"Text https://gallery.example/?key=one+two&view=%2f。\", \"unknown\":9007199254740993 }")
	got, changes, err := i.inspectJSON(raw, true)
	if err != nil || len(changes) != 0 || !bytes.Equal(raw, got) {
		t.Fatal("ordinary biography source bytes changed")
	}
	unsafe := []byte(`{"biography":"https://artist.example/?access_token=` + marker + `"}`)
	if _, _, err := i.inspectJSON(unsafe, false); !errors.Is(err, errPrivate) {
		t.Fatal("setting acquired an author-profile editing exception")
	}
}

func TestColdCatalogPublicMarkdownInRemarksAndTXT(t *testing.T) {
	f := newFixture(t)
	f.author("writer", "Writer", "regular", 1)
	const note = "联系 [官网](https://artist.example) 和 [作品](https://gallery.example/post/(series))。"
	const source = "1  Writer\n来源  [官网](https://artist.example)\n作品\nexample.png"
	parent := map[string]any{"authorId": "writer", "revision": 1, "status": "verified", "remark": note,
		"homepageUrl": "https://artist.example/gallery/(series)", "evidenceUrls": []string{"https://artist.example"}}
	raw, err := json.Marshal(parent)
	if err != nil {
		t.Fatal(err)
	}
	f.setting("creator_profile:writer", string(raw))
	raw, err = json.Marshal([]fragment{{Filename: "public.txt", Content: source}})
	if err != nil {
		t.Fatal(err)
	}
	f.setting("imported_txt_sources", string(raw))
	if err := f.run(); err != nil {
		t.Fatal(err)
	}
	f.expect(note, "SELECT json_extract(data,'$.values.remark') FROM author_profile_blocks WHERE author_id='writer' AND block='basic'")
	f.expect(source, "SELECT content FROM author_txt_fragments WHERE name='public.txt'")
	f.expect("0", "SELECT count(*) FROM migration_relation_origins WHERE kind='cold26/profile-text-disposition'")
}

// Inspect the original database and any write-ahead/rollback pages before
// checkpoint or VACUUM can remove stale bytes. Then inspect the actual main
// file after checkpoint, including free pages, without compacting the DB.
func (f *fixture) ensureAbsentFromNativeFiles(marker string) {
	f.t.Helper()
	file := f.scalar("SELECT file FROM pragma_database_list WHERE name='main'")
	read := func(path string, optional bool) {
		f.t.Helper()
		data, err := os.ReadFile(path)
		if optional && errors.Is(err, os.ErrNotExist) {
			return
		}
		if err != nil {
			f.t.Fatal(err)
		}
		if bytes.Contains(data, []byte(marker)) {
			f.t.Fatal("discarded query value is present in the actual native database or journal")
		}
	}
	read(file, false)
	read(file+"-wal", true)
	read(file+"-journal", true)
	f.native("PRAGMA wal_checkpoint(TRUNCATE)")
	read(file, false)
}

func TestColdCatalogRedactedBiographyAbsentFromNativeDBAndArchive(t *testing.T) {
	f := newFixture(t)
	f.author("writer", "Writer", "regular", 1)
	f.native("INSERT INTO settings(key,value) VALUES('instance_id','synthetic-native')")
	marker := rand.Text()
	const link = "https://artist.example/posts"
	annotated := "前文 " + link + "?tag=a+b&%61ccess_token=" + marker + "&tag=%2f&flag=0#news 后文"
	const visible = "前文 " + link + "?tag=a+b&tag=%2f&flag=0#news 后文"
	const nested = "https://mirror.example/record/https%3A%2F%2Fartist.example%2Fhome"
	profile := map[string]any{
		"authorId": "writer", "revision": 7, "status": "verified", "nickname": "Public writer",
		"homepageUrl": nested, "evidenceUrls": []string{nested}, "biography": annotated,
		"xProfile": map[string]any{"url": "https://x.com/synthetic_fixture",
			"biography": "Original (" + link + "?sig=" + marker + ").", "biographyZh": "译文 " + link + "?token=" + marker + "。保留"},
		"candidate":    map[string]any{"biography": "Draft " + link + "?view=lower&api_key=" + marker, "evidenceUrls": []string{nested}},
		"extraInteger": json.Number("9007199254740993"),
	}
	raw, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	f.setting("creator_profile:writer", string(raw))
	if err := f.run(); err != nil {
		t.Fatal(err)
	}
	f.ensureAbsentFromNativeFiles(marker)
	after, snapshot, archivePath := f.restoreCatalog()
	after.ensureAbsentFromNativeFiles(marker)
	encoded, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, data := range [][]byte{snapshot, encoded} {
		if bytes.Contains(data, []byte(marker)) {
			t.Fatal("discarded parameter value entered a portable payload")
		}
	}
	for _, db := range []*fixture{f, after} {
		db.expect("1", "SELECT following FROM authors WHERE id='writer'")
		db.expect(visible, "SELECT json_extract(data,'$.biography') FROM authors WHERE id='writer'")
		db.expect(visible, "SELECT json_extract(data,'$.originalImport.biography') FROM author_profile_archives WHERE author_id='writer'")
		db.expect("Original ("+link+").", "SELECT json_extract(data,'$.values.biographyOriginal') FROM author_profile_blocks WHERE author_id='writer' AND block='x'")
		db.expect("译文 "+link+"。保留", "SELECT json_extract(data,'$.values.biographyZh') FROM author_profile_blocks WHERE author_id='writer' AND block='x'")
		db.expect("Draft "+link+"?view=lower", "SELECT json_extract(data,'$.values.summary') FROM author_profile_candidates WHERE author_id='writer' AND block='basic'")
		db.expect(nested, "SELECT json_extract(original,'$.value.homepageUrl') FROM migration_relation_origins WHERE kind='cold26/author-profile'")
		db.expect("9007199254740993", "SELECT json_extract(original,'$.value.extraInteger') FROM migration_relation_origins WHERE kind='cold26/author-profile'")
		db.expect("portable_query_redacted", "SELECT json_extract(original,'$.valueDisposition') FROM migration_relation_origins WHERE kind='cold26/author-profile'")
		db.expect("4", "SELECT json_array_length(original,'$.fields') FROM migration_relation_origins WHERE kind='cold26/author-profile'")
		for _, path := range []string{"/biography", "/candidate/biography", "/xProfile/biography", "/xProfile/biographyZh"} {
			db.expect("1", `SELECT count(*) FROM migration_relation_origins WHERE input_id=? AND kind='cold26/profile-text-disposition'
				AND json_extract(original,'$.sealedSource.sourceId')=? AND json_extract(original,'$.sealedSource.table')='kv_settings'
				AND json_extract(original,'$.sealedSource.key')='creator_profile:writer' AND json_extract(original,'$.decision.field')=?
				AND json_extract(original,'$.decision.action')='remove_sensitive_url_query_parameters' AND json_extract(original,'$.decision.removedParameters')=1`,
				sampleSource, sampleSource, path)
		}
	}
}

func TestColdCatalogRejectsClearCredentialsAfterProseAndQuotes(t *testing.T) {
	for _, tc := range []struct {
		name, field, prefix string
	}{
		{"Chinese label in biography", "biography", "说明：access_token="},
		{"Chinese quote in remark", "remark", "公开备注「api_key="},
		{"quoted key in remark", "remark", "作者备注中有“signature”：“"},
		{"ASCII quote in TXT", "txt", "笔记 'password="},
		{"Chinese comma in TXT", "txt", "说明，secret="},
		{"adjacent assignments in TXT", "txt", "note:token="},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.author("writer", "Writer", "regular", 0)
			marker := rand.Text()
			key := "creator_profile:writer"
			var value any = map[string]any{"authorId": "writer", "status": "empty", "revision": 1, tc.field: tc.prefix + marker}
			if tc.field == "txt" {
				key = "imported_txt_sources"
				value = []fragment{{Filename: "synthetic.txt", Content: "1  Writer\n来源  " + tc.prefix + marker}}
			}
			data, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			f.setting(key, string(data))
			err = f.run()
			if !errors.Is(err, errPrivate) || strings.Contains(err.Error(), marker) {
				t.Fatal("expected redacted rejection of a clear credential")
			}
			f.expect("0", "SELECT count(*) FROM migration_relation_origins")
			f.expect("0", "SELECT count(*) FROM authors WHERE id='writer'")
		})
	}
}
