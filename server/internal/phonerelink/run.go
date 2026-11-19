package phonerelink

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"qimeng.local/next/server/internal/service"
	"qimeng.local/next/server/internal/store"
)

type Options struct {
	Archive, ArchiveSHA                    string
	Restored, RestoredSHA, RestoreReceipt  string
	Target, TargetSHA                      string
	Plan, PlanSHA, Inventory, InventorySHA string
	Destination                            string
	TrashBudget                            int64
}

type Receipt struct {
	State              string      `json:"state"`
	BackupID           string      `json:"backupId"`
	SourceInstanceID   string      `json:"sourceInstanceId"`
	RestoredInstanceID string      `json:"restoredInstanceId"`
	TargetInstanceID   string      `json:"targetInstanceId"`
	ArchiveSHA         string      `json:"archiveSHA256"`
	RestoredSHA        string      `json:"restoredCatalogSHA256"`
	TargetSHA          string      `json:"targetCatalogSHA256"`
	PlanSHA            string      `json:"planSHA256"`
	InventorySHA       string      `json:"inventorySHA256"`
	OutputSHA          string      `json:"outputCatalogSHA256"`
	Libraries          int         `json:"libraries"`
	Live               int         `json:"liveOriginals"`
	Trash              int         `json:"trashOriginals"`
	StagedTrash        []TrashFile `json:"stagedTrash,omitempty"`
}

func inside(dir, candidate string) bool {
	rel, err := filepath.Rel(dir, candidate)
	return err == nil && (rel == "." || filepath.IsLocal(rel))
}

