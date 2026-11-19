package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/store"
)

// Pause the installed target's first verified read, while the actual publisher
// has yielded the writer. No timing or file size determines the interleaving.
type installedReadContext struct {
	context.Context
	target  string
	reading chan struct{}
	proceed chan struct{}
	once    sync.Once
}

func (c *installedReadContext) Err() error {
	if _, err := os.Stat(c.target); err == nil {
		c.once.Do(func() {
			close(c.reading)
			select {
			case <-c.proceed:
			case <-c.Context.Done():
			}
		})
	}
	return c.Context.Err()
}

func publicationResult[T any](t *testing.T, r Result) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(r.Body, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func requirePublicationError(t *testing.T, err error, code string) {
	t.Helper()
	var app *Error
	if !errors.As(err, &app) || app.Code != code {
		t.Fatalf("got %v, want %s", err, code)
	}
}

func changeDuringPublicationRead(t *testing.T, s *Service, u model.Upload, device model.Principal, op, key string, change func()) error {
	t.Helper()
	root, err := s.Root.LibraryPath(u.LibraryID)
	if err != nil {
		t.Fatal(err)
	}
	deadline, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	gate := &installedReadContext{Context: deadline, target: filepath.Join(root, u.FileName), reading: make(chan struct{}), proceed: make(chan struct{})}
	finished := make(chan error, 1)
	go func() {
		_, err := s.completeInspectedUpload(gate, device, u.ID, op, key, nil)
		finished <- err
	}()
	select {
	case <-gate.reading:
	case err := <-finished:
		t.Fatal("publisher ended before installing its target", err)
	case <-deadline.Done():
		t.Fatal("publisher did not reach target verification")
	}
	change()
	close(gate.proceed)
	return <-finished
}

func assertCancelledPublication(t *testing.T, s *Service, j fileIntent, target string, owner model.Principal) {
	t.Helper()
	ctx := context.Background()
	_, err := s.RunUploadCompletion(ctx, owner, j.UploadID, j.Operation, j.Key, nil)
	requirePublicationError(t, err, "upload_cancelled")
	preserved, err := s.Upload(ctx, owner, j.UploadID)
	if err != nil || preserved.State != "cancelled" || preserved.AssetID != "" {
		t.Fatal("lost the terminal cancellation", preserved.State, err)
	}
	if _, err = s.Root.Lstat(j.Target); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("cancelled target remained visible to the scanner", err)
	}
	name := "libraries/" + j.After.LibraryID + "/.qimeng-internal/publications/" + j.ID + ".cancelled"
	if err = s.verifyFileIdentity(name, target); err != nil {
		t.Fatal(err)
	}
	if err = s.verify(name, j.After.Hash, j.After.Size); err != nil {
		t.Fatal(err)
	}
}

func assertResumedPublication(t *testing.T, s *Service, j fileIntent, target string, requester model.Principal) {
	t.Helper()
	ctx := context.Background()
	first, err := s.RunUploadCompletion(ctx, requester, j.UploadID, j.Operation, j.Key, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := publicationResult[model.UploadResult](t, first)
	if got.Asset == nil || got.Asset.ID != j.After.ID || got.Upload.AssetID != j.After.ID || got.Upload.State != "ingested" || got.Upload.ErrorCode != "" {
		t.Fatal("resumed upload returned an incorrect asset or pending receipt")
	}
	replayed, err := s.RunUploadCompletion(ctx, requester, j.UploadID, j.Operation, j.Key, nil)
	if err != nil || !bytes.Equal(first.Body, replayed.Body) {
		t.Fatal("same key could not replay the resumed receipt", err)
	}
	var count int
	if err = s.DB.DB.QueryRow("SELECT count(*) FROM assets WHERE library_id=?", j.After.LibraryID).Scan(&count); err != nil || count != 1 {
		t.Fatal("resumed library has the wrong asset count", count, err)
	}
	asset, err := s.Asset(ctx, j.After.ID)
	if err != nil || asset.ContentVersion != 1 || asset.RelativePath != j.After.RelativePath || asset.Hash != j.After.Hash {
		t.Fatal("catalog differs from the accepted original", err)
	}
	if err = s.verifyFileIdentity(j.Target, target); err != nil {
		t.Fatal(err)
	}
	if err = s.verify(j.Target, j.After.Hash, j.After.Size); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Root.Stat(j.Source); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("committed upload retained its staging name", err)
	}
}

