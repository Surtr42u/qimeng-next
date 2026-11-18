package service

import (
	"context"
	"runtime"
	"runtime/debug"
	"time"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/store"
	"qimeng.local/next/server/internal/sysmon"
)

type SystemStatus struct {
	sysmon.Host
	InstanceID       string         `json:"instanceId"`
	Version          string         `json:"version"`
	GoVersion        string         `json:"goVersion"`
	StartedAt        time.Time      `json:"startedAt"`
	SampledAt        time.Time      `json:"sampledAt"`
	UptimeSeconds    int64          `json:"uptimeSeconds"`
	ReceivedBytes    uint64         `json:"receivedBytes"`
	SentBytes        uint64         `json:"sentBytes"`
	Mounts           []sysmon.Mount `json:"mounts"`
	NextMountCursor  string         `json:"nextMountCursor,omitempty"`
	TrashCount       int64          `json:"trashCount"`
	ClientErrorCount int64          `json:"clientErrorCount"`
}
type systemSamples struct {
	gate    chan struct{}
	started time.Time
	version string
	host    sysmon.Sampler
	cached  SystemStatus
}

func (s *systemSamples) init() {
	s.gate = make(chan struct{}, 1)
	s.started = time.Now()
	s.version = "0.1.0"
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" && len(setting.Value) >= 12 {
				s.version += "+" + setting.Value[:12]
				break
			}
		}
	}
}

// CheckReadiness reports whether the catalog serving current requests is reachable.
func (s *Service) CheckReadiness(ctx context.Context) error {
	return s.DB.DB.PingContext(ctx)
}

func (s *Service) SystemStatus(ctx context.Context, p model.Principal) (SystemStatus, error) {
	if err := Authorize(p, "admin", ""); err != nil {
		return SystemStatus{}, err
	}
	samples := &s.samples
	select {
	case samples.gate <- struct{}{}:
		defer func() { <-samples.gate }()
	case <-ctx.Done():
		return SystemStatus{}, ctx.Err()
	}
	now := time.Now()
	if !samples.cached.SampledAt.IsZero() && now.Sub(samples.cached.SampledAt) < 2*time.Second {
		return samples.cached, nil
	}
	mounts, err := s.systemStorage(ctx, "", 128)
	if err != nil {
		return SystemStatus{}, err
	}
	state := SystemStatus{InstanceID: s.InstanceID, Version: samples.version, GoVersion: runtime.Version(), StartedAt: samples.started.UTC(),
		UptimeSeconds: int64(now.Sub(samples.started) / time.Second), Mounts: mounts.Items, NextMountCursor: mounts.NextCursor}
	if err = s.DB.DB.QueryRowContext(ctx, "SELECT (SELECT count(*) FROM assets WHERE trashed=1),(SELECT count(*) FROM client_diagnostics)").Scan(&state.TrashCount, &state.ClientErrorCount); err != nil {
		return state, err
	}
	state.Host = samples.host.Host(ctx)
	if err = ctx.Err(); err != nil {
		return state, err
	}
	state.ReceivedBytes, state.SentBytes, _ = s.Traffic.Counts()
	state.SampledAt = time.Now().UTC()
	samples.cached = state
	return state, nil
}
func (s *Service) SystemStorage(ctx context.Context, p model.Principal, cursor string, n int) (model.Page[sysmon.Mount], error) {
	if err := Authorize(p, "admin", ""); err != nil {
		return model.Page[sysmon.Mount]{}, err
	}
	n, err := limited(n, 50)
	if err != nil {
		return model.Page[sysmon.Mount]{}, err
	}
	return s.systemStorage(ctx, cursor, n)
}
func (s *Service) systemStorage(ctx context.Context, cursor string, n int) (model.Page[sysmon.Mount], error) {
	page := model.Page[sysmon.Mount]{Items: []sysmon.Mount{}}
	key, err := readSeek(cursor, "system-storage", "id")
	if err != nil {
		return page, err
	}
	type folder struct{ ID, Name string }
	// The control directory has a dedicated key before all library keys. Fixed
	// keys survive inserts and renames between pages. Mounts can share a volume.
	rows, err := s.DB.DB.QueryContext(ctx, `SELECT key,name FROM (SELECT '0' key,'业务数据' name UNION ALL SELECT '1'||id,name FROM libraries)
	WHERE key>? ORDER BY key LIMIT ?`, key.ID, n+1)
	if err != nil {
		return page, err
	}
	input := make([]folder, 0, n+1)
	for rows.Next() {
		var f folder
		if err = rows.Scan(&f.ID, &f.Name); err != nil {
			rows.Close()
			return page, err
		}
		input = append(input, f)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return page, err
	}
	if len(input) > n {
		input = input[:n]
		page.NextCursor = makeSeek("system-storage", "id", "", input[n-1].ID)
	}
	for _, f := range input {
		if err = ctx.Err(); err != nil {
			return page, err
		}
		id, root := "data", s.Config.DataDir
		if f.ID != "0" {
			id = f.ID[1:]
			root, err = s.Root.LibraryPath(id)
			if err != nil {
				page.Items = append(page.Items, sysmon.Mount{ID: id, Label: f.Name})
				continue
			}
		}
		page.Items = append(page.Items, sysmon.ReadMount(ctx, id, f.Name, root))
	}
	return page, nil
}

