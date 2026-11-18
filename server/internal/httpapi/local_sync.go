package httpapi

import(
 "net/http"
 "qimeng.local/next/server/internal/service"
)

func(a *API)localSyncRoutes(){
 a.get("/local-sync/status",func(r *http.Request)(any,error){return a.s.LocalSyncStatus(r.Context(),principal(r))})
 write[struct{}](a,"POST","/local-sync/trigger",true,func(r *http.Request,w *service.Write,_ struct{})(any,int,error){v,e:=a.s.TriggerLocalSync(r.Context(),w);return v,202,e})
}
