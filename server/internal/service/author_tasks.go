package service

import (
 "context"
 "strings"
 "time"

 "qimeng.local/next/server/internal/model"
 "qimeng.local/next/server/internal/store"
)

type AuthorTaskCreate struct {
 AuthorID string `json:"authorId"`
 ExpectedRevision int64 `json:"expectedRevision"`
 AllowedFields []string `json:"allowedFields"`
 AllowClear bool `json:"allowClear"`
 MaxUpdates int `json:"maxUpdates"`
 ExpiresInSeconds int `json:"expiresInSeconds"`
}
type AuthorTask struct {
 ID string `json:"id"`
 AuthorID string `json:"authorId"`
 ExpectedRevision int64 `json:"expectedRevision"`
 AllowedFields []string `json:"allowedFields"`
 AllowClear bool `json:"allowClear"`
 MaxUpdates int `json:"maxUpdates"`
 UsedUpdates int `json:"usedUpdates"`
 State string `json:"state"`
 ExpiresAt time.Time `json:"expiresAt"`
 CreatedAt time.Time `json:"createdAt"`
}

func(s *Service)authorTask(ctx context.Context,id string)(AuthorTask,error){
 task,e:=store.Get[AuthorTask](ctx,s.DB.DB,"SELECT data FROM author_tasks WHERE id=?",id);return task,Normalize(e)
}
func(s *Service)CreateAuthorTask(ctx context.Context,w *Write,in AuthorTaskCreate)(AuthorTask,error){
 var result AuthorTask
 if e:=Authorize(w.Principal,"admin","");e!=nil{return result,e}
 author,e:=s.Author(ctx,in.AuthorID);if e!=nil{return result,e};if author.Placeholder{return result,Fail(409,"placeholder_protected","占位作者不能用于资料维护。")}
 if author.Revision!=in.ExpectedRevision{return result,Conflict(author.Revision)}
 fields,e:=cleanStrings(in.AllowedFields,4,32);if e!=nil{return result,e};if len(fields)==0{return result,Fail(400,"fields_required","至少授权一个资料字段。")}
 for _,field:=range fields{switch field{case "name","biography","homepage","aliases":default:return result,Fail(400,"invalid_fields","不支持的资料字段。")}}
 if in.MaxUpdates==0{in.MaxUpdates=1};if in.ExpiresInSeconds==0{in.ExpiresInSeconds=3600}
 if in.MaxUpdates<1||in.MaxUpdates>10||in.ExpiresInSeconds<60||in.ExpiresInSeconds>86400{return result,Fail(400,"invalid_budget","维护次数或有效期超出允许范围。")}
 now:=time.Now().UTC();result=AuthorTask{ID:w.ID("author-task"),AuthorID:author.ID,ExpectedRevision:author.Revision,
  AllowedFields:fields,AllowClear:in.AllowClear,MaxUpdates:in.MaxUpdates,State:"authorized",CreatedAt:now,ExpiresAt:now.Add(time.Duration(in.ExpiresInSeconds)*time.Second)}
 raw,e:=store.Encode(result);if e!=nil{return result,e};q,e:=w.Q();if e!=nil{return result,e}
 _,e=q.ExecContext(ctx,"INSERT INTO author_tasks VALUES(?,?,?,?,?)",result.ID,result.AuthorID,result.State,result.ExpiresAt.Format(time.RFC3339Nano),raw);return result,e
}

func(s *Service)AuthorTaskContext(ctx context.Context,p model.Principal,id string)(map[string]any,error){
 if !p.Has("admin")&&p.AuthorTaskID!=id{return nil,Fail(403,"task_scope_denied","凭据不属于此资料维护任务。")}
 task,e:=s.authorTask(ctx,id);if e!=nil{return nil,e};author,e:=s.Author(ctx,task.AuthorID);if e!=nil{return nil,e}
 actions:=[]string{};if task.State=="authorized"&&task.ExpiresAt.After(time.Now())&&task.ExpectedRevision==author.Revision{actions=append(actions,"author.profile.update")}
 return map[string]any{"task":task,"author":author,"nextActions":actions},nil
}

func(s *Service)CancelAuthorTask(ctx context.Context,w *Write,id string)error{
 if e:=Authorize(w.Principal,"admin","");e!=nil{return e};task,e:=s.authorTask(ctx,id);if e!=nil{return e};task.State="cancelled";raw,e:=store.Encode(task);if e!=nil{return e};q,e:=w.Q();if e!=nil{return e}
 _,e=q.ExecContext(ctx,"UPDATE author_tasks SET state=?,data=? WHERE id=?",task.State,raw,id);return e
}

// authorizeAuthorUpdate only accepts a trusted task bound to this author and version.
// It does not promote model-provided confirmation flags or create new authority.
func(s *Service)authorizeAuthorUpdate(ctx context.Context,p model.Principal,id string,in AuthorUpdate)(*AuthorTask,error){
 if p.Has("admin"){return nil,nil}
 if !p.Has("author.profile.write")||p.AuthorTaskID==""{return nil,Fail(403,"scope_denied","未授予作者资料维护权限。")}
 task,e:=s.authorTask(ctx,p.AuthorTaskID);if e!=nil{return nil,e}
 if task.AuthorID!=id||task.ExpectedRevision!=in.ExpectedRevision||task.State!="authorized"||!task.ExpiresAt.After(time.Now())||task.UsedUpdates>=task.MaxUpdates{return nil,Fail(409,"author_task_stale","维护对象、版本、状态或有效期已变化。")}
 allowed:=map[string]bool{};for _,field:=range task.AllowedFields{allowed[field]=true}
 present:=map[string]bool{"name":in.Name!=nil,"biography":in.Biography!=nil,"homepage":in.Homepage!=nil,"aliases":in.Aliases!=nil,"following":in.Following!=nil}
 count:=0;for field,exists:=range present{if !exists{continue};count++;if !allowed[field]{return nil,Fail(403,"field_scope_denied","本任务未授权修改该字段。")}}
 if count==0{return nil,Fail(400,"changes_required","必须提交授权范围内的具体字段变更。")}
 if !task.AllowClear{
  for _,value:=range []*string{in.Name,in.Biography,in.Homepage}{if value!=nil&&strings.TrimSpace(*value)==""{return nil,Fail(403,"clear_not_authorized","任务未授权清空资料。")}}
  if in.Aliases!=nil&&len(*in.Aliases)==0{return nil,Fail(403,"clear_not_authorized","任务未授权清空别名。")}
 }
 return &task,nil
}

func finishAuthorTask(ctx context.Context,q store.Querier,task *AuthorTask,revision int64)error{
 if task==nil{return nil};task.UsedUpdates++;task.ExpectedRevision=revision;if task.UsedUpdates>=task.MaxUpdates{task.State="completed"}
 raw,e:=store.Encode(task);if e!=nil{return e};_,e=q.ExecContext(ctx,"UPDATE author_tasks SET state=?,data=? WHERE id=?",task.State,raw,task.ID);return e
}
