package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"time"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/store"
)

func (b NativeVideoBinding) valid() bool {
	return intakeIdentity.MatchString(b.SourceAssetID) && intakeIdentity.MatchString(b.TargetAssetID) && b.SourceContentVersion > 0 && b.TargetContentVersion > 0
}

type nativeVideoRef struct {
	id      string
	version int64
}

type nativeVideoProof struct {
	file  *os.File
	info  os.FileInfo
	stamp string
	hash  string
	size  int64
}

// Files stay open across verification and commit so inode reuse or a pathname
// substitution cannot make different bytes pass the final version check.
type NativeVideoProofs struct {
	files map[nativeVideoRef]nativeVideoProof
}

var nativeVerificationSlots = make(chan struct{}, 2)

func (proofs *NativeVideoProofs) Close() {
	if proofs == nil || proofs.files == nil {
		return
	}
	for _, proof := range proofs.files {
		if proof.file != nil {
			proof.file.Close()
		}
	}
	proofs.files = nil
	<-nativeVerificationSlots
}

func (s *Service) InspectNativePublish(ctx context.Context, principal model.Principal, in NativePublish) (*NativeVideoProofs, error) {
	if err := Authorize(principal, "admin", ""); err != nil {
		return nil, err
	}
	if len(in.Items) > 32 {
		return nil, Fail(400, "invalid_native_selection", "单包最多 32 项。")
	}
	var refs []nativeVideoRef
	for _, item := range in.Items {
		if item.Kind == "timelineMark" && item.NativeVideoBinding.valid() {
			refs = append(refs, nativeVideoRef{item.SourceAssetID, item.SourceContentVersion})
		}
	}
	return s.inspectNativeVideos(ctx, refs)
}

func (s *Service) InspectNativeStage(ctx context.Context, p model.Principal, raw []byte, source, target, id string) (*NativeVideoProofs, error) {
	if err := Authorize(p, "admin", ""); err != nil {
		return nil, err
	}
	if target != s.InstanceID {
		return nil, Fail(409, "native_target_mismatch", "此包不属于当前接收实例。")
	}
	pkg, err := readNativePackage(raw, source, target, id)
	if err != nil {
		return nil, err
	}
	var staged bool
	if err = s.DB.DB.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM native_delivery_plans WHERE package_id=?)", id).Scan(&staged); err != nil || staged {
		return nil, err
	}
	return s.inspectNativeTargets(ctx, pkg)
}

func (s *Service) InspectNativeCommit(ctx context.Context, p model.Principal, id, fingerprint string) (*NativeVideoProofs, error) {
	plan, err := s.NativeDelivery(ctx, p, id)
	if err != nil {
		return nil, err
	}
	if plan.TargetInstanceID != s.InstanceID || plan.Fingerprint != fingerprint {
		return nil, Fail(409, "native_plan_changed", "预览指纹或目标实例不符。")
	}
	if plan.State != "prepared" {
		return nil, nil
	}
	var refs []nativeVideoRef
	for _, item := range plan.Items {
		if item.Kind == "timelineMark" && item.ReceiptID == "" {
			refs = append(refs, nativeVideoRef{item.TargetAssetID, item.TargetContentVersion})
		}
	}
	return s.inspectNativeVideos(ctx, refs)
}

func (s *Service) inspectNativeTargets(ctx context.Context, pkg nativePackage) (*NativeVideoProofs, error) {
	var refs []nativeVideoRef
	for _, item := range pkg.Items {
		if t := item.Timeline; t != nil {
			refs = append(refs, nativeVideoRef{t.TargetAssetID, t.TargetContentVersion})
		}
	}
	return s.inspectNativeVideos(ctx, refs)
}

func (s *Service) inspectNativeVideos(ctx context.Context, refs []nativeVideoRef) (*NativeVideoProofs, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	select {
	case nativeVerificationSlots <- struct{}{}:
	default:
		return nil, Fail(429, "native_verification_busy", "正在核验其他原生视频交付。")
	}
	proofs := &NativeVideoProofs{files: make(map[nativeVideoRef]nativeVideoProof)}
	for _, ref := range refs {
		if _, seen := proofs.files[ref]; seen {
			continue
		}
		proof, err := s.inspectNativeVideo(ctx, ref)
		if err != nil {
			proofs.Close()
			return nil, err
		}
		proofs.files[ref] = proof
	}
	return proofs, nil
}

func (s *Service) inspectNativeVideo(ctx context.Context, ref nativeVideoRef) (nativeVideoProof, error) {
	var proof nativeVideoProof
	if !syncPlatformSupported {
		return proof, nil
	}
	file, asset, err := s.openNativeVideo(ctx, ref)
	if err != nil {
		var problem *Error
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrPermission) || errors.As(err, &problem) && (problem.Status == 404 || problem.Status == 409) {
			return proof, nil
		}
		return proof, err
	}
	retained := false
	defer func() {
		if !retained {
			file.Close()
		}
	}()
	info, err := file.Stat()
	if err != nil || asset.MediaType != "video" || !info.Mode().IsRegular() || info.Size() != asset.Size {
		return proof, err
	}
	stamp, err := syncFileStamp(file, info)
	if err != nil {
		return proof, err
	}
	sum := sha256.New()
	n, err := io.Copy(sum, scanReader{ctx: ctx, source: file})
	if err != nil {
		return proof, err
	}
	hash := hex.EncodeToString(sum.Sum(nil))
	if n != asset.Size || hash != asset.Hash {
		return proof, nil
	}
	retained = true
	return nativeVideoProof{file, info, stamp, hash, n}, nil
}

