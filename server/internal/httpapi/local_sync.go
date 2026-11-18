package httpapi

import (
	"net/http"
	"qimeng.local/next/server/internal/service"
	"qimeng.local/next/server/internal/transport/generated"
)

func (a *API) ResolveInputDirectories(w http.ResponseWriter, r *http.Request) {
	if !allowScope(w, r, "read") {
		return
	}
	in, _, e := decode[struct {
		Names []string `json:"names"`
	}](w, r)
	if e != nil {
		fail(w, e)
		return
	}
	v, e := a.s.ResolveInputDirectories(r.Context(), principal(r), in.Names)
	if e != nil {
		fail(w, e)
		return
	}
	respond(w, 200, v)
}

func (a *API) GetLocalSyncStatus(w http.ResponseWriter, r *http.Request) {
	a.readJSON(w, r, func(r *http.Request) (any, error) { return a.s.LocalSyncStatus(r.Context(), principal(r)) })
}

func (a *API) TriggerLocalSync(w http.ResponseWriter, r *http.Request, params generated.TriggerLocalSyncParams) {
	write[struct{}](a, w, r, true, func(r *http.Request, w *service.Write, _ struct{}) (any, int, error) {
		v, e := a.s.TriggerLocalSync(r.Context(), w)
		return v, 202, e
	})
}
