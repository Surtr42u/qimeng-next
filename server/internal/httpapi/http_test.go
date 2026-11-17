package httpapi_test

import (
 "bytes"
 "crypto/sha256"
 "fmt"
 "encoding/json"
 "image"
 "image/color"
 "image/png"
 "io"
 "net/http"
 "net/http/httptest"
 "strconv"
 "strings"
 "sync"
 "testing"

 "qimeng.local/next/server/internal/httpapi"
 "qimeng.local/next/server/internal/model"
 "qimeng.local/next/server/internal/service"
)

const testSecret="synthetic-test-owner-credential-0000000000"
type fixture struct{t *testing.T;s *service.Service;h http.Handler;library string}
func setup(t *testing.T)*fixture{t.Helper();s,e:=service.New(service.Config{DataDir:t.TempDir(),AdminToken:testSecret,ReserveBytes:1});if e!=nil{t.Fatal(e)};t.Cleanup(func(){s.Close()});f:=&fixture{t:t,s:s,h:httpapi.New(s)};r:=f.json("POST","/libraries","library-create-1",map[string]any{"name":"Synthetic","kind":"regular"},201);var l model.Library;decode(t,r,&l);f.library=l.ID;return f}
func decode(t *testing.T,r *httptest.ResponseRecorder,out any){t.Helper();if e:=json.Unmarshal(r.Body.Bytes(),out);e!=nil{t.Fatalf("decode: %v, %s",e,r.Body.String())}}
func(f *fixture)request(method,p,key,credential string,body []byte,headers map[string]string)*httptest.ResponseRecorder{r:=httptest.NewRequest(method,"/api/v1"+p,bytes.NewReader(body));if credential!=""{r.Header.Set("Authorization","Bearer "+credential)};r.Header.Set("Content-Type","application/json");if key!=""{r.Header.Set("Idempotency-Key",key)};for k,v:=range headers{r.Header.Set(k,v)};w:=httptest.NewRecorder();f.h.ServeHTTP(w,r);return w}
func(f *fixture)json(method,p,key string,in any,status int)*httptest.ResponseRecorder{f.t.Helper();raw,e:=json.Marshal(in);if e!=nil{f.t.Fatal(e)};r:=f.request(method,p,key,testSecret,raw,nil);if r.Code!=status{f.t.Fatalf("%s %s: status %d want %d; %s",method,p,r.Code,status,r.Body.String())};return r}
func pngBytes(t *testing.T,shade uint8)[]byte{t.Helper();img:=image.NewRGBA(image.Rect(0,0,8,8));for y:=0;y<8;y++{for x:=0;x<8;x++{img.Set(x,y,color.RGBA{shade,uint8(x*20),uint8(y*20),255})}};var b bytes.Buffer;if e:=png.Encode(&b,img);e!=nil{t.Fatal(e)};return b.Bytes()}
func(f *fixture)upload(name string,shade uint8,extra map[string]any)(model.Upload,[]byte){f.t.Helper();body:=pngBytes(f.t,shade);in:=map[string]any{"libraryId":f.library,"fileName":name,"size":len(body),"mimeType":"image/png","mode":"ordinary"};for k,v:=range extra{in[k]=v};r:=f.json("POST","/uploads","create-upload-"+name,in,201);var u model.Upload;decode(f.t,r,&u);r=f.request("PUT","/uploads/"+u.ID+"/content","",testSecret,body,map[string]string{"Content-Type":"application/octet-stream","Upload-Offset":"0"});if r.Code!=200{f.t.Fatalf("append: %s",r.Body.String())};decode(f.t,r,&u);return u,body}
func(f *fixture)complete(u model.Upload)model.UploadResult{f.t.Helper();r:=f.json("POST","/uploads/"+u.ID+"/complete","complete-"+u.ID,struct{}{},200);var out model.UploadResult;decode(f.t,r,&out);return out}

