package httpapi

import (
	"bytes"
	"net/http"
	"time"

	"qimeng.local/next/server/internal/service"
	"qimeng.local/next/server/internal/transport/generated"
)

func (a *API) ListV2Imports(w http.ResponseWriter, r *http.Request, params generated.ListV2ImportsParams) {
	a.readJSON(w, r, func(r *http.Request) (any, error) {
		n, e := limit(r)
		if e != nil {
			return nil, e
		}
		return a.s.V2Imports(r.Context(), principal(r), r.URL.Query().Get("cursor"), n)
	})
}

func (a *API) StageV2Import(w http.ResponseWriter, r *http.Request, params generated.StageV2ImportParams) {
	retiredV2Write(w, r)
}

func (a *API) GetV2Import(w http.ResponseWriter, r *http.Request, id generated.Id) {
	a.readJSON(w, r, func(r *http.Request) (any, error) { return a.s.V2Import(r.Context(), principal(r), id) })
}

func (a *API) DownloadV2Import(w http.ResponseWriter, r *http.Request, id generated.Id) {
	if !allowScope(w, r, "read") {
		return
	}
	raw, e := a.s.V2Document(r.Context(), principal(r), id)
	if e != nil {
		fail(w, e)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="qimeng-v2-original.json"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-store")
	http.ServeContent(w, r, "qimeng-v2-original.json", time.Time{}, bytes.NewReader(raw))
}

func (a *API) PrepareV2Migration(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.PrepareV2MigrationParams) {
	retiredV2Write(w, r)
}

func (a *API) GetV2MigrationPlan(w http.ResponseWriter, r *http.Request, id generated.Id) {
	a.readJSON(w, r, func(r *http.Request) (any, error) {
		return a.s.V2MigrationPlan(r.Context(), principal(r), id)
	})
}

func (a *API) ListV2MigrationItems(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.ListV2MigrationItemsParams) {
	a.readJSON(w, r, func(r *http.Request) (any, error) {
		n, e := limit(r)
		if e != nil {
			return nil, e
		}
		return a.s.V2MigrationItems(r.Context(), principal(r), id, r.URL.Query().Get("cursor"), n)
	})
}

func (a *API) ListV2MigrationAuthors(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.ListV2MigrationAuthorsParams) {
	a.readJSON(w, r, func(r *http.Request) (any, error) {
		n, e := limit(r)
		if e != nil {
			return nil, e
		}
		return a.s.V2MigrationAuthors(r.Context(), principal(r), id, r.URL.Query().Get("cursor"), n)
	})
}

func (a *API) CommitV2Migration(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.CommitV2MigrationParams) {
	retiredV2Write(w, r)
}

func retiredV2Write(w http.ResponseWriter, r *http.Request) {
	if err := service.Authorize(principal(r), "admin", ""); err != nil {
		fail(w, err)
		return
	}
	fail(w, service.Fail(http.StatusGone, "v2_import_retired", "旧 v2 写入已退役。现存计划与回执可以继续读取。"))
}
