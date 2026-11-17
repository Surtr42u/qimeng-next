package httpapi_test

import (
 "encoding/json"
 "testing"

 "qimeng.local/next/server/internal/model"
 "qimeng.local/next/server/internal/service"
)

func TestAuthorMaintenanceGrantCannotCrossAuthorFieldsOrClear(t *testing.T){
 f:=setup(t)
 response:=f.json("POST","/authors","author-create-synthetic",map[string]any{"name":"Synthetic","kind":"regular","biography":"Original"},201)
 var author model.Author;decode(t,response,&author)
 response=f.json("POST","/author-tasks","author-task-synthetic",map[string]any{"authorId":author.ID,"expectedRevision":author.Revision,"allowedFields":[]string{"biography"},"allowClear":false},201)
 var task service.AuthorTask;decode(t,response,&task)
 response=f.json("POST","/credentials","",map[string]any{"label":"Synthetic author worker","authorTaskId":task.ID,"scopes":[]string{"read","author.profile.write"}},201)
 var credential model.Principal;decode(t,response,&credential)
 update:=func(key string,body map[string]any,intended int){t.Helper();raw,_:=json.Marshal(body);r:=f.request("PATCH","/authors/"+author.ID,key,credential.Token,raw,nil);if r.Code!=intended{t.Fatalf("author update: %d want %d: %s",r.Code,intended,r.Body.String())}}
 update("wrong-field-synthetic",map[string]any{"expectedRevision":author.Revision,"homepage":"https://example.org"},403)
 update("clear-field-synthetic",map[string]any{"expectedRevision":author.Revision,"biography":""},403)
 update("authorized-synthetic",map[string]any{"expectedRevision":author.Revision,"biography":"Updated"},200)
 update("authorized-synthetic",map[string]any{"expectedRevision":author.Revision,"biography":"Updated"},200)
 update("extra-update-synthetic",map[string]any{"expectedRevision":author.Revision+1,"biography":"Again"},409)
 var result model.Author;decode(t,f.json("GET","/authors/"+author.ID,"",nil,200),&result)
 if result.Revision!=author.Revision+1||result.Biography!="Updated"||result.Homepage!=""||result.FieldStatus["biography"]!="candidate"{t.Fatalf("unexpected profile: %+v",result)}
}

func TestAuthorMaintenanceExplicitClearPreservesOtherFields(t *testing.T){
 f:=setup(t);var author model.Author
 decode(t,f.json("POST","/authors","author-create-clear",map[string]any{"name":"Synthetic clear","kind":"regular","biography":"Original","homepage":"https://example.org"},201),&author)
 var task service.AuthorTask
 decode(t,f.json("POST","/author-tasks","task-create-clear",map[string]any{"authorId":author.ID,"expectedRevision":author.Revision,"allowedFields":[]string{"biography"},"allowClear":true},201),&task)
 var credential model.Principal
 decode(t,f.json("POST","/credentials","",map[string]any{"label":"Clear worker","authorTaskId":task.ID,"scopes":[]string{"read","author.profile.write"}},201),&credential)
 raw,_:=json.Marshal(map[string]any{"expectedRevision":author.Revision,"biography":""})
 response:=f.request("PATCH","/authors/"+author.ID,"explicit-clear-synthetic",credential.Token,raw,nil);if response.Code!=200{t.Fatal(response.Body.String())}
 var result model.Author;decode(t,response,&result);if result.Biography!=""||result.Homepage!=author.Homepage||result.ID!=author.ID{t.Fatal("clear changed unrelated profile")}
}