func TestOrdinaryUploadReplayTagDeletionAndFileLifecycle(t *testing.T){
 f:=setup(t);u,body:=f.upload("example.png",30,nil)
 replay:=f.request("PUT","/uploads/"+u.ID+"/content","",testSecret,body,map[string]string{"Upload-Offset":"0"});if replay.Code!=200{t.Fatal(replay.Body.String())}
 changed:=append([]byte(nil),body...);changed[len(changed)-1]^=1;r:=f.request("PUT","/uploads/"+u.ID+"/content","",testSecret,changed,map[string]string{"Upload-Offset":"0"});if r.Code!=409{t.Fatalf("changed chunk accepted: %d",r.Code)}
 out:=f.complete(u);if out.Asset==nil||out.Upload.State!="ingested"||out.Asset.AuthorStatus!="unknown"{t.Fatalf("not ingested: %+v",out)};a:=*out.Asset
 r=f.json("POST","/uploads/"+u.ID+"/complete","complete-"+u.ID,struct{}{},200);var again model.UploadResult;decode(t,r,&again);if again.Asset.ID!=a.ID{t.Fatal("completion duplicated identity")}
 r=f.json("PUT","/assets/"+a.ID+"/tags","tag-save-1",map[string]any{"expectedRevision":a.TagsRevision,"tags":[]string{"test"}},200);decode(t,r,&a)
 r=f.json("PUT","/assets/"+a.ID+"/tags","tag-clear-2",map[string]any{"expectedRevision":a.TagsRevision,"tags":[]string{}},200);decode(t,r,&a);if len(a.Tags)!=0{t.Fatal("explicit empty set was not saved")}
 f.json("PUT","/assets/"+a.ID+"/tags","tag-stale-3",map[string]any{"expectedRevision":1,"tags":[]string{"resurrect"}},409)
 f.json("PUT","/assets/"+a.ID+"/reaction","like-once-1",map[string]any{"favorite":true,"liked":true},200)
 r=f.json("PUT","/assets/"+a.ID+"/reaction","like-once-2",map[string]any{"favorite":true,"liked":true},200);decode(t,r,&a);if a.LikeCount!=1||!a.Liked{t.Fatalf("daily likes: %+v",a)}
 r=f.request("GET","/assets/"+a.ID+"/content","",testSecret,nil,map[string]string{"Range":"bytes=0-15"});if r.Code!=206||!bytes.Equal(r.Body.Bytes(),body[:16]){t.Fatal("original byte range was not preserved")}
 r=f.request("GET","/assets/"+a.ID+"/thumbnail","",testSecret,nil,nil);if r.Code!=200||r.Header().Get("Content-Type")!="image/jpeg"{t.Fatalf("thumbnail: %d %s",r.Code,r.Body.String())}
 r=f.json("POST","/assets/"+a.ID+"/move","move-file-1",map[string]any{"expectedRevision":a.Revision,"relativePath":"sub/renamed.png"},200);decode(t,r,&a)
 r=f.json("DELETE","/assets/"+a.ID+"?expectedRevision="+strconv.FormatInt(a.Revision,10),"trash-file-1",struct{}{},200);decode(t,r,&a);if !a.Trashed{t.Fatal("not trashed")}
 r=f.json("POST","/trash/"+a.ID+"/restore","restore-file-1",map[string]any{"expectedRevision":a.Revision},200);decode(t,r,&a);if a.Trashed||!a.Favorite||a.LikeCount!=1{t.Fatal("restore lost relationships")}
 r=f.json("GET","/assets","",nil,200);var page model.Page[model.Asset];decode(t,r,&page);if len(page.Items)!=1||page.Items[0].ID!=a.ID{t.Fatal("asset identity was not conserved")}
}
func TestReplacementPreservesIdentityAndRevisionCheckedMetadata(t *testing.T){
 f:=setup(t);u,_:=f.upload("first.png",10,nil);a:=*f.complete(u).Asset
 r:=f.json("PUT","/assets/"+a.ID+"/tags","replace-tags-1",map[string]any{"expectedRevision":a.TagsRevision,"tags":[]string{"keep"}},200);decode(t,r,&a)
 replacement,_:=f.upload("replacement.png",80,map[string]any{"replaceAssetId":a.ID,"expectedContentVersion":a.ContentVersion});updated:=f.complete(replacement);if updated.Asset.ID!=a.ID||updated.Asset.ContentVersion!=2||len(updated.Asset.Tags)!=1||updated.Asset.Tags[0]!="keep"{t.Fatal("replacement changed identity or relationships")}
 for _,suffix:=range []string{"content","thumbnail"}{r:=f.request("GET","/assets/"+a.ID+"/"+suffix+"?version=1","",testSecret,nil,nil);if r.Code!=409{t.Fatalf("old %s version accepted: %d",suffix,r.Code)}}
 duplicate,_:=f.upload("duplicate.png",80,nil);result:=f.complete(duplicate);if result.Upload.State!="duplicate"||result.Asset!=nil{t.Fatal("exact duplicate silently ingested")}
}

