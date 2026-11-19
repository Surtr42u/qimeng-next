package service

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"qimeng.local/next/server/internal/model"
)

func TestScanKnownFilesUpdateDatesContentAndOnlyChangedTaxonomy(t *testing.T) {
	ctx := context.Background()
	s, run, root := preparedScanFixture(t)
	touched := existingScanOriginal(t, s, run, root, "touched.png")
	changed := existingScanOriginal(t, s, run, root, "rewritten.png")
	moved := existingScanOriginal(t, s, run, root, "before.png")
	const renamed = "NieR Automata 2B 1.png"
	if err := os.Rename(filepath.Join(root, moved.FileName), filepath.Join(root, renamed)); err != nil {
		t.Fatal(err)
	}
	newBytes := syntheticPNG(t, 91)
	if err := os.WriteFile(filepath.Join(root, changed.FileName), newBytes, 0600); err != nil {
		t.Fatal(err)
	}
	stamp := time.Unix(1700000000, 123456700)
	for _, a := range []model.Asset{touched, changed} {
		if err := os.Chtimes(filepath.Join(root, a.FileName), stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	// A missing optional catalog field in an older record must stay absent.
	// BEFORE INSERT observes a membership replay even if its UPSERT affects zero rows.
	_, err := s.DB.DB.Exec(`UPDATE asset_records SET data=json_remove(data,'$.acquisitionSources');
		CREATE TABLE scan_file_writes(kind TEXT NOT NULL);
		CREATE TRIGGER scan_author_insert BEFORE INSERT ON asset_authors BEGIN INSERT INTO scan_file_writes VALUES('author'); END;
		CREATE TRIGGER scan_tag_insert BEFORE INSERT ON asset_tags BEGIN INSERT INTO scan_file_writes VALUES('tag'); END;
		CREATE TRIGGER scan_taxonomy_save BEFORE INSERT ON asset_taxonomy_state BEGIN INSERT INTO scan_file_writes VALUES('taxonomy'); END;
		CREATE TRIGGER scan_version_insert AFTER INSERT ON asset_content_versions BEGIN INSERT INTO scan_file_writes VALUES('version'); END;
		CREATE TRIGGER scan_location_update AFTER UPDATE ON asset_locations BEGIN INSERT INTO scan_file_writes VALUES('location'); END`)
	if err != nil {
		t.Fatal(err)
	}
	s.scanLibrary(ctx, run.id, scanTargets{inventory: true})
	job, err := s.Scan(ctx, run.id)
	if err != nil || job.State != "completed" || job.Processed != 3 || job.Changed != 2 || job.Moved != 1 {
		t.Fatal(job, err)
	}
	var memberships, taxonomy, versions, locations int
	err = s.DB.DB.QueryRow(`SELECT count(*) FILTER(WHERE kind IN ('author','tag')),count(*) FILTER(WHERE kind='taxonomy'),
		count(*) FILTER(WHERE kind='version'),count(*) FILTER(WHERE kind='location') FROM scan_file_writes`).
		Scan(&memberships, &taxonomy, &versions, &locations)
	if err != nil || memberships != 0 || taxonomy != 1 || versions != 1 || locations != 3 {
		t.Fatal("membership attempts, taxonomy saves, new versions, location updates", memberships, taxonomy, versions, locations, err)
	}
	for _, original := range []model.Asset{touched, changed, moved} {
		a, err := s.Asset(ctx, original.ID)
		if err != nil || a.Revision != original.Revision+1 || a.AuthorsRevision != original.AuthorsRevision ||
			a.Note != original.Note || !slices.Equal(a.AuthorIDs, original.AuthorIDs) || !slices.Equal(a.Tags, original.Tags) {
			t.Fatal("catalog fields after scan", a, err)
		}
		name, hash, version := original.FileName, original.Hash, original.ContentVersion
		if a.ID == changed.ID {
			hash, version = digest(newBytes), version+1
		}
		if a.ID == moved.ID {
			name = renamed
			if !a.TaxonomyDerived || a.Source != "尼尔 机械纪元" || !slices.Equal(a.Characters, []string{"2B"}) {
				t.Fatal("renamed file lost its new taxonomy", a)
			}
		}
		if a.RelativePath != name || a.Hash != hash || a.ContentVersion != version ||
			a.ContentURL != "/api/v1/assets/"+a.ID+"/content" || a.ThumbnailURL != "/api/v1/assets/"+a.ID+"/thumbnail" {
			t.Fatal("published original or display URLs", a)
		}
		info, err := os.Stat(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		var fileDate int64
		var updated string
		var absent bool
		err = s.DB.DB.QueryRow(`SELECT file_date,updated_at,json_type(data,'$.acquisitionSources') IS NULL FROM asset_records WHERE id=?`,
			a.ID).Scan(&fileDate, &updated, &absent)
		if err != nil || !absent || a.ContentMtime == nil || !a.ContentMtime.Equal(info.ModTime()) ||
			a.FileDate == nil || !a.FileDate.Equal(info.ModTime()) || a.FileDate.Location() != time.UTC ||
			fileDate != info.ModTime().UnixMilli() || !a.UpdatedAt.After(original.UpdatedAt) || updated != dateKey(a.UpdatedAt) {
			t.Fatal("date index, precise file facts or prior field absence", a, fileDate, updated, absent, err)
		}
	}
	f, _, err := s.Thumbnail(ctx, changed.ID, changed.ContentVersion)
	if f != nil {
		f.Close()
	}
	var stale *Error
	if !errors.As(err, &stale) || stale.Code != "content_changed" {
		t.Fatal("thumbnail request for the retired original", err)
	}
}

func TestScanKnownFileClearsAnUnavailableThumbnail(t *testing.T) {
	ctx := context.Background()
	t.Setenv("PATH", t.TempDir())
	t.Setenv("QIMENG_FFMPEG", "")
	t.Setenv("QIMENG_FFPROBE", "")
	s, run, root := preparedScanFixture(t)
	const name = "pixel.webp"
	pixel, err := base64.StdEncoding.DecodeString("UklGRiIAAABXRUJQVlA4IBYAAAAwAQCdASoBAAEAAUAmJaQAA3AA/v89WAAAAA==")
	if err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(root, name)
	if err = os.WriteFile(filename, pixel, 0600); err != nil {
		t.Fatal(err)
	}
	if err = s.scanFile(ctx, run, name, scanSignature{}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.DB.Exec(`UPDATE asset_records SET data=json_set(data,'$.thumbnailUrl','/old-preview')`); err != nil {
		t.Fatal(err)
	}
	stamp := time.Unix(1700000000, 0)
	if err = os.Chtimes(filename, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	s.scanLibrary(ctx, run.id, scanTargets{inventory: true})
	var removed bool
	if err = s.DB.DB.QueryRow(`SELECT json_type(data,'$.thumbnailUrl') IS NULL FROM assets WHERE file_name=?`, name).Scan(&removed); err != nil || !removed {
		t.Fatal("old thumbnail remained published after reprobe", removed, err)
	}
}

func TestScanBatchMoveMatchesAuthorAfterAnIndependentEdit(t *testing.T) {
	ctx := context.Background()
	s, run, root := preparedScanFixture(t)
	asset := existingScanOriginal(t, s, run, root, "original.png")
	const renamed = "match.png"
	testMutation(t, s, "scan-move-rule", func(w *Write) (any, int, error) {
		var revision int64
		result, err := s.ImportAuthorList(ctx, w, AuthorListInput{Name: "fixture.txt",
			Content: "1  Scanner author\n作品\n" + renamed + "\n", ExpectedRevision: &revision})
		return result, 200, err
	})
	if err := os.Rename(filepath.Join(root, asset.FileName), filepath.Join(root, renamed)); err != nil {
		t.Fatal(err)
	}
	run.targets = scanTargets{inventory: true}
	if err := s.scanFile(ctx, run, renamed, scanSignature{}); err != nil || len(run.prepared) != 1 {
		t.Fatal("read original before catalog edit", err)
	}
	note := "edited after file inspection"
	testMutation(t, s, "scan-staged-note", func(w *Write) (any, int, error) {
		current, err := s.UpdateAsset(ctx, w, asset.ID, AssetUpdate{ExpectedRevision: asset.Revision, Note: &note})
		return current, 200, err
	})
	if err := s.flushPreparedScans(ctx, run); err != nil {
		t.Fatal(err)
	}
	current, err := s.Asset(ctx, asset.ID)
	if err != nil || current.FileName != renamed || current.ContentVersion != asset.ContentVersion || current.Note != note ||
		current.AuthorName != "Scanner author" || current.AuthorsRevision != asset.AuthorsRevision+1 ||
		current.Revision != asset.Revision+3 || !slices.Equal(current.Tags, asset.Tags) {
		t.Fatal("move, late edit and author declaration", current, err)
	}
}
