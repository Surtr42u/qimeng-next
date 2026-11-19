package phonerelink

import (
	"context"
	"errors"
	"os"
	"path"
	"path/filepath"
)

// TrashFile is an explicit no-overwrite deployment item below phone. Its input
// comes only from the native restore's independent per-asset trash object.
type TrashFile struct {
	Asset   string `json:"assetId"`
	Library string `json:"libraryId"`
	Path    string `json:"phoneRelativePath"`
	Size    int64  `json:"size"`
	Hash    string `json:"sha256"`
	Mtime   int64  `json:"mtimeUnixNs"`
}

func (item *TrashFile) stage(ctx context.Context, restored, ready string) error {
	leaf := path.Base(item.Path)
	name := filepath.Join(restored, "libraries", item.Library, ".qimeng-internal", "trash", item.Asset, leaf)
	source, err := openInput(name, false)
	if err != nil {
		return errors.Join(refusal("restored_trash_original_unavailable"), err)
	}
	defer source.close()
	if source.info.Size() != item.Size {
		return refusal("restored_trash_original_differs")
	}
	target := filepath.Join(ready, "phone-files", filepath.FromSlash(item.Path))
	folder := filepath.Dir(target)
	if err := os.MkdirAll(folder, 0700); err != nil {
		return err
	}
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	err = source.copy(ctx, out, item.Hash)
	if err == nil {
		err = os.Chtimes(target, source.info.ModTime(), source.info.ModTime())
	}
	if err == nil {
		err = out.Sync()
	}
	if err := errors.Join(err, out.Close()); err != nil {
		return errors.Join(refusal("restored_trash_original_differs"), err)
	}
	hash, err := hashFile(target)
	if err != nil || hash != item.Hash {
		return errors.Join(refusal("staged_trash_readback_failed"), err)
	}
	item.Mtime = source.info.ModTime().UnixNano()
	for dir := folder; dir != ready; dir = filepath.Dir(dir) {
		if err := syncDir(dir); err != nil {
			return err
		}
	}
	return nil
}
