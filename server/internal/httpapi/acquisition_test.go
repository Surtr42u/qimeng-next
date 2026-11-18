package httpapi_test

import (
	"encoding/json"
	"testing"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/service"
)

func TestAcquisitionVocabularyPreservesCombinationsOrderAndExplicitEmpty(t *testing.T) {
	f := setup(t)
	r := f.json("GET", "/acquisition-vocabulary", "", nil, 200)
	var vocabulary service.AcquisitionVocabulary
	decode(t, r, &vocabulary)
	r = f.json("PUT", "/acquisition-vocabulary", "acquisition-words", map[string]any{"expectedRevision": vocabulary.Revision, "sources": []string{"渠道甲 渠道乙", "渠道乙  渠道丙"}}, 200)
	decode(t, r, &vocabulary)
	if len(vocabulary.Sources) != 2 || vocabulary.Sources[0] != "渠道甲 渠道乙" || len(vocabulary.Suggestions) != 3 || vocabulary.Suggestions[2] != "渠道丙" {
		t.Fatal("combined words were overwritten or reordered")
	}
	r = f.json("PUT", "/acquisition-vocabulary", "acquisition-clear", map[string]any{"expectedRevision": vocabulary.Revision, "sources": []string{}}, 200)
	decode(t, r, &vocabulary)
	if len(vocabulary.Sources) != 0 || len(vocabulary.Suggestions) != 0 {
		t.Fatal("explicit vocabulary clear was lost")
	}
	f.json("PUT", "/acquisition-vocabulary", "acquisition-stale", map[string]any{"expectedRevision": 1, "sources": []string{"old"}}, 409)
	r = f.json("POST", "/authors", "acquisition-author", map[string]any{"name": "Channel author", "homepage": "https://example.org/creator", "acquisitionSources": []string{"自购", "渠道甲"}}, 201)
	var author model.Author
	decode(t, r, &author)
	r = f.json("PATCH", "/authors/"+author.ID, "acquisition-author-clear", map[string]any{"expectedRevision": author.Revision, "acquisitionSources": []string{}}, 200)
	decode(t, r, &author)
	if len(author.AcquisitionSources) != 0 || author.Homepage != "https://example.org/creator" {
		t.Fatal("channel clearing changed public identity links")
	}
	r = f.json("POST", "/author-tasks", "acquisition-research-task", map[string]any{"authorId": author.ID, "expectedRevision": author.Revision, "allowedFields": []string{"biography"}, "allowClear": false}, 201)
	var task service.AuthorTask
	decode(t, r, &task)
	r = f.json("POST", "/credentials", "", map[string]any{"label": "Synthetic profile executor", "authorTaskId": task.ID, "scopes": []string{"read", "author.profile.write"}}, 201)
	var credential model.Principal
	decode(t, r, &credential)
	raw, _ := json.Marshal(map[string]any{"expectedRevision": author.Revision, "biography": "Allowed candidate", "acquisitionSources": []string{"not authorized"}})
	r = f.request("PATCH", "/authors/"+author.ID, "acquisition-scope-guard", credential.Token, raw, nil)
	if r.Code != 403 {
		t.Fatalf("new field escaped author task scope: %d %s", r.Code, r.Body.String())
	}
}
