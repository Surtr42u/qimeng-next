package httpapi

import (
	"net/http"
	"qimeng.local/next/server/internal/service"
	"qimeng.local/next/server/internal/transport/generated"
)

func (a *API) GetNamingContext(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.GetNamingContextParams) {
	a.readJSON(w, r, func(r *http.Request) (any, error) {
		q := r.URL.Query()
		return a.s.NamingContext(r.Context(), id, q.Get("directory"), q.Get("family"), q.Get("mediaType"), q.Get("collectionId"))
	})
}

func (a *API) SuggestNames(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.SuggestNamesParams) {
	a.readJSON(w, r, func(r *http.Request) (any, error) {
		q := r.URL.Query()
		return a.s.SuggestNames(r.Context(), id, q.Get("directory"), q.Get("q"), q.Get("mediaType"))
	})
}

func (a *API) PreviewNames(w http.ResponseWriter, r *http.Request, id generated.Id) {
	if !allowScope(w, r, "read") {
		return
	}
	in, _, e := decode[service.NamingPreviewInput](w, r)
	if e != nil {
		fail(w, e)
		return
	}
	v, e := a.s.PreviewNames(r.Context(), id, in)
	if e != nil {
		fail(w, e)
		return
	}
	respond(w, 200, v)
}
