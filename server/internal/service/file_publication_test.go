package service

import(
 "bytes"
 "context"
 "encoding/json"
 "errors"
 "io"
 "os"
 "testing"
 "time"

 "qimeng.local/next/server/internal/model"
)

func TestPendingAssetPreservesHumanChangesAndNeverUnlinksAnUnrelatedCopy(t *testing.T){
 for _,change:=range []string{"missing-target","other-target-same-bytes","new-source-same-bytes"}{t.Run(change,func(t *testing.T){
  ctx:=context.Background();s,e:=New(Config{DataDir:t.TempDir(),AdminToken:"synthetic-published-link-owner-token",ReserveBytes:1});if e!=nil{t.Fatal(e)};defer s.Close()
  var lib model.Library;r:=testMutation(t,s,"recovery-library",func(w *Write)(any,int,error){v,e:=s.CreateLibrary(ctx,w,"Recovery","regular");return v,201,e});if e=json.Unmarshal(r.Body,&lib);e!=nil{t.Fatal(e)}
  u:=stagedUpload(t,s,lib.ID,50,"before.png");var result model.UploadResult;r=testMutation(t,s,"recovery-ingest",func(w *Write)(any,int,error){v,e:=s.CompleteUpload(ctx,w,u.ID);return v,200,e});if e=json.Unmarshal(r.Body,&result);e!=nil{t.Fatal(e)}
  before:=*result.Asset;after:=before;after.FileName="after.png";after.RelativePath=after.FileName;after.Revision++;after.UpdatedAt=time.Now().UTC()
  p:=model.Principal{ID:"owner",Scopes:[]string{"admin"}};w:=&Write{s:s,ctx:ctx,Principal:p,operation:"recover-durable-move",key:"move-identity",inputHash:"synthetic"}
  j,e:=s.prepareFile(ctx,w,fileIntent{ID:"synthetic-move",Kind:"move",Before:&before,After:after,Source:assetPath(before),Target:assetPath(after)});if e!=nil{t.Fatal(e)}
  note:="Accepted after recovery";_,e=s.Run(ctx,p,"competing-note","competing-note-key",nil,func(w *Write)(any,int,error){v,e:=s.UpdateAsset(ctx,w,before.ID,AssetUpdate{ExpectedRevision:before.Revision,Note:&note});return v,200,e});var pending *Error;if !errors.As(e,&pending)||pending.Code!="publication_pending"{t.Fatalf("competing edit was accepted or hidden: %v",e)}
  if e=s.applyFile(j);e!=nil{t.Fatal(e)};tx,e:=s.DB.DB.BeginTx(ctx,nil);if e!=nil{t.Fatal(e)};if e=s.commitFile(ctx,tx,&j);e!=nil{tx.Rollback();t.Fatal(e)};if e=tx.Commit();e!=nil{t.Fatal(e)}
  raw:=syntheticPNG(t,50)
  original:=j.Target;if change=="new-source-same-bytes"{original=j.Source}
  if e=s.Root.Remove(original);e!=nil{t.Fatal(e)}
  if change!="missing-target"{f,e:=s.Root.OpenFile(original,os.O_CREATE|os.O_EXCL|os.O_WRONLY,0600);if e!=nil{t.Fatal(e)};if _,e=f.Write(raw);e!=nil{f.Close();t.Fatal(e)};if e=f.Close();e!=nil{t.Fatal(e)}}
  e=s.finishFile(ctx,j)
  if change=="missing-target"{
   if e!=nil{t.Fatal(e)};if _,e=s.Root.Stat(j.Source);!errors.Is(e,os.ErrNotExist){t.Fatal("old source retained after verified relink")}
  }else{
   if e==nil{t.Fatal("different object with identical bytes was removed")}
   a,e:=s.Root.Stat(j.Source);if e!=nil{t.Fatal("source was removed",e)};b,e:=s.Root.Stat(j.Target);if e!=nil||os.SameFile(a,b){t.Fatal("conflicting target was changed",e)}
  }
  f,e:=s.Root.Open(j.Target);if e!=nil{t.Fatal(e)};got,e:=io.ReadAll(f);f.Close();if e!=nil||!bytes.Equal(got,raw){t.Fatal("published bytes lost",e)}
 })}
}
