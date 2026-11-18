package httpapi_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/service"
)

func TestResearchMaterialsPersistAndRespectOriginalUploadScope(t *testing.T) {
	f := setup(t)
	u, original := f.upload("research-persist.png", 78, nil)
	completed := f.complete(u)
	u = completed.Upload
	var providers service.PublicResearchContext
	decode(t, f.json("GET", "/research/context", "", nil, 200), &providers)
	if providers.Enabled || len(providers.AllowedProviders) != 0 {
		t.Fatal("public provider egress is enabled by default")
	}
	f.json("POST", "/research/providers/fxtwitter/query", "research-disabled-key", service.PublicResearchInput{Operation: "author", Target: "fixture"}, 503)
	request := "/uploads/" + u.ID + "/research-materials?expectedHash=" + u.Hash
	var first service.ResearchMaterials
	decode(t, f.json("GET", request, "", nil, 200), &first)
	if first.BusinessWrites != 0 || first.SourceSHA256 != u.Hash || first.Width != 8 || first.Height != 8 || len(first.Artifacts) != 1 {
		t.Fatalf("bad research metadata: %+v", first)
	}
	artifact := first.Artifacts[0]
	if artifact.ArtifactRef != "sa_"+artifact.ID || artifact.MimeType != "image/jpeg" || artifact.Size <= 0 {
		t.Fatal("invalid standard image reference")
	}
	// Each query opens a new cache connection; the reference is not process-local state.
	var again service.ResearchMaterials
	decode(t, f.json("GET", request, "", nil, 200), &again)
	if again.Artifacts[0].ID != artifact.ID {
		t.Fatal("same original lost persistent preview identity")
	}
	image := f.request("GET", "/research-artifacts/"+artifact.ID+"/content", "", testSecret, nil, nil)
	if image.Code != 200 || image.Header().Get("Content-Type") != "image/jpeg" || !bytes.HasPrefix(image.Body.Bytes(), []byte{0xff, 0xd8, 0xff}) {
		t.Fatal("invalid research content")
	}
	raw := f.request("GET", "/uploads/"+u.ID+"/content?expectedHash="+u.Hash, "", testSecret, nil, map[string]string{"Range": "bytes=0-7"})
	if raw.Code != 206 || !bytes.Equal(raw.Body.Bytes(), original[:8]) {
		t.Fatal("original range read changed bytes")
	}
	f.json("GET", "/uploads/"+u.ID+"/research-materials?expectedHash=changed", "", nil, 409)
	var other model.Batch
	decode(t, f.json("POST", "/batches", "other-research-batch", map[string]any{"libraryId": f.library, "mode": "intelligent", "label": "Other scope"}, 201), &other)
	var credential model.Principal
	decode(t, f.json("POST", "/credentials", "", map[string]any{"label": "Other executor", "batchId": other.ID, "scopes": []string{"read", "research"}}, 201), &credential)
	for _, path := range []string{request, "/research-artifacts/" + artifact.ID, "/research-artifacts/" + artifact.ID + "/content", "/uploads/" + u.ID + "/content?expectedHash=" + u.Hash} {
		response := f.request("GET", path, "", credential.Token, nil, nil)
		if response.Code != 403 {
			t.Fatalf("cross-task research read: %d", response.Code)
		}
	}
	var assets model.Page[model.Asset]
	decode(t, f.json("GET", "/assets", "", nil, 200), &assets)
	if len(assets.Items) != 1 {
		t.Fatal("read-only research changed business inventory")
	}
	encoded, _ := json.Marshal(first)
	for _, private := range []string{f.s.Config.DataDir, testSecret} {
		if bytes.Contains(encoded, []byte(private)) {
			t.Fatal("private execution information exposed")
		}
	}
	f.json("DELETE", "/assets/"+completed.Asset.ID+"?expectedRevision=1", "research-trash-key", struct{}{}, 200)
	f.json("GET", "/research-artifacts/"+artifact.ID, "", nil, 404)
}
