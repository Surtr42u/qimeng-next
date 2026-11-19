package phonerelink

import (
	"context"
	"database/sql"
	"path"
	"strconv"
	"strings"
	"time"

	"qimeng.local/next/server/internal/backup"
	"qimeng.local/next/server/internal/domain"
)

type libraryRoot struct {
	ID   string `json:"libraryId"`
	Root string `json:"rootRelativePath"`
}

type binding struct {
	Asset      string `json:"assetId"`
	Library    string `json:"libraryId"`
	SourcePath string `json:"sourceRelativePath"`
	PhonePath  string `json:"phoneRelativePath"`
}

type plan struct {
	Format       string        `json:"format"`
	Libraries    []libraryRoot `json:"libraries"`
	Bindings     []binding     `json:"bindings"`
	RestoreTrash []string      `json:"restoreTrashAssetIds,omitempty"`
}

type physical struct {
	Path   string `json:"phoneRelativePath"`
	Size   *int64 `json:"size"`
	Hash   string `json:"sha256"`
	Device string `json:"device"`
	Inode  string `json:"inode"`
	Mtime  *int64 `json:"mtimeUnixNs"`
	Ctime  *int64 `json:"ctimeUnixNs"`
}

type inventory struct {
	Format  string     `json:"format"`
	Storage string     `json:"storageId"`
	Files   []physical `json:"files"`
	Absent  []string   `json:"absentTrashPaths,omitempty"`
}

type relocation struct {
	id, old, next, mtime string
}

type delivery struct {
	roots   map[string]string
	live    []relocation
	trashed int
	staged  []TrashFile
}

func decimal(s string, positive bool) bool {
	n, err := strconv.ParseUint(s, 10, 64)
	return err == nil && strconv.FormatUint(n, 10) == s && (!positive || n > 0)
}

func evidenceIndex(items []physical, absent []string) (map[string]physical, error) {
	files := make(map[string]physical, len(items))
	usedPaths := make(map[string]bool, len(items))
	inodes := make(map[string]bool, len(items))
	for _, f := range items {
		if f.Path == "" || !domain.ValidRelative(f.Path) || f.Size == nil || *f.Size < 0 || !isDigest(f.Hash) ||
			!decimal(f.Device, false) || !decimal(f.Inode, true) || f.Mtime == nil || f.Ctime == nil {
			return nil, refusal("invalid_phone_file_evidence")
		}
		key := f.Device + "/" + f.Inode
		folded := strings.ToLower(f.Path)
		if usedPaths[folded] || inodes[key] {
			return nil, refusal("duplicate_phone_path_or_file_identity")
		}
		files[f.Path], usedPaths[folded], inodes[key] = f, true, true
	}
	for _, name := range absent {
		key := strings.ToLower(name)
		if name == "" || !domain.ValidRelative(name) || usedPaths[key] {
			return nil, refusal("invalid_or_occupied_trash_destination")
		}
		usedPaths[key] = true
	}
	// A file cannot also be a directory containing another witnessed file.
	for key := range usedPaths {
		for dir := path.Dir(key); dir != "."; dir = path.Dir(dir) {
			if usedPaths[dir] {
				return nil, refusal("conflicting_phone_path_components")
			}
		}
	}
	return files, nil
}

