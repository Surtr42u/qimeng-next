package httpapi_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/service"
)

type permitReadGate struct {
	context.Context
	scratch string
	wait    func()
}

func (g permitReadGate) Err() error {
	if _, err := os.Stat(g.scratch); err == nil {
		g.wait()
	}
	return g.Context.Err()
}

func TestLegacyPermitListRechecksOutcomesAfterIndependentRead(t *testing.T) {
	f := setup(t)
	distinctProtectionTarget(t, f)
	var batch model.Batch
	decode(t, f.json("POST", "/batches", "legacy-permits-in-flight", map[string]any{"libraryId": f.library, "mode": "ordinary", "label": "Synthetic permit outcomes"}, 201), &batch)
	names := []string{"recorded.png", "replaced.png", "expired.png", "reported-late.png"}
	ids := f.declareInputs(batch.ID, names...)
	var originals []model.UploadResult
	for i, name := range names {
		upload, _ := f.upload(name, uint8(50+i), map[string]any{"batchId": batch.ID, "inputId": ids[name]})
		originals = append(originals, f.complete(upload))
	}
	proof := completedProtection(t, f, batch.ID, "prior-independent-permits")
	url := "/batches/" + batch.ID + "/protections/" + proof.ID + "/cleanup-permits"
	var permits []service.CleanupPermit
	for _, item := range proof.Items {
		var permit service.CleanupPermit
		body := service.CleanupPermitInput{InputID: item.InputID, ExpectedHash: item.Hash, SourceSize: item.Size}
		decode(t, f.json("POST", url, "old-issued-"+item.InputID, body, 201), &permit)
		permits = append(permits, permit)
	}
	if len(permits) != len(names) {
		t.Fatal("missing synthetic issued permits")
	}
	expire := func(id string) {
		t.Helper()
		past := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)
		if _, err := f.s.DB.DB.Exec("UPDATE source_cleanup_permits SET expires_at=?,data=json_set(data,'$.expiresAt',?) WHERE id=?", past, past, id); err != nil {
			t.Fatal(err)
		}
	}
	expire(permits[3].ID)
	if _, err := f.s.DB.DB.Exec("UPDATE batch_protections SET data=json_remove(data,'$.snapshotInputsVerified') WHERE id=?", proof.ID); err != nil {
		t.Fatal(err)
	}
	local := filepath.Join(f.s.Config.DataDir, "backups", proof.CheckpointID, "snapshot.sqlite")
	if err := os.Rename(local, local+".held"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	owner, err := f.s.Authenticate(ctx, testSecret)
	if err != nil {
		t.Fatal(err)
	}
	started, proceed := make(chan struct{}), make(chan struct{})
	scratch := filepath.Join(f.s.Config.DataDir, "cache", "protection-read-"+proof.InstanceID, "snapshot.sqlite")
	gate := permitReadGate{ctx, scratch, sync.OnceFunc(func() {
		close(started)
		select {
		case <-proceed:
		case <-ctx.Done():
		}
	})}
	type result struct {
		page model.Page[service.CleanupPermit]
		err  error
	}
	done := make(chan result, 1)
	go func() {
		page, err := f.s.CleanupPermits(gate, owner, batch.ID, "", "", 20)
		done <- result{page, err}
	}()
	select {
	case <-started:
	case early := <-done:
		t.Fatalf("list returned before reading the independent snapshot: %v", early.err)
	case <-ctx.Done():
		t.Fatal("list did not reach snapshot verification")
	}
	f.json("POST", "/cleanup-permits/"+permits[0].ID+"/receipt", "record-while-reading-snapshot", service.CleanupReceiptInput{Outcome: "retained"}, 200)
	a := originals[1].Asset
	replacement, _ := f.upload("updated-original.png", 72, map[string]any{"replaceAssetId": a.ID, "expectedContentVersion": a.ContentVersion})
	f.complete(replacement)
	expire(permits[2].ID)
	f.json("POST", "/cleanup-permits/"+permits[3].ID+"/receipt", "report-expired-while-reading", service.CleanupReceiptInput{Outcome: "retained"}, 200)
	close(proceed)
	got := <-done
	if got.err != nil || len(got.page.Items) != len(permits) {
		t.Fatalf("list did not return its selected permits: %v", got.err)
	}
	states := map[string]string{permits[0].ID: "recorded", permits[1].ID: "revoked", permits[2].ID: "expired", permits[3].ID: "recorded"}
	for _, p := range got.page.Items {
		if p.State != states[p.ID] || p.State == "recorded" && (p.Receipt == nil || p.Receipt.Outcome != "retained") {
			t.Fatalf("stale permit after snapshot read: %s %s", p.ID, p.State)
		}
	}
}

