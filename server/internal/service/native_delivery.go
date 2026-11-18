package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"path"
	"slices"
	"strings"

	"qimeng.local/next/server/internal/authorlist"
	"qimeng.local/next/server/internal/domain"
	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/store"
)

const NativeDeliveryBytes = 2 << 20
const nativeSegment = "nativeSelection"

type NativeSelection struct {
	NativeVideoBinding
	Kind             string `json:"kind"`
	SourceObjectID   string `json:"sourceObjectId"`
	Block            string `json:"block,omitempty"`
	ExpectedRevision int64  `json:"expectedRevision"`
}

type NativeVideoBinding struct {
	SourceAssetID        string `json:"sourceAssetId,omitempty"`
	SourceContentVersion int64  `json:"sourceContentVersion,omitempty"`
	TargetAssetID        string `json:"targetAssetId,omitempty"`
	TargetContentVersion int64  `json:"targetContentVersion,omitempty"`
}

type nativeTimeline struct {
	NativeVideoBinding
	ContentSHA256 string `json:"contentSha256"`
	ContentBytes  int64  `json:"contentBytes"`
	PositionMs    int64  `json:"positionMs"`
	Color         string `json:"color,omitempty"`
}

type NativePublish struct {
	TargetDeviceID   string            `json:"targetDeviceId"`
	TargetInstanceID string            `json:"targetInstanceId"`
	TTLSeconds       int               `json:"ttlSeconds,omitempty"`
	Items            []NativeSelection `json:"items"`
}

type nativeProfile struct {
	AuthorKind string            `json:"authorKind"`
	Values     ProfileValues     `json:"values"`
	Evidence   []ProfileEvidence `json:"evidence"`
}

type nativeItem struct {
	Kind           string          `json:"kind"`
	SourceObjectID string          `json:"sourceObjectId"`
	Block          string          `json:"block,omitempty"`
	SourceRevision int64           `json:"sourceRevision"`
	Label          string          `json:"label"`
	Profile        *nativeProfile  `json:"profile,omitempty"`
	Text           *string         `json:"text,omitempty"`
	Timeline       *nativeTimeline `json:"timeline,omitempty"`
}

type nativePackage struct {
	Format           string       `json:"format"`
	FormatVersion    int          `json:"formatVersion"`
	SourceInstanceID string       `json:"sourceInstanceId"`
	TargetInstanceID string       `json:"targetInstanceId"`
	Items            []nativeItem `json:"items"`
}

type NativeItemResult struct {
	NativeVideoBinding
	ContentSHA256  string `json:"contentSha256,omitempty"`
	ContentBytes   int64  `json:"contentBytes,omitempty"`
	Kind           string `json:"kind"`
	SourceObjectID string `json:"sourceObjectId"`
	Block          string `json:"block,omitempty"`
	SourceRevision int64  `json:"sourceRevision"`
	Label          string `json:"label"`
	TargetObjectID string `json:"targetObjectId,omitempty"`
	TargetRevision *int64 `json:"targetRevision,omitempty"`
	State          string `json:"state"`
	Code           string `json:"code,omitempty"`
	ReceiptID      string `json:"receiptId,omitempty"`
}

type NativePlan struct {
	ID               string             `json:"id"`
	PackageID        string             `json:"packageId"`
	SourceInstanceID string             `json:"sourceInstanceId"`
	TargetInstanceID string             `json:"targetInstanceId"`
	ConflictPolicy   string             `json:"conflictPolicy"`
	Fingerprint      string             `json:"fingerprint"`
	State            string             `json:"state"`
	Items            []NativeItemResult `json:"items"`
}

func nativeItemKey(item nativeItem) string {
	return item.SourceObjectID + "\x00" + item.Kind + "\x00" + item.Block
}

func nativeItemHash(item nativeItem) (string, error) {
	item.SourceRevision = 0
	// A display rename belongs to the author identity, not the selected
	// profile block revision. A repeat of that block retains its receipt.
	if item.Kind == "authorProfileBlock" {
		item.Label = ""
	}
	raw, err := json.Marshal(item)
	if err != nil {
		return "", err
	}
	return digest(raw), nil
}

