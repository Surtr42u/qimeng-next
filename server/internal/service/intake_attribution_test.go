package service_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"slices"
	"testing"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/service"
)

func TestUnknownResearchCannotPromoteAnUnselectedAuthor(t *testing.T) {
	f, _ := openQueueFixture(t, t.TempDir())
	var lib model.Library
	var author model.Author
	f.request("POST", "/libraries", researchTestOwner, "unresolved-library", map[string]string{"name": "Attribution fixture", "kind": "regular"}).expect(t, 201, &lib)
	f.request("POST", "/authors", researchTestOwner, "unresolved-author", map[string]string{"name": "Synthetic credited artist"}).expect(t, 201, &author)
	policy := service.DelegationPolicy{Label: "Fixture media intake", LibraryIDs: []string{lib.ID}, AuthorIDs: []string{}, AllowedFields: []string{}, CandidateBlocks: []string{}, CanIngest: true, CanResearch: true, CanOrganize: true, MaxResearchCalls: 5, MaxUpdates: 1, MaxTasksPerDay: 2, ValidDays: 1}
	var grant service.DelegationCreated
	f.request("POST", "/ai/delegations", researchTestOwner, "", policy).expect(t, 201, &grant)
	token := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{74}, 32))
	var task service.DelegatedTask
	f.request("POST", "/ai/delegated-tasks", grant.Credential.Token, "start-attribution-intake", map[string]string{"kind": "intake", "libraryId": lib.ID, "mode": "intelligent", "label": "Synthetic selection", "credentialSecret": token}).expect(t, 201, &task)
	route := "/batches/" + task.TaskID
	inputs := []model.IntakeInput{{ID: "unidentified-origin", OriginalFileName: "original-clue.png"}, {ID: "chosen-by-owner", OriginalFileName: "chosen.png"}, {ID: "later-evidence", OriginalFileName: "another-clue.png"}}
	f.request("PUT", route+"/inputs", researchTestOwner, "unresolved-manifest", model.BatchInputsSet{ExpectedManifestRevision: 1, Inputs: inputs}).expect(t, 200, nil)
	unknown := f.stage(t, lib.ID, task.TaskID, inputs[0].ID, inputs[0].OriginalFileName, "", 31)
	human := f.stage(t, lib.ID, task.TaskID, inputs[1].ID, inputs[1].OriginalFileName, author.ID, 93)
	later := f.stage(t, lib.ID, task.TaskID, inputs[2].ID, inputs[2].OriginalFileName, "", 155)
	var detail model.BatchDetail
	current := func() int64 {
		f.request("GET", route, token, "", nil).expect(t, 200, &detail)
		return detail.Batch.Revision
	}
	report := func(key string, u model.Upload, state string, evidence []model.ResearchCandidate) {
		result := model.Research{ExpectedRevision: current(), UploadID: u.ID, ExpectedHash: u.Hash, Outcome: state, Candidates: evidence, CallsUsed: 1}
		f.request("POST", route+"/research", token, key, result).expect(t, 200, nil)
	}
	report("unknown-first-source", unknown, "unknown", []model.ResearchCandidate{})
	report("unknown-second-check", human, "unknown", []model.ResearchCandidate{})
	report("unknown-before-evidence", later, "unknown", []model.ResearchCandidate{})
	prepare := func(key string, entries []model.PlanInput) apiReply {
		return f.request("POST", route+"/plans", token, key, model.PlanCreate{ExpectedRevision: current(), Items: entries})
	}
	unsafe := prepare("unproven-author-override", []model.PlanInput{{UploadID: unknown.ID, Family: "Unidentified", AuthorIDs: []string{author.ID}}})
	if unsafe.status != 409 || !bytes.Contains(unsafe.raw, []byte("research_author_unverified")) {
		t.Fatalf("unknown input accepted an unsupported real author or returned the wrong conflict (HTTP %d)", unsafe.status)
	}
	// The same batch may keep a real author selected on upload and adopt a later
	// independently sourced candidate for a different, verified input.
	report("evidenced-second-source", later, "candidates", []model.ResearchCandidate{{AuthorID: author.ID, Name: author.Name, SourceURL: "https://example.org/synthetic-work", Evidence: "The fixture credit identifies the author of this original."}})
	var unrelated model.Author
	f.request("POST", "/authors", researchTestOwner, "unrelated-intake-author", map[string]string{"name": "Unrelated fixture author"}).expect(t, 201, &unrelated)
	prepare("cannot-substitute-another-identity", []model.PlanInput{{UploadID: later.ID, Family: "With evidence", AuthorIDs: []string{unrelated.ID}}}).expect(t, 409, nil)
	var plan model.Plan
	prepare("publish-supported-choices", []model.PlanInput{
		{UploadID: unknown.ID, Family: "Original clue"}, {UploadID: human.ID, Family: "Selected"}, {UploadID: later.ID, Family: "With evidence", AuthorIDs: []string{author.ID}},
	}).expect(t, 201, &plan)
	f.request("POST", route+"/plans/"+plan.ID+"/commit", token, "publish-verified-attributions", map[string]string{"fingerprint": plan.Fingerprint}).expect(t, 200, &detail)
	if len(detail.Items) != 3 || detail.Batch.State != "completed" {
		t.Fatal("unsupported assignment prevented publication of the valid inputs")
	}
	for _, item := range detail.Items {
		var asset model.Asset
		f.request("GET", "/assets/"+item.Upload.AssetID, researchTestOwner, "", nil).expect(t, 200, &asset)
		if item.Upload.ID != unknown.ID {
			if asset.AuthorStatus != "confirmed" || !slices.Equal(asset.AuthorIDs, []string{author.ID}) {
				t.Fatal("lost the original selection or the new identity-bound evidence")
			}
			continue
		}
		if asset.OriginalFileName != inputs[0].OriginalFileName || asset.Hash != unknown.Hash || !slices.Equal(asset.AuthorIDs, []string{"unknown"}) || asset.AuthorStatus != "unknown" {
			t.Fatal("unidentified asset lost its input identity, original name or unknown attribution")
		}
		f.request("PATCH", "/assets/"+asset.ID, researchTestOwner, "", map[string]any{"expectedRevision": asset.Revision, "expectedAuthorsRevision": asset.AuthorsRevision, "authorIds": []string{author.ID}}).expect(t, 200, &asset)
		if asset.AuthorStatus != "confirmed" || !slices.Equal(asset.AuthorIDs, []string{author.ID}) || asset.OriginalFileName != inputs[0].OriginalFileName {
			t.Fatal("the owner's later explicit confirmation did not replace the placeholder")
		}
	}
}

