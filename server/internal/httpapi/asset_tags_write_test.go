package httpapi_test

import (
	"fmt"
	"strings"
	"testing"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/service"
)

func TestTagEditsLeaveAuthorTaxonomyAndOriginalRowsUntouched(t *testing.T) {
	f := setup(t)
	upload, _ := f.upload("tag-intent.png", 67, nil)
	a := f.complete(upload).Asset
	base := "/assets/" + a.ID
	var source, role struct{ ID string }
	decode(t, f.json("POST", "/sources", "tag-write-source", map[string]any{"name": "Fixture source", "aliases": []string{}}, 201), &source)
	decode(t, f.json("POST", "/characters", "tag-write-role", map[string]any{"name": "Fixture role", "sourceId": source.ID, "aliases": []string{}}, 201), &role)
	decode(t, f.json("PATCH", base, "tag-write-relations", map[string]any{"expectedRevision": a.Revision,
		"expectedAuthorsRevision": a.AuthorsRevision, "newAuthorNames": []string{"Fixture author"},
		"sourceIds": []string{source.ID}, "characterIds": []string{role.ID}}, 200), a)
	db := f.s.DB.DB
	if _, err := db.Exec("CREATE TABLE tag_write_trace(relation TEXT,operation TEXT)"); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"asset_authors", "asset_sources", "asset_characters", "asset_locations", "asset_content_versions"} {
		var n int
		if err := db.QueryRow("SELECT count(*) FROM "+table+" WHERE asset_id=?", a.ID).Scan(&n); err != nil || n != 1 {
			t.Fatal("missing populated relation", table, n, err)
		}
		for _, operation := range []string{"INSERT", "UPDATE", "DELETE"} {
			ddl := fmt.Sprintf("CREATE TRIGGER tag_check_%s_%s AFTER %s ON %s BEGIN INSERT INTO tag_write_trace VALUES('%s','%s'); END",
				table, strings.ToLower(operation), operation, table, table, operation)
			if _, err := db.Exec(ddl); err != nil {
				t.Fatal(err)
			}
		}
	}
	decode(t, f.json("PUT", base+"/tags", "tag-write-set", map[string]any{"expectedRevision": a.TagsRevision,
		"tags": []string{"one", "two"}}, 200), a)
	decode(t, f.json("PUT", base+"/tags", "tag-write-remove", map[string]any{"expectedRevision": a.TagsRevision,
		"tags": []string{"two"}}, 200), a)
	var saved model.Asset
	decode(t, f.json("GET", base, "", nil, 200), &saved)
	if len(saved.Tags) != 1 || saved.Tags[0] != "two" || len(saved.AuthorIDs) != 1 || saved.AuthorID == "unknown" ||
		len(saved.SourceIDs) != 1 || saved.SourceIDs[0] != source.ID || len(saved.CharacterIDs) != 1 || saved.CharacterIDs[0] != role.ID {
		t.Fatal("tag edit or existing associations were not preserved", saved.Tags)
	}
	var n int
	if err := db.QueryRow("SELECT count(*) FROM tag_write_trace").Scan(&n); err != nil || n != 0 {
		t.Fatal("tag edit wrote an unrelated row", n, err)
	}
	t.Log("two HTTP tag edits caused 0 writes to populated author, source, character, location and content-version tables")
}

func TestTagRenameKeepsItsIdentityMembershipAndStaleSpellingGuard(t *testing.T) {
	f := setup(t)
	u, _ := f.upload("stable-tag.png", 103, nil)
	a := f.complete(u).Asset
	base := "/assets/" + a.ID
	var word service.Tag
	decode(t, f.json("POST", "/tags", "create-stable-word", map[string]string{"name": "former"}, 201), &word)
	decode(t, f.json("PUT", base+"/tags", "associate-stable-word", map[string]any{"expectedRevision": a.TagsRevision,
		"tags": []string{"first", "former", "last"}}, 200), a)
	db := f.s.DB.DB
	for _, op := range []string{"INSERT", "UPDATE", "DELETE"} {
		guard := fmt.Sprintf("CREATE TRIGGER tag_rename_%s BEFORE %s ON asset_tags BEGIN SELECT RAISE(ABORT,'rename rewrote membership'); END", strings.ToLower(op), op)
		if _, err := db.Exec(guard); err != nil {
			t.Fatal(err)
		}
	}
	var renamed service.Tag
	action := map[string]any{"expectedRevision": word.Revision, "name": "current"}
	decode(t, f.json("PATCH", "/tags/former", "stable-word-rename", action, 200), &renamed)
	f.json("PATCH", "/tags/former", "stable-word-rename", action, 200)
	var current model.Asset
	decode(t, f.json("GET", base, "", nil, 200), &current)
	if word.ID == "" || renamed.ID != word.ID || renamed.Revision <= word.Revision || current.TagsRevision != a.TagsRevision+1 ||
		strings.Join(current.Tags, ",") != "first,current,last" {
		t.Fatal("renamed word lost its stable identity, position or asset clock", renamed, current.Tags)
	}
	var id string
	var ordinal int
	if err := db.QueryRow("SELECT tag_id,ordinal FROM asset_tags WHERE asset_id=? AND tag_id=?", a.ID, word.ID).Scan(&id, &ordinal); err != nil || id != renamed.ID || ordinal != 1 {
		t.Fatal("asset no longer references the original word", id, ordinal, err)
	}
	f.json("PUT", base+"/tags", "old-spelling-cannot-revive", map[string]any{"expectedRevision": current.TagsRevision, "tags": []string{"former"}}, 409)
	var newWord service.Tag
	decode(t, f.json("POST", "/tags", "explicitly-recreate-former", map[string]string{"name": "former"}, 201), &newWord)
	if newWord.ID == word.ID || newWord.Revision <= word.Revision {
		t.Fatal("reusing a spelling reused the active identity or accepted its old revision")
	}
	f.json("PATCH", "/tags/former", "stale-owner-cannot-rename", action, 409)
	t.Log("rename and replay performed zero writes to asset_tags; the original ID and ordinal survived")
}
