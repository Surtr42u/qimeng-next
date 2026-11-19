package catalog

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/service"
	"qimeng.local/next/server/internal/store"
)

func TestColdCatalogIdentityListsTagsAndTaxonomy(t *testing.T) {
	f := newFixture(t)
	f.author("amber", "Amber / Ember", "regular", 1)
	f.author("opaque-second", "Amber / Ember", "regular", 0)
	f.author("cos_studio", "Studio", "cos", 0)
	f.author("unknown", "A real artist", "regular", 0)
	f.author("其他搁置", "其他搁置", "regular", 0)
	f.asset("a", "library", "synthetic-a.png", "First", nil, sampleTime)
	f.asset("b", "library", "synthetic-b.png", "First+Second", nil, sampleTime)
	f.asset("c", "cos-library", "Studio/Album/synthetic-c.png", nil, "Album", "")
	f.asset("d", "library", "other/synthetic-a.png", nil, nil, "")
	f.old(`INSERT INTO asset_authors VALUES('a','amber',?,'client'),('a','opaque-second',NULL,'import'),
		('b','其他搁置',NULL,'legacy'),('c','cos_studio',?,'scanner'),('c','amber',?,'client'),('a','其他搁置',NULL,'legacy')`,
		sampleTime, sampleTime, sampleTime)
	f.old("INSERT INTO tags VALUES('t1','first tag',?),('t2','second tag',?)", sampleTime, sampleTime)
	f.old("INSERT INTO asset_tags VALUES('a','t1','1970-01-01T00:00:00.000Z','legacy'),('a','t2',?,'client')", sampleTime)
	f.old("INSERT INTO asset_characters VALUES('a','One',?,'scanner'),('b','Parent unrecorded',NULL,'legacy')", sampleTime)
	first := "\ufeff1 Amber  Ember\r\n作品\r\nsynthetic-a.png\r\nabsent.png\r\n"
	lists, _ := json.Marshal([]fragment{{"first.txt", first, sampleTime}, {"second.txt", "1  Amber\n来源  old second source\n", ""}})
	f.setting("imported_txt_sources", string(lists))
	f.setting("imported_txt_upload_entries", `{"first.txt":[{"authorId":"amber","displayName":"Amber / Ember","names":["Amber","Ember"],"works":["synthetic-a.png","absent.png"],"sources":[]}]}`)
	f.setting("custom_sources", `["Separate"]`)
	f.setting("custom_source_groups", `[{"Canonical":"First","Variants":["F"],"Characters":[{"Canonical":"One","Aliases":["One alias"]}]}]`)
	f.setting("custom_stop_words", `["ignored word"]`)
	if err := f.run(); err != nil {
		t.Fatal(err)
	}
	s := &service.Service{DB: f.target}
	a, err := s.Asset(t.Context(), "a")
	if err != nil || !slices.Equal(a.AuthorIDs, []string{"amber", "opaque-second"}) || a.AuthorStatus != "confirmed" ||
		!slices.Equal(a.Tags, []string{"second tag", "first tag"}) || a.TagSetUpdatedAt == nil ||
		a.Source != "First" || !slices.Equal(a.Characters, []string{"One"}) || len(a.CharacterIDs) != 1 || a.CollectionID != "" {
		t.Fatalf("native asset a=%+v err=%v", a, err)
	}
	b, err := s.Asset(t.Context(), "b")
	if err != nil || b.AuthorID != "unknown" || b.AuthorStatus != "unknown" || len(b.Tags) != 0 || b.TagSetUpdatedAt == nil ||
		len(b.SourceIDs) != 2 || len(b.CharacterIDs) != 0 || !slices.Equal(b.Characters, []string{"Parent unrecorded"}) {
		t.Fatalf("native unconfirmed asset b=%+v err=%v", b, err)
	}
	c, err := s.Asset(t.Context(), "c")
	if err != nil || !slices.Equal(c.AuthorIDs, []string{"cos_studio", "amber"}) || c.COSDirectoryAuthorID != "cos_studio" || c.CosWorkID == "" || c.CollectionID != "" {
		t.Fatalf("native mixed COS asset c=%+v err=%v", c, err)
	}
	f.expect("2", "SELECT count(*) FROM authors WHERE name='Amber / Ember'")
	f.expect("1", "SELECT following FROM authors WHERE id='amber'")
	f.expect("unknown", "SELECT author_id FROM migration_author_aliases WHERE source_id=? AND source_author_id='其他搁置'", sampleSource)
	f.expect("0", "SELECT author_id='unknown' FROM migration_author_aliases WHERE source_author_id='unknown'")
	f.expect("[]", "SELECT json_extract(data,'$.acquisitionSources') FROM authors WHERE id='amber'")
	f.expect(first, "SELECT content FROM author_txt_fragments WHERE name='first.txt' AND source_id=?", sampleSource)
	f.expect("first.txt", "SELECT l.name FROM asset_author_overrides o JOIN author_lists l ON l.id=o.list_id WHERE o.asset_id='a' AND o.author_id='amber'")
	f.expect("1", "SELECT removed FROM asset_author_overrides WHERE asset_id='d' AND author_id='amber'")
	f.expect("1", "SELECT COUNT(*) FROM asset_human_fields WHERE asset_id='b' AND field='tags'")
	f.expect("1", "SELECT count(*) FROM migration_relation_origins WHERE input_id=? AND kind='cold26/unbound-protected-work'", sampleSource)
	f.expect("null", `SELECT json_type(original,'$.relation.createdAt') FROM migration_relation_origins WHERE kind='cold26/asset-author'
		AND json_extract(original,'$.assetId')='a' AND json_extract(original,'$.relation.key')='opaque-second'`)
	f.expect("1970-01-01T00:00:00.000Z", `SELECT json_extract(original,'$.relation.createdAt') FROM migration_relation_origins
		WHERE kind='cold26/asset-tag' AND json_extract(original,'$.relation.key')='t1'`)
	f.expect("t2", "SELECT id FROM tags WHERE name='second tag' AND created_at=?", sampleTime)
	f.expect("1", "SELECT count(*) FROM migration_relation_origins WHERE kind='cold26/mixed-placeholder'")
	f.expect("1", "SELECT count(*) FROM sources WHERE name='Separate'")
	rules, err := s.SourceRules(t.Context())
	if err != nil || len(rules.Groups) != 2 || !slices.Equal(rules.StopWords, []string{"ignored word"}) || rules.Groups[0].Characters[0].Aliases[0] != "One alias" {
		t.Fatalf("rules=%+v err=%v", rules, err)
	}
	f.expect("0", "SELECT count(*) FROM migration_inputs")
}

