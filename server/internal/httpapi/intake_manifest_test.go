package httpapi_test

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/service"
)

func TestBatchCompletionReconcilesOriginalSelectionIncludingMissingUploads(t *testing.T) {
	f := setup(t)
	var batch model.Batch
	decode(t, f.json("POST", "/batches", "manifest-batch-create", map[string]any{"libraryId": f.library, "mode": "ordinary", "label": "Two original sources"}, 201), &batch)
	source := pngBytes(t, 20)
	f.json("POST", "/uploads", "manifest-too-early", map[string]any{"libraryId": f.library, "mode": "ordinary", "batchId": batch.ID, "fileName": "one.png", "size": len(source), "mimeType": "image/png"}, 409)
	ids := f.declareInputs(batch.ID, "one.png", "two.png")
	first, _ := f.upload("one.png", 20, map[string]any{"batchId": batch.ID, "inputId": ids["one.png"]})
	f.complete(first)
	var report model.BatchDetail
	decode(t, f.json("GET", "/batches/"+batch.ID, "", nil, 200), &report)
	if report.Batch.State == "completed" || report.Batch.ExpectedInputs != 2 || report.Batch.ManifestState != "sealed" || len(report.Inputs) != 2 || len(report.Items) != 1 || report.Inputs[0].State != "ingested" || report.Inputs[1].State != "pending" || report.Inputs[1].UploadID != "" {
		t.Fatalf("received uploads hid missing input: %+v", report)
	}
	var page struct {
		Inputs     []model.BatchInput `json:"inputs"`
		Items      []model.BatchItem  `json:"items"`
		NextCursor string             `json:"nextCursor"`
	}
	decode(t, f.json("GET", "/ai/tasks/"+batch.ID+"/context?limit=1", "", nil, 200), &page)
	if len(page.Inputs) != 1 || page.Inputs[0].ID != ids["one.png"] || page.NextCursor == "" {
		t.Fatalf("manifest first page: %+v", page)
	}
	cursor := page.NextCursor
	page.Inputs = nil
	page.Items = nil
	page.NextCursor = ""
	decode(t, f.json("GET", "/ai/tasks/"+batch.ID+"/context?limit=1&cursor="+url.QueryEscape(cursor), "", nil, 200), &page)
	if len(page.Inputs) != 1 || page.Inputs[0].ID != ids["two.png"] || page.Inputs[0].State != "pending" || len(page.Items) != 0 || page.NextCursor != "" {
		t.Fatalf("unreceived input was omitted from context: %+v", page)
	}
	f.json("POST", "/uploads", "manifest-double-owner", map[string]any{"libraryId": f.library, "mode": "ordinary", "batchId": batch.ID, "inputId": ids["one.png"], "fileName": "one.png", "size": len(source), "mimeType": "image/png"}, 409)
	f.json("POST", "/uploads", "manifest-changed-source", map[string]any{"libraryId": f.library, "mode": "ordinary", "batchId": batch.ID, "inputId": ids["two.png"], "fileName": "other.png", "size": len(source), "mimeType": "image/png"}, 409)
	f.json("PUT", "/batches/"+batch.ID+"/inputs", "manifest-cannot-shrink", map[string]any{"expectedManifestRevision": report.Batch.ManifestRevision, "inputs": []map[string]any{{"id": ids["one.png"], "originalFileName": "one.png"}}}, 409)
	second, _ := f.upload("two.png", 45, map[string]any{"batchId": batch.ID, "inputId": ids["two.png"]})
	f.complete(second)
	decode(t, f.json("GET", "/batches/"+batch.ID, "", nil, 200), &report)
	if report.Batch.State != "completed" || len(report.Inputs) != 2 || report.Inputs[1].State != "ingested" || report.Inputs[0].AssetID == report.Inputs[1].AssetID {
		t.Fatalf("complete manifest was not reconciled: %+v", report)
	}
}

