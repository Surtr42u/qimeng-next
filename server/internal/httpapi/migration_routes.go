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
	"qimeng.local/next/server/internal/transport/generated"
)

func (a *API) ListV2Imports(w http.ResponseWriter, r *http.Request, params generated.ListV2ImportsParams) {
	a.readJSON(w, r, func(r *http.Request) (any, error) {
		n, e := limit(r)
		if e != nil {
			return nil, e
		}
		return a.s.V2Imports(r.Context(), principal(r), r.URL.Query().Get("cursor"), n)
	})
}

func (a *API) StageV2Import(w http.ResponseWriter, r *http.Request, params generated.StageV2ImportParams) {
	if !allowScope(w, r, "read") {
		return
	}
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
}

func (a *API) GetV2Import(w http.ResponseWriter, r *http.Request, id generated.Id) {
	a.readJSON(w, r, func(r *http.Request) (any, error) { return a.s.V2Import(r.Context(), principal(r), id) })
}

func (a *API) DownloadV2Import(w http.ResponseWriter, r *http.Request, id generated.Id) {
	if !allowScope(w, r, "read") {
		return
	}
	raw, e := a.s.V2Document(r.Context(), principal(r), id)
	if e != nil {
		fail(w, e)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="qimeng-v2-original.json"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-store")
	http.ServeContent(w, r, "qimeng-v2-original.json", time.Time{}, bytes.NewReader(raw))
}

func (a *API) PrepareV2Migration(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.PrepareV2MigrationParams) {
	write[service.V2PlanCreate](a, w, r, true, func(r *http.Request, w *service.Write, in service.V2PlanCreate) (any, int, error) {
		v, e := a.s.PrepareV2Migration(r.Context(), w, id, in)
		return v, 201, e
	})
}

func (a *API) GetV2MigrationPlan(w http.ResponseWriter, r *http.Request, id generated.Id) {
	a.readJSON(w, r, func(r *http.Request) (any, error) {
		return a.s.V2MigrationPlan(r.Context(), principal(r), id)
	})
}

func (a *API) ListV2MigrationItems(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.ListV2MigrationItemsParams) {
	a.readJSON(w, r, func(r *http.Request) (any, error) {
		n, e := limit(r)
		if e != nil {
			return nil, e
		}
		return a.s.V2MigrationItems(r.Context(), principal(r), id, r.URL.Query().Get("cursor"), n)
	})
}

func (a *API) ListV2MigrationAuthors(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.ListV2MigrationAuthorsParams) {
	a.readJSON(w, r, func(r *http.Request) (any, error) {
		n, e := limit(r)
		if e != nil {
			return nil, e
		}
		return a.s.V2MigrationAuthors(r.Context(), principal(r), id, r.URL.Query().Get("cursor"), n)
	})
}

func (a *API) CommitV2Migration(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.CommitV2MigrationParams) {
	type commit struct {
		Fingerprint   string `json:"fingerprint"`
		AcceptSkipped *bool  `json:"acceptSkipped"`
	}
	write[commit](a, w, r, true, func(r *http.Request, w *service.Write, in commit) (any, int, error) {
		if in.AcceptSkipped == nil {
			return nil, 0, service.Fail(400, "migration_decision_required", "请明确是否接受计划中的跳过项。")
		}
		v, e := a.s.CommitV2Migration(r.Context(), w, id, in.Fingerprint, *in.AcceptSkipped)
		return v, 200, e
	})
}
