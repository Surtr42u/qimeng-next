package httpapi_test

import (
 "testing"
 "qimeng.local/next/server/internal/model"
)

func TestCOSDirectoryIdentitySurvivesRegularAuthorEditing(t *testing.T){
 f:=setup(t);r:=f.json("POST","/authors","mixed-regular-author",map[string]any{"name":"Synthetic regular creator","aliases":[]string{}},201);var regular model.Author;decode(t,r,&regular)
 r=f.json("POST","/libraries","mixed-cos-library",map[string]any{"name":"Mixed synthetic COS","kind":"cos"},201);var library model.Library;decode(t,r,&library);f.library=library.ID;u,_:=f.upload("mixed-authors.png",206,map[string]any{"relativeDirectory":"固定目录作者/作品"});a:=*f.complete(u).Asset;fixed:=a.COSDirectoryAuthorID
 r=f.json("PATCH","/assets/"+a.ID,"attach-regular-to-cos",map[string]any{"expectedRevision":a.Revision,"expectedAuthorsRevision":a.AuthorsRevision,"authorIds":[]string{regular.ID}},200);decode(t,r,&a);if len(a.AuthorIDs)!=2||a.AuthorIDs[0]!=fixed||a.AuthorIDs[1]!=regular.ID{t.Fatal("directory identity was removed or regular creator rejected")}
 r=f.json("PATCH","/assets/"+a.ID,"detach-regular-from-cos",map[string]any{"expectedRevision":a.Revision,"expectedAuthorsRevision":a.AuthorsRevision,"authorIds":[]string{}},200);decode(t,r,&a);if len(a.AuthorIDs)!=1||a.AuthorIDs[0]!=fixed{t.Fatal("clearing regular authors cleared the directory identity")}
}
