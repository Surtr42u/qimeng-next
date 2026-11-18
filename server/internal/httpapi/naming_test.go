package httpapi_test

import (
	"context"
	"net/url"
	"os"
	"path"
	"strconv"
	"testing"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/service"
	"qimeng.local/next/server/internal/store"
)

func TestNamingUsesOnlyDirectEntriesFromTheIndexAndPhysicalDirectory(t *testing.T) {
	f := setup(t)
	const target = "书架/插画"
	root := "libraries/" + f.library
	if err := f.s.Root.MkdirAll(path.Join(root, target), 0700); err != nil {
		t.Fatal(err)
	}
	// The catalog still reserves missing original names. A real file which has
	// not been indexed yet independently occupies one slot in this directory.
	for _, row := range []struct{ id, dir, name string }{
		{"root", "", "系列 01.png"},
		{"target", target, "系列 02.png"},
		{"below", target + "/内页", "系列 03.png"},
		{"sibling", "书架/插画收藏", "别处作品 01.png"},
	} {
		a := model.Asset{ID: row.id, LibraryID: f.library, FileName: row.name, RelativePath: path.Join(row.dir, row.name),
			MediaType: "image", AuthorID: "unknown", AuthorIDs: []string{"unknown"}, Revision: 1, ContentVersion: 1}
		if err := store.SaveAsset(context.Background(), f.s.DB.DB, a); err != nil {
			t.Fatal(err)
		}
	}
	file, err := f.s.Root.OpenFile(path.Join(root, target, "系列 01.png"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	for _, scope := range []struct {
		directory string
		next      int
	}{{"", 2}, {target, 3}, {"未建目录", 1}} {
		query := url.Values{"directory": {scope.directory}, "family": {"系列"}, "mediaType": {"image"}}
		var got service.NamingContext
		decode(t, f.json("GET", "/libraries/"+f.library+"/naming/context?"+query.Encode(), "", nil, 200), &got)
		if got.NextWorkNumber != scope.next {
			t.Fatalf("directory %q got %d, want %d", scope.directory, got.NextWorkNumber, scope.next)
		}
	}
	for _, search := range []struct {
		family string
		count  int
	}{{"系列", 1}, {"别处作品", 0}} {
		query := url.Values{"directory": {target}, "q": {search.family}, "mediaType": {"image"}}
		var result struct {
			Items []service.NameSuggestion `json:"items"`
		}
		decode(t, f.json("GET", "/libraries/"+f.library+"/naming/suggestions?"+query.Encode(), "", nil, 200), &result)
		if len(result.Items) != search.count || search.count == 1 && result.Items[0].WorkNumber != 3 {
			t.Fatalf("directory suggestion for %q included names outside its scope: %+v", search.family, result.Items)
		}
	}
}

func TestNamingPreviewUsesPhysicalOccupancyAndSharedWorkAllocator(t *testing.T) {
	f := setup(t)
	for _, name := range []string{"Family 01.png", "Family 03.jpg", "Family 02.mp4"} {
		file, e := f.s.Root.OpenFile(path.Join("libraries", f.library, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = file.Write([]byte("synthetic name occupancy")); e != nil {
			t.Fatal(e)
		}
		if e = file.Close(); e != nil {
			t.Fatal(e)
		}
	}
	r := f.json("GET", "/libraries/"+f.library+"/naming/context?family=Family&mediaType=image", "", nil, 200)
	var context service.NamingContext
	decode(t, r, &context)
	if context.NextWorkNumber != 2 || context.Width != 2 || context.NumberClass != "image" || context.Reserved {
		t.Fatalf("context: %+v", context)
	}
	r = f.json("POST", "/libraries/"+f.library+"/naming/preview", "", map[string]any{"intent": "new_work", "family": "Family", "items": []any{map[string]any{"fileName": "bad�.png", "groupKey": "confirmed-work", "page": 1}, map[string]any{"fileName": "second.png", "groupKey": "confirmed-work", "page": 2}}}, 200)
	var preview service.NamingPreview
	decode(t, r, &preview)
	if preview.Reserved || len(preview.Items) != 2 || preview.Items[0].FileName != "Family 02 (1).png" || preview.Items[1].FileName != "Family 02 (2).png" {
		t.Fatalf("group preview: %+v", preview)
	}
	upload, _ := f.upload("ordinary-source.png", 220, map[string]any{"family": "Family"})
	asset := *f.complete(upload).Asset
	if asset.FileName != "Family 02.png" {
		t.Fatalf("ordinary upload used a different allocator: %s", asset.FileName)
	}
	r = f.json("GET", "/libraries/"+f.library+"/naming/context?family=Family&mediaType=image", "", nil, 200)
	decode(t, r, &context)
	if context.NextWorkNumber != 4 {
		t.Fatal("preview was incorrectly reserved or commit ignored occupancy")
	}
	r = f.json("GET", "/libraries/"+f.library+"/naming/context?family=Family&mediaType=video", "", nil, 200)
	decode(t, r, &context)
	if context.NextWorkNumber != 1 {
		t.Fatal("video numbering leaked image sequence")
	}
}
func TestOrdinaryGroupAppendAndPreviewSkipMissingPages(t *testing.T) {
	f := setup(t)
	r := f.json("POST", "/collections", "create-image-collection", map[string]any{"libraryId": f.library, "name": "Known Group", "kind": "image_set"}, 201)
	var collection model.Collection
	decode(t, r, &collection)
	r = f.json("GET", "/libraries/"+f.library+"/naming/context?collectionId="+collection.ID, "", nil, 200)
	var emptyContext service.NamingContext
	decode(t, r, &emptyContext)
	if emptyContext.NextPage != 1 || emptyContext.CurrentWorkNumber != 1 {
		t.Fatal("empty group cannot preview its first work")
	}
	r = f.json("POST", "/libraries/"+f.library+"/naming/preview", "", map[string]any{"intent": "append_pages", "collectionId": collection.ID, "items": []any{map[string]any{"fileName": "first.png"}, map[string]any{"fileName": "second.png"}}}, 200)
	var firstPreview service.NamingPreview
	decode(t, r, &firstPreview)
	if firstPreview.Items[0].FileName != "Known Group 1 (1).png" || firstPreview.Items[1].Page != 2 {
		t.Fatal("empty group page preview diverged from ordinary upload")
	}
	assets := []model.Asset{}
	for i := 0; i < 3; i++ {
		upload, _ := f.upload("group-input-"+strconv.Itoa(i)+".png", uint8(221+i), map[string]any{"collectionId": collection.ID})
		assets = append(assets, *f.complete(upload).Asset)
	}
	if assets[0].FileName != "Known Group 1 (1).png" || assets[2].Page != 3 {
		t.Fatal("ordinary group append did not share work number")
	}
	f.json("DELETE", "/assets/"+assets[1].ID+"?expectedRevision="+strconv.FormatInt(assets[1].Revision, 10), "trash-middle-page", struct{}{}, 200)
	r = f.json("POST", "/libraries/"+f.library+"/naming/preview", "", map[string]any{"intent": "append_pages", "collectionId": collection.ID, "items": []any{map[string]any{"fileName": "next.png"}}}, 200)
	var preview service.NamingPreview
	decode(t, r, &preview)
	if preview.Items[0].Page != 4 || preview.Items[0].FileName != "Known Group 1 (4).png" {
		t.Fatal("append filled a missing page or changed work number")
	}
	upload, _ := f.upload("last-group-input.png", 225, map[string]any{"collectionId": collection.ID})
	asset := *f.complete(upload).Asset
	if asset.FileName != preview.Items[0].FileName || asset.Page != 4 {
		t.Fatal("preview and ordinary append diverged")
	}
}
