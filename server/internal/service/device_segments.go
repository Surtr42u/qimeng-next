package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"time"

	"qimeng.local/next/server/internal/authorlist"
	"qimeng.local/next/server/internal/model"
)

const deviceChunkBytes = 1 << 20
const maxActiveDeviceBytes = 256 << 20

type DevicePackageFile struct {
	Reader io.ReaderAt
	Size   int64
}
type PreparedDevicePackage struct {
	manifest DeviceApplyManifest
	files    map[string]DevicePackageFile
}

func PrepareDevicePackage(ctx context.Context, m DeviceApplyManifest, files map[string]DevicePackageFile) (PreparedDevicePackage, error) {
	err := checkDevicePackage(ctx, m, files)
	if err != nil {
		return PreparedDevicePackage{}, err
	}
	return PreparedDevicePackage{manifest: m, files: files}, nil
}
func checkDevicePackage(ctx context.Context, m DeviceApplyManifest, files map[string]DevicePackageFile) error {
	if !deviceLabel.MatchString(m.TargetDeviceID) || m.ProtocolVersion != 1 || m.TTLSeconds != 0 && (m.TTLSeconds < 300 || m.TTLSeconds > 604800) ||
		len(m.Segments) < 1 || len(m.Segments) > 2 || len(m.Segments) != len(files) {
		return Fail(400, "invalid_device_manifest", "设备标识、协议、有效期或声明段无效。")
	}
	seen := make(map[string]bool)
	for _, part := range m.Segments {
		if err := ctx.Err(); err != nil {
			return err
		}
		f, ok := files[part.Kind]
		if !ok || seen[part.Kind] || f.Reader == nil || part.Bytes != f.Size || part.Bytes < 1 || !deviceHash.MatchString(part.SHA256) ||
			part.PackageID != "" || part.SourceInstanceID != "" || part.TargetInstanceID != "" {
			return Fail(400, "device_segment_mismatch", "每个声明段须有唯一对应的内容、长度和 SHA-256。")
		}
		seen[part.Kind] = true
		switch part.Kind {
		case "masterTxt":
			if part.Bytes > 1<<20 || part.ExpectedLocalRevision == nil || *part.ExpectedLocalRevision < 0 || part.SourceID != "" {
				return Fail(400, "invalid_device_txt", "作者总表须在 1 MiB 内，并指定目标片段的预期修订。")
			}
		case "v2Backup":
			return Fail(410, "legacy_delivery_retired", "新设备交付请从源实例选择原生对象。")
		default:
			return Fail(400, "invalid_device_segment", "未知设备包段。")
		}
		raw, err := io.ReadAll(io.NewSectionReader(f.Reader, 0, f.Size))
		if err != nil {
			return err
		}
		if int64(len(raw)) != part.Bytes || digest(raw) != part.SHA256 {
			return Fail(409, "device_hash_mismatch", "实际段内容与签发摘要不一致。")
		}
		doc, err := authorlist.Parse(string(raw))
		if err != nil || doc.Format != "blocks" {
			return Fail(400, "invalid_device_txt", "作者总表段须包含可导入的编号作者块。")
		}
	}
	return nil
}

