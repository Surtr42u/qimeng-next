package service

import (
	"context"
	"errors"
	"time"

	"qimeng.local/next/server/internal/authorlist"
	"qimeng.local/next/server/internal/store"
)

func sameNativeTarget(a, b NativeItemResult) bool {
	if a.TargetObjectID != b.TargetObjectID || a.State != b.State || a.Code != b.Code {
		return false
	}
	if a.TargetRevision == nil || b.TargetRevision == nil {
		return a.TargetRevision == nil && b.TargetRevision == nil
	}
	return *a.TargetRevision == *b.TargetRevision
}

func (s *Service) CommitNativeDelivery(ctx context.Context, w *Write, id, fingerprint string, proofs *NativeVideoProofs) (NativePlan, error) {
	var plan NativePlan
	if err := Authorize(w.Principal, "admin", ""); err != nil {
		return plan, err
	}
	q, err := w.Q()
	if err != nil {
		return plan, err
	}
	plan, err = store.Get[NativePlan](ctx, q, "SELECT data FROM native_delivery_plans WHERE id=?", id)
	if err != nil {
		return plan, Normalize(err)
	}
	if fingerprint != plan.Fingerprint || plan.TargetInstanceID != s.InstanceID {
		return plan, Fail(409, "native_plan_changed", "预览指纹或目标实例不符。")
	}
	if plan.State != "prepared" {
		return plan, nil
	}
	var raw []byte
	if err = q.QueryRowContext(ctx, "SELECT payload FROM native_delivery_plans WHERE id=?", id).Scan(&raw); err != nil {
		return plan, err
	}
	pkg, err := readNativePackage(raw, plan.SourceInstanceID, plan.TargetInstanceID, plan.PackageID)
	if err != nil {
		return plan, err
	}
	if len(pkg.Items) != len(plan.Items) {
		return plan, Fail(409, "native_plan_changed", "预览与暂存的原生对象不符。")
	}
	plan.State = "applied"
	for i, item := range pkg.Items {
		current, err := s.inspectNativeItem(ctx, q, plan, item, proofs)
		if err != nil {
			return plan, err
		}
		if current.ReceiptID == "" {
			hash, err := nativeItemHash(item)
			if err != nil {
				return plan, err
			}
			if !sameNativeTarget(current, plan.Items[i]) {
				if current.State != "conflict" {
					current.State, current.Code = "conflict", "target_changed"
				}
			} else if current.State == "ready" {
				if _, err = q.ExecContext(ctx, "SAVEPOINT native_item"); err != nil {
					return plan, err
				}
				revision, err := s.applyNativeObject(ctx, w, q, plan, item, current)
				if err != nil {
					var issue *Error
					if !errors.As(err, &issue) || issue.Status != 409 {
						return plan, err
					}
					if _, err = q.ExecContext(ctx, "ROLLBACK TO native_item"); err != nil {
						return plan, err
					}
					current.State, current.Code = "conflict", "target_changed"
				} else {
					current.State, current.TargetRevision = "applied", &revision
					if _, err = q.ExecContext(ctx, `INSERT INTO native_delivery_bindings(source_instance,kind,object_id,block,source_revision,payload_hash,target_id,target_revision)
						VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(source_instance,kind,object_id,block) DO UPDATE SET source_revision=excluded.source_revision,
						payload_hash=excluded.payload_hash,target_id=excluded.target_id,target_revision=excluded.target_revision`,
						plan.SourceInstanceID, item.Kind, item.SourceObjectID, item.Block, item.SourceRevision, hash, current.TargetObjectID, revision); err != nil {
						return plan, err
					}
				}
				if _, err = q.ExecContext(ctx, "RELEASE native_item"); err != nil {
					return plan, err
				}
			}
			current.ReceiptID = nativeReceiptID(plan, item, hash)
			record, err := store.Encode(current)
			if err != nil {
				return plan, err
			}
			if _, err = q.ExecContext(ctx, `INSERT INTO native_delivery_receipts(target_instance,source_instance,kind,object_id,block,source_revision,payload_hash,data)
				VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(target_instance,source_instance,kind,object_id,block,source_revision) DO NOTHING`,
				plan.TargetInstanceID, plan.SourceInstanceID, item.Kind, item.SourceObjectID, item.Block, item.SourceRevision, hash, record); err != nil {
				return plan, err
			}
		}
		if current.State == "conflict" || current.State == "preserved" && current.Code != "keep_existing" && current.Code != "already_current" {
			plan.State = "partial"
		}
		plan.Items[i] = current
	}
	record, err := store.Encode(plan)
	if err != nil {
		return plan, err
	}
	_, err = q.ExecContext(ctx, "UPDATE native_delivery_plans SET state=?,data=?,payload=NULL WHERE id=?", plan.State, record, id)
	return plan, err
}

func (s *Service) applyNativeObject(ctx context.Context, w *Write, q store.Querier, plan NativePlan, item nativeItem, target NativeItemResult) (int64, error) {
	if item.Timeline != nil {
		return applyNativeMark(ctx, q, item, target)
	}
	if item.Kind == "authorList" {
		list, err := s.importAuthorList(ctx, w, AuthorListInput{Name: item.Label, Content: *item.Text, ExpectedRevision: target.TargetRevision, ConflictResolution: "keep"}, func(kind string) string {
			if kind == "author-list" {
				return target.TargetObjectID
			}
			return nativeID("native-list-member", plan.PackageID, nativeItemKey(item), kind)
		})
		if err != nil {
			return 0, err
		}
		if list.List == nil || list.List.ID != target.TargetObjectID {
			return 0, Fail(409, "native_identity_changed", "目标片段身份变化。")
		}
		return list.List.Revision, nil
	}
	var exists bool
	if err := q.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM authors WHERE id=?)", target.TargetObjectID).Scan(&exists); err != nil {
		return 0, err
	}
	now := time.Now().UTC()
	if !exists {
		names, err := authorlist.Names(item.Label)
		if err != nil {
			return 0, err
		}
		author := newTextAuthor(target.TargetObjectID, names, now)
		author.Kind = item.Profile.AuthorKind
		if author.Kind == "cos" {
			author.Name = item.Label
		}
		if err = saveNewRegularAuthor(ctx, q, author); err != nil {
			return 0, err
		}
	}
	block, err := profileBlock(ctx, q, target.TargetObjectID, item.Block)
	if err != nil {
		return 0, err
	}
	// Only an unchanged block previously delivered from this exact source can
	// be updated. This also avoids merging candidates or touching subscriptions.
	if target.TargetRevision == nil || block.Revision != *target.TargetRevision {
		return 0, Conflict(block.Revision)
	}
	block.Values, block.Evidence = item.Profile.Values, item.Profile.Evidence
	block.Revision++
	block.Source, block.UpdatedAt, block.CandidateID = "native_delivery", &now, ""
	return block.Revision, saveProfileBlock(ctx, q, block)
}
