package httpapi_test

import (
	"net/url"
	"strings"
	"testing"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/service"
)

func TestSystemReadingsAndTypedDiagnosticsRequireOwner(t *testing.T) {
	f := setup(t)
	readonly := f.s.Config.AIToken
	if readonly == "" {
		readonly = "synthetic-readonly-monitoring-credential"
		f.s.Config.AIToken = readonly
	}
	for _, path := range []string{"/system/status", "/system/storage", "/system/client-errors", "/maintenance/jobs"} {
		if reply := f.request("GET", path, "", readonly, nil, nil); reply.Code != 403 {
			t.Fatalf("non-owner read %s: %d", path, reply.Code)
		}
	}
	f.json("POST", "/system/client-errors", "", map[string]any{"code": "private source path", "screen": "detail", "level": "error"}, 400)
	f.json("POST", "/system/client-errors", "", map[string]any{"code": "playback_failed", "screen": "detail", "level": "error", "message": "private fixture"}, 400)
	f.json("POST", "/system/client-errors", "", map[string]any{"code": "playback_failed", "screen": "detail", "level": "error", "httpStatus": 415}, 201)
	var state service.SystemStatus
	decode(t, f.json("GET", "/system/status", "", nil, 200), &state)
	if state.InstanceID != f.s.InstanceID || state.StartedAt.IsZero() || state.SampledAt.Before(state.StartedAt) || state.ClientErrorCount != 1 || len(state.Mounts) != 2 {
		t.Fatal("system overview disagrees with mounted catalog and diagnostic count")
	}
	if !state.Mounts[0].Available || state.Mounts[0].Free == nil || state.Mounts[0].Total == nil || *state.Mounts[0].Total < *state.Mounts[0].Free {
		t.Fatal("missing real data-volume capacity on the CI runner")
	}
	var errors model.Page[service.ClientErrorItem]
	decode(t, f.json("GET", "/system/client-errors", "", nil, 200), &errors)
	if len(errors.Items) != 1 || errors.Items[0].Message != "播放器无法播放" || strings.Contains(errors.Items[0].Message, "private") {
		t.Fatal("raw client details escaped the diagnostic boundary")
	}
}
func TestMaintenancePagesPreserveTaskKindAndStateAcrossWorkflows(t *testing.T) {
	f := setup(t)
	f.scan("monitor-scanned")
	f.json("POST", "/batches", "monitor-new-batch", map[string]any{"libraryId": f.library, "mode": "ordinary", "label": "Synthetic task"}, 201)
	var page model.Page[service.MaintenanceJob]
	decode(t, f.json("GET", "/maintenance/jobs?limit=1", "", nil, 200), &page)
	if len(page.Items) != 1 || page.Items[0].Kind != "batch" || !page.Items[0].Cancellable || page.NextCursor == "" {
		t.Fatal("new batch disappeared from unified task queue")
	}
	next := page.NextCursor
	page = model.Page[service.MaintenanceJob]{}
	decode(t, f.json("GET", "/maintenance/jobs?limit=1&cursor="+url.QueryEscape(next), "", nil, 200), &page)
	if len(page.Items) != 1 || page.Items[0].Kind != "scan" || page.Items[0].State != "completed" || page.Items[0].Cancellable {
		t.Fatal("terminal scan lost on continuation")
	}
	f.json("GET", "/maintenance/jobs?kind=author&cursor="+url.QueryEscape(next), "", nil, 400)
}
