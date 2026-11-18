package httpapi_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/service"
)

func profileAuthor(t *testing.T, f *fixture, name string) model.Author {
	t.Helper()
	var author model.Author
	decode(t, f.json("POST", "/authors", "profile-author-"+name, map[string]any{"name": name, "kind": "regular"}, 201), &author)
	return author
}

func profileEvidence() []map[string]any {
	return []map[string]any{{"url": "https://example.org/profile", "title": "Synthetic public profile", "fields": []string{"summary"}, "status": "candidate", "note": "Synthetic evidence"}}
}

func TestAuthorProfilesIndependentRevisionsTranslationAndClear(t *testing.T) {
	f := setup(t)
	author := profileAuthor(t, f, "Blocks")
	base := "/authors/" + author.ID + "/profile"
	var profile service.AuthorProfile
	decode(t, f.json("GET", base, "", nil, 200), &profile)
	if len(profile.Blocks) != 3 || !profile.Editable || profile.Blocks[0].Revision != 0 {
		t.Fatal("absent blocks not represented explicitly")
	}
	f.json("PATCH", base+"/x", "profile-x-initial", map[string]any{"expectedRevision": 0, "values": map[string]string{"biographyOriginal": "Original", "biographyZh": "译文"}, "translationReviewed": true}, 200)
	f.json("PATCH", base+"/basic", "profile-basic-initial", map[string]any{"expectedRevision": 0, "values": map[string]string{"summary": "Curated", "note": "Private note"}}, 200)
	f.json("PATCH", base+"/x", "profile-original-change", map[string]any{"expectedRevision": 1, "values": map[string]string{"biographyOriginal": "Changed"}}, 200)
	var summary service.AuthorProfileSummary
	decode(t, f.json("GET", "/authors/"+author.ID+"/profile-summary", "", nil, 200), &summary)
	if summary.BiographyOriginal != "Changed" || summary.BiographyZh != "" || summary.XRevision != 2 {
		t.Fatal("old translation remained associated with changed original")
	}
	f.json("PATCH", base+"/x", "profile-unreviewed-translation", map[string]any{"expectedRevision": 2, "values": map[string]string{"biographyOriginal": "Another", "biographyZh": "翻译"}}, 409)
	f.json("PATCH", base+"/basic", "profile-clear-note", map[string]any{"expectedRevision": 1, "values": map[string]string{"note": ""}}, 200)
	f.json("PATCH", base+"/basic", "profile-stale-note", map[string]any{"expectedRevision": 1, "values": map[string]string{"note": "Resurrect"}}, 409)
	f.json("PATCH", base+"/basic", "profile-wrong-block", map[string]any{"expectedRevision": 2, "values": map[string]string{"handle": "no"}}, 400)
	f.json("PATCH", base+"/basic", "profile-no-revision", map[string]any{"values": map[string]string{"summary": "no"}}, 400)
	decode(t, f.json("GET", base, "", nil, 200), &profile)
	basic := profile.Blocks[1]
	if basic.Values.Note == nil || *basic.Values.Note != "" || basic.Values.Summary == nil || *basic.Values.Summary != "Curated" || basic.Revision != 2 {
		t.Fatal("explicit clear changed unrelated data")
	}
	var unchanged model.Author
	decode(t, f.json("GET", "/authors/"+author.ID, "", nil, 200), &unchanged)
	if unchanged.Revision != author.Revision {
		t.Fatal("profile revisions were incorrectly coupled to identity")
	}
}