func (o Options) validate() error {
	if o.TrashBudget < 0 {
		return refusal("invalid_trash_byte_budget")
	}
	for _, digest := range []string{o.ArchiveSHA, o.RestoredSHA, o.TargetSHA, o.PlanSHA, o.InventorySHA} {
		if !isDigest(digest) {
			return refusal("independent_lowercase_sha256_required")
		}
	}
	for _, name := range []string{o.Archive, o.Restored, o.RestoreReceipt, o.Target, o.Plan, o.Inventory, o.Destination} {
		if !filepath.IsAbs(name) || name != filepath.Clean(name) {
			return refusal("absolute_private_paths_required")
		}
	}
	if inside(filepath.Dir(o.Restored), o.Destination) {
		return refusal("destination_inside_restored_data")
	}
	if filepath.Base(o.Target) == "qimeng.sqlite" && inside(filepath.Dir(o.Target), o.Destination) {
		return refusal("destination_inside_target_data")
	}
	if err := plainDir(filepath.Dir(o.Destination)); err != nil {
		return err
	}
	if _, err := os.Lstat(o.Destination); err == nil {
		return refusal("destination_already_exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func verifySource(ctx context.Context, archive, receiptFile, source *inputFile, snapshot string, o Options) (service.RestoreResult, error) {
	var restored service.RestoreResult
	for _, name := range []string{"RESTORE_INCOMPLETE", "authority.sqlite"} {
		if _, err := source.root.Lstat(name); err == nil {
			return restored, refusal("fresh_stopped_native_restore_required")
		} else if !errors.Is(err, os.ErrNotExist) {
			return restored, err
		}
	}
	if err := readJSON(ctx, receiptFile, "", 16<<10, &restored); err != nil {
		return restored, err
	}
	verified, err := service.VerifyBackupCatalog(ctx, archive.file, o.ArchiveSHA, snapshot)
	if err != nil {
		return restored, errors.Join(refusal("native_archive_verification_failed"), err)
	}
	if err := archive.check(); err != nil {
		return restored, err
	}
	if restored.State != "restored" || restored.ArchiveHash != verified.ArchiveHash || restored.BackupID != verified.BackupID ||
		restored.SourceInstanceID != verified.SourceInstanceID || restored.AssetCount != verified.AssetCount || restored.UploadCount != verified.UploadCount ||
		!isInstanceID(restored.InstanceID) || restored.InstanceID == restored.SourceInstanceID {
		return restored, refusal("restore_receipt_does_not_match_native_archive")
	}
	if verified.UploadCount != 0 {
		return restored, refusal("unsettled_upload_files_require_full_restore")
	}
	return restored, nil
}

func prepare(ctx context.Context, o Options, dir, ready string, inputs []*inputFile) (Receipt, error) {
	var result Receipt
	var p plan
	var census inventory
	if err := readJSON(ctx, inputs[4], o.PlanSHA, 256<<20, &p); err != nil {
		return result, err
	}
	if err := readJSON(ctx, inputs[5], o.InventorySHA, 256<<20, &census); err != nil {
		return result, err
	}
	sourceFile, targetFile := filepath.Join(dir, "restored.sqlite"), filepath.Join(dir, "target.sqlite")
	if err := copyCatalog(ctx, inputs[1], sourceFile, o.RestoredSHA); err != nil {
		return result, err
	}
	if err := copyCatalog(ctx, inputs[3], targetFile, o.TargetSHA); err != nil {
		return result, err
	}
	for _, name := range []string{sourceFile, targetFile} {
		if err := sealedDB(ctx, name); err != nil {
			return result, err
		}
	}
	snapshot := filepath.Join(dir, "archive.sqlite")
	r, err := verifySource(ctx, inputs[0], inputs[2], inputs[1], snapshot, o)
	if err != nil {
		return result, err
	}
	catalog, err := store.Open(snapshot)
	if err != nil {
		return result, err
	}
	defer catalog.DB.Close()
	if err := matchRestored(ctx, catalog.DB, sourceFile, r); err != nil {
		return result, err
	}
	targetID, err := checkTarget(ctx, catalog.DB, targetFile, r)
	if err != nil {
		return result, err
	}
	d, err := bindFiles(ctx, catalog.DB, p, census, r.AssetCount, o.TrashBudget)
	if err != nil {
		return result, err
	}
	if err := d.apply(ctx, catalog.DB, targetID); err != nil {
		return result, err
	}
	for i := range d.staged {
		if err := d.staged[i].stage(ctx, filepath.Dir(o.Restored), ready); err != nil {
			return result, err
		}
	}
	if _, err := catalog.DB.ExecContext(ctx, "DETACH DATABASE phone"); err != nil {
		return result, err
	}
	outputFile := filepath.Join(ready, "qimeng.sqlite")
	// The final catalog is compacted after native capability revocation. Source
	// credential remnants in freed snapshot pages never enter the delivery.
	if _, err := catalog.DB.ExecContext(ctx, "VACUUM main INTO ?", outputFile); err != nil {
		return result, err
	}
	if err := os.Chmod(outputFile, 0600); err != nil {
		return result, err
	}
	if err := sealedDB(ctx, outputFile); err != nil {
		return result, err
	}
	outputSHA, err := hashFile(outputFile)
	if err != nil {
		return result, err
	}
	result = Receipt{State: "prepared", BackupID: r.BackupID, SourceInstanceID: r.SourceInstanceID, RestoredInstanceID: r.InstanceID,
		TargetInstanceID: targetID, ArchiveSHA: o.ArchiveSHA, RestoredSHA: o.RestoredSHA, TargetSHA: o.TargetSHA,
		PlanSHA: o.PlanSHA, InventorySHA: o.InventorySHA, OutputSHA: outputSHA,
		Libraries: len(d.roots), Live: len(d.live), Trash: d.trashed, StagedTrash: d.staged}
	out, err := os.OpenFile(filepath.Join(ready, "phone-relink.json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return result, err
	}
	err = json.NewEncoder(out).Encode(result)
	return result, errors.Join(err, out.Sync(), out.Close())
}

func Run(ctx context.Context, o Options) (Receipt, error) {
	var result Receipt
	if err := o.validate(); err != nil {
		return result, err
	}
	var inputs []*inputFile
	for i, name := range []string{o.Archive, o.Restored, o.RestoreReceipt, o.Target, o.Plan, o.Inventory} {
		input, err := openInput(name, i == 1 || i == 3)
		if err != nil {
			return result, err
		}
		defer input.close()
		inputs = append(inputs, input)
	}
	parent := filepath.Dir(o.Destination)
	dir, err := os.MkdirTemp(parent, ".qimeng-phone-relink-")
	if err != nil {
		return result, err
	}
	defer os.RemoveAll(dir)
	ready := filepath.Join(dir, "ready")
	if err := os.Mkdir(ready, 0700); err != nil {
		return result, err
	}
	result, err = prepare(ctx, o, dir, ready, inputs)
	if err != nil {
		return Receipt{}, err
	}
	for _, input := range inputs {
		if err := input.check(); err != nil {
			return Receipt{}, err
		}
	}
	entries, err := os.ReadDir(ready)
	if err != nil {
		return Receipt{}, err
	}
	if len(result.StagedTrash) > 0 && len(entries) == 3 && entries[0].Name() == "phone-files" && entries[0].IsDir() {
		entries = entries[1:]
	}
	if len(entries) != 2 || entries[0].Name() != "phone-relink.json" || entries[1].Name() != "qimeng.sqlite" {
		return Receipt{}, refusal("output_contains_unexpected_files")
	}
	if err := ctx.Err(); err != nil {
		return Receipt{}, err
	}
	if err := syncDir(ready); err != nil {
		return Receipt{}, err
	}
	if err := publish(ready, o.Destination); err != nil {
		return Receipt{}, err
	}
	return result, syncDir(parent)
}
