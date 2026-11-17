package service

import (
 "bytes"
 "context"
 "encoding/json"
 "image"
 "image/color"
 "image/png"
 "testing"
 "time"

 "qimeng.local/next/server/internal/model"
 "qimeng.local/next/server/internal/store"
)

func syntheticPNG(t *testing.T,v uint8)[]byte{t.Helper();img:=image.NewRGBA(image.Rect(0,0,2,2));img.Set(0,0,color.RGBA{v,10,20,255});var out bytes.Buffer;if e:=png.Encode(&out,img);e!=nil{t.Fatal(e)};return out.Bytes()}
func testMutation(t *testing.T,s *Service,operation string,fn func(*Write)(any,int,error))Result{t.Helper();r,e:=s.Run(context.Background(),model.Principal{ID:"owner",Scopes:[]string{"admin"}},operation,operation+"-receipt",[]byte(operation),fn);if e!=nil{t.Fatal(e)};return r}
func stagedUpload(t *testing.T,s *Service,library string,shade uint8,name string)model.Upload{t.Helper();ctx:=context.Background();data:=syntheticPNG(t,shade);r:=testMutation(t,s,"create-"+name,func(w *Write)(any,int,error){u,e:=s.CreateUpload(ctx,w,model.UploadInput{LibraryID:library,FileName:name,Size:int64(len(data)),MimeType:"image/png",Mode:"ordinary"});return u,201,e});var u model.Upload;if e:=json.Unmarshal(r.Body,&u);e!=nil{t.Fatal(e)};testMutation(t,s,"append-"+name,func(w *Write)(any,int,error){v,e:=s.AppendUpload(ctx,w,u.ID,0,data);return v,200,e});u.Offset=u.Size;u.Hash=digest(data);u.State="staged";if e:=store.SaveUpload(ctx,s.DB.DB,u);e!=nil{t.Fatal(e)};return u}
func TestPublishedIntentRecoversSameAssetAndReceipt(t *testing.T){
 ctx:=context.Background();config:=Config{DataDir:t.TempDir(),AdminToken:"synthetic-recovery-secret-0000000000000000",ReserveBytes:1};s,e:=New(config);if e!=nil{t.Fatal(e)}
 r:=testMutation(t,s,"library-create",func(w *Write)(any,int,error){v,e:=s.CreateLibrary(ctx,w,"Recovery","regular");return v,201,e});var l model.Library;if e=json.Unmarshal(r.Body,&l);e!=nil{t.Fatal(e)};u:=stagedUpload(t,s,l.ID,50,"recovery.png")
 now:=time.Now().UTC();a:=model.Asset{ID:digest([]byte("asset:"+u.ID))[:32],LibraryID:l.ID,FileName:u.FileName,OriginalFileName:u.FileName,RelativePath:u.FileName,MediaType:"image",MimeType:"image/png",Size:u.Size,Hash:u.Hash,Revision:1,TagsRevision:1,AuthorsRevision:1,ContentVersion:1,AuthorID:"unknown",AuthorIDs:[]string{"unknown"},AuthorStatus:"unknown",Tags:[]string{},Characters:[]string{},SourceIDs:[]string{},CharacterIDs:[]string{},AcquisitionSources:[]string{},CreatedAt:now,UpdatedAt:now,ProbeState:"unavailable"};a.ContentURL="/api/v1/assets/"+a.ID+"/content"
 w:=&Write{s:s,ctx:ctx,Principal:model.Principal{ID:"owner",Scopes:[]string{"admin"}},operation:"complete-crash",key:"crash-receipt-key",inputHash:digest([]byte("original-input"))}
 j,e:=s.prepareFile(ctx,w,fileIntent{ID:"synthetic-publish-intent",Kind:"ingest",Source:uploadPath(u.ID),Target:assetPath(a),UploadID:u.ID,After:a});if e!=nil{t.Fatal(e)};if e=s.applyFile(j);e!=nil{t.Fatal(e)}
 if e=s.Close();e!=nil{t.Fatal(e)};s,e=New(config);if e!=nil{t.Fatal(e)};defer s.Close()
 got,e:=s.Asset(ctx,a.ID);if e!=nil{t.Fatal(e)};if got.Hash!=u.Hash||got.ID!=a.ID{t.Fatal("recovery changed content or identity")};restored,e:=s.Upload(ctx,w.Principal,u.ID);if e!=nil{t.Fatal(e)};if restored.State!="ingested"||restored.AssetID!=a.ID{t.Fatal("upload did not reconcile")}
 result,e:=s.Run(ctx,w.Principal,w.operation,w.key,[]byte("original-input"),func(*Write)(any,int,error){t.Fatal("recovered receipt was not replayed");return nil,0,nil});if e!=nil{t.Fatal(e)};var completed model.UploadResult;if e=json.Unmarshal(result.Body,&completed);e!=nil{t.Fatal(e)};if completed.Asset.ID!=a.ID{t.Fatal("wrong receipt asset")}
}
func TestExclusiveServiceOwnerAndSymlinkBoundary(t *testing.T){
 config:=Config{DataDir:t.TempDir(),AdminToken:"synthetic-exclusive-secret-00000000000000",ReserveBytes:1};s,e:=New(config);if e!=nil{t.Fatal(e)};defer s.Close();other,e:=New(config);if e==nil{other.Close();t.Fatal("second service acquired same data root")}
 if e=s.safe("../outside");e==nil{t.Fatal("traversal accepted")}
}
