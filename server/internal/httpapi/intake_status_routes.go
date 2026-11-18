package httpapi

import (
	"net/http"

	"qimeng.local/next/server/internal/service"
	"qimeng.local/next/server/internal/transport/generated"
)

func (a *API) ReportIntakeExecutorStatus(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.ReportIntakeExecutorStatusParams) {
	write[service.BatchExecutorReport](a, w, r, true, func(r *http.Request, w *service.Write, input service.BatchExecutorReport) (any, int, error) {
		status, err := a.s.ReportIntakeExecutorStatus(r.Context(), w, id, input)
		return status, http.StatusOK, err
	})
}
