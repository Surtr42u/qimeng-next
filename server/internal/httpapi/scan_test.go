package httpapi_test

import (
 "os"
 "path"
 "strconv"
 "testing"
 "time"

 "qimeng.local/next/server/internal/model"
 "qimeng.local/next/server/internal/service"
)

func(f *fixture)scan(key string)service.ScanJob{
 f.t.Helper();r:=f.json("POST","/libraries/"+f.library+"/scans",key,map[string]any{"expectedRevision":1},202);var job service.ScanJob;decode(f.t,r,&job);deadline:=time.Now().Add(10*time.Second)
 for time.Now().Before(deadline){r=f.json("GET","/scans/"+job.ID,"",nil,200);decode(f.t,r,&job);if job.State!="queued"&&job.State!="running"{return job};time.Sleep(10*time.Millisecond)};f.t.Fatal("synthetic scan did not finish");return job
}
func TestScanRegistersExistingFileAndConservesIdentityAfterExternalMove(t *testing.T){
 f:=setup(t);original:=path.Join("libraries",f.library,"before.png");file,e:=f.s.Root.OpenFile(original,os.O_CREATE|os.O_EXCL|os.O_WRONLY,0600);if e!=nil{t.Fatal(e)};if _,e=file.Write(pngBytes(t,210));e!=nil{t.Fatal(e)};if e=file.Close();e!=nil{t.Fatal(e)}
 job:=f.scan("scan-existing-file");if job.State!="completed"||job.Added!=1{t.Fatalf("first scan: %+v",job)}
 r:=f.json("GET","/assets","",nil,200);var assets model.Page[model.Asset];decode(t,r,&assets);if len(assets.Items)!=1{t.Fatal("existing original not indexed")};a:=assets.Items[0]
 r=f.json("PUT","/assets/"+a.ID+"/tags","scan-preserve-tags",map[string]any{"expectedRevision":a.TagsRevision,"tags":[]string{"preserve"}},200);decode(t,r,&a)
 destination:=path.Join("libraries",f.library,"after.png");if e=f.s.Root.Link(original,destination);e!=nil{t.Fatal(e)};if e=f.s.Root.Remove(original);e!=nil{t.Fatal(e)}
 job=f.scan("scan-external-move");if job.State!="completed"||job.Moved!=1{t.Fatalf("move scan: %+v",job)};r=f.json("GET","/assets/"+a.ID,"",nil,200);decode(t,r,&a);if a.FileName!="after.png"||len(a.Tags)!=1||a.Tags[0]!="preserve"{t.Fatal("external move lost identity or tags")}
 if e=f.s.Root.Remove(destination);e!=nil{t.Fatal(e)};job=f.scan("scan-missing-original");if job.State!="partial"||job.Missing!=1{t.Fatalf("missing original was hidden: %+v",job)};r=f.json("GET","/assets/"+a.ID,"",nil,200);decode(t,r,&a);if a.Trashed{t.Fatal("missing file was treated as authorized deletion")}
}
func TestScanSkipsUnchangedContentAndPrivateTrash(t *testing.T){
 f:=setup(t);u,_:=f.upload("indexed-upload.png",211,nil);a:=*f.complete(u).Asset;job:=f.scan("scan-no-change");if job.Added!=0||job.Changed!=0{t.Fatalf("unchanged original reprobed as new: %+v",job)}
 r:=f.json("DELETE","/assets/"+a.ID+"?expectedRevision="+strconv.FormatInt(a.Revision,10),"scan-trash",struct{}{},200);decode(t,r,&a);job=f.scan("scan-skips-internal-trash");if job.Added!=0{t.Fatal("private trash was indexed as a new asset")}
}
