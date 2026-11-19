package service

import (
	"context"
	"strings"

	"qimeng.local/next/server/internal/model"
)

func authorizePlanCommit(p model.Principal, batchID string) error {
	if err := Authorize(p, "organize", batchID); err != nil {
		return err
	}
	return Authorize(p, "ingest", batchID)
}

func (s *Service) commitPlanItem(ctx context.Context, principal model.Principal, plan model.Plan, item model.PlanItem, declared []string) (Result, error) {
	release, err := s.acquireUploadVerification(ctx)
	if err != nil {
		return Result{}, err
	}
	defer release()
	principal, err = s.currentWritePrincipal(ctx, principal)
	if err != nil {
		return Result{}, err
	}
	if err = authorizePlanCommit(principal, plan.BatchID); err != nil {
		return Result{}, err
	}
	prepared, inspectionErr := s.preparePlanUploadInspection(ctx, principal, plan, item)
	defer prepared.close()
	op := "commitPlanItem:" + plan.ID + ":" + item.UploadID
	return s.Run(ctx, principal, op, plan.Fingerprint, []byte(plan.Fingerprint), func(w *Write) (any, int, error) {
		if err := ctx.Err(); err != nil {
			return nil, 0, err
		}
		u, err := s.checkPlanApplication(ctx, w.Principal, plan, item)
		if err != nil {
			return nil, 0, err
		}
		if u.State == "ingested" {
			a, err := s.Asset(ctx, u.AssetID)
			return model.UploadResult{Upload: u, Asset: &a}, 200, err
		}
		if u.State == "duplicate" {
			return model.UploadResult{Upload: u}, 200, nil
		}
		if syncPlatformSupported {
			w.preparedUpload = u.ID
		}
		if u.ErrorCode == "publication_pending" {
			out, err := s.resumePlanPublication(ctx, w, u)
			return out, 200, err
		}
		if inspectionErr != nil {
			return nil, 0, inspectionErr
		}
		if syncPlatformSupported {
			if prepared == nil {
				return nil, 0, Fail(409, "plan_input_conflict", "暂存输入状态已变化，请重新读取计划。")
			}
			if err = s.checkUploadInspection(ctx, u, prepared); err != nil {
				return nil, 0, err
			}
		}
		out, err := s.ingestPrepared(ctx, w, u, &item, prepared, declared)
		return out, 200, err
	})
}

func (s *Service) preparePlanUploadInspection(ctx context.Context, p model.Principal, plan model.Plan, item model.PlanItem) (*uploadInspection, error) {
	if !syncPlatformSupported {
		return nil, nil
	}
	u, err := s.Upload(ctx, p, item.UploadID)
	if err != nil {
		return nil, err
	}
	if u.BatchID != plan.BatchID || u.State != "staged" || u.Offset != u.Size || u.Hash != item.ExpectedHash || u.ErrorCode == "publication_pending" {
		return nil, nil
	}
	if err = s.uploadsAllowed(ctx); err != nil {
		return nil, err
	}
	if err = s.checkUploadLibrary(ctx, u.LibraryID); err != nil {
		return nil, err
	}
	return s.openUploadInspection(ctx, u, true)
}

