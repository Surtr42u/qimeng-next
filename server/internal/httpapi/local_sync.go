package httpapi

import(
 "net/http"
 "qimeng.local/next/server/internal/service"
)

func(a *API)localSyncRoutes(){
 a.mux.HandleFunc("POST /api/v1/libraries/resolve-input-directories",a.auth(func(w http.ResponseWriter,r *http.Request){
  in,_,e:=decode[struct{Names []string `json:"names"`}](w,r);if e!=nil{fail(w,e);return};v,e:=a.s.ResolveInputDirectories(r.Context(),principal(r),in.Names);if e!=nil{fail(w,e);return};respond(w,200,v)
 }))
 a.get("/local-sync/status",func(r *http.Request)(any,error){return a.s.LocalSyncStatus(r.Context(),principal(r))})
 write[struct{}](a,"POST","/local-sync/trigger",true,func(r *http.Request,w *service.Write,_ struct{})(any,int,error){v,e:=a.s.TriggerLocalSync(r.Context(),w);return v,202,e})
}
