package httpapi_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/service"
	"qimeng.local/next/server/internal/store"
)

// Install a synthetic offline decision in the same native layout consumed by
// the service. Normal uploads, scans, moves and trash establish the originals.
func selectedImageFixture(t *testing.T) (*fixture, model.Asset, model.Asset, []byte) {
	t.Helper()
	f := setup(t)
	u, original := f.upload("first.png", 53, map[string]any{"relativeDirectory": "images"})
	rep := *f.complete(u).Asset
	root, err := f.s.Root.LibraryPath(f.library)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "images", "variant.png"), original, 0600); err != nil {
		t.Fatal(err)
	}
	if job := f.scan("scan-equal-images"); job.State != "completed" || job.Added != 1 {
		t.Fatal("failed to index the second physical file", job)
	}
	candidates := assetsAt(t, f, "?q=variant")
	if len(candidates.Items) != 1 {
		t.Fatal("missing synthetic member")
	}
	donor := candidates.Items[0]
	decode(t, f.json("GET", "/assets/"+rep.ID, "", nil, 200), &rep)
	repOriginal := rep
	decode(t, f.json("POST", "/assets/"+rep.ID+"/move", "review-name", map[string]any{
		"expectedRevision": rep.Revision, "relativePath": "images/reviewed.png"}, 200), &rep)
	donorOriginal := donor
	decode(t, f.json("DELETE", fmt.Sprintf("/assets/%s?expectedRevision=%d", donor.ID, donor.Revision), "retain-variant", struct{}{}, 200), &donor)
	var trash model.Page[model.Asset]
	decode(t, f.json("GET", "/trash", "", nil, 200), &trash)
	if len(trash.Items) != 1 || trash.Items[0].ID != donor.ID {
		t.Fatal("ordinary trash changed before the first merge decision")
	}
	tx, err := f.s.DB.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`INSERT INTO selected_image_merge_groups(representative_id,source_id,baseline_sha256,decision,applied_at)
		VALUES(?,'synthetic-source',?,'{}','2026-01-01T00:00:00Z')`, rep.ID, rep.Hash); err != nil {
		t.Fatal(err)
	}
	for _, member := range []model.Asset{repOriginal, donorOriginal} {
		record, err := store.Encode(member)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(`INSERT INTO selected_image_merge_members(physical_asset_id,representative_id,original_asset,original_relations)
			VALUES(?,?,?,'{}')`, member.ID, rep.ID, record); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return f, rep, donor, original
}

func checkPhysicalOriginal(t *testing.T, f *fixture, a model.Asset, original []byte) {
	t.Helper()
	name := "libraries/" + a.LibraryID + "/" + a.RelativePath
	if a.Trashed {
		name = "library-trash/" + a.LibraryID + "/" + a.ID + "/" + a.FileName
	}
	file, err := f.s.Root.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	actual, err := io.ReadAll(file)
	if err != nil || !bytes.Equal(actual, original) {
		t.Fatal("physical original differs at its own registered location", err)
	}
}

