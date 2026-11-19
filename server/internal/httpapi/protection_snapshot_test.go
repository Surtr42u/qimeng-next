package httpapi_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/service"
	"qimeng.local/next/server/internal/store"
)

func distinctProtectionTarget(t *testing.T, f *fixture) string {
	t.Helper()
	var parents []string
	if runtime.GOOS == "linux" {
		parents = append(parents, "/dev/shm")
	}
	if os.Getenv("GITHUB_ACTIONS") == "true" {
		parents = append(parents, os.Getenv("GITHUB_WORKSPACE"), os.Getenv("RUNNER_TEMP"))
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			parents = append(parents, filepath.Join(local, "Temp"))
		}
	}
	for _, parent := range parents {
		if parent == "" {
			continue
		}
		target, err := os.MkdirTemp(parent, "qimeng-synthetic-protection-")
		if err != nil {
			continue
		}
		t.Cleanup(func() { os.RemoveAll(target) })
		f.s.Config.BackupTargets = map[string]service.BackupTarget{"synthetic": {Path: target, FailureDomain: "synthetic-volume"}}
		f.s.Config.DefaultBackupTarget = "synthetic"
		var targets model.Page[service.BackupTargetInfo]
		decode(t, f.json("GET", "/backup-targets", "", nil, 200), &targets)
		if len(targets.Items) == 1 && targets.Items[0].DistinctVolume {
			return target
		}
	}
	t.Skip("runner has no second synthetic volume for the independent copy")
	return ""
}

