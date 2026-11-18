package httpapi

import (
	"io"
	"net/http"

	"qimeng.local/next/server/internal/service"
	"qimeng.local/next/server/internal/transport/generated"
)

func (a *API) PublishNativeDelivery(w http.ResponseWriter, r *http.Request, params generated.PublishNativeDeliveryParams) {
	writeScoped[service.NativePublish](a, w, r, "admin", true, func(r *http.Request, tx *service.Write, in service.NativePublish) (any, int, error) {
		v, err := a.s.PublishNativeDelivery(r.Context(), tx, in)
		return v, http.StatusCreated, err
	})
}

func (a *API) StageNativeDelivery(w http.ResponseWriter, r *http.Request, params generated.StageNativeDeliveryParams) {
	if !allowScope(w, r, "admin") {
		return
	}
	if len(params.IdempotencyKey) < 8 || len(params.IdempotencyKey) > 128 {
		fail(w, service.Fail(400, "idempotency_required", "暂存原生包需要有效的请求身份。"))
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, service.NativeDeliveryBytes))
	if err != nil {
		fail(w, service.Fail(413, "native_package_too_large", "原生包超过 2 MiB 段预算或正文读取失败。"))
		return
	}
	// Staging may be replayed after commit. Bind the key transactionally and
	// always read the current business result rather than a stale preview.
	output, err := a.s.Run(r.Context(), principal(r), "stageNativeDelivery", "", nil, func(tx *service.Write) (any, int, error) {
		v, err := a.s.StageNativeDelivery(r.Context(), tx, raw, params.SourceInstanceId, params.TargetInstanceId, params.PackageId, string(params.ConflictPolicy))
		if err == nil {
			err = tx.BindNativeStageRequest(r.Context(), v, params.IdempotencyKey, string(params.ConflictPolicy))
		}
		return v, http.StatusOK, err
	})
	if err != nil {
		fail(w, err)
		return
	}
	result(w, output)
}

func (a *API) GetNativeDelivery(w http.ResponseWriter, r *http.Request, id generated.Id) {
	a.readScoped(w, r, "admin", func(r *http.Request) (any, error) {
		return a.s.NativeDelivery(r.Context(), principal(r), id)
	})
}

func (a *API) CommitNativeDelivery(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.CommitNativeDeliveryParams) {
	writeScoped[generated.NativeDeliveryCommit](a, w, r, "admin", true, func(r *http.Request, tx *service.Write, in generated.NativeDeliveryCommit) (any, int, error) {
		v, err := a.s.CommitNativeDelivery(r.Context(), tx, id, in.Fingerprint)
		return v, http.StatusOK, err
	})
}
