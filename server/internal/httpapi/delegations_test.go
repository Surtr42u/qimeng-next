package httpapi_test

import (
 "context"
 "encoding/base64"
 "encoding/json"
 "fmt"
 "strings"
 "testing"

 "qimeng.local/next/server/internal/service"
)

func TestDelegationScopesLeaseRotationAndDurableBudget(t *testing.T){
 f:=setup(t)
 policy:=service.DelegationPolicy{Label:"Synthetic persistent executor",LibraryIDs:[]string{f.library},AuthorIDs:[]string{},AllowedFields:[]string{},CandidateBlocks:[]string{},CanIngest:true,CanResearch:true,CanOrganize:true,MaxResearchCalls:2,MaxUpdates:1,MaxTasksPerDay:2,ValidDays:7,MaxCallsPerTask:10,TaskSeconds:3600}
 var grant service.DelegationCreated;decode(t,f.json("POST","/ai/delegations","",policy,201),&grant)
 request:=func(method,path,key,token string,body any,status int)[]byte{t.Helper();raw,_:=json.Marshal(body);response:=f.request(method,path,key,token,raw,nil);if response.Code!=status{t.Fatalf("%s %s: %d want %d %s",method,path,response.Code,status,response.Body.String())};return response.Body.Bytes()}
 firstSecret:=base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("a",32)))
 input:=service.DelegatedTaskStart{Kind:"intake",Label:"Synthetic task",LibraryID:f.library,Mode:"intelligent",CredentialSecret:firstSecret}
 denied:=input;denied.LibraryID="outside-authorized-library"
 request("POST","/ai/delegated-tasks","delegation-outside-key",grant.Credential.Token,denied,403)
 raw:=request("POST","/ai/delegated-tasks","delegation-start-key",grant.Credential.Token,input,201)
 if strings.Contains(string(raw),firstSecret){t.Fatal("task credential secret was reflected to caller")}
 var task service.DelegatedTask;if e:=json.Unmarshal(raw,&task);e!=nil{t.Fatal(e)}
 replay:=request("POST","/ai/delegated-tasks","delegation-start-key",grant.Credential.Token,input,201);if string(raw)!=string(replay){t.Fatal("task start replay changed identity")}
 request("POST","/libraries","delegate-admin-denied",firstSecret,map[string]string{"name":"Forbidden","kind":"regular"},403)
 oldPrincipal,e:=f.s.Authenticate(context.Background(),firstSecret);if e!=nil{t.Fatal(e)}
 request("POST","/ai/delegated-tasks/"+task.TaskID+"/calls","durable-call-one",firstSecret,struct{}{},200)
 request("POST","/ai/delegated-tasks/"+task.TaskID+"/calls","durable-call-one",firstSecret,struct{}{},200)
 secondSecret:=base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("b",32)))
 raw=request("POST","/ai/delegated-tasks/"+task.TaskID+"/resume","delegation-resume-key",grant.Credential.Token,map[string]string{"credentialSecret":secondSecret},200)
 var resumed service.DelegatedTask;json.Unmarshal(raw,&resumed)
 if resumed.Credential.ID!=task.Credential.ID||resumed.Credential.Generation!=2{t.Fatal("resume lost operation identity or lease generation")}
 request("GET","/batches/"+task.TaskID,"",firstSecret,nil,401)
 executed:=false;_,e=f.s.Run(context.Background(),oldPrincipal,"queued-old-executor","queued-executor-key",nil,func(w *service.Write)(any,int,error){executed=true;return nil,204,nil});if e==nil||executed{t.Fatal("queued stale executor ran after takeover")}
 raw=request("GET","/ai/delegated-tasks/"+task.TaskID+"/budget","",secondSecret,nil,200)
 var budget service.DelegatedBudget;json.Unmarshal(raw,&budget);if budget.Calls!=1||budget.MaxCalls!=10{t.Fatal("resume reset budget or duplicate reservation consumed twice")}
 for n:=2;n<=10;n++{request("POST","/ai/delegated-tasks/"+task.TaskID+"/calls",fmt.Sprintf("durable-call-%d",n),secondSecret,struct{}{},200)}
 request("POST","/ai/delegated-tasks/"+task.TaskID+"/calls","durable-call-exhausted",secondSecret,struct{}{},429)
 request("PUT","/ai/delegated-tasks/"+task.TaskID+"/budget","denied-budget-renew",secondSecret,service.DelegatedBudgetUpdate{MaxCalls:20,ExpiresInSeconds:7200},403)
 f.json("PUT","/ai/delegated-tasks/"+task.TaskID+"/budget","trusted-budget-renew",service.DelegatedBudgetUpdate{MaxCalls:20,ExpiresInSeconds:7200},200)
 raw=request("POST","/ai/delegated-tasks/"+task.TaskID+"/calls","durable-call-after-renew",secondSecret,struct{}{},200);json.Unmarshal(raw,&budget);if budget.Calls!=11{t.Fatal("renewal reset prior consumption")}
 f.json("DELETE","/ai/delegations/"+grant.Delegation.ID,"",struct{}{},204)
 request("GET","/batches/"+task.TaskID,"",secondSecret,nil,401)
 request("POST","/ai/delegated-tasks","revoked-delegation-task",grant.Credential.Token,input,401)
}
