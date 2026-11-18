package service

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"os"
	"path"
	"slices"
	"strings"
	"time"

	"qimeng.local/next/server/internal/domain"
	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/store"
)

func (s *Service) localSyncLibraries(ctx context.Context) (map[string][]model.Library, error) {
	all, e := store.List[model.Library](ctx, s.DB.DB, "SELECT data FROM libraries ORDER BY id")
	if e != nil {
		return nil, e
	}
	out := map[string][]model.Library{}
	for _, lib := range all {
		name := strings.Map(func(r rune) rune {
			if strings.ContainsRune(`\/:*?"<>|`, r) {
				return '_'
			}
			return r
		}, strings.TrimSpace(lib.Name))
		out[name] = append(out[name], lib)
	}
	return out, nil
}

type InputDirectoryTarget struct {
	Name        string `json:"name"`
	State       string `json:"state"`
	LibraryID   string `json:"libraryId,omitempty"`
	LibraryName string `json:"libraryName,omitempty"`
}

func inputDirectoryTarget(libs map[string][]model.Library, name string) (InputDirectoryTarget, model.Library) {
	t := InputDirectoryTarget{Name: name}
	matches := libs[name]
	switch {
	case len(matches) == 0:
		t.State = "missing"
	case len(matches) > 1:
		t.State = "ambiguous"
	case !matches[0].Enabled:
		t.State = "disabled"
	case matches[0].Kind != "regular":
		t.State = "unsupported"
	default:
		t.State = "matched"
	}
	if len(matches) == 1 {
		t.LibraryID = matches[0].ID
		t.LibraryName = matches[0].Name
		return t, matches[0]
	}
	return t, model.Library{}
}
func (s *Service) ResolveInputDirectories(ctx context.Context, p model.Principal, names []string) (model.Page[InputDirectoryTarget], error) {
	out := model.Page[InputDirectoryTarget]{Items: []InputDirectoryTarget{}}
	if e := Authorize(p, "admin", ""); e != nil {
		return out, e
	}
	if names == nil || len(names) > 100 {
		return out, Fail(400, "invalid_input_directories", "请提交最多100个目录名。")
	}
	libs, e := s.localSyncLibraries(ctx)
	if e != nil {
		return out, e
	}
	for _, name := range names {
		if !domain.ValidName(name) {
			return out, Fail(400, "invalid_input_directory", "只接受单级目录名。")
		}
		item, _ := inputDirectoryTarget(libs, name)
		out.Items = append(out.Items, item)
	}
	return out, nil
}
func findSyncLibrary(libs map[string][]model.Library, relative string) (model.Library, error) {
	folder, _, _ := strings.Cut(relative, "/")
	target, l := inputDirectoryTarget(libs, folder)
	if target.State == "matched" {
		return l, nil
	}
	if target.State == "missing" || target.State == "ambiguous" {
		return l, Fail(409, "local_sync_library_ambiguous", "投放目录必须恰好匹配一个媒体库。")
	}
	return l, Fail(409, "local_sync_library_unavailable", "投放仅接受已启用的常规库。")
}
func openSyncSource(root *os.Root, name string) (*os.File, os.FileInfo, error) {
	parts := strings.Split(name, "/")
	var entry os.FileInfo
	for i := range parts {
		var e error
		entry, e = root.Lstat(strings.Join(parts[:i+1], "/"))
		if e != nil {
			return nil, nil, e
		}
		if syncPathLink(entry) {
			return nil, nil, Fail(409, "local_sync_link", "投放目录中的链接已跳过。")
		}
	}
	if !entry.IsDir() && !entry.Mode().IsRegular() {
		return nil, nil, Fail(409, "local_sync_special_file", "投放目录只读取普通文件。")
	}
	f, e := root.OpenFile(name, os.O_RDONLY|syncReadFlags, 0)
	if e != nil {
		return nil, nil, e
	}
	info, e := f.Stat()
	if e != nil {
		f.Close()
		return nil, nil, e
	}
	if !os.SameFile(entry, info) {
		f.Close()
		return nil, nil, Fail(409, "local_sync_changed", "来源在检查期间变化。")
	}
	return f, info, nil
}
func recordSyncItem(v *LocalSyncStatus, item LocalSyncItem) {
	switch item.State {
	case "waiting-stable":
		v.Counts.WaitingStable++
	case "failed":
		v.Counts.Failed++
	case "ignored":
		v.Counts.Ignored++
	default:
		return
	}
	i, _ := slices.BinarySearchFunc(v.Items, item.Path, func(a LocalSyncItem, b string) int { return strings.Compare(a.Path, b) })
	if i >= 500 {
		return
	}
	if len(v.Items) == 500 {
		v.Items = v.Items[:499]
	}
	v.Items = slices.Insert(v.Items, i, item)
}
func (s *Service) walkSyncDirectory(ctx context.Context, root *os.Root, dir string, depth int, cycle string, libs map[string][]model.Library, v *LocalSyncStatus) error {
	name := dir
	if name == "" {
		name = "."
	}
	f, _, e := openSyncSource(root, name)
	if e != nil {
		return Fail(503, "local_sync_open_directory", "无法打开投放目录。"+publicSyncError(e))
	}
	defer f.Close()
	for {
		if e = ctx.Err(); e != nil {
			return e
		}
		entries, readErr := f.ReadDir(128)
		for _, entry := range entries {
			relative := path.Join(dir, entry.Name())
			item := LocalSyncItem{Path: relative, Kind: "other", State: "ignored"}
			if strings.HasPrefix(entry.Name(), ".") || entry.Type()&os.ModeSymlink != 0 || !domain.ValidMediaPath(relative) {
				item.Error = "隐藏、链接或保留路径"
				recordSyncItem(v, item)
				continue
			}
			if entry.IsDir() {
				if depth >= 32 {
					item.Error = "目录过深"
					recordSyncItem(v, item)
					continue
				}
				if e = s.walkSyncDirectory(ctx, root, relative, depth+1, cycle, libs, v); e != nil {
					return e
				}
				continue
			}
			_, _, media := domain.MediaType(relative)
			switch {
			case dir == "" && strings.EqualFold(path.Ext(relative), ".txt"):
				item.Kind = "txt"
			case dir != "" && media:
				item.Kind = "media"
			default:
				item.Error = "不属于导入通道的文件"
				recordSyncItem(v, item)
				continue
			}
			file, info, openErr := openSyncSource(root, relative)
			if openErr != nil {
				item.State = "failed"
				item.Error = publicSyncError(openErr)
				recordSyncItem(v, item)
				// Being temporarily unreadable is different from disappearing. Preserve
				// its resumable upload when this cycle later reconciles missing paths.
				if _, e = s.DB.DB.ExecContext(ctx, "UPDATE local_sync_sources SET seen_cycle=? WHERE root_id=? AND path=?", cycle, s.localSync.rootID, relative); e != nil {
					return e
				}
				continue
			}
			if !info.Mode().IsRegular() {
				file.Close()
				item.State = "ignored"
				item.Error = "只处理普通文件"
				recordSyncItem(v, item)
				continue
			}
			item.SizeBytes = info.Size()
			e = s.observeSyncSource(ctx, root, file, info, item, cycle, libs, v)
			file.Close()
			if e != nil {
				return e
			}
		}
		if errors.Is(readErr, io.EOF) {
			return nil
		}
		if readErr != nil {
			return Fail(503, "local_sync_read_directory", "无法枚举投放目录。"+publicSyncError(readErr))
		}
	}
}

