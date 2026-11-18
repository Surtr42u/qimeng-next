// Package backup defines the portable, streaming Next backup format. It has no
// database or service dependency; filenames in the container are opaque slots.
package backup

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

const Format = "qimeng-next-backup"
const Version = 1
const MaxManifestBytes = 64 << 20
const MaxFiles = 500000

type File struct {
	Kind           string     `json:"kind"`
	ObjectID       string     `json:"objectId"`
	ContentVersion int64      `json:"contentVersion,omitempty"`
	StoragePath    string     `json:"storagePath"`
	SHA256         string     `json:"sha256"`
	Size           int64      `json:"size"`
	ModifiedAt     *time.Time `json:"modifiedAt,omitempty"`
}
type Manifest struct {
	Format     string    `json:"format"`
	Version    int       `json:"version"`
	BackupID   string    `json:"backupId"`
	InstanceID string    `json:"instanceId"`
	CreatedAt  time.Time `json:"createdAt"`
	Snapshot   File      `json:"snapshot"`
	Files      []File    `json:"files"`
}

func validHash(value string) bool {
	b, e := hex.DecodeString(value)
	return e == nil && len(b) == sha256.Size && value == strings.ToLower(value)
}
func (m Manifest) Validate() error {
	if m.Format != Format || m.Version != Version || m.BackupID == "" || m.InstanceID == "" || m.CreatedAt.IsZero() || m.Snapshot.Kind != "snapshot" || m.Snapshot.StoragePath != "qimeng.sqlite" || m.Snapshot.Size < 1 || !validHash(m.Snapshot.SHA256) || len(m.Files) > MaxFiles {
		return errors.New("invalid backup manifest")
	}
	seen := map[string]bool{}
	paths := map[string]bool{}
	for _, f := range m.Files {
		key := f.Kind + ":" + f.ObjectID
		if f.ObjectID == "" || len(f.ObjectID) > 256 || f.Size < 0 || !validHash(f.SHA256) || (f.Kind != "asset" && f.Kind != "upload") || f.Kind == "asset" && f.ContentVersion < 1 || f.StoragePath == "" || seen[key] || paths[f.StoragePath] {
			return errors.New("invalid backup file identity")
		}
		seen[key] = true
		paths[f.StoragePath] = true
	}
	return nil
}
func Encode(m Manifest) ([]byte, error) {
	if e := m.Validate(); e != nil {
		return nil, e
	}
	raw, e := json.Marshal(m)
	if e != nil {
		return nil, e
	}
	if len(raw) > MaxManifestBytes {
		return nil, errors.New("backup manifest exceeds resource budget")
	}
	return raw, nil
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if e := r.ctx.Err(); e != nil {
		return 0, e
	}
	return r.r.Read(p)
}
func slot(i int) string {
	if i < 0 {
		return "snapshot.sqlite"
	}
	return fmt.Sprintf("payload/%08d", i)
}

// Write verifies every source while copying. A partial archive has no successful
// result and must never be published under the final archive name.
func Write(ctx context.Context, out io.Writer, m Manifest, open func(File) (io.ReadCloser, error), progress func(int) error) error {
	raw, e := Encode(m)
	if e != nil {
		return e
	}
	writer := tar.NewWriter(out)
	if e = writer.WriteHeader(&tar.Header{Name: "manifest.json", Mode: 0600, Size: int64(len(raw)), Typeflag: tar.TypeReg}); e != nil {
		return e
	}
	if _, e = writer.Write(raw); e != nil {
		return e
	}
	write := func(index int, f File) error {
		source, e := open(f)
		if e != nil {
			return e
		}
		defer source.Close()
		if e = writer.WriteHeader(&tar.Header{Name: slot(index), Mode: 0600, Size: f.Size, Typeflag: tar.TypeReg}); e != nil {
			return e
		}
		hash := sha256.New()
		if _, e = io.CopyN(io.MultiWriter(writer, hash), contextReader{ctx, source}, f.Size); e != nil {
			return e
		}
		if hex.EncodeToString(hash.Sum(nil)) != f.SHA256 {
			return errors.New("backup source content changed")
		}
		return nil
	}
	if e = write(-1, m.Snapshot); e != nil {
		return e
	}
	for i, f := range m.Files {
		if e = write(i, f); e != nil {
			return e
		}
		if progress != nil {
			if e = progress(i + 1); e != nil {
				return e
			}
		}
	}
	return writer.Close()
}

// Read validates names, lengths and hashes before returning a verified manifest.
// consume receives only the exact bytes of one regular file, in manifest order.
// Callers must publish extracted data only after Read succeeds in full.
func Read(ctx context.Context, input io.Reader, consume func(File, io.Reader) error) (Manifest, error) {
	var m Manifest
	reader := tar.NewReader(contextReader{ctx, input})
	header, e := reader.Next()
	if e != nil {
		return m, e
	}
	if header.Name != "manifest.json" || header.Typeflag != tar.TypeReg || header.Size < 1 || header.Size > MaxManifestBytes {
		return m, errors.New("missing or oversized backup manifest")
	}
	raw, e := io.ReadAll(reader)
	if e != nil {
		return m, e
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if e = decoder.Decode(&m); e != nil {
		return m, e
	}
	if e = decoder.Decode(new(any)); e != io.EOF {
		return m, errors.New("trailing manifest JSON")
	}
	if e = m.Validate(); e != nil {
		return m, e
	}
	read := func(index int, f File) error {
		header, e := reader.Next()
		if e != nil {
			return e
		}
		if header.Name != slot(index) || header.Typeflag != tar.TypeReg || header.Size != f.Size || header.Linkname != "" {
			return errors.New("backup entry differs from manifest")
		}
		hash := sha256.New()
		verified := io.TeeReader(reader, hash)
		if consume == nil {
			_, e = io.Copy(io.Discard, verified)
		} else {
			e = consume(f, verified)
		}
		if e != nil {
			return e
		}
		var extra [1]byte
		n, e := verified.Read(extra[:])
		if n != 0 || e != io.EOF {
			return errors.New("backup consumer did not read the full entry")
		}
		if hex.EncodeToString(hash.Sum(nil)) != f.SHA256 {
			return errors.New("backup content checksum mismatch")
		}
		return nil
	}
	if e = read(-1, m.Snapshot); e != nil {
		return m, e
	}
	for i, f := range m.Files {
		if e = read(i, f); e != nil {
			return m, e
		}
	}
	if _, e = reader.Next(); e != io.EOF {
		return m, errors.New("unlisted backup payload")
	}
	var extra [1]byte
	if n, e := input.Read(extra[:]); n != 0 || e != io.EOF {
		return m, errors.New("trailing backup bytes")
	}
	return m, nil
}
