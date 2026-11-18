package httpapi

import (
	"fmt"
	"net/http"
	"qimeng.local/next/server/internal/service"
	"qimeng.local/next/server/internal/transport/generated"
)

func (a *API) GetServiceMetrics(w http.ResponseWriter, r *http.Request) {
	if !allowScope(w, r, "read") {
		return
	}
	if err := service.Authorize(principal(r), "admin", ""); err != nil {
		fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	received, sent, requests := a.s.Traffic.Counts()
	fmt.Fprintf(w, "# TYPE qimeng_http_requests_total counter\nqimeng_http_requests_total %d\n# TYPE qimeng_http_received_bytes_total counter\nqimeng_http_received_bytes_total %d\n# TYPE qimeng_http_sent_bytes_total counter\nqimeng_http_sent_bytes_total %d\n", requests, received, sent)
}

func (a *API) GetSystemStatus(w http.ResponseWriter, r *http.Request) {
	a.readJSON(w, r, func(r *http.Request) (any, error) { return a.s.SystemStatus(r.Context(), principal(r)) })
}

func (a *API) ListSystemStorage(w http.ResponseWriter, r *http.Request, params generated.ListSystemStorageParams) {
	a.readJSON(w, r, func(r *http.Request) (any, error) {
		n, err := limit(r)
		if err != nil {
			return nil, err
		}
		return a.s.SystemStorage(r.Context(), principal(r), r.URL.Query().Get("cursor"), n)
	})
}

func (a *API) ListClientErrors(w http.ResponseWriter, r *http.Request, params generated.ListClientErrorsParams) {
	a.readJSON(w, r, func(r *http.Request) (any, error) {
		n, err := limit(r)
		if err != nil {
			return nil, err
		}
		return a.s.ClientErrors(r.Context(), principal(r), r.URL.Query().Get("cursor"), n)
	})
}

func (a *API) RecordClientError(w http.ResponseWriter, r *http.Request) {
	write[service.ClientErrorReport](a, w, r, false, func(r *http.Request, w *service.Write, v service.ClientErrorReport) (any, int, error) {
		item, err := a.s.RecordClientError(r.Context(), w, v)
		return item, 201, err
	})
}

func (a *API) ListMaintenanceJobs(w http.ResponseWriter, r *http.Request, params generated.ListMaintenanceJobsParams) {
	a.readJSON(w, r, func(r *http.Request) (any, error) {
		n, err := limit(r)
		if err != nil {
			return nil, err
		}
		q := r.URL.Query()
		return a.s.MaintenanceJobs(r.Context(), principal(r), q.Get("kind"), q.Get("state"), q.Get("cursor"), n)
	})
}
