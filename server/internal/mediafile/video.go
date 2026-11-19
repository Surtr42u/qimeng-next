package mediafile

import (
	"bytes"
	"encoding/binary"
	"math/bits"
)

type VideoContainer string

const (
	VideoISOBaseMedia VideoContainer = "isobmff"
	VideoMatroska     VideoContainer = "matroska"
	VideoAVI          VideoContainer = "avi"
)

func hasFileTypeBox(head []byte) bool {
	return len(head) >= 12 && string(head[4:8]) == "ftyp"
}

func hasEBMLHeader(head []byte) bool {
	return bytes.HasPrefix(head, []byte{0x1a, 0x45, 0xdf, 0xa3})
}

func hasAVIHeader(head []byte) bool {
	return len(head) >= 12 && string(head[:4]) == "RIFF" && string(head[8:12]) == "AVI "
}

// DetectVideoContainer recognizes signatures strong enough to override a
// stored type. A generic EBML header or image-family ftyp is insufficient.
func DetectVideoContainer(head []byte) VideoContainer {
	switch {
	case hasVideoFileType(head):
		return VideoISOBaseMedia
	case hasMatroskaDocType(head):
		return VideoMatroska
	case hasAVIHeader(head) && binary.LittleEndian.Uint32(head[4:8]) >= 4:
		return VideoAVI
	}
	return ""
}

func hasVideoFileType(head []byte) bool {
	if !hasFileTypeBox(head) {
		return false
	}
	size := uint64(binary.BigEndian.Uint32(head[:4]))
	if size < 16 || size > uint64(len(head)) || size%4 != 0 {
		return false
	}
	switch string(head[8:12]) {
	case "isom", "iso2", "iso3", "iso4", "iso5", "iso6", "iso7", "iso8", "iso9",
		"avc1", "mp41", "mp42", "M4V ", "qt  ", "dash":
		return true
	}
	return false
}

func hasMatroskaDocType(head []byte) bool {
	if !hasEBMLHeader(head) {
		return false
	}
	header, _, ok := ebmlElementData(head[4:])
	for ok && len(header) > 0 {
		idSize := bits.LeadingZeros8(header[0]) + 1
		if idSize > 4 || idSize >= len(header) {
			return false
		}
		value, rest, parsed := ebmlElementData(header[idSize:])
		if !parsed {
			return false
		}
		if idSize == 2 && header[0] == 0x42 && header[1] == 0x82 {
			return string(value) == "matroska" || string(value) == "webm"
		}
		header = rest
	}
	return false
}

// EBML size uses the first set bit to describe the width. Unknown lengths
// and lengths that run beyond the available header cannot identify a type.
func ebmlElementData(data []byte) (value, rest []byte, ok bool) {
	if len(data) == 0 {
		return nil, nil, false
	}
	width := bits.LeadingZeros8(data[0]) + 1
	if width > 8 || width > len(data) {
		return nil, nil, false
	}
	marker := byte(1 << (8 - width))
	size := uint64(data[0] & (marker - 1))
	for _, next := range data[1:width] {
		size = size<<8 | uint64(next)
	}
	if size == (uint64(1)<<(7*width))-1 || size > uint64(len(data)-width) {
		return nil, nil, false
	}
	end := width + int(size)
	return data[width:end], data[end:], true
}
