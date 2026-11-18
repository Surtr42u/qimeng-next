//go:build qimeng_legacy_import

package httpapi_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/service"
)

func TestV2RegularCreatorOnBothPartitionsRetainsFixedDirectoryAndSourceList(t *testing.T) {
	f := setup(t)
	regularUpload, _ := f.upload("普通.png", 143, nil)
	reg := *f.complete(regularUpload).Asset
	var lib model.Library
	decode(t, f.json("POST", "/libraries", "v2-cos-partition", map[string]string{"name": "Costumes", "kind": "cos"}, 201), &lib)
	f.library = lib.ID
	cosUpload, _ := f.upload("合拍.png", 144, map[string]any{"relativeDirectory": "Folder/Work"})
	cos := *f.complete(cosUpload).Asset
	fixed := cos.AuthorID
	doc := map[string]any{"format": "qimeng_backup", "schemaVersion": 2, "appIdentifier": "synthetic.legacy", "data": map[string]any{
		"mediaFiles": []any{map[string]any{"recordKey": "regular", "fileName": reg.FileName, "mediaType": "image", "sizeBytes": reg.Size}, map[string]any{"recordKey": "cos", "fileName": cos.FileName, "mediaType": "image", "sizeBytes": cos.Size, "isCosFile": true}},
		"viewEvents": []any{}, "authors": []any{map[string]any{"authorId": "studio", "displayName": "Studio"}, map[string]any{"authorId": "cos_folder", "displayName": "Folder"}},
		"authorMediaRefs": []any{map[string]any{"recordKey": "regular", "authorId": "studio", "fileName": reg.FileName}, map[string]any{"recordKey": "cos", "authorId": "studio", "fileName": cos.FileName}, map[string]any{"recordKey": "cos", "authorId": "cos_folder", "fileName": cos.FileName}},
		"txtFragments":    []any{map[string]any{"filename": "合成清单.txt", "content": "7  Studio\n来源  已购\n作品\n历史作品.png\n"}},
	}}
	raw, e := json.Marshal(doc)
	if e != nil {
		t.Fatal(e)
	}
	input := stageV2(t, f, "v2-mixed-owner-data", raw)
	plan := planV2(t, f, input, "v2-mixed-owner-plan")
	decode(t, f.json("POST", "/migration/v2/plans/"+plan.ID+"/commit", "v2-mixed-owner-commit", map[string]any{"fingerprint": plan.Fingerprint, "acceptSkipped": false}, 200), &plan)
	if plan.State != "committed" || plan.Counts["txtListsActivated"] != 1 || plan.Counts["authorSetsApplied"] != 2 {
		t.Fatalf("unexpected migration result: %+v", plan)
	}
	decode(t, f.json("GET", "/assets/"+reg.ID, "", nil, 200), &reg)
	decode(t, f.json("GET", "/assets/"+cos.ID, "", nil, 200), &cos)
	if reg.AuthorID == "unknown" || cos.AuthorID != fixed || !slices.Equal(cos.AuthorIDs, []string{fixed, reg.AuthorID}) {
		t.Fatal("media partition was mistaken for creator identity")
	}
	var lists model.Page[service.AuthorList]
	decode(t, f.json("GET", "/author-lists", "", nil, 200), &lists)
	if len(lists.Items) != 1 {
		t.Fatal("migration added redundant automatic text lists")
	}
	body := listContent(t, f, "/author-lists/"+lists.Items[0].ID+"/content")
	for _, term := range []string{"7  Studio", "已购", "历史作品.png", reg.FileName, cos.FileName} {
		if !strings.Contains(body, term) {
			t.Fatal("lost imported declaration", term)
		}
	}
	f.json("POST", "/author-lists/rebuild", "v2-mixed-rebuild", struct{}{}, 200)
	decode(t, f.json("GET", "/assets/"+cos.ID, "", nil, 200), &cos)
	if !slices.Equal(cos.AuthorIDs, []string{fixed, reg.AuthorID}) {
		t.Fatal("rebuild discarded imported mixed-creator relationship")
	}
}
