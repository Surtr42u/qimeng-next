package service

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"image"
	"image/jpeg"
	"io"

	"golang.org/x/image/draw"
)

// Only inspect bounded JPEG metadata before the compressed image. TIFF offsets
// are relative to the TIFF header inside the same APP1 segment.
func jpegOrientation(source io.Reader) int {
	reader := io.LimitReader(source, 512<<10)
	var pair [2]byte
	if _, err := io.ReadFull(reader, pair[:]); err != nil || pair != [2]byte{0xff, 0xd8} {
		return 1
	}
	for {
		if _, err := io.ReadFull(reader, pair[:]); err != nil || pair[0] != 0xff {
			return 1
		}
		marker := pair[1]
		for marker == 0xff {
			if _, err := io.ReadFull(reader, pair[:1]); err != nil {
				return 1
			}
			marker = pair[0]
		}
		if marker == 0xda || marker == 0xd9 {
			return 1
		}
		if marker == 0x01 || marker >= 0xd0 && marker <= 0xd7 {
			continue
		}
		if _, err := io.ReadFull(reader, pair[:]); err != nil {
			return 1
		}
		size := int(binary.BigEndian.Uint16(pair[:])) - 2
		if size < 0 {
			return 1
		}
		if marker != 0xe1 {
			if _, err := io.CopyN(io.Discard, reader, int64(size)); err != nil {
				return 1
			}
			continue
		}
		data := make([]byte, size)
		if _, err := io.ReadFull(reader, data); err != nil {
			return 1
		}
		if !bytes.HasPrefix(data, []byte("Exif\x00\x00")) {
			continue
		}
		return tiffOrientation(data[6:])
	}
}

// orientationFilterChain maps an EXIF Orientation value to the ffmpeg -vf
// segment that normalizes the decoded frame. ffmpeg's implicit EXIF autorotate
// differs between versions, so the still command disables it with
// -noautorotate and treats this explicit chain as the only orientation
// authority (same mapping as the legacy generator).
func orientationFilterChain(orientation int) string {
	switch orientation {
	case 2:
		return "hflip"
	case 3:
		return "hflip,vflip"
	case 4:
		return "vflip"
	case 5:
		return "transpose=0"
	case 6:
		return "transpose=1"
	case 7:
		return "transpose=3"
	case 8:
		return "transpose=2"
	default:
		// Orientation 1 and unknown values stay unrotated.
		return ""
	}
}

// jpegStillOrientation reads the bounded EXIF header from the pinned input.
// A parse or read failure is not fatal for preview generation: the legacy
// behavior is to keep the unrotated frame.
func (s *Service) jpegStillOrientation(input mediaInput) int {
	reader, err := input.reader(s)
	if err != nil {
		return 1
	}
	defer reader.Close()
	return jpegOrientation(reader)
}

func tiffOrientation(data []byte) int {
	if len(data) < 8 {
		return 1
	}
	var order binary.ByteOrder
	switch string(data[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 1
	}
	if order.Uint16(data[2:4]) != 42 {
		return 1
	}
	offset := int64(order.Uint32(data[4:8]))
	if offset < 8 || offset+2 > int64(len(data)) {
		return 1
	}
	count := int64(order.Uint16(data[offset : offset+2]))
	for at := offset + 2; count > 0 && at+12 <= int64(len(data)); at, count = at+12, count-1 {
		entry := data[at : at+12]
		if order.Uint16(entry[:2]) == 0x0112 && order.Uint16(entry[2:4]) == 3 && order.Uint32(entry[4:8]) == 1 {
			value := int(order.Uint16(entry[8:10]))
			if value >= 1 && value <= 8 {
				return value
			}
		}
	}
	return 1
}

// Transform the scaled preview instead of allocating a second full-sized photo.
func orientPreview(src *image.RGBA, orientation int) *image.RGBA {
	if orientation <= 1 || orientation > 8 {
		return src
	}
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	size := image.Rect(0, 0, w, h)
	if orientation >= 5 {
		size = image.Rect(0, 0, h, w)
	}
	dst := image.NewRGBA(size)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var dx, dy int
			switch orientation {
			case 2:
				dx, dy = w-1-x, y
			case 3:
				dx, dy = w-1-x, h-1-y
			case 4:
				dx, dy = x, h-1-y
			case 5:
				dx, dy = y, x
			case 6:
				dx, dy = h-1-y, x
			case 7:
				dx, dy = h-1-y, w-1-x
			case 8:
				dx, dy = y, w-1-x
			}
			dst.SetRGBA(dx, dy, src.RGBAAt(x, y))
		}
	}
	return dst
}

