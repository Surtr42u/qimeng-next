package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"qimeng.local/next/server/internal/authorlist"
	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/store"
)

type nativeBinding struct {
	targetID       string
	targetRevision int64
	sourceRevision int64
	hash           string
}

func nativeID(parts ...string) string {
	return digest([]byte(strings.Join(parts, "\x00")))[:32]
}

func (w *Write) BindNativeStageRequest(ctx context.Context, plan NativePlan, key, policy string) error {
	q, err := w.Q()
	if err != nil {
		return err
	}
	hash := digest([]byte(plan.SourceInstanceID + "\x00" + plan.TargetInstanceID + "\x00" + plan.PackageID + "\x00" + policy))
	var prior string
	err = q.QueryRowContext(ctx, "SELECT input_hash FROM native_delivery_stage_requests WHERE principal_id=? AND request_key=?", w.Principal.ID, key).Scan(&prior)
	if err == nil {
		if prior != hash {
			return Fail(409, "idempotency_conflict", "该暂存身份已绑定其他原生包或选项。")
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	_, err = q.ExecContext(ctx, "INSERT INTO native_delivery_stage_requests(principal_id,request_key,input_hash,plan_id) VALUES(?,?,?,?)", w.Principal.ID, key, hash, plan.ID)
	return err
}

func (s *Service) NativeDelivery(ctx context.Context, principal model.Principal, id string) (NativePlan, error) {
	if err := Authorize(principal, "admin", ""); err != nil {
		return NativePlan{}, err
	}
	v, err := store.Get[NativePlan](ctx, s.DB.DB, "SELECT data FROM native_delivery_plans WHERE id=?", id)
	return v, Normalize(err)
}

func (s *Service) StageNativeDelivery(ctx context.Context, w *Write, raw []byte, source, target, packageID, policy string, proofs *NativeVideoProofs) (NativePlan, error) {
	var plan NativePlan
	if err := Authorize(w.Principal, "admin", ""); err != nil {
		return plan, err
	}
	if target != s.InstanceID || policy != "keep" && policy != "replace" {
		return plan, Fail(409, "native_target_mismatch", "接收实例或冻结的冲突处理方式无效。")
	}
	pkg, err := readNativePackage(raw, source, target, packageID)
	if err != nil {
		return plan, err
	}
	q, err := w.Q()
	if err != nil {
		return plan, err
	}
	plan, err = store.Get[NativePlan](ctx, q, "SELECT data FROM native_delivery_plans WHERE package_id=?", packageID)
	if err == nil {
		if plan.State == "prepared" && plan.ConflictPolicy != policy {
			return plan, Fail(409, "native_policy_frozen", "同一包已按另一明确选择生成预览。")
		}
		return plan, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return plan, err
	}
	now := time.Now().UTC()
	if _, err = q.ExecContext(ctx, "DELETE FROM native_delivery_plans WHERE state='prepared' AND created_at<?", dateKey(now.Add(-7*24*time.Hour))); err != nil {
		return plan, err
	}
	var count int
	var bytes int64
	if err = q.QueryRowContext(ctx, "SELECT count(*),coalesce(sum(length(payload)),0) FROM native_delivery_plans WHERE state='prepared'").Scan(&count, &bytes); err != nil {
		return plan, err
	}
	if count >= 64 || bytes+int64(len(raw)) > 64<<20 {
		return plan, Fail(429, "native_stage_capacity", "原生包预览已达暂存预算。")
	}
	plan = NativePlan{ID: nativeID("native-plan", target, packageID), PackageID: packageID, SourceInstanceID: source, TargetInstanceID: target, ConflictPolicy: policy, State: "prepared"}
	for _, item := range pkg.Items {
		result, err := s.inspectNativeItem(ctx, q, plan, item, proofs)
		if err != nil {
			return plan, err
		}
		plan.Items = append(plan.Items, result)
	}
	encoded, err := json.Marshal(plan)
	if err != nil {
		return plan, err
	}
	plan.Fingerprint = digest(encoded)
	record, err := store.Encode(plan)
	if err != nil {
		return plan, err
	}
	_, err = q.ExecContext(ctx, "INSERT INTO native_delivery_plans(id,package_id,source_instance,target_instance,state,created_at,data,payload) VALUES(?,?,?,?,?,?,?,?)",
		plan.ID, packageID, source, target, plan.State, dateKey(now), record, raw)
	return plan, err
}

func (s *Service) inspectNativeItem(ctx context.Context, q store.Querier, plan NativePlan, item nativeItem, proofs *NativeVideoProofs) (NativeItemResult, error) {
	r := NativeItemResult{Kind: item.Kind, SourceObjectID: item.SourceObjectID, Block: item.Block, SourceRevision: item.SourceRevision, Label: item.Label, State: "ready"}
	if t := item.Timeline; t != nil {
		r.NativeVideoBinding, r.ContentSHA256, r.ContentBytes = t.NativeVideoBinding, t.ContentSHA256, t.ContentBytes
	}
	hash, err := nativeItemHash(item)
	if err != nil {
		return r, err
	}
	var oldHash, raw string
	err = q.QueryRowContext(ctx, "SELECT payload_hash,data FROM native_delivery_receipts WHERE target_instance=? AND source_instance=? AND kind=? AND object_id=? AND block=? AND source_revision=?",
		plan.TargetInstanceID, plan.SourceInstanceID, item.Kind, item.SourceObjectID, item.Block, item.SourceRevision).Scan(&oldHash, &raw)
	if err == nil {
		if oldHash != hash {
			r.State, r.Code = "conflict", "source_revision_conflict"
			if t := item.Timeline; t != nil {
				var old NativeItemResult
				if err := json.Unmarshal([]byte(raw), &old); err != nil {
					return r, err
				}
				if old.TargetAssetID != t.TargetAssetID || old.TargetContentVersion != t.TargetContentVersion {
					r.Code = "target_binding_conflict"
				}
			}
			return r, nil
		}
		err = json.Unmarshal([]byte(raw), &r)
		return r, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return r, err
	}
	var newer bool
	if err = q.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM native_delivery_receipts WHERE target_instance=? AND source_instance=? AND kind=? AND object_id=? AND block=? AND source_revision>?)",
		plan.TargetInstanceID, plan.SourceInstanceID, item.Kind, item.SourceObjectID, item.Block, item.SourceRevision).Scan(&newer); err != nil {
		return r, err
	}
	if newer {
		r.State, r.Code = "conflict", "source_revision_conflict"
		return r, nil
	}
	var binding nativeBinding
	err = q.QueryRowContext(ctx, "SELECT target_id,target_revision,source_revision,payload_hash FROM native_delivery_bindings WHERE source_instance=? AND kind=? AND object_id=? AND block=?",
		plan.SourceInstanceID, item.Kind, item.SourceObjectID, item.Block).Scan(&binding.targetID, &binding.targetRevision, &binding.sourceRevision, &binding.hash)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return r, err
	}
	if binding.sourceRevision > item.SourceRevision || binding.sourceRevision == item.SourceRevision && binding.hash != hash {
		r.State, r.Code = "conflict", "source_revision_conflict"
		return r, nil
	}
	r.TargetObjectID = binding.targetID
	if r.TargetObjectID == "" {
		r.TargetObjectID = nativeID("native-object", plan.TargetInstanceID, plan.SourceInstanceID, item.Kind, item.SourceObjectID)
	}
	var revision int64
	if item.Kind == "authorList" {
		var existingName string
		err = q.QueryRowContext(ctx, "SELECT revision,name FROM author_lists WHERE id=?", r.TargetObjectID).Scan(&revision, &existingName)
		if err == nil && (binding.targetID == "" || existingName != item.Label) {
			r.State, r.Code = "preserved", "human_protected"
			return r, nil
		}
		if errors.Is(err, sql.ErrNoRows) {
			var collision bool
			err = q.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM author_lists WHERE name=?)", item.Label).Scan(&collision)
			if collision {
				r.State, r.Code = "conflict", "identity_conflict"
				return r, err
			}
		}
	} else if item.Kind == "timelineMark" {
		r, err = s.inspectNativeMark(ctx, q, proofs, item, binding, r)
		if err != nil || r.State != "ready" {
			return r, err
		}
		revision = *r.TargetRevision
	} else {
		var author model.Author
		author, err = store.Get[model.Author](ctx, q, "SELECT data FROM authors WHERE id=?", r.TargetObjectID)
		if err == nil {
			var ours bool
			err = q.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM native_delivery_bindings WHERE source_instance=? AND kind=? AND object_id=? AND target_id=?)",
				plan.SourceInstanceID, item.Kind, item.SourceObjectID, r.TargetObjectID).Scan(&ours)
			if err != nil {
				return r, err
			}
			if !ours || author.Placeholder || author.Kind != item.Profile.AuthorKind {
				r.State, r.Code = "conflict", "identity_conflict"
				return r, nil
			}
			current, err := profileBlock(ctx, q, r.TargetObjectID, item.Block)
			if err != nil {
				return r, err
			}
			revision = current.Revision
			if item.Block == "x" {
				subscription, err := savedAuthorUpdates(ctx, q, r.TargetObjectID)
				if err != nil {
					return r, err
				}
				if subscription.TrackUpdates {
					r.TargetRevision = &revision
					r.State, r.Code = "preserved", "human_protected"
					return r, nil
				}
			}
			if current.Source != "native_delivery" && revision > 0 {
				r.TargetRevision = &revision
				r.State, r.Code = "preserved", "human_protected"
				return r, nil
			}
		} else if errors.Is(err, sql.ErrNoRows) {
			var collision bool
			if item.Profile.AuthorKind == "regular" {
				names, _ := authorlist.Names(item.Label)
				err = q.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM author_lookup_keys WHERE identity_key=?)", authorlist.IdentityKey(names[0])).Scan(&collision)
			} else {
				err = q.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM authors WHERE name=? AND (json_extract(data,'$.kind')='cos' OR id='unknown'))", item.Label).Scan(&collision)
			}
			if collision {
				r.State, r.Code = "conflict", "identity_conflict"
				return r, err
			}
		}
	}
	if err != nil {
		return r, err
	}
	r.TargetRevision = &revision
	if binding.targetID != "" && binding.targetRevision != revision {
		r.State, r.Code = "preserved", "human_protected"
	} else if binding.targetID != "" && binding.hash == hash {
		r.State, r.Code = "preserved", "already_current"
	} else if revision > 0 && plan.ConflictPolicy == "keep" {
		r.State, r.Code = "preserved", "keep_existing"
	}
	return r, nil
}

func nativeReceiptID(plan NativePlan, item nativeItem, hash string) string {
	return nativeID("native-receipt", plan.TargetInstanceID, plan.SourceInstanceID, nativeItemKey(item), strconv.FormatInt(item.SourceRevision, 10), hash)
}