func TestLegacyProtectionRetryAndIssueFromIndependentCopy(t *testing.T) {
	f := setup(t)
	target := distinctProtectionTarget(t, f)
	batch, a, pending := batchWithPendingSecondUpload(t, f)
	proof := completedProtection(t, f, batch.ID, "independent-before-second-upload")
	b := f.complete(pending)
	modern := completedProtection(t, f, batch.ID, "independent-after-second-upload")
	backupDir := filepath.Join(f.s.Config.DataDir, "backups")
	local := filepath.Join(backupDir, proof.CheckpointID, "snapshot.sqlite")
	external := filepath.Join(target, "instances", proof.InstanceID, "snapshots", proof.SnapshotHash+".sqlite")
	checkProtectionSnapshot(t, external, batch, a, b, false)
	if err := os.Rename(local, local+".held"); err != nil {
		t.Fatal(err)
	}

	// The independent receipt already exists, but the old producer stopped
	// before committing it. No verified-input marker existed in its report.
	_, err := f.s.DB.DB.Exec(`UPDATE batch_protections SET state='interrupted',data=json_remove(
 json_set(data,'$.state','interrupted'),'$.snapshotInputsVerified','$.proofHash','$.verifiedAt') WHERE id=?`, proof.ID)
	if err != nil {
		t.Fatal(err)
	}
	base := "/batches/" + batch.ID + "/protections/"
	f.json("POST", base+proof.ID+"/retry", "retry-with-only-independent-snapshot", struct{}{}, 202)
	retried := awaitProtection(t, f, batch.ID, proof.ID)
	if retried.State != "completed" || retried.ProofHash != proof.ProofHash || retried.CheckpointID != proof.CheckpointID {
		t.Fatalf("old receipt changed on retry: state=%s error=%s", retried.State, retried.ErrorCode)
	}
	var legacy bool
	if err = f.s.DB.DB.QueryRow("SELECT json_type(data,'$.snapshotInputsVerified') IS NULL FROM batch_protections WHERE id=?", proof.ID).Scan(&legacy); err != nil || !legacy {
		t.Fatalf("retry changed the historical verification contract: %v", err)
	}
	url := base + proof.ID + "/cleanup-permits"
	body := service.CleanupPermitInput{InputID: a.Upload.InputID, ExpectedHash: a.Asset.Hash, SourceSize: a.Asset.Size}
	var permit service.CleanupPermit
	decode(t, f.json("POST", url, "issue-with-independent-snapshot", body, 201), &permit)
	if permit.State != "issued" || permit.InputID != a.Upload.InputID || permit.ProofHash != proof.ProofHash {
		t.Fatal("new permit lost its original checkpoint and input")
	}
	detail := "/cleanup-permits/" + permit.ID
	blocked := func(status int, code string) {
		t.Helper()
		for _, response := range []*httptest.ResponseRecorder{
			f.json("GET", detail, "", nil, status),
			f.json("POST", url, "attempt-while-unverified", body, status),
		} {
			var rejection service.Error
			decode(t, response, &rejection)
			if rejection.Code != code {
				t.Fatalf("unverified evidence returned %s, want %s", rejection.Code, code)
			}
		}
	}
	if err = os.WriteFile(local, []byte("damaged synthetic local checkpoint"), 0600); err != nil {
		t.Fatal(err)
	}
	blocked(409, "content_mismatch")
	if err = os.Remove(local); err != nil {
		t.Fatal(err)
	}
	original := f.s.Config.BackupTargets[proof.TargetID]
	f.s.Config.BackupTargets[proof.TargetID] = service.BackupTarget{Path: target, FailureDomain: "different-synthetic-domain"}
	blocked(409, "backup_target_changed")
	f.s.Config.BackupTargets[proof.TargetID] = service.BackupTarget{Path: f.s.Config.DataDir, FailureDomain: original.FailureDomain}
	blocked(409, "backup_target_changed")
	f.s.Config.BackupTargets[proof.TargetID] = service.BackupTarget{Path: filepath.Join(target, "offline"), FailureDomain: original.FailureDomain}
	blocked(503, "backup_target_unavailable")
	f.s.Config.BackupTargets[proof.TargetID] = original
	if err = os.Rename(external, external+".held"); err != nil {
		t.Fatal(err)
	}
	blocked(409, "snapshot_unavailable")
	if err = os.WriteFile(external, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Truncate(external, proof.SnapshotBytes); err != nil {
		t.Fatal(err)
	}
	blocked(409, "backup_target_content_changed")
	if err = os.Remove(external); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(external+".held", external); err != nil {
		t.Fatal(err)
	}
	scratch := filepath.Join(f.s.Config.DataDir, "cache", "protection-read-"+proof.InstanceID, "snapshot.sqlite")
	if err = os.Mkdir(scratch, 0700); err != nil {
		t.Fatal(err)
	}
	blocked(409, "snapshot_scratch_occupied")
	// A local legacy snapshot and a modern proof still work while the scratch
	// path is occupied. The new proof can cover the later ingested input.
	if err = os.Rename(local+".held", local); err != nil {
		t.Fatal(err)
	}
	f.json("GET", detail, "", nil, 200)
	if err = os.Rename(local, local+".held"); err != nil {
		t.Fatal(err)
	}
	modernLocal := filepath.Join(backupDir, modern.CheckpointID, "snapshot.sqlite")
	if err = os.Rename(modernLocal, modernLocal+".held"); err != nil {
		t.Fatal(err)
	}
	modernBody := service.CleanupPermitInput{InputID: b.Upload.InputID, ExpectedHash: b.Asset.Hash, SourceSize: b.Asset.Size}
	f.json("POST", base+modern.ID+"/cleanup-permits", "modern-later-input-permit", modernBody, 201)
	if info, err := os.Stat(scratch); err != nil || !info.IsDir() {
		t.Fatalf("unexpected scratch object was removed: %v", err)
	}
	if err = os.Remove(scratch); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(scratch, []byte("interrupted private copy"), 0600); err != nil {
		t.Fatal(err)
	}
	unrelated := filepath.Join(filepath.Dir(scratch), "unrelated")
	if err = os.WriteFile(unrelated, []byte("retained"), 0600); err != nil {
		t.Fatal(err)
	}
	responses := make(chan *httptest.ResponseRecorder, 3)
	for range cap(responses) {
		go func() { responses <- f.request("GET", detail, "", testSecret, nil, nil) }()
	}
	for range cap(responses) {
		r := <-responses
		var current service.CleanupPermit
		decode(t, r, &current)
		if r.Code != 200 || current.ID != permit.ID || current.State != "issued" {
			t.Fatalf("concurrent permit read did not recover: %d %s", r.Code, current.State)
		}
	}
	var replay service.CleanupPermit
	var page model.Page[service.CleanupPermit]
	decode(t, f.json("POST", url, "issue-with-independent-snapshot", body, 201), &replay)
	decode(t, f.json("GET", "/batches/"+batch.ID+"/cleanup-permits?inputId="+a.Upload.InputID, "", nil, 200), &page)
	if replay.ID != permit.ID || replay.State != "issued" || len(page.Items) != 1 || page.Items[0].ID != permit.ID || page.Items[0].State != "issued" {
		t.Fatal("temporary unavailability changed the original permit")
	}
	if _, err = os.Stat(scratch); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("private scratch survived the read: %v", err)
	}
	if content, err := os.ReadFile(unrelated); err != nil || string(content) != "retained" {
		t.Fatalf("unrelated file changed: %v", err)
	}
}
