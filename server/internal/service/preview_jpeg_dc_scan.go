package service

import (
	"bufio"
	"errors"
)

func jpegMarker(r *bufio.Reader) (byte, error) {
	b, err := r.ReadByte()
	if err != nil || b != 0xff {
		return 0, errProgressiveJPEG
	}
	return jpegMarkerCode(r)
}

func jpegMarkerCode(r *bufio.Reader) (byte, error) {
	for {
		b, err := r.ReadByte()
		if err != nil || b != 0xff {
			return b, err
		}
	}
}

func jpegMarkerAfterAC(r *bufio.Reader) (byte, error) {
	for {
		_, err := r.ReadSlice(0xff)
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if err != nil {
			return 0, err
		}
		b, err := jpegMarkerCode(r)
		if err != nil || b != 0 && (b < 0xd0 || b > 0xd7) {
			return b, err
		}
	}
}

type jpegDCBits struct {
	r           *bufio.Reader
	value, left byte
}

func (b *jpegDCBits) bit() (int, error) {
	if b.left == 0 {
		v, err := b.r.ReadByte()
		if err != nil {
			return 0, err
		}
		if v == 0xff {
			if stuffed, err := b.r.ReadByte(); err != nil || stuffed != 0 {
				return 0, errProgressiveJPEG
			}
		}
		b.value, b.left = v, 8
	}
	b.left--
	return int(b.value>>b.left) & 1, nil
}

type jpegDCHuffman struct {
	count   [16]byte
	symbols []byte
}

func (f *jpegDCFrame) readHuffman(data []byte) error {
	for len(data) > 0 {
		if len(data) < 17 || data[0]>>4 > 1 || data[0]&15 > 3 {
			return errProgressiveJPEG
		}
		var table jpegDCHuffman
		copy(table.count[:], data[1:17])
		total, slots := 0, 1
		for _, n := range table.count {
			total += int(n)
			slots = 2*slots - int(n)
			if slots < 0 {
				return errProgressiveJPEG
			}
		}
		if total == 0 || total > 256 || len(data) < 17+total {
			return errProgressiveJPEG
		}
		if data[0]>>4 == 0 {
			table.symbols = append([]byte(nil), data[17:17+total]...)
			f.tables[data[0]&15] = table
		}
		data = data[17+total:]
	}
	return nil
}

func (t *jpegDCHuffman) delta(bits *jpegDCBits) (int, error) {
	code, first, offset := 0, 0, 0
	for _, n := range t.count {
		bit, err := bits.bit()
		if err != nil {
			return 0, err
		}
		code = 2*code + bit
		if code-first < int(n) {
			size := int(t.symbols[offset+code-first])
			if size > 11 {
				return 0, errProgressiveJPEG
			}
			value := 0
			for range size {
				bit, err := bits.bit()
				if err != nil {
					return 0, err
				}
				value = 2*value + bit
			}
			if size > 0 && value < 1<<(size-1) {
				value -= (1 << size) - 1
			}
			return value, nil
		}
		first = 2 * (first + int(n))
		offset += int(n)
	}
	return 0, errProgressiveJPEG
}

type jpegDCScan struct {
	p               *jpegDCPlane
	table           *jpegDCHuffman
	h, v, predictor int
}

func (s *jpegDCScan) decode(bits *jpegDCBits, x, y int, refine bool, shift int) error {
	for dy := 0; dy < s.v; dy++ {
		row := (y*s.v+dy)*s.p.stride + x*s.h
		for dx := 0; dx < s.h; dx++ {
			if refine {
				bit, err := bits.bit()
				if err != nil {
					return err
				}
				s.p.blocks[row+dx] |= int16(bit << shift)
				continue
			}
			delta, err := s.table.delta(bits)
			if err != nil {
				return err
			}
			s.predictor += delta
			value := s.predictor << shift
			if value < -2048 || value > 2047 {
				return errProgressiveJPEG
			}
			s.p.blocks[row+dx] = int16(value)
		}
	}
	return nil
}
