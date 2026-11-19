package httpapi_test

import (
	"fmt"
	"testing"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/service"
)

func writeNativeProfileField(f *fixture, authorID, block, field, value string, revision int64) service.NativeSelection {
	f.t.Helper()
	key := fmt.Sprintf("native-field-%s-%s-%d", authorID, block, revision)
	var saved service.AuthorProfileBlock
	decode(f.t, f.json("PATCH", "/authors/"+authorID+"/profile/"+block, key, map[string]any{
		"expectedRevision": revision, "values": map[string]string{field: value},
	}, 200), &saved)
	return service.NativeSelection{Kind: "authorProfileBlock", SourceObjectID: saved.AuthorID, Block: saved.Block, ExpectedRevision: saved.Revision}
}

func (p nativeDeliveryPair) deliverBlock(key string, selection service.NativeSelection, planState, itemState, code string) service.NativePlan {
	t := p.target.t
	t.Helper()
	task := p.publish(key, selection)
	raw := p.download(task, key+"-claim")
	plan := p.commit(p.stage(task, raw, "replace", key+"-stage", 200), key+"-commit")
	if plan.State != planState || len(plan.Items) != 1 {
		t.Fatal("unexpected terminal plan", plan)
	}
	item := plan.Items[0]
	if item.Kind != selection.Kind || item.SourceObjectID != selection.SourceObjectID || item.Block != selection.Block ||
		item.SourceRevision != selection.ExpectedRevision || item.State != itemState || item.Code != code || item.ReceiptID == "" {
		t.Fatal("unexpected block receipt", item)
	}
	p.report(task, plan, key+"-result")
	return plan
}

func readNativeProfileBlocks(f *fixture, id string) map[string]service.AuthorProfileBlock {
	f.t.Helper()
	var profile service.AuthorProfile
	decode(f.t, f.json("GET", "/authors/"+id+"/profile", "", nil, 200), &profile)
	blocks := make(map[string]service.AuthorProfileBlock, len(profile.Blocks))
	for _, b := range profile.Blocks {
		blocks[b.Block] = b
	}
	return blocks
}

