package httpapi

import "net/http"

// GetLibraryRevision exposes the instance-local preview revision used by device
// prefetch to decide whether a full scan can be skipped.
func (a *API) GetLibraryRevision(w http.ResponseWriter, r *http.Request) {
	a.readJSON(w, r, func(r *http.Request) (any, error) {
		return a.s.LibraryRevision(r.Context(), principal(r))
	})
}