func TestCreatePayloadsDoNotAcceptStoredStateAndKeepExplicitZeroBudget(t *testing.T){
 f:=setup(t)
 f.json("POST","/batches","reject-batch-state",map[string]any{"libraryId":f.library,"mode":"intelligent","label":"Synthetic","state":"completed","researchCalls":-100},400)
 f.json("POST","/collections","reject-collection-state",map[string]any{"libraryId":f.library,"name":"Synthetic","kind":"image_set","id":"forged"},400)
 r:=f.json("POST","/batches","default-research-budget",map[string]any{"libraryId":f.library,"mode":"intelligent","label":"Default budget"},201);var b model.Batch;decode(t,r,&b);if b.MaxResearchCalls!=8{t.Fatalf("default budget = %d",b.MaxResearchCalls)}
 r=f.json("POST","/batches","zero-research-budget",map[string]any{"libraryId":f.library,"mode":"intelligent","label":"No research","maxResearchCalls":0},201);decode(t,r,&b);if b.MaxResearchCalls!=0{t.Fatalf("explicit zero budget = %d",b.MaxResearchCalls)}
}
func TestConcurrentCompletionReplaysOneAsset(t *testing.T){
 f:=setup(t);u,_:=f.upload("concurrent.png",40,nil);var wg sync.WaitGroup;var mu sync.Mutex;ids:=[]string{}
 for n:=0;n<8;n++{wg.Add(1);go func(){defer wg.Done();r:=f.request("POST","/uploads/"+u.ID+"/complete","same-completion-key",testSecret,[]byte("{}"),nil);if r.Code!=200{t.Errorf("completion status %d",r.Code);return};var out model.UploadResult;if e:=json.Unmarshal(r.Body.Bytes(),&out);e!=nil{t.Error(e);return};mu.Lock();ids=append(ids,out.Asset.ID);mu.Unlock()}()};wg.Wait();if len(ids)!=8{t.Fatal(ids)};for _,id:=range ids{if id!=ids[0]{t.Fatal("duplicate asset")}}
}
func TestBatchBoundCredentialCannotWriteOutsideTask(t *testing.T){
 f:=setup(t);r:=f.json("POST","/batches","batch-create-1",map[string]any{"libraryId":f.library,"mode":"intelligent","label":"Synthetic batch","maxResearchCalls":1},201);var b model.Batch;decode(t,r,&b)
 r=f.json("POST","/credentials","",map[string]any{"label":"Synthetic AI","batchId":b.ID,"scopes":[]string{"read","ingest","research","organize"}},201);var credential model.Principal;decode(t,r,&credential)
 body:=pngBytes(t,50);in:=map[string]any{"libraryId":f.library,"fileName":"research.png","size":len(body),"mimeType":"image/png","mode":"intelligent","batchId":b.ID};raw,_:=json.Marshal(in)
 r=f.request("POST","/uploads","ai-create-upload-1",credential.Token,raw,nil);if r.Code!=201{t.Fatal(r.Body.String())};var u model.Upload;decode(t,r,&u)
 r=f.request("PUT","/uploads/"+u.ID+"/content","",credential.Token,body,map[string]string{"Upload-Offset":"0"});if r.Code!=200{t.Fatal(r.Body.String())}
 r=f.request("POST","/uploads/"+u.ID+"/complete","ai-complete-upload-1",credential.Token,[]byte("{}"),nil);if r.Code!=200{t.Fatal(r.Body.String())};var staged model.UploadResult;decode(t,r,&staged);if staged.Upload.State!="staged"||staged.Asset!=nil{t.Fatal("intelligent upload prematurely ingested")}
 r=f.request("POST","/libraries","ai-outside-scope-1",credential.Token,[]byte(`{"name":"Forbidden","kind":"regular"}`),nil);if r.Code!=403{t.Fatalf("AI administrative write: %d",r.Code)}
 r=f.request("GET","/ai/tasks/"+b.ID+"/context","",credential.Token,nil,nil);if r.Code!=200{t.Fatal(r.Body.String())}
 var ctx struct{Batch model.Batch `json:"batch"`};decode(t,r,&ctx)
 planRaw,_:=json.Marshal(map[string]any{"expectedRevision":ctx.Batch.Revision,"items":[]any{map[string]any{"uploadId":u.ID,"family":"未知作品"}}})
 r=f.request("POST","/batches/"+b.ID+"/plans","ai-plan-prepare-1",credential.Token,planRaw,nil);if r.Code!=201{t.Fatal(r.Body.String())};var p model.Plan;decode(t,r,&p)
 commitRaw,_:=json.Marshal(map[string]string{"fingerprint":p.Fingerprint});r=f.request("POST","/batches/"+b.ID+"/plans/"+p.ID+"/commit","ai-plan-commit-1",credential.Token,commitRaw,nil);if r.Code!=200{t.Fatal(r.Body.String())};var detail model.BatchDetail;decode(t,r,&detail);if detail.Items[0].Upload.State!="ingested"||detail.Batch.State!="completed"{t.Fatal("unknown author did not complete normally")}
 r=f.request("GET","/operations/ai-plan-prepare-1","",credential.Token,nil,nil);if r.Code!=200||!strings.Contains(r.Body.String(),`"committed"`){t.Fatal("missing durable receipt")}
}
func TestCookieOriginAndInvalidMedia(t *testing.T){
 f:=setup(t);r:=f.request("POST","/sessions","","",[]byte(`{"token":"`+testSecret+`","label":"Browser"}`),nil);if r.Code!=201{t.Fatal(r.Body.String())};cookies:=r.Result().Cookies();if len(cookies)==0||!cookies[0].HttpOnly||cookies[0].SameSite!=http.SameSiteStrictMode{t.Fatal("session cookie is not protected")}
 req:=httptest.NewRequest("POST","/api/v1/libraries",strings.NewReader(`{"name":"blocked","kind":"regular"}`));req.AddCookie(cookies[0]);req.Header.Set("Origin","https://unrelated.invalid");req.Header.Set("Idempotency-Key","cross-origin-key");rr:=httptest.NewRecorder();f.h.ServeHTTP(rr,req);if rr.Code!=403{t.Fatal("cross-origin cookie write accepted")}
 r=f.json("POST","/uploads","invalid-path-upload",map[string]any{"libraryId":f.library,"fileName":"../escape.png","size":10,"mimeType":"image/png","mode":"ordinary"},400);io.Copy(io.Discard,r.Result().Body)
}

