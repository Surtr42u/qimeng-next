package httpapi

import (
	"net/http"

	"qimeng.local/next/server/internal/transport/generated"
)

func (a *API) GetLibraryLocation(w http.ResponseWriter, r *http.Request, id generated.Id) {
	a.readJSON(w, r, func(r *http.Request) (any, error) {
		return a.s.LibraryLocation(r.Context(), principal(r), id)
	})
}

func (a *API) ResolveLibraryRoot(w http.ResponseWriter, r *http.Request) {
	if !allowScope(w, r, "read") {
		return
	}
	in, _, e := decode[struct {
		RootPath string `json:"rootPath"`
	}](w, r)
	if e != nil {
		fail(w, e)
		return
	}
	location, e := a.s.ResolveLibraryRoot(principal(r), in.RootPath)
	if e != nil {
		fail(w, e)
		return
	}
	respond(w, http.StatusOK, location)
}
