package httpapi_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"testing"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/transport/generated"
)

type aliasOwner struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Aliases   []string `json:"aliases"`
	Revision  int64    `json:"revision"`
	UpdatedAt string   `json:"updatedAt"`
}

func createAliasOwner(t *testing.T, f *fixture, library, name, key string, aliases []string) aliasOwner {
	t.Helper()
	var owner aliasOwner
	if library == "" {
		decode(t, f.json("POST", "/tags", key, map[string]string{"name": name}, 201), &owner)
	} else {
		decode(t, f.json("POST", "/collections", key, map[string]any{"libraryId": library, "name": name, "kind": "image_set", "aliases": aliases}, 201), &owner)
	}
	if owner.ID == "" || owner.Revision < 1 || owner.Aliases == nil || !slices.Equal(owner.Aliases, aliases) {
		t.Fatal("created owner did not expose its persisted identity and aliases", owner)
	}
	return owner
}

func TestAliasEditsCheckOwnerRevisionAndPreserveAssetsAndMembers(t *testing.T) {
	f := setup(t)
	const shared = "冬日 731"
	tag := createAliasOwner(t, f, "", "Original tag", "alias-create-tag", []string{})
	collection := createAliasOwner(t, f, f.library, "Original collection", "alias-create-group", []string{"creation spelling", shared})
	firstUpload, original := f.upload("alias-member.png", 33, map[string]any{"collectionId": collection.ID})
	first := f.complete(firstUpload).Asset
	secondUpload, _ := f.upload("alias-member-two.png", 34, map[string]any{"collectionId": collection.ID})
	second := f.complete(secondUpload).Asset
	assetURL := "/assets/" + first.ID
	decode(t, f.json("PUT", assetURL+"/tags", "alias-assign-tag", map[string]any{"expectedRevision": first.TagsRevision, "tags": []string{tag.Name}}, 200), first)
	peer := createAliasOwner(t, f, "", "Other tag", "alias-create-other-tag", []string{})
	f.json("PUT", "/tag-vocabulary/"+peer.ID+"/aliases", "alias-other-spellings", generated.AliasSetUpdate{ExpectedRevision: peer.Revision, Aliases: []string{shared, "冬日 7310"}}, 200)
	outsideUpload, _ := f.upload("outside-alias-set.png", 35, nil)
	outside := f.complete(outsideUpload).Asset
	f.json("PUT", "/assets/"+outside.ID+"/tags", "alias-assign-other-tag", map[string]any{"expectedRevision": outside.TagsRevision, "tags": []string{peer.Name}}, 200)
	searchAssets := func(query string, want ...string) {
		t.Helper()
		q := url.Values{"q": {query}, "limit": {"1"}}
		seen := map[string]bool{}
		for {
			var page model.Page[model.Asset]
			decode(t, f.json("GET", "/assets?"+q.Encode(), "", nil, 200), &page)
			if page.TotalMatched == nil || *page.TotalMatched != int64(len(want)) || len(page.Items) > 1 {
				t.Fatal("wrong alias search count or page size", query, page)
			}
			for _, asset := range page.Items {
				if seen[asset.ID] || !slices.Contains(want, asset.ID) {
					t.Fatal("duplicate or unrelated alias hit", query, asset.ID)
				}
				seen[asset.ID] = true
			}
			if page.NextCursor == "" {
				break
			}
			if len(page.Items) == 0 {
				t.Fatal("empty page advertised continuation", query)
			}
			q.Set("cursor", page.NextCursor)
		}
		if len(seen) != len(want) {
			t.Fatal("lost a matching asset across pages", query, seen)
		}
	}
	membersURL := "/collections/" + collection.ID + "/members"
	members := f.json("GET", membersURL, "", nil, 200).Body.Bytes()
	if !bytes.Contains(members, []byte(first.ID)) || !bytes.Contains(members, []byte(second.ID)) {
		t.Fatal("expected two real collection members")
	}
	for _, table := range []string{"asset_records", "asset_tags", "asset_authors", "asset_locations", "asset_content_versions", "collection_members"} {
		for _, action := range []string{"INSERT", "UPDATE", "DELETE"} {
			ddl := fmt.Sprintf("CREATE TRIGGER alias_guard_%s_%s BEFORE %s ON %s BEGIN SELECT RAISE(ABORT,'alias changed asset or membership'); END", table, action, action, table)
			if _, err := f.s.DB.DB.Exec(ddl); err != nil {
				t.Fatal(err)
			}
		}
	}
	searchAssets("creation spelling", first.ID, second.ID)
	searchAssets("冬日 7310", outside.ID)
	searchAssets("冬日 73")
	for _, tc := range []struct {
		owner aliasOwner
		path  string
		list  string
	}{
		{tag, "/tag-vocabulary/" + tag.ID + "/aliases", "/tag-vocabulary"},
		{collection, "/collections/" + collection.ID + "/aliases", "/collections"},
	} {
		key := "alias-write-" + tc.owner.ID
		for _, invalid := range []any{
			map[string]any{"expectedRevision": tc.owner.Revision},
			map[string]any{"expectedRevision": tc.owner.Revision, "aliases": nil},
			map[string]any{"aliases": []string{"missing revision"}},
		} {
			f.json("PUT", tc.path, key, invalid, 400)
		}
		input := generated.AliasSetUpdate{ExpectedRevision: tc.owner.Revision, Aliases: []string{"  " + shared + "  ", "snow", shared}}
		f.json("PUT", tc.path, "", input, 400)
		var edited aliasOwner
		firstReceipt := f.json("PUT", tc.path, key, input, 200)
		decode(t, firstReceipt, &edited)
		if edited.ID != tc.owner.ID || edited.Name != tc.owner.Name || edited.Revision != tc.owner.Revision+1 || !slices.Equal(edited.Aliases, []string{shared, "snow"}) {
			t.Fatal("edit changed the wrong identity, order or revision", edited)
		}
		searchAssets(shared, first.ID, second.ID, outside.ID)
		if tc.owner.ID == tag.ID {
			searchAssets("snow", first.ID)
		}
		clear := generated.AliasSetUpdate{ExpectedRevision: edited.Revision, Aliases: []string{}}
		var problem generated.Error
		decode(t, f.json("PUT", tc.path, key, clear, 409), &problem)
		if problem.Code != "idempotency_conflict" {
			t.Fatal(problem)
		}
		var cleared aliasOwner
		decode(t, f.json("PUT", tc.path, key+"-clear", clear, 200), &cleared)
		if cleared.ID != edited.ID || cleared.Aliases == nil || len(cleared.Aliases) != 0 || cleared.Revision != edited.Revision+1 {
			t.Fatal("explicit clear did not remove only aliases", cleared)
		}
		decode(t, f.json("PUT", tc.path, key+"-stale", clear, 409), &problem)
		if problem.Code != "revision_conflict" || problem.CurrentRevision == nil || *problem.CurrentRevision != cleared.Revision {
			t.Fatal("stale same-value edit escaped the revision check", problem)
		}
		clear.ExpectedRevision = cleared.Revision
		var unchanged aliasOwner
		decode(t, f.json("PUT", tc.path, key+"-unchanged", clear, 200), &unchanged)
		if unchanged.Revision != cleared.Revision || unchanged.UpdatedAt != cleared.UpdatedAt {
			t.Fatal("saving the same set revised the owner", unchanged)
		}
		replay := f.json("PUT", tc.path, key, input, 200)
		if !bytes.Equal(firstReceipt.Body.Bytes(), replay.Body.Bytes()) {
			t.Fatal("lost response did not return the original receipt")
		}
		var read model.Page[aliasOwner]
		decode(t, f.json("GET", tc.list+"?q="+url.QueryEscape(tc.owner.Name), "", nil, 200), &read)
		if len(read.Items) != 1 || read.Items[0].ID != cleared.ID || read.Items[0].Revision != cleared.Revision || read.Items[0].Aliases == nil || len(read.Items[0].Aliases) != 0 {
			t.Fatal("replaying the old save resurrected a cleared alias", read)
		}
	}
	searchAssets("snow")
	searchAssets(shared, outside.ID)
	if after := f.json("GET", membersURL, "", nil, 200).Body.Bytes(); !bytes.Equal(members, after) {
		t.Fatal("aliases changed the members or their ordering")
	}
	var stored model.Asset
	decode(t, f.json("GET", assetURL, "", nil, 200), &stored)
	if stored.Revision != first.Revision || stored.TagsRevision != first.TagsRevision || stored.ContentVersion != first.ContentVersion || !slices.Equal(stored.Tags, []string{tag.Name}) {
		t.Fatal("alias editing altered asset facts", stored)
	}
	assertNativeOriginal(t, f, first.ID, first.ContentVersion, original)
}

