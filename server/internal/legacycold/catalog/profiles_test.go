package catalog

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/service"
)

func TestColdCatalogProfilesSubscriptionsTombstonesAndOfflineAvatar(t *testing.T) {
	f := newFixture(t)
	for _, id := range []string{"reviewed", "pending", "cleared", "recreated"} {
		f.author(id, id, "regular", 0)
	}
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	img.Set(1, 1, color.NRGBA{R: 90, G: 30, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	avatar := base64.StdEncoding.EncodeToString(buf.Bytes())
	raw := fmt.Sprintf(`{"authorId":"reviewed","revision":15,"status":"verified","nickname":"Public name","biography":"Owner biography",
		"remark":"","homepageUrl":"https://artist.example/profile","avatarUrl":"https://artist.example/portrait.png","avatarData":%q,
		"feedUrl":"https://artist.example/works.xml","trackUpdates":true,"autoRefresh":true,
		"evidenceUrls":["https://artist.example/evidence"],"lastCheckedAt":"2025-02-03T04:05:06Z",
		"xProfile":{"url":"https://x.com/synthetic_fixture","biography":"Original text","biographyZh":""},
		"updates":[{"id":"feed-entry","title":"Published work","url":"https://artist.example/work","publishedAt":"Mon, 03 Feb 2025 04:05:06 GMT"}],
		"candidate":{"nickname":"Alternative","homepageUrl":"https://artist.example/other","evidenceUrls":["https://artist.example/discovery"]},
		"extraInteger":9007199254740993}`, avatar)
	f.setting("creator_profile:reviewed", raw)
	f.setting("creator_profile_revision:reviewed", "15")
	f.setting("creator_profile:pending", `{"authorId":"pending","revision":1,"status":"candidate","nickname":"Not confirmed",
		"evidenceUrls":["https://artist.example/hint"],"biography":"draft"}`)
	f.setting("creator_profile:cleared", `{"authorId":"cleared","revision":4,"status":"empty","nickname":"","biography":"","remark":"",
		"avatarUrl":"","homepageUrl":"","evidenceUrls":[],"trackUpdates":false,"autoRefresh":false,"feedUrl":""}`)
	f.setting("creator_profile_revision:recreated", "7")
	f.setting("creator_profile_revision:deleted", "11")
	if err := f.run(); err != nil {
		t.Fatal(err)
	}
	s := &service.Service{DB: f.target}
	p := model.Principal{ID: "owner", Scopes: []string{"admin"}}
	profile, err := s.AuthorProfile(t.Context(), p, "reviewed")
	if err != nil {
		t.Fatal(err)
	}
	var x service.AuthorProfileBlock
	for _, block := range profile.Blocks {
		if block.Block == "x" {
			x = block
		}
	}
	if x.Avatar == nil || x.Values.BiographyZh == nil || *x.Values.BiographyZh != "" ||
		x.Values.BiographyOriginal == nil || *x.Values.BiographyOriginal != "Original text" {
		t.Fatalf("x profile=%+v", x)
	}
	body, _, err := s.AuthorProfileAvatar(t.Context(), x.Avatar.ID)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := png.Decode(bytes.NewReader(body))
	if err != nil || decoded.Bounds().Dx() != 2 {
		t.Fatalf("offline avatar decode=%v", err)
	}
	a := nativeAuthor(t, f, "reviewed")
	if a.Biography != "Owner biography" || a.Homepage != "https://artist.example/profile" || a.Name != "reviewed" || a.FieldStatus["biography"] != "confirmed" {
		t.Fatalf("core author=%+v", a)
	}
	sub, err := s.AuthorUpdates(t.Context(), "reviewed")
	if err != nil || !sub.TrackUpdates || sub.FeedURL != "https://artist.example/works.xml" || sub.CheckedAt == nil ||
		len(sub.Items) != 1 || sub.Items[0].PublishedAt == nil || sub.Items[0].ID != "feed-entry" {
		t.Fatalf("subscription=%+v err=%v", sub, err)
	}
	native := nativeAuthor(t, f, "pending")
	if native.Biography != "" || native.FieldStatus["biography"] != "candidate" {
		t.Fatalf("candidate published=%+v", native)
	}
	f.expect("1", "SELECT EXISTS(SELECT 1 FROM author_profile_candidates WHERE author_id='pending' AND state='pending')")
	f.expect("candidate", "SELECT json_extract(data,'$.values.identityStatus') FROM author_profile_blocks WHERE author_id='pending' AND block='accounts'")
	f.expect("text", "SELECT json_type(data,'$.values.summary') FROM author_profile_blocks WHERE author_id='cleared' AND block='basic'")
	f.expect("", "SELECT json_extract(data,'$.values.summary') FROM author_profile_blocks WHERE author_id='cleared' AND block='basic'")
	f.expect("7", "SELECT revision FROM author_profile_blocks WHERE author_id='recreated' AND block='basic'")
	f.expect("11", "SELECT revision FROM author_tombstones WHERE id='deleted'")
	f.expect("0", "SELECT count(*) FROM author_tombstones WHERE id='recreated'")
	f.expect("9007199254740993", "SELECT json_extract(original,'$.value.extraInteger') FROM migration_relation_origins WHERE kind='cold26/author-profile' AND json_extract(original,'$.authorId')='reviewed'")
	f.expect(avatar, "SELECT json_extract(original,'$.value.avatarData') FROM migration_relation_origins WHERE kind='cold26/author-profile' AND json_extract(original,'$.authorId')='reviewed'")
	archive, err := s.AuthorProfileArchive(t.Context(), "reviewed", stableID(sampleSource, "profile", "reviewed"))
	if err != nil || !strings.Contains(string(archive.OriginalImport), "9007199254740993") {
		t.Fatalf("native archive=%+v err=%v", archive, err)
	}
}
