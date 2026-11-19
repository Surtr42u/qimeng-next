package legacycold

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/shirou/gopsutil/v4/disk"
	"qimeng.local/next/server/internal/backup"
	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/storage"
	"qimeng.local/next/server/internal/store"
)

type Options struct {
	SourceDB, Roots, TrashRoot, Output, SourceTimezone string
	ReserveBytes                                        int64
}

type Projection func(context.Context, *sql.DB, *sql.Tx, string) error

type Receipt struct {
	Format           string    `json:"format"`
	CreatedAt        time.Time `json:"createdAt"`
	SourceID         string    `json:"sourceId"`
	SourceInstanceID string    `json:"sourceInstanceId"`
	BackupID         string    `json:"backupId"`
	ArchiveSHA256    string    `json:"archiveSHA256"`
	SnapshotSHA256   string    `json:"snapshotSHA256"`
	AssetCount       int       `json:"assetCount"`
	OriginalBytes    int64     `json:"originalBytes"`
}

type inputs struct {
	owned   []*readRoot
	dbDir   *readRoot
	trash   *readRoot
	library map[string]*readRoot
}

func (in *inputs) open(name string) (*readRoot, error) {
	r, err := openDirectory(name)
	if err == nil {
		in.owned = append(in.owned, r)
	}
	return r, err
}

func (in *inputs) close() {
	for _, r := range in.owned {
		r.root.Close()
	}
}

func (in *inputs) prepare(ctx context.Context, opt Options) error {
	for _, name := range []string{opt.SourceDB, opt.Roots, opt.TrashRoot, opt.Output} {
		if _, err := absolute(name); err != nil {
			return err
		}
	}
	mapping, err := readMapping(ctx, opt.Roots)
	if err != nil {
		return err
	}
	in.dbDir, err = in.open(filepath.Dir(opt.SourceDB))
	if err != nil {
		return err
	}
	if _, err = in.open(filepath.Dir(opt.TrashRoot)); err != nil {
		return err
	}
	if _, err = os.Lstat(opt.TrashRoot); !errors.Is(err, fs.ErrNotExist) {
		in.trash, err = in.open(opt.TrashRoot)
		if err != nil {
			return err
		}
	}
	in.library = make(map[string]*readRoot, len(mapping))
	for id, name := range mapping {
		r, err := in.open(name)
		if err != nil {
			return locate(err, "library_roots", 0, id)
		}
		for _, previous := range in.library {
			if os.SameFile(r.info, previous.info) {
				return failed("duplicate_physical_library_root")
			}
		}
		in.library[id] = r
	}
	parent := filepath.Dir(opt.Output)
	if _, err := realDirectory(parent); err != nil {
		return err
	}
	return noOutputInside(parent, in.owned)
}

func available(ctx context.Context, dir string, reserve int64) (int64, error) {
	stats, err := disk.UsageWithContext(ctx, dir)
	if err != nil || stats.Free > 1<<63-1 {
		return 0, failed("output_space_unavailable")
	}
	room := int64(stats.Free) - reserve
	if room < 8<<20 {
		return 0, failed("output_reserve_exhausted")
	}
	return room, nil
}

type reservedWriter struct {
	ctx     context.Context
	dir     string
	reserve int64
	w       io.Writer
	left    int64
}

func (w *reservedWriter) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	if w.left < int64(len(p)) {
		room, err := available(w.ctx, w.dir, w.reserve)
		if err != nil || room < int64(len(p)) {
			return 0, failed("output_reserve_exhausted")
		}
		w.left = min(room, 8<<20)
	}
	n, err := w.w.Write(p)
	w.left -= int64(n)
	return n, err
}

func writeExclusive(root *os.Root, name string, data []byte) error {
	f, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	return errors.Join(err, f.Close())
}

func writeJSON(root *os.Root, name string, data any) error {
	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	return writeExclusive(root, name, append(raw, '\n'))
}

