package service

import (
	"path/filepath"
	"strings"
	"testing"

	"qimeng.local/next/server/internal/store"
)

func TestNamingUsesTheLiveLibraryAndParentDirectoryIndex(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "naming.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.DB.Close()
	rows, err := db.DB.Query("EXPLAIN QUERY PLAN "+directoryNamesSQL, "fixture-library", "书架/插画")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plans []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plans = append(plans, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	plan := strings.Join(plans, "\n")
	if !strings.Contains(plan, "assets_live_directory") || !strings.Contains(plan, "<expr>=?") {
		t.Fatalf("naming did not seek the exact parent directory: %s", plan)
	}
	t.Log(plan)
}
