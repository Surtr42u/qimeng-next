package service

import (
	"context"
	"encoding/json"
	"testing"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/store"
)

func TestUnknownPlaceholderCannotBecomeConfirmedDirectoryAuthor(t *testing.T) {
	ctx := context.Background()
	s, e := New(Config{DataDir: t.TempDir(), AdminToken: "synthetic-placeholder-credential-00000000", ReserveBytes: 1})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	p := model.Principal{ID: "owner", Scopes: []string{"admin"}}
	for _, kind := range []string{"regular", "cos"} {
		if _, e = s.Run(ctx, p, "create-unknown-"+kind, "unknown-conflict-"+kind, nil, func(w *Write) (any, int, error) {
			a, e := s.CreateAuthor(ctx, w, AuthorInput{Name: "其他搁置", Kind: kind})
			return a, 201, e
		}); e == nil {
			t.Fatal("created a confirmed author with the reserved placeholder name")
		}
	}
	created := testMutation(t, s, "placeholder-library", func(w *Write) (any, int, error) {
		v, e := s.CreateLibrary(ctx, w, "Synthetic COS", "cos")
		return v, 201, e
	})
	var library model.Library
	if e = json.Unmarshal(created.Body, &library); e != nil {
		t.Fatal(e)
	}
	a := model.Asset{LibraryID: library.ID, RelativePath: "其他搁置/unknown.png", AuthorID: "unknown", AuthorIDs: []string{"unknown"}, AuthorsRevision: 1, AuthorStatus: "unknown"}
	if e = s.scanCOSAuthor(ctx, s.DB.DB, &a); e != nil {
		t.Fatal(e)
	}
	if a.AuthorID != "unknown" || a.AuthorStatus != "unknown" {
		t.Fatal("COS directory converted placeholder into confirmed ownership")
	}
	authors, e := store.List[model.Author](ctx, s.DB.DB, "SELECT data FROM authors")
	if e != nil || len(authors) != 1 {
		t.Fatal("scanner invented an additional placeholder identity")
	}
}
