//go:build qimeng_legacy_import

package httpapi_test

import (
	"strings"
	"context"
	"encoding/base64"
	"encoding/json"
	"path/filepath"
	"slices"
	"testing"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/service"
	"qimeng.local/next/server/internal/store"
)

func portableProfile(t *testing.T, f *fixture, key string, a model.Author, fields map[string]any, avatar []byte) service.V2Import {
	t.Helper()
	author := map[string]any{"authorId": a.ID, "displayName": a.Name}
	if fields != nil {
		fields["authorId"] = a.ID
		profile, err := json.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
		author["profileJson"] = string(profile)
	}
	if len(avatar) > 0 {
		author["avatarData"] = base64.StdEncoding.EncodeToString(avatar)
	}
	raw, err := json.Marshal(map[string]any{"format": "qimeng_backup", "schemaVersion": 2, "appIdentifier": "synthetic.device",
		"data": map[string]any{"authors": []any{author}, "mediaFiles": []any{}, "viewEvents": []any{}}})
	if err != nil {
		t.Fatal(err)
	}
	return stageV2(t, f, key, raw)
}

func replacePlan(t *testing.T, f *fixture, in service.V2Import, key string) service.V2MigrationPlan {
	t.Helper()
	var plan service.V2MigrationPlan
	decode(t, f.json("POST", "/migration/v2/imports/"+in.ID+"/plans", key, map[string]string{"authorProfileConflict": "replace"}, 201), &plan)
	return plan
}

func applyProfilePlan(t *testing.T, f *fixture, plan service.V2MigrationPlan, key string, status int) service.V2MigrationPlan {
	t.Helper()
	response := f.json("POST", "/migration/v2/plans/"+plan.ID+"/commit", key, map[string]any{"fingerprint": plan.Fingerprint, "acceptSkipped": false}, status)
	if status == 200 {
		decode(t, response, &plan)
	}
	return plan
}

func TestV2ExplicitReplacementPreviewsAndBacksUpBeforeChangingOnlyImportedMaterial(t *testing.T) {
	f := setup(t)
	a := profileAuthor(t, f, "Device fixture")
	original := portableProfile(t, f, "device-profile-original", a, map[string]any{"status": "verified", "nickname": "Original", "biography": "Retain absent source field", "avatarUrl": "https://example.org/first.png", "xProfile": map[string]string{"url": "https://x.com/first", "biography": "First", "biographyZh": "旧译文"}}, pngBytes(t, 12))
	applyProfilePlan(t, f, planV2(t, f, original, "device-profile-first-plan"), "device-profile-first-apply", 200)
	updated := portableProfile(t, f, "device-profile-updated", a, map[string]any{"status": "verified", "nickname": "Updated", "avatarUrl": "https://example.org/second.png", "xProfile": map[string]string{"url": "https://x.com/second", "biography": "Second"}, "evidenceUrls": []string{"https://example.org/record"}}, pngBytes(t, 87))
	keep := planV2(t, f, updated, "device-profile-keep-plan")
	applyProfilePlan(t, f, keep, "device-profile-keep-apply", 200)
	plan := replacePlan(t, f, updated, "device-profile-replace-plan")
	if keep.AuthorProfileConflict != "keep" || plan.Fingerprint == keep.Fingerprint || plan.RequiresReview == nil || *plan.RequiresReview || plan.Counts["profileBlocksToReplace"] < 2 {
		t.Fatalf("different policy or confirmed differences missing: %+v", plan)
	}
	var page model.Page[service.V2MigrationAuthor]
	decode(t, f.json("GET", "/migration/v2/plans/"+plan.ID+"/authors", "", nil, 200), &page)
	var basic service.V2ProfileBlockPlan
	for _, b := range page.Items[0].ProfileBlocks {
		if b.Block == "basic" {
			basic = b
		}
	}
	if basic.Action != "replace" || basic.ExpectedRevision != 1 || basic.BeforeFingerprint == "" || !slices.Contains(basic.Fields, "publicName") ||
		basic.CurrentValues.PublicName == nil || *basic.CurrentValues.PublicName != "Original" || basic.IncomingValues.PublicName == nil || *basic.IncomingValues.PublicName != "Updated" {
		t.Fatalf("incomplete replacement preview: %+v", basic)
	}
	var profile service.AuthorProfile
	path := "/authors/" + a.ID + "/profile"
	decode(t, f.json("GET", path, "", nil, 200), &profile)
	oldAvatar := profile.Blocks[0].Avatar.ID
	if *profile.Blocks[1].Values.PublicName != "Original" {
		t.Fatal("default keep or preview modified an occupied block")
	}
	result := applyProfilePlan(t, f, plan, "device-profile-replace-apply", 200)
	if result.State != "committed" || result.CheckpointID == "" || result.Counts["profileBlocksReplaced"] < 2 {
		t.Fatalf("replacement outcome: %+v", result)
	}
	snapshot, err := store.OpenDatabase(filepath.Join(f.s.Config.DataDir, "backups", result.CheckpointID, "snapshot.sqlite"), store.SnapshotDatabase)
	if err != nil {
		t.Fatal(err)
	}
	var previous string
	err = snapshot.QueryRow("SELECT json_extract(data,'$.values.publicName') FROM author_profile_blocks WHERE author_id=? AND block='basic'", a.ID).Scan(&previous)
	snapshot.Close()
	if err != nil || previous != "Original" {
		t.Fatalf("checkpoint did not retain the exact pre-apply block: %q %v", previous, err)
	}
	// Each response must decode into a fresh value: an omitted JSON field does
	// not clear a pointer left in the destination by an earlier GET.
	var afterProfile service.AuthorProfile
	decode(t, f.json("GET", path, "", nil, 200), &afterProfile)
	x, afterBasic := afterProfile.Blocks[0], afterProfile.Blocks[1]
	if x.Avatar == nil || x.Avatar.ID == oldAvatar || x.Avatar.SourceURL != "https://example.org/second.png" {
		t.Fatal("the new source did not receive its own offline avatar")
	}
	if x.Values.BiographyZh != nil {
		t.Fatal("a new account retained the previous account's translation")
	}
	if afterBasic.Values.Summary == nil || *afterBasic.Values.Summary != "Retain absent source field" {
		t.Fatal("a field absent from the new source was erased")
	}
	if afterBasic.Values.PublicName == nil || *afterBasic.Values.PublicName != "Updated" {
		t.Fatal("the explicitly verified nickname was not applied")
	}
	replay := applyProfilePlan(t, f, plan, "device-profile-replace-apply", 200)
	if replay.CheckpointID != result.CheckpointID || replacePlan(t, f, updated, "device-profile-replan").Counts["profileBlocksToReplace"] != 0 {
		t.Fatal("replay or replan repeated the replacement")
	}
}

