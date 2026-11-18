package httpapi

import (
 "net/http"
 "qimeng.local/next/server/internal/service"
)

func(a *API)scanRoutes(){
 a.get("/libraries/{id}/monitoring",func(r *http.Request)(any,error){return a.s.LibraryMonitoring(r.Context(),principal(r),r.PathValue("id"))})
 a.get("/scans",func(r *http.Request)(any,error){n,e:=limit(r);if e!=nil{return nil,e};q:=r.URL.Query();return a.s.Scans(r.Context(),q.Get("libraryId"),q.Get("state"),q.Get("cursor"),n)})
 write[revisionBody](a,"POST","/libraries/{id}/scans",true,func(r *http.Request,w *service.Write,in revisionBody)(any,int,error){v,e:=a.s.StartScan(r.Context(),w,r.PathValue("id"),in.ExpectedRevision);return v,202,e})
 a.get("/scans/{id}",func(r *http.Request)(any,error){return a.s.Scan(r.Context(),r.PathValue("id"))})
 write[struct{}](a,"DELETE","/scans/{id}",true,func(r *http.Request,w *service.Write,_ struct{})(any,int,error){v,e:=a.s.CancelScan(r.Context(),w,r.PathValue("id"));return v,200,e})
}
