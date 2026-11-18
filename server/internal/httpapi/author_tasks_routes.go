package httpapi

import (
	"net/http"
	"qimeng.local/next/server/internal/service"
	"qimeng.local/next/server/internal/transport/generated"
)

type credentialRequest struct {
	Label        string   `json:"label"`
	BatchID      string   `json:"batchId"`
	AuthorTaskID string   `json:"authorTaskId"`
	Scopes       []string `json:"scopes"`
}

func (a *API) ListAuthorTasks(w http.ResponseWriter, r *http.Request, params generated.ListAuthorTasksParams) {
	a.readJSON(w, r, func(r *http.Request) (any, error) {
		n, e := limit(r)
		if e != nil {
			return nil, e
		}
		q := r.URL.Query()
		return a.s.AuthorTasks(r.Context(), principal(r), q.Get("authorId"), q.Get("state"), q.Get("cursor"), n)
	})
}

func (a *API) CreateAuthorTask(w http.ResponseWriter, r *http.Request, params generated.CreateAuthorTaskParams) {
	write[service.AuthorTaskCreate](a, w, r, true, func(r *http.Request, w *service.Write, in service.AuthorTaskCreate) (any, int, error) {
		value, e := a.s.CreateAuthorTask(r.Context(), w, in)
		return value, 201, e
	})
}

func (a *API) GetAuthorTask(w http.ResponseWriter, r *http.Request, id generated.Id) {
	a.readJSON(w, r, func(r *http.Request) (any, error) {
		return a.s.AuthorTaskContext(r.Context(), principal(r), id)
	})
}

func (a *API) CancelAuthorTask(w http.ResponseWriter, r *http.Request, id generated.Id) {
	write[struct{}](a, w, r, false, func(r *http.Request, w *service.Write, _ struct{}) (any, int, error) {
		return nil, 204, a.s.CancelAuthorTask(r.Context(), w, id)
	})
}
