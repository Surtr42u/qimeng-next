package httpapi

import (
	"net/http"
	"qimeng.local/next/server/internal/service"
	"qimeng.local/next/server/internal/transport/generated"
)

func (a *API) GetCustomSourceGroups(w http.ResponseWriter, r *http.Request) {
	if !allowScope(w, r, "admin") {
		return
	}
	value, err := a.s.SourceRules(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	respond(w, 200, value)
}

func (a *API) SetCustomSourceGroups(w http.ResponseWriter, r *http.Request, params generated.SetCustomSourceGroupsParams) {
	writeScoped(a, w, r, "admin", true, func(r *http.Request, w *service.Write, in service.SourceRuleUpdate) (any, int, error) {
		value, err := a.s.SetSourceRules(r.Context(), w, in)
		return value, 200, err
	})
}