func TestV2ReplacementProtectsHumanClearPendingCandidatesAndSubscription(t *testing.T) {
	f := setup(t)
	a := profileAuthor(t, f, "Protected device fixture")
	path := "/authors/" + a.ID
	original := portableProfile(t, f, "device-protected-original", a, map[string]any{"status": "verified", "nickname": "Old", "homepageUrl": "https://example.org/old", "feedUrl": "https://example.org/feed"}, nil)
	applyProfilePlan(t, f, planV2(t, f, original, "device-protected-base"), "device-protected-install", 200)
	in := portableProfile(t, f, "device-protected-incoming", a, map[string]any{"status": "verified", "nickname": "Replacement", "homepageUrl": "https://example.org/new", "feedUrl": "https://example.org/other-feed", "evidenceUrls": []string{"https://example.org/proof"}}, nil)
	stale := replacePlan(t, f, in, "device-protected-stale")
	f.json("PATCH", path+"/profile/basic", "owner-explicitly-clears", map[string]any{"expectedRevision": 1, "values": map[string]string{"publicName": ""}}, 200)
	var checkpointCount, afterRejection int
	if err := f.s.DB.DB.QueryRow("SELECT COUNT(*) FROM backups").Scan(&checkpointCount); err != nil {
		t.Fatal(err)
	}
	applyProfilePlan(t, f, stale, "device-reject-stale-block", 409)
	decode(t, f.json("GET", "/migration/v2/plans/"+stale.ID, "", nil, 200), &stale)
	if err := f.s.DB.DB.QueryRow("SELECT COUNT(*) FROM backups").Scan(&afterRejection); err != nil {
		t.Fatal(err)
	}
	if stale.CheckpointID != "" || stale.State != "prepared" || afterRejection != checkpointCount {
		t.Fatal("a rejected plan checkpointed or wrote active blocks")
	}
	beforeSubscription := replacePlan(t, f, in, "device-before-subscribe")
	f.json("PUT", path+"/updates", "owner-subscribes-feed", map[string]any{"expectedRevision": 1, "trackUpdates": true, "feedUrl": "https://example.org/feed"}, 200)
	applyProfilePlan(t, f, beforeSubscription, "device-reject-stale-subscription", 409)
	var sub service.AuthorUpdates
	decode(t, f.json("GET", path+"/updates", "", nil, 200), &sub)
	final := replacePlan(t, f, in, "device-protected-final")
	if final.Counts["profileBlocksProtected"] != 2 {
		t.Fatalf("missing explicit human and subscription protections: %+v", final.Counts)
	}
	final = applyProfilePlan(t, f, final, "device-protected-apply", 200)
	if final.State != "partial" || final.Counts["profileBlocksReplaced"] != 0 || final.CheckpointID == "" {
		t.Fatalf("incorrect protected result: %+v", final)
	}
	var profile service.AuthorProfile
	decode(t, f.json("GET", path+"/profile", "", nil, 200), &profile)
	if *profile.Blocks[1].Values.PublicName != "" || profile.Blocks[1].Source != "human" || (*profile.Blocks[2].Values.Accounts)[0].URL != "https://example.org/old" {
		t.Fatal("protected clear or subscribed account changed")
	}
	var after service.AuthorUpdates
	decode(t, f.json("GET", path+"/updates", "", nil, 200), &after)
	if !after.TrackUpdates || after.FeedURL != sub.FeedURL || after.Revision != sub.Revision {
		t.Fatal("replace changed or enabled a subscription")
	}
	unverified := portableProfile(t, f, "device-candidate-source", a, map[string]any{"status": "candidate", "nickname": "Still pending", "evidenceUrls": []string{"https://example.org/candidate"}}, nil)
	candidate := replacePlan(t, f, unverified, "device-candidate-plan")
	if candidate.Counts["profileBlocksCandidateOnly"] == 0 || candidate.Counts["profileBlocksToReplace"] != 0 {
		t.Fatal("candidate offered as a verified field")
	}
	applied := applyProfilePlan(t, f, candidate, "device-candidate-apply", 200)
	if applied.State != "partial" || applied.Counts["profileCandidatesPreserved"] == 0 {
		t.Fatal("unconfirmed record was lost or promoted")
	}
	absent := portableProfile(t, f, "device-no-profile", a, nil, nil)
	noChange := replacePlan(t, f, absent, "device-absent-plan")
	applyProfilePlan(t, f, noChange, "device-absent-apply", 200)
	decode(t, f.json("GET", path+"/profile", "", nil, 200), &profile)
	if profile.Blocks[1].Revision != 2 || *profile.Blocks[1].Values.PublicName != "" {
		t.Fatal("unverified or missing source replaced an explicit human clear")
	}
}