func TestColdCatalogOwnerProtectionOnNormalTextReimport(t *testing.T) {
	f := newFixture(t)
	f.author("artist", "Artist", "regular", 1)
	f.asset("file", "library", "illustration.png", nil, nil, "")
	f.old("INSERT INTO asset_authors VALUES('file','artist',?,'txt')", sampleTime)
	f.setting("imported_txt_sources", `[{"filename":"review.txt","content":"1  Artist\n来源  Gallery\n作品\nillustration.png"}]`)
	f.setting("imported_txt_upload_entries", `{"review.txt":[{"authorId":"artist","displayName":"Artist","works":["illustration.png"],"sources":["Gallery"]}]}`)
	if err := f.run(); err != nil {
		t.Fatal(err)
	}
	s := &service.Service{DB: f.target}
	owner := model.Principal{ID: "owner", Scopes: []string{"admin"}}
	input := service.AuthorListInput{Name: "review.txt", Content: "1  Artist\n来源  Replacement\n", ExpectedRevision: new(int64(1))}
	apply := func() error {
		_, err := s.Run(t.Context(), owner, "cold-fixture-import", "", nil, func(w *service.Write) (any, int, error) {
			out, err := s.ImportAuthorList(t.Context(), w, input)
			return out, 200, err
		})
		return err
	}
	var conflict *service.Error
	if err := apply(); !errors.As(err, &conflict) || conflict.Code != "author_list_missing_entries" {
		t.Fatalf("reimport did not protect actual work: %v", err)
	}
	input.ConflictResolution = "keep"
	if err := apply(); err != nil {
		t.Fatal(err)
	}
	f.expect(`["Gallery"]`, "SELECT json_extract(data,'$.acquisitionSources') FROM authors WHERE id='artist'")
	f.expect("1", "SELECT count(*) FROM author_list_rules WHERE work='illustration.png'")
}

func TestColdCatalogTextUsesV26WholeLineAndExplicitOwnerEvidence(t *testing.T) {
	f := newFixture(t)
	f.author("aa", "AA", "regular", 0)
	f.author("aa__bb", "AA / BB", "regular", 0)
	f.author("historical-other", "AA", "regular", 0)
	f.setting("imported_txt_sources", `[{"filename":"plain.txt","content":"AA / BB\nignored.png\n1\n"},
		{"filename":"inconsistent.txt","content":"1 AA\n作品\nw.png"}]`)
	f.setting("imported_txt_upload_entries", `{"inconsistent.txt":[{"authorId":"historical-other","names":["AA"],"works":["w.png"]}]}`)
	if err := f.run(); err != nil {
		t.Fatal(err)
	}
	f.expect("aa__bb", "SELECT m.author_id FROM author_list_members m JOIN author_lists l ON l.id=m.list_id WHERE l.name='plain.txt'")
	f.expect("0", "SELECT count(*) FROM author_list_rules WHERE work='w.png'")
	f.expect("1", "SELECT count(*) FROM migration_relation_origins WHERE kind='cold26/unresolved-list-member'")
	f.expect("2", "SELECT count(*) FROM author_txt_fragments WHERE source_id=?", sampleSource)
}