func TestImplicitOrdinaryUploadHasSealedSingleInput(t *testing.T) {
	f := setup(t)
	u, _ := f.upload("implicit.png", 51, nil)
	f.complete(u)
	var report model.BatchDetail
	decode(t, f.json("GET", "/batches/"+u.BatchID, "", nil, 200), &report)
	if report.Batch.ManifestState != "sealed" || report.Batch.ExpectedInputs != 1 || len(report.Inputs) != 1 || report.Inputs[0].UploadID != u.ID || u.InputID == "" || report.Inputs[0].ID != u.InputID {
		t.Fatalf("implicit upload omitted original intent: %+v", report)
	}
}

func TestCancellingAnUnreceivedInputKeepsTheOriginalManifest(t *testing.T) {
	f := setup(t)
	var batch model.Batch
	decode(t, f.json("POST", "/batches", "cancel-manifest-batch", map[string]any{"libraryId": f.library, "mode": "ordinary", "label": "Cancel selected input"}, 201), &batch)
	ids := f.declareInputs(batch.ID, "not-started.png", "staged.png")
	var detail model.BatchDetail
	decode(t, f.json("GET", "/batches/"+batch.ID, "", nil, 200), &detail)
	cancelPath := "/batches/" + batch.ID + "/inputs/" + ids["not-started.png"] + "/cancel"
	f.json("POST", cancelPath, "cancel-wrong-selection", map[string]string{"manifestFingerprint": "wrong"}, 409)
	body := map[string]string{"manifestFingerprint": detail.Batch.ManifestFingerprint}
	var cancelled model.BatchInput
	decode(t, f.json("POST", cancelPath, "cancel-before-upload", body, 200), &cancelled)
	if cancelled.State != "cancelled" || cancelled.UploadID != "" {
		t.Fatalf("cancel fabricated an upload: %+v", cancelled)
	}
	f.json("POST", cancelPath, "cancel-before-upload", body, 200)
	f.json("POST", "/uploads", "upload-cancelled-input", map[string]any{"libraryId": f.library, "mode": "ordinary", "batchId": batch.ID, "inputId": ids["not-started.png"], "fileName": "not-started.png", "size": 20, "mimeType": "image/png"}, 409)
	upload, _ := f.upload("staged.png", 55, map[string]any{"batchId": batch.ID, "inputId": ids["staged.png"]})
	f.json("POST", "/batches/"+batch.ID+"/inputs/"+ids["staged.png"]+"/cancel", "cancel-received-input", body, 200)
	var stopped model.Upload
	decode(t, f.json("GET", "/uploads/"+upload.ID, "", nil, 200), &stopped)
	if stopped.State != "cancelled" {
		t.Fatal("input cancellation did not cancel its bound upload")
	}
	decode(t, f.json("GET", "/batches/"+batch.ID, "", nil, 200), &detail)
	if detail.Batch.State != "partial" || detail.Batch.ExpectedInputs != 2 || len(detail.Inputs) != 2 || detail.Inputs[0].State != "cancelled" || detail.Inputs[1].State != "cancelled" {
		t.Fatalf("cancellation hid original selection: %+v", detail)
	}
}

func TestUnavailablePickerNameIsNotReplacedByInventedOriginalName(t *testing.T) {
	f := setup(t)
	var batch model.Batch
	decode(t, f.json("POST", "/batches", "unknown-name-batch", map[string]any{"libraryId": f.library, "mode": "ordinary", "label": "Unavailable metadata"}, 201), &batch)
	f.json("PUT", "/batches/"+batch.ID+"/inputs", "unknown-name-seal", map[string]any{"expectedManifestRevision": 1, "inputs": []map[string]any{{"id": "unknown-name-item", "nameState": "unavailable"}}}, 200)
	u, _ := f.upload("chosen-target.png", 71, map[string]any{"batchId": batch.ID, "inputId": "unknown-name-item", "mimeType": "application/octet-stream"})
	result := f.complete(u)
	if result.Asset == nil || result.Asset.OriginalFileName != "" || result.Asset.MimeType != "image/png" {
		t.Fatal("target name was presented as a known source name or MIME was not normalized")
	}
	var report model.BatchDetail
	decode(t, f.json("GET", "/batches/"+batch.ID, "", nil, 200), &report)
	if report.Inputs[0].OriginalFileName != "" || report.Inputs[0].NameState != "unavailable" {
		t.Fatal("the original unknown metadata was rewritten after upload")
	}
}

