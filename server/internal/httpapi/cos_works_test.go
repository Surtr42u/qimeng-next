package httpapi_test

import (
	"path"
	"strconv"
	"testing"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/service"
)

func TestCOSWorkIdentityFollowsScansUploadsAndExplicitDirectoryMoves(t *testing.T) {
	f := setup(t)
	regular := f.library
	u, _ := f.upload("regular-other.png", 111, nil)
	_ = f.complete(u)
	r := f.json("POST", "/libraries", "create-cos-library", map[string]any{"name": "Synthetic COS", "kind": "cos"}, 201)
	var library model.Library
	decode(t, r, &library)
	f.library = library.ID
	u, _ = f.upload("cos-first.png", 112, map[string]any{"relativeDirectory": "目录甲/作品甲"})
	a := *f.complete(u).Asset
	if a.CosWorkID == "" || a.COSDirectoryAuthorID == "" || a.AuthorID == "unknown" || a.CollectionID != "" {
		t.Fatal("COS directory work was conflated with collections or left unknown")
	}
	r = f.json("GET", "/assets/facets?row=works", "", nil, 200)
	var facets model.Page[service.AssetFacet]
	decode(t, r, &facets)
	if facets.TotalMatched == nil || *facets.TotalMatched != 2 {
		t.Fatal("mixed facets lost one partition")
	}
	mixed := assetsAt(t, f, "?workKeys=source:__other__&workKeys=author:"+a.AuthorID)
	if *mixed.TotalMatched != 2 {
		t.Fatal("regular source and COS author in the same row were ANDed")
	}
	mixed = assetsAt(t, f, "?workKeys=source:__other__&workKeys=author:"+a.AuthorID+"&roleKeys=work:"+a.CosWorkID)
	if *mixed.TotalMatched != 1 || mixed.Items[0].ID != a.ID {
		t.Fatal("cross-row AND did not narrow to COS work")
	}
	oldAuthor, oldWork, assetID := a.AuthorID, a.CosWorkID, a.ID
	r = f.json("POST", "/assets/"+a.ID+"/move", "cos-cross-directory", map[string]any{"expectedRevision": a.Revision, "relativePath": "目录乙/作品乙/moved.png"}, 200)
	decode(t, r, &a)
	if a.ID != assetID || a.AuthorID == oldAuthor || a.CosWorkID == oldWork || a.AuthorName != "目录乙" {
		t.Fatal("explicit move did not rebind directory-derived identity")
	}
	u, _ = f.upload("cos-following.png", 113, map[string]any{"cosWorkId": a.CosWorkID})
	second := *f.complete(u).Asset
	if second.CosWorkID != a.CosWorkID || second.AuthorID != a.AuthorID || path.Dir(second.RelativePath) != "目录乙/作品乙" {
		t.Fatal("explicit COS work upload ignored its registered directory")
	}
	r = f.json("PATCH", "/assets/"+a.ID, "cos-human-note", map[string]any{"expectedRevision": a.Revision, "note": "Keep this human note"}, 200)
	decode(t, r, &a)
	source := path.Join("libraries", f.library, a.RelativePath)
	target := path.Join("libraries", f.library, "目录丙/作品丙/external.png")
	if e := f.s.Root.MkdirAll(path.Dir(target), 0700); e != nil {
		t.Fatal(e)
	}
	if e := f.s.Root.Link(source, target); e != nil {
		t.Fatal(e)
	}
	if e := f.s.Root.Remove(source); e != nil {
		t.Fatal(e)
	}
	job := f.scan("cos-external-directory-move")
	if job.Moved != 1 || job.State != "completed" {
		t.Fatalf("scan: %+v", job)
	}
	r = f.json("GET", "/assets/"+assetID, "", nil, 200)
	decode(t, r, &a)
	if a.AuthorName != "目录丙" || a.Note != "Keep this human note" || a.CosWorkID == second.CosWorkID {
		t.Fatal("directory scan overwrote human note or retained stale work")
	}
	if a.CosWorkName != "作品丙" || len(a.WorkKeys) != 1 || a.WorkKeys[0] != "author:"+a.AuthorID || len(a.RoleKeys) != 1 || a.RoleKeys[0] != "work:"+a.CosWorkID || len(a.WorkFacets) != 1 || a.WorkFacets[0].Name != "目录丙" || len(a.RoleFacets) != 1 || a.RoleFacets[0].Name != "作品丙" {
		t.Fatal("COS group/title presentation was reconstructed from stale metadata")
	}
	f.json("POST", "/uploads", "conflicting-work-directory", map[string]any{"libraryId": f.library, "fileName": "conflict.png", "size": 10, "mimeType": "image/png", "mode": "ordinary", "cosWorkId": a.CosWorkID, "relativeDirectory": "elsewhere"}, 409)
	f.json("PATCH", "/authors/"+a.AuthorID, "rename-cos-identity", map[string]any{"expectedRevision": 1, "name": "does not rename directory"}, 409)
	f.json("DELETE", "/assets/"+a.ID+"?expectedRevision="+strconv.FormatInt(a.Revision, 10), "cos-trash-item", struct{}{}, 200)
	f.library = regular
	r = f.json("GET", "/cos-works?libraryId="+library.ID, "", nil, 200)
	var works model.Page[model.CosWork]
	decode(t, r, &works)
	if len(works.Items) != 3 {
		t.Fatal("directory work identities were silently deleted during file moves")
	}
}