func validateNativeItem(item nativeItem) error {
	if !intakeIdentity.MatchString(item.SourceObjectID) || item.SourceRevision < 1 || !profileText(item.Label, 240) || strings.TrimSpace(item.Label) == "" {
		return Fail(400, "invalid_native_item", "原生对象身份、名称或业务修订无效。")
	}
	switch item.Kind {
	case "authorList":
		if item.Block != "" || item.Profile != nil || item.Timeline != nil || item.Text == nil || len(*item.Text) > 1<<20 ||
			!domain.ValidName(item.Label) || !strings.EqualFold(path.Ext(item.Label), ".txt") {
			return Fail(400, "invalid_native_list", "作者片段须包含有效文件名及有界的编号声明。")
		}
		document, err := authorlist.Parse(*item.Text)
		if err != nil || document.Format != "blocks" || len(document.Blocks) == 0 {
			return Fail(400, "invalid_native_list", "作者片段必须包含编号作者块。")
		}
	case "authorProfileBlock":
		p := item.Profile
		if !validProfileBlock(item.Block) || item.Text != nil || item.Timeline != nil || p == nil || p.AuthorKind != "regular" && p.AuthorKind != "cos" ||
			p.Values.Note != nil || p.Values.Remark != nil || p.Values.AvatarSourceURL != nil {
			return Fail(400, "invalid_native_profile", "资料块不能包含私人备注、头像或未确认候选。")
		}
		if _, err := authorlist.Names(item.Label); err != nil {
			return Fail(400, "invalid_native_profile", "作者名称无效。")
		}
		n, err := validateProfileValues(item.Block, p.Values)
		if err != nil {
			return err
		}
		if n == 0 && len(p.Evidence) == 0 {
			return Fail(400, "native_profile_empty", "所选资料块没有可交付字段。")
		}
		if profileString(p.Values.BiographyZh) != "" && profileString(p.Values.BiographyOriginal) == "" {
			return Fail(400, "translation_without_original", "译文须包含对应原文。")
		}
		if err = validateProfileEvidence(item.Block, p.Evidence, false); err != nil {
			return err
		}
		for _, proof := range p.Evidence {
			if proof.Status != "confirmed" || proof.Note != "" {
				return Fail(400, "invalid_native_evidence", "原生资料包只接受确认过的公开证据。")
			}
		}
	case "timelineMark":
		t := item.Timeline
		if t == nil || item.Text != nil || item.Profile != nil || item.Block != "" || !t.NativeVideoBinding.valid() || !deviceHash.MatchString(t.ContentSHA256) ||
			t.ContentBytes < 1 || !validTimelineValues(t.PositionMs, item.Label, t.Color, 0) {
			return Fail(400, "invalid_native_mark", "时间轴标记需要明确绑定两端视频、版本、摘要和大小。")
		}
	default:
		return Fail(400, "native_kind_unavailable", "该对象类型尚未开放原生交付。")
	}
	return nil
}

func readNativePackage(raw []byte, source, target, packageID string) (nativePackage, error) {
	var pkg nativePackage
	if len(raw) == 0 || len(raw) > NativeDeliveryBytes || !deviceHash.MatchString(packageID) || digest(raw) != packageID {
		return pkg, Fail(409, "native_hash_mismatch", "原生段长度或 SHA-256 不匹配。")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&pkg); err != nil || !errors.Is(d.Decode(new(any)), io.EOF) || pkg.Format != "qimeng-native-selection" || pkg.FormatVersion != 1 ||
		source == target || !intakeIdentity.MatchString(source) || !intakeIdentity.MatchString(target) || pkg.SourceInstanceID != source || pkg.TargetInstanceID != target ||
		len(pkg.Items) < 1 || len(pkg.Items) > 32 {
		return pkg, Fail(400, "invalid_native_package", "包格式、来源、目标或对象数量无效。")
	}
	seen := map[string]bool{}
	for _, item := range pkg.Items {
		if err := validateNativeItem(item); err != nil {
			return pkg, err
		}
		key := nativeItemKey(item)
		if seen[key] {
			return pkg, Fail(400, "duplicate_native_object", "每个来源对象在一次交付中只能出现一次。")
		}
		seen[key] = true
	}
	return pkg, nil
}

