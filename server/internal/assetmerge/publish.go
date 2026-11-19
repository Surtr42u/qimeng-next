package assetmerge

import (
	"context"
	"errors"
	"io/fs"
	"path"
	"path/filepath"

	"qimeng.local/next/server/internal/store"
)

type inventory struct {
	LibraryDirs map[string]bool `json:"libraryDirectories"`
	Files       []original      `json:"files"`
}

type publication struct {
	Claim         claim   `json:"claim"`
	Receipt       Receipt `json:"receipt"`
	CatalogHash   string  `json:"catalogSHA256"`
	CatalogObject string  `json:"catalogFileIdentity"`
	InventoryHash string  `json:"inventorySHA256"`
	SelectionHash string  `json:"selectionSHA256"`
}

func (op *operation) readJSON(ctx context.Context, name string, limit int64, to any) (string, error) {
	return externalJSON(ctx, filepath.Join(op.name, filepath.FromSlash(name)), limit, to)
}

func (a *area) exists(name string) (bool, error) {
	_, err := a.inspect(name)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

func (op *operation) resume(ctx context.Context) (Receipt, error) {
	pre := op.req.Preimage
	var ready publication
	if _, err := op.readJSON(ctx, pre+"/ready.json", 128<<10, &ready); err != nil {
		return Receipt{}, phase("incomplete_attempt_restore_again", err)
	}
	if ready.Claim.Request != op.req || ready.Claim.Token == "" || ready.Receipt.InstanceID != op.req.Restore.InstanceID ||
		ready.Receipt.PlanSHA256 != op.req.PlanHash || ready.Receipt.ArchiveSHA256 != op.req.ArchiveHash || ready.Receipt.BaselineSHA256 != op.plan.BaselineSHA256 {
		return Receipt{}, phase("existing_attempt_has_different_authority", errors.New("mismatched existing receipt"))
	}
	completed, err := op.exists(pre + "/published.json")
	if err != nil {
		return Receipt{}, phase("publication_receipt", err)
	}
	if completed {
		return op.completed(ctx, ready)
	}
	// This lock lives outside the directory being renamed. A second process
	// cannot finish the same handoff during the Windows close-and-rename window.
	if _, err := op.inspect(pre + "/handoff.sqlite"); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return Receipt{}, phase("publication_lock", err)
	}
	owner, err := store.OpenDatabase(filepath.Join(op.name, pre, "handoff.sqlite"), store.OwnershipDatabase)
	if err != nil {
		return Receipt{}, phase("publication_lock", err)
	}
	defer owner.Close()
	if _, err = owner.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS authority(id INTEGER); BEGIN EXCLUSIVE"); err != nil {
		return Receipt{}, phase("publication_lock", err)
	}
	completed, err = op.exists(pre + "/published.json")
	if err != nil {
		return Receipt{}, phase("publication_receipt", err)
	}
	if completed {
		return op.completed(ctx, ready)
	}
	stage, final := op.req.Stage, op.req.Destination
	existsStage, err := op.exists(stage)
	if err != nil {
		return Receipt{}, phase("working_directory_changed", err)
	}
	dir := final
	if existsStage {
		if err := op.absent(final); err != nil {
			return Receipt{}, phase("destination_must_be_new", err)
		}
		dir = stage
	}
	if err := op.checkNote(ctx, dir, ready, true); err != nil {
		return Receipt{}, phase("private_handoff_identity", err)
	}
	if err := op.verifyReady(ctx, dir, ready); err != nil {
		return Receipt{}, phase("final_catalog_and_file_readback", err)
	}
	if err := op.sameInputs(ctx); err != nil {
		return Receipt{}, phase("authorized_inputs_changed", err)
	}
	if dir == stage {
		info, err := op.inspect(stage)
		if err != nil || !info.IsDir() {
			return Receipt{}, phase("working_directory_changed", errors.New("working directory is unavailable"))
		}
		if err := op.move(stage, final, info); err != nil {
			return Receipt{}, phase("atomic_directory_publication", err)
		}
	}
	if err := op.checkNote(ctx, final, ready, true); err != nil {
		return Receipt{}, phase("final_handoff_identity", err)
	}
	marker := final + "/RESTORE_INCOMPLETE"
	info, err := op.inspect(marker)
	if err != nil {
		return Receipt{}, phase("finish_publication", err)
	}
	// The server refuses this marker throughout the directory move. Moving it
	// to the private receipt is the release point and is itself no-replace.
	if err := op.move(marker, pre+"/published.json", info); err != nil {
		return Receipt{}, phase("finish_publication", err)
	}
	return ready.Receipt, nil
}

