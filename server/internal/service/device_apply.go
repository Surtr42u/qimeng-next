package service

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/store"
)

const deviceScope = "device.apply"

var deviceLabel = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,100}$`)
var deviceHash = regexp.MustCompile(`^[0-9a-f]{64}$`)

type DeviceCredentialInput struct {
	DeviceID            string `json:"deviceId"`
	Label               string `json:"label"`
	CredentialSecret    string `json:"credentialSecret"`
	ReplaceCredentialID string `json:"replaceCredentialId,omitempty"`
}
type DeviceCredential struct {
	ID        string    `json:"id"`
	DeviceID  string    `json:"deviceId"`
	Label     string    `json:"label"`
	ExpiresAt time.Time `json:"expiresAt"`
}
type DeviceApplySegment struct {
	Kind                  string `json:"kind"`
	Bytes                 int64  `json:"bytes"`
	SHA256                string `json:"sha256"`
	ExpectedLocalRevision *int64 `json:"expectedLocalRevision,omitempty"`
	SourceID              string `json:"sourceId,omitempty"`
	PackageID             string `json:"packageId,omitempty"`
	SourceInstanceID      string `json:"sourceInstanceId,omitempty"`
	TargetInstanceID      string `json:"targetInstanceId,omitempty"`
}
type DeviceApplyManifest struct {
	TargetDeviceID  string               `json:"targetDeviceId"`
	ProtocolVersion int                  `json:"protocolVersion"`
	TTLSeconds      int                  `json:"ttlSeconds,omitempty"`
	Segments        []DeviceApplySegment `json:"segments"`
}
type DeviceApplyOutcome struct {
	Kind      string `json:"kind"`
	State     string `json:"state"`
	SHA256    string `json:"sha256,omitempty"`
	ReceiptID string `json:"receiptId,omitempty"`
	Code      string `json:"code,omitempty"`
}
type DeviceApplyReport struct {
	ExpectedRevision int64                `json:"expectedRevision"`
	State            string               `json:"state"`
	Results          []DeviceApplyOutcome `json:"results"`
}
type DeviceApplyTask struct {
	ID              string               `json:"id"`
	TargetDeviceID  string               `json:"targetDeviceId"`
	ProtocolVersion int                  `json:"protocolVersion"`
	State           string               `json:"state"`
	Revision        int64                `json:"revision"`
	Segments        []DeviceApplySegment `json:"segments"`
	CreatedAt       time.Time            `json:"createdAt"`
	ExpiresAt       time.Time            `json:"expiresAt"`
	ClaimedAt       *time.Time           `json:"claimedAt,omitempty"`
	FinishedAt      *time.Time           `json:"finishedAt,omitempty"`
	Results         []DeviceApplyOutcome `json:"results,omitempty"`
}

func (s *Service) DeviceCredential(ctx context.Context, p model.Principal, id string) (DeviceCredential, error) {
	if err := Authorize(p, "admin", ""); err != nil {
		return DeviceCredential{}, err
	}
	v, err := store.Get[DeviceCredential](ctx, s.DB.DB, "SELECT data FROM credentials WHERE json_extract(data,'$.deviceId')=?", id)
	return v, Normalize(err)
}

func (s *Service) CreateDeviceCredential(ctx context.Context, w *Write, in DeviceCredentialInput) (DeviceCredential, error) {
	var result DeviceCredential
	if err := Authorize(w.Principal, "admin", ""); err != nil {
		return result, err
	}
	label := strings.TrimSpace(in.Label)
	if !deviceLabel.MatchString(in.DeviceID) || label == "" || len(label) > 80 || !validExecutorSecret(in.CredentialSecret) ||
		in.CredentialSecret == s.Config.AdminToken || in.CredentialSecret == s.Config.AIToken {
		return result, Fail(400, "invalid_device_credential", "设备标识、名称或客户端生成的独立凭据无效。")
	}
	q, err := w.Q()
	if err != nil {
		return result, err
	}
	var previous string
	err = q.QueryRowContext(ctx, "SELECT id FROM credentials WHERE json_extract(data,'$.deviceId')=?", in.DeviceID).Scan(&previous)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return result, err
	}
	if previous != in.ReplaceCredentialID {
		return result, Fail(409, "device_credential_changed", "设备凭据已变化，请核对后再轮换。")
	}
	var used bool
	if err = q.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM credentials WHERE token_hash=?)", digest([]byte(in.CredentialSecret))).Scan(&used); err != nil {
		return result, err
	}
	if used {
		return result, Fail(409, "device_secret_in_use", "请为设备生成一份新的独立凭据。")
	}
	if previous != "" {
		if _, err = q.ExecContext(ctx, "DELETE FROM credentials WHERE id=?", previous); err != nil {
			return result, err
		}
	}
	p := model.Principal{ID: w.ID("device-credential"), DeviceID: in.DeviceID, Label: label, Scopes: []string{deviceScope}, ExpiresAt: time.Now().UTC().AddDate(1, 0, 0)}
	if err = saveDelegatedCredential(ctx, q, p, in.CredentialSecret); err != nil {
		return result, err
	}
	return DeviceCredential{ID: p.ID, DeviceID: p.DeviceID, Label: label, ExpiresAt: p.ExpiresAt}, nil
}

func allowDevice(p model.Principal, id string, receiver bool) error {
	if !receiver && p.Has("admin") {
		return nil
	}
	if !p.Has("admin") && p.Has(deviceScope) && p.DeviceID != "" && p.DeviceID == id {
		return nil
	}
	return Fail(403, "device_scope_denied", "此操作需要绑定目标设备的专用凭据。")
}
func deviceTask(ctx context.Context, q store.Querier, p model.Principal, id string, receiver bool) (DeviceApplyTask, error) {
	v, err := store.Get[DeviceApplyTask](ctx, q, "SELECT data FROM device_apply_tasks WHERE id=?", id)
	if err != nil {
		return v, Normalize(err)
	}
	if err = allowDevice(p, v.TargetDeviceID, receiver); err != nil {
		return DeviceApplyTask{}, err
	}
	if (v.State == "pending" || v.State == "claimed") && !v.ExpiresAt.After(time.Now()) {
		v.State = "expired"
	}
	return v, nil
}
func (s *Service) GetDeviceApplyTask(ctx context.Context, p model.Principal, id string) (DeviceApplyTask, error) {
	return deviceTask(ctx, s.DB.DB, p, id, false)
}
func (s *Service) DeviceApplyTasks(ctx context.Context, p model.Principal, id, state, cursor string, limit, protocol int) (model.Page[DeviceApplyTask], error) {
	page := model.Page[DeviceApplyTask]{Items: []DeviceApplyTask{}}
	if !deviceLabel.MatchString(id) || protocol < 0 || protocol > 2 || !slices.Contains([]string{"", "pending", "claimed", "applied", "partial", "failed", "cancelled", "expired"}, state) {
		return page, Fail(400, "invalid_device_filter", "设备标识或任务状态无效。")
	}
	if err := allowDevice(p, id, false); err != nil {
		return page, err
	}
	n, err := limited(limit, 50)
	if err != nil {
		return page, err
	}
	filter := "device:" + id + ":" + state
	if protocol != 0 {
		filter += ":v" + strconv.Itoa(protocol)
	}
	seek, err := readSeek(cursor, filter, "created_desc")
	if err != nil {
		return page, err
	}
	now := dateKey(time.Now().UTC())
	items, err := store.List[DeviceApplyTask](ctx, s.DB.DB, `SELECT json_set(data,'$.state',actual) FROM (
	 SELECT id,data,created_at,CASE WHEN state IN ('pending','claimed') AND expires_at<=? THEN 'expired' ELSE state END actual
	 FROM device_apply_tasks WHERE device_id=? AND (?=0 OR json_extract(data,'$.protocolVersion')=?) AND (?='' OR (created_at,id)<(?,?)))
	 WHERE (?='' OR actual=?) ORDER BY created_at DESC,id DESC LIMIT ?`, now, id, protocol, protocol, seek.ID, seek.Value, seek.ID, state, state, n+1)
	if err != nil {
		return page, err
	}
	if len(items) > n {
		last := items[n-1]
		page.NextCursor = makeSeek(filter, "created_desc", dateKey(last.CreatedAt), last.ID)
		items = items[:n]
	}
	page.Items = items
	return page, nil
}
func saveDeviceTask(ctx context.Context, q store.Querier, v DeviceApplyTask) error {
	body, err := store.Encode(v)
	if err != nil {
		return err
	}
	_, err = q.ExecContext(ctx, `INSERT INTO device_apply_tasks(id,device_id,state,created_at,expires_at,data) VALUES(?,?,?,?,?,?)
	 ON CONFLICT(id) DO UPDATE SET state=excluded.state,data=excluded.data`, v.ID, v.TargetDeviceID, v.State, dateKey(v.CreatedAt), dateKey(v.ExpiresAt), body)
	return err
}

// Device claims authorize a subsequent download. Bind the request key and
// still recheck expiration/cancellation on replay instead of returning an old
// "claimed" response after the owner revoked the package.
func (w *Write) BindDeviceRequest(ctx context.Context, id, operation, key string, body []byte) error {
	if len(key) < 8 || len(key) > 128 {
		return Fail(400, "idempotency_required", "请提供有效的领取或回执幂等键。")
	}
	q, err := w.Q()
	if err != nil {
		return err
	}
	hash := digest(body)
	var old string
	err = q.QueryRowContext(ctx, `SELECT input_hash FROM device_apply_requests
	 WHERE task_id=? AND principal_id=? AND operation=? AND request_key=?`, id, w.Principal.ID, operation, key).Scan(&old)
	if err == nil {
		if hash != old {
			return Fail(409, "idempotency_conflict", "此幂等键登记过不同的设备请求。")
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	_, err = q.ExecContext(ctx, `INSERT INTO device_apply_requests(task_id,principal_id,operation,request_key,input_hash)
	 VALUES(?,?,?,?,?)`, id, w.Principal.ID, operation, key, hash)
	return err
}
func (s *Service) ChangeDeviceClaim(ctx context.Context, w *Write, id string, revision int64, cancel bool) (DeviceApplyTask, error) {
	if cancel {
		if err := Authorize(w.Principal, "admin", ""); err != nil {
			return DeviceApplyTask{}, err
		}
	}
	if revision < 1 {
		return DeviceApplyTask{}, Fail(400, "revision_required", "请提供当前任务修订号。")
	}
	q, err := w.Q()
	if err != nil {
		return DeviceApplyTask{}, err
	}
	v, err := deviceTask(ctx, q, w.Principal, id, !cancel)
	if err != nil {
		return v, err
	}
	if !cancel && v.State == "claimed" || cancel && v.State == "cancelled" {
		if revision == v.Revision || revision == v.Revision-1 {
			return v, nil
		}
		return v, Conflict(v.Revision)
	}
	if v.State != "pending" && !(cancel && v.State == "claimed") {
		return v, Fail(409, "device_task_closed", "设备包已结束或到期。")
	}
	if revision != v.Revision {
		return v, Conflict(v.Revision)
	}
	now := time.Now().UTC()
	if cancel {
		v.State = "cancelled"
		v.FinishedAt = &now
		if _, err = q.ExecContext(ctx, "DELETE FROM device_apply_chunks WHERE task_id=?", id); err != nil {
			return v, err
		}
	} else {
		v.State = "claimed"
		v.ClaimedAt = &now
	}
	v.Revision++
	return v, saveDeviceTask(ctx, q, v)
}
func (s *Service) ReportDeviceApply(ctx context.Context, w *Write, id string, in DeviceApplyReport) (DeviceApplyTask, error) {
	if in.ExpectedRevision < 1 {
		return DeviceApplyTask{}, Fail(400, "revision_required", "请提供当前任务修订号。")
	}
	q, err := w.Q()
	if err != nil {
		return DeviceApplyTask{}, err
	}
	v, err := deviceTask(ctx, q, w.Principal, id, true)
	if err != nil {
		return v, err
	}
	if len(in.Results) != len(v.Segments) {
		return v, Fail(400, "device_result_incomplete", "须逐段列出本机处理结果。")
	}
	normalized := make([]DeviceApplyOutcome, 0, len(v.Segments))
	applied, partial := 0, 0
	for _, segment := range v.Segments {
		i := slices.IndexFunc(in.Results, func(x DeviceApplyOutcome) bool { return x.Kind == segment.Kind })
		if i < 0 {
			return v, Fail(400, "device_result_incomplete", "处理结果与签发段不匹配。")
		}
		got := in.Results[i]
		if got.SHA256 != "" && !deviceHash.MatchString(got.SHA256) || got.ReceiptID != "" && !intakeIdentity.MatchString(got.ReceiptID) {
			return v, Fail(400, "invalid_device_result", "段摘要或本机回执身份无效。")
		}
		if got.State == "applied" || got.State == "partial" {
			if got.SHA256 == "" || got.ReceiptID == "" {
				return v, Fail(400, "device_receipt_required", "已应用的段须有本机摘要与实际导入回执身份。")
			}
			if got.SHA256 != segment.SHA256 {
				return v, Fail(409, "device_hash_mismatch", "设备回读的摘要与签发段不匹配。")
			}
			if got.State == "applied" {
				applied++
			} else {
				partial++
			}
		} else if got.State != "failed" && got.State != "skipped" {
			return v, Fail(400, "invalid_device_result", "段处理状态无效。")
		}
		if got.State != "applied" && got.Code == "" || got.Code != "" && !deviceLabel.MatchString(got.Code) {
			return v, Fail(400, "invalid_device_result", "未完成项须提供不含路径的原因码。")
		}
		normalized = append(normalized, got)
	}
	expected := "failed"
	if applied == len(v.Segments) {
		expected = "applied"
	} else if applied+partial > 0 {
		expected = "partial"
	}
	if in.State != expected {
		return v, Fail(409, "device_result_mismatch", "汇总状态与逐段回执不一致。")
	}
	if v.State == in.State && slices.Equal(v.Results, normalized) {
		if in.ExpectedRevision == v.Revision || in.ExpectedRevision == v.Revision-1 {
			return v, nil
		}
		return v, Conflict(v.Revision)
	}
	if v.State != "claimed" {
		return v, Fail(409, "device_task_closed", "任务未领取、已到期或已有不同终态，不能覆盖。")
	}
	if in.ExpectedRevision != v.Revision {
		return v, Conflict(v.Revision)
	}
	now := time.Now().UTC()
	v.State = expected
	v.Results = normalized
	v.FinishedAt = &now
	v.Revision++
	if _, err = q.ExecContext(ctx, "DELETE FROM device_apply_chunks WHERE task_id=?", id); err != nil {
		return v, err
	}
	return v, saveDeviceTask(ctx, q, v)
}