func TestAuthorProfileCandidatesAreScopedDurableAndRequireOwnerSave(t *testing.T) {
	f := setup(t)
	author := profileAuthor(t, f, "Candidate")
	other := profileAuthor(t, f, "Other")
	base := "/authors/" + author.ID + "/profile"
	var task service.AuthorTask
	decode(t, f.json("POST", "/author-tasks", "profile-candidate-task", map[string]any{"authorId": author.ID, "expectedRevision": author.Revision, "allowedFields": []string{}, "allowClear": false, "candidateBlocks": []string{"basic"}, "maxUpdates": 1, "expiresInSeconds": 3600}, 201), &task)
	var credential model.Principal
	decode(t, f.json("POST", "/credentials", "", map[string]any{"label": "Synthetic profile researcher", "authorTaskId": task.ID, "scopes": []string{"read", "research"}}, 201), &credential)
	input := map[string]any{"block": "basic", "baseRevision": 0, "values": map[string]string{"summary": "Suggested"}, "evidence": profileEvidence()}
	raw, _ := json.Marshal(input)
	denied := f.request("POST", "/authors/"+other.ID+"/profile/candidates", "candidate-wrong-author", credential.Token, raw, nil)
	if denied.Code != 409 {
		t.Fatalf("cross-author candidate accepted: %d", denied.Code)
	}
	write := f.request("PATCH", base+"/basic", "candidate-no-direct-write", credential.Token, []byte(`{"expectedRevision":0,"values":{"summary":"Unauthorized"}}`), nil)
	if write.Code != 403 {
		t.Fatal("research candidate wrote confirmed profile")
	}
	result := f.request("POST", base+"/candidates", "candidate-one-result", credential.Token, raw, nil)
	if result.Code != 201 {
		t.Fatal(result.Body.String())
	}
	var candidate service.ProfileCandidate
	decode(t, result, &candidate)
	replay := f.request("POST", base+"/candidates", "candidate-one-result", credential.Token, raw, nil)
	if replay.Code != 201 || replay.Body.String() != result.Body.String() {
		t.Fatal("lost candidate response was not replayed")
	}
	another := f.request("POST", base+"/candidates", "candidate-second-result", credential.Token, raw, nil)
	if another.Code != 409 {
		t.Fatal("candidate budget not consumed")
	}
	var profile service.AuthorProfile
	decode(t, f.json("GET", base, "", nil, 200), &profile)
	if profile.Blocks[1].Revision != 0 {
		t.Fatal("candidate changed confirmed block")
	}
	f.json("PATCH", base+"/basic", "candidate-human-save", map[string]any{"expectedRevision": 0, "values": map[string]string{"summary": "Human reviewed"}, "candidateId": candidate.ID, "evidence": profileEvidence()}, 200)
	var candidates model.Page[service.ProfileCandidate]
	decode(t, f.json("GET", base+"/candidates", "", nil, 200), &candidates)
	if len(candidates.Items) != 1 || candidates.Items[0].State != "applied" || candidates.Items[0].AppliedRevision != 1 {
		t.Fatal("candidate application provenance was lost")
	}
	f.json("POST", base+"/candidates/"+candidate.ID+"/dismiss", "candidate-already-applied", struct{}{}, 409)
}

func TestAuthorProfileAvatarVersionBindingAndAuthentication(t *testing.T) {
	f := setup(t)
	author := profileAuthor(t, f, "Avatar")
	base := "/authors/" + author.ID + "/profile"
	bytes := pngBytes(t, 92)
	uploaded := f.request("PUT", base+"/avatar?expectedRevision=0", "profile-avatar-upload", testSecret, bytes, map[string]string{"Content-Type": "application/octet-stream"})
	if uploaded.Code != 200 {
		t.Fatal(uploaded.Body.String())
	}
	var block service.AuthorProfileBlock
	decode(t, uploaded, &block)
	if block.Avatar == nil || block.Avatar.Origin != "upload" || block.Avatar.Width != 8 || block.Avatar.SourceURL != "" {
		t.Fatal("avatar source binding missing")
	}
	url := strings.TrimPrefix(block.Avatar.URL, "/api/v1")
	if response := f.request("GET", url, "", "", nil, nil); response.Code != 401 {
		t.Fatal("avatar readable without authentication")
	}
	if response := f.request("GET", url, "", testSecret, nil, nil); response.Code != 200 || response.Header().Get("Content-Type") != "image/png" {
		t.Fatal("normalized avatar unavailable")
	}
	replay := f.request("PUT", base+"/avatar?expectedRevision=0", "profile-avatar-upload", testSecret, bytes, nil)
	if replay.Code != 200 || replay.Body.String() != uploaded.Body.String() {
		t.Fatal("avatar replay altered revision")
	}
	conflict := f.request("PUT", base+"/avatar?expectedRevision=0", "profile-avatar-upload", testSecret, pngBytes(t, 93), nil)
	if conflict.Code != 409 {
		t.Fatal("avatar key reused with changed input")
	}
	f.json("POST", base+"/avatar-from-url", "profile-avatar-private", map[string]any{"expectedRevision": 1, "sourceUrl": "https://127.0.0.1/private"}, 400)
	f.json("PATCH", base+"/x", "profile-avatar-source-change", map[string]any{"expectedRevision": 1, "values": map[string]string{"avatarSourceUrl": "https://example.org/replacement.png"}}, 200)
	var summary service.AuthorProfileSummary
	decode(t, f.json("GET", "/authors/"+author.ID+"/profile-summary", "", nil, 200), &summary)
	if summary.Avatar != nil {
		t.Fatal("old cached image displayed under a new source")
	}
	if response := f.request("GET", url, "", testSecret, nil, nil); response.Code != 404 {
		t.Fatal("unreferenced avatar was not pruned")
	}
}

