// Package mediafile inspects media bytes without acquiring catalog locks or
// resolving paths. The caller owns the reader, authority and published identity.
package mediafile

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"path"
	"strings"
)

var ErrContainer = errors.New("media container does not match its declaration")

func validContainer(name, declared string, head []byte) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".avif":
		return len(head) >= 12 && string(head[4:8]) == "ftyp" && (bytes.Contains(head[8:], []byte("avif")) || bytes.Contains(head[8:], []byte("avis")))
	case ".mp4", ".m4v", ".mov":
		return len(head) >= 12 && string(head[4:8]) == "ftyp"
	case ".mkv", ".webm":
		return bytes.HasPrefix(head, []byte{0x1a, 0x45, 0xdf, 0xa3})
	case ".avi":
		return len(head) >= 12 && string(head[:4]) == "RIFF" && string(head[8:12]) == "AVI "
	case ".webp":
		return len(head) >= 12 && string(head[:4]) == "RIFF" && string(head[8:12]) == "WEBP"
	default:
		return http.DetectContentType(head) == declared
	}
}

type cancellableReader struct {
	ctx    context.Context
	source io.Reader
}

func (r cancellableReader) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.source.Read(buffer)
}

func Inspect(ctx context.Context, source io.Reader, name, declared string) (string, int64, error) {
	reader := cancellableReader{ctx, source}
	hash := sha256.New()
	head := make([]byte, 512)
	n, err := io.ReadFull(reader, head)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return "", 0, err
	}
	head = head[:n]
	if !validContainer(name, declared, head) {
		return "", 0, ErrContainer
	}
	hash.Write(head)
	remaining, err := io.Copy(hash, reader)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(hash.Sum(nil)), int64(n) + remaining, nil
}
