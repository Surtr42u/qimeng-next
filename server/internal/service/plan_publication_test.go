package service

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/store"
)

func publicationLibrary(t *testing.T, s *Service, name string) model.Library {
	t.Helper()
	return publicationResult[model.Library](t, testMutation(t, s, "library-"+name, func(w *Write) (any, int, error) {
		lib, err := s.CreateLibrary(context.Background(), w, name, "regular")
		return lib, 201, err
	}))
}

func stagedIntelligentPlan(t *testing.T, s *Service, library string) (model.Upload, model.Plan) {
	t.Helper()
	ctx := context.Background()
	const name, input = "selected.png", "selected-input"
	batch := publicationResult[model.Batch](t, testMutation(t, s, "planned-batch", func(w *Write) (any, int, error) {
		b, err := s.CreateBatch(ctx, w, model.Batch{LibraryID: library, Mode: "intelligent", Label: "Synthetic selection", MaxResearchCalls: 1})
		return b, 201, err
	}))
	testMutation(t, s, "seal-planned-input", func(w *Write) (any, int, error) {
		b, err := s.SetBatchInputs(ctx, w, batch.ID, model.BatchInputsSet{ExpectedManifestRevision: batch.ManifestRevision,
			Inputs: []model.IntakeInput{{ID: input, OriginalFileName: name}}})
		return b, 200, err
	})
	data := syntheticPNG(t, 71)
	started := publicationResult[model.Upload](t, testMutation(t, s, "receive-plan-upload", func(w *Write) (any, int, error) {
		u, err := s.CreateUpload(ctx, w, model.UploadInput{LibraryID: library, BatchID: batch.ID, InputID: input, OriginalFileName: name,
			Mode: "intelligent", FileName: name, MimeType: "image/png", Size: int64(len(data))})
		return u, 201, err
	}))
	testMutation(t, s, "append-planned-upload", func(w *Write) (any, int, error) {
		u, err := s.AppendUpload(ctx, w, started.ID, 0, data)
		return u, 200, err
	})
	owner := model.Principal{ID: "owner", Scopes: []string{"admin"}}
	result, err := s.RunUploadCompletion(ctx, owner, started.ID, "seal-planned-upload", "seal-accepted-bytes", nil)
	if err != nil {
		t.Fatal(err)
	}
	u := publicationResult[model.UploadResult](t, result).Upload
	if u.State != "staged" || u.Hash != digest(data) {
		t.Fatal("upload was not staged")
	}
	batch, err = s.Batch(ctx, batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	plan := publicationResult[model.Plan](t, testMutation(t, s, "prepare-smart-publication", func(w *Write) (any, int, error) {
		p, err := s.PreparePlan(ctx, w, batch.ID, model.PlanCreate{ExpectedRevision: batch.Revision,
			Items: []model.PlanInput{{UploadID: u.ID, Family: "Selected work"}}})
		return p, 201, err
	}))
	return u, plan
}

func TestPlanPreflightLetsIndependentTagWriteAndCancelledRequestCanRetry(t *testing.T) {
	if !syncPlatformSupported {
		t.Skip("native file identity is unavailable")
	}
	s, err := New(Config{DataDir: t.TempDir(), AdminToken: "synthetic-plan-preflight-owner-credential", ReserveBytes: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	library := publicationLibrary(t, s, "Independent tags")
	owner := model.Principal{ID: "owner", Scopes: []string{"admin"}}
	ctx := context.Background()
	other := stagedUpload(t, s, library.ID, 83, "independent.png")
	completed, err := s.RunUploadCompletion(ctx, owner, other.ID, "publish-independent", "safe-independent-publication", nil)
	if err != nil {
		t.Fatal(err)
	}
	asset := publicationResult[model.UploadResult](t, completed).Asset
	u, plan := stagedIntelligentPlan(t, s, library.ID)
	deadline, stop := context.WithTimeout(ctx, 15*time.Second)
	defer stop()
	request, cancel := context.WithCancel(deadline)
	defer cancel()
	started := make(chan struct{})
	var once sync.Once
	s.Root.SetUploadLibraryLookup(func(id string) (string, error) {
		if id == u.ID {
			once.Do(func() {
				close(started)
				<-request.Done()
			})
			return library.ID, nil
		}
		return "", nil
	})
	const key = "commit-original-plan-after-cancel"
	failed := make(chan error, 1)
	go func() {
		_, err := s.CommitPlan(request, owner, plan.BatchID, plan.ID, plan.Fingerprint, key)
		failed <- err
	}()
	select {
	case <-started:
	case err = <-failed:
		t.Fatal("commit exited before reading staging", err)
	case <-deadline.Done():
		t.Fatal("commit did not reach staging")
	}
	updated, err := s.Run(deadline, owner, "independent-tags", "write-independent-tags", nil, func(w *Write) (any, int, error) {
		a, err := s.UpdateTags(deadline, w, asset.ID, asset.TagsRevision, []string{"during preflight"})
		return a, 200, err
	})
	if err != nil || !slices.Equal(publicationResult[model.Asset](t, updated).Tags, []string{"during preflight"}) {
		t.Fatal("independent tags waited for staging IO", err)
	}
	cancel()
	if err = <-failed; !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled preflight returned", err)
	}
	planID := digest([]byte("publish:" + u.ID))[:32]
	var intents int
	if err = s.DB.DB.QueryRow("SELECT count(*) FROM file_intents WHERE id=?", planID).Scan(&intents); err != nil || intents != 0 {
		t.Fatal("cancelled preflight created a publication intent", intents, err)
	}
	if _, err = s.Root.Stat("libraries/" + library.ID + "/" + plan.Items[0].RelativePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("cancelled preflight published a target", err)
	}
	if err = s.verify(uploadPath(u.ID), u.Hash, u.Size); err != nil {
		t.Fatal("cancelled preflight lost accepted bytes", err)
	}
	type attempt struct {
		result Result
		err    error
	}
	retries := make(chan attempt, 2)
	for range 2 {
		go func() {
			r, err := s.CommitPlan(deadline, owner, plan.BatchID, plan.ID, plan.Fingerprint, key)
			retries <- attempt{r, err}
		}()
	}
	first, second := <-retries, <-retries
	if first.err != nil || second.err != nil || first.result.Status != 200 || second.result.Status != 200 || !bytes.Equal(first.result.Body, second.result.Body) {
		t.Fatal("same-key retries did not share the completed receipt", first.err, second.err)
	}
	got := publicationResult[model.BatchDetail](t, first.result)
	if got.Batch.State != "completed" || len(got.Items) != 1 || got.Items[0].Upload.AssetID == "" {
		t.Fatalf("retry did not publish the original input: %+v", got)
	}
	op := "commitPlan:" + plan.ID
	var receipts, publications int
	if err = s.DB.DB.QueryRow(`SELECT (SELECT count(*) FROM idempotency WHERE operation=? AND key=?),
		(SELECT count(*) FROM published_plan_items WHERE upload_id=?)`, op, key, u.ID).Scan(&receipts, &publications); err != nil || receipts != 1 || publications != 1 {
		t.Fatal("same key created repeated receipts or publications", receipts, publications, err)
	}
	receipt, err := s.Operation(ctx, owner, key, op)
	if err != nil || receipt["state"] != "committed" || receipt["status"] != 200 {
		t.Fatal("standard operation read lost the committed result", receipt, err)
	}
}

func TestPlanPublicationHoldsIntentWhenAuthorizationOrRevisionChanges(t *testing.T) {
	if !syncPlatformSupported {
		t.Skip("native file identity is unavailable")
	}
	for _, change := range []string{"revoked", "revision_conflict"} {
		t.Run(change, func(t *testing.T) {
			ctx := context.Background()
			config := Config{DataDir: t.TempDir(), AdminToken: "synthetic-plan-authorization-owner-key", ReserveBytes: 1}
			s, err := New(config)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if s != nil {
					s.Close()
				}
			}()
			library := publicationLibrary(t, s, "Planned originals")
			u, plan := stagedIntelligentPlan(t, s, library.ID)
			actor := publicationResult[model.Principal](t, testMutation(t, s, "plan-publication-credential", func(w *Write) (any, int, error) {
				p, err := s.CreateSession(ctx, w, "Synthetic planner", plan.BatchID, []string{"read", "ingest", "organize"})
				return p, 201, err
			}))
			root, err := s.Root.LibraryPath(library.ID)
			if err != nil {
				t.Fatal(err)
			}
			deadline, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			gate := &installedReadContext{Context: deadline, target: filepath.Join(root, plan.Items[0].RelativePath), reading: make(chan struct{}), proceed: make(chan struct{})}
			const key = "commit-plan-with-changing-grants"
			finished := make(chan error, 1)
			go func() {
				_, err := s.CommitPlan(gate, actor, plan.BatchID, plan.ID, plan.Fingerprint, key)
				finished <- err
			}()
			select {
			case <-gate.reading:
			case err = <-finished:
				t.Fatal("commit exited before verifying its installed target", err)
			case <-deadline.Done():
				t.Fatal("commit did not reach target verification")
			}
			owner := model.Principal{ID: "owner", Scopes: []string{"admin"}}
			_, err = s.Run(deadline, owner, "change-plan-authority", "change-before-plan-commit", nil, func(w *Write) (any, int, error) {
				if change == "revoked" {
					return nil, 204, s.Revoke(deadline, w, actor.ID)
				}
				b, err := s.SubmitResearch(deadline, w, plan.BatchID, model.Research{UploadID: u.ID, ExpectedRevision: plan.BatchRevision,
					ExpectedHash: u.Hash, Outcome: "unknown", Candidates: []model.ResearchCandidate{}, CallsUsed: 1})
				return b, 200, err
			})
			if err != nil {
				t.Fatal("independent writer could not change permission or batch revision", err)
			}
			close(gate.proceed)
			requirePublicationError(t, <-finished, change)
			if err = s.Close(); err != nil {
				t.Fatal(err)
			}
			s, err = New(config)
			if err != nil {
				t.Fatal("held plan stopped unrelated service recovery", err)
			}
			intent, err := store.Get[fileIntent](ctx, s.DB.DB, "SELECT data FROM file_intents WHERE id=?", digest([]byte("publish:"+u.ID))[:32])
			if err != nil || intent.State != "prepared" || intent.PrincipalID != actor.ID {
				t.Fatal("restart discarded the held publication evidence", err)
			}
			still, err := s.Upload(ctx, owner, u.ID)
			if err != nil || still.State != "staged" || still.AssetID != "" || still.ErrorCode != "publication_pending" {
				t.Fatal("restart published the held input", still, err)
			}
			if err = s.verify(intent.Source, u.Hash, u.Size); err != nil {
				t.Fatal("restart changed the held staging object", err)
			}
			if err = s.verify(intent.Target, u.Hash, u.Size); err != nil {
				t.Fatal("restart changed the held target object", err)
			}
			receipt, err := s.Operation(ctx, actor, key, "commitPlan:"+plan.ID)
			if err != nil || receipt["state"] != "not_recorded" {
				t.Fatal("held publication claimed a receipt", receipt, err)
			}
			other := publicationLibrary(t, s, "Unrelated library")
			candidate := stagedUpload(t, s, other.ID, 89, "unrelated.png")
			if _, err = s.RunUploadCompletion(ctx, owner, candidate.ID, "publish-other-library", "other-library-upload-completion", nil); err != nil {
				t.Fatal("held plan blocked another library", err)
			}
			if change == "revoked" {
				result, err := s.CommitPlan(ctx, owner, plan.BatchID, plan.ID, plan.Fingerprint, key)
				if err != nil || publicationResult[model.BatchDetail](t, result).Batch.State != "completed" {
					t.Fatal("authorized retry could not resume the exact reserved target", err)
				}
			}
		})
	}
}
