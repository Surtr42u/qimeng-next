package legacycold

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"qimeng.local/next/server/internal/backup"
	"qimeng.local/next/server/internal/domain"
	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/store"
)

var plainCodec = regexp.MustCompile(`^[a-zA-Z0-9_.+-]{1,64}$`)

type original struct {
	file backup.File
	ref  fileRef
}

type mediaSet struct {
	files    []original
	ids      map[string]int
	names    map[string]bool
	onDisk   map[string]bool
	trash    map[string]fileRef
	report   *Report
	sourceID string
}

func newMediaSet(report *Report) *mediaSet {
	return &mediaSet{ids: map[string]int{}, names: map[string]bool{}, onDisk: map[string]bool{},
		trash: map[string]fileRef{}, report: report, sourceID: report.SourceID}
}

func readLibraries(ctx context.Context, source *sql.DB, target *sql.Tx, roots map[string]*readRoot, report *Report) error {
	rows, err := source.QueryContext(ctx, "SELECT rowid,id,name,kind,enabled,created_at FROM libraries ORDER BY id")
	if err != nil {
		return err
	}
	defer rows.Close()
	seen := map[string]bool{}
	for rows.Next() {
		var row int64
		var lib model.Library
		var kind, created string
		var enabled int
		if err = rows.Scan(&row, &lib.ID, &lib.Name, &kind, &enabled, &created); err != nil {
			return err
		}
		switch kind {
		case "normal":
			lib.Kind = "regular"
		case "cos":
			lib.Kind = "cos"
		default:
			return locate(failed("unsupported_library_kind"), "libraries", row, "")
		}
		lib.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
		if err != nil || !domain.ValidName(lib.ID) || (enabled != 0 && enabled != 1) || lib.Name == "" {
			return locate(failed("invalid_library_record"), "libraries", row, "")
		}
		fold := strings.ToLower(lib.ID)
		if roots[lib.ID] == nil || seen[fold] {
			return locate(failed("library_root_missing_or_identity_conflict"), "libraries", row, "")
		}
		seen[fold] = true
		lib.StorageID, lib.RootRelativePath = "data", "libraries/"+lib.ID
		lib.Revision, lib.ScanState, lib.Enabled = 1, "idle", enabled == 1
		raw, err := store.Encode(lib)
		if err != nil {
			return err
		}
		if _, err = target.ExecContext(ctx, "INSERT INTO libraries(id,name,kind,data) VALUES(?,?,?,?)", lib.ID, lib.Name, lib.Kind, raw); err != nil {
			return locate(failed("library_insert_failed"), "libraries", row, "")
		}
		report.Libraries++
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if len(roots) != len(seen) {
		return failed("extra_library_mapping")
	}
	return nil
}

func (s *mediaSet) add(ctx context.Context, tx *sql.Tx, a model.Asset, ref fileRef, hash string) error {
	if !domain.ValidName(a.ID) || !domain.ValidName(a.LibraryID) || !domain.ValidMediaPath(a.RelativePath) ||
		!domain.ValidName(a.FileName) || a.FileName != path.Base(a.RelativePath) {
		return failed("invalid_asset_identity_or_path")
	}
	probed, mime, allowed := domain.MediaType(a.FileName)
	if !allowed || a.MediaType != probed {
		return failed("unsupported_original_media_type")
	}
	storagePath := "libraries/" + a.LibraryID + "/" + a.RelativePath
	if a.Trashed {
		storagePath = "library-trash/" + a.LibraryID + "/" + a.ID + "/" + a.FileName
	}
	fold := strings.ToLower(storagePath)
	if _, duplicate := s.ids[a.ID]; duplicate || s.names[fold] || !domain.ValidRelative(storagePath) {
		return failed("archive_path_or_identity_conflict")
	}
	if len(s.files) >= backup.MaxFiles || ref.info.Size() > math.MaxInt64-s.report.OriginalBytes {
		return failed("native_archive_limit")
	}
	modified := ref.info.ModTime().UTC()
	a.Hash, a.Size, a.MimeType, a.ContentMtime = hash, ref.info.Size(), mime, &modified
	a.ContentVersion, a.StorageLayoutVersion, a.Revision = 1, 2, 1
	a.OriginalFileName, a.MetadataSource = a.FileName, "migration"
	a.AuthorID, a.AuthorIDs, a.AuthorStatus, a.AuthorsRevision = "unknown", []string{"unknown"}, "unknown", 1
	a.TagsRevision, a.Tags = 1, []string{}
	a.SourceIDs, a.CharacterIDs, a.Characters, a.AcquisitionSources = []string{}, []string{}, []string{}, []string{}
	a.ProbeState = "unavailable"
	if a.Width > 0 && a.Height > 0 {
		a.ProbeState = "ready"
	}
	a.ContentURL = "/api/v1/assets/" + url.PathEscape(a.ID) + "/content"
	a.ThumbnailURL = "/api/v1/assets/" + url.PathEscape(a.ID) + "/thumbnail"
	if err := store.InsertAsset(ctx, tx, a); err != nil {
		return failed("asset_insert_failed")
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO migration_media_aliases(source_id,source_asset_id,asset_id) VALUES(?,?,?)`,
		s.sourceID, a.ID, a.ID); err != nil {
		return failed("asset_alias_conflict")
	}
	entry := backup.File{Kind: "asset", ObjectID: a.ID, ContentVersion: 1, StoragePath: storagePath, SHA256: hash, Size: a.Size, ModifiedAt: &modified}
	s.ids[a.ID], s.names[fold] = len(s.files), true
	s.files = append(s.files, original{entry, ref})
	s.report.OriginalBytes += a.Size
	if a.Trashed {
		s.report.TrashedAssets++
	} else {
		s.report.LiveAssets++
		s.onDisk[a.LibraryID+"/"+a.RelativePath] = true
	}
	return nil
}

func (s *mediaSet) live(ctx context.Context, source *sql.DB, tx *sql.Tx, roots map[string]*readRoot) error {
	rows, err := source.QueryContext(ctx, `SELECT rowid,asset_id,library_id,rel_path,file_name,media_type,size_bytes,mtime,
		coalesce(width,0),coalesce(height,0),coalesce(duration_ms,0),created_at,updated_at,video_codec,audio_codec FROM assets ORDER BY asset_id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var a model.Asset
		var row int64
		var recorded, created, updated string
		var video, audio sql.NullString
		if err = rows.Scan(&row, &a.ID, &a.LibraryID, &a.RelativePath, &a.FileName, &a.MediaType, &a.Size, &recorded,
			&a.Width, &a.Height, &a.DurationMs, &created, &updated, &video, &audio); err != nil {
			return err
		}
		root := roots[a.LibraryID]
		if root == nil || !domain.ValidMediaPath(a.RelativePath) || a.RelativePath == "" || a.Size < 0 || a.Width < 0 || a.Height < 0 || a.DurationMs < 0 {
			return locate(failed("invalid_active_asset"), "assets", row, "")
		}
		sourceTime, e1 := time.Parse(time.RFC3339Nano, recorded)
		a.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
		var e2 error
		a.UpdatedAt, e2 = time.Parse(time.RFC3339Nano, updated)
		if e1 != nil || e2 != nil || err != nil {
			return locate(failed("invalid_asset_timestamp"), "assets", row, "")
		}
		a.SourceMtime = &sourceTime
		ref, hash, err := sealFile(ctx, root, a.RelativePath)
		if err != nil {
			return locate(err, "assets", row, "")
		}
		if ref.info.Size() != a.Size || !ref.info.ModTime().Truncate(time.Millisecond).Equal(sourceTime.Truncate(time.Millisecond)) {
			return locate(failed("original_differs_from_catalog_size_or_mtime"), "assets", row, "")
		}
		if err = s.add(ctx, tx, a, ref, hash); err != nil {
			return locate(err, "assets", row, "")
		}
		// Codec labels belong to their observed source file. Only normal probe
		// identifiers enter the archive; free-form host strings stay at source.
		codec := func(v sql.NullString) any {
			if v.Valid && plainCodec.MatchString(v.String) {
				return v.String
			}
			return nil
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO migration_relation_origins VALUES(?,?,?,json_object(
			'rowId',?,'recordedMtime',?,'videoCodec',?,'audioCodec',?))`, s.sourceID, "cold26/asset", a.ID, row, recorded, codec(video), codec(audio))
		if err != nil {
			return err
		}
	}
	return rows.Err()
}

// Read directory entries in bounded batches. Neither a huge flat library nor
// an unexpected tree can force WalkDir's whole-directory sort into memory.
func walk(ctx context.Context, root *readRoot, dir string, skip func(os.DirEntry) bool, visit func(string) error) error {
	if strings.Count(dir, "/") > 255 {
		return failed("source_directory_too_deep")
	}
	f, err := root.root.Open(filepath.FromSlash(dir))
	if err != nil {
		return failed("source_directory_unreadable")
	}
	defer f.Close()
	for {
		if err = ctx.Err(); err != nil {
			return err
		}
		items, err := f.ReadDir(128)
		if err != nil && !errors.Is(err, io.EOF) {
			return failed("source_directory_unreadable")
		}
		for _, item := range items {
			if skip != nil && skip(item) {
				continue
			}
			name := path.Join(dir, item.Name())
			if item.Type()&(os.ModeSymlink|os.ModeIrregular) != 0 {
				return locate(failed("source_link_or_special_file"), "source_files", 0, name)
			}
			if item.IsDir() {
				if e := walk(ctx, root, name, skip, visit); e != nil {
					return e
				}
			} else if e := visit(name); e != nil {
				return e
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
	}
}

func (s *mediaSet) checkLibraryFiles(ctx context.Context, roots map[string]*readRoot, count bool) error {
	for id, root := range roots {
		skip := func(item os.DirEntry) bool {
			name := strings.ToLower(item.Name())
			exclude := strings.HasPrefix(name, ".") || item.IsDir() && (name == "#recycle" || name == "$recycle.bin" || name == "_gsdata_" || name == "system volume information")
			if count && exclude {
				s.report.ExcludedEntries++
			}
			return exclude
		}
		err := walk(ctx, root, ".", skip, func(rel string) error {
			if _, _, media := domain.MediaType(rel); !media {
				if count {
					s.report.OtherFiles++
				}
				return nil
			}
			if !s.onDisk[id+"/"+rel] {
				return locate(failed("unindexed_source_media"), "library_files", 0, id+"/"+rel)
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

type trashMeta struct {
	AssetID      string    `json:"asset_id"`
	LibraryID    string    `json:"library_id"`
	OriginalPath string    `json:"original_path"`
	MediaType    string    `json:"media_type"`
	DeletedAt    time.Time `json:"deleted_at"`
	RestorePath  string    `json:"restore_path"`
	RestoreSize  int64     `json:"restore_size"`
	RestoreMtime time.Time `json:"restore_mtime"`
}

func (s *mediaSet) trashFiles(ctx context.Context, tx *sql.Tx, root *readRoot, libraries map[string]*readRoot, checkOnly bool) error {
	if root == nil {
		return nil
	}
	found := 0
	err := walk(ctx, root, ".", nil, func(rel string) error {
		fault := func(code string) error { return locate(failed(code), "trash_files", 0, rel) }
		if checkOnly {
			ref, ok := s.trash[rel]
			if !ok || ref.check() != nil {
				return fault("trash_inventory_changed")
			}
			found++
			return nil
		}
		if !strings.HasSuffix(rel, ".meta.json") {
			if _, err := root.root.Lstat(filepath.FromSlash(rel + ".meta.json")); err != nil {
				return fault("trash_metadata_missing")
			}
			return nil
		}
		parts := strings.SplitN(strings.TrimSuffix(rel, ".meta.json"), "/", 3)
		if len(parts) != 3 || len(parts[0]) != 19 {
			return fault("invalid_trash_layout")
		}
		f, err := root.openFile(rel)
		if err != nil {
			return fault("trash_metadata_unavailable")
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil || info.Size() > 64<<10 {
			return fault("invalid_trash_metadata_size")
		}
		dec := json.NewDecoder(io.LimitReader(f, 64<<10))
		dec.DisallowUnknownFields()
		var m trashMeta
		stamp, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil || dec.Decode(&m) != nil || dec.Decode(new(any)) != io.EOF || m.DeletedAt.IsZero() || m.DeletedAt.UnixNano() != stamp ||
			m.AssetID != parts[1] || m.OriginalPath != parts[2] || libraries[m.LibraryID] == nil || !domain.ValidMediaPath(m.OriginalPath) {
			return fault("invalid_trash_metadata_or_library")
		}
		if m.RestorePath != "" || m.RestoreSize != 0 || !m.RestoreMtime.IsZero() {
			return fault("unfinished_trash_restore_intent")
		}
		payload := strings.TrimSuffix(rel, ".meta.json")
		ref, hash, err := sealFile(ctx, root, payload)
		if err != nil {
			return fault("trash_payload_missing_or_changed")
		}
		// The old meta has a deletion time, not an original creation time.
		// Record the observed deletion as this migrated trash record's inception.
		a := model.Asset{ID: m.AssetID, LibraryID: m.LibraryID, RelativePath: m.OriginalPath, FileName: path.Base(m.OriginalPath),
			MediaType: m.MediaType, CreatedAt: m.DeletedAt.UTC(), UpdatedAt: m.DeletedAt.UTC(), Trashed: true}
		if err = s.add(ctx, tx, a, ref, hash); err != nil {
			return locate(err, "trash_files", 0, rel)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO migration_relation_origins VALUES(?,?,?,json_object('deletedAt',?,'sourceCreatedAt',NULL))`,
			s.sourceID, "cold26/trash", m.AssetID, m.DeletedAt.Format(time.RFC3339Nano)); err != nil {
			return fault("trash_origin_insert_failed")
		}
		s.trash[rel], s.trash[payload] = fileRef{root, rel, info}, ref
		return nil
	})
	if err == nil && checkOnly && found != len(s.trash) {
		return failed("trash_inventory_changed")
	}
	return err
}
