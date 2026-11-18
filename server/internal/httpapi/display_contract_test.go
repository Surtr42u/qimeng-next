package httpapi_test

import "testing"

func TestUnknownFileDateCountIsExplicitAndIndependentOfPageSize(t *testing.T) {
	f := setup(t)
	first, _ := f.upload("known-date.png", 140, nil)
	_ = f.complete(first)
	second, _ := f.upload("unknown-date.png", 141, nil)
	asset := f.complete(second).Asset
	if _, e := f.s.DB.DB.Exec("UPDATE asset_locations SET content_mtime=NULL WHERE asset_id=?", asset.ID); e != nil {
		t.Fatal(e)
	}
	if _, e := f.s.DB.DB.Exec("UPDATE asset_records SET file_date=NULL,data=json_remove(data,'$.fileDate','$.sourceMtime') WHERE id=?", asset.ID); e != nil {
		t.Fatal(e)
	}
	page := assetsAt(t, f, "?dateCounts=true&limit=1")
	if page.UnknownDateCount == nil || *page.UnknownDateCount != 1 || page.TotalMatched == nil || *page.TotalMatched != 2 || page.DateCounts == nil || len(*page.DateCounts) != 1 || (*page.DateCounts)[0].FileCount != 1 {
		t.Fatalf("unknown group counted loaded items: %+v", page)
	}
}
