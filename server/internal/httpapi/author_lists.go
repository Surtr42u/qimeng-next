package httpapi

import (
 "crypto/sha256"
 "fmt"
 "net/http"

 "qimeng.local/next/server/internal/service"
)

func(a *API)authorListRoutes(){
 a.get("/author-lists",func(r *http.Request)(any,error){if e:=service.Authorize(principal(r),"admin","");e!=nil{return nil,e};n,e:=limit(r);if e!=nil{return nil,e};return a.s.AuthorLists(r.Context(),r.URL.Query().Get("cursor"),n)})
 a.get("/author-lists/{id}",func(r *http.Request)(any,error){if e:=service.Authorize(principal(r),"admin","");e!=nil{return nil,e};return a.s.AuthorList(r.Context(),r.PathValue("id"))})
 write[service.AuthorListInput](a,"POST","/author-lists",true,func(r *http.Request,w *service.Write,in service.AuthorListInput)(any,int,error){v,e:=a.s.ImportAuthorList(r.Context(),w,in);return v,200,e})
 write[struct{}](a,"POST","/author-lists/rebuild",true,func(r *http.Request,w *service.Write,_ struct{})(any,int,error){v,e:=a.s.RebuildAuthorLists(r.Context(),w);return v,200,e})
 write[struct{}](a,"DELETE","/author-lists/{id}",true,func(r *http.Request,w *service.Write,_ struct{})(any,int,error){revision,e:=revision(r);if e!=nil{return nil,0,e};return nil,204,a.s.DeleteAuthorList(r.Context(),w,r.PathValue("id"),revision)})
 for _,pattern:=range []string{"/author-lists/master","/author-lists/{id}/content"}{
  a.mux.HandleFunc("GET /api/v1"+pattern,a.auth(func(w http.ResponseWriter,r *http.Request){
   body,e:=a.s.ExportAuthorList(r.Context(),principal(r),r.PathValue("id"));if e!=nil{fail(w,e);return}
   w.Header().Set("Content-Type","text/plain; charset=utf-8");w.Header().Set("Cache-Control","no-store");w.Header().Set("ETag",fmt.Sprintf("\"%x\"",sha256.Sum256([]byte(body))))
   fmt.Fprint(w,body)
  }))
 }
 a.get("/author-lists/mirror",func(r *http.Request)(any,error){return a.s.AuthorListMirror(r.Context(),principal(r))})
 write[service.AuthorListMirrorUpdate](a,"PUT","/author-lists/mirror",true,func(r *http.Request,w *service.Write,in service.AuthorListMirrorUpdate)(any,int,error){v,e:=a.s.UpdateAuthorListMirror(r.Context(),w,in);return v,200,e})
}
