package httpapi

import (
	"net/http"

	"qimeng.local/next/server/internal/service"
)

func (a *API) completeUploadRequest(w http.ResponseWriter, r *http.Request, id string) {
	a.authScope("read", func(w http.ResponseWriter, r *http.Request) {
		_, raw, err := decode[struct{}](w, r)
		if err != nil {
			fail(w, err)
			return
		}
		key := r.Header.Get("Idempotency-Key")
		if key == "" {
			fail(w, service.Fail(400, "idempotency_required", "此操作必须提供稳定幂等键。"))
			return
		}
		raw = append(raw, []byte("\x00"+r.URL.Query().Encode())...)
		out, err := a.s.RunUploadCompletion(r.Context(), principal(r), id, r.Method+" "+r.URL.Path, key, raw)
		if err != nil {
			fail(w, err)
			return
		}
		result(w, out)
	})(w, r)
}
