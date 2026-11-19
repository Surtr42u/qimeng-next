package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"io"
)

// At 1/8 resolution each pixel is the dequantized DC coefficient (mean) of
// one 8x8 block. Progressive JPEG stores DC and AC in separate scans. Read
// the DC scans, including successive refinement, and skip AC entropy to the
// next marker. No full-resolution coefficient planes are required.
var errProgressiveJPEG = errors.New("invalid progressive JPEG preview")

type jpegDCPlane struct {
	id, h, v, table byte
	stride          int
	quant, lowBit   int
	blocks          []int16
}

type jpegDCFrame struct {
	config     image.Config
	planes     []jpegDCPlane
	wide, high int // maximum horizontal and vertical sampling factors
	cols, rows int // interleaved MCU grid
	quant      [4]int
	tables     [4]jpegDCHuffman
	restart    int
	rgb, jfif  bool
}

func progressivePreview(ctx context.Context, src io.Reader, config image.Config) (image.Image, error) {
	if int64(config.Width)*int64(config.Height) > 128_000_000 {
		return nil, Fail(422, "thumbnail_resource_limit", "渐进 JPEG 超出缩略图的扫描预算。")
	}
	reader := bufio.NewReaderSize(src, 32<<10)
	frame := jpegDCFrame{config: config}
	marker, err := jpegMarker(reader)
	if err != nil || marker != 0xd8 {
		return nil, errProgressiveJPEG
	}
	var segment [65535]byte
	marker, err = jpegMarker(reader)
	for err == nil && marker != 0xd9 {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		if _, err = io.ReadFull(reader, segment[:2]); err != nil {
			return nil, err
		}
		length := int(binary.BigEndian.Uint16(segment[:2])) - 2
		if length < 0 {
			return nil, errProgressiveJPEG
		}
		data := segment[:length]
		if _, err = io.ReadFull(reader, data); err != nil {
			return nil, err
		}
		switch marker {
		case 0xc2:
			err = frame.start(data)
		case 0xc4:
			err = frame.readHuffman(data)
		case 0xdb:
			err = frame.readQuant(data)
		case 0xdd:
			if len(data) != 2 {
				return nil, errProgressiveJPEG
			}
			frame.restart = int(binary.BigEndian.Uint16(data))
		case 0xda:
			if err = frame.scan(ctx, reader, data); err != nil {
				return nil, err
			}
			if data[len(data)-3] > 0 {
				marker, err = jpegMarkerAfterAC(reader)
				continue
			}
		case 0xe0:
			frame.jfif = frame.jfif || bytes.HasPrefix(data, []byte("JFIF\x00"))
		case 0xee:
			if len(data) >= 12 && bytes.HasPrefix(data, []byte("Adobe")) {
				frame.rgb = data[11] == 0
			}
		}
		if err != nil {
			return nil, err
		}
		marker, err = jpegMarker(reader)
	}
	if err != nil {
		return nil, err
	}
	return frame.image(ctx)
}

func (f *jpegDCFrame) start(data []byte) error {
	if len(f.planes) != 0 || len(data) < 6 || data[0] != 8 || len(data) != 6+3*int(data[5]) ||
		int(binary.BigEndian.Uint16(data[1:3])) != f.config.Height || int(binary.BigEndian.Uint16(data[3:5])) != f.config.Width {
		return errProgressiveJPEG
	}
	if data[5] != 1 && data[5] != 3 {
		return Fail(422, "thumbnail_resource_limit", "大幅渐进 JPEG 预览支持灰度和三通道图片。")
	}
	for i := 6; i < len(data); i += 3 {
		p := jpegDCPlane{id: data[i], h: data[i+1] >> 4, v: data[i+1] & 15, table: data[i+2], lowBit: -1}
		if p.h < 1 || p.h > 4 || p.v < 1 || p.v > 4 || p.table > 3 {
			return errProgressiveJPEG
		}
		if data[5] == 1 {
			p.h, p.v = 1, 1
		}
		f.planes = append(f.planes, p)
		f.wide, f.high = max(f.wide, int(p.h)), max(f.high, int(p.v))
	}
	f.cols = (f.config.Width + 8*f.wide - 1) / (8 * f.wide)
	f.rows = (f.config.Height + 8*f.high - 1) / (8 * f.high)
	for i := range f.planes {
		p := &f.planes[i]
		p.stride = f.cols * int(p.h)
		p.blocks = make([]int16, p.stride*f.rows*int(p.v))
	}
	return nil
}

