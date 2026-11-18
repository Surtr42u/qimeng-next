package httpapi_test

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"qimeng.local/next/server/internal/httpapi"
	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/service"
)

func monitoredLibrary(t *testing.T, native bool, interval time.Duration) (*fixture, string, model.Library) {
	t.Helper()
	storage := t.TempDir()
	s, err := service.New(service.Config{
		DataDir: t.TempDir(), AdminToken: testSecret, ReserveBytes: 1,
		StorageRoots: map[string]string{"media": storage},
		Scanner:      &service.LibraryMonitorConfig{WatchEnabled: native, PollInterval: interval},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	f := &fixture{t: t, s: s, h: httpapi.New(s)}
	var lib model.Library
	decode(t, f.json("POST", "/libraries", "register-monitored-media", map[string]string{
		"name": "Synthetic monitored library", "kind": "regular", "storageId": "media", "rootRelativePath": "registered",
	}, 201), &lib)
	f.library = lib.ID
	return f, filepath.Join(storage, "registered"), lib
}

func (f *fixture) waitForMonitor(condition string, ready func() bool) {
	f.t.Helper()
	for end := time.Now().Add(12 * time.Second); time.Now().Before(end); {
		if ready() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	var monitor service.LibraryMonitoring
	decode(f.t, f.json("GET", "/libraries/"+f.library+"/monitoring", "", nil, 200), &monitor)
	var scans model.Page[service.ScanJob]
	decode(f.t, f.json("GET", "/scans?libraryId="+f.library, "", nil, 200), &scans)
	f.t.Fatalf("%s: monitor=%+v scans=%+v", condition, monitor, scans)
}

func (f *fixture) monitoredAssets() []model.Asset {
	f.t.Helper()
	var assets model.Page[model.Asset]
	decode(f.t, f.json("GET", "/assets?libraryId="+f.library, "", nil, 200), &assets)
	return assets.Items
}

func TestNativeLibraryChangesPreserveMovedIdentityAndHumanTags(t *testing.T) {
	f, root, library := monitoredLibrary(t, true, time.Hour)
	monitorState := func(state string) bool {
		var monitor service.LibraryMonitoring
		decode(t, f.json("GET", "/libraries/"+library.ID+"/monitoring", "", nil, 200), &monitor)
		return monitor.State == state && (state != "watching" || monitor.WatchedDirectories > 0)
	}
	f.waitForMonitor("initial root registered", func() bool {
		if !monitorState("watching") {
			return false
		}
		var scans model.Page[service.ScanJob]
		decode(t, f.json("GET", "/scans?libraryId="+f.library, "", nil, 200), &scans)
		return len(scans.Items) > 0 && scans.Items[0].State == "completed" && scans.Items[0].Trigger == "watch"
	})
	sub := filepath.Join(root, "nested")
	if err := os.Mkdir(sub, 0700); err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(sub, "before.png")
	if err := os.WriteFile(original, pngBytes(t, 81), 0600); err != nil {
		t.Fatal(err)
	}
	var first model.Asset
	f.waitForMonitor("file in new directory indexed", func() bool {
		list := f.monitoredAssets()
		if len(list) != 1 || list[0].RelativePath != "nested/before.png" {
			return false
		}
		first = list[0]
		var monitor service.LibraryMonitoring
		decode(t, f.json("GET", "/libraries/"+library.ID+"/monitoring", "", nil, 200), &monitor)
		return monitor.State == "watching" && monitor.WatchedDirectories >= 2
	})
	decode(t, f.json("PUT", "/assets/"+first.ID+"/tags", "monitor-human-tags", map[string]any{
		"expectedRevision": first.TagsRevision, "tags": []string{"keep"},
	}, 200), &first)
	renamed := filepath.Join(sub, "after.png")
	if err := os.Rename(original, renamed); err != nil {
		t.Fatal(err)
	}
	f.waitForMonitor("external rename retains one tagged asset", func() bool {
		list := f.monitoredAssets()
		return len(list) == 1 && list[0].ID == first.ID && list[0].RelativePath == "nested/after.png" &&
			len(list[0].Tags) == 1 && list[0].Tags[0] == "keep"
	})
	updated := pngBytes(t, 90)
	if err := os.WriteFile(renamed, updated, 0600); err != nil {
		t.Fatal(err)
	}
	expectedHash := fmt.Sprintf("%x", sha256.Sum256(updated))
	f.waitForMonitor("external write advances the same asset version", func() bool {
		list := f.monitoredAssets()
		return len(list) == 1 && list[0].ID == first.ID && list[0].ContentVersion == first.ContentVersion+1 &&
			list[0].Hash == expectedHash && len(list[0].Tags) == 1 && list[0].Tags[0] == "keep"
	})
	decode(t, f.json("PATCH", "/libraries/"+library.ID, "disable-monitored-library",
		map[string]any{"expectedRevision": library.Revision, "enabled": false}, 200), &library)
	f.waitForMonitor("disabled library leaves monitoring", func() bool { return monitorState("disabled") })
	if err := os.WriteFile(filepath.Join(root, "offline.png"), pngBytes(t, 91), 0600); err != nil {
		t.Fatal(err)
	}
	var present int
	if err := f.s.DB.DB.QueryRow("SELECT count(*) FROM assets WHERE library_id=? AND relative_path='offline.png'", library.ID).Scan(&present); err != nil || present != 0 {
		t.Fatal("new media was indexed while disabled", err)
	}
	decode(t, f.json("PATCH", "/libraries/"+library.ID, "resume-monitored-library",
		map[string]any{"expectedRevision": library.Revision, "enabled": true}, 200), &library)
	f.waitForMonitor("re-enabled library covers its offline interval", func() bool {
		return monitorState("watching") && len(f.monitoredAssets()) == 2
	})
}

func TestLibraryPollingFindsExternalChangesWithNotificationsOff(t *testing.T) {
	f, root, lib := monitoredLibrary(t, false, time.Second)
	var status service.LibraryMonitoring
	decode(t, f.json("GET", "/libraries/"+lib.ID+"/monitoring", "", nil, 200), &status)
	if status.WatchEnabled || status.State != "polling" || status.PollIntervalSeconds != 1 {
		t.Fatal("polling status", status)
	}
	internal := filepath.Join(root, ".qimeng-internal", "trash")
	if err := os.MkdirAll(internal, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(internal, "private.png"), pngBytes(t, 92), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "polled.png"), pngBytes(t, 93), 0600); err != nil {
		t.Fatal(err)
	}
	f.waitForMonitor("periodic scan indexes only public originals", func() bool {
		items := f.monitoredAssets()
		if len(items) != 1 || items[0].FileName != "polled.png" {
			return false
		}
		var scans model.Page[service.ScanJob]
		decode(t, f.json("GET", "/scans?libraryId="+lib.ID+"&state=completed", "", nil, 200), &scans)
		for _, job := range scans.Items {
			if job.Trigger == "poll" && job.Scope == "library" && job.Added == 1 {
				return true
			}
		}
		return false
	})
}
