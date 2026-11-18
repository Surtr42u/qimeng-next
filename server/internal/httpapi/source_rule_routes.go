package httpapi

import (
  "net/http"
  "qimeng.local/next/server/internal/service"
)
func(a *API)sourceRuleRoutes(){
  a.mux.HandleFunc("GET /api/v1/sources/custom-groups", a.authScope("admin",func(w http.ResponseWriter,r *http.Request){
    value,err:=a.s.SourceRules(r.Context());if err!=nil{fail(w,err);return};respond(w,200,value)
  }))
  writeScoped(a,"admin","PUT","/sources/custom-groups",true,func(r *http.Request,w *service.Write,in service.SourceRuleUpdate)(any,int,error){
    value,err:=a.s.SetSourceRules(r.Context(),w,in);return value,200,err
  })
}