func TestV2ImportedAuthorProfilesRestoreKnownFieldsWithoutOverwritingConfirmedBlocks(t *testing.T) {
	f := setup(t)
	author := profileAuthor(t, f, "Imported")
	base := "/authors/" + author.ID + "/profile"
	f.json("PATCH", base+"/basic", "import-keep-explicit-clear", map[string]any{"expectedRevision": 0, "values": map[string]string{"summary": "", "note": "本地私人备注"}}, 200)
	raw := []byte(`{"authorId":"legacy-author","revision":91,"nickname":"公开昵称","biography":"迁入简介","remark":"已退圈，仅有存档","avatarUrl":"https://example.org/avatar.png","homepageUrl":"https://example.org/author","feedUrl":"https://example.org/works","status":"verified","evidenceUrls":["https://example.org/proof"],"autoRefresh":true,"trackUpdates":true,"xProfile":{"url":"https://twitter.com/Example","biography":"Original","biographyZh":"译文"},"unknownInteger":9007199254740993}`)
	prepared, err := service.PrepareImportedAuthorProfile(context.Background(), "legacy-author", raw, pngBytes(t, 67))
	if err != nil {
		t.Fatal(err)
	}
	tx, err := f.s.DB.DB.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	imported, err := service.RestoreImportedAuthorProfile(context.Background(), tx, author.ID, author.Name, "synthetic-import-archive", prepared)
	if err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if len(imported.RestoredBlocks) != 2 || len(imported.CandidateBlocks) != 1 || !imported.AvatarRestored {
		t.Fatalf("unexpected import outcome %+v", imported)
	}
	var profile service.AuthorProfile
	decode(t, f.json("GET", base, "", nil, 200), &profile)
	if *profile.Blocks[1].Values.Summary != "" || *profile.Blocks[1].Values.Note != "本地私人备注" || profile.Blocks[1].Revision != 1 {
		t.Fatal("migration overwrote a confirmed empty field")
	}
	if profile.Blocks[0].Revision != 1 || *profile.Blocks[0].Values.BiographyZh != "译文" || profile.Blocks[0].Avatar.Origin != "import" {
		t.Fatal("known X fields or offline avatar were not restored")
	}
	if profile.Blocks[2].Values.Accounts == nil || (*profile.Blocks[2].Values.Accounts)[0].PublishingURL != "https://example.org/works" {
		t.Fatal("manual publication entry lost")
	}
	archived := f.json("GET", base+"/archives/synthetic-import-archive", "", nil, 200)
	if !strings.Contains(archived.Body.String(), `9007199254740993`) {
		t.Fatal("opaque imported number lost precision")
	}
	// A second import must not overwrite a deliberately cleared translation.
	f.json("PATCH", base+"/x", "import-clear-translation", map[string]any{"expectedRevision": 1, "values": map[string]string{"biographyZh": ""}}, 200)
	tx, err = f.s.DB.DB.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.RestoreImportedAuthorProfile(context.Background(), tx, author.ID, author.Name, "synthetic-second-archive", prepared)
	if err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	decode(t, f.json("GET", base, "", nil, 200), &profile)
	if profile.Blocks[0].Values.BiographyZh == nil || *profile.Blocks[0].Values.BiographyZh != "" {
		t.Fatal("explicit empty translation resurrected")
	}
	if _, err = service.PrepareImportedAuthorProfile(context.Background(), "different-author", raw, nil); err == nil {
		t.Fatal("mismatched imported identity accepted")
	}
	if _, err = service.PrepareImportedAuthorProfile(context.Background(), "legacy-author", raw, []byte("<svg/>")); err == nil {
		t.Fatal("invalid offline avatar passed preflight")
	}
}