func TestAliasSearchReturnsAllOwnersAndLiteralScopedMatches(t *testing.T) {
	f := setup(t)
	var other model.Library
	decode(t, f.json("POST", "/libraries", "alias-second-library", map[string]string{"name": "Second alias library", "kind": "regular"}, 201), &other)
	const shared = "共同"
	aliases := []string{shared, "共同录入", `ref%_\id`, "left", "right"}
	tagA := createAliasOwner(t, f, "", "A "+shared, "alias-search-tag-a", []string{})
	tagB := createAliasOwner(t, f, "", "B tag", "alias-search-tag-b", []string{})
	for _, entry := range []aliasOwner{tagA, tagB} {
		f.json("PUT", "/tag-vocabulary/"+entry.ID+"/aliases", "alias-index-"+entry.ID, generated.AliasSetUpdate{ExpectedRevision: entry.Revision, Aliases: aliases}, 200)
	}
	groupA := createAliasOwner(t, f, f.library, "A "+shared, "alias-search-collection-a", aliases)
	groupB := createAliasOwner(t, f, f.library, "B set", "alias-search-collection-b", aliases)
	groupC := createAliasOwner(t, f, other.ID, "C set", "alias-search-collection-c", aliases)
	createAliasOwner(t, f, "", "Plain tag", "alias-literal-tag", []string{})
	createAliasOwner(t, f, f.library, "Plain set", "alias-literal-set", []string{})
	check := func(path, query string, want ...string) {
		t.Helper()
		q := url.Values{"q": {query}, "limit": {"1"}}
		if path == "/collections" {
			q.Set("libraryId", f.library)
		}
		var got []string
		for {
			var page model.Page[aliasOwner]
			decode(t, f.json("GET", path+"?"+q.Encode(), "", nil, 200), &page)
			for _, item := range page.Items {
				got = append(got, item.ID)
			}
			if page.NextCursor == "" {
				break
			}
			if len(page.Items) != 1 || len(got) > len(want) {
				t.Fatal("owner pagination repeated a result", page, got)
			}
			q.Set("cursor", page.NextCursor)
		}
		if !slices.Equal(got, want) {
			t.Fatal(path, query, got, want)
		}
	}
	check("/tag-vocabulary", shared, tagA.ID, tagB.ID)
	check("/collections", shared, groupA.ID, groupB.ID)
	check("/tag-vocabulary", "%", tagA.ID, tagB.ID)
	check("/collections", "_", groupA.ID, groupB.ID)
	check("/collections", `ref%_\id`, groupA.ID, groupB.ID)
	check("/tag-vocabulary", `left","right`)
	check("/collections", `left","right`)
	var global model.Page[aliasOwner]
	decode(t, f.json("GET", "/collections?q="+url.QueryEscape(shared), "", nil, 200), &global)
	if len(global.Items) != 3 || global.Items[2].ID != groupC.ID {
		t.Fatal("unscoped lookup lost a library candidate", global)
	}
	var compatible model.Page[string]
	decode(t, f.json("GET", "/tags?q="+url.QueryEscape(shared), "", nil, 200), &compatible)
	if !slices.Equal(compatible.Items, []string{tagA.Name, tagB.Name}) {
		t.Fatal("legacy tag page lost canonical names", compatible)
	}
}