func TestNativeProfileBlocksKeepAuthorIdentityAfterRestore(t *testing.T) {
	source, target := setup(t), setup(t)
	pair := pairNativeReceiver(source, target, "")
	author := profileAuthor(t, source, "Native restore author")
	writeNativeProfileField(source, author.ID, "basic", "publicName", "Draft", 0)
	basic := writeNativeProfileField(source, author.ID, "basic", "publicName", "Public name", 1)
	x := writeNativeProfileField(source, author.ID, "x", "biographyOriginal", "Public biography", 0)
	before := pair.deliverBlock("native-before-restore", basic, "applied", "applied", "")
	first := before.Items[0]
	id := first.TargetObjectID
	if id == "" || id == author.ID || first.SourceRevision != 2 || first.TargetRevision == nil || *first.TargetRevision != 1 {
		t.Fatal("first block did not establish a local author", first)
	}
	upload, original := target.upload("native-restore.png", 53, nil)
	asset := target.complete(upload).Asset
	restored := nativeCheckpointCopy(t, target, "profile-receiver", 1)
	assertNativeOriginal(t, restored, asset.ID, asset.ContentVersion, original)
	var history service.NativePlan
	decode(t, restored.json("GET", "/native-deliveries/imports/"+before.ID, "", nil, 200), &history)
	if len(history.Items) != 1 || history.Items[0].ReceiptID != first.ReceiptID || history.Items[0].TargetObjectID != id {
		t.Fatal("archive lost the original author's receipt", history)
	}

	pair = pairNativeReceiver(source, restored, pair.credential.ID)
	after := pair.deliverBlock("native-after-restore", x, "applied", "applied", "")
	second := after.Items[0]
	if after.TargetInstanceID == before.TargetInstanceID || second.TargetObjectID != id || second.SourceRevision != 1 ||
		second.TargetRevision == nil || *second.TargetRevision != 1 || second.ReceiptID == first.ReceiptID {
		t.Fatal("new block lost its independent revision or the restored identity", after)
	}
	replay := pair.deliverBlock("native-after-another-transport", x, "applied", "applied", "")
	if replay.ID != after.ID || replay.Items[0].ReceiptID != second.ReceiptID || replay.Items[0].TargetObjectID != id {
		t.Fatal("new transport changed the restored receiver's receipt", replay)
	}
	confirmed := pair.deliverBlock("native-restored-basic", basic, "applied", "preserved", "already_current")
	if confirmed.Items[0].TargetObjectID != id || confirmed.Items[0].TargetRevision == nil || *confirmed.Items[0].TargetRevision != 1 {
		t.Fatal("first block lost its binding across the restore", confirmed)
	}
	blocks := readNativeProfileBlocks(restored, id)
	gotBasic, gotX := blocks["basic"], blocks["x"]
	if gotBasic.AuthorID != id || gotBasic.Revision != 1 || gotBasic.Values.PublicName == nil || *gotBasic.Values.PublicName != "Public name" ||
		gotX.AuthorID != id || gotX.Revision != 1 || gotX.Values.BiographyOriginal == nil || *gotX.Values.BiographyOriginal != "Public biography" {
		t.Fatal("profile blocks after replay", blocks)
	}
	var authors model.Page[model.Author]
	decode(t, restored.json("GET", "/authors", "", nil, 200), &authors)
	if authors.TotalMatched == nil || *authors.TotalMatched != 2 {
		t.Fatal("expected only the placeholder and the restored author", authors)
	}

	x = writeNativeProfileField(source, author.ID, "x", "biographyOriginal", "Delayed update", 1)
	delayed := pair.publish("native-delayed-x", x)
	pending := pair.stage(delayed, pair.download(delayed, "native-delayed-claim"), "replace", "native-delayed-stage", 200)
	x = writeNativeProfileField(source, author.ID, "x", "biographyOriginal", "Current biography", 2)
	latest := pair.deliverBlock("native-current-x", x, "applied", "applied", "")
	stale := pair.commit(pending, "native-delayed-commit")
	if latest.Items[0].TargetObjectID != id || latest.Items[0].TargetRevision == nil || *latest.Items[0].TargetRevision != 2 ||
		stale.State != "partial" || len(stale.Items) != 1 || stale.Items[0].Code != "source_revision_conflict" || stale.Items[0].ReceiptID == "" {
		t.Fatal("a delayed revision competed with the current block", latest, stale)
	}
	pair.report(delayed, stale, "native-delayed-result")

	writeNativeProfileField(restored, id, "basic", "publicName", "", 1)
	writeNativeProfileField(restored, id, "accounts", "identityStatus", "verified", 0)
	restored.json("PUT", "/authors/"+id+"/updates", "native-enable-subscription", map[string]any{
		"expectedRevision": 0, "trackUpdates": true, "feedUrl": "https://example.org/works.rss",
	}, 200)
	basic = writeNativeProfileField(source, author.ID, "basic", "publicName", "New source name", 2)
	x = writeNativeProfileField(source, author.ID, "x", "biographyOriginal", "New source biography", 3)
	for _, selection := range []service.NativeSelection{basic, x} {
		result := pair.deliverBlock("native-guard-"+selection.Block, selection, "partial", "preserved", "human_protected")
		if result.Items[0].TargetObjectID != id {
			t.Fatal("protection applied to another author", result)
		}
	}
	blocks = readNativeProfileBlocks(restored, id)
	gotBasic, gotX = blocks["basic"], blocks["x"]
	var subscription service.AuthorUpdates
	decode(t, restored.json("GET", "/authors/"+id+"/updates", "", nil, 200), &subscription)
	if gotBasic.Source != "human" || gotBasic.Revision != 2 || gotBasic.Values.PublicName == nil || *gotBasic.Values.PublicName != "" ||
		gotX.Revision != 2 || gotX.Values.BiographyOriginal == nil || *gotX.Values.BiographyOriginal != "Current biography" || !subscription.TrackUpdates {
		t.Fatal("delivery changed a local clear or active subscription", blocks, subscription)
	}

	// The sender's own restore preserves its author ID and display name, but
	// establishes a different source instance for the receiver.
	otherSource := nativeCheckpointCopy(t, source, "independent-sender", 0)
	otherPair := pairNativeReceiver(otherSource, restored, "")
	otherPair.deliverBlock("native-another-source", basic, "partial", "conflict", "identity_conflict")
}

