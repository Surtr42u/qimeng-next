package httpapi

import (
 "net/http"
 "qimeng.local/next/server/internal/service"
)

type credentialRequest struct {Label string `json:"label"`;BatchID string `json:"batchId"`;AuthorTaskID string `json:"authorTaskId"`;Scopes []string `json:"scopes"`}

func(a *API)authorTaskRoutes(){
 write[service.AuthorTaskCreate](a,"POST","/author-tasks",true,func(r *http.Request,w *service.Write,in service.AuthorTaskCreate)(any,int,error){value,e:=a.s.CreateAuthorTask(r.Context(),w,in);return value,201,e})
 a.get("/author-tasks/{id}",func(r *http.Request)(any,error){return a.s.AuthorTaskContext(r.Context(),principal(r),r.PathValue("id"))})
 write[struct{}](a,"DELETE","/author-tasks/{id}",false,func(r *http.Request,w *service.Write,_ struct{})(any,int,error){return nil,204,a.s.CancelAuthorTask(r.Context(),w,r.PathValue("id"))})
}
