package service

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"qimeng.local/next/server/internal/model"
)

func TestResearchCacheRetriesInactiveSameProcessCleanup(t *testing.T) {
	ctx := context.Background()
	s, e := New(Config{DataDir: t.TempDir(), AdminToken: "synthetic-research-cache-owner-0000000000", ReserveBytes: 1})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	result := testMutation(t, s, "cache-library", func(w *Write) (any, int, error) {
		v, e := s.CreateLibrary(ctx, w, "Cache recovery", "regular")
		return v, 201, e
	})
	var library model.Library
	if e = json.Unmarshal(result.Body, &library); e != nil {
		t.Fatal(e)
	}
	cache, e := s.openResearchCache(ctx)
	if e != nil {
		t.Fatal(e)
	}
	id := ID()
	snapshot := "libraries/" + library.ID + "/.qimeng-internal/research-sources/" + id + ".png"
	// A nonempty directory deterministically makes Remove fail on Linux and Windows.
	// It stands in for an OS failure to release an interrupted private source link.
	if e = s.Root.MkdirAll(snapshot, 0700); e != nil {
		t.Fatal(e)
	}
	f, e := s.Root.OpenFile(snapshot+"/block", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		t.Fatal(e)
	}
	f.Close()
	if _, e = cache.Exec("INSERT INTO work_sources VALUES(?,?,?)", id, researchProcess, snapshot); e != nil {
		t.Fatal(e)
	}
	cache.Close()
	activeResearchSources.Store(id, struct{}{})
	current, e := s.openResearchCache(ctx)
	if e != nil {
		t.Fatal("active source was cleaned:", e)
	}
	current.Close()
	activeResearchSources.Delete(id)
	if unexpected, e := s.openResearchCache(ctx); e == nil {
		unexpected.Close()
		t.Fatal("same-process cleanup failure was hidden")
	}
	if e = s.Root.Remove(snapshot + "/block"); e != nil {
		t.Fatal(e)
	}
	recovered, e := s.openResearchCache(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer recovered.Close()
	var count int
	if e = recovered.QueryRow("SELECT count(*) FROM work_sources WHERE id=?", id).Scan(&count); e != nil {
		t.Fatal(e)
	}
	if count != 0 {
		t.Fatal("inactive source intent did not settle")
	}
	if _, e = s.Root.Stat(snapshot); !os.IsNotExist(e) {
		t.Fatal("private snapshot remained after successful retry")
	}
}
