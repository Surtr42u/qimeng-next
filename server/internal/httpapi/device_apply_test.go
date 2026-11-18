package httpapi_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"qimeng.local/next/server/internal/httpapi"
	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/service"
)

func putDevicePackage(t *testing.T, f *fixture, key string, m service.DeviceApplyManifest, files map[string][]byte, status int) service.DeviceApplyTask {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	manifest, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err = form.WriteField("manifest", string(manifest)); err != nil {
		t.Fatal(err)
	}
	for _, part := range m.Segments {
		out, err := form.CreateFormFile(part.Kind, part.Kind)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = out.Write(files[part.Kind]); err != nil {
			t.Fatal(err)
		}
	}
	if err = form.Close(); err != nil {
		t.Fatal(err)
	}
	r := f.request("POST", "/device/apply-tasks", key, testSecret, body.Bytes(), map[string]string{"Content-Type": form.FormDataContentType()})
	if r.Code != status {
		t.Fatalf("package publication: %d %s", r.Code, r.Body.String())
	}
	var task service.DeviceApplyTask
	if status == 201 {
		decode(t, r, &task)
	}
	return task
}

func TestDeviceScopedPackageResumesAcrossChunkBoundaryAndReportsRealLocalImports(t *testing.T) {
	origin, local := setup(t), setup(t)
	register := func(device, label, secret, previous string) service.DeviceCredential {
		t.Helper()
		var v service.DeviceCredential
		r := origin.json("POST", "/device/credentials", "create-device-"+label, service.DeviceCredentialInput{
			DeviceID: device, Label: label, CredentialSecret: secret, ReplaceCredentialID: previous,
		}, 201)
		if strings.Contains(r.Body.String(), secret) {
			t.Fatal("device secret leaked into receipt")
		}
		decode(t, r, &v)
		return v
	}
	secret := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{'p'}, 32))
	other := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{'o'}, 32))
	originalCredential := register("phone", "synthetic-phone", secret, "")
	register("other", "second-device", other, "")
	var secretCopies int
	if err := origin.s.DB.DB.QueryRow("SELECT COUNT(*) FROM idempotency WHERE instr(CAST(response AS TEXT),?)>0", secret).Scan(&secretCopies); err != nil || secretCopies != 0 {
		t.Fatal("credential was persisted in a replay body", err)
	}
	u, _ := local.upload("receiver.png", 129, nil)
	asset := *local.complete(u).Asset
	raw := v2Document(t, asset, map[string]any{"favorites": []string{"source-record"}})
	raw = append(raw, bytes.Repeat([]byte{' '}, (1<<20)+23)...)
	files := map[string][]byte{"masterTxt": []byte("1  Synthetic receiver author\n作品\nreceiver.png\n" + strings.Repeat("\n", 600<<10)), "v2Backup": raw}
	rev := int64(0)
	manifest := service.DeviceApplyManifest{TargetDeviceID: "phone", ProtocolVersion: 1, Segments: []service.DeviceApplySegment{
		{Kind: "masterTxt", ExpectedLocalRevision: &rev}, {Kind: "v2Backup", SourceID: "synthetic-installation"},
	}}
	for i := range manifest.Segments {
		seg := &manifest.Segments[i]
		body := files[seg.Kind]
		seg.Bytes = int64(len(body))
		seg.SHA256 = fmt.Sprintf("%x", sha256.Sum256(body))
	}
	task := putDevicePackage(t, origin, "publish-real-local-imports", manifest, files, 201)
	replay := putDevicePackage(t, origin, "publish-real-local-imports", manifest, files, 201)
	if replay.ID != task.ID || task.State != "pending" || task.TargetDeviceID != "phone" {
		t.Fatal("package replay or device binding changed")
	}
	unverified := manifest
	unverified.Segments = append([]service.DeviceApplySegment{}, manifest.Segments...)
	unverified.Segments[0].SHA256 = strings.Repeat("f", 64)
	putDevicePackage(t, origin, "reject-unverified-digest", unverified, files, 409)
	url := "/device/apply-tasks/" + task.ID
	request := func(method, path, key, credential string, body any, status int) *httptest.ResponseRecorder {
		t.Helper()
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		r := origin.request(method, path, key, credential, encoded, nil)
		if r.Code != status {
			t.Fatalf("%s %s: %d %s", method, path, r.Code, r.Body.String())
		}
		return r
	}
	request("GET", "/assets", "", secret, nil, 403)
	var identity model.Principal
	decode(t, request("GET", "/sessions/current", "", secret, nil, 200), &identity)
	if identity.ID != originalCredential.ID || identity.DeviceID != "phone" || len(identity.Scopes) != 1 || identity.Scopes[0] != "device.apply" {
		t.Fatal("unexpected device credential scope", identity.ID)
	}
	request("GET", url, "", other, nil, 403)
	var pending model.Page[service.DeviceApplyTask]
	decode(t, request("GET", "/device/apply-tasks?targetDeviceId=phone&state=pending", "", secret, nil, 200), &pending)
	if len(pending.Items) != 1 || pending.Items[0].ID != task.ID {
		t.Fatal("receiver did not find its published package")
	}
	request("GET", "/device/apply-tasks?targetDeviceId=other", "", secret, nil, 403)
	request("GET", url+"/segments/masterTxt", "", secret, nil, 409)
	var page service.DeviceApplyTask
	decode(t, request("POST", url+"/claim", "claim-device-once", secret, map[string]int{"expectedRevision": 1}, 200), &page)
	if page.State != "claimed" || page.Revision != 2 {
		t.Fatal("claim not durable")
	}
	newSecret := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{'n'}, 32))
	register("phone", "rotated-phone", newSecret, originalCredential.ID)
	request("GET", url, "", secret, nil, 401)
	decode(t, request("POST", url+"/claim", "resume-device-claim", newSecret, map[string]int{"expectedRevision": 2}, 200), &page)
	if page.Revision != 2 {
		t.Fatal("resuming created a new claim")
	}
	var before service.Backup
	decode(t, local.json("POST", "/backups", "before-device-application", struct{}{}, 201), &before)
	local.json("POST", "/backups/"+before.ID+"/export", "export-before-device-application", struct{}{}, 202)
	before = awaitBackup(t, local, before.ID)
	if before.ExportState != "completed" || before.ArchiveHash == "" {
		t.Fatal("local pre-application backup unavailable", before.ExportError)
	}
	var outcomes []service.DeviceApplyOutcome
	for _, seg := range page.Segments {
		split := int64(7)
		if seg.Kind == "v2Backup" {
			split = (1 << 20) - 4
		}
		var got []byte
		for _, value := range []string{fmt.Sprintf("bytes=0-%d", split-1), fmt.Sprintf("bytes=%d-", split)} {
			r := origin.request("GET", url+"/segments/"+seg.Kind, "", newSecret, nil, map[string]string{"Range": value, "If-Range": `"` + seg.SHA256 + `"`})
			if r.Code != 206 || r.Header().Get("X-Content-SHA256") != seg.SHA256 {
				t.Fatalf("invalid resumed bytes: %d", r.Code)
			}
			got = append(got, r.Body.Bytes()...)
		}
		if int64(len(got)) != seg.Bytes || fmt.Sprintf("%x", sha256.Sum256(got)) != seg.SHA256 {
			t.Fatal("downloaded segment changed")
		}
		var receipt string
		switch seg.Kind {
		case "masterTxt":
			res := importList(t, local, "receiver-apply-master", "作者总表.txt", string(got), *seg.ExpectedLocalRevision)
			if res.List == nil {
				t.Fatal("missing local TXT result")
			}
			receipt = res.List.ID
			local.json("POST", "/author-lists", "stale-device-txt", map[string]any{"name": "作者总表.txt", "content": string(got), "expectedRevision": 0}, 409)
		case "v2Backup":
			r := local.request("POST", "/migration/v2/imports?sourceId="+seg.SourceID, "device-local-v2-input", testSecret, got, map[string]string{"Content-Type": "application/octet-stream"})
			if r.Code != 201 {
				t.Fatal("stage received V2", r.Code)
			}
			var in service.V2Import
			decode(t, r, &in)
			plan := planV2(t, local, in, "device-local-v2-plan")
			decode(t, local.json("POST", "/migration/v2/plans/"+plan.ID+"/commit", "device-local-v2-commit",
				map[string]any{"fingerprint": plan.Fingerprint, "acceptSkipped": false}, 200), &plan)
			receipt = plan.ID
		}
		outcomes = append(outcomes, service.DeviceApplyOutcome{Kind: seg.Kind, State: "applied", SHA256: seg.SHA256, ReceiptID: receipt})
	}
	var current model.Asset
	decode(t, local.json("GET", "/assets/"+asset.ID, "", nil, 200), &current)
	if !current.Favorite || current.AuthorName != "Synthetic receiver author" {
		t.Fatal("local authorized metadata was not applied", current)
	}
	result := service.DeviceApplyReport{State: "applied", ExpectedRevision: page.Revision, Results: outcomes}
	decode(t, request("POST", url+"/result", "real-device-result", newSecret, result, 200), &page)
	if page.State != "applied" || len(page.Results) != 2 {
		t.Fatal("missing device receipt")
	}
	request("POST", url+"/result", "resume-real-device-result", newSecret, result, 200)
	request("GET", url+"/segments/v2Backup", "", newSecret, nil, 409)
	request("POST", url+"/claim", "resume-device-claim", newSecret, map[string]int{"expectedRevision": 2}, 409)
	result.Results[1].ReceiptID = "different-local-receipt"
	request("POST", url+"/result", "immutable-device-terminal", newSecret, result, 409)

	waiting := service.DeviceApplyManifest{TargetDeviceID: "phone", ProtocolVersion: 1, Segments: manifest.Segments[:1]}
	notTaken := putDevicePackage(t, origin, "also-snapshot-pending-device", waiting, files, 201)
	var checkpoint service.Backup
	decode(t, origin.json("POST", "/backups", "snapshot-device-decisions", struct{}{}, 201), &checkpoint)
	origin.json("POST", "/backups/"+checkpoint.ID+"/export", "export-device-decisions", struct{}{}, 202)
	checkpoint = awaitBackup(t, origin, checkpoint.ID)
	if checkpoint.ExportState != "completed" {
		t.Fatal(checkpoint.ExportError)
	}
	restoredRoot := filepath.Join(t.TempDir(), "restored-origin")
	_, err := service.RestoreBackup(context.Background(), service.RestoreOptions{ArchivePath: filepath.Join(origin.s.Config.DataDir, "backups", checkpoint.ID, "archive.qnext"),
		Destination: restoredRoot, ExpectedSHA256: checkpoint.ArchiveHash, ReserveBytes: 1})
	if err != nil {
		t.Fatal(err)
	}
	restored, err := service.New(service.Config{DataDir: restoredRoot, AdminToken: testSecret, ReserveBytes: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	historical := &fixture{t: t, s: restored, h: httpapi.New(restored)}
	decode(t, historical.json("GET", url, "", nil, 200), &page)
	if page.State != "applied" || len(page.Results) != 2 {
		t.Fatal("restoration removed the existing result")
	}
	decode(t, historical.json("GET", "/device/apply-tasks/"+notTaken.ID, "", nil, 200), &page)
	if page.State != "cancelled" {
		t.Fatal("old instance could still dispatch the pending package")
	}
	if r := historical.request("GET", "/sessions/current", "", newSecret, nil, nil); r.Code != 401 {
		t.Fatal("restored instance recognized the old device credential")
	}
}
