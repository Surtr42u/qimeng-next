package service

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"strconv"
	"time"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
	"qimeng.local/next/server/internal/model"
)

type normalizedProfileAvatar struct {
	body          []byte
	width, height int
	sourceHash    string
}

func normalizeProfileAvatar(body []byte) (normalizedProfileAvatar, error) {
	var result normalizedProfileAvatar
	if len(body) == 0 || len(body) > maxProfileAvatarBytes {
		return result, Fail(413, "avatar_too_large", "头像原图必须为 1 字节至 4 MiB。")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(body))
	if err != nil || (format != "jpeg" && format != "png" && format != "gif" && format != "webp") {
		return result, Fail(400, "avatar_format_unsupported", "头像需为可解码的 PNG、JPEG、GIF 或 WebP 图片。")
	}
	if config.Width < 1 || config.Height < 1 || config.Width > 4096 || config.Height > 4096 || int64(config.Width)*int64(config.Height) > 4_000_000 {
		return result, Fail(413, "avatar_pixel_limit", "头像尺寸最多 4096 边长且不超过 400 万像素。")
	}
	decoded, _, err := image.Decode(bytes.NewReader(body))
	if err != nil {
		return result, Fail(400, "avatar_decode_failed", "头像内容无法解码，原头像保持不变。")
	}
	width, height := config.Width, config.Height
	if width > 512 || height > 512 {
		if width >= height {
			height = max(1, height*512/width)
			width = 512
		} else {
			width = max(1, width*512/height)
			height = 512
		}
	}
	output := image.NewNRGBA(image.Rect(0, 0, width, height))
	draw.ApproxBiLinear.Scale(output, output.Bounds(), decoded, decoded.Bounds(), draw.Src, nil)
	var normalized bytes.Buffer
	if err = png.Encode(&normalized, output); err != nil {
		return result, err
	}
	if normalized.Len() > 2<<20 {
		return result, Fail(413, "avatar_normalized_limit", "头像规范化结果过大。")
	}
	return normalizedProfileAvatar{body: normalized.Bytes(), width: width, height: height, sourceHash: digest(body)}, nil
}

func withAvatarSlot(ctx context.Context) (func(), error) {
	select {
	case avatarDecodeSlots <- struct{}{}:
		return func() { <-avatarDecodeSlots }, nil
	case <-ctx.Done():
		return nil, Fail(504, "avatar_timeout", "头像处理等待超时，原头像保持不变。")
	}
}

// The early receipt read avoids re-downloading a changed remote resource after a
// lost response. Run revalidates identity, input and revision under the write lock.
func (s *Service) operationReceipt(ctx context.Context, p model.Principal, operation, key string, input []byte) (Result, bool, error) {
	if err := Authorize(p, "admin", ""); err != nil {
		return Result{}, false, err
	}
	if len(key) < 8 || len(key) > 128 {
		return Result{}, false, Fail(400, "idempotency_required", "此操作必须提供稳定幂等键。")
	}
	var previous string
	var result Result
	err := s.DB.DB.QueryRowContext(ctx, "SELECT input_hash,response,status FROM idempotency WHERE principal_id=? AND operation=? AND key=?", p.ID, operation, key).Scan(&previous, &result.Body, &result.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return Result{}, false, nil
	}
	if err != nil {
		return Result{}, false, err
	}
	if previous != digest(input) {
		return Result{}, false, Fail(409, "idempotency_conflict", "同一幂等键的输入已经改变。")
	}
	// Even a replay must pass the queued-write revocation check.
	result, err = s.Run(ctx, p, operation, key, input, func(*Write) (any, int, error) {
		return nil, 0, Fail(409, "receipt_changed", "操作回执发生变化，请重新回读。")
	})
	return result, true, err
}

