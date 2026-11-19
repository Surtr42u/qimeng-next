package service_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"qimeng.local/next/server/internal/httpapi"
	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/service"
)

const fixtureLensSecret = "synthetic-sidecar-test-only"
const imageLookupRoute = "/research/image-lookup"

func imageResearchServer(t *testing.T, endpoint string) *researchFixture {
	t.Helper()
	s, err := service.New(service.Config{DataDir: t.TempDir(), AdminToken: researchTestOwner, ReserveBytes: 1, ResearchLensEndpoint: endpoint, ResearchSidecarToken: fixtureLensSecret})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	api := httptest.NewServer(httpapi.New(s))
	t.Cleanup(api.Close)
	return &researchFixture{s: s, api: api}
}

func imageResearchGrant(t *testing.T, f *researchFixture, library string, allowed bool) service.DelegationCreated {
	t.Helper()
	policy := service.DelegationPolicy{Label: "Synthetic framed input", LibraryIDs: []string{library}, AuthorIDs: []string{}, AllowedFields: []string{}, CandidateBlocks: []string{},
		CanIngest: true, CanOrganize: true, CanResearch: true, CanLookupImages: allowed, MaxResearchCalls: 4, MaxUpdates: 1, MaxTasksPerDay: 8, ValidDays: 1}
	var grant service.DelegationCreated
	f.request("POST", "/ai/delegations", researchTestOwner, "", policy).expect(t, 201, &grant)
	return grant
}

func imageResearchTask(t *testing.T, f *researchFixture, grant service.DelegationCreated, library string, imageOptIn bool, n byte, status int) (string, string) {
	t.Helper()
	token := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{n}, 32))
	var task service.DelegatedTask
	f.request("POST", "/ai/delegated-tasks", grant.Credential.Token, fmt.Sprintf("image-task-%d", n), map[string]any{
		"kind": "intake", "label": "Synthetic intake", "libraryId": library, "mode": "intelligent", "allowImageLookup": imageOptIn, "credentialSecret": token,
	}).expect(t, status, &task)
	return task.TaskID, token
}

