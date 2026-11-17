package service

import (
 "context"
 "encoding/base64"
 "encoding/json"
 "time"

 "qimeng.local/next/server/internal/model"
 "qimeng.local/next/server/internal/store"
)

type authorTaskCursor struct { Created string `json:"at"`; ID string `json:"id"`; Filter string `json:"filter"` }

func effectiveAuthorTask(task AuthorTask, now time.Time) AuthorTask {
 if task.State=="authorized"&&!task.ExpiresAt.After(now){task.State="expired"};return task
}

func(s *Service)AuthorTasks(ctx context.Context,p model.Principal,authorID,state,cursor string,count int)(model.Page[AuthorTask],error){
 result:=model.Page[AuthorTask]{Items:[]AuthorTask{}}
 if !p.Has("admin")&&p.AuthorTaskID==""{return result,Fail(403,"task_scope_denied","只允许读取获授权的资料维护任务。")}
 switch state {case "","authorized","completed","cancelled","expired":default:return result,Fail(400,"invalid_state","任务状态无效。")}
 n,e:=limited(count,50);if e!=nil{return result,e}
 bound,_:=json.Marshal([]string{p.AuthorTaskID,authorID,state});filter:=digest(bound)
 var after authorTaskCursor
 if cursor!=""{raw,err:=base64.RawURLEncoding.DecodeString(cursor);if err!=nil||len(raw)>2048||json.Unmarshal(raw,&after)!=nil||after.Filter!=filter||after.ID==""||after.Created==""{return result,Fail(400,"invalid_cursor","游标不属于当前任务查询。")}}
 now:=time.Now().UTC()
 query:=`SELECT data FROM author_tasks WHERE (?='' OR id=?) AND (?='' OR author_id=?)
 AND (?='' OR CASE WHEN state='authorized' AND julianday(expires_at)<=julianday(?) THEN 'expired' ELSE state END=?)
 AND (?='' OR json_extract(data,'$.createdAt')<? OR (json_extract(data,'$.createdAt')=? AND id<?))
 ORDER BY json_extract(data,'$.createdAt') DESC,id DESC LIMIT ?`
 values,e:=store.List[AuthorTask](ctx,s.DB.DB,query,p.AuthorTaskID,p.AuthorTaskID,authorID,authorID,state,now.Format(time.RFC3339Nano),state,after.ID,after.Created,after.Created,after.ID,n+1)
 if e!=nil{return result,e}
 if len(values)>n{values=values[:n];last:=values[n-1];raw,_:=json.Marshal(authorTaskCursor{last.CreatedAt.Format(time.RFC3339Nano),last.ID,filter});result.NextCursor=base64.RawURLEncoding.EncodeToString(raw)}
 for _,task:=range values{result.Items=append(result.Items,effectiveAuthorTask(task,now))};return result,nil
}
