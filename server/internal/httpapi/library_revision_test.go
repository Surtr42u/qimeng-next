package httpapi_test

import (
	"testing"

	"qimeng.local/next/server/internal/service"
)

func readLibraryRevision(t *testing.T, f *fixture) service.LibraryRevisionState {
	t.Helper()
	var state service.LibraryRevisionState
	decode(t, f.json("GET", "/library/revision", "", nil, 200), &state)
	if state.InstanceID != f.s.InstanceID {
		t.Fatalf("revision instance %q want %q", state.InstanceID, f.s.InstanceID)
	}
	if state.ThumbnailVersion != service.PreviewPolicyVersion() {
		t.Fatalf("thumbnail version %q", state.ThumbnailVersion)
	}
	return state
}

func TestLibraryRevisionRequiresReadAndTracksPreviewFacts(t *testing.T) {
	f := setup(t)
	if r := f.request("GET", "/library/revision", "", "", nil, nil); r.Code != 401 {
		t.Fatalf("anonymous revision status %d", r.Code)
	}
	const reader = "synthetic-revision-read-only-credential"
	f.s.Config.AIToken = reader
	if r := f.request("GET", "/library/revision", "", reader, nil, nil); r.Code != 200 {
		t.Fatalf("read-only revision status %d: %s", r.Code, r.Body.String())
	}

	initial := readLibraryRevision(t, f)
	if initial.Revision < 2 {
		t.Fatalf("library creation did not advance revision: %d", initial.Revision)
	}

	u, _ := f.upload("revision.png", 71, nil)
	asset := f.complete(u).Asset
	withAsset := readLibraryRevision(t, f)
	if withAsset.Revision <= initial.Revision {
		t.Fatal("asset content did not advance revision")
	}

	f.json("POST", "/assets/"+asset.ID+"/events", "", map[string]any{
		"eventId": "revision-browse-only", "kind": "view", "contentVersion": asset.ContentVersion,
	}, 200)
	if after := readLibraryRevision(t, f); after.Revision != withAsset.Revision {
		t.Fatalf("browsing event advanced revision: %d -> %d", withAsset.Revision, after.Revision)
	}

	f.json("PATCH", "/libraries/"+f.library, "revision-library-rename", map[string]any{
		"expectedRevision": 1, "name": "Renamed",
	}, 200)
	if after := readLibraryRevision(t, f); after.Revision <= withAsset.Revision {
		t.Fatal("library data update did not advance revision")
	}
}
