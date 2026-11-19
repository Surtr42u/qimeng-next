package assetmerge

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"

	"qimeng.local/next/server/internal/assetmerge/facts"
	"qimeng.local/next/server/internal/backup"
	"qimeng.local/next/server/internal/service"
	"qimeng.local/next/server/internal/store"
)

type phaseError struct {
	phase string
	err   error
}

func (e *phaseError) Error() string { return e.phase + ": " + e.err.Error() }
func (e *phaseError) Unwrap() error { return e.err }
func phase(name string, err error) error {
	if err == nil {
		return nil
	}
	return &phaseError{name, err}
}

// FailurePhase is suitable for an operator's terminal even when a wrapped
// filesystem or SQLite error contains private path, identity or source text.
func FailurePhase(err error) string {
	var failure *phaseError
	if errors.As(err, &failure) {
		return failure.phase
	}
	return "arguments"
}

type operation struct {
	*area
	opts        Options
	req         request
	plan        facts.Plan
	manifest    backup.Manifest
	archiveInfo os.FileInfo
	scratch     string
	baseline    stamp
	projected   stamp
	files       []original
	libraryDirs map[string]bool
	moves       []relocation
	result      Receipt
}

func Run(ctx context.Context, mode string, o Options) (Receipt, error) {
	if mode != "check" && mode != "apply" {
		return Receipt{}, errors.New("expected check or apply")
	}
	if o.ReserveBytes == 0 {
		o.ReserveBytes = 512 << 20
	}
	parent, r, err := splitPaths(o)
	if err != nil {
		return Receipt{}, phase("input_paths", err)
	}
	a, err := openArea(parent)
	if err != nil {
		return Receipt{}, phase("private_parent", err)
	}
	defer a.root.Close()
	op := operation{area: a, req: r, opts: o}
	if err = op.inputs(ctx); err != nil {
		return Receipt{}, err
	}
	if _, err = a.inspect(r.Preimage); err == nil {
		if mode != "apply" {
			return Receipt{}, phase("preimage_already_exists", fs.ErrExist)
		}
		return op.resume(ctx)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return Receipt{}, phase("preimage_path", err)
	}
	if err = a.absent(r.Destination); err != nil {
		return Receipt{}, phase("destination_must_be_new", err)
	}
	stage, err := a.inspect(r.Stage)
	if err != nil || !stage.IsDir() {
		return Receipt{}, phase("new_offline_restore_required", errors.New("missing new private restore"))
	}
	for _, item := range []string{"authority.sqlite", "RESTORE_INCOMPLETE", publishedNote} {
		if err := a.absent(path.Join(r.Stage, item)); err != nil {
			return Receipt{}, phase("new_offline_restore_required", err)
		}
	}
	op.scratch = ".qimeng-merge-" + rand.Text()
	if err = a.mkdir(op.scratch); err != nil {
		return Receipt{}, phase("private_scratch", err)
	}
	// Only disposable database copies are written here. Originals in transit
	// after the claim stay in the separately recorded preimage directory.
	defer a.root.RemoveAll(op.scratch)
	if err = op.prepare(ctx); err != nil {
		return Receipt{}, err
	}
	if mode == "check" {
		return op.result, nil
	}
	if err = op.apply(ctx, stage); err != nil {
		return Receipt{}, err
	}
	return op.resume(ctx)
}

func (op *operation) inputs(ctx context.Context) error {
	hash, err := externalJSON(ctx, op.opts.Plan, backup.MaxManifestBytes, &op.plan)
	if err != nil || hash != op.req.PlanHash {
		return phase("private_plan_fingerprint", errors.New("plan is unreadable or differs from its expected digest"))
	}
	if _, err = externalJSON(ctx, op.opts.RestoreReceipt, 64<<10, &op.req.Restore); err != nil {
		return phase("native_restore_receipt", err)
	}
	op.archiveInfo, err = os.Lstat(op.opts.Archive)
	if err != nil {
		return phase("native_archive", err)
	}
	op.manifest, err = verifiedArchive(ctx, op.opts, op.req.Restore)
	return phase("native_archive_and_restore_binding", err)
}