func (s *Service) PublishNativeDelivery(ctx context.Context, w *Write, in NativePublish, proofs *NativeVideoProofs) (DeviceApplyTask, error) {
	var task DeviceApplyTask
	if err := Authorize(w.Principal, "admin", ""); err != nil {
		return task, err
	}
	if !deviceLabel.MatchString(in.TargetDeviceID) || !intakeIdentity.MatchString(in.TargetInstanceID) || in.TargetInstanceID == s.InstanceID ||
		len(in.Items) < 1 || len(in.Items) > 32 || in.TTLSeconds != 0 && (in.TTLSeconds < 300 || in.TTLSeconds > 604800) {
		return task, Fail(400, "invalid_native_selection", "请选择另一目标实例、设备与 1 至 32 个来源对象。")
	}
	q, err := w.Q()
	if err != nil {
		return task, err
	}
	pkg := nativePackage{Format: "qimeng-native-selection", FormatVersion: 1, SourceInstanceID: s.InstanceID, TargetInstanceID: in.TargetInstanceID}
	size := 0
	for _, selection := range in.Items {
		item, err := s.exportNativeItem(ctx, q, selection, proofs)
		if err != nil {
			return task, err
		}
		part, err := json.Marshal(item)
		if err != nil {
			return task, err
		}
		size += len(part)
		if size > NativeDeliveryBytes {
			return task, Fail(413, "native_package_too_large", "所选对象超过 2 MiB 段预算。")
		}
		pkg.Items = append(pkg.Items, item)
	}
	slices.SortFunc(pkg.Items, func(a, b nativeItem) int {
		if a.Kind != b.Kind && a.Kind == "authorProfileBlock" {
			return -1
		}
		if a.Kind != b.Kind && b.Kind == "authorProfileBlock" {
			return 1
		}
		if a.Kind != b.Kind {
			return strings.Compare(a.Kind, b.Kind)
		}
		return strings.Compare(nativeItemKey(a), nativeItemKey(b))
	})
	raw, err := json.Marshal(pkg)
	if err != nil {
		return task, err
	}
	hash := digest(raw)
	if len(raw) > NativeDeliveryBytes {
		return task, Fail(413, "native_package_too_large", "所选对象超过 2 MiB 段预算。")
	}
	if _, err = readNativePackage(raw, s.InstanceID, in.TargetInstanceID, hash); err != nil {
		return task, err
	}
	segment := DeviceApplySegment{Kind: nativeSegment, Bytes: int64(len(raw)), SHA256: hash, PackageID: hash, SourceInstanceID: s.InstanceID, TargetInstanceID: in.TargetInstanceID}
	return s.PublishDeviceApply(ctx, w, PreparedDevicePackage{
		manifest: DeviceApplyManifest{TargetDeviceID: in.TargetDeviceID, ProtocolVersion: 2, TTLSeconds: in.TTLSeconds, Segments: []DeviceApplySegment{segment}},
		files:    map[string]DevicePackageFile{nativeSegment: {Reader: bytes.NewReader(raw), Size: segment.Bytes}},
	})
}