func (op *operation) checkNote(ctx context.Context, dir string, ready publication, pending bool) error {
	var note publication
	if _, err := op.readJSON(ctx, dir+"/"+publishedNote, 128<<10, &note); err != nil || ready != note {
		return errors.New("directory has a different publication receipt")
	}
	if !pending {
		return op.absent(dir + "/RESTORE_INCOMPLETE")
	}
	var marker claim
	if _, err := op.readJSON(ctx, dir+"/RESTORE_INCOMPLETE", 128<<10, &marker); err != nil || ready.Claim != marker {
		return errors.New("directory has a different incomplete marker")
	}
	return nil
}

func (op *operation) completed(ctx context.Context, ready publication) (Receipt, error) {
	var marker claim
	_, err := op.readJSON(ctx, op.req.Preimage+"/published.json", 128<<10, &marker)
	if err != nil || marker != ready.Claim {
		return Receipt{}, phase("publication_receipt", errors.New("private receipt changed"))
	}
	if err := op.absent(op.req.Stage); err != nil {
		return Receipt{}, phase("stage_name_reused", err)
	}
	if err := op.checkNote(ctx, op.req.Destination, ready, false); err != nil {
		return Receipt{}, phase("published_directory_changed", err)
	}
	// Read only the immutable handoff receipt. Current catalog and original
	// contents belong to the normal service after successful publication.
	return ready.Receipt, nil
}

func (op *operation) verifyReady(ctx context.Context, dir string, ready publication) error {
	s, err := op.readFile(ctx, dir+"/qimeng.sqlite", nil)
	if err != nil || s.Hash != ready.CatalogHash || s.Object != ready.CatalogObject {
		return errors.New("prepared catalog changed after projection")
	}
	pre := op.req.Preimage
	var inv inventory
	hash, err := op.readJSON(ctx, pre+"/baseline-files.json", 512<<20, &inv)
	if err != nil || hash != ready.InventoryHash {
		return errors.New("sealed inventory changed")
	}
	var moves []relocation
	hash, err = op.readJSON(ctx, pre+"/selected-files.json", 512<<20, &moves)
	if err != nil || hash != ready.SelectionHash {
		return errors.New("sealed physical decisions changed")
	}
	destinations := map[string]string{}
	for _, m := range moves {
		destinations[m.AssetID] = m.After
	}
	expected := map[string]bool{"qimeng.sqlite": true, "authority.sqlite": true, "RESTORE_INCOMPLETE": true, publishedNote: true}
	for p := range inv.LibraryDirs {
		expected[p] = true
	}
	for _, f := range inv.Files {
		name := f.Physical
		if moved, ok := destinations[f.ObjectID]; ok && f.Kind == "asset" {
			name = moved
		}
		for ancestor := path.Dir(name); ancestor != "."; ancestor = path.Dir(ancestor) {
			expected[ancestor] = true
		}
		if expected[name] {
			return errors.New("physical destinations collide")
		}
		expected[name] = true
		current, err := op.readFile(ctx, dir+"/"+name, nil)
		if err != nil || !f.Stamp.matches(current) {
			return errors.New("final physical identity, bytes or modification time differ from the baseline")
		}
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if err := op.absent(dir + "/qimeng.sqlite" + suffix); err != nil {
			return err
		}
	}
	return op.verifyTree(dir, expected)
}
