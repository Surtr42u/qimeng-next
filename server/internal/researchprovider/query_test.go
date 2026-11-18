package researchprovider

import (
	"encoding/base64"
	"net/url"
	"strings"
	"testing"
)

func TestPublicProviderOperations(t *testing.T) {
	tests := []struct{ name, operation, target, host, payload, wantURL string }{
		{"fxtwitter", "author", "@fixture", "api.fxtwitter.com", `{"code":200,"user":{"id":"9","screen_name":"fixture","name":"Fixture","website":{"url":"https://example.org/credits?private=drop"}}}`, "https://x.com/fixture"},
		{"fxtwitter", "post", "https://x.com/fixture/status/4", "api.fxtwitter.com", `{"code":200,"status":{"id":"4","author":{"id":"9","screen_name":"fixture"}}}`, "https://x.com/fixture/status/4"},
		{"pixiv", "author", "9", "www.pixiv.net", `{"error":false,"body":{"userId":"9","name":"Fixture"}}`, "https://www.pixiv.net/users/9"},
		{"pixiv", "work", "4", "www.pixiv.net", `{"error":false,"body":{"id":"4","userId":9,"userName":"fixture","title":"Fixture"}}`, "https://www.pixiv.net/artworks/4"},
		{"yandere", "artist", "fixture", "yande.re", `[{"id":9,"name":"fixture","urls":["https://example.org/credits?private=drop"]}]`, "https://yande.re/artist/show/9"},
		{"yandere", "tags", "fixture rating:safe", "yande.re", `[{"id":4,"source":"https://example.org/credits?private=drop"}]`, "https://yande.re/post/show/4"},
		{"e621", "post", "4", "e621.net", `{"post":{"id":4,"tags":{"artist":["fixture"]},"sources":["https://example.org/credits?private=drop"]}}`, "https://e621.net/posts/4"},
		{"e621", "tags", "fixture", "e621.net", `{"posts":[{"id":4},{"id":5}]}`, "https://e621.net/posts/4"},
		{"wayback", "url", "https://example.org/credits", "web.archive.org", `[["timestamp","original","statuscode"],["20200102030405","https://example.org/credits","200"]]`, "https://web.archive.org/web/20200102030405/https://example.org/credits"},
		{"steam", "workshop", "4", "api.steampowered.com", `{"response":{"publishedfiledetails":[{"publishedfileid":"4","creator":"9","result":1,"title":"Fixture"}]}}`, "https://steamcommunity.com/sharedfiles/filedetails/?id=4"},
		{"bing", "search", "fixture & credits", "www.bing.com", `<li class="b_algo"><h2><a href="https://www.bing.com/ck/a?u=a1` + base64.RawURLEncoding.EncodeToString([]byte("https://example.org/credits?private=drop")) + `">Fixture</a></h2></li>`, "https://example.org/credits"},
	}
	exercised := map[string]bool{}
	for _, tt := range tests {
		t.Run(tt.name+"/"+tt.operation, func(t *testing.T) {
			q, err := Build(tt.name, tt.operation, tt.target, 1)
			if err != nil {
				t.Fatal(err)
			}
			dst, err := url.Parse(q.URL)
			if err != nil || dst.Scheme != "https" || dst.Host != tt.host {
				t.Fatalf("unexpected request target %q", q.URL)
			}
			if tt.name == "steam" {
				form, e := url.ParseQuery(q.Form)
				if q.Method != "POST" || e != nil || len(form) != 2 || form.Get("publishedfileids[0]") != "4" || form.Get("itemcount") != "1" {
					t.Fatal("unexpected Steam form")
				}
			} else if q.Method != "GET" || q.Form != "" {
				t.Fatal("unexpected request body")
			}
			state, items := Parse(q, []byte(tt.payload))
			if state != "ok" || len(items) != 1 || items[0].URL != tt.wantURL {
				t.Fatalf("normalized %s %+v", state, items)
			}
			for _, link := range items[0].Sources {
				if strings.Contains(link, "private") {
					t.Fatal("source query remained in evidence")
				}
			}
			if (tt.name == "wayback" || tt.name == "bing") != (q.EvidenceKind == "lead") {
				t.Fatal("wrong evidence classification")
			}
			if state, items := Parse(q, []byte("<html>Access challenge</html>")); state != "unavailable" || len(items) != 0 {
				t.Fatal("unrecognized content was treated as public evidence")
			}
			exercised[tt.name+"."+tt.operation] = true
		})
	}
	for _, info := range Matrix() {
		for _, op := range info.Operations {
			if !exercised[info.Name+"."+op] {
				t.Fatalf("missing adapter test: %s.%s", info.Name, op)
			}
		}
	}
}

func TestUnsafeTargetsAndUnrelatedRecordsStayOut(t *testing.T) {
	for _, value := range []string{"http://example.org/item", "https://example.org/abc?token=secret", "https://example.org/p#key", "https://user@example.org/", "https://127.0.0.1/x", "https://0177.0.0.1/x", "https://nas.local/x", "https://example.org/path%5Cname", "https://example.org/path%0Dname"} {
		if _, err := Build("wayback", "url", value, 1); err == nil {
			t.Errorf("accepted archive target %q", value)
		}
	}
	for _, value := range []string{"file:///home/fixture", "C:\\private\\fixture", "/private/path", "https://example.org/secret?q=x"} {
		if _, err := Build("bing", "search", value, 1); err == nil {
			t.Errorf("accepted private search string %q", value)
		}
	}
	for _, tt := range []struct{ name, op, target, payload, state string }{
		{"fxtwitter", "author", "fixture", `{"code":200,"user":{"id":"9","screen_name":"someone_else"}}`, "unavailable"},
		{"pixiv", "work", "4", `{"error":false,"body":{"id":"5","userId":"9"}}`, "unavailable"},
		{"e621", "post", "4", `{"post":{"id":5}}`, "unavailable"},
		{"e621", "tags", "fixture", `{"posts":[]}`, "not_found"},
		{"steam", "workshop", "4", `{"response":{"publishedfiledetails":[{"result":15}]}}`, "blocked"},
		{"bing", "search", "fixture", `<li class="b_no">No results</li>`, "not_found"},
		{"bing", "search", "fixture", `<html><h2><a href="https://example.org">Challenge</a></h2></html>`, "unavailable"},
	} {
		q, e := Build(tt.name, tt.op, tt.target, 1)
		if e != nil {
			t.Fatal(e)
		}
		state, items := Parse(q, []byte(tt.payload))
		if state != tt.state || len(items) != 0 {
			t.Fatalf("%s: %s %+v", tt.name, state, items)
		}
	}
	for status, want := range map[int]string{403: "blocked", 404: "not_found", 429: "rate_limited", 302: "unavailable", 502: "unavailable"} {
		if HTTPState(status) != want {
			t.Errorf("HTTP %d", status)
		}
	}
}