// Convert claims a new private directory. The archive's no-replace
// publication is the commit point, after source rechecks and all receipts.
func Convert(ctx context.Context, opt Options, catalog, activity Projection) (receipt Receipt, err error) {
	if catalog == nil || activity == nil || opt.SourceTimezone == "" || opt.ReserveBytes < 0 {
		return receipt, failed("invalid_conversion_options")
	}
	if opt.ReserveBytes == 0 {
		opt.ReserveBytes = 512 << 20
	}
	var in inputs
	defer in.close()
	if err = in.prepare(ctx, opt); err != nil {
		return receipt, err
	}
	if err = os.Mkdir(opt.Output, 0700); err != nil {
		return receipt, failed("new_output_directory_required")
	}
	out, err := openDirectory(opt.Output)
	if err != nil {
		return receipt, err
	}
	defer out.root.Close()
	report := Report{Format: "qimeng-legacy-cold-coverage/v1", State: "incomplete", Stage: "input",
		SourceTimezone: opt.SourceTimezone, NonMediaHandling: "Separate private retention required for non-media files and old scanner exclusions."}
	var published []string
	committed := false
	defer func() {
		if err != nil && !committed {
			// Only remove sidecars created by this invocation in our newly
			// claimed output. A crash before library.qnext is never success.
			for _, name := range published {
				if e := out.root.Remove(name); e != nil {
					err = failed("private_output_cleanup_failed")
				}
			}
			if ctx.Err() != nil {
				err = failed("cancelled")
			}
			var i *issue
			if !errors.As(err, &i) {
				i = &issue{Code: "conversion_stage_failed"}
			}
			report.State, report.Problem, err = "incomplete", i, i
			if e := writeJSON(out.root, "coverage.json", report); e != nil {
				err = failed("private_report_write_failed")
			}
		}
	}()
	if err = out.root.Mkdir(".incomplete", 0700); err != nil {
		return receipt, err
	}
	defer func() {
		if e := out.root.RemoveAll(".incomplete"); e != nil && err == nil {
			// The completed archive and receipt remain valid even if private
			// scratch cleanup fails. Report this condition separately.
			err = failed("archive_published_private_scratch_remains")
		}
	}()
	source, dbRef, dbHash, err := openSealed(ctx, in.dbDir, filepath.Base(opt.SourceDB))
	if err != nil {
		return receipt, err
	}
	defer source.Close()
	report.SourceID = "legacy-cold-v26:" + dbHash
	report.Stage = "source_inventory"
	if err = inventoryTables(ctx, source, &report); err != nil {
		return receipt, err
	}
	media := newMediaSet(&report)
	room, err := available(ctx, opt.Output, opt.ReserveBytes)
	if err != nil {
		return receipt, err
	}
	report.Stage = "new_database"
	snapshotName := filepath.Join(opt.Output, ".incomplete", "catalog.sqlite")
	manifest := backup.Manifest{Format: backup.Format, Version: backup.Version, BackupID: rand.Text(), InstanceID: rand.Text(), CreatedAt: time.Now().UTC()}
	if err = project(ctx, source, snapshotName, room, manifest, &in, media, catalog, activity); err != nil {
		return receipt, err
	}
	report.Stage = "archive"
	scratch, err := openDirectory(filepath.Dir(snapshotName))
	if err != nil {
		return receipt, err
	}
	defer scratch.root.Close()
	snapshot, hash, err := sealFile(ctx, scratch, filepath.Base(snapshotName))
	if err != nil {
		return receipt, err
	}
	manifest.Snapshot = backup.File{Kind: "snapshot", ObjectID: manifest.BackupID, StoragePath: "qimeng.sqlite", Size: snapshot.info.Size(), SHA256: hash}
	manifest.Files = make([]backup.File, len(media.files))
	for i, f := range media.files {
		manifest.Files[i] = f.file
	}
	sum, err := writeArchive(ctx, opt, scratch, snapshot, manifest, media)
	if err != nil {
		return receipt, err
	}
	report.Stage = "final_source_check"
	if err = media.checkLibraryFiles(ctx, in.library, false); err != nil {
		return receipt, err
	}
	for _, entry := range media.files {
		if err = ctx.Err(); err != nil {
			return receipt, err
		}
		if err = entry.ref.check(); err != nil {
			return receipt, locate(err, "originals", 0, entry.file.ObjectID)
		}
	}
	if err = media.trashFiles(ctx, nil, in.trash, in.library, true); err != nil {
		return receipt, err
	}
	if in.trash == nil {
		if _, e := os.Lstat(opt.TrashRoot); !errors.Is(e, fs.ErrNotExist) {
			return receipt, failed("trash_inventory_changed")
		}
	}
	for _, root := range in.owned {
		if err = root.check(); err != nil {
			return receipt, err
		}
	}
	if err = noJournal(in.dbDir, dbRef.rel); err != nil {
		return receipt, err
	}
	last, lastHash, err := sealFile(ctx, in.dbDir, dbRef.rel)
	if err != nil || dbHash != lastHash || !matchFile(dbRef.info, last.info) {
		return receipt, failed("sealed_database_changed")
	}
	report.Stage, report.State = "publication", "converted"
	receipt = Receipt{Format: "qimeng-legacy-cold-receipt/v1", CreatedAt: manifest.CreatedAt, SourceID: report.SourceID,
		SourceInstanceID: manifest.InstanceID, BackupID: manifest.BackupID, ArchiveSHA256: sum, SnapshotSHA256: hash,
		AssetCount: len(media.files), OriginalBytes: report.OriginalBytes}
	if err = writeJSON(scratch.root, "coverage.json", report); err != nil {
		return receipt, err
	}
	if err = writeJSON(scratch.root, "receipt.json", receipt); err != nil {
		return receipt, err
	}
	if err = writeExclusive(scratch.root, "library.qnext.sha256", []byte(sum+"\n")); err != nil {
		return receipt, err
	}
	for _, name := range []string{"receipt.json", "library.qnext.sha256", "coverage.json", "library.qnext"} {
		if err = ctx.Err(); err != nil {
			return receipt, err
		}
		if err = storage.PublishTemp(out.root, ".incomplete/"+name, name); err != nil {
			return receipt, failed("private_output_publication_failed")
		}
		published = append(published, name)
	}
	committed = true
	return receipt, nil
}

