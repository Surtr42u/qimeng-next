package service

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path"
	"strings"
	"time"

	"qimeng.local/next/server/internal/domain"
	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/store"
)

const MaxChunkBytes int64 = 8 << 20
const MaxActiveUploads = 32
const MaxBatchItems = 100

func uploadPath(id string) string { return "uploads/" + id + ".part" }
func (s *Service) Upload(ctx context.Context, p model.Principal, id string) (model.Upload, error) {
	u, e := store.Get[model.Upload](ctx, s.DB.DB, "SELECT data FROM uploads WHERE id=?", id)
	if e != nil {
		return u, Normalize(e)
	}
	if e = Authorize(p, "read", u.BatchID); e != nil {
		return u, e
	}
	var state string
	e = s.DB.DB.QueryRowContext(ctx, "SELECT state FROM file_intents WHERE id=?", digest([]byte("publish:" + u.ID))[:32]).Scan(&state)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return u, e
	}
	if state == "committed" {
		u.ErrorCode = "cleanup_pending"
		u.ErrorMessage = "媒体已入库，旧版或临时文件清理待恢复。"
	} else if state == "prepared" {
		u.ErrorCode = "publication_pending"
		u.ErrorMessage = "发布已经开始，须沿原操作身份恢复。"
	}
	return u, nil
}
func (s *Service) CreateUpload(ctx context.Context, w *Write, in model.UploadInput) (model.Upload, error) {
	explicitTaxonomy := w.Principal.Has("admin") && (in.SourceIDs != nil || in.CharacterIDs != nil)
	if e := Authorize(w.Principal, "ingest", in.BatchID); e != nil {
		return model.Upload{}, e
	}
	if e := s.uploadsAllowed(ctx); e != nil {
		return model.Upload{}, e
	}
	library, e := s.library(ctx, in.LibraryID)
	if e != nil {
		return model.Upload{}, e
	}
	if !library.Enabled {
		return model.Upload{}, Fail(409, "library_disabled", "目标库已停用，不能接收新上传。")
	}
	if !domain.ValidName(in.FileName) || !domain.ValidMediaPath(in.RelativeDirectory) || in.Size < 1 || len(in.Note) > 10000 || (in.Mode != "ordinary" && in.Mode != "intelligent") {
		return model.Upload{}, Fail(400, "invalid_upload", "上传名称、目标、长度或模式无效。")
	}
	if in.OriginalFileName == "" && in.BatchID == "" {
		in.OriginalFileName = in.FileName
	}
	if len(in.OriginalFileName) > 1024 || strings.ContainsAny(in.OriginalFileName, "/\\:\x00") {
		return model.Upload{}, Fail(400, "invalid_source_name", "原始名称只能是文件名，不能包含绝对路径。")
	}
	sourcePath := strings.ReplaceAll(in.SourceRelativePath, "\\", "/")
	if len(sourcePath) > 4096 || strings.ContainsAny(sourcePath, ":\x00") || strings.HasPrefix(sourcePath, "/") || sourcePath == ".." || strings.HasPrefix(sourcePath, "../") || (sourcePath != "" && path.Clean(sourcePath) != sourcePath) {
		return model.Upload{}, Fail(400, "invalid_source_path", "来源线索必须是相对路径。")
	}
	in.SourceRelativePath = sourcePath
	_, mime, supported := domain.MediaType(in.FileName)
	if !supported {
		return model.Upload{}, Fail(415, "unsupported_media", "不支持该文件类型；不自动解压文件。")
	}
	if !domain.CompatibleMediaDeclaration(in.MimeType, mime) {
		return model.Upload{}, Fail(415, "mime_mismatch", "声明的 MIME 与文件扩展名不一致。")
	}
	in.MimeType = mime
	if in.ExpectedHash != "" {
		if _, e := hex.DecodeString(in.ExpectedHash); e != nil || len(in.ExpectedHash) != 64 {
			return model.Upload{}, Fail(400, "invalid_hash", "源摘要必须是完整 SHA-256。")
		}
		in.ExpectedHash = strings.ToLower(in.ExpectedHash)
	}
	if in.Family != "" && !domain.ValidName(in.Family) {
		return model.Upload{}, Fail(400, "invalid_name", "自动命名基名无效。")
	}
	if len(in.AuthorAcquisitionSources) > 0 {
		if !w.Principal.Has("admin") {
			return model.Upload{}, Fail(403, "scope_denied", "接收任务不能改写已有作者的获取渠道。")
		}
		in.AuthorAcquisitionSources, e = authorSourceTerms(in.AuthorAcquisitionSources)
		if e != nil {
			return model.Upload{}, e
		}
	}
	if in.AcquisitionSources == nil {
		in.AcquisitionSources = []string{}
	}
	in.AcquisitionSources, e = cleanStrings(in.AcquisitionSources, 100, 2000)
	if e != nil {
		return model.Upload{}, e
	}
	taxonomy := model.Asset{}
	declaredSources, e := cleanStrings(append([]string{}, in.SourceIDs...), 20, 100)
	if e != nil {
		return model.Upload{}, e
	}
	if e = s.resolveTaxonomy(ctx, &taxonomy, in.SourceIDs, in.CharacterIDs); e != nil {
		return model.Upload{}, e
	}
	in.SourceIDs = taxonomy.SourceIDs
	in.CharacterIDs = taxonomy.CharacterIDs
	if in.Family != "" {
		in.Family = domain.NamingFamily(taxonomy.Source, taxonomy.Characters, in.Family)
		if !domain.ValidName(in.Family) {
			return model.Upload{}, Fail(400, "invalid_name", "规范出处与角色不能生成合法目标基名。")
		}
	}
	if in.SourceMtime != nil && (in.SourceMtime.Year() < 1970 || in.SourceMtime.After(time.Now().Add(24*time.Hour))) {
		return model.Upload{}, Fail(400, "invalid_time", "源文件时间无效。")
	}
	if e = s.prepareCOSUpload(ctx, &in); e != nil {
		return model.Upload{}, e
	}
	if len(in.AuthorIDs) == 0 && in.AuthorID != "" {
		in.AuthorIDs = []string{in.AuthorID}
	}
	authors, e := s.resolveAuthors(ctx, in.LibraryID, in.AuthorIDs)
	if e != nil {
		return model.Upload{}, e
	}
	in.AuthorIDs = []string{}
	for _, a := range authors {
		in.AuthorIDs = append(in.AuthorIDs, a.ID)
	}
	in.AuthorID = authors[0].ID
	if in.CollectionID != "" {
		c, e := store.Get[model.Collection](ctx, s.DB.DB, "SELECT data FROM collections WHERE id=?", in.CollectionID)
		if e != nil {
			return model.Upload{}, Normalize(e)
		}
		if c.LibraryID != in.LibraryID {
			return model.Upload{}, Fail(409, "collection_library_mismatch", "集合属于其他库。")
		}
		if c.Kind == "short_drama" {
			if domain.NumberClass(in.FileName) != "video" {
				return model.Upload{}, Fail(400, "drama_requires_video", "短剧集合只接收视频。")
			}
			in.Family = c.Name
			in.RelativeDirectory = c.Name
		} else {
			if domain.NumberClass(in.FileName) != "image" {
				return model.Upload{}, Fail(400, "image_group_requires_image", "组图集合只接收图片。")
			}
			if in.Family == "" {
				in.Family = domain.NamingFamily(taxonomy.Source, taxonomy.Characters, c.Name)
			}
		}
	}
	if in.Family != "" && !domain.ValidName(in.Family) {
		return model.Upload{}, Fail(400, "invalid_name", "规范命名基名无效。")
	}
	if !domain.ValidMediaPath(in.RelativeDirectory) {
		return model.Upload{}, Fail(400, "invalid_path", "目标为保留目录。")
	}
	if in.ReplaceAssetID != "" {
		if !w.Principal.Has("admin") {
			return model.Upload{}, Fail(403, "replacement_requires_owner", "AI 新增权限不包含替换既有资产。")
		}
		if in.Mode != "ordinary" {
			return model.Upload{}, Fail(400, "replacement_mode", "明确替换须使用普通上传通道。")
		}
		a, e := s.Asset(ctx, in.ReplaceAssetID)
		if e != nil {
			return model.Upload{}, e
		}
		if a.LibraryID != in.LibraryID || a.Trashed || a.ContentVersion != in.ExpectedContentVersion {
			return model.Upload{}, Fail(409, "replacement_conflict", "替换对象或内容版本已变化。")
		}
	}
	if in.Mode == "intelligent" && in.BatchID == "" {
		return model.Upload{}, Fail(400, "batch_required", "智能整理须先创建接收批次。")
	}
	if in.BatchID == "" && in.InputID != "" {
		return model.Upload{}, Fail(400, "input_batch_required", "原始输入ID必须属于已登记的批次。")
	}
	if in.BatchID != "" {
		if e = s.validateUploadInput(ctx, &in); e != nil {
			return model.Upload{}, e
		}
		b, e := s.Batch(ctx, in.BatchID)
		if e != nil {
			return model.Upload{}, e
		}
		if b.LibraryID != in.LibraryID || b.Mode != in.Mode || b.State == "cancelled" {
			return model.Upload{}, Fail(409, "batch_mismatch", "批次目标或状态不允许接收文件。")
		}
		var count int
		if e = s.DB.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM uploads WHERE batch_id=?", in.BatchID).Scan(&count); e != nil {
			return model.Upload{}, e
		}
		if count >= MaxBatchItems {
			return model.Upload{}, Fail(409, "batch_limit", "每批最多接收 100 项。")
		}
	}
	var active int
	var reserved int64
	if e := s.DB.DB.QueryRowContext(ctx, "SELECT count(*) FROM uploads WHERE state='receiving'").Scan(&active); e != nil {
		return model.Upload{}, e
	}
	if e := s.DB.DB.QueryRowContext(ctx, "SELECT COALESCE(sum(json_extract(u.data,'$.size')-json_extract(u.data,'$.offset')),0) FROM uploads u JOIN libraries l ON l.id=json_extract(u.data,'$.libraryId') WHERE u.state='receiving' AND COALESCE(json_extract(l.data,'$.storageId'),'data')=?", library.StorageID).Scan(&reserved); e != nil {
		return model.Upload{}, e
	}
	if active >= MaxActiveUploads {
		return model.Upload{}, Fail(429, "upload_capacity", "同时接收的上传已满，请稍后重试。")
	}
	diskPath, e := s.Root.LibraryPath(in.LibraryID)
	if e != nil {
		return model.Upload{}, e
	}
	free, e := freeBytes(diskPath)
	if e != nil {
		return model.Upload{}, e
	}
	if in.Size > free-s.Config.ReserveBytes-reserved {
		return model.Upload{}, Fail(507, "insufficient_storage", "可用空间不足以保留上传与存储余量。")
	}
	u := model.Upload{UploadInput: in, ID: w.ID("upload"), State: "receiving", ExplicitTaxonomy: explicitTaxonomy, CreatedAt: time.Now().UTC()}
	s.Root.RegisterUpload(u.ID, u.LibraryID)
	if e = s.ensureParent(uploadPath(u.ID)); e != nil {
		return u, e
	}
	f, e := s.Root.OpenFile(uploadPath(u.ID), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if errors.Is(e, os.ErrExist) {
		if e = s.safe(uploadPath(u.ID)); e != nil {
			return u, e
		}
		info, se := s.Root.Stat(uploadPath(u.ID))
		if se != nil {
			return u, se
		}
		if info.Size() != 0 {
			return u, Fail(409, "upload_orphan_conflict", "同操作残留内容需要核对。")
		}
		f, e = s.Root.OpenFile(uploadPath(u.ID), os.O_WRONLY, 0600)
	}
	if e != nil {
		return u, e
	}
	if e = f.Sync(); e != nil {
		f.Close()
		return u, e
	}
	if e = f.Close(); e != nil {
		return u, e
	}
	if e = s.syncParent(uploadPath(u.ID)); e != nil {
		return u, e
	}
	q, e := w.Q()
	if e != nil {
		return u, e
	}
	if u.BatchID == "" {
		b := newBatch(w.ID("batch"), in.LibraryID, in.Mode, in.FileName, 0)
		u.BatchID = b.ID
		u.InputID = w.ID("input")
		if e = store.SaveBatch(ctx, q, b); e != nil {
			return u, e
		}
		input := model.IntakeInput{ID: u.InputID, OriginalFileName: u.OriginalFileName, SourceRelativePath: u.SourceRelativePath, Size: &u.Size, ExpectedHash: u.ExpectedHash}
		if _, e = sealIntakeInputs(ctx, q, b, []model.IntakeInput{input}, true); e != nil {
			return u, e
		}
	}
	if e = bindUploadInput(ctx, q, u); e != nil {
		return u, e
	}
	if e = store.SaveUpload(ctx, q, u); e != nil {
		return u, e
	}
	declared, e := store.Encode(declaredSources)
	if e != nil {
		return u, e
	}
	if _, e = q.ExecContext(ctx, "UPDATE uploads SET declared_source_ids=? WHERE id=?", declared, u.ID); e != nil {
		return u, e
	}
	if e = s.refreshBatch(ctx, q, u.BatchID); e != nil {
		return u, e
	}
	return u, nil
}
func (s *Service) AppendUpload(ctx context.Context, w *Write, id string, offset int64, body []byte) (model.Upload, error) {
	if e := s.uploadsAllowed(ctx); e != nil {
		return model.Upload{}, e
	}
	u, e := s.Upload(ctx, w.Principal, id)
	if e != nil {
		return u, e
	}
	if e = Authorize(w.Principal, "ingest", u.BatchID); e != nil {
		return u, e
	}
	if u.State != "receiving" {
		return u, Fail(409, "upload_sealed", "上传已经封存，不能再写分片。")
	}
	if len(body) == 0 || int64(len(body)) > MaxChunkBytes || offset < 0 || offset > u.Size || int64(len(body)) > u.Size-offset {
		return u, Fail(400, "invalid_chunk", "分片长度或偏移无效。")
	}
	b, e := s.Batch(ctx, u.BatchID)
	if e != nil {
		return u, e
	}
	if b.State == "cancelled" {
		return u, Fail(409, "batch_cancelled", "批次已取消。")
	}
	if e = s.safe(uploadPath(u.ID)); e != nil {
		return u, e
	}
	f, e := s.Root.OpenFile(uploadPath(u.ID), os.O_RDWR, 0600)
	if e != nil {
		return u, e
	}
	defer f.Close()
	if offset < u.Offset {
		if offset+int64(len(body)) > u.Offset {
			return u, Fail(409, "offset_conflict", "请从服务端已保存偏移继续。")
		}
		old := make([]byte, len(body))
		if _, e = f.ReadAt(old, offset); e != nil {
			return u, e
		}
		if !bytes.Equal(old, body) {
			return u, Fail(409, "chunk_conflict", "重复分片的内容已改变。")
		}
		return u, nil
	}
	if offset != u.Offset {
		return u, Fail(409, "offset_conflict", "请读取当前偏移后继续上传。")
	}
	diskPath, e := s.Root.LibraryPath(u.LibraryID)
	if e != nil {
		return u, e
	}
	free, e := freeBytes(diskPath)
	if e != nil {
		return u, e
	}
	if free-int64(len(body)) < s.Config.ReserveBytes {
		return u, Fail(507, "insufficient_storage", "存储余量不足，保留已接收分片。")
	}
	// An uncommitted tail after a process crash is discarded; only the persisted
	// offset is authoritative. Bytes are flushed before advancing that offset.
	if e = f.Truncate(u.Offset); e != nil {
		return u, e
	}
	if _, e = f.WriteAt(body, offset); e != nil {
		return u, e
	}
	if e = f.Sync(); e != nil {
		return u, e
	}
	u.Offset += int64(len(body))
	q, e := w.Q()
	if e != nil {
		return u, e
	}
	return u, store.SaveUpload(ctx, q, u)
}
func (s *Service) inspectUpload(ctx context.Context, u model.Upload) (string, error) {
	name := uploadPath(u.ID)
	if err := s.safe(name); err != nil {
		return "", err
	}
	file, err := s.Root.Open(name)
	if err != nil {
		return "", err
	}
	defer file.Close()
	return inspectUploadContent(ctx, file, u)
}
func (s *Service) CompleteUpload(ctx context.Context, w *Write, id string) (model.UploadResult, error) {
	return s.completeUpload(ctx, w, id, nil, nil)
}