func (s *Service) openNativeVideo(ctx context.Context, ref nativeVideoRef) (*os.File, model.Asset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, err := s.Asset(ctx, ref.id)
	if err != nil {
		return nil, a, err
	}
	if a.Trashed || a.MediaType != "video" || a.ContentVersion != ref.version {
		return nil, a, Fail(409, "native_content_changed", "所选视频的活动版本不符。")
	}
	name := assetPath(a)
	if err = s.safe(name); err != nil {
		return nil, a, err
	}
	info, err := s.Root.Lstat(name)
	if err != nil {
		return nil, a, err
	}
	if !info.Mode().IsRegular() || syncPathLink(info) {
		return nil, a, Fail(409, "native_content_changed", "视频原件已被其他类型的目录项替换。")
	}
	// On Unix an external replacement with a FIFO between Lstat and Open must
	// not block the mutation lock indefinitely.
	file, err := s.Root.OpenFile(name, os.O_RDONLY|syncReadFlags, 0)
	return file, a, err
}

func (s *Service) verifiedNativeVideo(ctx context.Context, q store.Querier, proofs *NativeVideoProofs, id string, version int64) (model.Asset, string, error) {
	a, err := store.Get[model.Asset](ctx, q, "SELECT data FROM assets WHERE id=? AND trashed=0", id)
	if errors.Is(err, sql.ErrNoRows) {
		return a, "target_asset_missing", nil
	}
	if err != nil {
		return a, "", err
	}
	if a.MediaType != "video" || a.ContentVersion != version {
		return a, "target_content_changed", nil
	}
	if proofs == nil {
		return a, "target_content_unverified", nil
	}
	proof := proofs.files[nativeVideoRef{id, version}]
	if proof.file == nil || proof.hash != a.Hash || proof.size != a.Size {
		return a, "target_content_changed", nil
	}
	named, err := s.Root.Lstat(assetPath(a))
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrPermission) {
		return a, "target_content_changed", nil
	}
	if err != nil {
		return a, "", err
	}
	if !named.Mode().IsRegular() || !os.SameFile(proof.info, named) || named.Size() != proof.size || !proof.info.ModTime().Equal(named.ModTime()) {
		return a, "target_content_changed", nil
	}
	current, err := syncFileStamp(proof.file, named)
	if err != nil {
		return a, "", err
	}
	if current != proof.stamp {
		return a, "target_content_changed", nil
	}
	return a, "", nil
}

func (s *Service) inspectNativeMark(ctx context.Context, q store.Querier, proofs *NativeVideoProofs, item nativeItem, binding nativeBinding, r NativeItemResult) (NativeItemResult, error) {
	t := item.Timeline
	video, code, err := s.verifiedNativeVideo(ctx, q, proofs, t.TargetAssetID, t.TargetContentVersion)
	if err != nil {
		return r, err
	}
	if code != "" || video.Hash != t.ContentSHA256 || video.Size != t.ContentBytes || !validTimelineValues(t.PositionMs, item.Label, t.Color, video.DurationMs) {
		r.State, r.Code = "conflict", "target_content_changed"
		if code != "" {
			r.Code = code
		}
		return r, nil
	}
	mark, err := store.Get[TimelineMark](ctx, q, "SELECT data FROM timeline_marks WHERE id=?", r.TargetObjectID)
	revision := int64(0)
	if err == nil {
		revision = mark.Revision
		if binding.targetID == "" || mark.AssetID != t.TargetAssetID || mark.ContentVersion != t.TargetContentVersion {
			r.State, r.Code = "conflict", "target_binding_conflict"
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return r, err
	}
	r.TargetRevision = &revision
	return r, nil
}

func applyNativeMark(ctx context.Context, q store.Querier, item nativeItem, target NativeItemResult) (int64, error) {
	t := item.Timeline
	now := time.Now().UTC()
	mark := TimelineMark{ID: target.TargetObjectID, AssetID: t.TargetAssetID, ContentVersion: t.TargetContentVersion,
		PositionMs: t.PositionMs, Label: item.Label, Color: t.Color, Revision: 1, CreatedAt: now, UpdatedAt: now}
	if target.TargetRevision != nil && *target.TargetRevision != 0 {
		old, err := store.Get[TimelineMark](ctx, q, "SELECT data FROM timeline_marks WHERE id=? AND asset_id=?", mark.ID, mark.AssetID)
		if err != nil {
			return 0, err
		}
		if old.Revision != *target.TargetRevision || old.ContentVersion != t.TargetContentVersion {
			return 0, Conflict(old.Revision)
		}
		mark.Revision, mark.CreatedAt = old.Revision+1, old.CreatedAt
	}
	return mark.Revision, writeTimelineMark(ctx, q, mark)
}
