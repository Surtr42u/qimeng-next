package httpapi_test

import (
	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/service"
	"testing"
)

func TestSubscriptionRequiresOwnerVerifiedIdentityAndPublicFeed(t *testing.T) {
	f := setup(t)
	var author model.Author
	decode(t, f.json("POST", "/authors", "create-subscription-author", map[string]string{"name": "Explicit works"}, 201), &author)
	base := "/authors/" + author.ID
	url := base + "/updates"
	var v service.AuthorUpdates
	decode(t, f.json("GET", url, "", nil, 200), &v)
	if v.Revision != 0 || v.TrackUpdates || len(v.Items) != 0 {
		t.Fatal("implicit subscription")
	}
	set := map[string]any{"expectedRevision": 0, "trackUpdates": true, "feedUrl": "https://example.org/works.rss"}
	f.json("PUT", url, "unverified-feed-subscription", set, 409)
	f.json("PATCH", base+"/profile/accounts", "verify-subscription-identity", map[string]any{"expectedRevision": 0, "values": map[string]string{"identityStatus": "verified"}}, 200)
	set["feedUrl"] = "https://127.0.0.1/private"
	f.json("PUT", url, "deny-local-works-feed", set, 400)
	set["feedUrl"] = "https://example.org/works.rss"
	decode(t, f.json("PUT", url, "save-verified-feed", set, 200), &v)
	if !v.TrackUpdates || v.CheckedAt != nil || len(v.Items) != 0 {
		t.Fatal("enabling a subscription invented or fetched entries")
	}
	f.json("POST", url+"/check", "stale-feed-check", map[string]int{"expectedRevision": 9}, 409)
	f.json("PATCH", base+"/profile/accounts", "unverify-subscription-identity", map[string]any{"expectedRevision": 1, "values": map[string]string{"identityStatus": "candidate"}}, 200)
	decode(t, f.json("GET", url, "", nil, 200), &v)
	if v.TrackUpdates || v.Revision != 2 {
		t.Fatal("withdrawn identity still allowed feed access")
	}
}
