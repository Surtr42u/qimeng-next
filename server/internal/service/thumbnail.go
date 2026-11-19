package service

import (
	"bytes"
	"context"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"os/exec"
	"path"
	"strconv"
	"time"

	"golang.org/x/image/draw"
	"qimeng.local/next/server/internal/model"
)

type boundedOutput struct {
	bytes.Buffer
	limit int
}

func (w *boundedOutput) Write(b []byte) (int, error) {
	if len(b) > w.limit-w.Len() {
		return 0, errors.New("media tool output exceeded limit")
	}
	return w.Buffer.Write(b)
}
func builtinImage(mime string) bool {
	return mime == "image/jpeg" || mime == "image/png" || mime == "image/gif"
}
func mediaDemuxer(mime string) string {
	switch mime {
	case "image/png":
		return "png_pipe"
	case "image/jpeg":
		return "mjpeg"
	case "image/gif":
		return "gif"
	case "image/webp":
		return "webp_pipe"
	case "image/avif", "video/mp4", "video/quicktime":
		return "mov"
	case "video/webm", "video/x-matroska":
		return "matroska"
	case "video/x-msvideo":
		return "avi"
	}
	return ""
}
func toolPath(env, name string) string {
	if configured := os.Getenv(env); configured != "" {
		return configured
	}
	value, e := exec.LookPath(name)
	if e != nil {
		return ""
	}
	return value
}
func (s *Service) probe(ctx context.Context, name string, a *model.Asset) error {
	return s.probeMedia(ctx, mediaInput{name: name}, a)
}
func (s *Service) probeMedia(ctx context.Context, input mediaInput, a *model.Asset) error {
	a.ProbeState = "unavailable"
	a.ThumbnailURL = ""
	a.Width = 0
	a.Height = 0
	a.DurationMs = 0
	if builtinImage(a.MimeType) {
		f, e := input.reader(s)
		if e != nil {
			return e
		}
		defer f.Close()
		reader := scanReader{ctx: ctx, source: f}
		cfg, _, e := image.DecodeConfig(reader)
		if e != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return Fail(415, "invalid_image", "文件不是可解码的图片。")
		}
		a.Width, a.Height = cfg.Width, cfg.Height
		if a.MimeType == "image/jpeg" {
			if _, e = f.Seek(0, io.SeekStart); e != nil {
				return e
			}
			if jpegOrientation(reader) >= 5 {
				a.Width, a.Height = a.Height, a.Width
			}
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		a.ProbeState = "ready"
		a.ThumbnailURL = "/api/v1/assets/" + a.ID + "/thumbnail"
		return nil
	}
	if ffmpeg := toolPath("QIMENG_FFMPEG", "ffmpeg"); ffmpeg != "" {
		a.ThumbnailURL = "/api/v1/assets/" + a.ID + "/thumbnail"
	}
	ffprobe := toolPath("QIMENG_FFPROBE", "ffprobe")
	if ffprobe == "" {
		return nil
	}
	demuxer := mediaDemuxer(a.MimeType)
	if demuxer == "" {
		return Fail(415, "media_unsupported", "此格式没有受限媒体探测通道。")
	}
	deadline, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	result, e := s.probeStreams(deadline, ffprobe, demuxer, input)
	if e != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		a.ProbeState = "failed"
		return nil
	}
	stream, found := result.primary()
	if !found {
		a.ProbeState = "failed"
		return nil
	}
	a.Width, a.Height = stream.Width, stream.Height
	if result.Format.Duration != "" && result.Format.Duration != "N/A" {
		duration, e := strconv.ParseFloat(result.Format.Duration, 64)
		if e != nil {
			return e
		}
		if duration >= 0 && duration < 1e9 {
			a.DurationMs = int64(duration * 1000)
		}
	}
	a.ProbeState = "ready"
	return nil
}
func previewPath(id string, version int64, edge int) string {
	return "cache/" + id + "/v3-" + strconv.FormatInt(version, 10) + "-" + strconv.Itoa(edge) + ".jpg"
}
func (s *Service) thumbnailSource(ctx context.Context, id string, version int64, edge int, snapshotNeeded bool) (*os.File, model.Asset, string, mediaInput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, e := s.AssetDetail(ctx, id)
	if e != nil {
		return nil, a, "", mediaInput{}, e
	}
	if a.Trashed {
		return nil, a, "", mediaInput{}, Fail(404, "not_found", "媒体位于回收站。")
	}
	if version > 0 && version != a.ContentVersion {
		return nil, a, "", mediaInput{}, Fail(409, "content_changed", "缩略图对应的内容版本已变化，请刷新条目。")
	}
	if e = s.currentThumbnailSource(a); e != nil {
		return nil, a, "", mediaInput{}, e
	}
	cache := previewPath(a.ID, a.ContentVersion, edge)
	if e = s.safe(cache); e != nil {
		return nil, a, "", mediaInput{}, e
	}
	if f, e := s.Root.Open(cache); e == nil {
		return f, a, cache, mediaInput{}, nil
	} else if !errors.Is(e, os.ErrNotExist) {
		return nil, a, "", mediaInput{}, e
	}
	if !snapshotNeeded {
		return nil, a, cache, mediaInput{}, nil
	}
	snapshot := "libraries/" + a.LibraryID + "/.qimeng-internal/cache-sources/" + ID() + path.Ext(a.FileName)
	pinned, e := s.pinMedia(assetPath(a), snapshot, a.Size)
	return nil, a, cache, pinned, e
}
func (s *Service) Thumbnail(ctx context.Context, id string, versions ...int64) (*os.File, model.Asset, error) {
	version := int64(0)
	if len(versions) > 0 {
		version = versions[0]
	}
	return s.ThumbnailSize(ctx, id, version, "")
}
func (s *Service) currentThumbnailSource(a model.Asset) error {
	info, e := s.Root.Stat(assetPath(a))
	if e != nil {
		return e
	}
	if !info.Mode().IsRegular() || info.Size() != a.Size || a.ContentMtime != nil && !info.ModTime().Equal(*a.ContentMtime) {
		return Fail(409, "source_changed", "原件已变化，请扫描后重新读取对应版本。")
	}
	return nil
}
func (s *Service) ThumbnailSize(ctx context.Context, id string, version int64, size string) (*os.File, model.Asset, error) {
	edge := 512
	switch size {
	case "", "md":
	case "sm":
		edge = 256
	case "lg":
		edge = 1024
	default:
		return nil, model.Asset{}, Fail(400, "invalid_thumbnail_size", "缩略图尺寸必须是sm、md或lg。")
	}
	return s.thumbnailFor(ctx, id, version, edge, false)
}
func (s *Service) thumbnailFor(ctx context.Context, id string, version int64, edge int, background bool) (*os.File, model.Asset, error) {
	cached, a, key, _, err := s.thumbnailSource(ctx, id, version, edge, false)
	if err != nil || cached != nil {
		return cached, a, err
	}
	err = s.previews.join(ctx, s, key, background, func(work context.Context) error { return s.generatePreview(work, a.ID, a.ContentVersion, edge) })
	if err != nil {
		return nil, a, err
	}
	cached, a, _, _, err = s.thumbnailSource(ctx, id, a.ContentVersion, edge, false)
	if err == nil && cached == nil {
		err = Fail(409, "thumbnail_changed", "生成的缩略图已失效，请重新读取。")
	}
	return cached, a, err
}
func (s *Service) generatePreview(ctx context.Context, id string, version int64, edge int) error {
	cached, a, cache, source, e := s.thumbnailSource(ctx, id, version, edge, true)
	if e != nil {
		return e
	}
	if cached != nil {
		return cached.Close()
	}
	defer source.release(s)
	var data []byte
	if builtinImage(a.MimeType) {
		data, e = s.scaledStill(source, a.MimeType, edge, 92, draw.CatmullRom)
	} else {
		data, e = s.externalPreview(ctx, source, a, edge)
	}
	if e != nil {
		return e
	}
	if e = s.verifySelectedImage(ctx, a, source); e != nil {
		return e
	}
	if e = ctx.Err(); e != nil {
		return e
	}
	free, e := freeBytes(s.Config.DataDir)
	if e != nil {
		return e
	}
	if free-int64(len(data))-4096 < s.Config.ReserveBytes {
		return Fail(507, "thumbnail_space", "剩余空间须留给原件，暂缓写入缩略图。")
	}
	if e = s.ensureParent(cache); e != nil {
		return e
	}
	tmp := path.Dir(cache) + "/" + ID() + ".tmp"
	f, e := s.Root.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	defer s.Root.Remove(tmp)
	if _, e = f.Write(data); e != nil {
		f.Close()
		return e
	}
	if e = f.Sync(); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, e := s.Asset(ctx, id)
	if e != nil {
		return e
	}
	if current.Trashed || current.ContentVersion != a.ContentVersion {
		return Fail(409, "content_changed", "生成期间媒体版本已变化，请刷新条目。")
	}
	if e = s.currentThumbnailSource(current); e != nil {
		return e
	}
	if e = s.Root.PublishTemp(tmp, cache); e != nil && !errors.Is(e, os.ErrExist) {
		return e
	}
	return nil
}