func (f *jpegDCFrame) readQuant(data []byte) error {
	for len(data) > 0 {
		precision, id := data[0]>>4, data[0]&15
		length := 1 + 64*(int(precision)+1)
		if precision > 1 || id > 3 || len(data) < length {
			return errProgressiveJPEG
		}
		f.quant[id] = int(data[1])
		if precision == 1 {
			f.quant[id] = int(binary.BigEndian.Uint16(data[1:3]))
		}
		data = data[length:]
	}
	return nil
}

func (f *jpegDCFrame) scan(ctx context.Context, reader *bufio.Reader, data []byte) error {
	if len(data) < 6 || int(data[0]) > len(f.planes) || len(data) != 4+2*int(data[0]) {
		return errProgressiveJPEG
	}
	band := data[len(data)-3:]
	hi, lo := int(band[2]>>4), int(band[2]&15)
	if band[0] > band[1] || band[1] > 63 || lo > 13 || (hi != 0 && hi != lo+1) {
		return errProgressiveJPEG
	}
	if band[0] > 0 {
		if data[0] != 1 {
			return errProgressiveJPEG
		}
		return nil
	}
	if band[1] != 0 {
		return errProgressiveJPEG
	}
	var scans []jpegDCScan
	for i := 1; i < len(data)-3; i += 2 {
		var plane *jpegDCPlane
		for j := range f.planes {
			if f.planes[j].id == data[i] {
				plane = &f.planes[j]
			}
		}
		table := data[i+1] >> 4
		if plane == nil || table > 3 || hi == 0 && plane.lowBit >= 0 || hi > 0 && plane.lowBit != hi {
			return errProgressiveJPEG
		}
		plane.lowBit = lo
		if hi == 0 {
			plane.quant = f.quant[plane.table]
			if plane.quant == 0 {
				return errProgressiveJPEG
			}
		}
		scans = append(scans, jpegDCScan{p: plane, table: &f.tables[table], h: int(plane.h), v: int(plane.v)})
	}
	cols, rows := f.cols, f.rows
	if len(scans) == 1 {
		p := scans[0].p
		cols = (f.config.Width*int(p.h) + 8*f.wide - 1) / (8 * f.wide)
		rows = (f.config.Height*int(p.v) + 8*f.high - 1) / (8 * f.high)
		scans[0].h, scans[0].v = 1, 1
	}
	bits := jpegDCBits{r: reader}
	for y := 0; y < rows; y++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		for x := 0; x < cols; x++ {
			mcu := y*cols + x
			if f.restart > 0 && mcu > 0 && mcu%f.restart == 0 {
				marker, err := jpegMarker(reader)
				if err != nil || marker != byte(0xd0+(mcu/f.restart-1)%8) {
					return errProgressiveJPEG
				}
				bits.left = 0
				for i := range scans {
					scans[i].predictor = 0
				}
			}
			for i := range scans {
				if err := scans[i].decode(&bits, x, y, hi != 0, lo); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (f *jpegDCFrame) image(ctx context.Context) (image.Image, error) {
	if len(f.planes) == 0 {
		return nil, errProgressiveJPEG
	}
	for _, p := range f.planes {
		if p.lowBit < 0 {
			return nil, errProgressiveJPEG
		}
	}
	rgb := !f.jfif && (f.rgb || len(f.planes) == 3 && f.planes[0].id == 'R' && f.planes[1].id == 'G' && f.planes[2].id == 'B')
	dst := image.NewRGBA(image.Rect(0, 0, (f.config.Width+7)/8, (f.config.Height+7)/8))
	for y := 0; y < dst.Rect.Dy(); y++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for x := 0; x < dst.Rect.Dx(); x++ {
			var c [3]byte
			for i := range f.planes {
				p := &f.planes[i]
				dc := int(p.blocks[(y*int(p.v)/f.high)*p.stride+x*int(p.h)/f.wide])
				c[i] = byte(min(255, max(0, 128+(dc*p.quant+4)>>3)))
			}
			if len(f.planes) == 1 {
				c[1], c[2] = c[0], c[0]
			} else if !rgb {
				c[0], c[1], c[2] = color.YCbCrToRGB(c[0], c[1], c[2])
			}
			dst.SetRGBA(x, y, color.RGBA{R: c[0], G: c[1], B: c[2], A: 255})
		}
	}
	return dst, nil
}
