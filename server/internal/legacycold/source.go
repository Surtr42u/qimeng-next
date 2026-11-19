// Package legacycold converts a sealed PC v26 catalog and its explicitly
// selected originals. Only the one-time offline command imports this package.
package legacycold

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"qimeng.local/next/server/internal/domain"
	"qimeng.local/next/server/internal/store"

	_ "modernc.org/sqlite"
)

type issue struct {
	Code    string `json:"code"`
	Table   string `json:"table,omitempty"`
	RowID   int64  `json:"rowId,omitempty"`
	Locator string `json:"locatorSHA256,omitempty"`
}

func (i *issue) Error() string { return i.Code }

func failed(code string) error { return &issue{Code: code} }

func locate(err error, table string, row int64, locator string) error {
	var i *issue
	if !errors.As(err, &i) {
		i = &issue{Code: "read_or_write_failed"}
	}
	copy := *i
	copy.Table, copy.RowID = table, row
	if locator != "" {
		sum := sha256.Sum256([]byte(locator))
		copy.Locator = hex.EncodeToString(sum[:])
	}
	return &copy
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

// A root and each path component must be ordinary filesystem objects. os.Root
// also anchors the open on platforms where a directory entry can be exchanged.
func absolute(name string) (string, error) {
	if !filepath.IsAbs(name) || filepath.Clean(name) != name {
		return "", failed("absolute_clean_path_required")
	}
	return name, nil
}

func realDirectory(name string) (os.FileInfo, error) {
	info, err := os.Lstat(name)
	if err != nil || !info.IsDir() || info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
		return nil, failed("directory_missing_or_linked")
	}
	parent := filepath.Dir(name)
	if parent != name {
		if _, err = realDirectory(parent); err != nil {
			return nil, err
		}
	}
	return info, nil
}

type readRoot struct {
	name string
	root *os.Root
	info os.FileInfo
}

func openDirectory(name string) (*readRoot, error) {
	if _, err := absolute(name); err != nil {
		return nil, err
	}
	info, err := realDirectory(name)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(name)
	if err != nil {
		return nil, failed("directory_unavailable")
	}
	got, err := root.Stat(".")
	if err != nil || !os.SameFile(info, got) {
		root.Close()
		return nil, failed("directory_identity_changed")
	}
	return &readRoot{name, root, info}, nil
}

func (r *readRoot) check() error {
	now, err := realDirectory(r.name)
	if err != nil || !os.SameFile(now, r.info) {
		return failed("directory_identity_changed")
	}
	return nil
}

func noOutputInside(parent string, sources []*readRoot) error {
	for {
		info, err := os.Stat(parent)
		if err != nil {
			return failed("output_parent_unavailable")
		}
		for _, source := range sources {
			if os.SameFile(source.info, info) {
				return failed("output_inside_source")
			}
		}
		next := filepath.Dir(parent)
		if next == parent {
			return nil
		}
		parent = next
	}
}

type fileRef struct {
	base *readRoot
	rel  string // forward slashes, relative to the opened directory
	info os.FileInfo
}

func (r *readRoot) openFile(rel string) (*os.File, error) {
	if !fs.ValidPath(rel) || rel == "." || strings.ContainsAny(rel, "\\:\x00") {
		return nil, failed("invalid_source_path")
	}
	prefix := ""
	var expected os.FileInfo
	for part := range strings.SplitSeq(rel, "/") {
		prefix += part
		info, err := r.root.Lstat(filepath.FromSlash(prefix))
		if err != nil {
			return nil, failed("source_file_missing")
		}
		if info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 || prefix != rel && !info.IsDir() || prefix == rel && !info.Mode().IsRegular() {
			return nil, failed("source_link_or_special_file")
		}
		expected = info
		prefix += "/"
	}
	f, err := r.root.Open(filepath.FromSlash(rel))
	if err != nil {
		return nil, failed("source_file_unreadable")
	}
	info, err := f.Stat()
	if err != nil || !os.SameFile(info, expected) {
		f.Close()
		return nil, failed("source_file_changed")
	}
	return f, nil
}

