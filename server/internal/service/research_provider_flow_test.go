package service_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"qimeng.local/next/server/internal/httpapi"
	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/service"
)

const researchTestOwner = "synthetic-owner-for-provider-flow-only"

type researchFixture struct {
	s    *service.Service
	api  *httptest.Server
	hits atomic.Int32
}

func researchServer(t *testing.T, hold chan struct{}, entered chan struct{}) *researchFixture {
	t.Helper()
	f := &researchFixture{}
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.hits.Add(1)
		if r.Host != "api.fxtwitter.com" || r.Method != http.MethodGet || r.URL.Path != "/2/profile/fixture" || r.URL.RawQuery != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" || r.ContentLength > 0 {
			t.Error("unexpected destination or private inputs on the upstream request")
			w.WriteHeader(400)
			return
		}
		if entered != nil {
			select {
			case entered <- struct{}{}:
			default:
			}
		}
		if hold != nil {
			select {
			case <-hold:
			case <-r.Context().Done():
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"code":200,"user":{"id":"9","name":"Different platform display name","screen_name":"fixture","website":{"url":"https://example.org/credits?private=drop"}}}`)
	}))
	t.Cleanup(upstream.Close)
	transport := upstream.Client().Transport.(*http.Transport).Clone()
	transport.TLSClientConfig = transport.TLSClientConfig.Clone()
	fixtureHost, _, err := net.SplitHostPort(upstream.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	transport.TLSClientConfig.ServerName = fixtureHost
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != "api.fxtwitter.com:443" {
			return nil, fmt.Errorf("unexpected upstream address")
		}
		return (&net.Dialer{}).DialContext(ctx, network, upstream.Listener.Addr().String())
	}
	t.Cleanup(transport.CloseIdleConnections)
	f.s, err = service.New(service.Config{DataDir: t.TempDir(), AdminToken: researchTestOwner, ReserveBytes: 1, ResearchEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.s.Close() })
	f.s.UseResearchTransportForTest(transport)
	f.api = httptest.NewServer(httpapi.New(f.s))
	t.Cleanup(f.api.Close)
	return f
}

type apiReply struct {
	status int
	raw    []byte
	err    error
}

func (f *researchFixture) request(method, path, token, key string, body any) apiReply {
	raw, err := json.Marshal(body)
	if err != nil {
		return apiReply{err: err}
	}
	req, err := http.NewRequest(method, f.api.URL+"/api/v1"+path, bytes.NewReader(raw))
	if err != nil {
		return apiReply{err: err}
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	resp, err := f.api.Client().Do(req)
	if err != nil {
		return apiReply{err: err}
	}
	defer resp.Body.Close()
	raw, err = io.ReadAll(resp.Body)
	return apiReply{resp.StatusCode, raw, err}
}
func (r apiReply) expect(t *testing.T, status int, dst any) {
	t.Helper()
	if r.err != nil {
		t.Fatal(r.err)
	}
	if r.status != status {
		var problem struct {
			Code string `json:"code"`
		}
		json.Unmarshal(r.raw, &problem)
		t.Fatalf("HTTP %d, want %d, code %q", r.status, status, problem.Code)
	}
	if dst != nil {
		if err := json.Unmarshal(r.raw, dst); err != nil {
			t.Fatal(err)
		}
	}
}
func (f *researchFixture) authorGrant(t *testing.T) (model.Author, service.DelegationCreated) {
	t.Helper()
	var author model.Author
	f.request("POST", "/authors", researchTestOwner, "research-fixture-author", map[string]string{"name": "Human name", "biography": "Human biography"}).expect(t, 201, &author)
	policy := service.DelegationPolicy{
		Label: "Fixture research permission", LibraryIDs: []string{}, AuthorIDs: []string{author.ID},
		AllowedFields: []string{}, CandidateBlocks: []string{"basic"}, CanResearch: true,
		ResearchProviders: []string{"fxtwitter"}, MaxResearchCalls: 1, MaxUpdates: 2, MaxTasksPerDay: 8, ValidDays: 1,
	}
	var grant service.DelegationCreated
	f.request("POST", "/ai/delegations", researchTestOwner, "", policy).expect(t, 201, &grant)
	return author, grant
}
func (f *researchFixture) authorTask(t *testing.T, a model.Author, grant service.DelegationCreated, label string, repeated byte) (service.DelegatedTask, string) {
	t.Helper()
	secret := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{repeated}, 32))
	in := map[string]any{"kind": "author", "label": label, "authorId": a.ID, "expectedRevision": a.Revision, "candidateBlocks": []string{"basic"}, "credentialSecret": secret}
	var task service.DelegatedTask
	f.request("POST", "/ai/delegated-tasks", grant.Credential.Token, label+"-start", in).expect(t, 201, &task)
	return task, secret
}

func TestAuthorizedProviderHTTPReplayAndBudget(t *testing.T) {
	hold, entered := make(chan struct{}), make(chan struct{}, 1)
	f := researchServer(t, hold, entered)
	defer func() {
		select {
		case <-hold:
		default:
			close(hold)
		}
	}()
	author, grant := f.authorGrant(t)
	task, token := f.authorTask(t, author, grant, "first-task", 1)
	_, other := f.authorTask(t, author, grant, "different-task", 2)
	query := "/research/providers/fxtwitter/query"
	in := service.PublicResearchInput{Operation: "author", Target: "@fixture"}
	for _, caller := range []string{researchTestOwner, grant.Credential.Token} {
		f.request("POST", query, caller, "owner-no-egress", in).expect(t, 403, nil)
	}
	f.request("POST", "/research/providers/pixiv/query", token, "denied-provider", service.PublicResearchInput{Operation: "author", Target: "9"}).expect(t, 403, nil)
	first := make(chan apiReply, 1)
	go func() { first <- f.request("POST", query, token, "one-stable-query", in) }()
	select {
	case <-entered:
	case reply := <-first:
		reply.expect(t, 200, nil)
		t.Fatal("request never reached the fixture")
	case <-time.After(5 * time.Second):
		t.Fatal("fixture upstream not reached")
	}
	var pending, record service.PublicResearchResult
	f.request("POST", query, token, "one-stable-query", in).expect(t, 202, &pending)
	const exactReceipt = "/operations/one-stable-query?operation=queryResearchProvider"
	f.request("GET", exactReceipt, token, "", nil).expect(t, 409, nil)
	if pending.ID == "" || pending.State != "pending" || pending.SubjectRevision != author.Revision || pending.SubjectID != author.ID {
		t.Fatalf("invalid pending receipt: %+v", pending)
	}
	f.request("POST", query, token, "one-stable-query", service.PublicResearchInput{Operation: "author", Target: "another"}).expect(t, 409, nil)
	f.request("POST", query, token, "out-of-budget", in).expect(t, 429, nil)
	close(hold)
	(<-first).expect(t, 200, &record)
	if record.ID != pending.ID || record.State != "ok" || len(record.Items) != 1 || record.Items[0].URL != "https://x.com/fixture" || !slices.Equal(record.Items[0].Sources, []string{"https://example.org/credits"}) {
		t.Fatalf("unexpected normalized evidence: %+v", record)
	}
	f.request("POST", query, token, "one-stable-query", in).expect(t, 200, &record)
	f.request("GET", "/research/queries/"+record.ID, other, "", nil).expect(t, 403, nil)
	f.request("GET", "/research/queries/"+record.ID, token, "", nil).expect(t, 200, &record)
	var budget service.DelegatedBudget
	f.request("GET", "/ai/delegated-tasks/"+task.TaskID+"/budget", researchTestOwner, "", nil).expect(t, 200, &budget)
	var after model.Author
	f.request("GET", "/authors/"+author.ID, researchTestOwner, "", nil).expect(t, 200, &after)
	if budget.Calls != 1 || f.hits.Load() != 1 || after.Revision != author.Revision || after.Name != author.Name || after.Biography != author.Biography {
		t.Fatal("read-only query modified the subject, double-charged or fetched twice")
	}
	var receipt struct {
		State          string                       `json:"state"`
		Operation      string                       `json:"operation"`
		IdempotencyKey string                       `json:"idempotencyKey"`
		Response       service.PublicResearchResult `json:"response"`
	}
	f.request("GET", exactReceipt, other, "", nil).expect(t, 200, &receipt)
	if receipt.State != "not_recorded" || receipt.Response.ID != "" {
		t.Fatal("another task could reconcile this receipt")
	}
	// A different real operation can use the same key under the same principal.
	f.request("POST", "/ai/delegated-tasks/"+task.TaskID+"/calls", token, "one-stable-query", struct{}{}).expect(t, 200, nil)
	f.request("GET", "/operations/one-stable-query", token, "", nil).expect(t, 409, nil)
	f.request("GET", exactReceipt, token, "", nil).expect(t, 200, &receipt)
	if receipt.State != "committed" || receipt.Operation != "queryResearchProvider" || receipt.IdempotencyKey != "one-stable-query" || receipt.Response.ID != record.ID || receipt.Response.TaskID != task.TaskID {
		t.Fatal("exact receipt lost the original operation, key, task or query")
	}
}

func TestProviderPolicyMustBeExplicitAndCannotExpand(t *testing.T) {
	f := researchServer(t, nil, nil)
	author, grant := f.authorGrant(t)
	secret := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{6}, 32))
	in := map[string]any{"kind": "author", "label": "Narrower scope", "authorId": author.ID, "expectedRevision": author.Revision, "candidateBlocks": []string{"basic"}, "credentialSecret": secret, "researchProviders": []string{"pixiv"}}
	f.request("POST", "/ai/delegated-tasks", grant.Credential.Token, "deny-provider-expansion", in).expect(t, 403, nil)
	in["researchProviders"] = []string{}
	f.request("POST", "/ai/delegated-tasks", grant.Credential.Token, "drop-all-providers", in).expect(t, 201, nil)
	var narrowed service.PublicResearchContext
	f.request("GET", "/research/context", secret, "", nil).expect(t, 200, &narrowed)
	if len(narrowed.AllowedProviders) != 0 {
		t.Fatal("an explicit empty task allowlist was expanded")
	}
	policy := grant.Delegation.DelegationPolicy
	policy.ResearchProviders = nil
	var local service.DelegationCreated
	f.request("POST", "/ai/delegations", researchTestOwner, "", policy).expect(t, 201, &local)
	delete(in, "researchProviders")
	secret = base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{8}, 32))
	in.CredentialSecret = secret
	f.request("POST", "/ai/delegated-tasks", local.Credential.Token, "local-author-research", in).expect(t, 201, nil)
	var permissions service.PublicResearchContext
	f.request("GET", "/research/context", secret, "", nil).expect(t, 200, &permissions)
	if !permissions.Enabled || len(permissions.AllowedProviders) != 0 {
		t.Fatal("instance setting granted outbound permission")
	}
	f.request("POST", "/research/providers/fxtwitter/query", secret, "no-egress-without-selection", service.PublicResearchInput{Operation: "author", Target: "fixture"}).expect(t, 403, nil)
	if f.hits.Load() != 0 {
		t.Fatal("request escaped an empty allowlist")
	}
}

func TestResearchRecoveryKeepsItsIssuerAcrossRotationAndIsolatedRestore(t *testing.T) {
	f := researchServer(t, nil, nil)
	author, grant := f.authorGrant(t)
	task, original := f.authorTask(t, author, grant, "recover-query-task", 3)
	const key = "stable-research-recovery"
	const lookup = "/operations/" + key + "?operation=queryResearchProvider"
	const endpoint = "/research/providers/fxtwitter/query"
	input := service.PublicResearchInput{Operation: "author", Target: "fixture"}
	var result service.PublicResearchResult
	f.request("POST", endpoint, original, key, input).expect(t, 200, &result)
	rotated := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{21}, 32))
	f.request("POST", "/ai/delegated-tasks/"+task.TaskID+"/resume", grant.Credential.Token, "rotate-for-recovery", map[string]string{"credentialSecret": rotated}).expect(t, 200, nil)
	f.request("GET", lookup, original, "", nil).expect(t, 401, nil)
	var outcome map[string]any
	f.request("GET", lookup, rotated, "", nil).expect(t, 200, &outcome)
	if outcome["state"] != "committed" {
		t.Fatal("the resumed task lost a terminal receipt established by the same credential identity")
	}
	// A recovered batch can have the same task ID and operation key under a new
	// grant. Older development records may have no evidence of their issuer.
	for _, change := range []string{
		"json_set(data,'$.receiptCredentialId','earlier-credential')",
		"json_set(data,'$.receiptCredentialId',?,'$.receiptDelegationId','earlier-delegation')",
		"json_remove(data,'$.receiptCredentialId','$.receiptDelegationId')",
	} {
		args := []any{result.ID}
		if strings.Contains(change, "?") {
			args = []any{task.Credential.ID, result.ID}
		}
		if _, err := f.s.DB.DB.Exec("UPDATE research_provider_calls SET data="+change+" WHERE id=?", args...); err != nil {
			t.Fatal(err)
		}
		outcome = nil
		f.request("GET", lookup, rotated, "", nil).expect(t, 200, &outcome)
		if outcome["state"] != "not_recorded" {
			t.Fatal("same batch and key vouched for another issuer")
		}
		f.request("POST", endpoint, rotated, key, input).expect(t, 409, nil)
		f.request("GET", "/research/queries/"+result.ID, rotated, "", nil).expect(t, 403, nil)
		f.request("GET", "/research/queries/"+result.ID, researchTestOwner, "", nil).expect(t, 200, nil)
	}
	if f.hits.Load() != 1 {
		t.Fatal("reconciling a lookup sent it again")
	}
}

func TestSubjectEditedWhilePublicQueryRuns(t *testing.T) {
	hold, entered := make(chan struct{}), make(chan struct{}, 1)
	f := researchServer(t, hold, entered)
	defer func() {
		select {
		case <-hold:
		default:
			close(hold)
		}
	}()
	author, grant := f.authorGrant(t)
	_, token := f.authorTask(t, author, grant, "subject-edit-test", 7)
	const endpoint = "/research/providers/fxtwitter/query"
	in := service.PublicResearchInput{Operation: "author", Target: "fixture"}
	done := make(chan apiReply, 1)
	go func() { done <- f.request("POST", endpoint, token, "human-changed-subject", in) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("pending request did not dispatch")
	}
	var pending service.PublicResearchResult
	f.request("POST", endpoint, token, "human-changed-subject", in).expect(t, 202, &pending)
	f.request("PATCH", "/authors/"+author.ID, researchTestOwner, "edit-while-fetching", map[string]any{"expectedRevision": author.Revision, "biography": "Owner updated the profile during the query"}).expect(t, 200, nil)
	close(hold)
	(<-done).expect(t, 409, nil)
	f.request("GET", "/research/queries/"+pending.ID, researchTestOwner, "", nil).expect(t, 200, &pending)
	if pending.State != "interrupted" || len(pending.Items) != 0 {
		t.Fatal("obsolete platform response survived a human edit")
	}
	var replay service.PublicResearchResult
	f.request("POST", endpoint, token, "human-changed-subject", in).expect(t, 200, &replay)
	if replay.ID != pending.ID || replay.State != "interrupted" || f.hits.Load() != 1 {
		t.Fatal("reconciling a changed subject must return the original terminal receipt without new egress")
	}
}

func TestRevokingPublicProviderInterruptsInFlight(t *testing.T) {
	hold, entered := make(chan struct{}), make(chan struct{}, 1)
	f := researchServer(t, hold, entered)
	defer close(hold)
	author, grant := f.authorGrant(t)
	_, token := f.authorTask(t, author, grant, "revocation-task", 3)
	in := service.PublicResearchInput{Operation: "author", Target: "fixture"}
	const endpoint = "/research/providers/fxtwitter/query"
	done := make(chan apiReply, 1)
	go func() { done <- f.request("POST", endpoint, token, "revoked-pending", in) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("pending request did not dispatch")
	}
	var receipt service.PublicResearchResult
	f.request("POST", endpoint, token, "revoked-pending", in).expect(t, 202, &receipt)
	f.request("DELETE", "/ai/delegations/"+grant.Delegation.ID, researchTestOwner, "", nil).expect(t, 204, nil)
	select {
	case reply := <-done:
		reply.expect(t, 403, nil)
	case <-time.After(3 * time.Second):
		t.Fatal("revocation left upstream request running")
	}
	f.request("GET", "/research/queries/"+receipt.ID, researchTestOwner, "", nil).expect(t, 200, &receipt)
	if receipt.State != "interrupted" || receipt.FinishedAt == nil || len(receipt.Items) != 0 {
		t.Fatal("revoked authority returned or retained candidate evidence")
	}
}

func TestPublicProviderStdioMCP(t *testing.T) {
	python := os.Getenv("QIMENG_PROVIDER_MCP_PYTHON")
	if python == "" {
		t.Skip("real stdio subprocess runs in the cloud MCP job")
	}
	f := researchServer(t, nil, nil)
	author, grant := f.authorGrant(t)
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, python, filepath.Join(root, "ai", "tests", "provider_stdio.py"))
	cmd.Env = append(os.Environ(), "QIMENG_TEST_BASE="+f.api.URL, "QIMENG_PUBLIC_TEST_GRANT="+grant.Credential.Token, "QIMENG_PUBLIC_TEST_AUTHOR="+author.ID)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("MCP synthetic subprocess: %v\n%s", err, strings.ReplaceAll(string(output), grant.Credential.Token, "[redacted]"))
	}
	if f.hits.Load() != 1 {
		t.Fatalf("MCP emitted %d upstream requests", f.hits.Load())
	}
}

func (f *researchFixture) stage(t *testing.T, library, batch, input, original, author string, shade uint8) model.Upload {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 5, 5))
	img.Set(1, 1, color.NRGBA{R: shade, A: 255})
	var content bytes.Buffer
	if err := png.Encode(&content, img); err != nil {
		t.Fatal(err)
	}
	in := map[string]any{"libraryId": library, "batchId": batch, "mode": "intelligent", "inputId": input,
		"originalFileName": original, "fileName": original, "size": content.Len(), "mimeType": "image/png"}
	if author != "" {
		in["authorIds"] = []string{author}
	}
	var upload model.Upload
	f.request("POST", "/uploads", researchTestOwner, "fixture-upload-"+input, in).expect(t, 201, &upload)
	req, err := http.NewRequest(http.MethodPut, f.api.URL+"/api/v1/uploads/"+upload.ID+"/content", bytes.NewReader(content.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+researchTestOwner)
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Upload-Offset", "0")
	resp, err := f.api.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("upload chunk: %d", resp.StatusCode)
	}
	var result model.UploadResult
	f.request("POST", "/uploads/"+upload.ID+"/complete", researchTestOwner, "fixture-complete-"+input, struct{}{}).expect(t, 200, &result)
	if result.Upload.Hash != fmt.Sprintf("%x", sha256.Sum256(content.Bytes())) || result.Upload.State != "staged" {
		t.Fatal("original was not staged")
	}
	return result.Upload
}

func TestChargedLookupBecomesOnlyCandidateForOriginalIntake(t *testing.T) {
	f := researchServer(t, nil, nil)
	author, _ := f.authorGrant(t)
	var library model.Library
	f.request("POST", "/libraries", researchTestOwner, "intake-research-library", map[string]string{"name": "Synthetic originals", "kind": "regular"}).expect(t, 201, &library)
	policy := service.DelegationPolicy{
		Label: "Intake public lookup", LibraryIDs: []string{library.ID}, AuthorIDs: []string{},
		AllowedFields: []string{}, CandidateBlocks: []string{}, CanIngest: true, CanOrganize: true, CanResearch: true,
		ResearchProviders: []string{"fxtwitter"}, MaxResearchCalls: 1, MaxUpdates: 1, MaxTasksPerDay: 2, ValidDays: 1,
	}
	var grant service.DelegationCreated
	f.request("POST", "/ai/delegations", researchTestOwner, "", policy).expect(t, 201, &grant)
	token := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{5}, 32))
	var task service.DelegatedTask
	f.request("POST", "/ai/delegated-tasks", grant.Credential.Token, "intake-research-start", map[string]any{
		"kind": "intake", "label": "Two synthetic originals", "libraryId": library.ID, "mode": "intelligent", "credentialSecret": token}).expect(t, 201, &task)
	batch := "/batches/" + task.TaskID
	inputs := []model.IntakeInput{{ID: "unidentified", OriginalFileName: "private-input.png"}, {ID: "human-chosen", OriginalFileName: "another-input.png"}}
	f.request("PUT", batch+"/inputs", researchTestOwner, "intake-research-manifest", model.BatchInputsSet{ExpectedManifestRevision: 1, Inputs: inputs}).expect(t, 200, nil)
	unknown := f.stage(t, library.ID, task.TaskID, inputs[0].ID, inputs[0].OriginalFileName, "", 30)
	confirmed := f.stage(t, library.ID, task.TaskID, inputs[1].ID, inputs[1].OriginalFileName, author.ID, 60)
	const gateway = "/research/providers/fxtwitter/query"
	in := service.PublicResearchInput{Operation: "author", Target: "fixture", UploadID: confirmed.ID, ExpectedHash: confirmed.Hash}
	f.request("POST", gateway, token, "keep-known-creator", in).expect(t, 409, nil)
	in.UploadID = unknown.ID
	f.request("POST", gateway, token, "wrong-original-version", in).expect(t, 409, nil)
	in.ExpectedHash = unknown.Hash
	var receipt service.PublicResearchResult
	f.request("POST", gateway, token, "unknown-source-query", in).expect(t, 200, &receipt)
	if receipt.State != "ok" || receipt.SubjectID != unknown.ID || receipt.SubjectHash != unknown.Hash {
		t.Fatal("missing binding to original upload")
	}
	var detail model.BatchDetail
	f.request("GET", batch, token, "", nil).expect(t, 200, &detail)
	result := model.Research{ExpectedRevision: detail.Batch.Revision, UploadID: confirmed.ID, ExpectedHash: confirmed.Hash,
		Outcome: "candidates", Candidates: []model.ResearchCandidate{{Name: "fixture", SourceURL: "https://x.com/fixture", Evidence: "Public synthetic account record."}},
		CallsUsed: 0, ProviderQueryIDs: []string{receipt.ID}}
	f.request("POST", batch+"/research", token, "cross-object-evidence", result).expect(t, 409, nil)
	result.UploadID = unknown.ID
	result.ExpectedHash = unknown.Hash
	result.Candidates[0].SourceURL = "https://example.org/unrelated"
	f.request("POST", batch+"/research", token, "fabricated-source", result).expect(t, 409, nil)
	result.Candidates[0].SourceURL = "https://x.com/fixture"
	f.request("POST", batch+"/research", token, "store-actual-evidence", result).expect(t, 200, nil)
	f.request("GET", batch, token, "", nil).expect(t, 200, &detail)
	if detail.Batch.ResearchCalls != 1 || f.hits.Load() != 1 {
		t.Fatal("received evidence charged a second research call")
	}
	for _, item := range detail.Items {
		if item.Upload.ID == unknown.ID && (item.Upload.AuthorID != "unknown" || item.ResearchOutcome != "candidates" || !slices.Equal(item.ProviderQueryIDs, []string{receipt.ID})) {
			t.Fatal("candidate changed original authority or lost receipt")
		}
	}
	plan := model.PlanCreate{ExpectedRevision: detail.Batch.Revision, Items: []model.PlanInput{{UploadID: confirmed.ID, Family: "Example", AuthorIDs: []string{"unknown"}}}}
	f.request("POST", batch+"/plans", token, "plan-cannot-replace-user", plan).expect(t, 409, nil)
	plan.Items[0].AuthorIDs = nil
	f.request("POST", batch+"/plans", token, "plan-preserves-selection", plan).expect(t, 201, nil)
}
