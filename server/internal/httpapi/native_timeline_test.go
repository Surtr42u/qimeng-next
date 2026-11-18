package httpapi_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/service"
)

func TestNativeTimelineRequiresChosenTargetAndBothContentVersions(t *testing.T) {
	low, high := makeNativeVideo(t, "80x60"), makeNativeVideo(t, "160x120")
	source, target := setup(t), setup(t)
	original := *source.complete(nativeVideoUpload(t, source, "camera.mp4", low, nil)).Asset
	chosen := *target.complete(nativeVideoUpload(t, target, "camera.mp4", low, nil)).Asset
	root, err := target.s.Root.LibraryPath(target.library)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "unselected.mp4"), low, 0600); err != nil {
		t.Fatal(err)
	}
	if scanned := target.scan("native-identical-different-identity", 45*time.Second); scanned.Added != 1 {
		t.Fatal("identically encoded file was not independently indexed", scanned)
	}
	var assets model.Page[model.Asset]
	decode(t, target.json("GET", "/assets", "", nil, 200), &assets)
	var other model.Asset
	for _, a := range assets.Items {
		if a.ID != chosen.ID && a.Hash == chosen.Hash {
			other = a
		}
	}
	if other.ID == "" || original.ID == chosen.ID {
		t.Fatal("fixture requires explicit independent identities")
	}
	secret := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{'t'}, 32))
	source.json("POST", "/device/credentials", "native-mark-credential", service.DeviceCredentialInput{
		DeviceID: "mark-phone", Label: "Timeline fixture", CredentialSecret: secret,
	}, 201)
	makeMark := func(key, label string) service.TimelineMark {
		var m service.TimelineMark
		decode(t, source.json("POST", "/assets/"+original.ID+"/timeline", key, map[string]any{
			"contentVersion": original.ContentVersion, "positionMs": 350, "label": label, "color": "#336699",
		}, 201), &m)
		return m
	}
	selection := func(mark service.TimelineMark) service.NativePublish {
		return service.NativePublish{
			TargetDeviceID: "mark-phone", TargetInstanceID: target.s.InstanceID,
			Items: []service.NativeSelection{{Kind: "timelineMark", SourceObjectID: mark.ID, ExpectedRevision: mark.Revision,
				NativeVideoBinding: service.NativeVideoBinding{SourceAssetID: original.ID, SourceContentVersion: original.ContentVersion,
					TargetAssetID: chosen.ID, TargetContentVersion: chosen.ContentVersion}}},
		}
	}
	deliver := func(mark service.TimelineMark, key string) service.NativePlan {
		t.Helper()
		var task service.DeviceApplyTask
		decode(t, source.json("POST", "/device/native-deliveries", key, selection(mark), 201), &task)
		base := "/device/apply-tasks/" + task.ID
		if res := source.request("POST", base+"/claim", key+"-claim", secret, []byte(`{"expectedRevision":1}`), nil); res.Code != 200 {
			t.Fatal(res.Code, res.Body.String())
		}
		res := source.request("GET", base+"/segments/nativeSelection", "", secret, nil, nil)
		seg := task.Segments[0]
		if res.Code != 200 || fmt.Sprintf("%x", sha256.Sum256(res.Body.Bytes())) != seg.SHA256 || bytes.Contains(res.Body.Bytes(), low) {
			t.Fatal("selection did not deliver bounded mark metadata", res.Code)
		}
		query := url.Values{"sourceInstanceId": {source.s.InstanceID}, "targetInstanceId": {target.s.InstanceID}, "packageId": {seg.PackageID}, "conflictPolicy": {"keep"}}
		out := target.request("POST", "/native-deliveries/imports?"+query.Encode(), key+"-stage", testSecret, res.Body.Bytes(), map[string]string{"Content-Type": "application/octet-stream"})
		if out.Code != 200 {
			t.Fatal(out.Code, out.Body.String())
		}
		var plan service.NativePlan
		decode(t, out, &plan)
		return plan
	}
	commit := func(plan service.NativePlan, key string) service.NativePlan {
		t.Helper()
		var result service.NativePlan
		decode(t, target.json("POST", "/native-deliveries/imports/"+plan.ID+"/commit", key, map[string]string{"fingerprint": plan.Fingerprint}, 200), &result)
		return result
	}
	marks := func(id string) []service.TimelineMark {
		var page model.Page[service.TimelineMark]
		decode(t, target.json("GET", "/assets/"+id+"/timeline", "", nil, 200), &page)
		return page.Items
	}
	first := makeMark("native-source-marker", "Opening")
	var published service.DeviceApplyTask
	decode(t, source.json("POST", "/device/native-deliveries", "native-mark-delivery", selection(first), 201), &published)
	func() {
		ctx := context.Background()
		owner, err := source.s.Authenticate(ctx, testSecret)
		if err != nil {
			t.Fatal(err)
		}
		in := selection(first)
		for range 2 {
			proof, err := source.s.InspectNativePublish(ctx, owner, in)
			if err != nil || proof == nil {
				t.Fatal("could not hold a genuine video verification slot", err)
			}
			defer proof.Close()
		}
		source.json("POST", "/device/native-deliveries", "native-new-while-busy", in, 429)
		var replay service.DeviceApplyTask
		decode(t, source.json("POST", "/device/native-deliveries", "native-mark-delivery", in, 201), &replay)
		if replay.ID != published.ID || replay.Segments[0].PackageID != published.Segments[0].PackageID {
			t.Fatal("completed publication lost its original receipt", replay)
		}
		in.TTLSeconds = 600
		source.json("POST", "/device/native-deliveries", "native-mark-delivery", in, 409)
	}()
	plan := deliver(first, "native-mark-delivery")
	done := commit(plan, "native-mark-applied")
	retry := commit(deliver(first, "native-mark-other-transport"), "native-mark-replayed")
	if done.State != "applied" || len(done.Items) != 1 || done.Items[0].SourceObjectID != first.ID || done.Items[0].ContentSHA256 != original.Hash ||
		done.Items[0].SourceAssetID != original.ID || done.Items[0].TargetAssetID != chosen.ID || done.Items[0].TargetContentVersion != chosen.ContentVersion ||
		done.Items[0].ReceiptID == "" || done.Items[0].ReceiptID != retry.Items[0].ReceiptID {
		t.Fatal("unexpected mark identity or receipt", done, retry)
	}
	applied := marks(chosen.ID)
	if len(applied) != 1 || applied[0].ID != done.Items[0].TargetObjectID || applied[0].PositionMs != first.PositionMs || applied[0].ContentVersion != chosen.ContentVersion || len(marks(other.ID)) != 0 {
		t.Fatal("mark attached to the wrong video/version", applied)
	}
	external := deliver(makeMark("native-external-mark", "Same name"), "native-before-external-write")
	live := filepath.Join(root, filepath.FromSlash(chosen.RelativePath))
	originalInfo, err := os.Stat(live)
	if err != nil {
		t.Fatal(err)
	}
	changed := bytes.Clone(low)
	changed[len(changed)-1] ^= 1
	if err = os.WriteFile(live, changed, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Chtimes(live, originalInfo.ModTime(), originalInfo.ModTime()); err != nil {
		t.Fatal(err)
	}
	sameName := commit(external, "native-equal-size-mtime-conflict")
	if sameName.State != "partial" || sameName.Items[0].Code != "target_content_changed" || len(marks(chosen.ID)) != 1 || len(marks(other.ID)) != 0 {
		t.Fatal("unchanged catalog metadata concealed different original bytes", sameName)
	}
	if err = os.WriteFile(live, low, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Chtimes(live, originalInfo.ModTime(), originalInfo.ModTime()); err != nil {
		t.Fatal(err)
	}
	late := deliver(makeMark("native-new-mark", "Later"), "native-before-replacement")
	replaced := target.complete(nativeVideoUpload(t, target, "camera-hd.mp4", high, &chosen)).Asset
	if replaced == nil || replaced.ID != chosen.ID || replaced.ContentVersion == chosen.ContentVersion {
		t.Fatal("fixture failed to replace the target video")
	}
	blocked := commit(late, "native-mark-conflict")
	applied = marks(chosen.ID)
	if blocked.State != "partial" || blocked.Items[0].Code != "target_content_changed" || len(applied) != 1 ||
		applied[0].ContentVersion != chosen.ContentVersion || len(marks(other.ID)) != 0 {
		t.Fatal("old mark crossed a replacement or an identical second asset", blocked, applied)
	}
}
