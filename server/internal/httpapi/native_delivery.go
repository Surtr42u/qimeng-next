package httpapi

import (
	"io"
	"net/http"

	"qimeng.local/next/server/internal/service"
	"qimeng.local/next/server/internal/transport/generated"
)

func (a *API) PublishNativeDelivery(w http.ResponseWriter, r *http.Request, params generated.PublishNativeDeliveryParams) {
	nativeVideoMutation(a, w, r, params.IdempotencyKey, http.StatusCreated, func(in service.NativePublish) (*service.NativeVideoProofs, error) {
		return a.s.InspectNativePublish(r.Context(), principal(r), in)
	}, func(tx *service.Write, in service.NativePublish, proofs *service.NativeVideoProofs) (any, error) {
		return a.s.PublishNativeDelivery(r.Context(), tx, in, proofs)
	})
}

func nativeVideoMutation[T any](a *API, w http.ResponseWriter, r *http.Request, key string, status int,
	inspect func(T) (*service.NativeVideoProofs, error), apply func(*service.Write, T, *service.NativeVideoProofs) (any, error)) {
	if !allowScope(w, r, "admin") {
		return
	}
	if len(key) < 8 || len(key) > 128 {
		fail(w, service.Fail(400, "idempotency_required", "原生交付需要有效的请求身份。"))
		return
	}
	input, raw, err := decode[T](w, r)
	if err != nil {
		fail(w, err)
		return
	}
	raw = append(raw, []byte("\x00"+r.URL.Query().Encode())...)
	operation := r.Method + " " + r.URL.Path
	replay, found, err := a.s.ReplayBeforeInspection(r.Context(), principal(r), "admin", operation, key, raw)
	if err != nil {
		fail(w, err)
		return
	}
	if found {
		result(w, replay)
		return
	}
	proofs, inspectionErr := inspect(input)
	defer proofs.Close()
	out, err := a.s.Run(r.Context(), principal(r), operation, key, raw, func(tx *service.Write) (any, int, error) {
		if inspectionErr != nil {
			return nil, 0, inspectionErr
		}
		v, err := apply(tx, input, proofs)
		return v, status, err
	})
	if err != nil {
		fail(w, err)
		return
	}
	result(w, out)
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
	proofs, err := a.s.InspectNativeStage(r.Context(), principal(r), raw, params.SourceInstanceId, params.TargetInstanceId, params.PackageId)
	if err != nil {
		fail(w, err)
		return
	}
	defer proofs.Close()
	// Staging may be replayed after commit. Bind the key transactionally and
	// always read the current business result rather than a stale preview.
	output, err := a.s.Run(r.Context(), principal(r), "stageNativeDelivery", "", nil, func(tx *service.Write) (any, int, error) {
		v, err := a.s.StageNativeDelivery(r.Context(), tx, raw, params.SourceInstanceId, params.TargetInstanceId, params.PackageId, string(params.ConflictPolicy), proofs)
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
	nativeVideoMutation(a, w, r, params.IdempotencyKey, http.StatusOK, func(in generated.NativeDeliveryCommit) (*service.NativeVideoProofs, error) {
		return a.s.InspectNativeCommit(r.Context(), principal(r), id, in.Fingerprint)
	}, func(tx *service.Write, in generated.NativeDeliveryCommit, proofs *service.NativeVideoProofs) (any, error) {
		return a.s.CommitNativeDelivery(r.Context(), tx, id, in.Fingerprint, proofs)
	})
}
