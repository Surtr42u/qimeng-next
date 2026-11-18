package httpapi_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"qimeng.local/next/server/internal/httpapi"
	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/service"
)

func waitSyncCycle(t *testing.T, f *fixture, after time.Time) service.LocalSyncStatus {
	t.Helper()
	for end := time.Now().Add(12 * time.Second); time.Now().Before(end); {
		var status service.LocalSyncStatus
		decode(t, f.json("GET", "/local-sync/status", "", nil, 200), &status)
		if !status.Running && status.LastCycleAt != nil && status.LastCycleAt.After(after) {
			return status
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("local folder cycle did not finish")
	return service.LocalSyncStatus{}
}
func nextSyncCycle(t *testing.T, f *fixture, key string) service.LocalSyncStatus {
	t.Helper()
	before := time.Now()
	f.json("POST", "/local-sync/trigger", key, struct{}{}, 202)
	return waitSyncCycle(t, f, before)
}
func TestConfiguredInputFolderRetainsSourcesAcrossRestartsAndAppliesTXTWhileUploadsPaused(t *testing.T) {
	root := t.TempDir()
	cfg := service.Config{DataDir: t.TempDir(), AdminToken: testSecret, ReserveBytes: 1, LocalSync: service.LocalSyncConfig{Root: root, StableAge: time.Millisecond, Interval: time.Hour}}
	s, e := service.New(cfg)
	if e != nil {
		t.Fatal(e)
	}
	f := &fixture{t: t, s: s, h: httpapi.New(s)}
	t.Cleanup(func() { f.s.Close() })
	waitSyncCycle(t, f, time.Time{})
	var lib model.Library
	decode(t, f.json("POST", "/libraries", "local-library-register", map[string]string{"name": "Synthetic regular", "kind": "regular"}, 201), &lib)
	sub := filepath.Join(root, "Synthetic regular", "sub")
	if e = os.MkdirAll(sub, 0700); e != nil {
		t.Fatal(e)
	}
	raw := pngBytes(t, 133)
	source := filepath.Join(sub, "投放.png")
	if e = os.WriteFile(source, raw, 0600); e != nil {
		t.Fatal(e)
	}
	text := []byte("1  合成作者\n来源  素材库\n作品\n投放.png\n")
	if e = os.WriteFile(filepath.Join(root, "名单.txt"), text, 0600); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(root, ".private.txt"), []byte("ignored"), 0600); e != nil {
		t.Fatal(e)
	}
	first := nextSyncCycle(t, f, "observe-file-versions")
	if first.Counts.WaitingStable != 2 || first.Counts.Ignored != 1 || first.SyncedTotal != 0 {
		t.Fatalf("first observations: %+v", first)
	}
	var settings service.Settings
	decode(t, f.json("GET", "/settings", "", nil, 200), &settings)
	pause := func(enabled bool, key string) {
		decode(t, f.json("PUT", "/settings", key, map[string]any{"expectedRevision": settings.Revision, "scan": map[string]int{"workers": 0, "thumbEdge": 800}, "upload": map[string]bool{"autoAccept": enabled}}, 200), &settings)
	}
	pause(false, "pause-input-folder")
	paused := nextSyncCycle(t, f, "process-txt-only")
	if !paused.Paused || paused.SyncedTotal != 1 || paused.Counts.WaitingStable != 1 {
		t.Fatalf("paused cycle: %+v", paused)
	}
	pause(true, "resume-input-folder")
	success := nextSyncCycle(t, f, "process-nested-media")
	if success.Counts.Failed != 0 || success.SyncedTotal != 2 || len(success.RecentSynced) != 2 {
		t.Fatalf("import result: %+v", success)
	}
	var assets model.Page[model.Asset]
	decode(t, f.json("GET", "/assets", "", nil, 200), &assets)
	if len(assets.Items) != 1 || assets.Items[0].AuthorName != "合成作者" || assets.Items[0].RelativePath != "sub/投放.png" {
		t.Fatal("folder import bypassed naming/author associations", assets)
	}
	if actual, e := os.ReadFile(source); e != nil || !bytes.Equal(raw, actual) {
		t.Fatal("source changed after import", e)
	}
	if actual, e := os.ReadFile(filepath.Join(root, "名单.txt")); e != nil || !bytes.Equal(text, actual) {
		t.Fatal("text source was moved", e)
	}
	if e = f.s.Close(); e != nil {
		t.Fatal(e)
	}
	f.s, e = service.New(cfg)
	if e != nil {
		t.Fatal(e)
	}
	f.h = httpapi.New(f.s)
	restarted := waitSyncCycle(t, f, time.Time{})
	if restarted.SyncedTotal != 0 || len(restarted.RecentSynced) != 2 {
		t.Fatal("unchanged retained versions re-imported")
	}
	var uploads int
	if e = f.s.DB.DB.QueryRow("SELECT count(*) FROM uploads").Scan(&uploads); e != nil || uploads != 1 {
		t.Fatal("restart created another upload", e)
	}
}

func TestLocalSyncUnconfiguredOrOverlappingRootsAreRejected(t *testing.T) {
	f := setup(t)
	var off service.LocalSyncStatus
	decode(t, f.json("GET", "/local-sync/status", "", nil, 200), &off)
	if off.Enabled || off.Root != "" {
		t.Fatal("unconfigured watcher became active")
	}
	f.json("POST", "/local-sync/trigger", "disabled-folder-trigger", struct{}{}, 409)
	data := t.TempDir()
	reader := strings.Repeat("a", 32)
	s, e := service.New(service.Config{DataDir: data, AdminToken: testSecret, AIToken: reader, ReserveBytes: 1, LocalSync: service.LocalSyncConfig{Root: data, Interval: time.Hour}})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	other := &fixture{t: t, s: s, h: httpapi.New(s)}
	invalid := waitSyncCycle(t, other, time.Time{})
	if invalid.Enabled || invalid.LastError == "" {
		t.Fatal("overlapping root accepted")
	}
	other.json("POST", "/local-sync/trigger", "unsafe-folder-trigger", struct{}{}, 409)
	if r := other.request("GET", "/local-sync/status", "", reader, nil, nil); r.Code != 403 || strings.Contains(r.Body.String(), data) {
		t.Fatal("host path disclosed to a read-only model")
	}
}