func TestUploadInspectionRetainsPublicationAcrossPolicyChangeAndRestart(t *testing.T) {
	if !syncPlatformSupported {
		t.Skip("requires native file identity and change stamps")
	}
	for _, hold := range []string{"disabled", "revoked", "legacy", "cancelled-batch", "cancelled-upload"} {
		t.Run(hold, func(t *testing.T) {
			ctx := context.Background()
			config := Config{DataDir: t.TempDir(), AdminToken: "synthetic-publication-restart-test-token", ReserveBytes: 1}
			s, err := New(config)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if s != nil {
					s.Close()
				}
			})
			newLibrary := func(name string) model.Library {
				t.Helper()
				return publicationResult[model.Library](t, testMutation(t, s, "create-"+name, func(w *Write) (any, int, error) {
					v, err := s.CreateLibrary(ctx, w, name, "regular")
					return v, 201, err
				}))
			}
			library := newLibrary("Source")
			setEnabled := func(enabled bool, label string) {
				t.Helper()
				library = publicationResult[model.Library](t, testMutation(t, s, label, func(w *Write) (any, int, error) {
					v, err := s.UpdateLibrary(ctx, w, library.ID, LibraryUpdate{ExpectedRevision: library.Revision, Enabled: &enabled})
					return v, 200, err
				}))
			}
			device := publicationResult[model.Principal](t, testMutation(t, s, "issue-device", func(w *Write) (any, int, error) {
				v, err := s.CreateSession(ctx, w, "Synthetic device", "", nil)
				return v, 201, err
			}))
			owner := model.Principal{ID: "owner", Scopes: []string{"admin"}}
			u := stagedUpload(t, s, library.ID, 81, "reserved.png")
			op := "POST /api/v1/uploads/" + u.ID + "/complete"
			const key = "publish-reserved-upload"
			code := "library_disabled"
			err = changeDuringPublicationRead(t, s, u, device, op, key, func() {
				if hold == "revoked" {
					testMutation(t, s, "revoke-publisher", func(w *Write) (any, int, error) {
						return nil, 204, s.Revoke(ctx, w, device.ID)
					})
					code = "revoked"
				} else {
					setEnabled(false, "disable-during-publication")
				}
			})
			requirePublicationError(t, err, code)
			intentID := digest([]byte("publish:" + u.ID))[:32]
			j, err := store.Get[fileIntent](ctx, s.DB.DB, "SELECT data FROM file_intents WHERE id=?", intentID)
			if err != nil || j.State != "prepared" || j.PrincipalID != device.ID {
				t.Fatal("interrupted publication lost its prepared intent", err)
			}
			target, err := s.managedFileIdentity(j.Target)
			if err != nil {
				t.Fatal(err)
			}
			if hold == "legacy" {
				if _, err = s.DB.DB.Exec(`UPDATE file_intents SET data=json_remove(data,'$.PrincipalID','$.Operation','$.Key','$.InputHash','$.principalGeneration','$.sourceObject') WHERE id=?`, intentID); err != nil {
					t.Fatal(err)
				}
			} else if hold == "cancelled-batch" {
				// Model a persisted cancellation from before the pending-intent guard.
				batch, err := s.Batch(ctx, u.BatchID)
				if err != nil {
					t.Fatal(err)
				}
				batch.State = "cancelled"
				if err = store.SaveBatch(ctx, s.DB.DB, batch); err != nil {
					t.Fatal(err)
				}
			} else if hold == "cancelled-upload" {
				u.State = "cancelled"
				if err = store.SaveUpload(ctx, s.DB.DB, u); err != nil {
					t.Fatal(err)
				}
			}
			if err = s.Close(); err != nil {
				t.Fatal(err)
			}
			s, err = New(config)
			if err != nil {
				t.Fatal("pending input prevented startup", err)
			}
			var assets, receipts int
			if err = s.DB.DB.QueryRow(`SELECT (SELECT count(*) FROM assets),(SELECT count(*) FROM idempotency WHERE operation=? AND key=?)`, op, key).Scan(&assets, &receipts); err != nil || assets != 0 || receipts != 0 {
				t.Fatal("restart published the held input or issued a success receipt", assets, receipts, err)
			}
			if err = s.verifyFileIdentity(j.Source, j.SourceObject); err != nil {
				t.Fatal(err)
			}
			if err = s.verify(j.Source, u.Hash, u.Size); err != nil {
				t.Fatal(err)
			}
			other := newLibrary("Independent")
			independent := stagedUpload(t, s, other.ID, 83, "independent.png")
			if _, err = s.RunUploadCompletion(ctx, owner, independent.ID, "complete-independent", "independent-upload", nil); err != nil {
				t.Fatal("another library could not publish after restart", err)
			}
			backup := func() error {
				_, err := s.Run(ctx, owner, "backup", "after-policy-change", nil, func(w *Write) (any, int, error) {
					v, err := s.CreateBackup(ctx, w)
					return v, 201, err
				})
				return err
			}
			if hold == "cancelled-batch" || hold == "cancelled-upload" {
				assertCancelledPublication(t, s, j, target, owner)
			} else {
				_, err = s.RunUploadCompletion(ctx, device, u.ID, op, key, nil)
				requirePublicationError(t, err, code)
				requirePublicationError(t, backup(), "publication_pending")
				if err = s.verifyFileIdentity(j.Target, target); err != nil {
					t.Fatal(err)
				}
				requester := device
				if hold != "revoked" {
					setEnabled(true, "reenable-library")
				}
				if hold != "disabled" {
					requester = owner
				}
				assertResumedPublication(t, s, j, target, requester)
			}
			if err = backup(); err != nil {
				t.Fatal("settled original still held the global backup reservation", err)
			}
		})
	}
}
