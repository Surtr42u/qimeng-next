package service

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"qimeng.local/next/server/internal/domain"
	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/store"
)

var intakeIdentity = regexp.MustCompile(`^[A-Za-z0-9_-]{8,128}$`)

func normalizeIntakeInput(in model.IntakeInput) (model.IntakeInput, error) {
	if in.NameState == "" {
		in.NameState = "known"
	}
	if in.NameState != "known" && in.NameState != "unavailable" || in.NameState == "known" && in.OriginalFileName == "" || in.NameState == "unavailable" && in.OriginalFileName != "" {
		return in, Fail(400, "invalid_source_name_state", "已知原名须真实提供；读取失败则省略原名并明确标记unavailable。")
	}
	if !intakeIdentity.MatchString(in.ID) || len(in.OriginalFileName) > 1024 || !utf8.ValidString(in.OriginalFileName) || strings.ContainsAny(in.OriginalFileName, "/\\:\x00") {
		return in, Fail(400, "invalid_input_identity", "输入需有稳定ID和不含路径的原始文件名。")
	}
	in.SourceRelativePath = strings.ReplaceAll(in.SourceRelativePath, "\\", "/")
	p := in.SourceRelativePath
	if len(p) > 4096 || !utf8.ValidString(p) || strings.ContainsAny(p, ":\x00") || strings.HasPrefix(p, "/") || p == ".." || strings.HasPrefix(p, "../") || (p != "" && path.Clean(p) != p) {
		return in, Fail(400, "invalid_source_path", "输入来源只能是规范相对路径。")
	}
	if in.Size != nil && *in.Size < 0 {
		return in, Fail(400, "invalid_input_size", "来源长度不能为负数。")
	}
	if in.ExpectedHash != "" {
		if _, e := hex.DecodeString(in.ExpectedHash); e != nil || len(in.ExpectedHash) != 64 {
			return in, Fail(400, "invalid_hash", "来源摘要须为完整SHA-256。")
		}
		in.ExpectedHash = strings.ToLower(in.ExpectedHash)
	}
	return in, nil
}

func intakeRejection(in model.IntakeInput) string {
	if in.Size != nil && *in.Size == 0 {
		return "empty_input"
	}
	if in.NameState == "known" && len(path.Ext(in.OriginalFileName)) > 1 {
		if _, _, supported := domain.MediaType(in.OriginalFileName); !supported {
			return "unsupported_media"
		}
	}
	return ""
}

func (s *Service) SetBatchInputs(ctx context.Context, w *Write, id string, in model.BatchInputsSet) (model.Batch, error) {
	if e := Authorize(w.Principal, "ingest", id); e != nil {
		return model.Batch{}, e
	}
	batch, e := s.Batch(ctx, id)
	if e != nil {
		return batch, e
	}
	if batch.State == "cancelled" {
		return batch, Fail(409, "batch_cancelled", "已取消批次不能接收输入。")
	}
	if batch.ManifestRevision != in.ExpectedManifestRevision {
		return batch, Conflict(batch.ManifestRevision)
	}
	if batch.ManifestState != "open" {
		return batch, Fail(409, "manifest_sealed", "原始输入清单已经封存或来源未经验证；新的选择必须创建新批次。")
	}
	var uploads int
	if e = s.DB.DB.QueryRowContext(ctx, "SELECT count(*) FROM uploads WHERE batch_id=?", id).Scan(&uploads); e != nil {
		return batch, e
	}
	if uploads != 0 {
		return batch, Fail(409, "manifest_too_late", "清单必须先于文件传输登记，不能用已收到文件反推原始选择。")
	}
	q, e := w.Q()
	if e != nil {
		return batch, e
	}
	return sealIntakeInputs(ctx, q, batch, in.Inputs, false)
}

func sealIntakeInputs(ctx context.Context, q store.Querier, batch model.Batch, inputs []model.IntakeInput, implicitUpload bool) (model.Batch, error) {
	if len(inputs) < 1 || len(inputs) > MaxBatchItems {
		return batch, Fail(400, "invalid_manifest", "每批须完整登记1–100个输入，不能截断清单。")
	}
	normalized := make([]model.IntakeInput, len(inputs))
	seen := map[string]bool{}
	for i, input := range inputs {
		value, e := normalizeIntakeInput(input)
		if e != nil {
			return batch, e
		}
		if seen[value.ID] {
			return batch, Fail(400, "duplicate_input_identity", "每个原始输入必须有不同的稳定身份。")
		}
		seen[value.ID] = true
		normalized[i] = value
	}
	raw, e := json.Marshal(normalized)
	if e != nil {
		return batch, e
	}
	batch.ManifestFingerprint = digest(raw)
	batch.ManifestState = "sealed"
	batch.ExpectedInputs = len(normalized)
	batch.ManifestRevision++
	batch.Revision++
	batch.UpdatedAt = time.Now().UTC()
	for ordinal, input := range normalized {
		raw, e := store.Encode(input)
		if e != nil {
			return batch, e
		}
		// An implicit upload has already supplied a validated transfer name and
		// positive size; its original picker name is only source metadata.
		code := ""
		if !implicitUpload {
			code = intakeRejection(input)
			if code != "" {
				batch.State = "partial"
			}
		}
		if _, e = q.ExecContext(ctx, "INSERT INTO batch_inputs(batch_id,input_id,ordinal,data,rejection_code) VALUES(?,?,?,?,?)", batch.ID, input.ID, ordinal, raw, code); e != nil {
			return batch, e
		}
	}
	return batch, store.SaveBatch(ctx, q, batch)
}