func (op *operation) sameInputs(ctx context.Context) error {
	var receipt service.RestoreResult
	var plan facts.Plan
	hash, err := externalJSON(ctx, op.opts.Plan, backup.MaxManifestBytes, &plan)
	if err != nil || hash != op.req.PlanHash {
		return errors.New("private plan changed")
	}
	if _, err := externalJSON(ctx, op.opts.RestoreReceipt, 64<<10, &receipt); err != nil || receipt != op.req.Restore {
		return errors.New("restore receipt changed")
	}
	info, err := os.Lstat(op.opts.Archive)
	if err != nil || !sameInfo(info, op.archiveInfo) {
		return errors.New("sealed archive changed")
	}
	return nil
}

func (op *operation) prepare(ctx context.Context) error {
	sides, err := op.sidecars()
	if err != nil {
		return phase("sealed_restored_catalog", err)
	}
	stageDB := op.req.Stage + "/qimeng.sqlite"
	copyName := op.scratch + "/projected.sqlite"
	op.baseline, err = op.copy(ctx, stageDB, copyName, op.opts.ReserveBytes)
	if err != nil {
		return phase("copy_stopped_catalog", err)
	}
	if op.req.CatalogHash != "" && op.baseline.Hash != op.req.CatalogHash {
		return phase("verified_catalog_fingerprint", errors.New("restored catalog differs from the independently verified copy"))
	}
	// Reading only from the private copy keeps the restored WAL and catalog
	// untouched, including when the owner asks for a check without apply.
	candidate := filepath.Join(op.name, filepath.FromSlash(copyName))
	snapshot, err := store.OpenDatabase(candidate, store.SnapshotDatabase)
	if err != nil {
		return phase("restored_catalog_schema", err)
	}
	var id string
	if err = store.ValidateBackupSchema(ctx, snapshot); err == nil {
		err = snapshot.QueryRowContext(ctx, "SELECT value FROM settings WHERE key='instance_id'").Scan(&id)
	}
	err = errors.Join(err, snapshot.Close())
	if err != nil || id != op.req.Restore.InstanceID {
		return phase("restored_catalog_identity", errors.New("invalid native catalog or restored instance ID"))
	}
	if err = op.sameSidecars(sides); err != nil {
		return phase("catalog_changed", err)
	}
	db, err := store.Open(candidate)
	if err != nil {
		return phase("upgrade_private_catalog_copy", err)
	}
	defer db.DB.Close()
	stage := filepath.Join(op.name, op.req.Stage)
	before, err := readCatalog(ctx, db.DB, stage)
	if err != nil {
		return phase("restored_current_versions", err)
	}
	op.files, err = op.verifyBaseline(ctx, op.req.Stage, op.manifest, before)
	if err != nil {
		return phase("baseline_inventory_and_originals", err)
	}
	op.libraryDirs = before.dirs
	result, err := facts.Apply(ctx, db.DB, op.plan)
	if err != nil {
		return phase("authorized_group_facts", err)
	}
	if op.req.ArchiveHash != op.plan.BaselineSHA256 && !result.AlreadyApplied {
		return phase("baseline_sha256_binding", errors.New("a different archive can only replay existing decisions"))
	}
	after, err := readCatalog(ctx, db.DB, stage)
	if err != nil {
		return phase("projected_current_versions", err)
	}
	op.moves, err = layout(op.plan, op.files, after, result.AlreadyApplied)
	if err != nil {
		return phase("authorized_physical_layout", err)
	}
	if err = op.checkTargetPaths(op.req.Stage, op.moves); err != nil {
		return phase("destination_occupancy", err)
	}
	if err = store.ValidateBackupSchema(ctx, db.DB); err != nil {
		return phase("projected_catalog_integrity", err)
	}
	var busy, logPages, done int
	err = db.DB.QueryRowContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &logPages, &done)
	if err != nil || busy != 0 {
		return phase("seal_projected_catalog", errors.New("could not checkpoint catalog"))
	}
	if err = db.DB.Close(); err != nil {
		return phase("seal_projected_catalog", err)
	}
	op.projected, err = op.readFile(ctx, copyName, nil)
	if err != nil {
		return phase("seal_projected_catalog", err)
	}
	op.result = Receipt{State: "checked", InstanceID: id, ArchiveSHA256: op.req.ArchiveHash, BaselineSHA256: op.plan.BaselineSHA256,
		PlanSHA256: op.req.PlanHash, VerifiedAssets: op.req.Restore.AssetCount, RestoredCatalogSHA256: op.baseline.Hash, Result: result}
	return nil
}

