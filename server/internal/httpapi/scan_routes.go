package httpapi

import (
 "net/http"
 "qimeng.local/next/server/internal/service"
)

func(a *API)scanRoutes(){
 write[revisionBody](a,"POST","/libraries/{id}/scans",true,func(r *http.Request,w *service.Write,in revisionBody)(any,int,error){v,e:=a.s.StartScan(r.Context(),w,r.PathValue("id"),in.ExpectedRevision);return v,202,e})
 a.get("/scans/{id}",func(r *http.Request)(any,error){return a.s.Scan(r.Context(),r.PathValue("id"))})
 write[struct{}](a,"DELETE","/scans/{id}",true,func(r *http.Request,w *service.Write,_ struct{})(any,int,error){v,e:=a.s.CancelScan(r.Context(),w,r.PathValue("id"));return v,200,e})
}
