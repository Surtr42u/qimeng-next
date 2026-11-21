package assetmerge

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"qimeng.local/next/server/internal/assetmerge/facts"
	"qimeng.local/next/server/internal/backup"
	"qimeng.local/next/server/internal/domain"
	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/storage"
	"qimeng.local/next/server/internal/store"
)

type payload struct {
	backup.File
	Physical string `json:"physical"`
}

type catalog struct {
	files map[string]payload
	dirs  map[string]bool
}

func payloadKey(f backup.File) string { return f.Kind + "/" + f.ObjectID }

func (c *catalog) add(router *storage.Root, stage string, f backup.File) error {
	if !domain.ValidName(f.ObjectID) {
		return errors.New("invalid physical object ID")
	}
	absolute, err := router.AbsolutePath(f.StoragePath)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(stage, absolute)
	if err != nil || !filepath.IsLocal(relative) || relative == "." {
		return errors.New("payload is outside the private data root")
	}
	p := payload{f, filepath.ToSlash(relative)}
	key := payloadKey(f)
	if _, duplicate := c.files[key]; duplicate {
		return errors.New("duplicate current physical identity")
	}
	c.files[key] = p
	return nil
}

func readCatalog(ctx context.Context, db *sql.DB, stage string, recordsOnly bool) (catalog, error) {
	c := catalog{map[string]payload{}, map[string]bool{}}
	// Storage roots use their resolved spelling. A Windows temp directory can
	// arrive through a legitimate 8.3 alias even after all links were rejected.
	stage, err := filepath.EvalSymlinks(stage)
	if err != nil {
		return c, err
	}
	router, err := storage.Open(stage, nil)
	if err != nil {
		return c, err
	}
	defer router.Close()
	libs, err := store.List[model.Library](ctx, db, "SELECT data FROM libraries")
	if err != nil {
		return c, err
	}
	ids := map[string]bool{}
	for _, l := range libs {
		if !domain.ValidName(l.ID) || l.StorageID != "data" || l.RootRelativePath != "" {
			return c, errors.New("restore contains a nonlocal library")
		}
		if err = router.RegisterLibrary(l.ID, "data", ""); err != nil {
			return c, err
		}
		c.dirs["libraries/"+l.ID] = true
		c.dirs["libraries"] = true
		ids[l.ID] = true
	}
	rows, err := db.QueryContext(ctx, `SELECT l.asset_id,l.library_id,l.relative_path,l.file_name,l.current_version,l.trashed,
		l.storage_layout,l.content_mtime,v.hash,v.size FROM asset_locations l JOIN asset_content_versions v
		ON v.asset_id=l.asset_id AND v.version=l.current_version`)
	if err != nil {
		return c, err
	}
	defer rows.Close()
	for rows.Next() {
		var library, relative, name string
		var trashed bool
		var layout int
		var mt sql.NullString
		f := backup.File{Kind: "asset"}
		if err = rows.Scan(&f.ObjectID, &library, &relative, &name, &f.ContentVersion, &trashed, &layout, &mt, &f.SHA256, &f.Size); err != nil {
			return c, err
		}
		if !ids[library] || !domain.ValidName(name) || !domain.ValidMediaPath(relative) || path.Base(relative) != name {
			return c, errors.New("invalid restored location")
		}
		if mt.Valid {
			t, err := time.Parse(time.RFC3339Nano, mt.String)
			if err != nil {
				return c, err
			}
			f.ModifiedAt = &t
		}
		switch {
		case !trashed:
			f.StoragePath = "libraries/" + library + "/" + relative
		case layout >= 2:
			f.StoragePath = "library-trash/" + library + "/" + f.ObjectID + "/" + name
		default:
			f.StoragePath = "trash/" + f.ObjectID + "/" + name
		}
		if err = c.add(router, stage, f); err != nil {
			return c, err
		}
	}
	if err = rows.Err(); err != nil {
		return c, err
	}
	if err = rows.Close(); err != nil {
		return c, err
	}
	// A records-only restore marks lost prefixes cancelled and never creates
	// their .part files. Older full archives may still carry a cancelled upload
	// prefix, so keep the previous behavior for complete archives.
	query := "SELECT data FROM uploads WHERE state!='ingested'"
	if recordsOnly {
		query = "SELECT data FROM uploads WHERE state!='ingested' AND state!='cancelled'"
	}
	uploads, err := store.List[model.Upload](ctx, db, query)
	if err != nil {
		return c, err
	}
	for _, u := range uploads {
		if ids[u.LibraryID] {
			router.RegisterUpload(u.ID, u.LibraryID)
		}
		if err = c.add(router, stage, backup.File{Kind: "upload", ObjectID: u.ID, StoragePath: "uploads/" + u.ID + ".part", SHA256: u.Hash, Size: u.Offset}); err != nil {
			return c, err
		}
	}
	return c, nil
}

type original struct {
	payload
	Stamp stamp `json:"observed"`
}

func sameTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
}