func batchWithPendingSecondUpload(t *testing.T, f *fixture) (model.Batch, model.UploadResult, model.Upload) {
	t.Helper()
	var batch model.Batch
	decode(t, f.json("POST", "/batches", "snapshot-batch-selection", map[string]any{"libraryId": f.library, "mode": "ordinary", "label": "Synthetic pair"}, 201), &batch)
	ids := f.declareInputs(batch.ID, "first.png", "second.png")
	first, _ := f.upload("first.png", 51, map[string]any{"batchId": batch.ID, "inputId": ids["first.png"]})
	published := f.complete(first)
	second, _ := f.upload("second.png", 92, map[string]any{"batchId": batch.ID, "inputId": ids["second.png"]})
	sealed, err := f.s.Batch(context.Background(), batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	return sealed, published, second
}

func checkProtectionSnapshot(t *testing.T, name string, batch model.Batch, first, second model.UploadResult, wantSecond bool) {
	t.Helper()
	db, err := store.OpenDatabase(name, store.SnapshotDatabase)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count int
	if err = db.QueryRow(`SELECT count(*) FROM batch_inputs i JOIN batches b ON b.id=i.batch_id
 WHERE b.id=? AND json_extract(b.data,'$.manifestState')='sealed' AND json_extract(b.data,'$.manifestFingerprint')=?
 AND i.input_id IN (?,?)`, batch.ID, batch.ManifestFingerprint, first.Upload.InputID, second.Upload.InputID).Scan(&count); err != nil || count != 2 {
		t.Fatalf("snapshot lost the two sealed inputs: count=%d err=%v", count, err)
	}
	for _, result := range []model.UploadResult{first, second} {
		var included bool
		u, a := result.Upload, result.Asset
		err = db.QueryRow(`SELECT EXISTS(SELECT 1 FROM batch_inputs i JOIN uploads u ON u.id=i.upload_id AND u.batch_id=i.batch_id
 JOIN assets a ON a.id=json_extract(u.data,'$.assetId')
 JOIN asset_content_versions v ON v.asset_id=a.id AND v.version=json_extract(u.data,'$.publishedContentVersion')
 JOIN asset_authors r ON r.asset_id=a.id JOIN authors author ON author.id=r.author_id
 WHERE i.batch_id=? AND i.input_id=? AND u.id=? AND u.state='ingested' AND a.id=? AND v.version=? AND v.hash=? AND v.size=? AND author.id=?)`,
			batch.ID, u.InputID, u.ID, a.ID, a.ContentVersion, a.Hash, a.Size, a.AuthorID).Scan(&included)
		want := u.ID == first.Upload.ID || wantSecond
		if err != nil || included != want {
			t.Fatalf("snapshot input %s has published version and author=%t, want %t: %v", u.InputID, included, want, err)
		}
	}
}

const interruptedCheckpointKey = "synthetic-checkpoint-interruption"

func interruptCheckpointReceipt(t *testing.T, f *fixture, route string) {
	t.Helper()
	// The receipt insert is the last write before commit. The connection-local
	// trigger is absent from the snapshot and cannot affect a reopened checkpoint.
	_, err := f.s.DB.DB.Exec(`CREATE TEMP TRIGGER interrupt_receipt BEFORE INSERT ON idempotency
 WHEN NEW.key='` + interruptedCheckpointKey + `' BEGIN SELECT RAISE(ABORT,'synthetic interrupted receipt'); END`)
	if err != nil {
		t.Fatal(err)
	}
	f.json("POST", route, interruptedCheckpointKey, struct{}{}, 500)
	if _, err = f.s.DB.DB.Exec("DROP TRIGGER interrupt_receipt"); err != nil {
		t.Fatal(err)
	}
}

func TestProtectionRetryUsesPublishedSnapshotBeforeReceipt(t *testing.T) {
	f := setup(t)
	target := distinctProtectionTarget(t, f)
	batch, first, pending := batchWithPendingSecondUpload(t, f)
	base := "/batches/" + batch.ID + "/protections"
	interruptCheckpointReceipt(t, f, base)
	paths, err := filepath.Glob(filepath.Join(f.s.Config.DataDir, "backups", "*", "snapshot.sqlite"))
	if err != nil || len(paths) != 1 {
		t.Fatalf("expected one published checkpoint before commit, got %v: %v", paths, err)
	}
	frozen, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	var committed int
	err = f.s.DB.DB.QueryRow(`SELECT (SELECT count(*) FROM batch_protections)+(SELECT count(*) FROM backups)
 +(SELECT count(*) FROM idempotency WHERE key=?)`, interruptedCheckpointKey).Scan(&committed)
	if err != nil || committed != 0 {
		t.Fatalf("interrupted protection committed %d rows: %v", committed, err)
	}
	second := f.complete(pending)
	checkProtectionSnapshot(t, paths[0], batch, first, second, false)

	start := func(key string) service.BatchProtection {
		t.Helper()
		var queued service.BatchProtection
		decode(t, f.json("POST", base, key, struct{}{}, 202), &queued)
		return queued
	}
	old := start(interruptedCheckpointKey)
	old = awaitProtection(t, f, batch.ID, old.ID)
	if old.State != "completed" || old.CheckpointID != filepath.Base(filepath.Dir(paths[0])) || old.SnapshotHash != fmt.Sprintf("%x", sha256.Sum256(frozen)) ||
		len(old.Items) != 2 || old.Items[0].InputID != first.Upload.InputID || old.Items[0].State != "protected" ||
		old.Items[1].InputID != pending.InputID || old.Items[1].State != "retained" || old.Items[1].Reason != pending.State {
		t.Fatalf("retry relabelled the interrupted checkpoint: %+v", old)
	}
	snapshot := func(p service.BatchProtection) string {
		return filepath.Join(target, "instances", p.InstanceID, "snapshots", p.SnapshotHash+".sqlite")
	}
	checkProtectionSnapshot(t, snapshot(old), batch, first, second, false)
	if replay := start(interruptedCheckpointKey); replay.ID != old.ID || replay.SnapshotHash != old.SnapshotHash {
		t.Fatal("replayed protection changed its checkpoint")
	}
	body := func(r model.UploadResult) service.CleanupPermitInput {
		return service.CleanupPermitInput{InputID: r.Upload.InputID, ExpectedHash: r.Asset.Hash, SourceSize: r.Asset.Size}
	}
	permits := base + "/" + old.ID + "/cleanup-permits"
	var refusal service.Error
	decode(t, f.json("POST", permits, "old-second-permit", body(second), 409), &refusal)
	if refusal.Code != "input_not_protected" {
		t.Fatalf("uncovered source returned %s", refusal.Code)
	}
	f.json("POST", permits, "old-first-permit", body(first), 201)
	var permitsForSecond int
	if err = f.s.DB.DB.QueryRow("SELECT count(*) FROM source_cleanup_permits WHERE batch_id=? AND input_id=?", batch.ID, pending.InputID).Scan(&permitsForSecond); err != nil || permitsForSecond != 0 {
		t.Fatalf("old checkpoint issued %d permits for the later input: %v", permitsForSecond, err)
	}

	fresh := start("protection-after-second-publication")
	fresh = awaitProtection(t, f, batch.ID, fresh.ID)
	if fresh.State != "completed" || fresh.SnapshotHash == old.SnapshotHash || len(fresh.Items) != 2 || fresh.Items[1].State != "protected" {
		t.Fatalf("new checkpoint did not cover the later publication: %+v", fresh)
	}
	checkProtectionSnapshot(t, snapshot(fresh), batch, first, second, true)
	original, err := os.ReadFile(snapshot(old))
	if err != nil || !bytes.Equal(original, frozen) {
		t.Fatalf("new protection changed the original checkpoint: %v", err)
	}
	f.json("POST", base+"/"+fresh.ID+"/cleanup-permits", "fresh-second-permit", body(second), 201)
}

// A pre-upgrade report and its issued permits can survive independently of any
// currently mounted target. Exercise their persisted format on every platform.
func TestLegacyProtectionAndPermitReplayRequireSnapshotMembership(t *testing.T) {
	checkLegacyPermitMembership(t, false)
}

func TestLegacyPermitReplayUsesIndependentSnapshotMembership(t *testing.T) {
	checkLegacyPermitMembership(t, true)
}

func completedProtection(t *testing.T, f *fixture, batchID, key string) service.BatchProtection {
	t.Helper()
	var queued service.BatchProtection
	decode(t, f.json("POST", "/batches/"+batchID+"/protections", key, struct{}{}, 202), &queued)
	proof := awaitProtection(t, f, batchID, queued.ID)
	if proof.State != "completed" {
		t.Fatalf("independent protection did not complete: %s", proof.ErrorCode)
	}
	return proof
}

func checkLegacyPermitMembership(t *testing.T, useIndependentCopy bool) {
	t.Helper()
	f := setup(t)
	batch, first, pending := batchWithPendingSecondUpload(t, f)
	var independent service.BatchProtection
	var target string
	if useIndependentCopy {
		target = distinctProtectionTarget(t, f)
		independent = completedProtection(t, f, batch.ID, "historical-independent-snapshot")
	} else {
		interruptCheckpointReceipt(t, f, "/backups")
	}
	second := f.complete(pending)
	checkpoint := service.Backup{ID: independent.CheckpointID, SnapshotHash: independent.SnapshotHash}
	if !useIndependentCopy {
		decode(t, f.json("POST", "/backups", interruptedCheckpointKey, struct{}{}, 201), &checkpoint)
	}
	name := filepath.Join(f.s.Config.DataDir, "backups", checkpoint.ID, "snapshot.sqlite")
	checkProtectionSnapshot(t, name, batch, first, second, false)
	info, err := os.Stat(name)
	if err != nil {
		t.Fatal(err)
	}
	if useIndependentCopy {
		checkProtectionSnapshot(t, filepath.Join(target, "instances", independent.InstanceID, "snapshots", independent.SnapshotHash+".sqlite"), batch, first, second, false)
		if err = os.Rename(name, name+".held"); err != nil {
			t.Fatal(err)
		}
		// A later independent backup contains both originals. Its SQLite file
		// and the shared object for the second upload cannot prove membership.
		completedProtection(t, f, batch.ID, "both-originals-now-independent")
	}
	ctx := context.Background()
	owner, err := f.s.Authenticate(ctx, testSecret)
	if err != nil {
		t.Fatal(err)
	}
	proof := service.BatchProtection{
		ID: "synthetic-old-proof", InstanceID: f.s.InstanceID, BatchID: batch.ID, ManifestFingerprint: batch.ManifestFingerprint,
		CheckpointID: checkpoint.ID, SnapshotHash: checkpoint.SnapshotHash, SnapshotBytes: info.Size(), State: "completed",
		TargetID: independent.TargetID, FailureDomain: independent.FailureDomain,
		ProofHash: fmt.Sprintf("%x", sha256.Sum256([]byte("synthetic legacy receipt"))), CreatedAt: time.Now().UTC(),
	}
	for ordinal, result := range []model.UploadResult{first, second} {
		proof.Items = append(proof.Items, service.ProtectedInput{InputID: result.Upload.InputID, Ordinal: ordinal, State: "protected",
			UploadID: result.Upload.ID, AssetID: result.Asset.ID, ContentVersion: result.Asset.ContentVersion, Hash: result.Asset.Hash, Size: result.Asset.Size})
	}
	raw, err := store.Encode(proof)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.DB.DB.Exec("INSERT INTO batch_protections(id,batch_id,state,created_at,data) VALUES(?,?,?,?,?)", proof.ID, batch.ID, proof.State, proof.CreatedAt, raw); err != nil {
		t.Fatal(err)
	}
	url := "/batches/" + batch.ID + "/protections/" + proof.ID + "/cleanup-permits"
	for _, item := range proof.Items {
		body := service.CleanupPermitInput{InputID: item.InputID, ExpectedHash: item.Hash, SourceSize: item.Size}
		request, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		key := "old-permit-" + item.InputID
		var permit service.CleanupPermit
		_, err = f.s.Run(ctx, owner, "POST /api/v1"+url, key, request, func(w *service.Write) (any, int, error) {
			permit = service.CleanupPermit{ID: w.ID("source-cleanup"), InstanceID: proof.InstanceID, ProtectionID: proof.ID, BatchID: batch.ID, InputID: item.InputID,
				AssetID: item.AssetID, ContentVersion: item.ContentVersion, Hash: item.Hash, Size: item.Size, ProofHash: proof.ProofHash,
				CredentialID: owner.ID, ExpiresAt: time.Now().UTC().Add(20 * time.Minute), State: "issued"}
			raw, err := store.Encode(permit)
			if err != nil {
				return nil, 0, err
			}
			q, err := w.Q()
			if err != nil {
				return nil, 0, err
			}
			_, err = q.ExecContext(ctx, `INSERT INTO source_cleanup_permits(id,protection_id,batch_id,input_id,principal_id,state,expires_at,data)
 VALUES(?,?,?,?,?,?,?,?)`, permit.ID, proof.ID, batch.ID, item.InputID, owner.ID, permit.State, permit.ExpiresAt, raw)
			return permit, 201, err
		})
		if err != nil {
			t.Fatal(err)
		}
		want := "issued"
		if item.InputID == second.Upload.InputID {
			want = "revoked"
			var rejection service.Error
			decode(t, f.json("POST", url, "later-request-for-old-proof", body, 409), &rejection)
			if rejection.Code != "input_version_unverified" {
				t.Fatalf("old proof missed the snapshot membership check: %+v", rejection)
			}
		}
		var replay, detail service.CleanupPermit
		var page model.Page[service.CleanupPermit]
		decode(t, f.json("POST", url, key, body, 201), &replay)
		decode(t, f.json("GET", "/cleanup-permits/"+permit.ID, "", nil, 200), &detail)
		decode(t, f.json("GET", "/batches/"+batch.ID+"/cleanup-permits?inputId="+item.InputID, "", nil, 200), &page)
		if replay.ID != permit.ID || replay.State != want || detail.State != want || len(page.Items) != 1 || page.Items[0].State != want {
			t.Fatalf("legacy permit %s should read %s on all three routes: replay=%s detail=%s page=%+v", item.InputID, want, replay.State, detail.State, page.Items)
		}
		if want == "revoked" {
			var closed service.CleanupPermit
			decode(t, f.json("POST", "/cleanup-permits/"+permit.ID+"/receipt", "legacy-retained-outcome", service.CleanupReceiptInput{Outcome: "retained"}, 200), &closed)
			if closed.State != "recorded" || closed.Receipt == nil || closed.Receipt.Outcome != "retained" {
				t.Fatal("old permit no longer accepts a report of the actual source outcome")
			}
		}
	}
}