func TestNativeProfileBlocksRejectSplitAuthorBindings(t *testing.T) {
	source, target := setup(t), setup(t)
	pair := pairNativeReceiver(source, target, "")
	author := profileAuthor(t, source, "Split history author")
	basic := writeNativeProfileField(source, author.ID, "basic", "publicName", "First value", 0)
	x := writeNativeProfileField(source, author.ID, "x", "biographyOriginal", "Prior biography", 0)
	first := pair.deliverBlock("native-split-basic", basic, "applied", "applied", "")
	pair.deliverBlock("native-split-x", x, "applied", "applied", "")
	other := profileAuthor(t, target, "Other historical target")
	// Earlier receivers could bind the next block to a second local author
	// after a restore and a display rename. Represent that historical row.
	if _, err := target.s.DB.DB.ExecContext(t.Context(), `UPDATE native_delivery_bindings SET target_id=?
		WHERE source_instance=? AND kind=? AND object_id=? AND block=?`, other.ID, source.s.InstanceID, x.Kind, author.ID, x.Block); err != nil {
		t.Fatal(err)
	}
	basic = writeNativeProfileField(source, author.ID, "basic", "publicName", "Updated value", 1)
	accounts := writeNativeProfileField(source, author.ID, "accounts", "identityStatus", "verified", 0)
	for _, selection := range []service.NativeSelection{basic, accounts} {
		result := pair.deliverBlock("native-ambiguous-"+selection.Block, selection, "partial", "conflict", "identity_conflict")
		if result.Items[0].TargetObjectID != "" {
			t.Fatal("ambiguous history selected one of the two targets", result)
		}
	}
	original := readNativeProfileBlocks(target, first.Items[0].TargetObjectID)
	untouched := readNativeProfileBlocks(target, other.ID)
	if original["basic"].Revision != 1 || original["accounts"].Revision != 0 || untouched["basic"].Revision != 0 || untouched["accounts"].Revision != 0 {
		t.Fatal("split history wrote to a local author", original, untouched)
	}
}

func TestNativeProfileBlocksPreserveNamesakesAndDeletedAuthors(t *testing.T) {
	source, target := setup(t), setup(t)
	pair := pairNativeReceiver(source, target, "")
	author := profileAuthor(t, source, "Bound author")
	basic := writeNativeProfileField(source, author.ID, "basic", "publicName", "Bound name", 0)
	first := pair.deliverBlock("native-bind-first-author", basic, "applied", "applied", "")
	id := first.Items[0].TargetObjectID

	other := profileAuthor(t, source, "Local namesake")
	namesake := profileAuthor(t, target, other.Name)
	otherBasic := writeNativeProfileField(source, other.ID, "basic", "publicName", "Another name", 0)
	pair.deliverBlock("native-unbound-namesake", otherBasic, "partial", "conflict", "identity_conflict")
	if readNativeProfileBlocks(target, namesake.ID)["basic"].Revision != 0 || readNativeProfileBlocks(target, id)["basic"].Revision != 1 {
		t.Fatal("delivery changed a different author from the same source or a local namesake")
	}

	var local model.Author
	decode(t, target.json("GET", "/authors/"+id, "", nil, 200), &local)
	target.json("DELETE", "/authors/"+id+"?expectedRevision="+fmt.Sprint(local.Revision), "native-delete-author", struct{}{}, 204)
	x := writeNativeProfileField(source, author.ID, "x", "biographyOriginal", "Later biography", 0)
	pair.deliverBlock("native-keep-deleted-author", x, "partial", "preserved", "human_protected")
	target.json("GET", "/authors/"+id, "", nil, 404)
}
