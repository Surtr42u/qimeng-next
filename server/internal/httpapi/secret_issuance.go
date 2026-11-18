package httpapi

import (
	"net/http"

	"qimeng.local/next/server/internal/service"
)

// Issuance returns the plaintext secret once. A replayable mutation would
// persist the entire HTTP response, which must never happen on this path.
func issueSecret[T any](a *API, w http.ResponseWriter, r *http.Request, issue func(*http.Request, *service.Write, T) (any, int, error)) {
	if !allowScope(w, r, "read") {
		return
	}
	if r.Header.Get("Idempotency-Key") != "" {
		fail(w, service.Fail(400, "secret_not_replayable", "凭据只在签发时显示一次，请移除幂等键。"))
		return
	}
	input, raw, err := decode[T](w, r)
	if err != nil {
		fail(w, err)
		return
	}
	value, err := a.s.Run(r.Context(), principal(r), r.Method+" "+r.URL.Path, "", raw, func(write *service.Write) (any, int, error) {
		return issue(r, write, input)
	})
	if err != nil {
		fail(w, err)
		return
	}
	result(w, value)
}
