package httpapi

import (
	"bytes"
	"net/http"
	"qimeng.local/next/server/internal/service"
	"qimeng.local/next/server/internal/transport/generated"
	"time"
)

func (a *API) GetResearchProviderContext(w http.ResponseWriter, r *http.Request) {
	a.readJSON(w, r, func(r *http.Request) (any, error) { return a.s.ResearchProviderContext(r.Context(), principal(r)) })
}

func (a *API) GetResearchProviderResult(w http.ResponseWriter, r *http.Request, id generated.Id) {
	a.readJSON(w, r, func(r *http.Request) (any, error) {
		return a.s.PublicResearchResult(r.Context(), principal(r), id)
	})
}

func (a *API) QueryResearchProvider(w http.ResponseWriter, r *http.Request, provider generated.ResearchProviderName, params generated.QueryResearchProviderParams) {
	if !allowScope(w, r, "read") {
		return
	}
	in, _, e := decode[service.PublicResearchInput](w, r)
	if e != nil {
		fail(w, e)
		return
	}
	out, e := a.s.QueryResearchProvider(r.Context(), principal(r), string(provider), r.Header.Get("Idempotency-Key"), in)
	if e != nil {
		fail(w, e)
		return
	}
	result(w, out)
}

func (a *API) GetAssetMaintenanceTask(w http.ResponseWriter, r *http.Request, id generated.Id) {
	a.readJSON(w, r, func(r *http.Request) (any, error) {
		return a.s.AssetTaskContext(r.Context(), principal(r), id)
	})
}

func (a *API) ListAssetTaskEvidence(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.ListAssetTaskEvidenceParams) {
	a.readJSON(w, r, func(r *http.Request) (any, error) {
		n, e := limit(r)
		if e != nil {
			return nil, e
		}
		return a.s.AssetTaskEvidence(r.Context(), principal(r), id, r.URL.Query().Get("cursor"), n)
	})
}

func (a *API) GetAssetTaskMaterials(w http.ResponseWriter, r *http.Request, id generated.Id) {
	a.readJSON(w, r, func(r *http.Request) (any, error) {
		return a.s.AssetTaskMaterials(r.Context(), principal(r), id)
	})
}

func (a *API) ApplyAssetTaskMetadata(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.ApplyAssetTaskMetadataParams) {
	write[service.AssetMaintenanceUpdate](a, w, r, true, func(r *http.Request, w *service.Write, in service.AssetMaintenanceUpdate) (any, int, error) {
		v, e := a.s.ApplyAssetMaintenance(r.Context(), w, id, in)
		return v, 200, e
	})
}

func (a *API) RenameAssetForTask(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.RenameAssetForTaskParams) {
	write[service.AssetTaskRename](a, w, r, true, func(r *http.Request, w *service.Write, in service.AssetTaskRename) (any, int, error) {
		v, e := a.s.RenameAssetForTask(r.Context(), w, id, in)
		return v, 200, e
	})
}

func (a *API) GetUploadResearchMaterials(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.GetUploadResearchMaterialsParams) {
	a.readJSON(w, r, func(r *http.Request) (any, error) {
		return a.s.UploadResearchMaterials(r.Context(), principal(r), id, r.URL.Query().Get("expectedHash"))
	})
}

func (a *API) GetUploadContent(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.GetUploadContentParams) {
	if !allowScope(w, r, "read") {
		return
	}
	f, u, e := a.s.UploadContent(r.Context(), principal(r), id, r.URL.Query().Get("expectedHash"))
	if e != nil {
		fail(w, e)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", u.MimeType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("ETag", `"`+u.Hash+`"`)
	http.ServeContent(w, r, u.OriginalFileName, u.CreatedAt, f)
}

func (a *API) GetResearchArtifact(w http.ResponseWriter, r *http.Request, id generated.Id) {
	a.readJSON(w, r, func(r *http.Request) (any, error) {
		artifact, _, e := a.s.ResearchArtifact(r.Context(), principal(r), id)
		return artifact, e
	})
}

func (a *API) GetResearchArtifactContent(w http.ResponseWriter, r *http.Request, id generated.Id) {
	if !allowScope(w, r, "read") {
		return
	}
	artifact, content, e := a.s.ResearchArtifact(r.Context(), principal(r), id)
	if e != nil {
		fail(w, e)
		return
	}
	w.Header().Set("Content-Type", artifact.MimeType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("ETag", `"`+artifact.SHA256+`"`)
	http.ServeContent(w, r, "research-preview.jpg", artifact.ExpiresAt.Add(-24*time.Hour), bytes.NewReader(content))
}
