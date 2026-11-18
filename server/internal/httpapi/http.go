// Package httpapi translates the versioned protocol into application use cases.
package httpapi

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "errors"
 "io"
 "log/slog"
 "net/http"
 "net/url"
 "strconv"
 "strings"
 "time"

 "qimeng.local/next/server/internal/model"
 "qimeng.local/next/server/internal/service"
)

type principalKey struct{}
type API struct{s *service.Service;mux *http.ServeMux}
func New(s *service.Service)http.Handler{a:=&API{s:s,mux:http.NewServeMux()};a.routes();return a.mux}
func principal(r *http.Request)model.Principal{return r.Context().Value(principalKey{}).(model.Principal)}
func fail(w http.ResponseWriter,err error){
 var app *service.Error;if errors.As(service.Normalize(err),&app){respond(w,app.Status,app);return}
 // Never log credentials, source names, private paths or driver error strings.
 slog.Error("request failed","errorType",errorType(err));respond(w,500,&service.Error{Code:"internal_error",Message:"服务器操作失败，原内容保留。请使用同一操作身份回读状态。"})
}
func errorType(err error)string{if err==nil{return "none"};return "io_or_persistence"}
func respond(w http.ResponseWriter,status int,value any){w.Header().Set("Content-Type","application/json; charset=utf-8");w.Header().Set("Cache-Control","no-store");w.WriteHeader(status);if status!=204{if e:=json.NewEncoder(w).Encode(value);e!=nil{slog.Error("response write failed")}}}
func result(w http.ResponseWriter,r service.Result){w.Header().Set("Content-Type","application/json; charset=utf-8");w.Header().Set("Cache-Control","no-store");w.WriteHeader(r.Status);if len(r.Body)>0{if _,e:=w.Write(r.Body);e!=nil{slog.Error("response write failed")}}}
func originOK(r *http.Request,secureOrigin ...bool)bool{o:=r.Header.Get("Origin");if o==""{return true};u,e:=url.Parse(o);if e!=nil{return false};scheme:="http";if r.TLS!=nil||len(secureOrigin)>0&&secureOrigin[0]{scheme="https"};return u.Scheme==scheme&&u.Host==r.Host&&u.User==nil}
func(a *API)auth(next http.HandlerFunc)http.HandlerFunc{return func(w http.ResponseWriter,r *http.Request){
 token:=strings.TrimPrefix(r.Header.Get("Authorization"),"Bearer ");if token==r.Header.Get("Authorization")&&token!=""{fail(w,service.Fail(401,"invalid_authorization","仅支持 Bearer 凭据。"));return}
 cookieAuth:=false;if token==""{if c,e:=r.Cookie("qimeng_session");e==nil{token=c.Value;cookieAuth=true}}
 if cookieAuth&&r.Method!="GET"&&r.Method!="HEAD"&&!originOK(r,a.s.Config.SecureCookies){fail(w,service.Fail(403,"origin_denied","请求来源与当前服务不一致。"));return}
 p,e:=a.s.Authenticate(r.Context(),token);if e!=nil{fail(w,e);return};if e=service.Authorize(p,"read","");e!=nil{fail(w,e);return};next(w,r.WithContext(context.WithValue(r.Context(),principalKey{},p)))
}}
func(a *API)get(pattern string,fn func(*http.Request)(any,error)){a.mux.HandleFunc("GET /api/v1"+pattern,a.auth(func(w http.ResponseWriter,r *http.Request){v,e:=fn(r);if e!=nil{fail(w,e);return};respond(w,200,v)}))}
func decode[T any](w http.ResponseWriter,r *http.Request)(T,[]byte,error){
 var v T;r.Body=http.MaxBytesReader(w,r.Body,1<<20);raw,e:=io.ReadAll(r.Body);if e!=nil{return v,nil,service.Fail(413,"body_too_large","请求正文超出限制。")};if len(raw)==0{raw=[]byte("{}")};d:=json.NewDecoder(strings.NewReader(string(raw)));d.DisallowUnknownFields();if e=d.Decode(&v);e!=nil{return v,nil,service.Fail(400,"invalid_json","请求字段或 JSON 格式无效。")};if d.Decode(new(any))!=io.EOF{return v,nil,service.Fail(400,"invalid_json","请求包含多余 JSON 数据。")};canonical,e:=json.Marshal(v);return v,canonical,e
}
func write[T any](a *API,method,pattern string,idempotent bool,fn func(*http.Request,*service.Write,T)(any,int,error)){
 a.mux.HandleFunc(method+" /api/v1"+pattern,a.auth(func(w http.ResponseWriter,r *http.Request){
  in,raw,e:=decode[T](w,r);if e!=nil{fail(w,e);return};key:=r.Header.Get("Idempotency-Key");if idempotent&&key==""{fail(w,service.Fail(400,"idempotency_required","此操作必须提供稳定幂等键。"));return}
  // Include query preconditions in the receipt identity as well as the body.
  raw=append(raw,[]byte("\x00"+r.URL.Query().Encode())...)
  out,e:=a.s.Run(r.Context(),principal(r),r.Method+" "+r.URL.Path,key,raw,func(tx *service.Write)(any,int,error){return fn(r,tx,in)});if e!=nil{fail(w,e);return};result(w,out)
 }))
}
func limit(r *http.Request)(int,error){v:=r.URL.Query().Get("limit");if v==""{return 0,nil};n,e:=strconv.Atoi(v);if e!=nil{return 0,service.Fail(400,"invalid_limit","分页数量无效。")};return n,nil}
func revision(r *http.Request)(int64,error){v,e:=strconv.ParseInt(r.URL.Query().Get("expectedRevision"),10,64);if e!=nil||v<1{return 0,service.Fail(400,"revision_required","必须提供当前修订号。")};return v,nil}
func contentVersion(r *http.Request)(int64,error){raw:=r.URL.Query().Get("version");if raw==""{return 0,nil};v,e:=strconv.ParseInt(raw,10,64);if e!=nil||v<1{return 0,service.Fail(400,"invalid_version","内容版本必须为正整数。")};return v,nil}
func boolQuery(r *http.Request,name string)(*bool,error){v:=r.URL.Query().Get(name);if v==""{return nil,nil};b,e:=strconv.ParseBool(v);if e!=nil{return nil,service.Fail(400,"invalid_filter","筛选参数无效。")};return &b,nil}
type revisionBody struct{ExpectedRevision int64 `json:"expectedRevision"`}
type batchCreateRequest struct{LibraryID string `json:"libraryId"`;Mode string `json:"mode"`;Label string `json:"label"`;MaxResearchCalls *int `json:"maxResearchCalls"`}
type collectionCreateRequest struct{LibraryID string `json:"libraryId"`;Name string `json:"name"`;Kind string `json:"kind"`;Aliases []string `json:"aliases"`}
type libraryCreateRequest struct{Name string `json:"name"`;Kind string `json:"kind"`;StorageID string `json:"storageId"`;RootRelativePath string `json:"rootRelativePath"`}
type eventCreateRequest struct{EventID string `json:"eventId"`;Kind string `json:"kind"`;ContentVersion int64 `json:"contentVersion"`;PositionMs int64 `json:"positionMs"`;SessionID string `json:"sessionId,omitempty"`;Seconds float64 `json:"seconds,omitempty"`;OccurredAt *time.Time `json:"occurredAt,omitempty"`}
func(a *API)routes(){
 a.mux.HandleFunc("GET /healthz",func(w http.ResponseWriter,r *http.Request){if e:=a.s.DB.DB.PingContext(r.Context());e!=nil{fail(w,e);return};respond(w,200,map[string]string{"status":"ok"})})
 a.mux.HandleFunc("GET /api/v1/instance",func(w http.ResponseWriter,r *http.Request){respond(w,200,map[string]any{"id":a.s.InstanceID,"name":"绮梦 Next","apiVersion":"0.1.0","capabilities":map[string]bool{"aiConfigured":a.s.Config.AIConfigured,"recommendations":true,"rankings":true,"stats":true,"backups":true,"migration":false}})})
 a.mux.HandleFunc("POST /api/v1/sessions",func(w http.ResponseWriter,r *http.Request){
  if !originOK(r,a.s.Config.SecureCookies){fail(w,service.Fail(403,"origin_denied","请求来源不匹配。"));return};in,_,e:=decode[service.SessionInput](w,r);if e!=nil{fail(w,e);return};p,e:=a.s.AuthenticateLogin(r.Context(),in);if e!=nil{fail(w,e);return}
  out,e:=a.s.Run(r.Context(),p,"createSession","",nil,func(tx *service.Write)(any,int,error){v,e:=a.s.CreateSession(r.Context(),tx,in.Label,"",nil);return v,201,e});if e!=nil{fail(w,e);return};var session model.Principal;if e=json.Unmarshal(out.Body,&session);e!=nil{fail(w,e);return};http.SetCookie(w,&http.Cookie{Name:"qimeng_session",Value:session.Token,Path:"/",HttpOnly:true,Secure:a.s.Config.SecureCookies,SameSite:http.SameSiteStrictMode,Expires:session.ExpiresAt});result(w,out)
 })
 a.get("/sessions/current",func(r *http.Request)(any,error){return principal(r),nil})
 a.mux.HandleFunc("DELETE /api/v1/sessions/current",a.auth(func(w http.ResponseWriter,r *http.Request){p:=principal(r);out,e:=a.s.Run(r.Context(),p,"logout","",nil,func(tx *service.Write)(any,int,error){return nil,204,a.s.Revoke(r.Context(),tx,p.ID)});if e!=nil{fail(w,e);return};http.SetCookie(w,&http.Cookie{Name:"qimeng_session",Value:"",Path:"/",HttpOnly:true,Secure:a.s.Config.SecureCookies,SameSite:http.SameSiteStrictMode,MaxAge:-1});result(w,out)}))
 write[credentialRequest](a,"POST","/credentials",false,func(r *http.Request,w *service.Write,in credentialRequest)(any,int,error){if (in.BatchID=="")==(in.AuthorTaskID==""){return nil,0,service.Fail(400,"task_required","AI 凭据必须且只能绑定一个接收或资料维护任务。")};v,e:=a.s.CreateSession(r.Context(),w,in.Label,in.BatchID,in.Scopes,in.AuthorTaskID);return v,201,e})
 write[struct{}](a,"DELETE","/credentials/{id}",false,func(r *http.Request,w *service.Write,_ struct{})(any,int,error){return nil,204,a.s.Revoke(r.Context(),w,r.PathValue("id"))})
 a.get("/libraries",func(r *http.Request)(any,error){return a.s.Libraries(r.Context())})
 a.get("/storages",func(r *http.Request)(any,error){return a.s.Storages(),nil})
 a.libraryLocationRoutes()
 a.authorListRoutes()
 write[libraryCreateRequest](a,"POST","/libraries",true,func(r *http.Request,w *service.Write,in libraryCreateRequest)(any,int,error){v,e:=a.s.CreateLibrary(r.Context(),w,in.Name,in.Kind,in.StorageID,in.RootRelativePath);return v,201,e})
 a.get("/assets",func(r *http.Request)(any,error){f,e:=assetFilterRequest(r);if e!=nil{return nil,e};return a.s.Assets(r.Context(),f)})
 a.get("/assets/{id}",func(r *http.Request)(any,error){return a.s.Asset(r.Context(),r.PathValue("id"))})
 write[service.AssetUpdate](a,"PATCH","/assets/{id}",false,func(r *http.Request,w *service.Write,in service.AssetUpdate)(any,int,error){v,e:=a.s.UpdateAsset(r.Context(),w,r.PathValue("id"),in);return v,200,e})
 write[struct{ExpectedRevision int64 `json:"expectedRevision"`;Tags []string `json:"tags"`}](a,"PUT","/assets/{id}/tags",true,func(r *http.Request,w *service.Write,in struct{ExpectedRevision int64 `json:"expectedRevision"`;Tags []string `json:"tags"`})(any,int,error){v,e:=a.s.UpdateTags(r.Context(),w,r.PathValue("id"),in.ExpectedRevision,in.Tags);return v,200,e})
 write[struct{Favorite bool `json:"favorite"`;Liked bool `json:"liked"`}](a,"PUT","/assets/{id}/reaction",true,func(r *http.Request,w *service.Write,in struct{Favorite bool `json:"favorite"`;Liked bool `json:"liked"`})(any,int,error){v,e:=a.s.Reaction(r.Context(),w,r.PathValue("id"),in.Favorite,in.Liked);return v,200,e})
 write[eventCreateRequest](a,"POST","/assets/{id}/events",false,func(r *http.Request,w *service.Write,in eventCreateRequest)(any,int,error){v,e:=a.s.RecordEvent(r.Context(),w,r.PathValue("id"),model.Event{EventID:in.EventID,Kind:in.Kind,ContentVersion:in.ContentVersion,PositionMs:in.PositionMs,SessionID:in.SessionID,Seconds:in.Seconds,OccurredAt:in.OccurredAt});return v,200,e})
 a.mux.HandleFunc("GET /api/v1/assets/{id}/content",a.auth(func(w http.ResponseWriter,r *http.Request){v,e:=contentVersion(r);if e!=nil{fail(w,e);return};f,asset,e:=a.s.Content(r.Context(),r.PathValue("id"),v);if e!=nil{fail(w,e);return};defer f.Close();w.Header().Set("Content-Type",asset.MimeType);w.Header().Set("X-Content-Type-Options","nosniff");w.Header().Set("ETag",`"`+asset.Hash+`"`);w.Header().Set("Cache-Control","private, no-cache");http.ServeContent(w,r,asset.FileName,asset.UpdatedAt,f)}))
 a.mux.HandleFunc("GET /api/v1/assets/{id}/thumbnail",a.auth(func(w http.ResponseWriter,r *http.Request){v,e:=contentVersion(r);if e!=nil{fail(w,e);return};f,asset,e:=a.s.ThumbnailSize(r.Context(),r.PathValue("id"),v,r.URL.Query().Get("size"));if e!=nil{fail(w,e);return};defer f.Close();hash:=sha256.New();if _,e=io.Copy(hash,f);e!=nil{fail(w,e);return};if _,e=f.Seek(0,io.SeekStart);e!=nil{fail(w,e);return};w.Header().Set("Content-Type","image/jpeg");w.Header().Set("X-Content-Type-Options","nosniff");w.Header().Set("Cache-Control","private, no-cache");w.Header().Set("ETag",`"thumb-`+hex.EncodeToString(hash.Sum(nil))+`"`);http.ServeContent(w,r,"thumbnail.jpg",asset.UpdatedAt,f)}))
 write[struct{ExpectedRevision int64 `json:"expectedRevision"`;RelativePath string `json:"relativePath"`}](a,"POST","/assets/{id}/move",true,func(r *http.Request,w *service.Write,in struct{ExpectedRevision int64 `json:"expectedRevision"`;RelativePath string `json:"relativePath"`})(any,int,error){v,e:=a.s.Move(r.Context(),w,r.PathValue("id"),in.RelativePath,in.ExpectedRevision);return v,200,e})
 write[struct{}](a,"DELETE","/assets/{id}",true,func(r *http.Request,w *service.Write,_ struct{})(any,int,error){rev,e:=revision(r);if e!=nil{return nil,0,e};v,e:=a.s.Trash(r.Context(),w,r.PathValue("id"),rev);return v,200,e})
 a.get("/tags",func(r *http.Request)(any,error){n,e:=limit(r);if e!=nil{return nil,e};return a.s.Tags(r.Context(),r.URL.Query().Get("cursor"),n)})
 a.get("/history",func(r *http.Request)(any,error){f,e:=assetFilterRequest(r);if e!=nil{return nil,e};return a.s.FilteredHistory(r.Context(),f)})
 a.get("/authors",func(r *http.Request)(any,error){n,e:=limit(r);if e!=nil{return nil,e};f,e:=boolQuery(r,"following");if e!=nil{return nil,e};return a.s.Authors(r.Context(),r.URL.Query().Get("q"),r.URL.Query().Get("cursor"),n,f!=nil&&*f,r.URL.Query().Get("kind"),r.URL.Query().Get("sort"))})
 a.get("/authors/{id}",func(r *http.Request)(any,error){return a.s.Author(r.Context(),r.PathValue("id"))})
 write[service.AuthorInput](a,"POST","/authors",true,func(r *http.Request,w *service.Write,in service.AuthorInput)(any,int,error){v,e:=a.s.CreateAuthor(r.Context(),w,in);return v,201,e})
 write[service.AuthorUpdate](a,"PATCH","/authors/{id}",true,func(r *http.Request,w *service.Write,in service.AuthorUpdate)(any,int,error){v,e:=a.s.UpdateAuthor(r.Context(),w,r.PathValue("id"),in);return v,200,e})
 a.get("/collections",func(r *http.Request)(any,error){n,e:=limit(r);if e!=nil{return nil,e};q:=r.URL.Query();return a.s.Collections(r.Context(),q.Get("libraryId"),q.Get("q"),q.Get("cursor"),n)})
 write[collectionCreateRequest](a,"POST","/collections",true,func(r *http.Request,w *service.Write,in collectionCreateRequest)(any,int,error){v,e:=a.s.CreateCollection(r.Context(),w,model.Collection{LibraryID:in.LibraryID,Name:in.Name,Kind:in.Kind,Aliases:in.Aliases});return v,201,e})
 a.uploadRoutes();a.batchRoutes()
 a.get("/trash",func(r *http.Request)(any,error){n,e:=limit(r);if e!=nil{return nil,e};return a.s.Assets(r.Context(),service.AssetFilter{Trashed:true,Cursor:r.URL.Query().Get("cursor"),Limit:n})})
 write[revisionBody](a,"POST","/trash/{id}/restore",true,func(r *http.Request,w *service.Write,in revisionBody)(any,int,error){v,e:=a.s.Restore(r.Context(),w,r.PathValue("id"),in.ExpectedRevision);return v,200,e})
 write[struct{}](a,"DELETE","/trash/{id}",true,func(r *http.Request,w *service.Write,_ struct{})(any,int,error){rev,e:=revision(r);if e!=nil{return nil,0,e};return nil,204,a.s.Purge(r.Context(),w,r.PathValue("id"),rev)})
 a.get("/stats",func(r *http.Request)(any,error){return a.s.Stats(r.Context())})
 a.get("/settings",func(r *http.Request)(any,error){return a.s.Settings(r.Context())})
 write[service.SettingsUpdate](a,"PUT","/settings",true,func(r *http.Request,w *service.Write,in service.SettingsUpdate)(any,int,error){v,e:=a.s.UpdateSettings(r.Context(),w,in);return v,200,e})
 write[struct{}](a,"POST","/backups",true,func(r *http.Request,w *service.Write,_ struct{})(any,int,error){v,e:=a.s.CreateBackup(r.Context(),w);return v,201,e})
 a.get("/operations/{key}",func(r *http.Request)(any,error){return a.s.Operation(r.Context(),principal(r),r.PathValue("key"),r.URL.Query().Get("operation"))})
 a.analyticsRoutes()
 a.authorTaskRoutes()
 a.delegationRoutes()
 a.researchRoutes()
 a.managementRoutes()
 a.scanRoutes()
 a.authorProfileRoutes()
 a.namingRoutes()
 a.migrationRoutes()
 a.backupRoutes()
 a.browseRoutes()
}
func(a *API)uploadRoutes(){
 write[model.UploadInput](a,"POST","/uploads",true,func(r *http.Request,w *service.Write,in model.UploadInput)(any,int,error){v,e:=a.s.CreateUpload(r.Context(),w,in);return v,201,e})
 a.get("/uploads/{id}",func(r *http.Request)(any,error){return a.s.Upload(r.Context(),principal(r),r.PathValue("id"))})
 write[struct{}](a,"DELETE","/uploads/{id}",false,func(r *http.Request,w *service.Write,_ struct{})(any,int,error){return nil,204,a.s.CancelUpload(r.Context(),w,r.PathValue("id"))})
 a.mux.HandleFunc("PUT /api/v1/uploads/{id}/content",a.auth(func(w http.ResponseWriter,r *http.Request){offset,e:=strconv.ParseInt(r.Header.Get("Upload-Offset"),10,64);if e!=nil{fail(w,service.Fail(400,"offset_required","必须提供有效 Upload-Offset。"));return};body,e:=io.ReadAll(http.MaxBytesReader(w,r.Body,service.MaxChunkBytes));if e!=nil{fail(w,service.Fail(413,"chunk_too_large","单个分片最多 8 MiB。"));return};out,e:=a.s.Run(r.Context(),principal(r),"appendUpload:"+r.PathValue("id"),"",nil,func(tx *service.Write)(any,int,error){v,e:=a.s.AppendUpload(r.Context(),tx,r.PathValue("id"),offset,body);return v,200,e});if e!=nil{fail(w,e);return};result(w,out)}))
 write[struct{}](a,"POST","/uploads/{id}/complete",true,func(r *http.Request,w *service.Write,_ struct{})(any,int,error){v,e:=a.s.CompleteUpload(r.Context(),w,r.PathValue("id"));return v,200,e})
}
func(a *API)batchRoutes(){
 a.get("/batches",func(r *http.Request)(any,error){n,e:=limit(r);if e!=nil{return nil,e};return a.s.Batches(r.Context(),principal(r),r.URL.Query().Get("cursor"),n)})
 write[batchCreateRequest](a,"POST","/batches",true,func(r *http.Request,w *service.Write,in batchCreateRequest)(any,int,error){budget:=8;if in.MaxResearchCalls!=nil{budget=*in.MaxResearchCalls};v,e:=a.s.CreateBatch(r.Context(),w,model.Batch{LibraryID:in.LibraryID,Mode:in.Mode,Label:in.Label,MaxResearchCalls:budget});return v,201,e})
 a.get("/batches/{id}",func(r *http.Request)(any,error){return a.s.BatchDetail(r.Context(),principal(r),r.PathValue("id"))})
 write[model.BatchInputsSet](a,"PUT","/batches/{id}/inputs",true,func(r *http.Request,w *service.Write,in model.BatchInputsSet)(any,int,error){v,e:=a.s.SetBatchInputs(r.Context(),w,r.PathValue("id"),in);return v,200,e})
 write[struct{ManifestFingerprint string `json:"manifestFingerprint"`}](a,"POST","/batches/{id}/inputs/{inputId}/cancel",true,func(r *http.Request,w *service.Write,in struct{ManifestFingerprint string `json:"manifestFingerprint"`})(any,int,error){v,e:=a.s.CancelBatchInput(r.Context(),w,r.PathValue("id"),r.PathValue("inputId"),in.ManifestFingerprint);return v,200,e})
 write[revisionBody](a,"POST","/batches/{id}/cancel",false,func(r *http.Request,w *service.Write,in revisionBody)(any,int,error){v,e:=a.s.CancelBatch(r.Context(),w,r.PathValue("id"),in.ExpectedRevision);return v,200,e})
 a.get("/ai/tasks/{id}/context",func(r *http.Request)(any,error){n,e:=limit(r);if e!=nil{return nil,e};return a.s.Context(r.Context(),principal(r),r.PathValue("id"),r.URL.Query().Get("cursor"),n)})
 write[model.Research](a,"POST","/batches/{id}/research",true,func(r *http.Request,w *service.Write,in model.Research)(any,int,error){v,e:=a.s.SubmitResearch(r.Context(),w,r.PathValue("id"),in);return v,200,e})
 write[model.PlanCreate](a,"POST","/batches/{id}/plans",true,func(r *http.Request,w *service.Write,in model.PlanCreate)(any,int,error){v,e:=a.s.PreparePlan(r.Context(),w,r.PathValue("id"),in);return v,201,e})
 a.get("/batches/{id}/plans/{planId}",func(r *http.Request)(any,error){return a.s.Plan(r.Context(),principal(r),r.PathValue("id"),r.PathValue("planId"))})
 a.mux.HandleFunc("POST /api/v1/batches/{id}/plans/{planId}/commit",a.auth(func(w http.ResponseWriter,r *http.Request){in,_,e:=decode[struct{Fingerprint string `json:"fingerprint"`}](w,r);if e!=nil{fail(w,e);return};key:=r.Header.Get("Idempotency-Key");if len(key)<8||len(key)>128{fail(w,service.Fail(400,"idempotency_required","提交计划须提供稳定幂等键。"));return};out,e:=a.s.CommitPlan(r.Context(),principal(r),r.PathValue("id"),r.PathValue("planId"),in.Fingerprint,key);if e!=nil{fail(w,e);return};result(w,out)}))
}