func TestExpectedSourceHashRejectsMixedOrChangedBytesBeforePublication(t *testing.T){
 f:=setup(t);body:=pngBytes(t,33);wrong:=sha256.Sum256([]byte("a different source snapshot"))
 in:=map[string]any{"libraryId":f.library,"fileName":"hash-guard.png","size":len(body),"mimeType":"image/png","mode":"ordinary","expectedHash":fmt.Sprintf("%x",wrong)}
 r:=f.json("POST","/uploads","source-hash-create",in,201);var upload model.Upload;decode(t,r,&upload)
 r=f.request("PUT","/uploads/"+upload.ID+"/content","",testSecret,body,map[string]string{"Upload-Offset":"0"});if r.Code!=200{t.Fatal(r.Body.String())}
 f.json("POST","/uploads/"+upload.ID+"/complete","source-hash-complete",struct{}{},409)
 r=f.json("GET","/assets","",nil,200);var assets model.Page[model.Asset];decode(t,r,&assets);if len(assets.Items)!=0{t.Fatal("hash-mismatched source was published")}
 in["expectedHash"]=fmt.Sprintf("%x",sha256.Sum256(body));f.json("POST","/uploads","source-hash-create",in,409)
 r=f.json("POST","/uploads","source-hash-correct-create",in,201);decode(t,r,&upload)
 r=f.request("PUT","/uploads/"+upload.ID+"/content","",testSecret,body,map[string]string{"Upload-Offset":"0"});if r.Code!=200{t.Fatal(r.Body.String())};completed:=f.complete(upload);if completed.Asset==nil||completed.Asset.Hash!=in["expectedHash"]{t.Fatal("matching snapshot did not publish exactly")}
}