// The handler checks the package before idempotency replay. While writing
// chunks, recompute the digest from the exact bytes that enter the transaction.
func (s *Service) PublishDeviceApply(ctx context.Context, w *Write, validated PreparedDevicePackage) (DeviceApplyTask, error) {
	var v DeviceApplyTask
	if err := Authorize(w.Principal, "admin", ""); err != nil {
		return v, err
	}
	m, files := validated.manifest, validated.files
	if len(m.Segments) == 0 {
		return v, Fail(400, "invalid_device_manifest", "维护包尚未核验。")
	}
	q, err := w.Q()
	if err != nil {
		return v, err
	}
	now := time.Now().UTC()
	var active bool
	if err = q.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM credentials WHERE json_extract(data,'$.deviceId')=? AND qimeng_time_key(expires_at)>?)", m.TargetDeviceID, dateKey(now)).Scan(&active); err != nil {
		return v, err
	}
	if !active {
		return v, Fail(409, "device_unregistered", "请先为目标设备签发有效的专用凭据。")
	}
	if _, err = q.ExecContext(ctx, "DELETE FROM device_apply_chunks WHERE task_id IN (SELECT id FROM device_apply_tasks WHERE expires_at<=?)", dateKey(now)); err != nil {
		return v, err
	}
	var all, device int
	err = q.QueryRowContext(ctx, `SELECT count(*),coalesce(sum(device_id=?),0) FROM device_apply_tasks
	 WHERE state IN ('pending','claimed') AND expires_at>?`, m.TargetDeviceID, dateKey(now)).Scan(&all, &device)
	if err != nil {
		return v, err
	}
	if all >= 256 || device >= 32 {
		return v, Fail(429, "device_package_capacity", "待处理维护包数量已达上限，请先完成或取消旧包。")
	}
	var occupied int64
	if err = q.QueryRowContext(ctx, "SELECT coalesce(sum(length(payload)),0) FROM device_apply_chunks").Scan(&occupied); err != nil {
		return v, err
	}
	var incoming int64
	for _, segment := range m.Segments {
		incoming += segment.Bytes
	}
	if occupied+incoming > maxActiveDeviceBytes {
		return v, Fail(429, "device_package_capacity", "等待领取的段已达存储预算，请先完成或取消旧包。")
	}
	free, err := freeBytes(s.Config.DataDir)
	if err != nil {
		return v, err
	}
	if free-s.Config.ReserveBytes < incoming*2 {
		return v, Fail(507, "insufficient_storage", "存储余量不足以提交维护包及其事务日志。")
	}
	if m.TTLSeconds == 0 {
		m.TTLSeconds = 86400
	}
	v = DeviceApplyTask{ID: w.ID("device-package"), TargetDeviceID: m.TargetDeviceID, ProtocolVersion: m.ProtocolVersion, State: "pending", Revision: 1,
		Segments: m.Segments, CreatedAt: now, ExpiresAt: now.Add(time.Duration(m.TTLSeconds) * time.Second)}
	if err = saveDeviceTask(ctx, q, v); err != nil {
		return v, err
	}
	buf := make([]byte, deviceChunkBytes)
	for _, segment := range v.Segments {
		f := files[segment.Kind]
		reader := io.NewSectionReader(f.Reader, 0, f.Size)
		hash := sha256.New()
		for offset := int64(0); offset < segment.Bytes; {
			part := buf[:min(int64(len(buf)), segment.Bytes-offset)]
			if _, err = io.ReadFull(reader, part); err != nil {
				return v, err
			}
			if _, err = q.ExecContext(ctx, "INSERT INTO device_apply_chunks(task_id,kind,ordinal,payload) VALUES(?,?,?,?)", v.ID, segment.Kind, offset/deviceChunkBytes, part); err != nil {
				return v, err
			}
			hash.Write(part)
			offset += int64(len(part))
		}
		if hex.EncodeToString(hash.Sum(nil)) != segment.SHA256 {
			return v, Fail(409, "device_hash_mismatch", "待发布内容在登记期间变化。")
		}
	}
	return v, nil
}

type DeviceDownload struct {
	Reader    *io.SectionReader
	Segment   DeviceApplySegment
	CreatedAt time.Time
}
type segmentReader struct {
	ctx                   context.Context
	db                    *sql.DB
	task, kind, principal string
	size                  int64
	cached                int64
	chunk                 []byte
}

func (r *segmentReader) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, errors.New("negative device segment offset")
	}
	n := 0
	for len(p) > 0 {
		if off >= r.size {
			return n, io.EOF
		}
		if err := r.ctx.Err(); err != nil {
			return n, err
		}
		index := off / deviceChunkBytes
		if r.cached != index {
			now := dateKey(time.Now().UTC())
			err := r.db.QueryRowContext(r.ctx, `SELECT c.payload FROM device_apply_chunks c JOIN device_apply_tasks t ON t.id=c.task_id
			 WHERE c.task_id=? AND c.kind=? AND c.ordinal=? AND t.state='claimed' AND t.expires_at>?
			 AND EXISTS(SELECT 1 FROM credentials a WHERE a.id=? AND qimeng_time_key(a.expires_at)>?)`,
				r.task, r.kind, index, now, r.principal, now).Scan(&r.chunk)
			if err != nil {
				return n, err
			}
			r.cached = index
		}
		start := int(off % deviceChunkBytes)
		if start >= len(r.chunk) {
			return n, io.ErrUnexpectedEOF
		}
		count := copy(p, r.chunk[start:])
		n += count
		off += int64(count)
		p = p[count:]
	}
	return n, nil
}
func (s *Service) DeviceApplyDownload(ctx context.Context, p model.Principal, id, kind string) (DeviceDownload, error) {
	var out DeviceDownload
	v, err := deviceTask(ctx, s.DB.DB, p, id, true)
	if err != nil {
		return out, err
	}
	if v.State != "claimed" {
		return out, Fail(409, "device_claim_required", "请用目标设备的有效领取记录下载。")
	}
	for _, part := range v.Segments {
		if part.Kind == kind {
			reader := &segmentReader{ctx: ctx, db: s.DB.DB, task: id, kind: kind, principal: p.ID, size: part.Bytes, cached: -1}
			return DeviceDownload{Reader: io.NewSectionReader(reader, 0, part.Bytes), Segment: part, CreatedAt: v.CreatedAt}, nil
		}
	}
	return out, Fail(404, "device_segment_missing", "该维护包没有此段。")
}
