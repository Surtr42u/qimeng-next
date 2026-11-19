package catalog

import (
	"encoding/json"
	"errors"
	"net/url"
	"testing"
)

func TestColdCatalogTXTEncodedRelayRestoresVerbatim(t *testing.T) {
	const relay = "https://mirror.example/?https%3a%2F%2Fartist.example%2fwOrk%3Fview%3Dpublic"
	const source = "1  Writer\r\n来源  " + relay + "\r\n作品\r\nexample.png\r\n"
	f := newFixture(t)
	f.author("writer", "Writer", "regular", 0)
	f.native("INSERT INTO settings(key,value) VALUES('instance_id','synthetic-native')")
	raw, err := json.Marshal([]fragment{{Filename: "public.txt", Content: source}})
	if err != nil {
		t.Fatal(err)
	}
	f.setting("imported_txt_sources", string(raw))
	if err := f.run(); err != nil {
		t.Fatal(err)
	}
	after, _, _ := f.restoreCatalog()
	for _, db := range []*fixture{f, after} {
		db.expect(source, "SELECT content FROM author_txt_fragments WHERE name='public.txt'")
		db.expect(relay, "SELECT json_extract(data,'$.acquisitionSources[0]') FROM authors WHERE id='writer'")
	}
}

func TestColdCatalogBareRelayRestrictions(t *testing.T) {
	i := importer{roots: []string{"/synthetic-mount"}}
	const outer = "https://mirror.example/?"
	const safe = "https%3A%2F%2Fartist.example%2Fwork"
	if _, _, err := i.inspectJSON([]byte(`["`+outer+safe+`"]`), "author_source_vocabulary"); !errors.Is(err, errPrivate) {
		t.Fatal("ordinary setting acquired a TXT-only relay exception")
	}
	deep := "https://artist.example/work"
	for range 6 {
		deep = outer + url.QueryEscape(deep)
	}
	for _, unsafe := range []string{
		deep, outer + "next=" + safe, outer + safe + "&other", outer + safe + "=",
		outer + "https://artist.example/work", outer + "http%3A%2F%2Fartist.example%2Fwork",
		outer + "https%253A%252F%252Fartist.example%252Fwork",
		outer + url.QueryEscape("https://artist.example/x?access_token=fixture"),
		outer + url.QueryEscape("https://u@artist.example/x"),
		outer + url.QueryEscape("https://127.0.0.1/x"),
		outer + url.QueryEscape("https://artist.example/synthetic-mount/private"),
		outer + url.QueryEscape("https://artist.example/C:/private"),
	} {
		if _, _, err := i.inspectText(unsafe, fragmentText); !errors.Is(err, errPrivate) {
			t.Fatal("ambiguous or unsafe bare relay escaped strict validation")
		}
	}
}

func TestColdCatalogFullwidthColonSeparatesPublicProseCitations(t *testing.T) {
	i := importer{}
	const first = "https://one.example/work下一处："
	const second = "https://two.example/item"
	plain := first + second
	if got, count, err := i.inspectText(plain, biographyText); err != nil || count != 0 || got != plain {
		t.Fatal("joined prose citations changed")
	}
	if got, count, err := i.inspectText(plain+"?sig=fixture&view=all", biographyText); err != nil || count != 1 || got != plain+"?view=all" {
		t.Fatal("second prose citation was not inspected independently")
	}
	if _, _, err := i.inspectText(first+"https://127.0.0.1/item", biographyText); !errors.Is(err, errPrivate) {
		t.Fatal("private second citation was accepted")
	}
	if _, _, err := i.publicReference(plain, strictText, 0); !errors.Is(err, errPrivate) {
		t.Fatal("structured URL acquired a prose delimiter")
	}
}
