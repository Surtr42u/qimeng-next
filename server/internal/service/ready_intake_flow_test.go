package service_test

import (
	"bytes"
	"encoding/base64"
	"maps"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"qimeng.local/next/server/internal/httpapi"
	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/service"
)

const readyBatchRoute = "/ai/delegations/current/ready-batches"

func openQueueFixture(t *testing.T, dir string) (*researchFixture, func()) {
	t.Helper()
	s, err := service.New(service.Config{DataDir: dir, AdminToken: researchTestOwner, ReserveBytes: 1})
	if err != nil {
		t.Fatal(err)
	}
	api := httptest.NewServer(httpapi.New(s))
	stop := sync.OnceFunc(func() { api.Close(); s.Close() })
	t.Cleanup(stop)
	return &researchFixture{s: s, api: api}, stop
}

func queueGrant(t *testing.T, f *researchFixture, library string, organize bool) service.DelegationCreated {
	t.Helper()
	policy := service.DelegationPolicy{Label: "Synthetic queue worker", LibraryIDs: []string{library}, AuthorIDs: []string{}, AllowedFields: []string{}, CandidateBlocks: []string{}, CanIngest: true, CanOrganize: organize, MaxUpdates: 1, MaxTasksPerDay: 1, ValidDays: 1}
	var grant service.DelegationCreated
	f.request("POST", "/ai/delegations", researchTestOwner, "", policy).expect(t, 201, &grant)
	return grant
}

func queuedBatch(t *testing.T, f *researchFixture, key, library, mode string, inputs ...model.IntakeInput) model.Batch {
	t.Helper()
	var batch model.Batch
	f.request("POST", "/batches", researchTestOwner, key+"-create", map[string]any{"libraryId": library, "mode": mode, "label": key, "maxResearchCalls": 0}).expect(t, 201, &batch)
	if len(inputs) > 0 {
		f.request("PUT", "/batches/"+batch.ID+"/inputs", researchTestOwner, key+"-manifest", model.BatchInputsSet{ExpectedManifestRevision: batch.ManifestRevision, Inputs: inputs}).expect(t, 200, &batch)
	}
	return batch
}

func readyFor(t *testing.T, f *researchFixture, token string) []service.ReadyIntakeBatch {
	t.Helper()
	var result service.ReadyIntakeBatches
	f.request("GET", readyBatchRoute, token, "", nil).expect(t, 200, &result)
	if result.Items == nil {
		t.Fatal("queue response omitted its items array")
	}
	return result.Items
}