func TestOlderPreparedAttributionIsRecheckedPerItemOnPublication(t *testing.T) {
	f, _ := openQueueFixture(t, t.TempDir())
	var library model.Library
	var author model.Author
	f.request("POST", "/libraries", researchTestOwner, "legacy-attribution-library", map[string]string{"name": "Historical plan fixture", "kind": "regular"}).expect(t, 201, &library)
	f.request("POST", "/authors", researchTestOwner, "legacy-attribution-author", map[string]string{"name": "Unverified fixture author"}).expect(t, 201, &author)
	grant := queueGrant(t, f, library.ID, true)
	token := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{80}, 32))
	var task service.DelegatedTask
	f.request("POST", "/ai/delegated-tasks", grant.Credential.Token, "legacy-attribution-task", map[string]string{"kind": "intake", "mode": "intelligent", "libraryId": library.ID, "label": "Historical intake", "credentialSecret": token}).expect(t, 201, &task)
	route := "/batches/" + task.TaskID
	inputs := []model.IntakeInput{{ID: "historical-first", OriginalFileName: "first.png"}, {ID: "historical-other", OriginalFileName: "other.png"}}
	f.request("PUT", route+"/inputs", researchTestOwner, "historical-attribution-inputs", model.BatchInputsSet{ExpectedManifestRevision: 1, Inputs: inputs}).expect(t, 200, nil)
	a := f.stage(t, library.ID, task.TaskID, inputs[0].ID, inputs[0].OriginalFileName, "", 91)
	b := f.stage(t, library.ID, task.TaskID, inputs[1].ID, inputs[1].OriginalFileName, "", 117)
	var detail model.BatchDetail
	f.request("GET", route, token, "", nil).expect(t, 200, &detail)
	var plan model.Plan
	f.request("POST", route+"/plans", token, "historical-prepare", model.PlanCreate{ExpectedRevision: detail.Batch.Revision, Items: []model.PlanInput{
		{UploadID: a.ID, Family: "First", AuthorIDs: []string{author.ID}}, {UploadID: b.ID, Family: "Other"},
	}}).expect(t, 201, &plan)
	// An older Next could sign this exact plan while the original still had a
	// durable unknown research outcome. Restore that historical combination.
	historical, err := json.Marshal(model.Research{UploadID: a.ID, ExpectedHash: a.Hash, Outcome: "unknown", Candidates: []model.ResearchCandidate{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.DB.DB.Exec("INSERT INTO research(upload_id,data) VALUES(?,?)", a.ID, string(historical)); err != nil {
		t.Fatal(err)
	}
	f.request("POST", route+"/plans/"+plan.ID+"/commit", token, "historical-commit", map[string]string{"fingerprint": plan.Fingerprint}).expect(t, 200, &detail)
	seen := map[string]model.Upload{}
	for _, item := range detail.Items {
		seen[item.Upload.ID] = item.Upload
	}
	if seen[a.ID].State != "staged" || seen[a.ID].ErrorCode != "research_author_unverified" || seen[a.ID].AssetID != "" || seen[b.ID].State != "ingested" || seen[b.ID].AssetID == "" {
		t.Fatal("the unsupported old item was published or its eligible neighbor was blocked")
	}
}
