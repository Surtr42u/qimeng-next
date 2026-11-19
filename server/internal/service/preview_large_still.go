package service

import (
	"bytes"
	"context"
	"errors"
	"image"
	"io"

	jpegscaled "github.com/m8rge/go-scaled-jpeg"
	pngscaled "github.com/m8rge/go-scaled-png"
)

const (
	largeStillPixels = 256_000_000
	largeStillSide   = 32_768
	largeStillBytes  = 256 << 20
	largeStillHeader = 2 << 20
)

func largeHeaderLimit() error {
	return Fail(422, "thumbnail_resource_limit", "缩略图需要的图片头超过 2 MiB。")
}

func decodeLargeStill(ctx context.Context, r io.Reader, size int64, format string, config image.Config, width, height int) (image.Image, error) {
	if size < 1 || size > largeStillBytes || config.Width > largeStillSide || config.Height > largeStillSide ||
		int64(config.Width)*int64(config.Height) > largeStillPixels {
		return nil, Fail(422, "thumbnail_resource_limit", "原图超过大图缩略图的输入预算。")
	}
	// Replay the exact header the bounded decoder was admitted for. A pinned
	// descriptor survives renames, but another writer could still edit its inode.
	input := io.LimitReader(r, size)
	var header bytes.Buffer
	metadata := &io.LimitedReader{R: input, N: largeStillHeader}
	probe := io.TeeReader(metadata, &header)
	if format == "png" {
		info, err := pngscaled.DecodeConfig(probe)
		if err != nil {
			if metadata.N == 0 && (errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF)) {
				return nil, largeHeaderLimit()
			}
			return nil, err
		}
		if info.Width != config.Width || info.Height != config.Height {
			return nil, Fail(409, "source_changed", "原图尺寸在生成期间变化，请重新扫描。")
		}
		// This decoder scales each row first, then shrinks the columns in
		// place. Adam7 would allocate a full raster. Eight bytes per pixel
		// also cover 16-bit transparency introduced by a later tRNS chunk.
		if info.Interlaced || int64(width)*int64(info.Height)*8 > 96<<20 {
			return nil, Fail(422, "thumbnail_resource_limit", "此 PNG 的交错方式或缩放工作区超出大图预览预算。")
		}
		return pngscaled.Decode(io.MultiReader(&header, input), width, height, pngscaled.CatmullRom)
	}
	info, err := jpegscaled.DecodeConfig(probe)
	if err != nil {
		if metadata.N == 0 && (errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF)) {
			return nil, largeHeaderLimit()
		}
		return nil, err
	}
	if info.Width != config.Width || info.Height != config.Height {
		return nil, Fail(409, "source_changed", "原图尺寸在生成期间变化，请重新扫描。")
	}
	stream := io.MultiReader(&header, input)
	if info.JpegType == jpegscaled.JpegTypeProgressive {
		return progressivePreview(ctx, stream, info.Config)
	}
	if info.JpegType != jpegscaled.JpegTypeBaseline {
		return nil, Fail(422, "thumbnail_resource_limit", "此 JPEG 编码方式不支持有界大图预览。")
	}
	// Decode a 1x1 or 2x2 IDCT per 8x8 block. Keep at least the requested
	// long edge for near-threshold images without materializing the original.
	blockSize := 1
	if max(info.Width, info.Height)/8 < max(width, height) {
		blockSize = 2
	}
	return jpegscaled.Decode(stream, jpegscaled.DecodeOptions{DCTSizeScaled: blockSize})
}