func TestRejectedSelectionKeepsItsReasonAndOnlyUnfinishedMediaCanBeClaimed(t *testing.T) {
	f := setup(t)
	policy := service.DelegationPolicy{Label: "Mixed selection", LibraryIDs: []string{f.library}, AuthorIDs: []string{},
		AllowedFields: []string{}, CandidateBlocks: []string{}, CanIngest: true, CanResearch: true, CanOrganize: true,
		MaxResearchCalls: 2, MaxUpdates: 1, MaxTasksPerDay: 2, ValidDays: 1}
	var grant service.DelegationCreated
	decode(t, f.json("POST", "/ai/delegations", "", policy, 201), &grant)
	for _, withMedia := range []bool{false, true} {
		label := "closed"
		inputs := []map[string]any{{"id": "notes-input", "originalFileName": "notes.txt", "size": 3}}
		if withMedia {
			label = "actionable"
			inputs = append(inputs, map[string]any{"id": "image-input", "originalFileName": "picture.png"})
		}
		var b model.Batch
		decode(t, f.json("POST", "/batches", "create-mixed-"+label, map[string]any{"libraryId": f.library, "mode": "intelligent",
			"label": label, "maxResearchCalls": 2}, 201), &b)
		uri := "/batches/" + b.ID
		decode(t, f.json("PUT", uri+"/inputs", "seal-mixed-"+label, map[string]any{"expectedManifestRevision": 1, "inputs": inputs}, 200), &b)
		if b.State != "partial" || b.ExpectedInputs != len(inputs) {
			t.Fatal("rejected source was missing from the sealed selection", b)
		}
		spoof := map[string]any{"libraryId": f.library, "mode": "intelligent", "batchId": b.ID, "inputId": "notes-input",
			"originalFileName": "notes.txt", "fileName": "renamed.png", "mimeType": "image/png", "size": 3}
		var rejected service.Error
		decode(t, f.json("POST", "/uploads", "rename-rejected-"+label, spoof, 409), &rejected)
		if rejected.Code != "unsupported_media" {
			t.Fatalf("renamed target bypassed the sealed disposition: %v", rejected)
		}
		f.json("POST", uri+"/inputs/notes-input/cancel", "cancel-rejected-"+label, map[string]string{"manifestFingerprint": b.ManifestFingerprint}, 409)
		body, err := json.Marshal(map[string]any{"kind": "intake", "mode": "intelligent", "libraryId": f.library,
			"batchId": b.ID, "label": label, "credentialSecret": base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("x", 32)))})
		if err != nil {
			t.Fatal(err)
		}
		status := 409
		if withMedia {
			status = 201
		}
		claim := f.request("POST", "/ai/delegated-tasks", "claim-mixed-"+label, grant.Credential.Token, body, nil)
		if claim.Code != status {
			t.Fatalf("claiming %s mixed selection: got %d, want %d: %s", label, claim.Code, status, claim.Body.String())
		}
		var report model.BatchDetail
		decode(t, f.json("GET", uri, "", nil, 200), &report)
		f.json("POST", uri+"/cancel", "cancel-mixed-"+label, map[string]int64{"expectedRevision": report.Batch.Revision}, 200)
		decode(t, f.json("GET", uri, "", nil, 200), &report)
		first := report.Inputs[0]
		if first.State != "failed" || first.ErrorCode != "unsupported_media" || first.UploadID != "" || first.AssetID != "" {
			t.Fatalf("batch cancellation replaced the original rejection: %+v", first)
		}
		if withMedia && report.Inputs[1].State != "cancelled" {
			t.Fatal("remaining media input did not honour explicit cancellation")
		}
	}
}
