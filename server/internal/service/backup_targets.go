package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/storage"
	"qimeng.local/next/server/internal/store"
)

// Backup targets are deployment configuration, never arbitrary paths accepted
// from HTTP or a model. FailureDomain is an operator assertion; the service also
// requires a different filesystem volume and verifies actual copied bytes.
type BackupTarget struct {
	Path          string `json:"path"`
	FailureDomain string `json:"failureDomain"`
}
type BackupTargetInfo struct {
	ID             string `json:"id"`
	FailureDomain  string `json:"failureDomain"`
	Available      bool   `json:"available"`
	DistinctVolume bool   `json:"distinctVolume"`
}

func validateBackupTargets(targets map[string]BackupTarget, defaultID string) error {
	for id, target := range targets {
		if !safeBackupID(id) || len(id) > 64 || !filepath.IsAbs(target.Path) || strings.TrimSpace(target.FailureDomain) == "" || len(target.FailureDomain) > 128 {
			return errors.New("invalid trusted backup target configuration")
		}
	}
	if defaultID != "" {
		if _, ok := targets[defaultID]; !ok {
			return errors.New("default backup target is not configured")
		}
	}
	return nil
}
func (s *Service) backupTarget(ctx context.Context, id string) (BackupTarget, BackupTargetInfo, error) {
	target, ok := s.Config.BackupTargets[id]
	info := BackupTargetInfo{ID: id, FailureDomain: target.FailureDomain}
	if !ok {
		return target, info, Fail(409, "backup_target_unconfigured", "尚未配置可信独立备份目标。")
	}
	targetVolume, e := volumeIdentity(target.Path)
	if e != nil {
		return target, info, Fail(503, "backup_target_unavailable", "独立备份目标当前不可用。")
	}
	info.Available = true
	local, e := volumeIdentity(s.Config.DataDir)
	if e != nil {
		return target, info, e
	}
	info.DistinctVolume = targetVolume != local
	libraries, e := store.List[model.Library](ctx, s.DB.DB, "SELECT data FROM libraries")
	if e != nil {
		return target, info, e
	}
	for _, library := range libraries {
		root, e := s.Root.LibraryPath(library.ID)
		if e != nil {
			return target, info, e
		}
		mediaVolume, e := volumeIdentity(root)
		if e != nil {
			return target, info, e
		}
		if mediaVolume == targetVolume {
			info.DistinctVolume = false
		}
	}
	return target, info, nil
}

func (s *Service) protectionTarget(ctx context.Context, p BatchProtection) (BackupTarget, error) {
	target, info, err := s.backupTarget(ctx, p.TargetID)
	if err != nil {
		return target, err
	}
	if !info.DistinctVolume || target.FailureDomain != p.FailureDomain {
		return target, Fail(409, "backup_target_changed", "独立备份目标配置或卷归属已变化，来源必须保留。")
	}
	return target, nil
}

func (s *Service) BackupTargets(ctx context.Context, p model.Principal) (model.Page[BackupTargetInfo], error) {
	result := model.Page[BackupTargetInfo]{Items: []BackupTargetInfo{}}
	if e := Authorize(p, "admin", ""); e != nil {
		return result, e
	}
	ids := []string{}
	for id := range s.Config.BackupTargets {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		_, info, e := s.backupTarget(ctx, id)
		if e != nil {
			var app *Error
			if !errors.As(e, &app) || app.Code != "backup_target_unavailable" {
				return result, e
			}
		}
		result.Items = append(result.Items, info)
	}
	return result, nil
}

func verifyTargetObject(ctx context.Context, root *os.Root, name, expected string, size int64) error {
	return readTargetObject(ctx, root, name, expected, size, io.Discard)
}

func readTargetObject(ctx context.Context, root *os.Root, name, expected string, size int64, dst io.Writer) error {
	file, e := root.OpenFile(name, os.O_RDONLY|syncReadFlags, 0)
	if e != nil {
		return e
	}
	defer file.Close()
	stat, e := file.Stat()
	if e != nil {
		return e
	}
	if !stat.Mode().IsRegular() || stat.Size() != size {
		return Fail(409, "backup_target_content_changed", "目标备份对象长度改变，不能作为清理依据。")
	}
	actual, n, e := hashReaderContext(ctx, io.TeeReader(io.LimitReader(file, size), dst), nil)
	if e != nil {
		return e
	}
	current, e := root.Lstat(name)
	if e != nil {
		return e
	}
	if n != size || actual != expected || current.Size() != size || syncPathLink(current) || !os.SameFile(stat, current) {
		return Fail(409, "backup_target_content_changed", "目标备份对象已变化，不能作为清理依据。")
	}
	return nil
}
func copyTargetObject(ctx context.Context, root *os.Root, temporary, destination, hash string, size, reserve int64, open func() (io.ReadCloser, error)) error {
	if _, e := root.Lstat(destination); e == nil {
		return verifyTargetObject(ctx, root, destination, hash, size)
	} else if !errors.Is(e, os.ErrNotExist) {
		return e
	}
	if e := root.MkdirAll(path.Dir(temporary), 0700); e != nil {
		return e
	}
	if e := root.MkdirAll(path.Dir(destination), 0700); e != nil {
		return e
	}
	source, e := open()
	if e != nil {
		return e
	}
	defer source.Close()
	out, e := root.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if e != nil {
		return e
	}
	digest := sha256.New()
	writer := io.MultiWriter(&restoreWriter{ctx: ctx, root: root.Name(), reserve: reserve, file: out}, digest)
	_, e = io.CopyN(writer, source, size)
	if e == nil && hex.EncodeToString(digest.Sum(nil)) != hash {
		e = Fail(409, "backup_source_changed", "输入内容在备份期间变化。")
	}
	if e == nil {
		e = out.Sync()
	}
	closeErr := out.Close()
	if e == nil {
		e = closeErr
	}
	if e != nil {
		return e
	}
	if e = storage.PublishTemp(root, temporary, destination); e != nil {
		if !errors.Is(e, os.ErrExist) {
			return e
		}
		if e = verifyTargetObject(ctx, root, destination, hash, size); e != nil {
			return e
		}
		if e = root.Remove(temporary); e != nil {
			return e
		}
	}
	folder, e := root.Open(path.Dir(destination))
	if e != nil {
		return e
	}
	e = syncDirectory(folder)
	folder.Close()
	if e != nil {
		return e
	}
	// Flush completion alone is not proof: reopen and hash the target bytes.
	return verifyTargetObject(ctx, root, destination, hash, size)
}
