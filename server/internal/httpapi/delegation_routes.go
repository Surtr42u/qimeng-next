package httpapi

import (
 "net/http"
 "qimeng.local/next/server/internal/service"
)

func(a *API)delegationRoutes(){
 a.get("/ai/delegated-tasks/{id}/budget",func(r *http.Request)(any,error){return a.s.DelegatedTaskBudget(r.Context(),principal(r),r.PathValue("id"))})
 write[service.DelegatedBudgetUpdate](a,"PUT","/ai/delegated-tasks/{id}/budget",true,func(r *http.Request,w *service.Write,in service.DelegatedBudgetUpdate)(any,int,error){v,e:=a.s.RenewDelegatedBudget(r.Context(),w,r.PathValue("id"),in);return v,200,e})
 write[struct{}](a,"POST","/ai/delegated-tasks/{id}/calls",true,func(r *http.Request,w *service.Write,_ struct{})(any,int,error){v,e:=a.s.ReserveDelegatedCall(r.Context(),w,r.PathValue("id"));return v,200,e})
 a.get("/ai/delegations",func(r *http.Request)(any,error){n,e:=limit(r);if e!=nil{return nil,e};q:=r.URL.Query();return a.s.Delegations(r.Context(),principal(r),q.Get("state"),q.Get("cursor"),n)})
 a.get("/ai/delegations/current",func(r *http.Request)(any,error){return a.s.CurrentDelegation(r.Context(),principal(r))})
 write[service.DelegationPolicy](a,"POST","/ai/delegations",false,func(r *http.Request,w *service.Write,in service.DelegationPolicy)(any,int,error){v,e:=a.s.CreateDelegation(r.Context(),w,in);return v,201,e})
 write[struct{}](a,"DELETE","/ai/delegations/{id}",false,func(r *http.Request,w *service.Write,_ struct{})(any,int,error){return nil,204,a.s.RevokeDelegation(r.Context(),w,r.PathValue("id"))})
 write[service.DelegatedTaskStart](a,"POST","/ai/delegated-tasks",true,func(r *http.Request,w *service.Write,in service.DelegatedTaskStart)(any,int,error){v,e:=a.s.StartDelegatedTask(r.Context(),w,in);return v,201,e})
 write[struct{CredentialSecret string `json:"credentialSecret"`}](a,"POST","/ai/delegated-tasks/{id}/resume",true,func(r *http.Request,w *service.Write,in struct{CredentialSecret string `json:"credentialSecret"`})(any,int,error){v,e:=a.s.ResumeDelegatedTask(r.Context(),w,r.PathValue("id"),in.CredentialSecret);return v,200,e})
}