func matchFile(a, b os.FileInfo) bool {
	return a != nil && b != nil && os.SameFile(a, b) && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime())
}

func (r fileRef) open() (*os.File, error) {
	f, err := r.base.openFile(r.rel)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || !matchFile(r.info, info) {
		f.Close()
		return nil, failed("source_file_changed")
	}
	return f, nil
}

func (r fileRef) check() error {
	f, err := r.open()
	if err != nil {
		return err
	}
	return f.Close()
}

func sealFile(ctx context.Context, root *readRoot, rel string) (fileRef, string, error) {
	ref := fileRef{base: root, rel: rel}
	f, err := root.openFile(rel)
	if err != nil {
		return ref, "", err
	}
	defer f.Close()
	ref.info, err = f.Stat()
	if err != nil {
		return ref, "", failed("source_file_stat_failed")
	}
	hash := sha256.New()
	n, err := io.Copy(hash, contextReader{ctx, f})
	if err != nil || n != ref.info.Size() {
		return ref, "", failed("source_read_interrupted")
	}
	if err = ref.check(); err != nil {
		return ref, "", err
	}
	return ref, hex.EncodeToString(hash.Sum(nil)), nil
}

func noJournal(r *readRoot, dbName string) error {
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if _, err := r.root.Lstat(dbName + suffix); !errors.Is(err, fs.ErrNotExist) {
			return failed("sealed_database_with_no_sidecars_required")
		}
	}
	return nil
}

func openSealed(ctx context.Context, root *readRoot, name string) (*sql.DB, fileRef, string, error) {
	if err := noJournal(root, name); err != nil {
		return nil, fileRef{}, "", err
	}
	ref, hash, err := sealFile(ctx, root, name)
	if err != nil {
		return nil, ref, "", err
	}
	// immutable prevents SQLite from creating SHM/WAL next to a checkpointed
	// WAL-mode database. The operator freezes writers for the entire conversion.
	db, err := sql.Open("sqlite", store.FileURI(filepath.Join(root.name, name))+
		"?mode=ro&immutable=1&_pragma=trusted_schema(0)&_pragma=query_only(1)")
	if err != nil {
		return nil, ref, "", failed("sealed_database_unreadable")
	}
	db.SetMaxOpenConns(4)
	var valid bool
	if err = db.QueryRowContext(ctx, `SELECT count(*)=1 AND min(version)=26 AND max(dirty)=0 FROM schema_migrations`).Scan(&valid); err != nil || !valid {
		db.Close()
		return nil, ref, "", failed("clean_v26_database_required")
	}
	return db, ref, hash, nil
}

func readMapping(ctx context.Context, filename string) (map[string]string, error) {
	if _, err := absolute(filename); err != nil {
		return nil, err
	}
	root, err := openDirectory(filepath.Dir(filename))
	if err != nil {
		return nil, err
	}
	defer root.root.Close()
	f, err := root.openFile(filepath.Base(filename))
	if err != nil {
		return nil, failed("roots_file_unavailable")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.Size() > 2<<20 {
		return nil, failed("roots_file_size_invalid")
	}
	dec := json.NewDecoder(contextReader{ctx, io.LimitReader(f, 2<<20)})
	token, err := dec.Token()
	if err != nil || token != json.Delim('{') {
		return nil, failed("invalid_roots_json")
	}
	mapping := map[string]string{}
	for dec.More() {
		token, err = dec.Token()
		id, ok := token.(string)
		var name string
		if err != nil || !ok || !domain.ValidName(id) || dec.Decode(&name) != nil {
			return nil, failed("invalid_roots_entry")
		}
		if _, err = absolute(name); err != nil {
			return nil, err
		}
		if _, exists := mapping[id]; exists {
			return nil, failed("duplicate_roots_entry")
		}
		mapping[id] = name
	}
	token, err = dec.Token()
	if err != nil || token != json.Delim('}') || dec.Decode(new(any)) != io.EOF {
		return nil, failed("invalid_roots_json")
	}
	return mapping, nil
}
