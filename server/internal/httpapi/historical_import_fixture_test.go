// These writers seed representative records from older Next databases. They
// are compiled only in tests. The shipped service exposes historical reads.
package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"mime"
	"net/http"

	legacy "qimeng.local/next/server/internal/migration"
	"qimeng.local/next/server/internal/service"
	"qimeng.local/next/server/internal/transport/generated"
)

type archiveHarness struct{ *API }

func LegacyImportTestHandler(s *service.Service) http.Handler {
	api := &API{s: s}
	return api.routes(&archiveHarness{api})
}

func (a *archiveHarness) StageV2Import(w http.ResponseWriter, r *http.Request, params generated.StageV2ImportParams) {
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

func (a *archiveHarness) PrepareV2Migration(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.PrepareV2MigrationParams) {
	write[service.V2PlanCreate](a.API, w, r, true, func(r *http.Request, w *service.Write, in service.V2PlanCreate) (any, int, error) {
		v, e := a.s.PrepareV2Migration(r.Context(), w, id, in)
		return v, 201, e
	})
}

func (a *archiveHarness) CommitV2Migration(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.CommitV2MigrationParams) {
	type commit struct {
		Fingerprint   string `json:"fingerprint"`
		AcceptSkipped *bool  `json:"acceptSkipped"`
	}
	write[commit](a.API, w, r, true, func(r *http.Request, w *service.Write, in commit) (any, int, error) {
		if in.AcceptSkipped == nil {
			return nil, 0, service.Fail(400, "migration_decision_required", "请明确是否接受计划中的跳过项。")
		}
		v, e := a.s.CommitV2Migration(r.Context(), w, id, in.Fingerprint, *in.AcceptSkipped)
		return v, 200, e
	})
}