func TestColdCatalogSettingsAndExcludedHostState(t *testing.T) {
	f := newFixture(t)
	f.setting("client_config", `{"scan":{"workers":3,"thumbEdge":640},"upload":{"autoAccept":false,"maxBytesMb":0}}`)
	f.setting("recommend_prefs", `{"maxRandom":0,"discovery":0.87,"tagCollection":null}`)
	f.setting("backup_schedule", `{"enabled":true,"intervalHours":20,"retention":7}`)
	f.setting("author_source_vocabulary", `["gallery feed","catalog"]`)
	f.setting("legacy_import_batch", "historic")
	f.setting("author_mirror", `{"mode":"fragment","fragmentFilename":"chosen.txt","path":"/synthetic-mount/excluded-host-marker"}`)
	for _, key := range []string{"enrichment_engine_version", "client_logs", "library_revision"} {
		f.setting(key, "excluded-host-marker")
	}
	if err := f.run(); err != nil {
		t.Fatal(err)
	}
	s := &service.Service{DB: f.target}
	cfg, err := s.Settings(t.Context())
	if err != nil || cfg.Scan.Workers != 3 || cfg.Scan.ThumbEdge != 640 || cfg.Upload.AutoAccept {
		t.Fatalf("settings=%+v err=%v", cfg, err)
	}
	prefs, err := s.RecommendationPreferences(t.Context())
	if err != nil || prefs.Weights.MaxRandom != 0 || prefs.Weights.Discovery != .87 || prefs.Weights.TagCollection != .15 {
		t.Fatalf("weights=%+v err=%v", prefs.Weights, err)
	}
	vocab, err := s.AcquisitionVocabulary(t.Context())
	if err != nil || !slices.Equal(vocab.Suggestions, []string{"gallery", "feed", "catalog"}) {
		t.Fatalf("vocab=%+v err=%v", vocab, err)
	}
	f.expect("0", "SELECT count(*) FROM migration_relation_origins WHERE instr(original,'excluded-host-marker')>0")
	f.expect("3", "SELECT count(*) FROM migration_relation_origins WHERE kind='cold26/excluded-setting'")
	f.expect("fragment", "SELECT mode FROM author_list_mirror WHERE state='disabled' AND path=''")
	f.expect("chosen.txt", "SELECT json_extract(original,'$.value.fragmentFilename') FROM migration_relation_origins WHERE json_extract(original,'$.key')='author_mirror'")
	f.expect("historic", "SELECT json_extract(original,'$.value') FROM migration_relation_origins WHERE kind='cold26/archived-setting' AND json_extract(original,'$.key')='legacy_import_batch'")
	f.expect("7", "SELECT json_extract(original,'$.value.retention') FROM migration_relation_origins WHERE kind='cold26/archived-setting' AND json_extract(original,'$.key')='backup_schedule'")
}

func TestColdCatalogRejectsUnknownSettingAndPrivateBusinessText(t *testing.T) {
	for _, tc := range []struct{ key, val string }{
		{"unsupported", "never-export-this"},
		{"author_source_vocabulary", `["/synthetic-mount/owner-subfolder"]`},
		{"author_source_vocabulary", `["https://example.org/a?access_token=never-export-this"]`},
	} {
		t.Run(tc.key, func(t *testing.T) {
			f := newFixture(t)
			f.author("test", "Synthetic", "regular", 0)
			f.setting(tc.key, tc.val)
			if err := f.run(); err == nil || strings.Contains(err.Error(), tc.val) {
				t.Fatalf("expected a redacted refusal, got %v", err)
			}
			f.expect("0", "SELECT count(*) FROM migration_relation_origins")
			f.expect("0", "SELECT count(*) FROM authors WHERE id='test'")
		})
	}
}

func nativeAuthor(t *testing.T, f *fixture, id string) model.Author {
	t.Helper()
	a, err := store.Get[model.Author](t.Context(), f.target.DB, "SELECT data FROM authors WHERE id=?", id)
	if err != nil {
		t.Fatal(err)
	}
	return a
}
