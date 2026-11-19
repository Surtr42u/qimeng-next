// Package phonerelink prepares a private catalog for an empty, initialized
// mobile instance. Its only view of phone storage is a frozen physical census.
package phonerelink

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

type refusal string

func (r refusal) Error() string { return string(r) }

// FailureCode deliberately excludes private paths and database values.
func FailureCode(err error) string {
	var reason refusal
	if errors.As(err, &reason) {
		return string(reason)
	}
	return "private_io_or_database_failure"
}

func isDigest(value string) bool {
	raw, err := hex.DecodeString(value)
	return err == nil && len(raw) == sha256.Size && strings.ToLower(value) == value
}

func plainDir(dir string) error {
	parent := filepath.Dir(dir)
	if parent != dir {
		if err := plainDir(parent); err != nil {
			return err
		}
	}
	fi, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !fi.IsDir() || linked(fi) {
		return refusal("linked_or_invalid_private_directory")
	}
	return nil
}

type inputFile struct {
	root *os.Root
	name string
	file *os.File
	info os.FileInfo
	cold bool
}

func openInput(name string, cold bool) (*inputFile, error) {
	dir := filepath.Dir(name)
	if err := plainDir(dir); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	input := &inputFile{root: root, name: filepath.Base(name), cold: cold}
	input.info, err = root.Lstat(input.name)
	if err == nil && (!input.info.Mode().IsRegular() || linked(input.info)) {
		err = refusal("linked_or_invalid_private_file")
	}
	if err == nil {
		input.file, err = root.Open(input.name)
	}
	if err == nil {
		err = input.check()
	}
	if err != nil {
		input.close()
		return nil, err
	}
	return input, nil
}

func (f *inputFile) close() {
	if f.file != nil {
		f.file.Close()
	}
	f.root.Close()
}

func (f *inputFile) check() error {
	current, err := f.root.Lstat(f.name)
	if err != nil {
		return err
	}
	pinned, err := f.file.Stat()
	if err != nil {
		return err
	}
	if linked(current) || !os.SameFile(f.info, current) || !os.SameFile(current, pinned) ||
		f.info.Size() != current.Size() || !f.info.ModTime().Equal(current.ModTime()) {
		return refusal("private_input_changed")
	}
	if f.cold {
		for _, suffix := range []string{"-wal", "-shm", "-journal"} {
			_, err := f.root.Lstat(f.name + suffix)
			if err == nil {
				return refusal("cold_database_has_sidecars")
			}
			if !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
	}
	return nil
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

func (f *inputFile) copy(ctx context.Context, dst io.Writer, digest string) error {
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(dst, hash), io.LimitReader(contextReader{ctx, f.file}, f.info.Size()+1))
	if err != nil {
		return err
	}
	if n != f.info.Size() || hex.EncodeToString(hash.Sum(nil)) != digest {
		return refusal("private_input_digest_mismatch")
	}
	return f.check()
}

func copyCatalog(ctx context.Context, input *inputFile, name, digest string) error {
	out, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	return errors.Join(input.copy(ctx, out, digest), out.Close())
}

// encoding/json matches struct keys without regard to case and otherwise
// accepts duplicate names. Reject ambiguous approval and evidence bytes.
func uniqueJSONKeys(d *json.Decoder, depth int) error {
	if depth > 8 {
		return refusal("invalid_evidence_json")
	}
	token, err := d.Token()
	if err != nil {
		return err
	}
	kind, container := token.(json.Delim)
	if !container {
		return nil
	}
	seen := map[string]bool{}
	for d.More() {
		if kind == '{' {
			token, err := d.Token()
			if err != nil {
				return err
			}
			key, ok := token.(string)
			key = strings.ToLower(key)
			if !ok || seen[key] {
				return refusal("invalid_evidence_json")
			}
			seen[key] = true
		}
		if err := uniqueJSONKeys(d, depth+1); err != nil {
			return err
		}
	}
	_, err = d.Token()
	return err
}

func readJSON(ctx context.Context, input *inputFile, digest string, limit int64, dst any) error {
	if input.info.Size() < 1 || input.info.Size() > limit {
		return refusal("invalid_evidence_length")
	}
	var raw bytes.Buffer
	if digest == "" { // The restore receipt is matched to the verified archive and restored catalog.
		n, err := io.Copy(&raw, io.LimitReader(contextReader{ctx, input.file}, limit+1))
		if err != nil {
			return err
		}
		if n != input.info.Size() {
			return refusal("private_input_changed")
		}
	} else if err := input.copy(ctx, &raw, digest); err != nil {
		return err
	}
	body := bytes.TrimPrefix(raw.Bytes(), []byte{0xef, 0xbb, 0xbf})
	if !utf8.Valid(body) {
		return refusal("invalid_evidence_json")
	}
	keys := json.NewDecoder(bytes.NewReader(body))
	keys.UseNumber()
	if err := uniqueJSONKeys(keys, 0); err != nil {
		return refusal("invalid_evidence_json")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return refusal("invalid_evidence_json")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return refusal("invalid_evidence_json")
	}
	return input.check()
}

func hashFile(name string) (string, error) {
	f, err := os.Open(name)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
