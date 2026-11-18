package service

import (
	"context"
	"strings"
	"testing"

	"qimeng.local/next/server/internal/model"
)

func TestCollectionAliasesPreserveAnExistingNamingPlanAndUseIndexedOwners(t *testing.T) {
	ctx := context.Background()
	s, err := New(Config{DataDir: t.TempDir(), AdminToken: "synthetic-collection-alias-owner", ReserveBytes: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	var library model.Library
	testMutation(t, s, "create-library-for-alias-plan", func(w *Write) (any, int, error) {
		var err error
		library, err = s.CreateLibrary(ctx, w, "Local archive", "regular")
		return library, 201, err
	})
	var collection model.Collection
	testMutation(t, s, "create-drama-for-alias-plan", func(w *Write) (any, int, error) {
		var err error
		collection, err = s.CreateCollection(ctx, w, model.Collection{LibraryID: library.ID, Kind: "short_drama", Name: "Series"})
		return collection, 201, err
	})
	item := model.PlanItem{CollectionID: collection.ID, NamingFamily: "Series", FileName: "Series 1.mp4", RelativePath: "Series/Series 1.mp4", Page: 1}
	scope := func() string {
		t.Helper()
		h, err := planItemScope(ctx, s.DB.DB, library.ID, item, nil, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		return h
	}
	before := scope()
	testMutation(t, s, "edit-aliases-on-planned-drama", func(w *Write) (any, int, error) {
		var err error
		collection, err = s.SetCollectionAliases(ctx, w, collection.ID, collection.Revision, []string{"Earlier title"})
		return collection, 200, err
	})
	if collection.Revision != 2 || scope() != before {
		t.Fatal("a display alias invalidated a plan already bound to the collection ID")
	}
	testMutation(t, s, "rename-the-planned-drama", func(w *Write) (any, int, error) {
		var err error
		collection, err = s.RenameCollection(ctx, w, collection.ID, "Renamed series", collection.Revision)
		return collection, 200, err
	})
	if scope() == before || len(collection.Aliases) != 1 || collection.Aliases[0] != "Earlier title" {
		t.Fatal("a real rename did not invalidate the plan or lost the aliases")
	}

	rows, err := s.DB.DB.QueryContext(ctx, "EXPLAIN QUERY PLAN SELECT c.data FROM collection_details c WHERE c.library_id=? AND "+
		collectionAliasMatch+" ORDER BY c.name,c.id LIMIT ? OFFSET ?", library.ID, "%Earlier%", "%Earlier%", 51, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var lines []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err = rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, detail)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	plan := strings.Join(lines, "\n")
	if !strings.Contains(plan, "collections_library_name (library_id=?)") || !strings.Contains(plan, "(collection_id=?)") {
		t.Fatal("scoped alias lookup did not seek the library and its individual owners", plan)
	}
	t.Log(plan)
}
