package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"time"

	archive "qimeng.local/next/server/internal/backup"
	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/store"
)

type CleanupPermitInput struct {
	InputID      string `json:"inputId"`
	ExpectedHash string `json:"expectedHash"`
	SourceSize   int64  `json:"sourceSize"`
}
type CleanupReceiptInput struct {
	Outcome         string `json:"outcome"`
	Code            string `json:"code,omitempty"`
	ProviderReceipt string `json:"providerReceipt,omitempty"`
}
type CleanupReceipt struct {
	CleanupReceiptInput
	RecordedAt time.Time `json:"recordedAt"`
	Generation int64     `json:"generation"`
}
type CleanupPermit struct {
	ID             string          `json:"id"`
	InstanceID     string          `json:"instanceId"`
	ProtectionID   string          `json:"protectionId"`
	BatchID        string          `json:"batchId"`
	InputID        string          `json:"inputId"`
	AssetID        string          `json:"assetId"`
	ContentVersion int64           `json:"contentVersion"`
	Hash           string          `json:"hash"`
	Size           int64           `json:"size"`
	ProofHash      string          `json:"proofHash"`
	CredentialID   string          `json:"credentialId"`
	Generation     int64           `json:"generation"`
	ExpiresAt      time.Time       `json:"expiresAt"`
	State          string          `json:"state"`
	Receipt        *CleanupReceipt `json:"receipt,omitempty"`
}

type cleanupPermitView struct {
	CleanupPermit
	VerifySnapshot bool `json:"verifySnapshot"`
}

func (p CleanupPermit) withExpiry() CleanupPermit {
	if p.State == "issued" && !p.ExpiresAt.After(time.Now()) {
		p.State = "expired"
	}
	return p
}