func (op *operation) sidecars() (map[string]os.FileInfo, error) {
	result := map[string]os.FileInfo{}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		p := op.req.Stage + "/qimeng.sqlite" + suffix
		info, err := op.inspect(p)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || (suffix != "-shm" && info.Size() != 0) {
			return nil, errors.New("restored catalog has unsealed changes")
		}
		result[suffix] = info
	}
	return result, nil
}

func (op *operation) sameSidecars(before map[string]os.FileInfo) error {
	after, err := op.sidecars()
	if err != nil {
		return err
	}
	if len(before) != len(after) {
		return errors.New("catalog sidecars changed")
	}
	for k, info := range before {
		current, ok := after[k]
		if !ok || !sameInfo(current, info) {
			return errors.New("catalog sidecar changed")
		}
	}
	return nil
}

func (op *operation) lockStage(ctx context.Context) (*sql.DB, error) {
	name := op.req.Stage + "/authority.sqlite"
	f, err := op.root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	if err = f.Close(); err != nil {
		return nil, err
	}
	owner, err := store.OpenDatabase(filepath.Join(op.name, filepath.FromSlash(name)), store.OwnershipDatabase)
	if err != nil {
		return nil, err
	}
	// The same exclusive lock is held by the normal server. Its database must
	// be new here because qimeng-backup never creates an authority database.
	if _, err = owner.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS authority(id INTEGER); BEGIN EXCLUSIVE"); err != nil {
		return nil, errors.Join(err, owner.Close())
	}
	return owner, nil
}

const publishedNote = "SELECTED_IMAGE_MERGE.json"

type claim struct {
	Token   string  `json:"operationId"`
	Request request `json:"request"`
}

func (op *operation) apply(ctx context.Context, stageInfo os.FileInfo) error {
	stage, pre := op.req.Stage, op.req.Preimage
	current, err := op.inspect(stage)
	if err != nil || !os.SameFile(stageInfo, current) {
		return phase("working_directory_changed", errors.New("private restore was replaced"))
	}
	if err := op.sameInputs(ctx); err != nil {
		return phase("authorized_inputs_changed", err)
	}
	if err = op.absent(op.req.Destination); err != nil {
		return phase("destination_must_be_new", err)
	}
	if err = op.mkdir(pre); err != nil {
		return phase("new_private_preimage", err)
	}
	c := claim{rand.Text(), op.req}
	if err = op.writeJSON(pre+"/attempt.json", c); err != nil {
		return phase("record_private_attempt", err)
	}
	owner, err := op.lockStage(ctx)
	if err != nil {
		return phase("exclusive_offline_ownership", err)
	}
	defer owner.Close()
	if err = op.writeJSON(stage+"/RESTORE_INCOMPLETE", c); err != nil {
		return phase("mark_private_working_copy", err)
	}
	if err = op.syncDir(stage); err != nil {
		return phase("mark_private_working_copy", err)
	}
	if err = op.archiveOriginals(ctx); err != nil {
		return phase("preserve_selected_preimages", err)
	}
	stageDB := stage + "/qimeng.sqlite"
	sides, err := op.sidecars()
	if err != nil {
		return phase("working_catalog_changed", err)
	}
	currentDB, err := op.readFile(ctx, stageDB, nil)
	if err != nil || !op.baseline.matches(currentDB) {
		return phase("working_catalog_changed", errors.New("working catalog differs from checked catalog"))
	}
	if err = op.sameSidecars(sides); err != nil {
		return phase("working_catalog_changed", err)
	}
	for suffix, saved := range sides {
		if err := op.move(stageDB+suffix, pre+"/idle"+suffix, saved); err != nil {
			return phase("quarantine_idle_sqlite_sidecars", err)
		}
	}
	// Preserve the actual sealed database as the independent preimage. SQLite
	// was opened only on its separate successor in the private scratch area.
	if err = op.move(stageDB, pre+"/qimeng.sqlite", currentDB.info); err != nil {
		return phase("preserve_original_catalog", err)
	}
	if err = op.relocate(ctx); err != nil {
		return phase("relocate_selected_originals", err)
	}
	if err = op.move(op.scratch+"/projected.sqlite", stageDB, op.projected.info); err != nil {
		return phase("install_checked_catalog", err)
	}
	op.result.State = "published"
	inventory, err := op.readFile(ctx, pre+"/baseline-files.json", nil)
	if err != nil {
		return phase("seal_working_copy", err)
	}
	selection, err := op.readFile(ctx, pre+"/selected-files.json", nil)
	if err != nil {
		return phase("seal_working_copy", err)
	}
	note := publication{Claim: c, Receipt: op.result, CatalogHash: op.projected.Hash, CatalogObject: op.projected.Object,
		BaselineCatalogHash: op.baseline.Hash, BaselineCatalogObject: op.baseline.Object, InventoryHash: inventory.Hash, SelectionHash: selection.Hash}
	if err = op.writeJSON(stage+"/"+publishedNote, note); err != nil {
		return phase("seal_working_copy", err)
	}
	if err = op.syncDir(stage); err != nil {
		return phase("seal_working_copy", err)
	}
	if err = op.writeJSON(pre+"/ready.json", note); err != nil {
		return phase("seal_working_copy", err)
	}
	if err = op.syncDir(pre); err != nil {
		return phase("seal_working_copy", err)
	}
	return phase("release_offline_ownership", owner.Close())
}