func (s *Service) completeUpload(ctx context.Context, w *Write, id string, prepared *uploadInspection, inspectionErr error) (model.UploadResult, error) {
	u, e := s.Upload(ctx, w.Principal, id)
	if e != nil {
		return model.UploadResult{}, e
	}
	if e = Authorize(w.Principal, "ingest", u.BatchID); e != nil {
		return model.UploadResult{}, e
	}
	if u.State == "ingested" {
		a, e := s.Asset(ctx, u.AssetID)
		if e != nil {
			return model.UploadResult{Upload: u}, e
		}
		return model.UploadResult{Upload: u, Asset: &a}, nil
	}
	if u.State == "duplicate" {
		return model.UploadResult{Upload: u}, nil
	}
	if u.State == "cancelled" {
		return model.UploadResult{}, Fail(409, "upload_cancelled", "上传已取消。")
	}
	if e = s.uploadsAllowed(ctx); e != nil {
		return model.UploadResult{}, e
	}
	b, e := s.Batch(ctx, u.BatchID)
	if e != nil {
		return model.UploadResult{}, e
	}
	if b.State == "cancelled" {
		return model.UploadResult{}, Fail(409, "batch_cancelled", "批次已取消，已接收内容保留。")
	}
	if u.Offset != u.Size {
		return model.UploadResult{}, Fail(409, "upload_incomplete", "尚未接收完整文件。")
	}
	if inspectionErr != nil {
		return model.UploadResult{}, inspectionErr
	}
	if prepared != nil {
		if e = s.checkUploadInspection(ctx, u, prepared); e != nil {
			return model.UploadResult{}, e
		}
	}
	if u.Hash == "" {
		if prepared != nil {
			u.Hash = prepared.hash
		} else {
			u.Hash, e = s.inspectUpload(ctx, u)
		}
		if e != nil {
			return model.UploadResult{}, e
		}
		u.State = "staged"
		if e = store.SaveUpload(ctx, s.DB.DB, u); e != nil {
			return model.UploadResult{}, e
		}
	}
	if u.Mode == "intelligent" {
		q, e := w.Q()
		if e != nil {
			return model.UploadResult{}, e
		}
		if e = s.refreshBatch(ctx, q, u.BatchID); e != nil {
			return model.UploadResult{}, e
		}
		return model.UploadResult{Upload: u}, nil
	}
	return s.ingestPrepared(ctx, w, u, nil, prepared, nil)
}
func (s *Service) ingestPrepared(ctx context.Context, w *Write, u model.Upload, item *model.PlanItem, prepared *uploadInspection, declaredSources []string) (model.UploadResult, error) {
	if e := s.uploadsAllowed(ctx); e != nil {
		return model.UploadResult{}, e
	}
	if prepared == nil {
		if e := s.verify(uploadPath(u.ID), u.Hash, u.Size); e != nil {
			return model.UploadResult{}, e
		}
	}
	var duplicate int
	if e := s.DB.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM assets WHERE hash=? AND size=? AND id!=?", u.Hash, u.Size, u.ReplaceAssetID).Scan(&duplicate); e != nil {
		return model.UploadResult{}, e
	}
	if duplicate > 0 && u.ReplaceAssetID == "" {
		u.State = "duplicate"
		u.ErrorCode = "duplicate_content"
		u.ErrorMessage = "已有相同内容，请由用户决定本项处理；原文件保留。"
		q, e := w.Q()
		if e != nil {
			return model.UploadResult{}, e
		}
		if e = store.SaveUpload(ctx, q, u); e != nil {
			return model.UploadResult{}, e
		}
		if e = s.refreshBatch(ctx, q, u.BatchID); e != nil {
			return model.UploadResult{}, e
		}
		return model.UploadResult{Upload: u}, nil
	}
	if u.ReplaceAssetID == "" {
		var pending bool
		e := s.DB.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM file_intents
			WHERE state='prepared' AND kind IN ('ingest','replace') AND json_extract(data,'$.UploadID')!=?
			AND json_extract(data,'$.After.hash')=? AND json_extract(data,'$.After.size')=?)`, u.ID, u.Hash, u.Size).Scan(&pending)
		if e != nil {
			return model.UploadResult{}, e
		}
		if pending {
			return model.UploadResult{}, Fail(409, "publication_pending", "相同内容正在发布，请沿用原上传会话重试。")
		}
	}
	now := time.Now().UTC()
	kind, mime, _ := domain.MediaType(u.FileName)
	if u.CollectionID != "" && u.ReplaceAssetID == "" && item == nil && kind == "video" {
		// A staged upload follows the stable drama ID even if its display
		// name and the directory for future episodes changed meanwhile.
		if e := s.DB.DB.QueryRowContext(ctx, "SELECT name FROM collections WHERE id=?", u.CollectionID).Scan(&u.Family); e != nil {
			return model.UploadResult{}, Normalize(e)
		}
		u.RelativeDirectory = u.Family
	}
	authors, e := s.resolveAuthors(ctx, u.LibraryID, u.AuthorIDs)
	if e != nil {
		return model.UploadResult{}, e
	}
	author := authors[0]
	a := model.Asset{ID: digest([]byte("asset:" + u.ID))[:32], LibraryID: u.LibraryID, OriginalFileName: u.OriginalFileName, SourceRelativePath: u.SourceRelativePath, FileName: u.FileName, RelativePath: path.Join(u.RelativeDirectory, u.FileName), MediaType: kind, MimeType: mime, Size: u.Size, Hash: u.Hash, Revision: 1, TagsRevision: 1, ContentVersion: 1, AuthorID: author.ID, AuthorName: author.Name, AuthorStatus: "confirmed", CollectionID: u.CollectionID, Tags: []string{}, Characters: []string{}, Note: u.Note, MetadataSource: "upload", SourceMtime: u.SourceMtime, CreatedAt: now, UpdatedAt: now}
	setAuthors(&a, authors)
	a.AuthorsRevision = 1
	a.AcquisitionSources = u.AcquisitionSources
	if declaredSources == nil {
		declaredSources, e = uploadDeclaredSources(ctx, s.DB.DB, u)
		if e != nil {
			return model.UploadResult{}, e
		}
	}
	if e = s.resolveTaxonomy(ctx, &a, declaredSources, u.CharacterIDs); e != nil {
		return model.UploadResult{}, e
	}
	var before *model.Asset
	operation := "ingest"
	if u.ReplaceAssetID != "" {
		old, e := s.Asset(ctx, u.ReplaceAssetID)
		if e != nil {
			return model.UploadResult{}, e
		}
		if old.Trashed || old.ContentVersion != u.ExpectedContentVersion {
			return model.UploadResult{}, Fail(409, "replacement_conflict", "目标内容版本已变化。")
		}
		before = &old
		a = old
		a.Size = u.Size
		a.Hash = u.Hash
		a.MimeType = mime
		a.MediaType = kind
		a.ContentVersion++
		a.Revision++
		a.UpdatedAt = now
		a.SourceMtime = u.SourceMtime
		if !strings.EqualFold(path.Ext(old.FileName), path.Ext(u.FileName)) {
			a.FileName = strings.TrimSuffix(old.FileName, path.Ext(old.FileName)) + strings.ToLower(path.Ext(u.FileName))
		}
		a.RelativePath = path.Join(path.Dir(old.RelativePath), a.FileName)
		a.AcquisitionSources = appendUnique(a.AcquisitionSources, u.AcquisitionSources...)
		a.OriginalFileName = u.OriginalFileName
		a.SourceRelativePath = u.SourceRelativePath
		operation = "replace"
	} else if item != nil {
		if e = preserveUploadSelection(w.Principal, u, item.AuthorIDs, item.SourceIDs, item.CharacterIDs); e != nil {
			return model.UploadResult{}, e
		}
		if item.ExpectedHash != u.Hash {
			return model.UploadResult{}, Fail(409, "plan_content_changed", "计划引用的内容已改变。")
		}
		a.FileName = item.FileName
		a.RelativePath = item.RelativePath
		a.CollectionID = item.CollectionID
		a.Page = item.Page
		a.MetadataSource = "ai"
		authors, e := s.resolveAuthors(ctx, a.LibraryID, item.AuthorIDs)
		if e != nil {
			return model.UploadResult{}, e
		}
		if e = s.checkPlanResearch(ctx, w.Principal, u, authors); e != nil {
			return model.UploadResult{}, e
		}
		setAuthors(&a, authors)
		if e = s.resolveTaxonomy(ctx, &a, declaredSources, item.CharacterIDs); e != nil {
			return model.UploadResult{}, e
		}
	} else if u.CollectionID != "" && kind != "video" {
		sequence, e := s.appendSequence(ctx, u.CollectionID, u.RelativeDirectory, u.Family)
		if e != nil {
			return model.UploadResult{}, e
		}
		a.Page = sequence.MaxPage + 1
		a.FileName = domain.WorkFilename(sequence.Family, sequence.Separator, sequence.Number, sequence.Width, a.Page, path.Ext(u.FileName))
		a.RelativePath = path.Join(sequence.Directory, a.FileName)
	} else if u.Family != "" {
		names, e := s.directoryNames(ctx, u.LibraryID, u.RelativeDirectory)
		if e != nil {
			return model.UploadResult{}, e
		}
		number, width, sep := domain.Allocate(u.Family, domain.NumberClass(u.FileName), names)
		if u.CollectionID != "" {
			number, e = s.nextEpisode(ctx, u.CollectionID)
			if e != nil {
				return model.UploadResult{}, e
			}
			a.Page = number
		}
		a.FileName = domain.WorkFilename(u.Family, sep, number, width, 0, path.Ext(u.FileName))
		if u.CollectionID != "" && u.Note != "" {
			a.FileName = strings.TrimSuffix(a.FileName, path.Ext(a.FileName)) + " " + safeNote(u.Note) + path.Ext(a.FileName)
		}
		a.RelativePath = path.Join(u.RelativeDirectory, a.FileName)
	} else {
		a.RelativePath, e = s.availablePath(ctx, u.LibraryID, a.RelativePath)
		if e != nil {
			return model.UploadResult{}, e
		}
		a.FileName = path.Base(a.RelativePath)
	}
	if !domain.ValidName(a.FileName) || !domain.ValidMediaPath(a.RelativePath) {
		return model.UploadResult{}, Fail(400, "invalid_name", "目标命名含不合法字符、保留目录或坏编码；未发布文件。")
	}
	if u.CosWorkID != "" {
		work, e := store.Get[model.CosWork](ctx, s.DB.DB, "SELECT data FROM cos_works WHERE id=?", u.CosWorkID)
		if e != nil {
			return model.UploadResult{}, Normalize(e)
		}
		if !strings.HasPrefix(a.RelativePath, work.RelativeDirectory+"/") {
			return model.UploadResult{}, Fail(409, "cos_work_directory_conflict", "命名后的目标不属于明确选择的 COS 作品目录。")
		}
	}
	if e = s.checkPath(ctx, a.LibraryID, a.RelativePath, a.ID); e != nil {
		return model.UploadResult{}, e
	}
	a.ContentURL = "/api/v1/assets/" + a.ID + "/content"
	if prepared != nil && prepared.probed {
		m := prepared.metadata
		a.Width, a.Height, a.DurationMs, a.ProbeState = m.Width, m.Height, m.DurationMs, m.ProbeState
		a.ThumbnailURL = ""
		if m.ThumbnailURL != "" {
			a.ThumbnailURL = "/api/v1/assets/" + a.ID + "/thumbnail"
		}
	} else {
		if e = s.probe(ctx, uploadPath(u.ID), &a); e != nil {
			return model.UploadResult{}, publicationError("probe", e)
		}
	}
	info, e := s.Root.Stat(uploadPath(u.ID))
	if e != nil {
		return model.UploadResult{}, publicationError("stat_source", e)
	}
	if u.SourceMtime != nil && !info.ModTime().Equal(*u.SourceMtime) {
		if e = s.Root.Chtimes(uploadPath(u.ID), *u.SourceMtime, *u.SourceMtime); e != nil {
			return model.UploadResult{}, publicationError("source_time", e)
		}
		info, e = s.Root.Stat(uploadPath(u.ID))
		if e != nil {
			return model.UploadResult{}, publicationError("stat_source", e)
		}
	}
	modified := info.ModTime().UTC()
	a.ContentMtime = &modified
	j := fileIntent{ID: digest([]byte("publish:" + u.ID))[:32], Kind: operation, Source: uploadPath(u.ID), Target: assetPath(a), Retired: "retired/" + u.ID, UploadID: u.ID, Before: before, After: a}
	if operation == "ingest" {
		j.DeclaredSourceIDs = &declaredSources
	}
	a, e = s.runFile(ctx, w, j)
	if e != nil {
		return model.UploadResult{}, e
	}
	u.State = "ingested"
	u.AssetID = a.ID
	u.PublishedContentVersion = a.ContentVersion
	return model.UploadResult{Upload: u, Asset: &a}, nil
}
func safeNote(note string) string {
	v := strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune("/\\<>:\"|?*\ufffd", r) {
			return '_'
		}
		return r
	}, note)
	v = strings.Trim(v, " .")
	if len([]rune(v)) > 80 {
		v = string([]rune(v)[:80])
	}
	return v
}
func (s *Service) CancelUpload(ctx context.Context, w *Write, id string) error {
	u, e := s.Upload(ctx, w.Principal, id)
	if e != nil {
		return e
	}
	if e = Authorize(w.Principal, "ingest", u.BatchID); e != nil {
		return e
	}
	if u.State == "ingested" {
		return Fail(409, "already_ingested", "取消上传不会删除已入库媒体。")
	}
	if u.ErrorCode == "publication_pending" {
		return Fail(409, "publication_pending", "发布已经开始，须先恢复并核对结果，不能用取消覆盖发布事实。")
	}
	u.State = "cancelled"
	q, e := w.Q()
	if e != nil {
		return e
	}
	if e = store.SaveUpload(ctx, q, u); e != nil {
		return e
	}
	return s.refreshBatch(ctx, q, u.BatchID)
}

const directoryNamesSQL = "SELECT file_name" + liveDirectoryRows + assetParentDir + "=?"

func (s *Service) directoryNames(ctx context.Context, library, dir string) ([]string, error) {
	parent := path.Clean(dir)
	if parent == "." {
		parent = ""
	}
	rows, e := s.DB.DB.QueryContext(ctx, directoryNamesSQL, library, parent)
	if e != nil {
		return nil, e
	}
	names := []string{}
	seen := map[string]bool{}
	add := func(name string) error {
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
		if len(names) > 200000 {
			return Fail(422, "namespace_budget", "目标目录超过命名盘点预算，请使用更小的作品目录。")
		}
		return nil
	}
	for rows.Next() {
		var name string
		if e = rows.Scan(&name); e != nil {
			rows.Close()
			return nil, e
		}
		if e = add(name); e != nil {
			rows.Close()
			return nil, e
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	full := path.Join("libraries", library, dir)
	if e = s.safe(full); e != nil {
		return nil, e
	}
	folder, e := s.Root.Open(full)
	if errors.Is(e, os.ErrNotExist) {
		return names, nil
	}
	if e != nil {
		return nil, e
	}
	defer folder.Close()
	for {
		entries, readErr := folder.ReadDir(256)
		for _, entry := range entries {
			if entry.Name() == ".qimeng-internal" {
				continue
			}
			if e = add(entry.Name()); e != nil {
				return nil, e
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return nil, readErr
		}
	}
	return names, nil
}
func (s *Service) nextEpisode(ctx context.Context, collectionID string) (int, error) {
	c, e := store.Get[model.Collection](ctx, s.DB.DB, "SELECT data FROM collections WHERE id=?", collectionID)
	if e != nil {
		return 0, Normalize(e)
	}
	maximum, e := s.maximumCollectionPosition(ctx, collectionID)
	if e != nil {
		return 0, e
	}
	names, e := s.directoryNames(ctx, c.LibraryID, c.Name)
	if e != nil {
		return 0, e
	}
	next := domain.NextChapter(c.Name, names, maximum)
	if next < 1 {
		return 0, Fail(409, "collection_position_exhausted", "此集合已用尽可表示的位置。")
	}
	return next, nil
}
