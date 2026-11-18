package httpapi

import (
	"net/http"
	"qimeng.local/next/server/internal/service"
	"qimeng.local/next/server/internal/transport/generated"
)

func (a *API) GetDelegatedBudget(w http.ResponseWriter, r *http.Request, id generated.Id) {
	a.readJSON(w, r, func(r *http.Request) (any, error) {
		return a.s.DelegatedTaskBudget(r.Context(), principal(r), id)
	})
}

func (a *API) RenewDelegatedBudget(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.RenewDelegatedBudgetParams) {
	write[service.DelegatedBudgetUpdate](a, w, r, true, func(r *http.Request, w *service.Write, in service.DelegatedBudgetUpdate) (any, int, error) {
		v, e := a.s.RenewDelegatedBudget(r.Context(), w, id, in)
		return v, 200, e
	})
}

func (a *API) ReserveDelegatedCall(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.ReserveDelegatedCallParams) {
	write[struct{}](a, w, r, true, func(r *http.Request, w *service.Write, _ struct{}) (any, int, error) {
		v, e := a.s.ReserveDelegatedCall(r.Context(), w, id)
		return v, 200, e
	})
}

func (a *API) ListAIDelegations(w http.ResponseWriter, r *http.Request, params generated.ListAIDelegationsParams) {
	a.readJSON(w, r, func(r *http.Request) (any, error) {
		n, e := limit(r)
		if e != nil {
			return nil, e
		}
		q := r.URL.Query()
		return a.s.Delegations(r.Context(), principal(r), q.Get("state"), q.Get("cursor"), n)
	})
}

func (a *API) GetCurrentAIDelegation(w http.ResponseWriter, r *http.Request) {
	a.readJSON(w, r, func(r *http.Request) (any, error) { return a.s.CurrentDelegation(r.Context(), principal(r)) })
}

func (a *API) CreateAIDelegation(w http.ResponseWriter, r *http.Request) {
	issueSecret[service.DelegationPolicy](a, w, r, func(r *http.Request, w *service.Write, in service.DelegationPolicy) (any, int, error) {
		v, e := a.s.CreateDelegation(r.Context(), w, in)
		return v, 201, e
	})
}

func (a *API) RevokeAIDelegation(w http.ResponseWriter, r *http.Request, id generated.Id) {
	write[struct{}](a, w, r, false, func(r *http.Request, w *service.Write, _ struct{}) (any, int, error) {
		return nil, 204, a.s.RevokeDelegation(r.Context(), w, id)
	})
}

func (a *API) StartDelegatedTask(w http.ResponseWriter, r *http.Request, params generated.StartDelegatedTaskParams) {
	write[service.DelegatedTaskStart](a, w, r, true, func(r *http.Request, w *service.Write, in service.DelegatedTaskStart) (any, int, error) {
		v, e := a.s.StartDelegatedTask(r.Context(), w, in)
		return v, 201, e
	})
}

func (a *API) ResumeDelegatedTask(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.ResumeDelegatedTaskParams) {
	write[struct {
		CredentialSecret string `json:"credentialSecret"`
	}](a, w, r, true, func(r *http.Request, w *service.Write, in struct {
		CredentialSecret string `json:"credentialSecret"`
	}) (any, int, error) {
		v, e := a.s.ResumeDelegatedTask(r.Context(), w, id, in.CredentialSecret)
		return v, 200, e
	})
}