func TestAuthorProfileMergeAndDeletePreserveConfirmedConflictsAndAvatars(t *testing.T) {
	f := setup(t)
	source := profileAuthor(t, f, "MergeSource")
	target := profileAuthor(t, f, "MergeTarget")
	sourceBase := "/authors/" + source.ID + "/profile"
	targetBase := "/authors/" + target.ID + "/profile"
	f.json("PATCH", sourceBase+"/basic", "profile-merge-source", map[string]any{"expectedRevision": 0, "values": map[string]string{"summary": "Source confirmed"}}, 200)
	f.json("PATCH", targetBase+"/basic", "profile-merge-target-empty", map[string]any{"expectedRevision": 0, "values": map[string]string{"summary": ""}}, 200)
	f.json("POST", sourceBase+"/candidates", "profile-merge-candidate", map[string]any{"block": "basic", "baseRevision": 1, "values": map[string]string{"summary": "Pending source"}, "evidence": profileEvidence()}, 201)
	uploaded := f.request("PUT", sourceBase+"/avatar?expectedRevision=0", "profile-merge-avatar", testSecret, pngBytes(t, 81), nil)
	if uploaded.Code != 200 {
		t.Fatal(uploaded.Body.String())
	}
	var imageBlock service.AuthorProfileBlock
	decode(t, uploaded, &imageBlock)
	var result service.AuthorMergeResult
	decode(t, f.json("POST", "/authors/merge", "profile-merge-confirmed", map[string]any{"sourceId": source.ID, "targetId": target.ID, "expectedSourceRevision": source.Revision, "expectedTargetRevision": target.Revision}, 200), &result)
	if len(result.PreservedConflicts) != 1 || result.PreservedConflicts[0] != "profile.basic" {
		t.Fatalf("profile conflict omitted from merge report: %+v", result.PreservedConflicts)
	}
	var profile service.AuthorProfile
	decode(t, f.json("GET", targetBase, "", nil, 200), &profile)
	if profile.Blocks[1].Values.Summary == nil || *profile.Blocks[1].Values.Summary != "" || profile.Blocks[0].Avatar == nil {
		t.Fatal("merge overwrote explicit empty target or failed to inherit absent block")
	}
	var archives model.Page[service.ProfileArchiveSummary]
	decode(t, f.json("GET", targetBase+"/archives", "", nil, 200), &archives)
	if len(archives.Items) != 1 || archives.Items[0].CandidateCount != 1 || archives.Items[0].BlockCount != 2 {
		t.Fatal("source profile archive incomplete")
	}
	var candidates model.Page[service.ProfileCandidate]
	decode(t, f.json("GET", targetBase+"/candidates?archiveId="+archives.Items[0].ID, "", nil, 200), &candidates)
	if len(candidates.Items) != 1 || candidates.Items[0].AuthorID != source.ID || candidates.Items[0].ArchiveID != archives.Items[0].ID {
		t.Fatal("source candidate provenance lost")
	}
	f.json("DELETE", "/authors/"+target.ID+"?expectedRevision="+fmt.Sprint(result.Author.Revision), "profile-delete-target", struct{}{}, 204)
	f.json("GET", targetBase+"/archives", "", nil, 200)
	response := f.request("GET", strings.TrimPrefix(imageBlock.Avatar.URL, "/api/v1"), "", testSecret, nil, nil)
	if response.Code != 200 {
		t.Fatal("delete removed archived avatar evidence")
	}
}
