package httpapi

import (
 "bytes"
 "io"
 "net/http"
 "strconv"

 "qimeng.local/next/server/internal/service"
)

func profileRevision(r *http.Request)(int64,error) {
 value,err:=strconv.ParseInt(r.URL.Query().Get("expectedRevision"),10,64)
 if err!=nil || value<0 { return 0,service.Fail(400,"revision_required","必须提供当前资料块修订号。") }
 return value,nil
}

func (a *API) authorProfileRoutes() {
 a.get("/authors/{id}/updates",func(r *http.Request)(any,error){return a.s.AuthorUpdates(r.Context(),r.PathValue("id"))})
 write[service.AuthorUpdatesInput](a,"PUT","/authors/{id}/updates",true,func(r *http.Request,w *service.Write,in service.AuthorUpdatesInput)(any,int,error){v,e:=a.s.SetAuthorUpdates(r.Context(),w,r.PathValue("id"),in);return v,200,e})
 a.mux.HandleFunc("POST /api/v1/authors/{id}/updates/check",a.auth(func(w http.ResponseWriter,r *http.Request){
  in,raw,e:=decode[service.AuthorUpdateCheck](w,r);if e!=nil{fail(w,e);return}
  v,e:=a.s.CheckAuthorUpdates(r.Context(),principal(r),r.PathValue("id"),r.Header.Get("Idempotency-Key"),raw,in);if e!=nil{fail(w,e);return};result(w,v)
 }))
 a.get("/authors/{id}/profile",func(r *http.Request)(any,error){
  return a.s.AuthorProfile(r.Context(),principal(r),r.PathValue("id"))
 })
 a.get("/authors/{id}/profile-summary",func(r *http.Request)(any,error){
  return a.s.AuthorProfileSummary(r.Context(),principal(r),r.PathValue("id"))
 })
 write[service.ProfileWrite](a,"PATCH","/authors/{id}/profile/{block}",true,func(r *http.Request,w *service.Write,in service.ProfileWrite)(any,int,error){
  value,err:=a.s.UpdateAuthorProfile(r.Context(),w,r.PathValue("id"),r.PathValue("block"),in)
  return value,200,err
 })
 a.get("/authors/{id}/profile/candidates",func(r *http.Request)(any,error){
  n,err:=limit(r);if err!=nil { return nil,err }
  return a.s.AuthorProfileCandidates(r.Context(),r.PathValue("id"),r.URL.Query().Get("archiveId"),r.URL.Query().Get("cursor"),n)
 })
 write[service.ProfileCandidateCreate](a,"POST","/authors/{id}/profile/candidates",true,func(r *http.Request,w *service.Write,in service.ProfileCandidateCreate)(any,int,error){
  value,err:=a.s.SubmitAuthorProfileCandidate(r.Context(),w,r.PathValue("id"),in)
  return value,201,err
 })
 write[struct{}](a,"POST","/authors/{id}/profile/candidates/{candidateId}/dismiss",true,func(r *http.Request,w *service.Write,_ struct{})(any,int,error){
  value,err:=a.s.DismissAuthorProfileCandidate(r.Context(),w,r.PathValue("id"),r.PathValue("candidateId"))
  return value,200,err
 })
 a.mux.HandleFunc("POST /api/v1/authors/{id}/profile/avatar-from-url",a.auth(func(w http.ResponseWriter,r *http.Request){
  in,raw,err:=decode[service.ProfileAvatarFetch](w,r);if err!=nil { fail(w,err);return }
  raw=append(raw,[]byte("\x00"+r.URL.Query().Encode())...)
  value,err:=a.s.FetchAuthorProfileAvatar(r.Context(),principal(r),r.PathValue("id"),r.Header.Get("Idempotency-Key"),raw,in)
  if err!=nil { fail(w,err);return };result(w,value)
 }))
 a.mux.HandleFunc("PUT /api/v1/authors/{id}/profile/avatar",a.auth(func(w http.ResponseWriter,r *http.Request){
  if err:=service.Authorize(principal(r),"admin","");err!=nil { fail(w,err);return }
  revision,err:=profileRevision(r);if err!=nil { fail(w,err);return }
  body,err:=io.ReadAll(http.MaxBytesReader(w,r.Body,4<<20));if err!=nil { fail(w,service.Fail(413,"avatar_too_large","头像原图最多 4 MiB。"));return }
  value,err:=a.s.UploadAuthorProfileAvatar(r.Context(),principal(r),r.PathValue("id"),r.Header.Get("Idempotency-Key"),revision,body)
  if err!=nil { fail(w,err);return };result(w,value)
 }))
 write[struct{}](a,"DELETE","/authors/{id}/profile/avatar",true,func(r *http.Request,w *service.Write,_ struct{})(any,int,error){
  revision,err:=profileRevision(r);if err!=nil { return nil,0,err }
  value,err:=a.s.ClearAuthorProfileAvatar(r.Context(),w,r.PathValue("id"),revision)
  return value,200,err
 })
 a.mux.HandleFunc("GET /api/v1/profile-avatars/{id}",a.auth(func(w http.ResponseWriter,r *http.Request){
  body,modified,err:=a.s.AuthorProfileAvatar(r.Context(),r.PathValue("id"));if err!=nil { fail(w,err);return }
  w.Header().Set("Content-Type","image/png")
  w.Header().Set("X-Content-Type-Options","nosniff")
  w.Header().Set("Cache-Control","private, no-cache")
  w.Header().Set("ETag",`"`+r.PathValue("id")+`"`)
  http.ServeContent(w,r,"avatar.png",modified,bytes.NewReader(body))
 }))
 a.get("/authors/{id}/profile/archives",func(r *http.Request)(any,error){
  n,err:=limit(r);if err!=nil { return nil,err }
  return a.s.AuthorProfileArchives(r.Context(),r.PathValue("id"),r.URL.Query().Get("cursor"),n)
 })
 a.get("/authors/{id}/profile/archives/{archiveId}",func(r *http.Request)(any,error){
  return a.s.AuthorProfileArchive(r.Context(),r.PathValue("id"),r.PathValue("archiveId"))
 })
}