func TestTagWriteReplaysImmutablePreIdentityReceipts(t *testing.T) {
	f := setup(t)
	tag := createAliasOwner(t, f, "", "Former", "alias-create-history-fixture", []string{})
	var renamed aliasOwner
	decode(t, f.json("PATCH", "/tags/Former", "alias-rename-history-fixture", map[string]any{"expectedRevision": tag.Revision, "name": "Current"}, 200), &renamed)
	var current aliasOwner
	decode(t, f.json("PUT", "/tag-vocabulary/"+tag.ID+"/aliases", "alias-update-history-fixture", generated.AliasSetUpdate{ExpectedRevision: renamed.Revision, Aliases: []string{"Lookup"}}, 200), &current)
	for _, tc := range []struct {
		method, path, key, canonical, receipt string
		status                               int
	}{
		{"POST", "/tags", "historical-create-tag", `{"name":"Former"}`, `{"name":"Former","revision":1,"createdAt":"2024-01-02T03:04:05Z","updatedAt":"2024-01-02T03:04:05Z"}`, 201},
		{"PATCH", "/tags/Former", "historical-rename-tag", `{"name":"Current","expectedRevision":1}`, `{"name":"Current","revision":1,"createdAt":"2024-01-02T04:05:06Z","updatedAt":"2024-01-02T04:05:06Z"}`, 200},
	} {
		// These are the four fields emitted before tags acquired stable IDs.
		// The original write pipeline included the empty query after a NUL.
		hash := fmt.Sprintf("%x", sha256.Sum256([]byte(tc.canonical+"\x00")))
		operation := tc.method + " /api/v1" + tc.path
		if _, err := f.s.DB.DB.Exec("INSERT INTO idempotency(principal_id,operation,key,input_hash,response,status) VALUES(?,?,?,?,?,?)",
			"owner", operation, tc.key, hash, []byte(tc.receipt), tc.status); err != nil {
			t.Fatal(err)
		}
		response := f.request(tc.method, tc.path, tc.key, testSecret, []byte(tc.canonical), nil)
		if response.Code != tc.status || response.Body.String() != tc.receipt {
			t.Fatal("historical tag receipt was reconstructed from current data", tc.method, response.Code, response.Body.String())
		}
		var old generated.TagWriteReceipt
		if err := json.Unmarshal(response.Body.Bytes(), &old); err != nil || old.Id != nil || old.Aliases != nil || old.Name == "" || old.CreatedAt == "" || old.UpdatedAt == "" || old.Revision != 1 {
			t.Fatal("generated legacy receipt projection", old, err)
		}
	}
	var page model.Page[aliasOwner]
	decode(t, f.json("GET", "/tag-vocabulary?q=Lookup", "", nil, 200), &page)
	if len(page.Items) != 1 || page.Items[0].ID != tag.ID || page.Items[0].Revision != current.Revision || !slices.Equal(page.Items[0].Aliases, current.Aliases) {
		t.Fatal("receipt replay changed the live tag", page)
	}
}