func (s *Service) FetchAuthorProfileAvatar(ctx context.Context, p model.Principal, authorID, key string, input []byte, in ProfileAvatarFetch) (Result, error) {
	operation := "POST /api/v1/authors/" + authorID + "/profile/avatar-from-url"
	if result, exists, err := s.operationReceipt(ctx, p, operation, key, input); err != nil || exists {
		return result, err
	}
	if _, err := s.editableProfile(ctx, p, authorID, "x", in.ExpectedRevision); err != nil {
		return Result{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	release, err := withAvatarSlot(ctx)
	if err != nil {
		return Result{}, err
	}
	defer release()
	body, finalURL, err := fetchAvatarBytes(ctx, in.SourceURL, avatarHTTPClient(defaultAvatarNetwork()))
	if err != nil {
		return Result{}, err
	}
	normalized, err := normalizeProfileAvatar(body)
	if err != nil {
		return Result{}, err
	}
	return s.Run(ctx, p, operation, key, input, func(w *Write) (any, int, error) {
		block, err := s.commitProfileAvatar(ctx, w, authorID, in.ExpectedRevision, "https", in.SourceURL, finalURL, normalized)
		return block, 200, err
	})
}

func (s *Service) UploadAuthorProfileAvatar(ctx context.Context, p model.Principal, authorID, key string, revision int64, body []byte) (Result, error) {
	operation := "PUT /api/v1/authors/" + authorID + "/profile/avatar"
	// The original content digest and expected block version are the complete
	// immutable input, independent of the multipart or transport representation.
	input := []byte(digest(body) + ":" + strconv.FormatInt(revision, 10))
	if result, exists, err := s.operationReceipt(ctx, p, operation, key, input); err != nil || exists {
		return result, err
	}
	if _, err := s.editableProfile(ctx, p, authorID, "x", &revision); err != nil {
		return Result{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	release, err := withAvatarSlot(ctx)
	if err != nil {
		return Result{}, err
	}
	defer release()
	normalized, err := normalizeProfileAvatar(body)
	if err != nil {
		return Result{}, err
	}
	return s.Run(ctx, p, operation, key, input, func(w *Write) (any, int, error) {
		block, err := s.commitProfileAvatar(ctx, w, authorID, &revision, "upload", "", "", normalized)
		return block, 200, err
	})
}

func (s *Service) commitProfileAvatar(ctx context.Context, w *Write, authorID string, revision *int64, origin, sourceURL, finalURL string, image normalizedProfileAvatar) (AuthorProfileBlock, error) {
	block, err := s.editableProfile(ctx, w.Principal, authorID, "x", revision)
	if err != nil {
		return block, err
	}
	free, err := freeBytes(s.Config.DataDir)
	if err != nil {
		return block, err
	}
	if free < s.Config.ReserveBytes+int64(len(image.body))*4+(1<<20) {
		return block, Fail(507, "storage_reserve", "头像保存会侵占存储保留空间。")
	}
	now := time.Now().UTC()
	id := digest(image.body)
	old := block.Avatar
	block.Avatar = &ProfileAvatar{ID: id, URL: "/api/v1/profile-avatars/" + id, Origin: origin, SourceURL: sourceURL, FetchedURL: finalURL, SourceHash: image.sourceHash, ContentHash: id, Width: image.width, Height: image.height, Bytes: len(image.body), FetchedAt: now}
	block.Values.AvatarSourceURL = &sourceURL
	block.Revision++
	block.Source = "human"
	block.UpdatedAt = &now
	block.CandidateID = ""
	q, err := w.Q()
	if err != nil {
		return block, err
	}
	if _, err = q.ExecContext(ctx, "INSERT OR IGNORE INTO author_profile_avatars(id,created_at,image) VALUES(?,?,?)", id, dateKey(now), image.body); err != nil {
		return block, err
	}
	if err = saveProfileBlock(ctx, q, block); err != nil {
		return block, err
	}
	if old != nil && old.ID != id {
		err = pruneProfileAvatar(ctx, q, old.ID)
	}
	return block, err
}

func (s *Service) ClearAuthorProfileAvatar(ctx context.Context, w *Write, authorID string, revision int64) (AuthorProfileBlock, error) {
	block, err := s.editableProfile(ctx, w.Principal, authorID, "x", &revision)
	if err != nil {
		return block, err
	}
	old := block.Avatar
	block.Avatar = nil
	empty := ""
	block.Values.AvatarSourceURL = &empty
	now := time.Now().UTC()
	block.Revision++
	block.Source = "human"
	block.UpdatedAt = &now
	block.CandidateID = ""
	q, err := w.Q()
	if err != nil {
		return block, err
	}
	if err = saveProfileBlock(ctx, q, block); err != nil {
		return block, err
	}
	if old != nil {
		err = pruneProfileAvatar(ctx, q, old.ID)
	}
	return block, err
}

func (s *Service) AuthorProfileAvatar(ctx context.Context, id string) ([]byte, time.Time, error) {
	var body []byte
	var created string
	err := s.DB.DB.QueryRowContext(ctx, "SELECT image,created_at FROM author_profile_avatars WHERE id=?", id).Scan(&body, &created)
	if err != nil {
		return nil, time.Time{}, Normalize(err)
	}
	modified, err := time.Parse(timestampKey, created)
	return body, modified, err
}
