package service

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"strconv"
	"testing"
	"time"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/store"
)

func syntheticPNG(t *testing.T, v uint8) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{v, 10, 20, 255})
	var out bytes.Buffer
	if e := png.Encode(&out, img); e != nil {
		t.Fatal(e)
	}
	return out.Bytes()
}
func testMutation(t *testing.T, s *Service, operation string, fn func(*Write) (any, int, error)) Result {
	t.Helper()
	r, e := s.Run(context.Background(), model.Principal{ID: "owner", Scopes: []string{"admin"}}, operation, operation+"-receipt", []byte(operation), fn)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func stagedUpload(t *testing.T, s *Service, library string, shade uint8, name string) model.Upload {
	t.Helper()
	ctx := context.Background()
	data := syntheticPNG(t, shade)
	r := testMutation(t, s, "create-"+name, func(w *Write) (any, int, error) {
		u, e := s.CreateUpload(ctx, w, model.UploadInput{LibraryID: library, FileName: name, Size: int64(len(data)), MimeType: "image/png", Mode: "ordinary"})
		return u, 201, e
	})
	var u model.Upload
	if e := json.Unmarshal(r.Body, &u); e != nil {
		t.Fatal(e)
	}
	testMutation(t, s, "append-"+name, func(w *Write) (any, int, error) { v, e := s.AppendUpload(ctx, w, u.ID, 0, data); return v, 200, e })
	u.Offset = u.Size
	u.Hash = digest(data)
	u.State = "staged"
	if e := store.SaveUpload(ctx, s.DB.DB, u); e != nil {
		t.Fatal(e)
	}
	return u
}
func TestPublishedIntentRecoversSameAssetAndReceipt(t *testing.T) {
	for _, copyPublication := range []bool{false, true} {
		t.Run("copy="+strconv.FormatBool(copyPublication), func(t *testing.T) {
			ctx := context.Background()
			config := Config{DataDir: t.TempDir(), AdminToken: "synthetic-recovery-secret-0000000000000000", ReserveBytes: 1}
			s, e := New(config)
			if e != nil {
				t.Fatal(e)
			}
			r := testMutation(t, s, "library-create", func(w *Write) (any, int, error) {
				v, e := s.CreateLibrary(ctx, w, "Recovery", "regular")
				return v, 201, e
			})
			var l model.Library
			if e = json.Unmarshal(r.Body, &l); e != nil {
				t.Fatal(e)
			}
			u := stagedUpload(t, s, l.ID, 50, "recovery.png")
			now := time.Now().UTC()
			a := model.Asset{ID: digest([]byte("asset:" + u.ID))[:32], LibraryID: l.ID, FileName: u.FileName, OriginalFileName: u.FileName, RelativePath: u.FileName, MediaType: "image", MimeType: "image/png", Size: u.Size, Hash: u.Hash, Revision: 1, TagsRevision: 1, AuthorsRevision: 1, ContentVersion: 1, AuthorID: "unknown", AuthorIDs: []string{"unknown"}, AuthorStatus: "unknown", Tags: []string{}, Characters: []string{}, SourceIDs: []string{}, CharacterIDs: []string{}, AcquisitionSources: []string{}, CreatedAt: now, UpdatedAt: now, ProbeState: "unavailable"}
			a.ContentURL = "/api/v1/assets/" + a.ID + "/content"
			w := &Write{s: s, ctx: ctx, Principal: model.Principal{ID: "owner", Scopes: []string{"admin"}}, operation: "complete-crash", key: "crash-receipt-key", inputHash: digest([]byte("original-input"))}
			j, e := s.prepareFile(ctx, w, fileIntent{ID: "synthetic-publish-intent", Kind: "ingest", Source: uploadPath(u.ID), Target: assetPath(a), UploadID: u.ID, After: a, CopyPublication: copyPublication})
			if e != nil {
				t.Fatal(e)
			}
			// Stop after the real off-lock installation and its locked version
			// check, before the catalog transaction can publish a receipt.
			s.mu.Lock()
			e = s.applyReservedUpload(ctx, w, &j)
			s.mu.Unlock()
			if e != nil {
				t.Fatal(e)
			}
			if _, e = s.Run(ctx, w.Principal, "backup-during-publication", "backup-during-publication-key", nil, func(write *Write) (any, int, error) { v, e := s.CreateBackup(ctx, write); return v, 201, e }); e == nil {
				t.Fatal("backup claimed a consistent manifest while publication was prepared")
			}
			if e = s.Close(); e != nil {
				t.Fatal(e)
			}
			s, e = New(config)
			if e != nil {
				t.Fatal(e)
			}
			defer s.Close()
			got, e := s.Asset(ctx, a.ID)
			if e != nil {
				t.Fatal(e)
			}
			if got.Hash != u.Hash || got.ID != a.ID {
				t.Fatal("recovery changed content or identity")
			}
			restored, e := s.Upload(ctx, w.Principal, u.ID)
			if e != nil {
				t.Fatal(e)
			}
			if restored.State != "ingested" || restored.AssetID != a.ID {
				t.Fatal("upload did not reconcile")
			}
			result, e := s.Run(ctx, w.Principal, w.operation, w.key, []byte("original-input"), func(*Write) (any, int, error) { t.Fatal("recovered receipt was not replayed"); return nil, 0, nil })
			if e != nil {
				t.Fatal(e)
			}
			var completed model.UploadResult
			if e = json.Unmarshal(result.Body, &completed); e != nil {
				t.Fatal(e)
			}
			if completed.Asset.ID != a.ID {
				t.Fatal("wrong receipt asset")
			}
		})
	}
}
func TestExclusiveServiceOwnerAndSymlinkBoundary(t *testing.T) {
	config := Config{DataDir: t.TempDir(), AdminToken: "synthetic-exclusive-secret-00000000000000", ReserveBytes: 1}
	s, e := New(config)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	other, e := New(config)
	if e == nil {
		other.Close()
		t.Fatal("second service acquired same data root")
	}
	if e = s.safe("../outside"); e == nil {
		t.Fatal("traversal accepted")
	}
}

func TestDifferentPlanCannotReplacePersistedPublicationIntent(t *testing.T) {
	ctx := context.Background()
	s, e := New(Config{DataDir: t.TempDir(), AdminToken: "synthetic-intent-secret-00000000000000000", ReserveBytes: 1})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	original := &Write{s: s, ctx: ctx, Principal: model.Principal{ID: "owner", Scopes: []string{"admin"}}, operation: "commitPlanItem:first:item", key: "plan-fingerprint-key", inputHash: "hash"}
	intent := fileIntent{ID: "synthetic-plan-intent", Kind: "ingest", UploadID: "same-input", Target: "libraries/first.png"}
	if _, e = s.prepareFile(ctx, original, intent); e != nil {
		t.Fatal(e)
	}
	different := *original
	different.operation = "commitPlanItem:different:item"
	intent.Target = "libraries/second.png"
	if _, e = s.prepareFile(ctx, &different, intent); e == nil {
		t.Fatal("different plan adopted earlier publication intent")
	}
}
func TestRevokedAuthenticatedPrincipalCannotStartWrite(t *testing.T) {
	ctx := context.Background()
	s, e := New(Config{DataDir: t.TempDir(), AdminToken: "synthetic-revocation-secret-00000000000000", ReserveBytes: 1})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	r := testMutation(t, s, "device-session", func(w *Write) (any, int, error) {
		p, e := s.CreateSession(ctx, w, "Synthetic device", "", nil)
		return p, 201, e
	})
	var issued model.Principal
	if e = json.Unmarshal(r.Body, &issued); e != nil {
		t.Fatal(e)
	}
	authenticated, e := s.Authenticate(ctx, issued.Token)
	if e != nil {
		t.Fatal(e)
	}
	testMutation(t, s, "revoke-device", func(w *Write) (any, int, error) { return nil, 204, s.Revoke(ctx, w, issued.ID) })
	if _, e = s.Run(ctx, authenticated, "queued-write", "queued-write-key", nil, func(*Write) (any, int, error) {
		t.Fatal("revoked principal reached business mutation")
		return nil, 204, nil
	}); e == nil {
		t.Fatal("revocation was not rechecked at write boundary")
	}
}

func TestDeterministicResourceIdentityIsNamespacedByInstance(t *testing.T) {
	ctx := context.Background()
	ids := []string{}
	for index := 0; index < 2; index++ {
		s, e := New(Config{DataDir: t.TempDir(), AdminToken: "synthetic-same-owner-secret-00000000000000", ReserveBytes: 1})
		if e != nil {
			t.Fatal(e)
		}
		defer s.Close()
		p := model.Principal{ID: "owner", Scopes: []string{"admin"}}
		create := func(w *Write) (any, int, error) {
			v, e := s.CreateLibrary(ctx, w, "Same input", "regular")
			return v, 201, e
		}
		first, e := s.Run(ctx, p, "create-library", "same-owner-same-key", []byte("identical input"), create)
		if e != nil {
			t.Fatal(e)
		}
		var library model.Library
		if e = json.Unmarshal(first.Body, &library); e != nil {
			t.Fatal(e)
		}
		ids = append(ids, library.ID)
		again, e := s.Run(ctx, p, "create-library", "same-owner-same-key", []byte("identical input"), func(*Write) (any, int, error) { t.Fatal("committed operation was not replayed"); return nil, 0, nil })
		if e != nil || !bytes.Equal(first.Body, again.Body) {
			t.Fatal("same-instance replay changed")
		}
	}
	if ids[0] == ids[1] {
		t.Fatal("different instances assigned the same deterministic identity")
	}
}