// Research frames have separate input and output budgets from display thumbnails.
type stillPreviewPolicy struct {
	quality    int
	scale      draw.Scaler
	allowLarge bool
}

var displayStill = stillPreviewPolicy{quality: 92, scale: draw.CatmullRom, allowLarge: true}
var researchStill = stillPreviewPolicy{quality: 85, scale: draw.ApproxBiLinear}

func previewDimensions(width, height, edge int) (int, int) {
	if width <= edge && height <= edge {
		return width, height
	}
	if width >= height {
		return edge, max(1, int(int64(height)*int64(edge)/int64(width)))
	}
	return max(1, int(int64(width)*int64(edge)/int64(height))), edge
}

func fullStillFits(width, height int) bool {
	return width > 0 && height > 0 && int64(width) <= 40_000_000/int64(height)
}

// Each metadata reader starts at byte zero. The physical input advances only
// when a reader needs bytes beyond the prefix already seen by the others.
type stillHeader struct {
	input io.Reader
	bytes bytes.Buffer
}

func (h *stillHeader) probe() io.Reader {
	more := io.LimitReader(h.input, int64(largeStillHeader-h.bytes.Len()))
	return io.MultiReader(bytes.NewReader(h.bytes.Bytes()), io.TeeReader(more, &h.bytes))
}

func (h *stillHeader) replay() io.Reader {
	return io.MultiReader(bytes.NewReader(h.bytes.Bytes()), h.input)
}

func (s *Service) scaledStill(ctx context.Context, source mediaInput, mime string, catalog image.Point, edge int, policy stillPreviewPolicy) ([]byte, error) {
	f, err := source.reader(s)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return renderStill(ctx, f, source.size, mime, catalog, edge, policy)
}

func renderStill(ctx context.Context, f io.ReadSeeker, size int64, mime string, catalog image.Point, edge int, policy stillPreviewPolicy) ([]byte, error) {
	knownSmall := fullStillFits(catalog.X, catalog.Y)
	if policy.allowLarge && !knownSmall && catalog.X > 0 && catalog.Y > 0 && size > largeStillBytes {
		return nil, Fail(422, "thumbnail_resource_limit", "原图超过大图缩略图的输入预算。")
	}
	reader := scanReader{ctx: ctx, source: f}
	header := stillHeader{input: reader}
	orientation := 1
	if mime == "image/jpeg" {
		orientation = jpegOrientation(header.probe())
	}
	cfg, format, err := image.DecodeConfig(header.probe())
	input := header.replay()
	if err != nil && header.bytes.Len() == largeStillHeader && (errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF)) {
		if policy.allowLarge && !knownSmall {
			return nil, largeHeaderLimit()
		}
		// A small cataloged original (or an existing research subject) can
		// legitimately have extensive metadata. Preserve its standard path.
		cfg, format, err = image.DecodeConfig(input)
		if err == nil {
			if !fullStillFits(cfg.Width, cfg.Height) {
				return nil, largeHeaderLimit()
			}
			if _, err = f.Seek(0, io.SeekStart); err != nil {
				return nil, err
			}
			input = reader
		}
	}
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, Fail(422, "thumbnail_decode_failed", "无法读取图片尺寸。")
	}
	if cfg.Height < 1 || cfg.Width < 1 {
		return nil, Fail(422, "thumbnail_resource_limit", "图片超过解码预算，原件可正常读取。")
	}
	width, height := previewDimensions(cfg.Width, cfg.Height, edge)
	var src image.Image
	if !fullStillFits(cfg.Width, cfg.Height) {
		if !policy.allowLarge || mime != "image/"+format || (format != "jpeg" && format != "png") {
			return nil, Fail(422, "thumbnail_resource_limit", "图片超过解码预算，原件可正常读取。")
		}
		src, err = decodeLargeStill(ctx, input, size, format, cfg, width, height)
	} else {
		src, _, err = image.Decode(input)
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		if _, ok := err.(*Error); ok {
			return nil, err
		}
		return nil, Fail(422, "thumbnail_decode_failed", "无法解码预览，原件保留。")
	}
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	policy.scale.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Src, nil)
	var output bytes.Buffer
	if err = jpeg.Encode(&output, orientPreview(dst, orientation), &jpeg.Options{Quality: policy.quality}); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}
