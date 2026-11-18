package service

import(
 "context"
 "database/sql"
 "errors"
 "time"

 "qimeng.local/next/server/internal/model"
 "qimeng.local/next/server/internal/store"
)

type AuthorUpdateEntry struct{ID string `json:"id"`;Title string `json:"title"`;URL string `json:"url"`;PublishedAt *time.Time `json:"publishedAt,omitempty"`}
type AuthorUpdates struct{AuthorID string `json:"authorId"`;Revision int64 `json:"revision"`;TrackUpdates bool `json:"trackUpdates"`;FeedURL string `json:"feedUrl"`;CheckedAt *time.Time `json:"checkedAt,omitempty"`;Items []AuthorUpdateEntry `json:"items"`}
type AuthorUpdatesInput struct{ExpectedRevision *int64 `json:"expectedRevision"`;TrackUpdates *bool `json:"trackUpdates"`;FeedURL *string `json:"feedUrl"`}
type AuthorUpdateCheck struct{ExpectedRevision int64 `json:"expectedRevision"`}

func savedAuthorUpdates(ctx context.Context,q store.Querier,id string)(AuthorUpdates,error){
 value,e:=store.Get[AuthorUpdates](ctx,q,"SELECT data FROM author_subscriptions WHERE author_id=?",id)
 if errors.Is(e,sql.ErrNoRows){return AuthorUpdates{AuthorID:id,Items:[]AuthorUpdateEntry{}},nil};return value,e
}
func saveAuthorUpdates(ctx context.Context,q store.Querier,v AuthorUpdates)error{
 raw,e:=store.Encode(v);if e!=nil{return e}
 _,e=q.ExecContext(ctx,"INSERT INTO author_subscriptions(author_id,data) VALUES(?,?) ON CONFLICT(author_id) DO UPDATE SET data=excluded.data",v.AuthorID,raw);return e
}
func(s *Service)AuthorUpdates(ctx context.Context,id string)(AuthorUpdates,error){
 v,e:=store.Get[AuthorUpdates](ctx,s.DB.DB,`SELECT COALESCE(sub.data,json_object('authorId',a.id,'items',json('[]'))) FROM authors a LEFT JOIN author_subscriptions sub ON sub.author_id=a.id WHERE a.id=?`,id);return v,Normalize(e)
}
func requireVerifiedAuthor(ctx context.Context,q store.Querier,id string)error{
 block,e:=profileBlock(ctx,q,id,"accounts");if e!=nil{return e}
 if profileString(block.Values.IdentityStatus)!="verified"{return Fail(409,"feed_identity_unverified","开启作品订阅须先核实作者身份。")};return nil
}
func(s *Service)SetAuthorUpdates(ctx context.Context,w *Write,id string,in AuthorUpdatesInput)(AuthorUpdates,error){
 if e:=Authorize(w.Principal,"admin","");e!=nil{return AuthorUpdates{},e}
 current,e:=s.AuthorUpdates(ctx,id);if e!=nil{return current,e}
 if in.ExpectedRevision==nil||*in.ExpectedRevision<0||in.TrackUpdates==nil||in.FeedURL==nil{return current,Fail(400,"invalid_feed_subscription","请提供修订号、订阅开关与订阅源地址。")}
 if *in.ExpectedRevision!=current.Revision{return current,Conflict(current.Revision)}
 if id=="unknown"{return current,Fail(409,"placeholder_protected","未知作者不能配置作品订阅。")}
 if *in.FeedURL!=""{if _,e:=validateAvatarURL(*in.FeedURL);e!=nil{return current,Fail(400,"invalid_feed_url","订阅源须为不含凭据的 HTTPS 公网地址。")}}
 if *in.TrackUpdates{if *in.FeedURL==""{return current,Fail(400,"feed_required","请输入作品专用的 RSS 或 Atom 地址。")};if e=requireVerifiedAuthor(ctx,s.DB.DB,id);e!=nil{return current,e}}
 if current.TrackUpdates==*in.TrackUpdates&&current.FeedURL==*in.FeedURL{return current,nil}
 current.Revision++;current.TrackUpdates=*in.TrackUpdates;current.FeedURL=*in.FeedURL;current.CheckedAt=nil;current.Items=[]AuthorUpdateEntry{}
 q,e:=w.Q();if e!=nil{return current,e};return current,saveAuthorUpdates(ctx,q,current)
}
func clearUnverifiedSubscription(ctx context.Context,q store.Querier,block AuthorProfileBlock)error{
 if block.Block!="accounts"||profileString(block.Values.IdentityStatus)=="verified"{return nil}
 v,e:=savedAuthorUpdates(ctx,q,block.AuthorID);if e!=nil||!v.TrackUpdates{return e}
 v.TrackUpdates=false;v.Revision++;v.CheckedAt=nil;v.Items=[]AuthorUpdateEntry{};return saveAuthorUpdates(ctx,q,v)
}
func(s *Service)CheckAuthorUpdates(ctx context.Context,p model.Principal,id,key string,input []byte,in AuthorUpdateCheck)(Result,error){
 return s.checkAuthorUpdates(ctx,p,id,key,input,in,fetchAuthorEntries)
}
func(s *Service)checkAuthorUpdates(ctx context.Context,p model.Principal,id,key string,input []byte,in AuthorUpdateCheck,fetch func(context.Context,string)([]AuthorUpdateEntry,error))(Result,error){
 operation:="POST /api/v1/authors/"+id+"/updates/check"
 if result,exists,e:=s.operationReceipt(ctx,p,operation,key,input);e!=nil||exists{return result,e}
 current,e:=s.AuthorUpdates(ctx,id);if e!=nil{return Result{},e}
 if current.Revision!=in.ExpectedRevision{return Result{},Conflict(current.Revision)}
 if !current.TrackUpdates||current.FeedURL==""{return Result{},Fail(409,"feed_disabled","请先开启明确的作品订阅。")}
 if e=requireVerifiedAuthor(ctx,s.DB.DB,id);e!=nil{return Result{},e}
 ctx,cancel:=context.WithTimeout(ctx,10*time.Second);defer cancel()
 release,e:=withAvatarSlot(ctx);if e!=nil{return Result{},Fail(504,"feed_timeout","订阅读取等待超时。")};defer release()
 items,e:=fetch(ctx,current.FeedURL);if e!=nil{return Result{},e}
 return s.Run(ctx,p,operation,key,input,func(w *Write)(any,int,error){
  latest,e:=s.AuthorUpdates(ctx,id);if e!=nil{return nil,0,e};if latest.Revision!=current.Revision||latest.FeedURL!=current.FeedURL||!latest.TrackUpdates{return nil,0,Conflict(latest.Revision)}
  if e=requireVerifiedAuthor(ctx,s.DB.DB,id);e!=nil{return nil,0,e}
  now:=time.Now().UTC();latest.CheckedAt=&now;latest.Revision++;latest.Items=items
  q,e:=w.Q();if e!=nil{return nil,0,e};return latest,200,saveAuthorUpdates(ctx,q,latest)
 })
}
