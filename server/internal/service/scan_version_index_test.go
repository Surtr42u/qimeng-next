package service

import (
	"context"
	"strings"
	"testing"

	"qimeng.local/next/server/internal/store"
)

func TestScanBatchIndexesOnlyCurrentlyLocatedContent(t *testing.T) {
	ctx := context.Background()
	s, run, root := preparedScanFixture(t)
	a := existingScanOriginal(t, s, run, root, "original.png")
	oldHash, oldSize := a.Hash, a.Size
	a.ContentVersion++
	a.Hash, a.Size = digest([]byte("replacement")), 11
	if err := store.SaveAsset(ctx, s.DB.DB, a); err != nil {
		t.Fatal(err)
	}
	check := func(candidate string, size int64, want bool) {
		t.Helper()
		var found bool
		err := s.DB.DB.QueryRowContext(ctx, freshScanCollisionSQL, a.LibraryID, "other.png", candidate, size, a.LibraryID).Scan(&found)
		if err != nil || found != want {
			t.Fatal("scan matched a historical version or missed the current one", found, err)
		}
	}
	check(oldHash, oldSize, false)
	check(a.Hash, a.Size, true)
	rows, err := s.DB.DB.QueryContext(ctx, "EXPLAIN QUERY PLAN "+freshScanCollisionSQL, a.LibraryID, "other.png", a.Hash, a.Size, a.LibraryID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var details []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err = rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		details = append(details, detail)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	plan := strings.Join(details, "\n")
	if !strings.Contains(plan, "assets_live_path (library_id=? AND relative_path=?)") ||
		!strings.Contains(plan, "asset_versions_hash (hash=? AND size=?)") || !strings.Contains(plan, "asset_id=?)") {
		t.Fatal("scan no longer seeks an exact path, size/hash and stable asset identity", plan)
	}
	t.Log(plan)
}