func TestImageLookupUsesOnlyOneAuthorizedDerivedFrameAndReceipt(t *testing.T) {
	var sends atomic.Int32
	received := make(chan string, 1)
	release := make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := sends.Add(1)
		if r.Method != http.MethodPost || r.URL.RequestURI() != "/lens" || r.Header.Get("Authorization") != "Bearer "+fixtureLensSecret || r.Header.Get("Cookie") != "" || r.Header.Get("Idempotency-Key") != "" {
			t.Error("sidecar received unexpected method, destination or credentials")
			http.Error(w, "unexpected request", 400)
			return
		}
		form, err := r.MultipartReader()
		if err != nil {
			t.Error(err)
			return
		}
		part, err := form.NextPart()
		if err != nil || part.FormName() != "file" || part.FileName() != "frame.jpg" || part.Header.Get("Content-Type") != "image/jpeg" {
			t.Error("sidecar did not receive one neutral JPEG part")
			return
		}
		frame, err := io.ReadAll(io.LimitReader(part, (2<<20)+1))
		dim, jpegErr := jpeg.DecodeConfig(bytes.NewReader(frame))
		if err != nil || len(frame) > 2<<20 || jpegErr != nil || dim.Width > 640 || dim.Height > 640 {
			t.Error("outbound file was not a bounded JPEG preview")
			return
		}
		if _, err = form.NextPart(); err != io.EOF {
			t.Error("outbound request carried extra fields")
			return
		}
		sha := fmt.Sprintf("%x", sha256.Sum256(frame))
		w.Header().Set("Content-Type", "application/json")
		payload := map[string]any{"ok": true, "status": "ok", "frame_sha256": sha}
		switch n {
		case 1:
			received <- sha
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			payload["links"] = []map[string]string{{"url": "https://example.org/work/7?utm_source=placeholder", "title": "Possible source", "author": "untrusted-claim"}}
			payload["cites"] = []map[string]string{{"url": "https://example.org/work/7"}}
			payload["topTitles"] = []map[string]string{{"domain": "example.org", "title": "Unlinked page title"}}
			payload["result_url"] = "https://example.org/private-result"
		case 2:
			payload["links"] = []string{}
		case 3:
			payload["frame_sha256"] = strings.Repeat("0", 64)
		case 4:
			w.WriteHeader(502)
			payload = map[string]any{"ok": false, "error": map[string]string{"code": "NO_RESULT", "message": "private-upstream-detail"}}
		}
		json.NewEncoder(w).Encode(payload)
	}))
	t.Cleanup(upstream.Close)
	f := imageResearchServer(t, upstream.URL)
	var library model.Library
	f.request("POST", "/libraries", researchTestOwner, "image-lookup-library", map[string]string{"name": "Synthetic inputs", "kind": "regular"}).expect(t, 201, &library)
	denied := imageResearchGrant(t, f, library.ID, false)
	imageResearchTask(t, f, denied, library.ID, true, 41, 403)
	grant := imageResearchGrant(t, f, library.ID, true)
	_, narrowToken := imageResearchTask(t, f, grant, library.ID, false, 42, 201)
	inputs := []model.IntakeInput{{ID: "fixture-first", OriginalFileName: "synthetic-first.png"}, {ID: "fixture-second", OriginalFileName: "synthetic-second.png"}}
	app := queuedBatch(t, f, "image-lookup-manifest", library.ID, "intelligent", inputs...)
	batchID, token := app.ID, base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{43}, 32))
	batch := "/batches/" + batchID
	u := f.stage(t, library.ID, batchID, inputs[0].ID, inputs[0].OriginalFileName, "", 81)
	second := f.stage(t, library.ID, batchID, inputs[1].ID, inputs[1].OriginalFileName, "", 127)
	claim := map[string]any{"kind": "intake", "mode": "intelligent", "label": "App frame lookup", "batchId": batchID, "libraryId": library.ID,
		"readyManifestFingerprint": app.ManifestFingerprint, "credentialSecret": token, "allowImageLookup": true}
	f.request("POST", "/ai/delegated-tasks", denied.Credential.Token, "no-image-claim", claim).expect(t, 403, nil)
	f.request("POST", "/ai/delegated-tasks", grant.Credential.Token, "authorized-image-claim", claim).expect(t, 201, nil)
	var materials service.ResearchMaterials
	f.request("GET", "/uploads/"+u.ID+"/research-materials?expectedHash="+u.Hash, token, "", nil).expect(t, 200, &materials)
	if len(materials.Artifacts) != 1 {
		t.Fatal("synthetic still did not produce one derived frame")
	}
	frame := materials.Artifacts[0]
	in := service.ResearchImageLookup{FrameID: frame.ID, FrameSHA256: frame.SHA256, UploadID: u.ID, ExpectedHash: u.Hash}
	var context service.PublicResearchContext
	f.request("GET", "/research/context", token, "", nil).expect(t, 200, &context)
	if context.Enabled || !context.ImageLookupEnabled || !context.ImageLookupAllowed || len(context.AllowedProviders) != 0 {
		t.Fatal("single-frame permission is coupled to text lookup")
	}
	for _, caller := range []string{researchTestOwner, grant.Credential.Token, narrowToken} {
		f.request("POST", imageLookupRoute, caller, "disallowed-image-lookup", in).expect(t, 403, nil)
	}
	wrong := in
	wrong.UploadID, wrong.ExpectedHash = second.ID, second.Hash
	f.request("POST", imageLookupRoute, token, "wrong-frame-owner", wrong).expect(t, 409, nil)
	wrong = in
	wrong.FrameSHA256 = strings.Repeat("0", 64)
	f.request("POST", imageLookupRoute, token, "wrong-frame-sha", wrong).expect(t, 409, nil)
	f.request("POST", imageLookupRoute, token, "no-arbitrary-url", map[string]any{"frameId": frame.ID, "frameSha256": frame.SHA256, "uploadId": u.ID, "expectedHash": u.Hash, "url": "https://example.org/"}).expect(t, 400, nil)
	if sends.Load() != 0 {
		t.Fatal("forbidden or mismatched input sent a request")
	}
	const key = "stable-frame-lookup"
	first := make(chan apiReply, 1)
	go func() { first <- f.request("POST", imageLookupRoute, token, key, in) }()
	select {
	case sha := <-received:
		if sha != frame.SHA256 || sha == u.Hash {
			t.Fatal("received bytes differ from the selected derivative or equal the original")
		}
	case reply := <-first:
		reply.expect(t, 200, nil)
		t.Fatal("lookup returned without contacting the sidecar")
	case <-time.After(5 * time.Second):
		t.Fatal("sidecar did not receive frame")
	}
	var pending, record service.PublicResearchResult
	f.request("POST", imageLookupRoute, token, key, in).expect(t, 202, &pending)
	f.request("GET", "/operations/"+key+"?operation=lookupResearchImage", token, "", nil).expect(t, 409, nil)
	f.request("POST", imageLookupRoute, token, key, wrong).expect(t, 409, nil)
	close(release)
	reply := <-first
	reply.expect(t, 200, &record)
	if pending.State != "pending" || pending.ID != record.ID || record.State != "ok" || record.EvidenceKind != "lead" || record.Provider != "lens" || record.FrameID != frame.ID || record.FrameSHA256 != frame.SHA256 || record.SubjectHash != u.Hash || len(record.Items) != 1 || record.Items[0].URL != "https://example.org/work/7" || len(record.ImageHints) != 1 {
		t.Fatal("receipt lost binding or promoted unverified hints")
	}
	for _, private := range []string{fixtureLensSecret, inputs[0].OriginalFileName, "private-result", "untrusted-claim", "utm_source"} {
		if bytes.Contains(reply.raw, []byte(private)) {
			t.Fatal("private upstream or source data escaped the receipt")
		}
	}
	replayed := f.request("POST", imageLookupRoute, token, key, in)
	replayed.expect(t, 200, nil)
	if !bytes.Equal(reply.raw, replayed.raw) || sends.Load() != 1 {
		t.Fatal("idempotent replay changed the receipt or repeated egress")
	}
	var recovered struct {
		State, Operation string
		Response         service.PublicResearchResult
	}
	f.request("GET", "/operations/"+key+"?operation=lookupResearchImage", token, "", nil).expect(t, 200, &recovered)
	if recovered.State != "committed" || recovered.Operation != "lookupResearchImage" || recovered.Response.ID != record.ID {
		t.Fatal("lost response could not be recovered")
	}
	f.request("GET", "/operations/"+key+"?operation=queryResearchProvider", token, "", nil).expect(t, 200, &recovered)
	if recovered.State != "not_recorded" {
		t.Fatal("text and image receipt operations were confused")
	}
	for i, want := range []string{"not_found", "unavailable", "unavailable"} {
		key := fmt.Sprintf("finished-frame-%d", i)
		var result service.PublicResearchResult
		response := f.request("POST", imageLookupRoute, token, key, in)
		response.expect(t, 200, &result)
		if result.State != want || len(result.Items) != 0 || len(result.ImageHints) != 0 || strings.Contains(string(response.raw), "private-upstream-detail") {
			t.Fatalf("image result %d has wrong status or leaked an unverified response", i)
		}
		replay := f.request("POST", imageLookupRoute, token, key, in)
		if !bytes.Equal(replay.raw, response.raw) {
			t.Fatal("terminal failure or empty result was not replayed")
		}
	}
	f.request("POST", imageLookupRoute, token, "frame-over-budget", in).expect(t, 429, nil)
	var detail model.BatchDetail
	f.request("GET", batch, token, "", nil).expect(t, 200, &detail)
	if sends.Load() != 4 || detail.Batch.ResearchCalls != 4 || detail.Batch.MaxResearchCalls != 8 {
		t.Fatal("denied calls or replays consumed a request or research budget")
	}
	var possible model.Author
	f.request("POST", "/authors", researchTestOwner, "image-unverified-name", map[string]string{"name": "Unverified name"}).expect(t, 201, &possible)
	evidence := model.Research{ExpectedRevision: detail.Batch.Revision, UploadID: u.ID, ExpectedHash: u.Hash, Outcome: "candidates", CallsUsed: 0, ProviderQueryIDs: []string{record.ID},
		Candidates: []model.ResearchCandidate{{AuthorID: possible.ID, Name: possible.Name, Evidence: "Candidate source only", SourceURL: record.Items[0].URL}}}
	f.request("POST", batch+"/research", token, "store-frame-lead", evidence).expect(t, 200, nil)
	f.request("GET", batch, token, "", nil).expect(t, 200, &detail)
	f.request("POST", batch+"/plans", token, "image-cannot-confirm-author", model.PlanCreate{ExpectedRevision: detail.Batch.Revision,
		Items: []model.PlanInput{{UploadID: u.ID, Family: "Candidate", AuthorIDs: []string{possible.ID}}}}).expect(t, 409, nil)
	for _, item := range detail.Items {
		if item.Upload.ID == u.ID && (item.Upload.AuthorID != "unknown" || item.ResearchOutcome != "candidates") {
			t.Fatal("image leads changed confirmed attribution")
		}
	}
}

func TestImageLookupRequiresConfiguredSidecar(t *testing.T) {
	f := imageResearchServer(t, "")
	var library model.Library
	f.request("POST", "/libraries", researchTestOwner, "no-sidecar-library", map[string]string{"name": "Synthetic library", "kind": "regular"}).expect(t, 201, &library)
	grant := imageResearchGrant(t, f, library.ID, true)
	_, token := imageResearchTask(t, f, grant, library.ID, true, 44, 201)
	in := service.ResearchImageLookup{FrameID: "synthetic", FrameSHA256: strings.Repeat("a", 64), UploadID: "synthetic", ExpectedHash: strings.Repeat("b", 64)}
	f.request("POST", imageLookupRoute, token, "disabled-image-lookup", in).expect(t, 403, nil)
	var c service.PublicResearchContext
	f.request("GET", "/research/context", token, "", nil).expect(t, 200, &c)
	if c.ImageLookupEnabled || !c.ImageLookupAllowed || c.RemainingResearchCalls == nil || *c.RemainingResearchCalls != 4 {
		t.Fatal("disabled instance consumed a call or advertised egress")
	}
}
