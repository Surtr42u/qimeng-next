package service_test

import (
	"encoding/base64"
	"reflect"
	"strings"
	"testing"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/service"
)

func TestClaimedIntakeStatusIsVisibleAcrossSessionsAndCannotCommitOrOverrideNewerWork(t *testing.T) {
	dir := t.TempDir()
	f, stop := openQueueFixture(t, dir)
	var library model.Library
	f.request("POST", "/libraries", researchTestOwner, "observed-library", map[string]string{"name": "Executor fixture", "kind": "regular"}).expect(t, 201, &library)
	first := model.IntakeInput{ID: "first-mixed-input", OriginalFileName: "first.png"}
	second := model.IntakeInput{ID: "second-mixed-input", OriginalFileName: "second.png"}
	b := queuedBatch(t, f, "observer-fixture", library.ID, "intelligent", first, second)
	read := func(token string) model.BatchDetail {
		t.Helper()
		var result model.BatchDetail
		f.request("GET", "/batches/"+b.ID, token, "", nil).expect(t, 200, &result)
		return result
	}
	initial := read(researchTestOwner)
	if initial.ExecutorStatus == nil || initial.ExecutorStatus.State != "not_configured" || initial.ExecutorStatus.ReportedAt != nil {
		t.Fatal("unconfigured deployment was presented as a live host", initial.ExecutorStatus)
	}
	grant := queueGrant(t, f, library.ID, true)
	if now := read(researchTestOwner).ExecutorStatus; now.State != "not_ready" || now.Source != "batch" {
		t.Fatal("unreceived selection was offered for claim", now)
	}
	upload := f.stage(t, library.ID, b.ID, first.ID, first.OriginalFileName, "", 61)
	f.stage(t, library.ID, b.ID, second.ID, second.OriginalFileName, "", 62)
	if now := read(researchTestOwner).ExecutorStatus; now.State != "awaiting_claim" || now.Source != "queue" || now.ReportedAt != nil {
		t.Fatal("ready selection should wait for a claim without an invented observation time", now)
	}
	oldToken := base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("a", 32)))
	claim := map[string]string{"kind": "intake", "label": "Real worker", "libraryId": library.ID, "batchId": b.ID,
		"mode": "intelligent", "readyManifestFingerprint": b.ManifestFingerprint, "credentialSecret": oldToken}
	f.request("POST", "/ai/delegated-tasks", grant.Credential.Token, "claim-observed-intake", claim).expect(t, 201, nil)
	var observer model.Principal
	f.request("POST", "/credentials", researchTestOwner, "", map[string]any{"label": "Other reader", "batchId": b.ID, "scopes": []string{"read"}}).expect(t, 201, &observer)
	before := read(observer.Token)
	if before.ExecutorStatus.State != "claimed" || before.ExecutorStatus.Source != "lease" || before.ExecutorStatus.Generation != 1 {
		t.Fatal("missing claim projection", before.ExecutorStatus)
	}
	rowCount := func(query string) int64 {
		t.Helper()
		var n int64
		if err := f.s.DB.DB.QueryRow(query).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	checkpoint := rowCount("SELECT revision FROM backup_business_state WHERE id=1")
	reportPath := "/ai/delegated-tasks/" + b.ID + "/intake-status"
	report := service.BatchExecutorReport{Generation: 1, BatchRevision: before.Batch.Revision, State: "paused", Reason: "host_unavailable"}
	for _, token := range []string{researchTestOwner, observer.Token, grant.Credential.Token} {
		f.request("PUT", reportPath, token, "report-without-child", report).expect(t, 403, nil)
	}
	f.request("PUT", "/ai/delegated-tasks/other-object/intake-status", oldToken, "report-wrong-object", report).expect(t, 403, nil)
	falseCompletion := report
	falseCompletion.State, falseCompletion.Reason = "completed", ""
	f.request("PUT", reportPath, oldToken, "cannot-report-completion", falseCompletion).expect(t, 400, nil)
	freeText := report
	freeText.Reason = "arbitrary model message"
	f.request("PUT", reportPath, oldToken, "cannot-store-model-text", freeText).expect(t, 400, nil)
	var pause, replay model.BatchExecutorStatus
	f.request("PUT", reportPath, oldToken, "observed-first-pause", report).expect(t, 200, &pause)
	f.request("PUT", reportPath, oldToken, "observed-first-pause", report).expect(t, 200, &replay)
	if pause.ReportedAt == nil || pause.Source != "worker" || pause.Revision != 1 || !reflect.DeepEqual(pause, replay) {
		t.Fatal("same operation did not preserve the original observation", pause, replay)
	}
	repeated := report
	repeated.ExpectedStatusRevision = pause.Revision
	f.request("PUT", reportPath, oldToken, "redundant-observation", repeated).expect(t, 409, nil)
	changed := report
	changed.Reason = "permission_required"
	f.request("PUT", reportPath, oldToken, "observed-first-pause", changed).expect(t, 409, nil)
	if rowCount("SELECT count(*) FROM intake_executor_observations") != 1 ||
		rowCount("SELECT count(*) FROM idempotency WHERE key IN ('observed-first-pause','redundant-observation')") != 1 ||
		rowCount("SELECT revision FROM backup_business_state WHERE id=1") != checkpoint || rowCount("SELECT count(*) FROM assets") != 0 {
		t.Fatal("reporting changed a checkpoint, created an asset, or amplified a repeated observation")
	}
	stop()
	f, _ = openQueueFixture(t, dir)
	after := read(observer.Token)
	if !reflect.DeepEqual(after.ExecutorStatus, &pause) || !reflect.DeepEqual(before.Batch, after.Batch) || !reflect.DeepEqual(before.Inputs, after.Inputs) {
		t.Fatal("another session lost the pause or saw rewritten business state after restart")
	}
	newToken := base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("b", 32)))
	f.request("POST", "/ai/delegated-tasks/"+b.ID+"/resume", grant.Credential.Token, "rotate-observed-worker", map[string]string{"credentialSecret": newToken}).expect(t, 200, nil)
	resumed := read(observer.Token).ExecutorStatus
	if resumed.State != "claimed" || resumed.Source != "lease" || resumed.Generation != 2 || resumed.Revision != 1 || resumed.ReportedAt != nil {
		t.Fatal("old host outage was attributed to the new lease", resumed)
	}
	f.request("PUT", reportPath, oldToken, "expired-worker-report", repeated).expect(t, 401, nil)
	f.request("PUT", reportPath, newToken, "late-worker-generation", repeated).expect(t, 409, nil)
	report.Generation, report.Reason, report.ExpectedStatusRevision = 2, "budget_exhausted", resumed.Revision
	f.request("PUT", reportPath, newToken, "current-worker-budget", report).expect(t, 200, &pause)
	report.ExpectedStatusRevision, report.State, report.Reason = pause.Revision, "claimed", ""
	var recovered model.BatchExecutorStatus
	f.request("PUT", reportPath, newToken, "worker-recovered", report).expect(t, 200, &recovered)
	if recovered.Source != "worker" || recovered.State != "claimed" || recovered.Revision != pause.Revision+1 || recovered.ReportedAt == nil || recovered.Reason != "" {
		t.Fatal("worker recovery did not supersede the pause", recovered)
	}
	f.request("PUT", reportPath, newToken, "late-same-generation", service.BatchExecutorReport{Generation: 2, BatchRevision: before.Batch.Revision,
		ExpectedStatusRevision: pause.Revision, State: "paused", Reason: "host_unavailable"}).expect(t, 409, nil)
	report.ExpectedStatusRevision, report.State, report.Reason = recovered.Revision, "paused", "needs_attention"
	f.request("PUT", reportPath, newToken, "pending-owner-decision", report).expect(t, 200, &pause)
	var plan model.Plan
	f.request("POST", "/batches/"+b.ID+"/plans", researchTestOwner, "owner-plan-after-pause", model.PlanCreate{ExpectedRevision: before.Batch.Revision,
		Items: []model.PlanInput{{UploadID: upload.ID, Family: "Reviewed"}}}).expect(t, 201, &plan)
	f.request("POST", "/batches/"+b.ID+"/plans/"+plan.ID+"/commit", researchTestOwner, "owner-commit-after-pause",
		map[string]string{"fingerprint": plan.Fingerprint}).expect(t, 200, nil)
	progress := read(observer.Token)
	if progress.Batch.Revision <= before.Batch.Revision || progress.ExecutorStatus == nil || progress.ExecutorStatus.State != "claimed" ||
		progress.ExecutorStatus.Reason != "" || progress.ExecutorStatus.Source != "lease" || progress.ExecutorStatus.Revision != pause.Revision {
		t.Fatal("stale observation obscured owner progress", progress.ExecutorStatus)
	}
	report.ExpectedStatusRevision = pause.Revision
	f.request("PUT", reportPath, newToken, "late-old-batch-revision", report).expect(t, 409, nil)
	f.request("POST", "/batches/"+b.ID+"/inputs/"+second.ID+"/cancel", researchTestOwner, "settle-remaining-selection",
		map[string]string{"manifestFingerprint": b.ManifestFingerprint}).expect(t, 200, nil)
	if settled := read(observer.Token); settled.Batch.State != "partial" || settled.ExecutorStatus != nil || rowCount("SELECT count(*) FROM assets") != 1 {
		t.Fatal("settled partial intake still advertised active executor work")
	}
}
