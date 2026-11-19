// Package assetmerge claims a new, stopped native restore for a single offline
// owner decision. Filesystem writes and SQLite projections stay within that copy.
package assetmerge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/disk"
)

type area struct {
	name string
	root *os.Root
	info os.FileInfo
}

func plainDirectory(name string) error {
	if !filepath.IsAbs(name) || filepath.Clean(name) != name {
		return errors.New("a clean absolute directory is required")
	}
	parent := filepath.Dir(name)
	if parent != name {
		if err := plainDirectory(parent); err != nil {
			return err
		}
	}
	info, err := os.Lstat(name)
	if err != nil {
		return err
	}
	if linked(info) || !info.IsDir() {
		return errors.New("directory is linked or unavailable")
	}
	return nil
}

func openArea(name string) (*area, error) {
	if err := plainDirectory(name); err != nil {
		return nil, err
	}
	info, err := os.Lstat(name)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	a := &area{name, root, info}
	if err := a.bound(); err != nil {
		root.Close()
		return nil, err
	}
	return a, nil
}

func (a *area) bound() error {
	if err := plainDirectory(a.name); err != nil {
		return err
	}
	named, err := os.Lstat(a.name)
	if err != nil {
		return err
	}
	pinned, err := a.root.Stat(".")
	if err != nil {
		return err
	}
	if !os.SameFile(named, a.info) || !os.SameFile(pinned, a.info) {
		return errors.New("private parent changed")
	}
	return nil
}

func (a *area) inspect(name string) (os.FileInfo, error) {
	if !fs.ValidPath(name) || strings.ContainsAny(name, "\\:\x00") {
		return nil, fs.ErrInvalid
	}
	parts := strings.Split(name, "/")
	var info os.FileInfo
	for i := range parts {
		var err error
		info, err = a.root.Lstat(strings.Join(parts[:i+1], "/"))
		if err != nil {
			return nil, err
		}
		if linked(info) || (i < len(parts)-1 && !info.IsDir()) {
			return nil, errors.New("unsafe file or parent")
		}
	}
	return info, nil
}

func (a *area) absent(name string) error {
	_, err := a.inspect(name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err == nil {
		return fs.ErrExist
	}
	return err
}

func (a *area) mkdir(name string) error {
	if err := a.absent(name); err != nil {
		return err
	}
	return a.root.Mkdir(name, 0700)
}

func (a *area) mkdirAll(name string) error {
	if _, err := a.inspect(name); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return a.root.MkdirAll(name, 0700)
}

func sameInfo(left, right os.FileInfo) bool {
	return os.SameFile(left, right) && left.Size() == right.Size() && left.ModTime().Equal(right.ModTime()) && left.Mode().Type() == right.Mode().Type()
}

func (a *area) openRegular(name string) (*os.File, os.FileInfo, error) {
	info, err := a.inspect(name)
	if err != nil {
		return nil, nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, nil, errors.New("expected a regular file")
	}
	file, err := a.root.Open(name)
	if err != nil {
		return nil, nil, err
	}
	pinned, err := file.Stat()
	if err == nil && !sameInfo(info, pinned) {
		err = errors.New("opened file changed")
	}
	if err == nil {
		err = singleLink(file)
	}
	if err != nil {
		file.Close()
		return nil, nil, err
	}
	return file, info, nil
}

type cancelReader struct {
	ctx context.Context
	r   io.Reader
}

func (r cancelReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

type stamp struct {
	Hash   string    `json:"sha256"`
	Size   int64     `json:"size"`
	Time   time.Time `json:"modifiedAt"`
	Object string    `json:"fileIdentity"`
	info   os.FileInfo
}

// readFile ties the digest to the same named file before and after reading.
func (a *area) readFile(ctx context.Context, name string, out io.Writer) (stamp, error) {
	f, info, err := a.openRegular(name)
	if err != nil {
		return stamp{}, err
	}
	defer f.Close()
	hash := sha256.New()
	if out == nil {
		out = hash
	} else {
		out = io.MultiWriter(out, hash)
	}
	n, err := io.Copy(out, cancelReader{ctx, f})
	if err != nil {
		return stamp{}, err
	}
	after, err := a.inspect(name)
	if err != nil {
		return stamp{}, err
	}
	if n != info.Size() || !sameInfo(info, after) {
		return stamp{}, errors.New("file identity, length or modification time changed")
	}
	object, err := fileIdentity(f)
	if err != nil {
		return stamp{}, err
	}
	return stamp{hex.EncodeToString(hash.Sum(nil)), n, info.ModTime().UTC(), object, info}, nil
}

func (s stamp) matches(other stamp) bool {
	return s.Hash == other.Hash && s.Size == other.Size && s.Time.Equal(other.Time) && s.Object == other.Object && (s.info == nil || os.SameFile(s.info, other.info))
}

func (a *area) capacity(ctx context.Context, reserve, need int64) error {
	usage, err := disk.UsageWithContext(ctx, a.name)
	if err != nil {
		return err
	}
	if usage.Free < uint64(reserve) || usage.Free-uint64(reserve) < uint64(need) {
		return errors.New("insufficient private disk space")
	}
	return nil
}

type reservedWriter struct {
	ctx            context.Context
	a              *area
	reserve, since int64
	out            io.Writer
}

func (w *reservedWriter) Write(p []byte) (int, error) {
	if w.since <= 0 {
		if err := w.a.capacity(w.ctx, w.reserve, int64(len(p))+(8<<20)); err != nil {
			return 0, err
		}
		w.since = 8 << 20
	}
	n, err := w.out.Write(p)
	w.since -= int64(n)
	return n, err
}

func (a *area) copy(ctx context.Context, from, to string, reserve int64) (stamp, error) {
	if err := a.absent(to); err != nil {
		return stamp{}, err
	}
	out, err := a.root.OpenFile(to, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return stamp{}, err
	}
	st, err := a.readFile(ctx, from, &reservedWriter{ctx: ctx, a: a, reserve: reserve, out: out})
	if err == nil {
		err = a.root.Chtimes(to, st.Time, st.Time)
	}
	if err == nil {
		err = out.Sync()
	}
	return st, errors.Join(err, out.Close())
}

func (a *area) move(from, to string, info os.FileInfo) error {
	if err := a.bound(); err != nil {
		return err
	}
	before, err := a.inspect(from)
	if err != nil {
		return err
	}
	if !os.SameFile(before, info) || (!info.IsDir() && !sameInfo(before, info)) {
		return errors.New("source of publication changed")
	}
	if err := a.absent(to); err != nil {
		return err
	}
	if err := renameExclusive(a, from, to); err != nil {
		return err
	}
	after, err := a.inspect(to)
	if err != nil || !os.SameFile(before, after) {
		return errors.New("published entry has a different identity")
	}
	return errors.Join(a.syncDir(path.Dir(from)), a.syncDir(path.Dir(to)))
}

func (a *area) syncDir(name string) error {
	d, err := a.root.Open(name)
	if err != nil {
		return err
	}
	defer d.Close()
	return syncDir(d)
}