func TestSelectedImageMergeReadsAndPreservesEveryNativeOriginal(t *testing.T) {
	source, rep, donor, original := selectedImageFixture(t)
	upload, controlBytes := source.upload("independent.png", 54, nil)
	control := *source.complete(upload).Asset
	upload, _ = source.upload("discard.png", 55, nil)
	discard := *source.complete(upload).Asset
	decode(t, source.json("DELETE", fmt.Sprintf("/assets/%s?expectedRevision=%d", discard.ID, discard.Revision), "trash-independent", struct{}{}, 200), &discard)
	f := nativeCheckpointCopy(t, source, "selected-images", 4)
	if scan := f.scan("scan-restored-merge"); scan.State != "completed" || scan.Added+scan.Changed+scan.Moved != 0 {
		t.Fatal("normal scan changed the restored originals", scan)
	}
	page := assetsAt(t, f, "?libraryId="+f.library+"&directory=images")
	if *page.TotalMatched != 1 || len(page.Items) != 1 || page.Items[0].ID != rep.ID {
		t.Fatal("directory filter exposed a retained member")
	}
	var dirs service.DirectoryPage
	decode(t, f.json("GET", "/libraries/"+f.library+"/directories?parent=images", "", nil, 200), &dirs)
	if dirs.DirectFileCount != 1 || len(dirs.Items) != 0 {
		t.Fatal("directory inventory disagrees with the album")
	}
	var facets model.Page[service.AssetFacet]
	decode(t, f.json("GET", "/assets/facets?row=types&q=reviewed", "", nil, 200), &facets)
	if facets.TotalMatched == nil || *facets.TotalMatched != 1 || *assetsAt(t, f, "?q=variant").TotalMatched != 0 {
		t.Fatal("search or its facet counts exposed a second image")
	}
	var trash model.Page[model.Asset]
	decode(t, f.json("GET", "/trash", "", nil, 200), &trash)
	if trash.TotalMatched == nil || *trash.TotalMatched != 1 || len(trash.Items) != 1 || trash.Items[0].ID != discard.ID {
		t.Fatal("ordinary trash must contain the unrelated deleted image")
	}
	var stats map[string]int64
	decode(t, f.json("GET", "/stats", "", nil, 200), &stats)
	var status service.SystemStatus
	decode(t, f.json("GET", "/system/status", "", nil, 200), &status)
	if stats["assets"] != 2 || stats["trash"] != 1 || status.TrashCount != 1 {
		t.Fatal("trash or live counts included a physical duplicate")
	}
	old := "/assets/" + donor.ID
	var detail model.Asset
	decode(t, f.json("GET", old, "", nil, 200), &detail)
	if detail.ID != rep.ID || detail.FileName != "reviewed.png" || detail.Trashed {
		t.Fatal("legacy link did not open the reviewed representative")
	}
	assertNativeOriginal(t, f, donor.ID, 1, original)
	if thumb := f.request("GET", old+"/thumbnail?version=1", "", testSecret, nil, nil); thumb.Code != 200 || thumb.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatal("legacy thumbnail did not resolve", thumb.Code)
	}
	f.json("GET", old+"/content?version=2", "", nil, 409)
	checkPhysicalOriginal(t, f, rep, original)
	checkPhysicalOriginal(t, f, donor, original)
	for _, request := range []struct {
		method, url string
		body        any
	}{
		{"PUT", old + "/favorite", map[string]bool{"favorite": true}},
		{"PUT", old + "/tags", map[string]any{"expectedRevision": rep.TagsRevision, "tags": []string{"stale"}}},
		{"PATCH", old, map[string]any{"expectedRevision": rep.Revision, "note": "stale"}},
		{"POST", old + "/events", map[string]any{"eventId": "stale-image-view", "kind": "view", "contentVersion": 1}},
		{"POST", old + "/move", map[string]any{"expectedRevision": donor.Revision, "relativePath": "stale.png"}},
		{"DELETE", fmt.Sprintf("%s?expectedRevision=%d", old, donor.Revision), struct{}{}},
		{"POST", "/trash/" + donor.ID + "/restore", map[string]any{"expectedRevision": donor.Revision}},
		{"DELETE", fmt.Sprintf("/trash/%s?expectedRevision=%d", donor.ID, donor.Revision), struct{}{}},
		{"POST", "/uploads", map[string]any{"libraryId": f.library, "fileName": "new.png", "size": len(original), "mimeType": "image/png", "mode": "ordinary", "replaceAssetId": donor.ID, "expectedContentVersion": 1}},
	} {
		var rejection service.Error
		decode(t, f.json(request.method, request.url, "stale-merge-write", request.body, 409), &rejection)
		if rejection.Code != "asset_merged" || rejection.Details["representativeId"] != rep.ID {
			t.Fatal("stale write requires the exact identity", rejection.Code)
		}
	}
	var unchanged model.Asset
	decode(t, f.json("GET", "/assets/"+rep.ID, "", nil, 200), &unchanged)
	if unchanged.Revision != detail.Revision || unchanged.Favorite || unchanged.ViewCount != 0 || len(unchanged.Tags) != 0 || unchanged.Note != "" {
		t.Fatal("stale write changed the representative")
	}
	f.json("POST", "/uploads", "replace-retained-representative", map[string]any{"libraryId": f.library, "fileName": "new.png",
		"size": len(original), "mimeType": "image/png", "mode": "ordinary", "replaceAssetId": rep.ID, "expectedContentVersion": 1}, 409)
	decode(t, f.json("PUT", "/assets/"+rep.ID+"/favorite", "favorite-visible", map[string]bool{"favorite": true}, 200), &rep)
	decode(t, f.json("DELETE", fmt.Sprintf("/assets/%s?expectedRevision=%d", rep.ID, rep.Revision), "trash-visible", struct{}{}, 200), &rep)
	f.json("GET", old+"/content", "", nil, 404)
	f.json("GET", old+"/thumbnail", "", nil, 404)
	f.json("DELETE", fmt.Sprintf("/trash/%s?expectedRevision=%d", rep.ID, rep.Revision), "purge-retained-representative", struct{}{}, 409)
	f.json("DELETE", "/libraries/"+f.library+"?expectedRevision=1", "unregister-retained-library", struct{}{}, 409)
	checkPhysicalOriginal(t, f, rep, original)
	checkPhysicalOriginal(t, f, donor, original)
	f.json("DELETE", fmt.Sprintf("/trash/%s?expectedRevision=%d", discard.ID, discard.Revision), "purge-unrelated", struct{}{}, 204)
	decode(t, f.json("POST", "/trash/"+rep.ID+"/restore", "restore-visible", map[string]any{"expectedRevision": rep.Revision}, 200), &rep)
	reopened := nativeCheckpointCopy(t, f, "selected-again", 3)
	var backups model.Page[service.Backup]
	decode(t, f.json("GET", "/backups", "", nil, 200), &backups)
	verified := false
	for _, backup := range backups.Items {
		if backup.ExportState == "completed" {
			result, err := service.VerifyBackup(context.Background(), filepath.Join(f.s.Config.DataDir, "backups", backup.ID, "archive.qnext"), backup.ArchiveHash)
			if err != nil || result.AssetCount != 3 {
				t.Fatal("physical coverage of the second archive", result, err)
			}
			verified = true
		}
	}
	if !verified {
		t.Fatal("missing completed archive after the second export")
	}
	decode(t, reopened.json("GET", old, "", nil, 200), &detail)
	if detail.ID != rep.ID || !detail.Favorite || detail.Trashed {
		t.Fatal("second restore lost the legacy link or representative's edit")
	}
	checkPhysicalOriginal(t, reopened, rep, original)
	checkPhysicalOriginal(t, reopened, donor, original)
	assertNativeOriginal(t, reopened, control.ID, 1, controlBytes)
}