type ClientErrorReport struct {
	Level      string `json:"level"`
	Code       string `json:"code"`
	Screen     string `json:"screen"`
	HTTPStatus *int   `json:"httpStatus,omitempty"`
}
type ClientErrorItem struct {
	ClientErrorReport
	ID         string    `json:"id"`
	OccurredAt time.Time `json:"occurredAt"`
	Message    string    `json:"message"`
}

var clientErrorMessages = map[string]string{"request_failed": "请求失败", "render_failed": "界面显示失败", "playback_failed": "播放器无法播放", "upload_failed": "上传失败", "storage_unavailable": "存储不可用", "connection_lost": "与影库断开连接"}

func (s *Service) RecordClientError(ctx context.Context, w *Write, report ClientErrorReport) (ClientErrorItem, error) {
	var item ClientErrorItem
	if err := Authorize(w.Principal, "admin", ""); err != nil {
		return item, err
	}
	message, ok := clientErrorMessages[report.Code]
	if !ok || report.Level != "warning" && report.Level != "error" || !containsString([]string{"home", "album", "search", "detail", "upload", "author", "settings", "maintenance", "backup", "other"}, report.Screen) || report.HTTPStatus != nil && (*report.HTTPStatus < 400 || *report.HTTPStatus > 599) {
		return item, Fail(400, "invalid_client_diagnostic", "请提交预定义的异常类别、等级和页面。")
	}
	item = ClientErrorItem{report, w.ID("client-error"), time.Now().UTC(), message}
	raw, err := store.Encode(item)
	if err != nil {
		return item, err
	}
	q, err := w.Q()
	if err != nil {
		return item, err
	}
	if _, err = q.ExecContext(ctx, "INSERT INTO client_diagnostics(id,data) VALUES(?,?)", item.ID, raw); err != nil {
		return item, err
	}
	_, err = q.ExecContext(ctx, "DELETE FROM client_diagnostics WHERE seq<=(SELECT seq FROM client_diagnostics ORDER BY seq DESC LIMIT 1 OFFSET 200)")
	return item, err
}
func (s *Service) ClientErrors(ctx context.Context, p model.Principal, cursor string, n int) (model.Page[ClientErrorItem], error) {
	var page model.Page[ClientErrorItem]
	if err := Authorize(p, "admin", ""); err != nil {
		return page, err
	}
	n, err := limited(n, 50)
	if err != nil {
		return page, err
	}
	key, err := readSeek(cursor, "client-errors", "time")
	if err != nil {
		return page, err
	}
	items, err := store.List[ClientErrorItem](ctx, s.DB.DB, `SELECT data FROM client_diagnostics WHERE ?='' OR qimeng_time_key(json_extract(data,'$.occurredAt'))<? OR
	 (qimeng_time_key(json_extract(data,'$.occurredAt'))=? AND id>?) ORDER BY qimeng_time_key(json_extract(data,'$.occurredAt')) DESC,id LIMIT ?`, key.ID, key.Value, key.Value, key.ID, n+1)
	if err != nil {
		return page, err
	}
	if len(items) > n {
		items = items[:n]
		last := items[n-1]
		page.NextCursor = makeSeek("client-errors", "time", dateKey(last.OccurredAt), last.ID)
	}
	page.Items = items
	return page, nil
}
