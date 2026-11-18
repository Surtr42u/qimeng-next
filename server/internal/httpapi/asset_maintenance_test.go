package httpapi_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"qimeng.local/next/server/internal/httpapi"
	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/service"
)

func TestScannedWorkUsesScopedResearchAndTheSameNamingRules(t *testing.T) {
	f := setup(t)
	file, err := f.s.Root.OpenFile("libraries/"+f.library+"/source.png", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.Write(pngBytes(t, 109)); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	if job := f.scan("scan-for-existing-asset"); job.Added != 1 {
		t.Fatal("synthetic source was not indexed")
	}
	var assets model.Page[model.Asset]
	decode(t, f.json("GET", "/assets", "", nil, 200), &assets)
	a := assets.Items[0]
	author := createAuthor(t, f, "Verified synthetic creator")
	policy := service.DelegationPolicy{Label: "Existing media", LibraryIDs: []string{f.library}, AuthorIDs: []string{}, AllowedFields: []string{}, CandidateBlocks: []string{}, CanResearch: true, CanResearchAssets: true, AssetFields: []string{"authors", "tags", "rename"}, MaxResearchCalls: 2, MaxUpdates: 3, MaxTasksPerDay: 4, ValidDays: 1}
	var grant service.DelegationCreated
	decode(t, f.json("POST", "/ai/delegations", "", policy, 201), &grant)
	call := func(method, path, key, token string, body any, status int) *httptest.ResponseRecorder {
		t.Helper()
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		response := f.request(method, path, key, token, raw, nil)
		if response.Code != status {
			t.Fatalf("%s %s expected %d received %d %s", method, path, status, response.Code, response.Body.String())
		}
		return response
	}
	start := func(tag, secret string, fields []string) service.DelegatedTask {
		t.Helper()
		in := map[string]any{"kind": "asset", "label": "One selected work", "assetId": a.ID, "expectedContentVersion": a.ContentVersion, "expectedRevision": a.Revision, "assetFields": fields, "credentialSecret": secret}
		var task service.DelegatedTask
		decode(t, call("POST", "/ai/delegated-tasks", "task-start-"+tag, grant.Credential.Token, in, 201), &task)
		return task
	}
	secret := base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("q", 32)))
	task := start("research", secret, policy.AssetFields)
	route := "/asset-tasks/" + task.TaskID
	var materials service.ResearchMaterials
	decode(t, call("GET", route+"/materials", "", secret, nil, 200), &materials)
	if materials.AssetID != a.ID || materials.UploadID != "" || materials.ContentVersion != a.ContentVersion || len(materials.Artifacts) != 1 || materials.SourceSHA256 != a.Hash {
		t.Fatal("research lost the scanned file's exact identity")
	}
	call("GET", "/research-artifacts/"+materials.Artifacts[0].ID+"/content", "", secret, nil, 200)
	call("GET", "/research-artifacts/"+materials.Artifacts[0].ID+"/content", "", grant.Credential.Token, nil, 403)
	evidence := []service.AssetMaintenanceEvidence{{Field: "authors", URL: "https://example.org/synthetic-work", Observation: "Synthetic publisher credits this creator."}}
	write := map[string]any{"expectedRevision": a.Revision, "authorIds": []string{author.ID}, "evidence": evidence}
	var updated service.AssetMaintenanceTask
	decode(t, call("PATCH", route+"/metadata", "apply-scanned-creator", secret, write, 200), &updated)
	replay := call("PATCH", route+"/metadata", "apply-scanned-creator", secret, write, 200)
	if updated.UsedUpdates != 1 || !strings.Contains(replay.Body.String(), `"usedUpdates":1`) {
		t.Fatal("replay repeated the association")
	}
	var records model.Page[service.AssetMaintenanceRecord]
	decode(t, call("GET", route+"/evidence", "", secret, nil, 200), &records)
	if len(records.Items) != 1 || records.Items[0].Evidence[0].URL != evidence[0].URL {
		t.Fatal("applied evidence unavailable for later research")
	}
	decode(t, f.json("GET", "/assets/"+a.ID, "", nil, 200), &a)
	if a.AuthorID != author.ID || a.ContentVersion != 1 {
		t.Fatal("metadata changed the file version or failed to link the creator")
	}
	previewInput := service.NamingPreviewInput{Intent: "new_work", Family: "Synthetic series", MediaType: "image", Items: []service.NamingInputItem{{FileName: a.FileName}}}
	var preview service.NamingPreview
	decode(t, f.json("POST", "/libraries/"+f.library+"/naming/preview", "", previewInput, 200), &preview)
	action := service.AssetTaskRename{ExpectedRevision: a.Revision, NamespaceFingerprint: preview.NamespaceFingerprint, Target: preview.Items[0].RelativePath, Preview: previewInput}
	wrong := action
	wrong.NamespaceFingerprint = "obsolete"
	call("POST", route+"/rename", "stale-scanned-name", secret, wrong, 409)
	decode(t, call("POST", route+"/rename", "publish-scanned-name", secret, action, 200), &a)
	if a.RelativePath != action.Target || a.ID != assets.Items[0].ID || a.ContentVersion != 1 {
		t.Fatal("using the naming preview replaced or duplicated the scanned work")
	}
	var uploads int
	if err = f.s.DB.DB.QueryRow("SELECT count(*) FROM uploads").Scan(&uploads); err != nil || uploads != 0 {
		t.Fatal("scanned work was re-ingested")
	}
	decode(t, f.json("PUT", "/assets/"+a.ID+"/tags", "intentional-empty-set", map[string]any{"expectedRevision": a.TagsRevision, "tags": []string{}}, 200), &a)
	tags := map[string]any{"expectedRevision": a.Revision, "tags": []string{"late guess"}, "evidence": []service.AssetMaintenanceEvidence{{Field: "tags", URL: "https://example.org/fixture", Observation: "Fixture"}}}
	call("PATCH", route+"/metadata", "changed-while-task-ran", secret, tags, 409)
	secondSecret := base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("z", 32)))
	fresh := start("protect-cleared-tags", secondSecret, []string{"tags"})
	denied := call("PATCH", "/asset-tasks/"+fresh.TaskID+"/metadata", "respect-manual-empty", secondSecret, tags, 409)
	if !strings.Contains(denied.Body.String(), "human_field_protected") {
		t.Fatal("a new task could undo a deliberate human clearing")
	}
	p, err := f.s.Authenticate(context.Background(), secondSecret)
	if err != nil || p.Has("ingest") || p.Has("organize") || p.Has("admin") {
		t.Fatal("asset task gained unrelated authority")
	}
	t.Run("restore revokes the inherited task and keeps its evidence", func(t *testing.T) {
		original := *f
		original.t = t
		f := &original
		var checkpoint service.Backup
		decode(t, f.json("POST", "/backups", "existing-task-checkpoint", struct{}{}, 201), &checkpoint)
		f.json("POST", "/backups/"+checkpoint.ID+"/export", "export-existing-task", struct{}{}, 202)
		checkpoint = awaitBackup(t, f, checkpoint.ID)
		if checkpoint.ExportState != "completed" {
			t.Fatal(checkpoint.ExportError)
		}
		destination := filepath.Join(t.TempDir(), "restored")
		_, err := service.RestoreBackup(context.Background(), service.RestoreOptions{
			ArchivePath: filepath.Join(f.s.Config.DataDir, "backups", checkpoint.ID, "archive.qnext"),
			Destination: destination, ExpectedSHA256: checkpoint.ArchiveHash, ReserveBytes: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		restored, err := service.New(service.Config{DataDir: destination, AdminToken: testSecret, ReserveBytes: 1})
		if err != nil {
			t.Fatal(err)
		}
		defer restored.Close()
		reader := &fixture{t: t, s: restored, h: httpapi.New(restored)}
		var historical service.AssetMaintenanceTask
		decode(t, reader.json("GET", route, "", nil, 200), &historical)
		if historical.State != "revoked" || historical.AssetID != a.ID || historical.UsedUpdates < 1 {
			t.Fatal("previous scope remained active or facts disappeared", historical)
		}
		decode(t, reader.json("GET", route+"/evidence", "", nil, 200), &records)
		if len(records.Items) == 0 || records.Items[0].Evidence[0].URL != evidence[0].URL {
			t.Fatal("lost previous research")
		}
		if response := reader.request("GET", route, "", secret, nil, nil); response.Code != 401 {
			t.Fatal("old credential still authenticated", response.Code)
		}
		update := reader.json("PATCH", route+"/metadata", "restored-task-write", tags, 409)
		if !strings.Contains(update.Body.String(), "asset_task_revoked") {
			t.Fatal("owner could still act under predecessor's task", update.Body.String())
		}
		if materials := reader.json("GET", route+"/materials", "", nil, 409); !strings.Contains(materials.Body.String(), "asset_task_revoked") {
			t.Fatal("historical task produced new research material")
		}
	})
}