func mappedRoots(ctx context.Context, db *sql.DB, mappings []libraryRoot) (map[string]string, error) {
	roots := make(map[string]string, len(mappings))
	paths := make(map[string]bool, len(mappings))
	for _, lib := range mappings {
		key := strings.ToLower(lib.Root)
		if !domain.ValidName(lib.ID) || lib.Root == "" || !domain.ValidMediaPath(lib.Root) || roots[lib.ID] != "" || paths[key] {
			return nil, refusal("invalid_or_duplicate_phone_library")
		}
		roots[lib.ID], paths[key] = lib.Root, true
	}
	for key := range paths {
		for dir := path.Dir(key); dir != "."; dir = path.Dir(dir) {
			if paths[dir] {
				return nil, refusal("overlapping_phone_library_roots")
			}
		}
	}
	rows, err := db.QueryContext(ctx, `SELECT id,json_extract(data,'$.id'),json_extract(data,'$.storageId'),json_extract(data,'$.rootRelativePath') FROM libraries`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var id, embeddedID, storage, relative string
		if err := rows.Scan(&id, &embeddedID, &storage, &relative); err != nil {
			return nil, err
		}
		if roots[id] == "" || id != embeddedID || storage != "data" || relative != "" {
			return nil, refusal("unmapped_or_nonrestored_library")
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if count != len(roots) {
		return nil, refusal("unidentified_phone_library")
	}
	return roots, nil
}

func bindFiles(ctx context.Context, db *sql.DB, p plan, census inventory, expected int, trashBudget int64) (delivery, error) {
	var d delivery
	if p.Format != "qimeng-phone-relink-v1" || census.Format != "qimeng-phone-files-v1" || census.Storage != "phone" ||
		p.Libraries == nil || p.Bindings == nil || census.Files == nil || len(p.Libraries) > backup.MaxFiles ||
		len(p.Bindings) != expected || len(census.Files)+len(census.Absent) != expected || len(census.Absent) != len(p.RestoreTrash) {
		return d, refusal("incomplete_plan_or_phone_inventory")
	}
	var err error
	d.roots, err = mappedRoots(ctx, db, p.Libraries)
	if err != nil {
		return d, err
	}
	files, err := evidenceIndex(census.Files, census.Absent)
	if err != nil {
		return d, err
	}
	staged := map[string]bool{}
	missing := map[string]bool{}
	for _, id := range p.RestoreTrash {
		if !domain.ValidName(id) || staged[id] || trashBudget <= 0 {
			return d, refusal("explicit_trash_ids_and_byte_budget_required")
		}
		staged[id] = true
	}
	for _, name := range census.Absent {
		missing[name] = true
	}
	bindings := make(map[string]binding, expected)
	for _, b := range p.Bindings {
		if !domain.ValidName(b.Asset) || bindings[b.Asset].Asset != "" {
			return d, refusal("invalid_or_duplicate_asset_binding")
		}
		bindings[b.Asset] = b
	}
	rows, err := db.QueryContext(ctx, `SELECT l.asset_id,l.library_id,l.relative_path,l.file_name,l.trashed,l.storage_layout,
		v.hash,v.size FROM asset_locations l JOIN asset_content_versions v ON v.asset_id=l.asset_id AND v.version=l.current_version ORDER BY l.asset_id`)
	if err != nil {
		return d, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, lib, relative, fileName, hash string
		var size int64
		var trash bool
		var layout int
		if err := rows.Scan(&id, &lib, &relative, &fileName, &trash, &layout, &hash, &size); err != nil {
			return d, err
		}
		b, ok := bindings[id]
		if !ok || b.Library != lib || b.SourcePath != relative || !domain.ValidMediaPath(relative) || path.Base(relative) != fileName {
			return d, refusal("source_asset_binding_mismatch")
		}
		root := d.roots[lib]
		current, inside := strings.CutPrefix(b.PhonePath, root+"/")
		if !inside || current == "" {
			return d, refusal("phone_path_outside_selected_library")
		}
		delete(bindings, id)
		if trash {
			if layout != 2 || current != ".qimeng-internal/trash/"+id+"/"+fileName {
				return d, refusal("missing_native_phone_trash_location")
			}
			// Keep the original restoration path, deletion time and stored
			// version. Native trash has its own witnessed physical object.
			d.trashed++
		}
		if staged[id] {
			if !trash || !missing[b.PhonePath] || size > trashBudget {
				return d, refusal("unproven_or_over_budget_trash_copy")
			}
			trashBudget -= size
			d.staged = append(d.staged, TrashFile{Asset: id, Library: lib, Path: b.PhonePath, Hash: hash, Size: size})
			delete(staged, id)
			delete(missing, b.PhonePath)
			continue
		}
		f, ok := files[b.PhonePath]
		if !ok || *f.Size != size || f.Hash != hash {
			return d, refusal("missing_or_changed_phone_original")
		}
		delete(files, b.PhonePath)
		if trash {
			continue
		}
		if !domain.ValidMediaPath(current) || !strings.EqualFold(path.Ext(current), path.Ext(fileName)) {
			return d, refusal("invalid_phone_media_location")
		}
		d.live = append(d.live, relocation{id, relative, current, time.Unix(0, *f.Mtime).UTC().Format(time.RFC3339Nano)})
	}
	if err := rows.Err(); err != nil {
		return d, err
	}
	if len(bindings) != 0 || len(files) != 0 || len(staged) != 0 || len(missing) != 0 {
		return d, refusal("unused_asset_or_phone_evidence")
	}
	return d, nil
}

func (d delivery) apply(ctx context.Context, db *sql.DB, instanceID string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "UPDATE settings SET value=? WHERE key='instance_id'", instanceID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO main.settings(key,value) SELECT p.key,p.value FROM phone.settings p
		WHERE NOT EXISTS (SELECT 1 FROM main.settings s WHERE s.key=p.key)`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO main.credentials SELECT * FROM phone.credentials"); err != nil {
		return err
	}
	for id, root := range d.roots {
		if _, err := tx.ExecContext(ctx, `UPDATE libraries SET data=json_set(data,'$.storageId','phone','$.rootRelativePath',?) WHERE id=?`, root, id); err != nil {
			return err
		}
	}
	// Free the live-name uniqueness slots inside the same transaction, so
	// two independently witnessed assets may exchange names without a clash.
	for _, a := range d.live {
		if a.old != a.next {
			if _, err := tx.ExecContext(ctx, "UPDATE asset_locations SET relative_path=? WHERE asset_id=?", ".qimeng-internal/relink/"+a.id, a.id); err != nil {
				return err
			}
		}
	}
	for _, a := range d.live {
		if _, err := tx.ExecContext(ctx, `UPDATE asset_locations SET relative_path=?,file_name=?,content_mtime=?
			WHERE asset_id=? AND (relative_path IS NOT ? OR content_mtime IS NOT ?)`, a.next, path.Base(a.next), a.mtime, a.id, a.next, a.mtime); err != nil {
			return err
		}
	}
	return tx.Commit()
}
