package httpapi_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"testing"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/service"
)

func TestNativeSelectiveDeliveryUsesTwoInstancesAndStableObjectReceipts(t *testing.T) {
	source, target := setup(t), setup(t)
	secret := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{'n'}, 32))
	source.json("POST", "/device/credentials", "native-device-secret", service.DeviceCredentialInput{
		DeviceID: "native-phone", Label: "Synthetic phone", CredentialSecret: secret,
	}, 201)
	author := profileAuthor(t, source, "Native author")
	authorPath := "/authors/" + author.ID + "/profile/basic"
	source.json("PATCH", authorPath, "native-initial-profile", map[string]any{"expectedRevision": 0, "values": map[string]string{
		"publicName": "First confirmed name", "note": "private-source-note",
	}}, 200)
	list := importList(t, source, "native-source-text", "native.txt", "1  Native author\n作品\nnative-image.png\n", 0)
	u, _ := target.upload("native-image.png", 67, nil)
	asset := target.complete(u).Asset
	items := []service.NativeSelection{
		{Kind: "authorList", SourceObjectID: list.List.ID, ExpectedRevision: 1},
		{Kind: "authorProfileBlock", SourceObjectID: author.ID, Block: "basic", ExpectedRevision: 1},
	}
	publish := func(key string) service.DeviceApplyTask {
		t.Helper()
		var result service.DeviceApplyTask
		decode(t, source.json("POST", "/device/native-deliveries", key, service.NativePublish{
			TargetDeviceID: "native-phone", TargetInstanceID: target.s.InstanceID, Items: items,
		}, 201), &result)
		if result.ProtocolVersion != 2 || len(result.Segments) != 1 || result.Segments[0].Kind != "nativeSelection" ||
			result.Segments[0].SourceInstanceID != source.s.InstanceID || result.Segments[0].TargetInstanceID != target.s.InstanceID {
			t.Fatal("unbound native package", result)
		}
		return result
	}
	download := func(task service.DeviceApplyTask, key string) []byte {
		t.Helper()
		base := "/device/apply-tasks/" + task.ID
		if w := source.request("POST", base+"/claim", key, secret, []byte(`{"expectedRevision":1}`), nil); w.Code != 200 {
			t.Fatal("claim", w.Code, w.Body.String())
		}
		raw := []byte{}
		for _, part := range []string{"bytes=0-36", "bytes=37-"} {
			w := source.request("GET", base+"/segments/nativeSelection", "", secret, nil, map[string]string{"Range": part})
			if w.Code != 206 {
				t.Fatal("range", w.Code, w.Body.String())
			}
			raw = append(raw, w.Body.Bytes()...)
		}
		s := task.Segments[0]
		if int64(len(raw)) != s.Bytes || fmt.Sprintf("%x", sha256.Sum256(raw)) != s.SHA256 || s.PackageID != s.SHA256 || bytes.Contains(raw, []byte("private-source-note")) {
			t.Fatal("invalid or overbroad native selection")
		}
		return raw
	}
	stage := func(task service.DeviceApplyTask, raw []byte, policy, key string, status int) service.NativePlan {
		t.Helper()
		s := task.Segments[0]
		query := url.Values{"packageId": {s.PackageID}, "targetInstanceId": {s.TargetInstanceID}, "sourceInstanceId": {s.SourceInstanceID}, "conflictPolicy": {policy}}
		w := target.request("POST", "/native-deliveries/imports?"+query.Encode(), key, testSecret, raw, map[string]string{"Content-Type": "application/octet-stream"})
		if w.Code != status {
			t.Fatal("stage", w.Code, w.Body.String())
		}
		var p service.NativePlan
		if w.Code == 200 {
			decode(t, w, &p)
		}
		return p
	}
	commit := func(plan service.NativePlan, key string) service.NativePlan {
		t.Helper()
		var result service.NativePlan
		decode(t, target.json("POST", "/native-deliveries/imports/"+plan.ID+"/commit", key, map[string]string{"fingerprint": plan.Fingerprint}, 200), &result)
		return result
	}
	task := publish("native-first-transport")
	raw := download(task, "native-first-claim")
	stage(task, append(slices.Clone(raw), ' '), "keep", "native-wrong-digest", 409)
	plan := stage(task, raw, "keep", "native-initial-stage", 200)
	if plan.State != "prepared" || len(plan.Items) != 2 {
		t.Fatal(plan)
	}
	target.json("POST", "/native-deliveries/imports/"+plan.ID+"/commit", "native-stale-approval", map[string]string{"fingerprint": strings.Repeat("a", 64)}, 409)
	applied := commit(plan, "native-commit-first")
	if applied.State != "applied" || applied.Items[0].Kind != "authorProfileBlock" || applied.Items[0].State != "applied" || applied.Items[1].State != "applied" ||
		applied.Items[0].ReceiptID == "" || applied.Items[0].ReceiptID == applied.Items[1].ReceiptID {
		t.Fatal(applied)
	}
	remoteReport := []byte(fmt.Sprintf(`{"expectedRevision":2,"state":"applied","results":[{"kind":"nativeSelection","state":"applied","sha256":%q,"receiptId":%q}]}`, task.Segments[0].SHA256, applied.ID))
	if w := source.request("POST", "/device/apply-tasks/"+task.ID+"/result", "native-report-to-source", secret, remoteReport, nil); w.Code != 200 {
		t.Fatal("device result did not bind to the received package", w.Code, w.Body.String())
	}
	targetAuthor := applied.Items[0].TargetObjectID
	var stored model.Asset
	decode(t, target.json("GET", "/assets/"+asset.ID, "", nil, 200), &stored)
	if !slices.Contains(stored.AuthorIDs, targetAuthor) {
		t.Fatal("TXT declarations did not reach the receiver asset", stored.AuthorIDs, targetAuthor)
	}
	second := publish("native-independent-transport")
	if second.ID == task.ID || second.Segments[0].PackageID != task.Segments[0].PackageID {
		t.Fatal("delivery identity follows the transport task")
	}
	replayed := stage(second, download(second, "native-second-claim"), "keep", "native-stage-again", 200)
	retry := commit(plan, "native-response-lost")
	var reloaded service.NativePlan
	decode(t, target.json("GET", "/native-deliveries/imports/"+plan.ID, "", nil, 200), &reloaded)
	for _, p := range []service.NativePlan{replayed, retry, reloaded} {
		if p.ID != applied.ID || p.State != applied.State || p.Items[0].ReceiptID != applied.Items[0].ReceiptID || p.Items[1].ReceiptID != applied.Items[1].ReceiptID {
			t.Fatal("lost terminal receipt", p)
		}
	}
	source.json("PATCH", authorPath, "native-change-source", map[string]any{"expectedRevision": 1, "values": map[string]string{"publicName": "Later source name"}}, 200)
	items[1].ExpectedRevision = 2
	third := publish("native-updated-transport")
	update := stage(third, download(third, "native-third-claim"), "replace", "native-update-plan", 200)
	targetPath := "/authors/" + targetAuthor + "/profile"
	target.json("PATCH", targetPath+"/basic", "native-target-clear", map[string]any{"expectedRevision": 1, "values": map[string]string{"publicName": ""}}, 200)
	partial := commit(update, "native-late-conflict")
	if partial.State != "partial" || partial.Items[0].Code != "target_changed" || partial.Items[1].ReceiptID != applied.Items[1].ReceiptID {
		t.Fatal("late manual decision or independent item receipt lost", partial)
	}
	source.json("POST", "/device/native-deliveries", "native-no-unversioned-asset", map[string]any{
		"targetDeviceId": "native-phone", "targetInstanceId": target.s.InstanceID,
		"items": []map[string]any{{"kind": "asset", "sourceObjectId": asset.ID, "expectedRevision": asset.Revision}},
	}, 400)
	var profile service.AuthorProfile
	decode(t, target.json("GET", targetPath, "", nil, 200), &profile)
	for _, b := range profile.Blocks {
		if b.Block == "basic" && (b.Source != "human" || b.Values.PublicName == nil || *b.Values.PublicName != "" || b.Values.Note != nil) {
			t.Fatal("native replay overwrote local clear or introduced private notes", b)
		}
	}
}
