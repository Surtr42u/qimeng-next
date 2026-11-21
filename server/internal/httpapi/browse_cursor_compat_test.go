package httpapi_test

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"testing"
	"time"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/service"
)

func decodeRawCursor(t *testing.T, cursor string) map[string]any {
	t.Helper()
	raw, e := base64.RawURLEncoding.DecodeString(cursor)
	if e != nil {
		t.Fatalf("decode cursor: %v", e)
	}
	var value map[string]any
	if e = json.Unmarshal(raw, &value); e != nil {
		t.Fatalf("unmarshal cursor: %v", e)
	}
	return value
}

func encodeRawCursor(t *testing.T, value map[string]any) string {
	t.Helper()
	raw, e := json.Marshal(value)
	if e != nil {
		t.Fatalf("marshal cursor: %v", e)
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func withoutCursorTotal(t *testing.T, cursor string) string {
	t.Helper()
	value := decodeRawCursor(t, cursor)
	if _, ok := value["t"]; !ok {
		t.Fatal("cursor produced by the new code did not carry a total")
	}
	delete(value, "t")
	return encodeRawCursor(t, value)
}

func withCursorValue(t *testing.T, cursor, value string) string {
	t.Helper()
	raw := decodeRawCursor(t, cursor)
	raw["v"] = value
	return encodeRawCursor(t, raw)
}

func assetIDs(items []model.Asset) []string {
	ids := make([]string, len(items))
	for i, item := range items {
		ids[i] = item.ID
	}
	return ids
}

func eventIDs(items []model.Event) []string {
	ids := make([]string, len(items))
	for i, item := range items {
		ids[i] = item.EventID
	}
	return ids
}

func facetPage(t *testing.T, f *fixture, query string) model.Page[service.AssetFacet] {
	t.Helper()
	r := f.json("GET", "/assets/facets"+query, "", nil, 200)
	var page model.Page[service.AssetFacet]
	decode(t, r, &page)
	if page.TotalMatched == nil {
		t.Fatal("facet response omitted matching count")
	}
	return page
}

func historyPage(t *testing.T, f *fixture, query string) model.Page[model.Event] {
	t.Helper()
	r := f.json("GET", "/history"+query, "", nil, 200)
	var page model.Page[model.Event]
	decode(t, r, &page)
	if page.TotalMatched == nil {
		t.Fatal("history response omitted matching count")
	}
	return page
}

func createTestSource(t *testing.T, f *fixture, name string) service.Source {
	t.Helper()
	r := f.json("POST", "/sources", "source-"+name, map[string]any{"aliases": []string{}, "name": name}, 201)
	var source service.Source
	decode(t, r, &source)
	return source
}

// TestCursorWithoutTotalRecountsContinuation covers cursors issued after the
// total field was introduced but before it was populated: the missing "t" must
// fall back to the count query and still return the same page.
func TestCursorWithoutTotalRecountsContinuation(t *testing.T) {
	t.Run("assets", func(t *testing.T) {
		f := setup(t)
		for index, name := range []string{"legacy-a.png", "legacy-b.png", "legacy-c.png"} {
			u, _ := f.upload(name, uint8(31+index), nil)
			_ = f.complete(u)
		}
		first := assetsAt(t, f, "?limit=1")
		if first.NextCursor == "" {
			t.Fatal("asset page did not produce a continuation cursor")
		}
		plain := assetsAt(t, f, "?limit=1&cursor="+url.QueryEscape(first.NextCursor))
		stripped := assetsAt(t, f, "?limit=1&cursor="+url.QueryEscape(withoutCursorTotal(t, first.NextCursor)))
		if !slices.Equal(assetIDs(stripped.Items), assetIDs(plain.Items)) {
			t.Fatalf("asset continuation changed without the carried total: %v vs %v", assetIDs(stripped.Items), assetIDs(plain.Items))
		}
		if stripped.TotalMatched == nil || *stripped.TotalMatched != 3 || plain.TotalMatched == nil || *plain.TotalMatched != 3 {
			t.Fatalf("asset continuation total = %v, want 3", stripped.TotalMatched)
		}
	})

	t.Run("history", func(t *testing.T) {
		f := setup(t)
		for index, name := range []string{"history-a.png", "history-b.png"} {
			u, _ := f.upload(name, uint8(41+index), nil)
			asset := *f.complete(u).Asset
			f.json("POST", "/assets/"+asset.ID+"/events", "", map[string]any{
				"eventId":        fmt.Sprintf("legacy-total-view-%d", index),
				"sessionId":      fmt.Sprintf("legacy-total-session-%d", index),
				"kind":           "view",
				"contentVersion": asset.ContentVersion,
				"occurredAt":     time.Now().UTC().Add(-time.Hour + time.Duration(index)*time.Minute),
			}, 200)
		}
		first := historyPage(t, f, "?limit=1")
		if first.NextCursor == "" {
			t.Fatal("history page did not produce a continuation cursor")
		}
		plain := historyPage(t, f, "?limit=1&cursor="+url.QueryEscape(first.NextCursor))
		stripped := historyPage(t, f, "?limit=1&cursor="+url.QueryEscape(withoutCursorTotal(t, first.NextCursor)))
		if !slices.Equal(eventIDs(stripped.Items), eventIDs(plain.Items)) {
			t.Fatalf("history continuation changed without the carried total: %v vs %v", eventIDs(stripped.Items), eventIDs(plain.Items))
		}
		if stripped.TotalMatched == nil || *stripped.TotalMatched != 2 {
			t.Fatalf("history continuation total = %v, want 2", stripped.TotalMatched)
		}
	})

	t.Run("facets", func(t *testing.T) {
		f := setup(t)
		sourceA := createTestSource(t, f, "Legacy source A")
		sourceB := createTestSource(t, f, "Legacy source B")
		for index, item := range []struct {
			name   string
			source string
		}{{"legacy-source-a1.png", sourceA.ID}, {"legacy-source-a2.png", sourceA.ID}, {"legacy-source-b1.png", sourceB.ID}} {
			u, _ := f.upload(item.name, uint8(51+index), map[string]any{"sourceIds": []string{item.source}})
			_ = f.complete(u)
		}
		first := facetPage(t, f, "?row=works&limit=1")
		if first.NextCursor == "" {
			t.Fatal("facet page did not produce a continuation cursor")
		}
		plain := facetPage(t, f, "?row=works&limit=1&cursor="+url.QueryEscape(first.NextCursor))
		stripped := facetPage(t, f, "?row=works&limit=1&cursor="+url.QueryEscape(withoutCursorTotal(t, first.NextCursor)))
		if len(stripped.Items) != 1 || len(plain.Items) != 1 || stripped.Items[0].Key != plain.Items[0].Key {
			t.Fatalf("facet continuation changed without the carried total: %+v vs %+v", stripped.Items, plain.Items)
		}
		if stripped.TotalMatched == nil || *stripped.TotalMatched != 3 {
			t.Fatalf("facet continuation total = %v, want 3", stripped.TotalMatched)
		}
	})
}

// TestLegacyNullCursorStillSeeksNullGroup keeps the pre-sentinel "~null"
// cursor value working for file-date order: it must select exactly the NULL
// rows after the anchor, in the same direction as the old IS NULL clause.
func TestLegacyNullCursorStillSeeksNullGroup(t *testing.T) {
	f := setup(t)
	dated, _ := f.upload("legacy-null-dated.png", 61, map[string]any{"sourceMtime": time.Date(2024, 3, 1, 12, 0, 0, 0, time.UTC)})
	_ = f.complete(dated)
	nullIDs := make([]string, 4)
	for index := range nullIDs {
		u, _ := f.upload(fmt.Sprintf("legacy-null-%d.png", index), uint8(62+index), nil)
		nullIDs[index] = f.complete(u).Asset.ID
	}
	for _, id := range nullIDs {
		if _, e := f.s.DB.DB.Exec("UPDATE asset_records SET file_date=NULL WHERE id=?", id); e != nil {
			t.Fatal(e)
		}
	}
	slices.Sort(nullIDs)

	descFirst := assetsAt(t, f, "?limit=2")
	descLegacy := withCursorValue(t, withoutCursorTotal(t, descFirst.NextCursor), "~null")
	descPage := assetsAt(t, f, "?limit=10&cursor="+url.QueryEscape(descLegacy))
	wantDesc := append([]string{}, nullIDs[:len(nullIDs)-1]...)
	slices.Reverse(wantDesc)
	if !slices.Equal(assetIDs(descPage.Items), wantDesc) {
		t.Fatalf("descending legacy ~null cursor = %v, want %v", assetIDs(descPage.Items), wantDesc)
	}

	ascFirst := assetsAt(t, f, "?order=asc&limit=2")
	ascLegacy := withCursorValue(t, withoutCursorTotal(t, ascFirst.NextCursor), "~null")
	ascPage := assetsAt(t, f, "?limit=10&order=asc&cursor="+url.QueryEscape(ascLegacy))
	wantAsc := nullIDs[1:]
	if !slices.Equal(assetIDs(ascPage.Items), wantAsc) {
		t.Fatalf("ascending legacy ~null cursor = %v, want %v", assetIDs(ascPage.Items), wantAsc)
	}
}

// TestFixedFacetPartitionContinuation covers the f.position>? branch for a
// fixed row, which the existing tests only exercised through the first page.
func TestFixedFacetPartitionContinuation(t *testing.T) {
	f := setup(t)
	for index, name := range []string{"partition-a.png", "partition-b.png"} {
		u, _ := f.upload(name, uint8(71+index), nil)
		_ = f.complete(u)
	}
	query := "?row=partitions&limit=1"
	got := []string{}
	for {
		page := facetPage(t, f, query)
		if page.TotalOptions == nil || *page.TotalOptions != 3 {
			t.Fatalf("partition options = %v, want 3", page.TotalOptions)
		}
		if page.TotalMatched == nil || *page.TotalMatched != 2 {
			t.Fatalf("partition matched = %v, want 2", page.TotalMatched)
		}
		for _, item := range page.Items {
			got = append(got, item.ID)
		}
		if page.NextCursor == "" {
			break
		}
		query = "?row=partitions&limit=1&cursor=" + url.QueryEscape(page.NextCursor)
	}
	if want := []string{"all", "regular", "cos"}; !slices.Equal(got, want) {
		t.Fatalf("partition continuation = %v, want %v", got, want)
	}
}
