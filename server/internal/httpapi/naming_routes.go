package httpapi

import (
	"net/http"
	"qimeng.local/next/server/internal/service"
)

func (a *API) namingRoutes() {
	a.get("/libraries/{id}/naming/context", func(r *http.Request) (any, error) {
		q := r.URL.Query()
		return a.s.NamingContext(r.Context(), r.PathValue("id"), q.Get("directory"), q.Get("family"), q.Get("mediaType"), q.Get("collectionId"))
	})
	a.get("/libraries/{id}/naming/suggestions", func(r *http.Request) (any, error) {
		q := r.URL.Query()
		return a.s.SuggestNames(r.Context(), r.PathValue("id"), q.Get("directory"), q.Get("q"), q.Get("mediaType"))
	})
	a.mux.HandleFunc("POST /api/v1/libraries/{id}/naming/preview", a.auth(func(w http.ResponseWriter, r *http.Request) {
		in, _, e := decode[service.NamingPreviewInput](w, r)
		if e != nil {
			fail(w, e)
			return
		}
		v, e := a.s.PreviewNames(r.Context(), r.PathValue("id"), in)
		if e != nil {
			fail(w, e)
			return
		}
		respond(w, 200, v)
	}))
}