type syncObservation struct {
	stamp, state, uploadID string
	attempts               int
}

func (s *Service) observeSyncSource(ctx context.Context, root *os.Root, f *os.File, info os.FileInfo, item LocalSyncItem, cycle string, libs map[string][]model.Library, v *LocalSyncStatus) error {
	stamp, e := syncFileStamp(f, info)
	if e != nil {
		return Fail(503, "local_sync_source_version", "无法核验来源版本。"+publicSyncError(e))
	}
	r := s.localSync
	var old syncObservation
	e = s.DB.DB.QueryRowContext(ctx, "SELECT stamp,state,upload_id,attempts FROM local_sync_sources WHERE root_id=? AND path=?", r.rootID, item.Path).Scan(&old.stamp, &old.state, &old.uploadID, &old.attempts)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return e
	}
	if old.stamp == stamp && old.state == "synced" {
		return nil
	}
	if old.stamp != stamp {
		if e = s.cancelSyncUpload(ctx, old.uploadID); e != nil {
			return e
		}
		old = syncObservation{stamp: stamp}
		item.State = "waiting-stable"
	} else if time.Since(info.ModTime()) < r.config.StableAge || item.Kind == "media" && v.Paused {
		item.State = "waiting-stable"
	} else {
		now := time.Now().UTC()
		item.LastAttemptAt = &now
		old.attempts++
		var lib model.Library
		if item.Kind == "media" {
			lib, e = findSyncLibrary(libs, item.Path)
			item.LibraryName = lib.Name
			if e == nil {
				e = s.uploadsAllowed(ctx)
			}
		} else {
			e = nil
		}
		if e == nil {
			e = s.importSyncSource(ctx, root, f, info, stamp, lib, cycle, &item, &old)
		}
		if e != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			item.State = "failed"
			item.Error = publicSyncError(e)
		} else {
			item.State = "synced"
			v.SyncedTotal++
		}
	}
	item.Attempts = old.attempts
	recordSyncItem(v, item)
	return s.saveSyncObservation(ctx, item, old, cycle)
}

func (s *Service) saveSyncObservation(ctx context.Context, item LocalSyncItem, obs syncObservation, cycle string) error {
	complete := ""
	if item.State == "synced" {
		complete = dateKey(time.Now().UTC())
	}
	raw, e := store.Encode(item)
	if e != nil {
		return e
	}
	_, e = s.DB.DB.ExecContext(ctx, `INSERT INTO local_sync_sources(root_id,path,stamp,state,attempts,upload_id,data,seen_cycle,completed_at) VALUES(?,?,?,?,?,?,?,?,?)
  ON CONFLICT(root_id,path) DO UPDATE SET stamp=excluded.stamp,state=excluded.state,attempts=excluded.attempts,
  upload_id=excluded.upload_id,data=excluded.data,seen_cycle=excluded.seen_cycle,completed_at=excluded.completed_at`, s.localSync.rootID, item.Path, obs.stamp, item.State, obs.attempts, obs.uploadID, raw, cycle, complete)
	return e
}