func TestReadyIntakeTwoWorkersRecoveryAndPublication(t *testing.T) {
	dir := t.TempDir()
	f, stop := openQueueFixture(t, dir)
	var library, outside model.Library
	f.request("POST", "/libraries", researchTestOwner, "queue-local-library", map[string]string{"name": "Local fixture", "kind": "regular"}).expect(t, 201, &library)
	f.request("POST", "/libraries", researchTestOwner, "queue-other-library", map[string]string{"name": "Other fixture", "kind": "regular"}).expect(t, 201, &outside)
	grants := []service.DelegationCreated{queueGrant(t, f, library.ID, true), queueGrant(t, f, library.ID, true)}
	low := queueGrant(t, f, library.ID, false)
	f.request("GET", readyBatchRoute, researchTestOwner, "", nil).expect(t, 403, nil)
	f.request("GET", readyBatchRoute, low.Credential.Token, "", nil).expect(t, 403, nil)
	input := func(id string) model.IntakeInput { return model.IntakeInput{ID: id, OriginalFileName: id + ".png"} }
	first, second, foreign := input("queue-first"), input("queue-second"), input("queue-private")
	queuedBatch(t, f, "app-open", library.ID, "intelligent")
	ordinary := queuedBatch(t, f, "app-ordinary", library.ID, "ordinary", input("ordinary-file"))
	out := queuedBatch(t, f, "other-library", outside.ID, "intelligent", foreign)
	f.stage(t, outside.ID, out.ID, foreign.ID, foreign.OriginalFileName, "", 11)
	rejected := model.IntakeInput{ID: "queue-rejection", OriginalFileName: "bad.txt"}
	queuedBatch(t, f, "only-rejected", library.ID, "intelligent", rejected)
	b := queuedBatch(t, f, "app-mixed", library.ID, "intelligent", first, second, rejected)
	a := f.stage(t, library.ID, b.ID, first.ID, first.OriginalFileName, "", 67)
	token := grants[0].Credential.Token
	if got := readyFor(t, f, token); len(got) != 0 {
		t.Fatal("manual, outside, unsealed, rejected-only or partially transferred intake appeared")
	}
	secrets := []string{base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{51}, 32)), base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{52}, 32))}
	claim := map[string]string{"kind": "intake", "mode": "intelligent", "label": "Resume settled uploads", "libraryId": library.ID, "batchId": b.ID, "readyManifestFingerprint": b.ManifestFingerprint, "credentialSecret": secrets[0]}
	f.request("POST", "/ai/delegated-tasks", token, "still-transferring", claim).expect(t, 409, nil)
	foreignClaim := maps.Clone(claim)
	foreignClaim["libraryId"], foreignClaim["batchId"], foreignClaim["readyManifestFingerprint"] = outside.ID, out.ID, out.ManifestFingerprint
	f.request("POST", "/ai/delegated-tasks", token, "different-library", foreignClaim).expect(t, 403, nil)
	ordinaryClaim := maps.Clone(claim)
	ordinaryClaim["batchId"], ordinaryClaim["readyManifestFingerprint"] = ordinary.ID, ordinary.ManifestFingerprint
	f.request("POST", "/ai/delegated-tasks", token, "never-claim-ordinary", ordinaryClaim).expect(t, 409, nil)
	z := f.stage(t, library.ID, b.ID, second.ID, second.OriginalFileName, "", 154)
	for _, g := range grants {
		rows := readyFor(t, f, g.Credential.Token)
		if len(rows) != 1 || rows[0].BatchID != b.ID || rows[0].LibraryID != library.ID || rows[0].ManifestFingerprint != b.ManifestFingerprint {
			t.Fatal("ready list did not restrict visibility or excluded the valid remainder of mixed input")
		}
	}
	revoked := queueGrant(t, f, library.ID, true)
	if len(readyFor(t, f, revoked.Credential.Token)) != 1 {
		t.Fatal("active grant could not observe the ready batch")
	}
	f.request("DELETE", "/ai/delegations/"+revoked.Delegation.ID, researchTestOwner, "", nil).expect(t, 204, nil)
	f.request("POST", "/ai/delegated-tasks", revoked.Credential.Token, "revoked-after-poll", claim).expect(t, 401, nil)
	stale := maps.Clone(claim)
	stale["readyManifestFingerprint"] = strings.Repeat("0", 64)
	f.request("POST", "/ai/delegated-tasks", token, "wrong-sealed-manifest", stale).expect(t, 409, nil)
	// A legacy scoped writer may already be working on this intake. Automatic
	// discovery cannot take it over, and a read-only monitor may coexist.
	var writer, reader model.Principal
	f.request("POST", "/credentials", researchTestOwner, "", map[string]any{"label": "Manual writer", "batchId": b.ID, "scopes": []string{"read", "organize"}}).expect(t, 201, &writer)
	if len(readyFor(t, f, token)) != 0 {
		t.Fatal("another scoped executor's batch was offered")
	}
	f.request("POST", "/ai/delegated-tasks", token, "keep-manual-writer", claim).expect(t, 409, nil)
	f.request("DELETE", "/credentials/"+writer.ID, researchTestOwner, "", nil).expect(t, 204, nil)
	f.request("POST", "/credentials", researchTestOwner, "", map[string]any{"label": "Read-only monitor", "batchId": b.ID, "scopes": []string{"read"}}).expect(t, 201, &reader)

	start := make(chan struct{})
	result := make(chan struct {
		index int
		reply apiReply
	}, 2)
	keys := []string{"claim-from-executor-a", "claim-from-executor-b"}
	for i := range grants {
		go func(i int) {
			attempt := maps.Clone(claim)
			attempt["credentialSecret"] = secrets[i]
			<-start
			result <- struct {
				index int
				reply apiReply
			}{i, f.request("POST", "/ai/delegated-tasks", grants[i].Credential.Token, keys[i], attempt)}
		}(i)
	}
	close(start)
	winner := -1
	var accepted apiReply
	for range grants {
		got := <-result
		if got.reply.status == 201 && winner < 0 {
			winner, accepted = got.index, got.reply
		} else {
			got.reply.expect(t, 409, nil)
			if !bytes.Contains(got.reply.raw, []byte("task_already_claimed")) {
				t.Fatal("competing executor did not receive the ownership conflict")
			}
		}
	}
	if winner < 0 || bytes.Contains(accepted.raw, []byte(secrets[winner])) {
		t.Fatal("no single claim or exposed executor secret")
	}
	other := 1 - winner
	f.request("GET", readyBatchRoute, secrets[winner], "", nil).expect(t, 403, nil)
	if len(readyFor(t, f, grants[other].Credential.Token)) != 0 {
		t.Fatal("the same batch was offered again")
	}
	stop()
	f, _ = openQueueFixture(t, dir)
	token = grants[winner].Credential.Token
	claim["credentialSecret"] = secrets[winner]
	replay := f.request("POST", "/ai/delegated-tasks", token, keys[winner], claim)
	replay.expect(t, 201, nil)
	if !bytes.Equal(accepted.raw, replay.raw) {
		t.Fatal("lost response or service restart allocated a different task")
	}
	var receipt struct {
		State, Operation string
		Response         service.DelegatedTask
	}
	f.request("GET", "/operations/"+keys[winner]+"?operation=POST%20%2Fapi%2Fv1%2Fai%2Fdelegated-tasks", token, "", nil).expect(t, 200, &receipt)
	if receipt.State != "committed" || receipt.Response.TaskID != b.ID || receipt.Operation != "POST /api/v1/ai/delegated-tasks" || receipt.Response.Credential.ParentDelegationID != grants[winner].Delegation.ID {
		t.Fatal("claim could not be reconciled against its issuing grant")
	}
	f.request("POST", "/ai/delegated-tasks/"+b.ID+"/resume", grants[other].Credential.Token, "foreign-cannot-resume", map[string]string{"credentialSecret": secrets[other]}).expect(t, 403, nil)
	f.request("GET", "/batches/"+b.ID, reader.Token, "", nil).expect(t, 200, nil)
	f.request("GET", readyBatchRoute, token, "", nil).expect(t, 429, nil)
	resumed := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{53}, 32))
	f.request("POST", "/ai/delegated-tasks/"+b.ID+"/resume", token, "resume-owned-intake", map[string]string{"credentialSecret": resumed}).expect(t, 200, nil)
	f.request("GET", "/batches/"+b.ID, secrets[winner], "", nil).expect(t, 401, nil)
	var detail model.BatchDetail
	f.request("GET", "/batches/"+b.ID, resumed, "", nil).expect(t, 200, &detail)
	var plan model.Plan
	in := model.PlanCreate{ExpectedRevision: detail.Batch.Revision, Items: []model.PlanInput{{UploadID: a.ID, Family: "First"}, {UploadID: z.ID, Family: "Second"}}}
	f.request("POST", "/batches/"+b.ID+"/plans", resumed, "queue-prepare-plan", in).expect(t, 201, &plan)
	commit := "/batches/" + b.ID + "/plans/" + plan.ID + "/commit"
	outcome := f.request("POST", commit, resumed, "queue-publish-plan", map[string]string{"fingerprint": plan.Fingerprint})
	outcome.expect(t, 200, &detail)
	again := f.request("POST", commit, resumed, "queue-publish-plan", map[string]string{"fingerprint": plan.Fingerprint})
	again.expect(t, 200, nil)
	if !bytes.Equal(outcome.raw, again.raw) || len(detail.Items) != 2 || len(detail.Inputs) != 3 || detail.Batch.State != "partial" {
		t.Fatal("mixed input publication lost its dispositions or was repeated")
	}
	for _, item := range detail.Items {
		if item.Upload.State != "ingested" || item.Upload.AssetID == "" || item.Upload.PublishedContentVersion != 1 {
			t.Fatal("a viable original was not published once")
		}
	}
	var count int
	if err := f.s.DB.DB.QueryRow("SELECT COUNT(*) FROM assets WHERE library_id=?", library.ID).Scan(&count); err != nil || count != 2 {
		t.Fatal("expected exactly two assets in the claimed library", err)
	}
	f.request("DELETE", "/ai/delegations/"+grants[other].Delegation.ID, researchTestOwner, "", nil).expect(t, 204, nil)
	f.request("GET", readyBatchRoute, grants[other].Credential.Token, "", nil).expect(t, 401, nil)
}