func TestSelectedImageMergeScanAndBackupCheckPhysicalContent(t *testing.T) {
	f, rep, donor, original := selectedImageFixture(t)
	root, err := f.s.Root.LibraryPath(f.library)
	if err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(root, filepath.FromSlash(rep.RelativePath))
	changed := pngBytes(t, 110)
	if err = os.WriteFile(name, changed, 0600); err != nil {
		t.Fatal(err)
	}
	// A manual inventory may skip the same size and mtime. Give the external
	// edit an unmistakably new timestamp even on a coarse filesystem.
	later := rep.ContentMtime.Add(2 * time.Minute)
	if err = os.Chtimes(name, later, later); err != nil {
		t.Fatal(err)
	}
	job := f.scan("external-edit-in-merge")
	if job.State != "partial" || job.Changed != 0 || len(job.Issues) != 1 || job.Issues[0].Code != "merge_original_changed" {
		t.Fatal("scan adopted different content as the reviewed original", job)
	}
	var current model.Asset
	decode(t, f.json("GET", "/assets/"+rep.ID, "", nil, 200), &current)
	if current.Hash != rep.Hash || current.ContentVersion != rep.ContentVersion {
		t.Fatal("scan lost the expected original digest or version")
	}
	checkPhysicalOriginal(t, f, donor, original)
	if err = os.WriteFile(name, original, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Chtimes(name, *rep.ContentMtime, *rep.ContentMtime); err != nil {
		t.Fatal(err)
	}
	checkPhysicalOriginal(t, f, rep, original)
	donorPath, err := f.s.Root.AbsolutePath("library-trash/" + f.library + "/" + donor.ID + "/" + donor.FileName)
	if err != nil {
		t.Fatal(err)
	}
	damaged := bytes.Clone(original)
	damaged[len(damaged)-1] ^= 1
	if err = os.WriteFile(donorPath, damaged, 0600); err != nil {
		t.Fatal(err)
	}
	var b service.Backup
	decode(t, f.json("POST", "/backups", "damaged-member-checkpoint", struct{}{}, 201), &b)
	f.json("POST", "/backups/"+b.ID+"/export", "damaged-member-export", struct{}{}, 202)
	b = awaitBackup(t, f, b.ID)
	if b.AssetCount != 2 || b.ExportState != "failed" || b.ArchiveHash != "" {
		t.Fatal("archive accepted a same-length damaged member")
	}
}
