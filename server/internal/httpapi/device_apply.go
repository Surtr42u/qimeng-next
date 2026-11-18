package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"qimeng.local/next/server/internal/service"
	"qimeng.local/next/server/internal/transport/generated"
)

var devicePackageSlots = make(chan struct{}, 2)

func deviceTransition[T any](a *API, w http.ResponseWriter, r *http.Request, id, action string, apply func(*http.Request, *service.Write, T) (any, error)) {
	if !allowScope(w, r, "device.apply") {
		return
	}
	input, raw, err := decode[T](w, r)
	if err != nil {
		fail(w, err)
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if len(key) < 8 || len(key) > 128 {
		fail(w, service.Fail(400, "idempotency_required", "请提供有效的设备幂等键。"))
		return
	}
	out, err := a.s.Run(r.Context(), principal(r), "deviceTransition", "", nil, func(tx *service.Write) (any, int, error) {
		v, err := apply(r, tx, input)
		if err != nil {
			return nil, 0, err
		}
		return v, 200, tx.BindDeviceRequest(r.Context(), id, action, key, raw)
	})
	if err != nil {
		fail(w, err)
		return
	}
	result(w, out)
}

func (a *API) CreateDeviceCredential(w http.ResponseWriter, r *http.Request, params generated.CreateDeviceCredentialParams) {
	write[service.DeviceCredentialInput](a, w, r, true, func(r *http.Request, w *service.Write, in service.DeviceCredentialInput) (any, int, error) {
		v, err := a.s.CreateDeviceCredential(r.Context(), w, in)
		return v, 201, err
	})
}

func (a *API) GetDeviceCredential(w http.ResponseWriter, r *http.Request, id generated.Id) {
	a.readJSON(w, r, func(r *http.Request) (any, error) {
		return a.s.DeviceCredential(r.Context(), principal(r), id)
	})
}

func (a *API) ListDeviceApplyTasks(w http.ResponseWriter, r *http.Request, params generated.ListDeviceApplyTasksParams) {
	a.readScoped(w, r, "device.apply", func(r *http.Request) (any, error) {
		n, err := limit(r)
		if err != nil {
			return nil, err
		}
		q := r.URL.Query()
		return a.s.DeviceApplyTasks(r.Context(), principal(r), q.Get("targetDeviceId"), q.Get("state"), q.Get("cursor"), n)
	})
}

func (a *API) GetDeviceApplyTask(w http.ResponseWriter, r *http.Request, id generated.Id) {
	a.readScoped(w, r, "device.apply", func(r *http.Request) (any, error) {
		return a.s.GetDeviceApplyTask(r.Context(), principal(r), id)
	})
}

func (a *API) ClaimDeviceApplyTask(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.ClaimDeviceApplyTaskParams) {
	deviceTransition[revisionBody](a, w, r, id, "claim", func(r *http.Request, w *service.Write, in revisionBody) (any, error) {
		return a.s.ChangeDeviceClaim(r.Context(), w, id, in.ExpectedRevision, false)
	})
}

func (a *API) ReportDeviceApplyTask(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.ReportDeviceApplyTaskParams) {
	deviceTransition[service.DeviceApplyReport](a, w, r, id, "result", func(r *http.Request, w *service.Write, in service.DeviceApplyReport) (any, error) {
		return a.s.ReportDeviceApply(r.Context(), w, id, in)
	})
}

func (a *API) CancelDeviceApplyTask(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.CancelDeviceApplyTaskParams) {
	write[revisionBody](a, w, r, true, func(r *http.Request, w *service.Write, in revisionBody) (any, int, error) {
		v, err := a.s.ChangeDeviceClaim(r.Context(), w, id, in.ExpectedRevision, true)
		return v, 200, err
	})
}

func (a *API) GetDeviceApplySegment(w http.ResponseWriter, r *http.Request, id generated.Id, kind generated.DeviceSegmentKind, params generated.GetDeviceApplySegmentParams) {
	if !allowScope(w, r, "device.apply") {
		return
	}
	file, err := a.s.DeviceApplyDownload(r.Context(), principal(r), id, string(kind))
	if err != nil {
		fail(w, err)
		return
	}
	if strings.Contains(r.Header.Get("Range"), ",") {
		fail(w, service.Fail(416, "single_range_required", "每次只接受一个连续字节范围。"))
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("ETag", `"`+file.Segment.SHA256+`"`)
	w.Header().Set("X-Content-SHA256", file.Segment.SHA256)
	http.ServeContent(w, r, file.Segment.Kind, file.CreatedAt, file.Reader)
}

func (a *API) PublishDeviceApplyTask(w http.ResponseWriter, r *http.Request, params generated.PublishDeviceApplyTaskParams) {
	if allowScope(w, r, "admin") {
		a.publishDevicePackage(w, r)
	}
}

func (a *API) publishDevicePackage(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get("Idempotency-Key")
	if len(key) < 8 || len(key) > 128 {
		fail(w, service.Fail(400, "idempotency_required", "请提供 8–128 字符的稳定包发布身份。"))
		return
	}
	select {
	case devicePackageSlots <- struct{}{}:
		defer func() { <-devicePackageSlots }()
	default:
		fail(w, service.Fail(429, "device_package_busy", "已有维护包正在核验。"))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 66<<20)
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		fail(w, service.Fail(400, "invalid_device_multipart", "维护包超过 66 MiB 或 multipart 格式无效。"))
		return
	}
	form := r.MultipartForm
	defer form.RemoveAll()
	if len(form.Value) != 1 || len(form.Value["manifest"]) != 1 || len(form.Value["manifest"][0]) > 256<<10 || len(form.File) > 2 {
		fail(w, service.Fail(400, "invalid_device_manifest", "请提供一个有界的 manifest 及其文件段。"))
		return
	}
	var m service.DeviceApplyManifest
	decoder := json.NewDecoder(strings.NewReader(form.Value["manifest"][0]))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&m); err != nil || decoder.Decode(new(any)) != io.EOF {
		fail(w, service.Fail(400, "invalid_device_manifest", "维护包清单 JSON 无效。"))
		return
	}
	parts := make(map[string]service.DevicePackageFile)
	for kind, headers := range form.File {
		if len(headers) != 1 || (kind != "masterTxt" && kind != "v2Backup") {
			fail(w, service.Fail(400, "invalid_device_segment", "每个文件段只能出现一次。"))
			return
		}
		f, err := headers[0].Open()
		if err != nil {
			fail(w, err)
			return
		}
		defer f.Close()
		parts[kind] = service.DevicePackageFile{Reader: f, Size: headers[0].Size}
	}
	validated, err := service.PrepareDevicePackage(r.Context(), m, parts)
	if err != nil {
		fail(w, err)
		return
	}
	identity, err := json.Marshal(m)
	if err != nil {
		fail(w, err)
		return
	}
	out, err := a.s.Run(r.Context(), principal(r), "publishDeviceApplyTask", key, identity, func(wr *service.Write) (any, int, error) {
		v, err := a.s.PublishDeviceApply(r.Context(), wr, validated)
		return v, 201, err
	})
	if err != nil {
		fail(w, err)
		return
	}
	result(w, out)
}
