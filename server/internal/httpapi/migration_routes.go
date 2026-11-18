package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"time"

	legacy "qimeng.local/next/server/internal/migration"
	"qimeng.local/next/server/internal/service"
)

func (a *API) migrationRoutes() {
	a.get("/migration/v2/imports", func(r *http.Request) (any, error) {
		n, e := limit(r)
		if e != nil {
			return nil, e
		}
		return a.s.V2Imports(r.Context(), principal(r), r.URL.Query().Get("cursor"), n)
	})
	a.mux.HandleFunc("POST /api/v1/migration/v2/imports", a.auth(func(w http.ResponseWriter, r *http.Request) {
		if e := service.Authorize(principal(r), "admin", ""); e != nil {
			fail(w, e)
			return
		}
		key := r.Header.Get("Idempotency-Key")
		if len(key) < 8 || len(key) > 128 {
			fail(w, service.Fail(400, "idempotency_required", "迁移输入需要稳定的幂等键。"))
			return
		}
		mediaType, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if e != nil || mediaType != "application/octet-stream" {
			fail(w, service.Fail(415, "migration_content_type", "请以 application/octet-stream 上传完整 v2 JSON 文件。"))
			return
		}
		raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, legacy.MaxInputBytes))
		if e != nil {
			fail(w, service.Fail(413, "migration_input_too_large", "迁移输入最多 64 MiB。"))
			return
		}
		sourceID := r.URL.Query().Get("sourceId")
		hash := sha256.Sum256(raw)
		identity, e := json.Marshal(struct{ Source, Hash string }{sourceID, hex.EncodeToString(hash[:])})
		if e != nil {
			fail(w, e)
			return
		}
		out, e := a.s.Run(r.Context(), principal(r), "stageV2Import", key, identity, func(tx *service.Write) (any, int, error) {
			v, e := a.s.StageV2Import(r.Context(), tx, sourceID, raw)
			return v, 201, e
		})
		if e != nil {
			fail(w, e)
			return
		}
		result(w, out)
	}))
	a.get("/migration/v2/imports/{id}", func(r *http.Request) (any, error) { return a.s.V2Import(r.Context(), principal(r), r.PathValue("id")) })
	a.mux.HandleFunc("GET /api/v1/migration/v2/imports/{id}/document", a.auth(func(w http.ResponseWriter, r *http.Request) {
		raw, e := a.s.V2Document(r.Context(), principal(r), r.PathValue("id"))
		if e != nil {
			fail(w, e)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="qimeng-v2-original.json"`)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "private, no-store")
		http.ServeContent(w, r, "qimeng-v2-original.json", time.Time{}, bytes.NewReader(raw))
	}))
	write[service.V2PlanCreate](a, "POST", "/migration/v2/imports/{id}/plans", true, func(r *http.Request, w *service.Write, in service.V2PlanCreate) (any, int, error) {
		v, e := a.s.PrepareV2Migration(r.Context(), w, r.PathValue("id"), in)
		return v, 201, e
	})
	a.get("/migration/v2/plans/{id}", func(r *http.Request) (any, error) {
		return a.s.V2MigrationPlan(r.Context(), principal(r), r.PathValue("id"))
	})
	a.get("/migration/v2/plans/{id}/items", func(r *http.Request) (any, error) {
		n, e := limit(r)
		if e != nil {
			return nil, e
		}
		return a.s.V2MigrationItems(r.Context(), principal(r), r.PathValue("id"), r.URL.Query().Get("cursor"), n)
	})
	a.get("/migration/v2/plans/{id}/authors", func(r *http.Request) (any, error) {
		n, e := limit(r)
		if e != nil {
			return nil, e
		}
		return a.s.V2MigrationAuthors(r.Context(), principal(r), r.PathValue("id"), r.URL.Query().Get("cursor"), n)
	})
	type commit struct {
		Fingerprint   string `json:"fingerprint"`
		AcceptSkipped *bool  `json:"acceptSkipped"`
	}
	write[commit](a, "POST", "/migration/v2/plans/{id}/commit", true, func(r *http.Request, w *service.Write, in commit) (any, int, error) {
		if in.AcceptSkipped == nil {
			return nil, 0, service.Fail(400, "migration_decision_required", "请明确是否接受计划中的跳过项。")
		}
		v, e := a.s.CommitV2Migration(r.Context(), w, r.PathValue("id"), in.Fingerprint, *in.AcceptSkipped)
		return v, 200, e
	})
}
