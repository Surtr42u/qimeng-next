package httpapi

import (
	"net/http"
	"qimeng.local/next/server/internal/service"
	"qimeng.local/next/server/internal/transport/generated"
)

func (a *API) GetLibraryMonitoring(w http.ResponseWriter, r *http.Request, id generated.Id) {
	a.readJSON(w, r, func(r *http.Request) (any, error) {
		return a.s.LibraryMonitoring(r.Context(), principal(r), id)
	})
}

func (a *API) ListLibraryScans(w http.ResponseWriter, r *http.Request, params generated.ListLibraryScansParams) {
	a.readJSON(w, r, func(r *http.Request) (any, error) {
		n, e := limit(r)
		if e != nil {
			return nil, e
		}
		q := r.URL.Query()
		return a.s.Scans(r.Context(), q.Get("libraryId"), q.Get("state"), q.Get("cursor"), n)
	})
}

func (a *API) StartLibraryScan(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.StartLibraryScanParams) {
	write[revisionBody](a, w, r, true, func(r *http.Request, w *service.Write, in revisionBody) (any, int, error) {
		v, e := a.s.StartScan(r.Context(), w, id, in.ExpectedRevision)
		return v, 202, e
	})
}

func (a *API) GetLibraryScan(w http.ResponseWriter, r *http.Request, id generated.Id) {
	a.readJSON(w, r, func(r *http.Request) (any, error) { return a.s.Scan(r.Context(), id) })
}

func (a *API) CancelLibraryScan(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.CancelLibraryScanParams) {
	write[struct{}](a, w, r, true, func(r *http.Request, w *service.Write, _ struct{}) (any, int, error) {
		v, e := a.s.CancelScan(r.Context(), w, id)
		return v, 200, e
	})
}