func (s *Service) legacyPermitMembership(ctx context.Context, principal model.Principal, p CleanupPermit, cache map[string]legacySnapshotResult) (bool, error) {
	protection, err := s.BatchProtection(ctx, principal, p.BatchID, p.ProtectionID)
	if err != nil {
		return false, err
	}
	item, err := protectedInput(protection, p.InputID)
	if err != nil || protection.State != "completed" || protection.InstanceID != s.InstanceID || protection.InstanceID != p.InstanceID || protection.ProofHash != p.ProofHash ||
		item.AssetID != p.AssetID || item.ContentVersion != p.ContentVersion || item.Hash != p.Hash || item.Size != p.Size {
		return false, nil
	}
	files := protectionFiles{service: s, proof: protection}
	defer files.close()
	if err = files.verifyLegacyProtectionItems(ctx, []ProtectedInput{item}, cache); err != nil {
		var app *Error
		if errors.As(err, &app) && (app.Code == "input_version_unverified" || app.Code == "manifest_changed") {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

const cleanupPermitProjection = `SELECT CASE WHEN p.state='issued' AND NOT EXISTS(
 SELECT 1 FROM batch_inputs i JOIN uploads u ON u.id=i.upload_id JOIN assets a ON a.id=json_extract(u.data,'$.assetId')
 WHERE i.batch_id=p.batch_id AND i.input_id=p.input_id AND u.batch_id=i.batch_id AND u.state='ingested' AND a.trashed=0
 AND a.id=json_extract(p.data,'$.assetId') AND a.hash=json_extract(p.data,'$.hash') AND a.size=json_extract(p.data,'$.size')
 AND json_extract(u.data,'$.publishedContentVersion')=json_extract(p.data,'$.contentVersion')
 AND json_extract(a.data,'$.contentVersion')=json_extract(p.data,'$.contentVersion'))
 THEN json_set(p.data,'$.state','revoked')
 WHEN p.state='issued' AND NOT EXISTS(SELECT 1 FROM batch_protections b WHERE b.id=p.protection_id AND json_extract(b.data,'$.snapshotInputsVerified')=1)
 THEN json_set(p.data,'$.verifySnapshot',json('true')) ELSE p.data END FROM source_cleanup_permits p `

func (s *Service) permitViews(ctx context.Context, principal model.Principal, batchID string, records []cleanupPermitView) ([]CleanupPermit, error) {
	type check struct {
		covered bool
		err     error
	}
	permits := make([]CleanupPermit, len(records))
	checks := make(map[string]check)
	snapshots := make(map[string]legacySnapshotResult)
	for i, record := range records {
		p := record.CleanupPermit.withExpiry()
		permits[i] = p
		if p.State == "issued" && record.VerifySnapshot {
			covered, err := s.legacyPermitMembership(ctx, principal, p, snapshots)
			if cancelled := ctx.Err(); cancelled != nil {
				return nil, cancelled
			}
			checks[p.ID] = check{covered, err}
		}
	}
	if len(checks) == 0 {
		return permits, nil
	}
	ids := make([]string, len(permits))
	for i, p := range permits {
		ids[i] = p.ID
	}
	encoded, err := store.Encode(ids)
	if err != nil {
		return nil, err
	}
	// A source outcome, asset replacement or lease change can commit while
	// waiting for the snapshot. Refresh the selected IDs after all snapshot I/O,
	// in the same ID order as the original page.
	s.mu.Lock()
	defer s.mu.Unlock()
	current, err := s.currentWritePrincipal(ctx, principal)
	if err != nil {
		return nil, err
	}
	if err = Authorize(current, "read", batchID); err != nil {
		return nil, err
	}
	if err = Authorize(current, "source.cleanup", batchID); err != nil {
		return nil, err
	}
	fresh, err := store.List[cleanupPermitView](ctx, s.DB.DB, cleanupPermitProjection+"WHERE p.batch_id=? AND p.principal_id=? AND p.id IN (SELECT value FROM json_each(?)) ORDER BY p.id", batchID, current.ID, encoded)
	if err != nil {
		return nil, err
	}
	if len(fresh) != len(ids) {
		return nil, Normalize(sql.ErrNoRows)
	}
	for i, record := range fresh {
		p := record.CleanupPermit.withExpiry()
		if p.State == "issued" && record.VerifySnapshot {
			result, checked := checks[p.ID]
			if !checked {
				// An input that became available again during this call can be
				// verified on the next read. Its initial view was not actionable.
				continue
			}
			if result.err != nil {
				return nil, result.err
			}
			if !result.covered {
				p.State = "revoked"
			}
		}
		permits[i] = p
	}
	return permits, nil
}

func (s *Service) CleanupPermit(ctx context.Context, p model.Principal, id string) (CleanupPermit, error) {
	v, e := store.Get[cleanupPermitView](ctx, s.DB.DB, cleanupPermitProjection+"WHERE p.id=?", id)
	if e != nil {
		return v.CleanupPermit, Normalize(e)
	}
	if e = Authorize(p, "source.cleanup", v.BatchID); e != nil {
		return v.CleanupPermit, e
	}
	if v.CredentialID != p.ID {
		return v.CleanupPermit, Fail(403, "cleanup_credential_mismatch", "此许可不属于当前可信执行器。")
	}
	if !v.VerifySnapshot {
		return v.CleanupPermit.withExpiry(), nil
	}
	values, e := s.permitViews(ctx, p, v.BatchID, []cleanupPermitView{v})
	if e != nil {
		return v.CleanupPermit, e
	}
	return values[0], nil
}
func (s *Service) CleanupPermits(ctx context.Context, p model.Principal, batchID, inputID, cursor string, limit int) (model.Page[CleanupPermit], error) {
	result := model.Page[CleanupPermit]{Items: []CleanupPermit{}}
	if e := Authorize(p, "source.cleanup", batchID); e != nil {
		return result, e
	}
	n, e := limited(limit, 20)
	if e != nil {
		return result, e
	}
	filter := "cleanup:" + batchID + ":" + inputID + ":" + p.ID
	seek, e := readSeek(cursor, filter, "id")
	if e != nil {
		return result, e
	}
	items, e := store.List[cleanupPermitView](ctx, s.DB.DB, cleanupPermitProjection+"WHERE p.batch_id=? AND p.principal_id=? AND (?='' OR p.input_id=?) AND p.id>? ORDER BY p.id LIMIT ?", batchID, p.ID, inputID, inputID, seek.ID, n+1)
	if e != nil {
		return result, e
	}
	if len(items) > n {
		result.NextCursor = makeSeek(filter, "id", "", items[n-1].ID)
		items = items[:n]
	}
	result.Items, e = s.permitViews(ctx, p, batchID, items)
	return result, e
}
func protectedInput(p BatchProtection, inputID string) (ProtectedInput, error) {
	for _, item := range p.Items {
		if item.InputID == inputID {
			if item.State != "protected" {
				return item, Fail(409, "input_not_protected", "此原始输入尚无完整独立保护，来源必须保留。")
			}
			return item, nil
		}
	}
	return ProtectedInput{}, Fail(404, "input_not_found", "输入不属于本保护清单。")
}
func (s *Service) verifyProtectionForCleanup(ctx context.Context, p BatchProtection, item ProtectedInput) error {
	if e := s.checkPublishedInput(ctx, p.BatchID, item); e != nil {
		return e
	}
	if p.InstanceID != s.InstanceID || p.State != "completed" || p.ProofHash == "" {
		return Fail(409, "protection_not_ready", "当前实例尚无可用于清理的完整保护证明。")
	}
	files := protectionFiles{service: s, proof: p}
	defer files.close()
	if !p.snapshotInputsVerified {
		if err := files.verifyLegacyProtectionItems(ctx, []ProtectedInput{item}, nil); err != nil {
			return err
		}
	}
	root, e := files.openTarget(ctx)
	if e != nil {
		return e
	}
	evidence, e := protectionEvidence(p)
	if e != nil {
		return e
	}
	if digest(evidence) != p.ProofHash {
		return Fail(409, "protection_changed", "保护证明内容不一致。")
	}
	if e = verifyTargetObject(ctx, root, protectionReceipt(p), p.ProofHash, int64(len(evidence))); e != nil {
		return e
	}
	if !files.targetSnapshotVerified {
		if e = verifyTargetObject(ctx, root, protectionSnapshot(p), p.SnapshotHash, p.SnapshotBytes); e != nil {
			return e
		}
	}
	if e = verifyTargetObject(ctx, root, protectionObject(item.Hash), item.Hash, item.Size); e != nil {
		return e
	}
	source, e := s.backupSource(ctx, archive.File{Kind: "asset", ObjectID: item.AssetID, ContentVersion: item.ContentVersion, SHA256: item.Hash, Size: item.Size})
	if e != nil {
		return e
	}
	defer source.Close()
	hash := sha256.New()
	buffer := make([]byte, 128<<10)
	for {
		if e = ctx.Err(); e != nil {
			return e
		}
		n, readErr := source.Read(buffer)
		if n > 0 {
			if _, e = hash.Write(buffer[:n]); e != nil {
				return e
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	if hex.EncodeToString(hash.Sum(nil)) != item.Hash {
		return Fail(409, "original_changed", "当前入库原件发生变化，未发放来源回收许可。")
	}
	return nil
}
func (s *Service) IssueCleanupPermit(ctx context.Context, p model.Principal, batchID, protectionID, key string, input []byte, in CleanupPermitInput) (Result, error) {
	if e := Authorize(p, "source.cleanup", batchID); e != nil {
		return Result{}, e
	}
	if len(key) < 8 || len(key) > 128 {
		return Result{}, Fail(400, "idempotency_required", "来源回收许可必须有稳定幂等键。")
	}
	operation := "POST /api/v1/batches/" + batchID + "/protections/" + protectionID + "/cleanup-permits"
	var previous string
	var receipt Result
	e := s.DB.DB.QueryRowContext(ctx, "SELECT input_hash,response,status FROM idempotency WHERE principal_id=? AND operation=? AND key=?", p.ID, operation, key).Scan(&previous, &receipt.Body, &receipt.Status)
	if e == nil {
		if previous != digest(input) {
			return Result{}, Fail(409, "idempotency_conflict", "许可请求不能用同一键更换输入。")
		}
		saved, e := s.Run(ctx, p, operation, key, input, func(*Write) (any, int, error) {
			return nil, 0, Fail(409, "cleanup_receipt_missing", "许可回执需要重新回读。")
		})
		if e != nil {
			return Result{}, e
		}
		var old CleanupPermit
		if e = json.Unmarshal(saved.Body, &old); e != nil {
			return Result{}, e
		}
		current, e := s.CleanupPermit(ctx, p, old.ID)
		if e != nil {
			return Result{}, e
		}
		saved.Body, e = json.Marshal(current)
		return saved, e
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return Result{}, e
	}
	protection, e := s.BatchProtection(ctx, p, batchID, protectionID)
	if e != nil {
		return Result{}, e
	}
	item, e := protectedInput(protection, in.InputID)
	if e != nil {
		return Result{}, e
	}
	if in.ExpectedHash != item.Hash || in.SourceSize != item.Size {
		return Result{}, Fail(409, "source_changed", "来源与原始保护内容不一致，未签发回收许可。")
	}
	if e = s.verifyProtectionForCleanup(ctx, protection, item); e != nil {
		return Result{}, e
	}
	return s.Run(ctx, p, operation, key, input, func(w *Write) (any, int, error) {
		if e := Authorize(w.Principal, "source.cleanup", batchID); e != nil {
			return nil, 0, e
		}
		if _, e := s.protectionTarget(ctx, protection); e != nil {
			return nil, 0, e
		}
		batch, e := s.Batch(ctx, batchID)
		if e != nil {
			return nil, 0, e
		}
		if batch.State == "cancelled" || batch.ManifestState != "sealed" || batch.ManifestFingerprint != protection.ManifestFingerprint {
			return nil, 0, Fail(409, "manifest_changed", "当前批次与受保护的原始清单不一致。")
		}
		asset, e := s.Asset(ctx, item.AssetID)
		if e != nil {
			return nil, 0, e
		}
		if asset.Trashed || asset.Hash != item.Hash || asset.ContentVersion != item.ContentVersion {
			return nil, 0, Fail(409, "input_version_changed", "入库版本已变化，来源应保留。")
		}
		var pending, attempts, recycled int
		if e = s.DB.DB.QueryRowContext(ctx, "SELECT count(*),coalesce(sum(state='issued'),0) FROM source_cleanup_permits WHERE batch_id=? AND input_id=?", batchID, item.InputID).Scan(&attempts, &pending); e != nil {
			return nil, 0, e
		}
		if pending > 0 {
			return nil, 0, Fail(409, "cleanup_outcome_pending", "该输入已有未决许可，请回读并登记实际去向，不得再发起来源副作用。")
		}
		if attempts >= 20 {
			return nil, 0, Fail(409, "cleanup_attempt_limit", "此输入的回收尝试已达上限，来源保持不变。")
		}
		if e = s.DB.DB.QueryRowContext(ctx, "SELECT count(*) FROM source_cleanup_permits p JOIN source_cleanup_receipts r ON r.permit_id=p.id WHERE p.batch_id=? AND p.input_id=? AND json_extract(r.data,'$.outcome')='recycled'", batchID, item.InputID).Scan(&recycled); e != nil {
			return nil, 0, e
		}
		if recycled > 0 {
			return nil, 0, Fail(409, "source_already_recycled", "该原始输入已有回收事实，不再授权第二次副作用。")
		}
		expires := time.Now().UTC().Add(20 * time.Minute)
		if w.Principal.ExpiresAt.Before(expires) {
			expires = w.Principal.ExpiresAt
		}
		permit := CleanupPermit{ID: w.ID("source-cleanup"), InstanceID: s.InstanceID, ProtectionID: protection.ID, BatchID: batchID, InputID: item.InputID, AssetID: item.AssetID, ContentVersion: item.ContentVersion, Hash: item.Hash, Size: item.Size, ProofHash: protection.ProofHash, CredentialID: w.Principal.ID, Generation: w.Principal.Generation, ExpiresAt: expires, State: "issued"}
		raw, e := store.Encode(permit)
		if e != nil {
			return nil, 0, e
		}
		q, e := w.Q()
		if e != nil {
			return nil, 0, e
		}
		_, e = q.ExecContext(ctx, "INSERT INTO source_cleanup_permits(id,protection_id,batch_id,input_id,principal_id,state,expires_at,data) VALUES(?,?,?,?,?,?,?,?)", permit.ID, permit.ProtectionID, permit.BatchID, permit.InputID, permit.CredentialID, permit.State, dateKey(expires), raw)
		return permit, 201, e
	})
}

var cleanupCode = regexp.MustCompile(`^[a-z0-9_]{0,80}$`)
var providerReceiptID = regexp.MustCompile(`^[A-Za-z0-9_-]{8,256}$`)

func (s *Service) RecordCleanupReceipt(ctx context.Context, w *Write, id string, in CleanupReceiptInput) (CleanupPermit, error) {
	permit, e := store.Get[CleanupPermit](ctx, s.DB.DB, "SELECT data FROM source_cleanup_permits WHERE id=?", id)
	if e != nil {
		return permit, Normalize(e)
	}
	if e = Authorize(w.Principal, "source.cleanup", permit.BatchID); e != nil {
		return permit, e
	}
	if permit.InstanceID != s.InstanceID || permit.CredentialID != w.Principal.ID || w.Principal.Generation < permit.Generation {
		return permit, Fail(403, "cleanup_credential_mismatch", "实际去向只能由同一执行器的当前租约回报。")
	}
	if !cleanupCode.MatchString(in.Code) || in.ProviderReceipt != "" && !providerReceiptID.MatchString(in.ProviderReceipt) {
		return permit, Fail(400, "invalid_cleanup_receipt", "回执只能包含有界错误码与不含路径的提供方标识。")
	}
	switch in.Outcome {
	case "recycled":
		if in.ProviderReceipt == "" {
			return permit, Fail(400, "provider_receipt_required", "回收成功须有可信来源提供方回执。")
		}
	case "retained", "unavailable", "source_changed", "failed":
	default:
		return permit, Fail(400, "invalid_cleanup_outcome", "来源处理结果无效。")
	}
	if permit.State == "recorded" {
		if permit.Receipt != nil && permit.Receipt.CleanupReceiptInput == in {
			return permit, nil
		}
		return permit, Fail(409, "cleanup_receipt_conflict", "来源去向已有不同事实，不能覆盖。")
	}
	if permit.State != "issued" {
		return permit, Fail(409, "cleanup_permit_closed", "此许可已经关闭。")
	}
	// Receipt recovery may happen after expiry or after claiming a newer lease.
	// It records an already completed action and never grants permission to start.
	now := time.Now().UTC()
	receipt := CleanupReceipt{CleanupReceiptInput: in, RecordedAt: now, Generation: w.Principal.Generation}
	permit.State = "recorded"
	permit.Receipt = &receipt
	q, e := w.Q()
	if e != nil {
		return permit, e
	}
	raw, e := store.Encode(receipt)
	if e != nil {
		return permit, e
	}
	if _, e = q.ExecContext(ctx, "INSERT INTO source_cleanup_receipts(permit_id,data) VALUES(?,?)", id, raw); e != nil {
		return permit, e
	}
	raw, e = store.Encode(permit)
	if e != nil {
		return permit, e
	}
	if _, e = q.ExecContext(ctx, "UPDATE source_cleanup_permits SET state='recorded',data=? WHERE id=?", raw, id); e != nil {
		return permit, e
	}
	batch, e := store.Get[model.Batch](ctx, q, "SELECT data FROM batches WHERE id=?", permit.BatchID)
	if e != nil {
		return permit, e
	}
	var recycled int
	if e = q.QueryRowContext(ctx, "SELECT count(DISTINCT p.input_id) FROM source_cleanup_permits p JOIN source_cleanup_receipts r ON r.permit_id=p.id WHERE p.batch_id=? AND json_extract(r.data,'$.outcome')='recycled'", permit.BatchID).Scan(&recycled); e != nil {
		return permit, e
	}
	batch.CleanupState = "retained"
	if recycled > 0 {
		batch.CleanupState = "partially_recycled"
	}
	if batch.ManifestState == "sealed" && recycled == batch.ExpectedInputs {
		batch.CleanupState = "recycled"
	}
	batch.Revision++
	batch.UpdatedAt = now
	return permit, store.SaveBatch(ctx, q, batch)
}