func (a *area) verifyBaseline(ctx context.Context, stage string, m backup.Manifest, c catalog) ([]original, error) {
	// A records-only archive has no payload list. The restored catalog is then
	// the relink index: verify every live original against its recorded hash,
	// size and current version instead of silently verifying nothing.
	entries := m.Files
	if m.Scope == backup.ScopeMetadata {
		entries = make([]backup.File, 0, len(c.files))
		for _, p := range c.files {
			entries = append(entries, p.File)
		}
		sort.Slice(entries, func(i, j int) bool { return payloadKey(entries[i]) < payloadKey(entries[j]) })
	}
	if len(entries) != len(c.files) {
		return nil, errors.New("restore omits archive entries")
	}
	files := make([]original, 0, len(entries))
	expected := map[string]bool{"qimeng.sqlite": true}
	for d := range c.dirs {
		expected[d] = true
	}
	for i, entry := range entries {
		actual, ok := c.files[payloadKey(entry)]
		hashMatches := actual.SHA256 == entry.SHA256 || (entry.Kind == "upload" && actual.SHA256 == "")
		if !ok || !hashMatches || actual.ContentVersion != entry.ContentVersion || actual.StoragePath != entry.StoragePath || actual.Size != entry.Size ||
			(entry.Kind == "asset" && !sameTime(entry.ModifiedAt, actual.ModifiedAt)) {
			return nil, fmt.Errorf("archive entry %d differs from the restored catalog", i+1)
		}
		actual.File = entry
		s, err := a.readFile(ctx, path.Join(stage, actual.Physical), nil)
		if err != nil {
			return nil, err
		}
		if s.Hash != entry.SHA256 || s.Size != entry.Size || (entry.ModifiedAt != nil && !s.Time.Equal(*entry.ModifiedAt)) {
			return nil, fmt.Errorf("restored bytes or modification time differ at entry %d", i+1)
		}
		if expected[actual.Physical] {
			return nil, errors.New("duplicate physical path")
		}
		expected[actual.Physical] = true
		for dir := path.Dir(actual.Physical); dir != "."; dir = path.Dir(dir) {
			expected[dir] = true
		}
		files = append(files, original{actual, s})
	}
	if err := a.verifyTree(stage, expected); err != nil {
		return nil, err
	}
	return files, nil
}

func (a *area) verifyTree(stage string, expected map[string]bool) error {
	return fs.WalkDir(a.root.FS(), stage, func(p string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if linked(info) {
			return errors.New("link in private restore")
		}
		if p == stage || expected[strings.TrimPrefix(p, stage+"/")] {
			return nil
		}
		switch strings.TrimPrefix(p, stage+"/") {
		case "qimeng.sqlite-wal", "qimeng.sqlite-journal":
			if info.Mode().IsRegular() && info.Size() == 0 {
				return nil
			}
		case "qimeng.sqlite-shm":
			if info.Mode().IsRegular() {
				return nil
			}
		}
		if entry.IsDir() {
			// 记录型备份的恢复会先建出库/上传的空目录骨架，原件由操作者已有的媒体根
			// 提供（真实恢复就是“恢复记录 + 已有文件”），所以允许空目录。目录里一旦
			// 有文件，WalkDir 会逐项经过这里并拒绝，未预期的字节仍然进不来。
			children, readErr := fs.ReadDir(a.root.FS(), p)
			if readErr == nil && len(children) == 0 {
				return nil
			}
		}
		return errors.New("working directory contains a file or directory outside the native restore")
	})
}

type relocation struct {
	AssetID string `json:"assetId"`
	Before  string `json:"from"`
	After   string `json:"to"`
	Version int64  `json:"contentVersion"`
	Copy    string `json:"preimage"`
	Stamp   stamp  `json:"original"`
}

func layout(plan facts.Plan, files []original, projected catalog, replay bool) ([]relocation, error) {
	byID := map[string]original{}
	for _, f := range files {
		if f.Kind == "asset" {
			byID[f.ObjectID] = f
		}
	}
	var moves []relocation
	for _, group := range plan.Groups {
		for _, member := range group.Members {
			from, ok := byID[member.AssetID]
			to, found := projected.files["asset/"+member.AssetID]
			if !ok || !found || from.SHA256 != to.SHA256 || from.Size != to.Size || from.ContentVersion != to.ContentVersion || !sameTime(from.ModifiedAt, to.ModifiedAt) {
				return nil, errors.New("projection changed file ownership or content facts")
			}
			if !replay {
				target := "libraries/" + group.LibraryID + "/" + group.RelativePath
				if member.AssetID != group.RepresentativeID {
					target = "library-trash/" + group.LibraryID + "/" + member.AssetID + "/" + path.Base(from.StoragePath)
				}
				if to.StoragePath != target {
					return nil, errors.New("projected layout differs from authorized images")
				}
			}
			moves = append(moves, relocation{member.AssetID, from.Physical, to.Physical, from.ContentVersion, fmt.Sprintf("originals/%08d", len(moves)+1), from.Stamp})
		}
	}
	return moves, nil
}

func (a *area) checkTargetPaths(stage string, moves []relocation) error {
	names := map[string]os.FileInfo{}
	for _, m := range moves {
		names[strings.ToLower(m.Before)] = m.Stamp.info
	}
	for _, m := range moves {
		target := stage + "/" + m.After
		found, err := a.inspect(target)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		before := names[strings.ToLower(m.After)]
		if before == nil || !os.SameFile(before, found) {
			return errors.New("physical destination is occupied")
		}
	}
	return nil
}