func (s *Service) batchInputs(ctx context.Context, q store.Querier, id string) ([]model.BatchInput, error) {
	rows, e := q.QueryContext(ctx, `SELECT i.data,i.ordinal,coalesce(i.upload_id,''),
 CASE WHEN i.rejection_code!='' THEN 'failed' WHEN i.cancelled=1 THEN 'cancelled' ELSE coalesce(u.state,'pending') END,
 coalesce(json_extract(u.data,'$.assetId'),''),coalesce(nullif(i.rejection_code,''),json_extract(u.data,'$.errorCode'),'')
 FROM batch_inputs i LEFT JOIN uploads u ON u.id=i.upload_id WHERE i.batch_id=? ORDER BY i.ordinal LIMIT 101`, id)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	result := []model.BatchInput{}
	for rows.Next() {
		var value model.BatchInput
		var raw string
		if e = rows.Scan(&raw, &value.Ordinal, &value.UploadID, &value.State, &value.AssetID, &value.ErrorCode); e != nil {
			return nil, e
		}
		if e = json.Unmarshal([]byte(raw), &value.IntakeInput); e != nil {
			return nil, e
		}
		result = append(result, value)
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	if len(result) > MaxBatchItems {
		return nil, Fail(500, "manifest_corrupt", "输入清单超出已声明边界，不能据此报告完成。")
	}
	return result, nil
}

// Called before filesystem creation. Only the same durable upload may own a
// selected input; a different request cannot manufacture a second result for it.
func (s *Service) validateUploadInput(ctx context.Context, in *model.UploadInput) error {
	batch, e := s.Batch(ctx, in.BatchID)
	if e != nil {
		return e
	}
	if batch.ManifestState != "sealed" || !intakeIdentity.MatchString(in.InputID) {
		return Fail(409, "manifest_required", "请先完整登记原始选择，再用inputId上传对应文件。")
	}
	var raw string
	var existing sql.NullString
	var cancelled bool
	var rejected string
	e = s.DB.DB.QueryRowContext(ctx, "SELECT data,upload_id,cancelled,rejection_code FROM batch_inputs WHERE batch_id=? AND input_id=?", in.BatchID, in.InputID).Scan(&raw, &existing, &cancelled, &rejected)
	if errors.Is(e, sql.ErrNoRows) {
		return Fail(409, "input_not_declared", "此输入不在原始清单内，不能补入已封存批次。")
	}
	if e != nil {
		return e
	}
	if rejected != "" {
		return Fail(409, rejected, "这项来源在原始选择中已记为无法上传，请查看其输入回执。")
	}
	if existing.Valid {
		return Fail(409, "input_already_received", "此原始输入已有上传身份，请读取批次结果后沿原上传续传。")
	}
	if cancelled {
		return Fail(409, "input_cancelled", "此原始输入已取消，不能继续创建上传。")
	}
	var input model.IntakeInput
	if e = json.Unmarshal([]byte(raw), &input); e != nil {
		return e
	}
	if input.NameState != "unavailable" && in.OriginalFileName == "" {
		in.OriginalFileName = in.FileName
	}
	if input.NameState != "unavailable" && input.OriginalFileName != in.OriginalFileName || input.SourceRelativePath != in.SourceRelativePath || input.Size != nil && *input.Size != in.Size || input.ExpectedHash != "" && in.ExpectedHash != "" && input.ExpectedHash != in.ExpectedHash {
		return Fail(409, "input_source_changed", "上传来源与原始清单不一致，不能改写原输入身份。")
	}
	if input.ExpectedHash != "" {
		in.ExpectedHash = input.ExpectedHash
	}
	return nil
}

func bindUploadInput(ctx context.Context, q store.Querier, u model.Upload) error {
	result, e := q.ExecContext(ctx, "UPDATE batch_inputs SET upload_id=? WHERE batch_id=? AND input_id=? AND upload_id IS NULL AND cancelled=0 AND rejection_code=''", u.ID, u.BatchID, u.InputID)
	if e != nil {
		return e
	}
	count, e := result.RowsAffected()
	if e != nil {
		return e
	}
	if count != 1 {
		return Fail(409, "input_binding_changed", "原始输入的上传归属已变化，未创建第二份结果。")
	}
	return nil
}

func (s *Service) CancelBatchInput(ctx context.Context, w *Write, id, inputID, fingerprint string) (model.BatchInput, error) {
	var result model.BatchInput
	if e := Authorize(w.Principal, "ingest", id); e != nil {
		return result, e
	}
	batch, e := s.Batch(ctx, id)
	if e != nil {
		return result, e
	}
	if batch.ManifestState != "sealed" || batch.ManifestFingerprint != fingerprint {
		return result, Fail(409, "manifest_changed", "取消对象与原始输入清单不一致。")
	}
	inputs, e := s.batchInputs(ctx, s.DB.DB, id)
	if e != nil {
		return result, e
	}
	found := false
	for _, input := range inputs {
		if input.ID == inputID {
			result = input
			found = true
			break
		}
	}
	if !found {
		return result, Fail(404, "input_not_found", "原始输入不存在。")
	}
	if result.State == "cancelled" {
		return result, nil
	}
	if result.State == "failed" && result.UploadID == "" {
		return result, Fail(409, "input_already_failed", "此项在传输前已有终态，原始原因保留。")
	}
	if result.UploadID != "" {
		if e = s.CancelUpload(ctx, w, result.UploadID); e != nil {
			return result, e
		}
	}
	q, e := w.Q()
	if e != nil {
		return result, e
	}
	if _, e = q.ExecContext(ctx, "UPDATE batch_inputs SET cancelled=1 WHERE batch_id=? AND input_id=?", id, inputID); e != nil {
		return result, e
	}
	if result.UploadID == "" {
		if e = s.refreshBatch(ctx, q, id); e != nil {
			return result, e
		}
	}
	result.State = "cancelled"
	return result, nil
}