func (op *operation) archiveOriginals(ctx context.Context) error {
	pre := op.req.Preimage
	if err := op.mkdir(pre + "/originals"); err != nil {
		return err
	}
	if err := op.mkdir(pre + "/transit"); err != nil {
		return err
	}
	for _, m := range op.moves {
		copied, err := op.copy(ctx, op.req.Stage+"/"+m.Before, pre+"/"+m.Copy, op.opts.ReserveBytes)
		if err != nil || !m.Stamp.matches(copied) {
			return errors.New("selected original changed or could not be preserved")
		}
		sealed, err := op.readFile(ctx, pre+"/"+m.Copy, nil)
		if err != nil || sealed.Hash != m.Stamp.Hash || !sealed.Time.Equal(m.Stamp.Time) || sealed.Size != m.Stamp.Size {
			return errors.New("selected preimage readback failed")
		}
	}
	if err := op.writeJSON(pre+"/baseline-files.json", inventory{op.libraryDirs, op.files}); err != nil {
		return err
	}
	if err := op.writeJSON(pre+"/selected-files.json", op.moves); err != nil {
		return err
	}
	if err := op.writeJSON(pre+"/catalog-fingerprint.json", op.baseline); err != nil {
		return err
	}
	return errors.Join(op.syncDir(pre+"/originals"), op.syncDir(pre))
}

func (op *operation) relocate(ctx context.Context) error {
	stage, transit := op.req.Stage, op.req.Preimage+"/transit/"
	if err := op.checkTargetPaths(stage, op.moves); err != nil {
		return err
	}
	// Evacuate all renamed files first, allowing the representative to claim a
	// donor's former name or change its spelling on a case-insensitive volume.
	for _, m := range op.moves {
		name := stage + "/" + m.Before
		s, err := op.readFile(ctx, name, nil)
		if err != nil || !m.Stamp.matches(s) {
			return errors.New("selected original changed before relocation")
		}
		if m.Before != m.After {
			if err := op.move(name, transit+path.Base(m.Copy), s.info); err != nil {
				return err
			}
		}
	}
	for _, m := range op.moves {
		if m.Before == m.After {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		to := stage + "/" + m.After
		if err := op.mkdirAll(path.Dir(to)); err != nil {
			return err
		}
		if err := op.move(transit+path.Base(m.Copy), to, m.Stamp.info); err != nil {
			return err
		}
	}
	return nil
}