func (s *Service) exportNativeItem(ctx context.Context, q store.Querier, in NativeSelection, proofs *NativeVideoProofs) (nativeItem, error) {
	item := nativeItem{Kind: in.Kind, SourceObjectID: in.SourceObjectID, Block: in.Block}
	if in.Kind != "timelineMark" && in.NativeVideoBinding != (NativeVideoBinding{}) {
		return item, Fail(400, "unexpected_native_media_version", "作者资料和 TXT 没有适用的视频内容版本。")
	}
	switch in.Kind {
	case "authorList":
		var recordSize int64
		if err := q.QueryRowContext(ctx, "SELECT length(CAST(data AS BLOB)) FROM author_lists WHERE id=?", in.SourceObjectID).Scan(&recordSize); err != nil {
			return item, Normalize(err)
		}
		if recordSize > 8<<20 {
			return item, Fail(413, "native_package_too_large", "片段记录超出选择性交付的读取预算。")
		}
		list, err := authorListRecordAt(ctx, q, in.SourceObjectID)
		if err != nil {
			return item, Normalize(err)
		}
		item.Label, item.SourceRevision = list.Name, list.Revision
		// Export the declarations owned by this revision. Derived live links
		// can change independently and do not identify a revision of this list.
		blocks := make([]authorlist.Block, len(list.Blocks))
		for i, entry := range list.Blocks {
			blocks[i] = entry.Declaration
		}
		text, err := boundedAuthorText(blocks)
		if err != nil {
			return item, err
		}
		item.Text = &text
	case "authorProfileBlock":
		author, err := store.Get[model.Author](ctx, q, "SELECT data FROM authors WHERE id=?", in.SourceObjectID)
		if err != nil {
			return item, Normalize(err)
		}
		block, err := profileBlock(ctx, q, author.ID, in.Block)
		if err != nil {
			return item, err
		}
		if author.Placeholder || block.Revision == 0 {
			return item, Fail(409, "native_profile_unavailable", "占位作者或未确认的空白资料块不可交付。")
		}
		item.Label, item.SourceRevision = author.Name, block.Revision
		values := block.Values
		values.Note, values.Remark, values.AvatarSourceURL = nil, nil, nil
		item.Profile = &nativeProfile{AuthorKind: author.Kind, Values: values, Evidence: []ProfileEvidence{}}
		confirmed := map[string]bool{}
		for _, proof := range block.Evidence {
			if proof.Status == "confirmed" {
				proof.Note = ""
				fields := make([]string, 0, len(proof.Fields))
				for _, field := range proof.Fields {
					if field != "note" && field != "remark" && field != "avatarSourceUrl" {
						fields = append(fields, field)
						confirmed[field] = true
					}
				}
				if len(fields) > 0 {
					proof.Fields = fields
					item.Profile.Evidence = append(item.Profile.Evidence, proof)
				}
			}
		}
		// Historical imports can expose an unverified field alongside confirmed
		// fields. Only owner-confirmed or evidenced values travel onward.
		if block.Source != "human" && block.Source != "native_delivery" {
			encoded, err := json.Marshal(values)
			if err != nil {
				return item, err
			}
			var fields map[string]json.RawMessage
			if err = json.Unmarshal(encoded, &fields); err != nil {
				return item, err
			}
			for field := range fields {
				if !confirmed[field] {
					delete(fields, field)
				}
			}
			encoded, err = json.Marshal(fields)
			if err != nil {
				return item, err
			}
			item.Profile.Values = ProfileValues{}
			if err = json.Unmarshal(encoded, &item.Profile.Values); err != nil {
				return item, err
			}
		}
	case "timelineMark":
		if !in.NativeVideoBinding.valid() || in.Block != "" {
			return item, Fail(400, "native_video_binding_required", "请选择两端稳定的视频资产和当前版本。")
		}
		mark, err := store.Get[TimelineMark](ctx, q, "SELECT data FROM timeline_marks WHERE id=? AND asset_id=?", in.SourceObjectID, in.SourceAssetID)
		if err != nil {
			return item, Normalize(err)
		}
		a, code, err := s.verifiedNativeVideo(ctx, q, proofs, in.SourceAssetID, in.SourceContentVersion)
		if err != nil {
			return item, err
		}
		if code != "" || mark.ContentVersion != in.SourceContentVersion {
			return item, Fail(409, "native_source_content_changed", "来源标记与所选视频的当前版本不符。")
		}
		item.Label, item.SourceRevision = mark.Label, mark.Revision
		item.Timeline = &nativeTimeline{NativeVideoBinding: in.NativeVideoBinding, ContentSHA256: a.Hash, ContentBytes: a.Size, PositionMs: mark.PositionMs, Color: mark.Color}
	default:
		return item, Fail(400, "native_kind_unavailable", "该对象类型尚未开放原生交付。")
	}
	if in.ExpectedRevision != item.SourceRevision {
		return item, Conflict(item.SourceRevision)
	}
	return item, validateNativeItem(item)
}