func project(ctx context.Context, source *sql.DB, filename string, room int64, m backup.Manifest, in *inputs, media *mediaSet, catalog, activity Projection) error {
	target, err := store.Open(filename)
	if err != nil {
		return err
	}
	defer target.DB.Close()
	// No concurrent writers or long-lived readers exist in this fresh target.
	// Bound the database plus rollback journal to the available scratch budget.
	var pageSize int64
	if err = target.DB.QueryRowContext(ctx, "PRAGMA page_size").Scan(&pageSize); err != nil {
		return err
	}
	if _, err = target.DB.ExecContext(ctx, "PRAGMA journal_mode=DELETE"); err != nil {
		return err
	}
	if _, err = target.DB.ExecContext(ctx, "PRAGMA max_page_count="+strconv.FormatInt(room/2/pageSize, 10)); err != nil {
		return err
	}
	tx, err := target.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	a := model.Author{ID: "unknown", Name: "其他搁置", Placeholder: true, Revision: 1, Aliases: []string{},
		FieldStatus: map[string]string{"identity": "missing", "biography": "missing", "homepage": "missing"}, CreatedAt: m.CreatedAt, UpdatedAt: m.CreatedAt}
	raw, err := store.Encode(a)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO authors(id,name,data) VALUES(?,?,?)", a.ID, a.Name, raw); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO settings(key,value) VALUES('instance_id',?)", m.InstanceID); err != nil {
		return err
	}
	media.report.Stage = "libraries"
	if err = readLibraries(ctx, source, tx, in.library, media.report); err != nil {
		return err
	}
	media.report.Stage = "originals"
	if err = media.live(ctx, source, tx, in.library); err != nil {
		return err
	}
	if err = media.checkLibraryFiles(ctx, in.library, true); err != nil {
		return err
	}
	media.report.Stage = "trash"
	if err = media.trashFiles(ctx, tx, in.trash, in.library, false); err != nil {
		return err
	}
	media.report.Stage = "catalog"
	if err = catalog(ctx, source, tx, media.sourceID); err != nil {
		return failed("catalog_projection_failed")
	}
	media.report.Stage = "activity"
	if err = activity(ctx, source, tx, media.sourceID); err != nil {
		return failed("activity_projection_failed")
	}
	media.report.Stage = "historical_records"
	if err = importHistory(ctx, source, tx, media.sourceID); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	media.report.Stage = "verify_snapshot"
	if err = store.ValidateBackupSchema(ctx, target.DB); err != nil {
		return err
	}
	rows, err := target.DB.QueryContext(ctx, "SELECT kind,count(*) FROM migration_relation_origins WHERE input_id=? GROUP BY kind ORDER BY kind", media.sourceID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var origin OriginCount
		if err = rows.Scan(&origin.Kind, &origin.Rows); err != nil {
			return err
		}
		media.report.Origins = append(media.report.Origins, origin)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	return target.DB.Close()
}

func writeArchive(ctx context.Context, opt Options, scratch *readRoot, snapshot fileRef, manifest backup.Manifest, media *mediaSet) (string, error) {
	raw, err := backup.Encode(manifest)
	if err != nil {
		return "", failed("native_archive_manifest_limit")
	}
	room, err := available(ctx, opt.Output, opt.ReserveBytes)
	// Each tar entry adds one 512-byte header and at most 511 bytes of padding.
	overhead := int64(len(raw)) + (int64(len(media.files))+3)*1024
	if err != nil || room-overhead-snapshot.info.Size() < media.report.OriginalBytes {
		return "", failed("insufficient_space_for_complete_archive")
	}
	out, err := scratch.root.OpenFile("library.qnext", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	w := &reservedWriter{ctx: ctx, dir: opt.Output, reserve: opt.ReserveBytes, w: io.MultiWriter(out, hash)}
	var active string
	open := func(f backup.File) (io.ReadCloser, error) {
		active = f.ObjectID
		if f.Kind == "snapshot" {
			return snapshot.open()
		}
		return media.files[media.ids[f.ObjectID]].ref.open()
	}
	err = backup.Write(ctx, w, manifest, open, func(i int) error {
		return media.files[i-1].ref.check()
	})
	if err == nil {
		err = out.Sync()
	}
	err = errors.Join(err, out.Close())
	if err != nil {
		var i *issue
		if errors.As(err, &i) {
			return "", locate(i, "archive", 0, active)
		}
		return "", locate(failed("archive_copy_or_source_verification_failed"), "archive", 0, active)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
