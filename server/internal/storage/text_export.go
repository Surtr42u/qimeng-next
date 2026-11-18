package storage

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// TextExportRoot anchors every current/previous publication below an already
// open operator root. No path supplied by a client registers a new root.
func (r *Root) TextExportRoot(directory string) (*os.Root, string, error) {
	id, relative, e := r.ResolveLibraryPath(directory)
	if e != nil {
		return nil, "", e
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	loc := r.locations[id]
	for _, part := range strings.Split(relative, "/") {
		if strings.EqualFold(part, ".qimeng-internal") {
			return nil, "", fs.ErrPermission
		}
	}
	// Internal media-maintenance paths and the service control root are private.
	canonical := filepath.Join(loc.absolute, filepath.FromSlash(relative))
	if overlaps(canonical, r.locations["data"].absolute) {
		return nil, "", fs.ErrPermission
	}
	if e = loc.root.MkdirAll(filepath.FromSlash(relativeOrDot(relative)), 0700); e != nil {
		return nil, "", e
	}
	dir, e := loc.root.OpenRoot(filepath.FromSlash(relativeOrDot(relative)))
	if e != nil {
		return nil, "", e
	}
	info, e := loc.root.Stat(filepath.FromSlash(relativeOrDot(relative)))
	if e != nil {
		dir.Close()
		return nil, "", e
	}
	resolved, e := dir.Stat(".")
	if e != nil || !os.SameFile(info, resolved) {
		dir.Close()
		return nil, "", errors.New("export directory changed")
	}
	return dir, relative, nil
}
func relativeOrDot(relative string) string {
	if relative == "" {
		return "."
	}
	return relative
}
