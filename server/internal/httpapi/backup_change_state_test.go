package httpapi_test

import (
	"strings"
	"testing"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/service"
)

func TestLocalBackupRotationTracksBusinessFactsNotBackupReads(t *testing.T) {
	f := setup(t)
	revision := func() string {
		var state service.BackupChangeState
		decode(t, f.json("GET", "/backups/change-state", "", nil, 200), &state)
		if state.InstanceID != f.s.InstanceID || state.BusinessRevision == "" {
			t.Fatal("incomplete backup change identity")
		}
		return state.BusinessRevision
	}
	first := revision()
	author := createAuthor(t, f, "Readable creator")
	u, _ := f.upload("archived-original.png", 239, map[string]any{"authorIds": []string{author.ID}})
	asset := f.complete(u).Asset
	ready := revision()
	if ready == first {
		t.Fatal("new media did not advance maintenance revision")
	}
	var backup service.Backup
	decode(t, f.json("POST", "/backups", "rotation-first-checkpoint", map[string]any{}, 201), &backup)
	if backup.BusinessRevision != ready || revision() != ready {
		t.Fatal("a checkpoint changed its own rotation anchor")
	}
	firstDoc := f.request("GET", "/backups/"+backup.ID+"/author-documents", "", testSecret, nil, nil)
	if firstDoc.Code != 200 || !strings.Contains(firstDoc.Body.String(), "Readable creator") || !strings.Contains(firstDoc.Body.String(), "archived-original.png") {
		t.Fatal("readable checkpoint omitted the author or associated original")
	}
	f.json("GET", "/backups/"+backup.ID, "", nil, 200)
	if revision() != ready {
		t.Fatal("downloading or polling rotated an unchanged generation")
	}
	var moved model.Asset
	decode(t, f.json("POST", "/assets/"+asset.ID+"/move", "revision-name-change", map[string]any{"expectedRevision": asset.Revision, "relativePath": "updated-name.png"}, 200), &moved)
	if revision() == ready {
		t.Fatal("updated work name did not advance maintenance revision")
	}
	secondDoc := f.request("GET", "/backups/"+backup.ID+"/author-documents", "", testSecret, nil, nil)
	if secondDoc.Code != 200 || firstDoc.Body.String() != secondDoc.Body.String() {
		t.Fatal("old checkpoint incorporated a later work name")
	}
}

func TestSecretIssuanceRejectsReplayStorage(t *testing.T) {
	f := setup(t)
	for _, route := range []string{"/credentials", "/ai/delegations"} {
		reply := f.json("POST", route, "do-not-store-one-time-secret", map[string]any{}, 400)
		if !strings.Contains(reply.Body.String(), "secret_not_replayable") {
			t.Fatal("credential endpoint entered generic receipt handling")
		}
	}
	var count int
	if err := f.s.DB.DB.QueryRow("SELECT count(*) FROM idempotency WHERE key='do-not-store-one-time-secret'").Scan(&count); err != nil || count != 0 {
		t.Fatal("one-time credential response left a replay record")
	}
}
