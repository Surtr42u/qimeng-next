package httpapi_test

import (
	"bytes"
	"strconv"
	"testing"

	"qimeng.local/next/server/internal/model"
)

func TestPlanUsesItsOwnDirectoryAndRefusesAnOccupiedTargetWithoutRepeatingCompletedItems(t *testing.T) {
	f := setup(t)
	var batch model.Batch
	decode(t, f.json("POST", "/batches", "scope-test-batch", map[string]any{"libraryId": f.library, "mode": "intelligent", "label": "Directory scope"}, 201), &batch)
	inputs := f.declareInputs(batch.ID, "one.png", "two.png")
	items := []map[string]any{}
	for i, name := range []string{"one.png", "two.png"} {
		u, _ := f.upload(name, uint8(92+i), map[string]any{"batchId": batch.ID, "inputId": inputs[name], "mode": "intelligent", "relativeDirectory": "A"})
		f.complete(u)
		items = append(items, map[string]any{"uploadId": u.ID, "family": "作品"})
	}
	var state model.BatchDetail
	decode(t, f.json("GET", "/batches/"+batch.ID, "", nil, 200), &state)
	var p model.Plan
	plans := "/batches/" + batch.ID + "/plans"
	decode(t, f.json("POST", plans, "prepare-directory-scope", map[string]any{"expectedRevision": state.Batch.Revision, "items": items}, 201), &p)
	if len(p.Items) != 2 || p.Items[0].NamespaceFingerprint == "" {
		t.Fatal("the new plan did not bind its namespace")
	}
	b, _ := f.upload(p.Items[0].FileName, 94, map[string]any{"relativeDirectory": "B"})
	f.complete(b)
	rival, original := f.upload(p.Items[1].FileName, 95, map[string]any{"relativeDirectory": "A"})
	occupied := *f.complete(rival).Asset
	commit := func() model.BatchDetail {
		var d model.BatchDetail
		decode(t, f.json("POST", plans+"/"+p.ID+"/commit", "submit-scope-once", map[string]any{"fingerprint": p.Fingerprint}, 200), &d)
		return d
	}
	first := commit()
	// A later occupied number does not change the earlier allocation.
	if first.Batch.State != "partial" || first.Items[1].Upload.ErrorCode != "path_occupied" {
		t.Fatal("target collision was not recorded for the affected input", first.Batch.State)
	}
	content := f.request("GET", "/assets/"+occupied.ID+"/content", "", testSecret, nil, nil)
	if content.Code != 200 || !bytes.Equal(content.Body.Bytes(), original) {
		t.Fatal("the conflicting client's original was overwritten")
	}
	firstID := first.Items[0].Upload.AssetID
	f.json("DELETE", "/assets/"+occupied.ID+"?expectedRevision="+strconv.FormatInt(occupied.Revision, 10), "remove-rival-before-retry", struct{}{}, 200)
	resumed := commit()
	again := commit()
	if resumed.Batch.State != "completed" || firstID == "" || resumed.Items[0].Upload.AssetID != firstID ||
		again.Items[0].Upload.AssetID != firstID || again.Items[1].Upload.AssetID != resumed.Items[1].Upload.AssetID {
		t.Fatal("a retry published an already completed input again")
	}
}
