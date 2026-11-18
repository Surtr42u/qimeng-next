package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"qimeng.local/next/server/internal/model"
)

func TestUploadInspectionRejectsChangedBytesAndCancellationAtCommit(t *testing.T) {
	if !syncPlatformSupported {
		t.Skip("this platform uses the serialized inspection path")
	}
	for _, change := range []string{"rewrite", "cancel"} {
		t.Run(change, func(t *testing.T) {
			ctx := context.Background()
			s, err := New(Config{DataDir: t.TempDir(), AdminToken: "synthetic-upload-inspection-owner-credential", ReserveBytes: 1})
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			result := testMutation(t, s, "inspection-library", func(w *Write) (any, int, error) {
				l, err := s.CreateLibrary(ctx, w, "Inspection", "regular")
				return l, 201, err
			})
			var library model.Library
			if err = json.Unmarshal(result.Body, &library); err != nil {
				t.Fatal(err)
			}
			u := stagedUpload(t, s, library.ID, 81, "pending.png")
			owner := model.Principal{ID: "owner", Scopes: []string{"admin"}}
			checked, err := s.prepareUploadInspection(ctx, owner, u.ID)
			if err != nil || checked == nil || !checked.probed {
				t.Fatal("missing pinned inspection", err)
			}
			defer checked.close()
			want := "content_mismatch"
			if change == "rewrite" {
				name := uploadPath(u.ID)
				f, err := s.Root.OpenFile(name, os.O_RDWR, 0600)
				if err != nil {
					t.Fatal(err)
				}
				var b [1]byte
				if _, err = f.ReadAt(b[:], u.Size-1); err == nil {
					b[0] ^= 0xff
					_, err = f.WriteAt(b[:], u.Size-1)
				}
				closeErr := f.Close()
				if err != nil || closeErr != nil {
					t.Fatal(err, closeErr)
				}
				if err = s.Root.Chtimes(name, checked.info.ModTime(), checked.info.ModTime()); err != nil {
					t.Fatal(err)
				}
			} else {
				testMutation(t, s, "cancel-inspected-upload", func(w *Write) (any, int, error) {
					return nil, 204, s.CancelUpload(ctx, w, u.ID)
				})
				want = "upload_cancelled"
			}
			_, err = s.Run(ctx, owner, "consume-inspected-upload", "consume-verified-source", nil, func(w *Write) (any, int, error) {
				out, err := s.completeUpload(ctx, w, u.ID, checked, nil)
				return out, 200, err
			})
			var problem *Error
			if !errors.As(err, &problem) || problem.Code != want {
				t.Fatal("stale inspection accepted or misclassified", err)
			}
			var assets, intents int
			if err = s.DB.DB.QueryRow("SELECT (SELECT count(*) FROM assets),(SELECT count(*) FROM file_intents)").Scan(&assets, &intents); err != nil || assets != 0 || intents != 0 {
				t.Fatal("rejected evidence published an asset or intent", assets, intents, err)
			}
		})
	}
}

func TestUploadInspectionDoesNotPublishReservedDuplicateBytes(t *testing.T) {
	ctx := context.Background()
	s, err := New(Config{DataDir: t.TempDir(), AdminToken: "synthetic-reserved-upload-owner-credential", ReserveBytes: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	result := testMutation(t, s, "reserved-content-library", func(w *Write) (any, int, error) {
		library, err := s.CreateLibrary(ctx, w, "Synthetic", "regular")
		return library, 201, err
	})
	var library model.Library
	if err = json.Unmarshal(result.Body, &library); err != nil {
		t.Fatal(err)
	}
	a := stagedUpload(t, s, library.ID, 43, "first.png")
	b := stagedUpload(t, s, library.ID, 43, "other-name.png")
	if err = s.saveIntent(ctx, fileIntent{ID: ID(), Kind: "ingest", State: "prepared", UploadID: a.ID,
		After: model.Asset{ID: ID(), LibraryID: library.ID, RelativePath: a.FileName, Hash: a.Hash, Size: a.Size}}); err != nil {
		t.Fatal(err)
	}
	owner := model.Principal{ID: "owner", Scopes: []string{"admin"}}
	_, err = s.RunUploadCompletion(ctx, owner, b.ID, "reserved-duplicate", "same-content-retry", nil)
	var problem *Error
	if !errors.As(err, &problem) || problem.Code != "publication_pending" {
		t.Fatal("a second upload claimed identical bytes during external IO", err)
	}
	var rows int
	if err = s.DB.DB.QueryRow("SELECT count(*) FROM assets").Scan(&rows); err != nil || rows != 0 {
		t.Fatal(err, rows)
	}
}

func TestUploadInspectionCommitRejectsReboundSourceDespiteVerifiedTarget(t *testing.T) {
	if !syncPlatformSupported {
		t.Skip("native object identity is unavailable")
	}
	ctx := context.Background()
	s, err := New(Config{DataDir: t.TempDir(), AdminToken: "synthetic-reserved-original-owner-credential", ReserveBytes: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r := testMutation(t, s, "rebound-source-library", func(w *Write) (any, int, error) {
		lib, e := s.CreateLibrary(ctx, w, "Rebind", "regular")
		return lib, 201, e
	})
	var lib model.Library
	if err = json.Unmarshal(r.Body, &lib); err != nil {
		t.Fatal(err)
	}
	u := stagedUpload(t, s, lib.ID, 56, "new.png")
	owner := model.Principal{ID: "owner", Scopes: []string{"admin"}}
	write := &Write{s: s, ctx: ctx, Principal: owner, operation: "rebound", key: "source-object-key", inputHash: digest(nil)}
	j, err := s.prepareFile(ctx, write, fileIntent{ID: ID(), Kind: "ingest", Source: uploadPath(u.ID), UploadID: u.ID,
		Target: "libraries/" + lib.ID + "/new.png", After: model.Asset{ID: ID(), LibraryID: lib.ID,
			RelativePath: "new.png", Size: u.Size, Hash: u.Hash, AuthorIDs: []string{"unknown"}}})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.applyFile(ctx, &j); err != nil {
		t.Fatal(err)
	}
	if err = s.Root.Remove(j.Source); err != nil {
		t.Fatal(err)
	}
	replacement, err := s.Root.OpenFile(j.Source, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = replacement.Write(syntheticPNG(t, 56))
	closeErr := replacement.Close()
	if err != nil || closeErr != nil {
		t.Fatal(err, closeErr)
	}
	// The bytes and native stamp for the installed target are still correct.
	verified, err := s.verifiedUploadTarget(ctx, j)
	if err != nil {
		t.Fatal(err)
	}
	err = s.validateReservedUpload(ctx, owner, &j, verified)
	var problem *Error
	if !errors.As(err, &problem) || problem.Code != "original_object_changed" {
		t.Fatal("rebound source was accepted with an otherwise valid target", err)
	}
	var assets, pending int
	if err = s.DB.DB.QueryRow("SELECT (SELECT count(*) FROM assets),(SELECT count(*) FROM file_intents WHERE state='prepared')").Scan(&assets, &pending); err != nil || assets != 0 || pending != 1 {
		t.Fatal("failed publication lost its recovery evidence", assets, pending, err)
	}
}