// checkPlanApplication runs under the catalog writer, both before claiming the
// file intent and after any unlocked publication work. Earlier committed items
// are counted against the plan's batch revision and excluded from its namespace.
func (s *Service) checkPlanApplication(ctx context.Context, p model.Principal, plan model.Plan, item model.PlanItem) (model.Upload, error) {
	var current model.Upload
	if err := authorizePlanCommit(p, plan.BatchID); err != nil {
		return current, err
	}
	batch, err := s.Batch(ctx, plan.BatchID)
	if err != nil {
		return current, err
	}
	if batch.State == "cancelled" {
		return current, Fail(409, "batch_cancelled", "批次已取消，已提交项目保留。")
	}
	ignore := map[string]bool{}
	ownPaths := map[string]bool{}
	var applied int64
	for _, planned := range plan.Items {
		ignore[digest([]byte("asset:" + planned.UploadID))[:32]] = true
		u, err := s.Upload(ctx, p, planned.UploadID)
		if err != nil {
			return current, err
		}
		if u.ID == item.UploadID {
			current = u
		}
		switch u.State {
		case "ingested":
			if err = s.verifyAppliedPlanItem(ctx, plan.ID, planned, u); err != nil {
				return current, err
			}
			ownPaths[planned.RelativePath] = true
			applied++
		case "duplicate":
			applied++
		}
	}
	if batch.Revision != plan.BatchRevision+applied {
		return current, Conflict(batch.Revision)
	}
	if current.State == "ingested" || current.State == "duplicate" {
		return current, nil
	}
	if current.BatchID != batch.ID || current.LibraryID != batch.LibraryID || current.Mode != "intelligent" || current.State != "staged" || current.Offset != current.Size {
		return current, Fail(409, "plan_input_conflict", "输入状态已改变。")
	}
	if current.Hash == "" || current.Hash != item.ExpectedHash {
		return current, Fail(409, "plan_content_changed", "计划引用的内容已改变。")
	}
	if item.NamespaceFingerprint == "" {
		// Retain the original precondition for plans issued by older Next builds.
		scope, err := s.namespaceFingerprint(ctx, batch.LibraryID, ignore)
		if err != nil {
			return current, err
		}
		if scope != plan.DirectoryFingerprint {
			return current, Fail(409, "namespace_changed", "已签发的旧计划需重新核对。")
		}
		return current, nil
	}
	dir := planDirectory(item.RelativePath)
	names, err := s.directoryNames(ctx, batch.LibraryID, dir)
	if err != nil {
		return current, err
	}
	for _, name := range names {
		if name == item.FileName {
			if current.ErrorCode != "publication_pending" {
				return current, Fail(409, "path_occupied", "当前计划的实际目标已被占用。")
			}
			// The intent separately verifies the reserved target's exact object.
			ownPaths[item.RelativePath] = true
		}
	}
	scope, err := planItemScope(ctx, s.DB.DB, batch.LibraryID, item, names, ownPaths, ignore)
	if err != nil {
		return current, err
	}
	if scope != item.NamespaceFingerprint {
		return current, Fail(409, "namespace_changed", "该命名族的占号、集合或引用对象已变化，请重新核对。")
	}
	return current, nil
}

func (s *Service) resumePlanPublication(ctx context.Context, w *Write, u model.Upload) (model.UploadResult, error) {
	j, err := s.existingFileIntent(ctx, w, fileIntent{ID: digest([]byte("publish:" + u.ID))[:32], Kind: "ingest", UploadID: u.ID})
	if err != nil {
		return model.UploadResult{}, err
	}
	if err = s.checkUploadPublication(ctx, w.Principal, u, &j); err != nil {
		return model.UploadResult{}, err
	}
	asset, err := s.runPreparedFile(ctx, w, j)
	if err != nil {
		return model.UploadResult{}, err
	}
	return model.UploadResult{Upload: publishedUpload(u, asset), Asset: &asset}, nil
}

func (s *Service) checkPlannedPublication(ctx context.Context, p model.Principal, u model.Upload, j *fileIntent) error {
	identity, ok := strings.CutPrefix(j.Operation, "commitPlanItem:")
	planID, uploadID, bound := strings.Cut(identity, ":")
	if !ok || !bound || uploadID != u.ID {
		return Fail(409, "plan_identity_changed", "计划与发布输入身份不匹配。")
	}
	plan, err := s.Plan(ctx, p, u.BatchID, planID)
	if err != nil {
		return err
	}
	if plan.Fingerprint != j.Key || digest([]byte(plan.Fingerprint)) != j.InputHash {
		return Fail(409, "plan_identity_changed", "发布意图已绑定另一个计划版本。")
	}
	for _, item := range plan.Items {
		if item.UploadID != u.ID {
			continue
		}
		if item.RelativePath != j.After.RelativePath || item.ExpectedHash != j.After.Hash || item.CollectionID != j.After.CollectionID || item.Page != j.After.Page {
			break
		}
		if _, err = s.checkPlanApplication(ctx, p, plan, item); err != nil {
			return err
		}
		if err = preserveUploadSelection(p, u, item.AuthorIDs, item.SourceIDs, item.CharacterIDs); err != nil {
			return err
		}
		authors, err := s.resolveAuthors(ctx, u.LibraryID, item.AuthorIDs)
		if err != nil {
			return err
		}
		return s.checkPlanResearch(ctx, p, u, authors)
	}
	return Fail(409, "plan_identity_changed", "计划与发布输入身份不匹配。")
}
