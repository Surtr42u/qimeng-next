// Package service owns business transactions, permissions and filesystem intents.
package service

import (
 "context"
 "crypto/rand"
 "crypto/sha256"
 "crypto/subtle"
 "database/sql"
 "encoding/base64"
 "encoding/hex"
 "encoding/json"
 "errors"
 "fmt"
 "os"
 "path/filepath"
 "strings"
 "sync"
 "time"

 "qimeng.local/next/server/internal/model"
 "qimeng.local/next/server/internal/store"
 "qimeng.local/next/server/internal/storage"
)

type Error struct { Code string `json:"code"`; Message string `json:"message"`; CurrentRevision int64 `json:"currentRevision,omitempty"`; Status int `json:"-"` }
func (e *Error) Error()string{return e.Code+": "+e.Message}
func Fail(status int,code,message string)error{return &Error{Code:code,Message:message,Status:status}}
func Conflict(rev int64)error{return &Error{Code:"revision_conflict",Message:"对象已被修改，请刷新后重新提交。",CurrentRevision:rev,Status:409}}
func Normalize(err error)error{if errors.Is(err,sql.ErrNoRows){return Fail(404,"not_found","对象不存在。")};return err}
type Config struct { DataDir,AdminToken,AIToken string; StorageRoots map[string]string; SecureCookies,AIConfigured bool; ReserveBytes int64 }
type Service struct { DB *store.Store; Root *storage.Root; Config Config; InstanceID string; mu sync.Mutex; owner *sql.DB; workCtx context.Context; stopWork context.CancelFunc; workers sync.WaitGroup }

func New(config Config)(*Service,error){
 if len(config.AdminToken)<32{return nil,errors.New("QIMENG_ADMIN_TOKEN must contain at least 32 characters")}
 if config.DataDir==""{return nil,errors.New("QIMENG_DATA_DIR is required")}
 if config.AIToken!=""&&len(config.AIToken)<32{return nil,errors.New("QIMENG_AI_TOKEN must contain at least 32 characters")}
 if config.ReserveBytes<=0{config.ReserveBytes=512<<20}
 if err:=os.MkdirAll(config.DataDir,0700);err!=nil{return nil,err}
 root,err:=storage.Open(config.DataDir,config.StorageRoots);if err!=nil{return nil,err}
 for _,d:=range []string{"libraries","uploads","trash","retired","backups"}{if err=root.MkdirAll(d,0700);err!=nil{root.Close();return nil,err}}
 // SQLite's exclusive ownership lock is held by a separate connection for this
 // process, rather than an unsafe stale-PID file. Main storage remains WAL.
 ownerURL:=store.FileURI(filepath.Join(config.DataDir,"authority.sqlite"))
 owner,err:=sql.Open("sqlite",ownerURL+"?_pragma=busy_timeout(0)");if err!=nil{root.Close();return nil,err};owner.SetMaxOpenConns(1)
 if _,err=owner.Exec("CREATE TABLE IF NOT EXISTS authority(id INTEGER); BEGIN EXCLUSIVE");err!=nil{owner.Close();root.Close();var coded interface{Code()int};if errors.As(err,&coded)&&(coded.Code()&255==5||coded.Code()&255==6){return nil,errors.New("another service owns this data directory")};return nil,fmt.Errorf("initialize ownership database: %w",err)}
 db,err:=store.Open(filepath.Join(config.DataDir,"qimeng.sqlite"));if err!=nil{owner.Close();root.Close();return nil,err}
 s:=&Service{DB:db,Root:root,Config:config,owner:owner}
 s.workCtx,s.stopWork=context.WithCancel(context.Background())
 ctx:=context.Background()
 libraries,err:=store.List[model.Library](ctx,db.DB,"SELECT data FROM libraries ORDER BY id");if err!=nil{s.Close();return nil,err}
 for _,library:=range libraries{if err=root.RegisterLibrary(library.ID,library.StorageID,library.RootRelativePath);err!=nil{s.Close();return nil,err}}
 uploadRows,err:=db.DB.QueryContext(ctx,"SELECT id,json_extract(data,'$.libraryId') FROM uploads");if err!=nil{s.Close();return nil,err};for uploadRows.Next(){var id,libraryID string;if err=uploadRows.Scan(&id,&libraryID);err!=nil{uploadRows.Close();s.Close();return nil,err};root.RegisterUpload(id,libraryID)};err=uploadRows.Err();uploadRows.Close();if err!=nil{s.Close();return nil,err}
 s.InstanceID,err=store.Get[string](ctx,db.DB,"SELECT json_quote(value) FROM settings WHERE key='instance_id'")
 if errors.Is(err,sql.ErrNoRows){s.InstanceID=ID();_,err=db.DB.Exec("INSERT INTO settings(key,value) VALUES('instance_id',?)",s.InstanceID)}
 if err!=nil{s.Close();return nil,err}
 var count int
 if err=db.DB.QueryRow("SELECT COUNT(*) FROM authors WHERE id='unknown'").Scan(&count);err!=nil{s.Close();return nil,err}
 if count==0{
  a:=model.Author{ID:"unknown",Name:"其他搁置",Aliases:[]string{},Revision:1,Placeholder:true,FieldStatus:map[string]string{"identity":"missing","biography":"missing","homepage":"missing"},CreatedAt:time.Now().UTC(),UpdatedAt:time.Now().UTC()}
  raw,_:=store.Encode(a);if _,err=db.DB.Exec("INSERT INTO authors(id,name,data) VALUES(?,?,?)",a.ID,a.Name,raw);err!=nil{s.Close();return nil,err}
 }
 if err=s.recoverIntents(ctx);err!=nil{s.Close();return nil,fmt.Errorf("file recovery requires attention: %w",err)}
 if _,err=db.DB.ExecContext(ctx,"UPDATE scans SET state='interrupted',data=json_set(data,'$.state','interrupted') WHERE state IN ('queued','running')");err!=nil{s.Close();return nil,err}
 return s,nil
}
func (s *Service)Close()error{if s.stopWork!=nil{s.stopWork()};s.workers.Wait();defer s.Root.Close();defer s.owner.Close();return s.DB.DB.Close()}
func ID()string{var b [16]byte;if _,err:=rand.Read(b[:]);err!=nil{panic(err)};return hex.EncodeToString(b[:])}
func digest(value []byte)string{h:=sha256.Sum256(value);return hex.EncodeToString(h[:])}
func token()string{var b [32]byte;if _,err:=rand.Read(b[:]);err!=nil{panic(err)};return base64.RawURLEncoding.EncodeToString(b[:])}
func (s *Service)Authenticate(ctx context.Context,value string)(model.Principal,error){
 if value==""{return model.Principal{},Fail(401,"unauthorized","请先连接服务并登录。")}
 equal:=func(a,b string)bool{ah:=sha256.Sum256([]byte(a));bh:=sha256.Sum256([]byte(b));return subtle.ConstantTimeCompare(ah[:],bh[:])==1}
 if equal(value,s.Config.AdminToken){return model.Principal{ID:"owner",Label:"Owner",Scopes:[]string{"admin"},ExpiresAt:time.Now().UTC().Add(24*time.Hour)},nil}
 if s.Config.AIToken!=""&&equal(value,s.Config.AIToken){return model.Principal{ID:"ai-reader",Label:"AI read only",Scopes:[]string{"read"},ExpiresAt:time.Now().UTC().Add(time.Hour)},nil}
 p,err:=store.Get[model.Principal](ctx,s.DB.DB,"SELECT data FROM credentials WHERE token_hash=?",digest([]byte(value)))
 if err!=nil{if !errors.Is(err,sql.ErrNoRows){return p,err};return p,Fail(401,"unauthorized","凭据无效或已撤销。")}
 if !p.ExpiresAt.After(time.Now()){return model.Principal{},Fail(401,"expired","凭据已过期。")}
 return p,nil
}
func Authorize(p model.Principal,scope,batchID string)error{
 if !p.Has(scope){return Fail(403,"scope_denied","凭据没有此操作权限。")}
 if p.Has("admin"){return nil}
 if scope!="read"&&(p.BatchID==""||batchID!=p.BatchID){return Fail(403,"batch_scope_denied","写入必须属于凭据绑定的接收批次。")}
 if p.BatchID!=""&&batchID!=""&&batchID!=p.BatchID{return Fail(403,"batch_scope_denied","不能访问其他批次。")}
 return nil
}

type Write struct{ s *Service; ctx context.Context; Principal model.Principal; tx *sql.Tx; after []func()error; rollback []func(); operation,key,inputHash string }
func (w *Write)Q()(store.Querier,error){if w.tx==nil{var e error;w.tx,e=w.s.DB.DB.BeginTx(w.ctx,nil);if e!=nil{return nil,e}};return w.tx,nil}
func (w *Write)ID(kind string)string{if w.key==""{return ID()};return digest([]byte(w.Principal.ID+"\x00"+w.operation+"\x00"+w.key+"\x00"+kind))[:32]}
type Result struct{ Body []byte; Status int }
// Run serializes state changes and commits the result receipt in the same SQLite
// transaction as business records. A file intent is made durable before disk IO.
func(s *Service)Run(ctx context.Context,p model.Principal,operation,key string,input []byte,fn func(*Write)(any,int,error))(result Result,err error){
 s.mu.Lock();defer s.mu.Unlock()
 // Authentication can precede a queued write. Recheck the revocable credential
 // at the mutation boundary so revocation also rejects work not yet started.
 if p.ID!="owner"&&p.ID!="ai-reader"{
  current,e:=store.Get[model.Principal](ctx,s.DB.DB,"SELECT data FROM credentials WHERE id=?",p.ID)
  if errors.Is(e,sql.ErrNoRows){return result,Fail(401,"revoked","凭据已撤销，尚未开始的写入已拒绝。")};if e!=nil{return result,e};if !current.ExpiresAt.After(time.Now()){return result,Fail(401,"expired","凭据已过期。")};p=current
 }
 if key!=""&&(len(key)<8||len(key)>128){return result,Fail(400,"invalid_idempotency_key","幂等键长度应为 8–128。")}
 hash:=digest(input)
 if key!=""{
  var previous string;var status int;var body []byte
  e:=s.DB.DB.QueryRowContext(ctx,"SELECT input_hash,response,status FROM idempotency WHERE principal_id=? AND operation=? AND key=?",p.ID,operation,key).Scan(&previous,&body,&status)
  if e==nil{if previous!=hash{return result,Fail(409,"idempotency_conflict","同一幂等键的输入已经改变。")};return Result{body,status},nil}
  if !errors.Is(e,sql.ErrNoRows){return result,e}
 }
 w:=&Write{s:s,ctx:ctx,Principal:p,operation:operation,key:key,inputHash:hash}
 committed:=false;defer func(){if w.tx!=nil{w.tx.Rollback()};if !committed{for i:=len(w.rollback)-1;i>=0;i--{w.rollback[i]()}}}()
 value,status,err:=fn(w);if err!=nil{return result,Normalize(err)}
 body:=[]byte{}
 if status!=204{body,err=json.Marshal(value);if err!=nil{return result,err}}
 q,err:=w.Q();if err!=nil{return result,err}
 if key!=""{if _,err=q.ExecContext(ctx,"INSERT OR IGNORE INTO idempotency(principal_id,operation,key,input_hash,response,status) VALUES(?,?,?,?,?,?)",p.ID,operation,key,hash,body,status);err!=nil{return result,err}}
 if err=w.tx.Commit();err!=nil{return result,err};w.tx=nil;committed=true
 for _,after:=range w.after{if err=after();err!=nil{return result,Fail(500,"cleanup_pending","业务已提交，文件后处理待恢复；请用同一操作身份回读，勿重新入库。")}}
 return Result{body,status},nil
}

func(s *Service)CreateSession(ctx context.Context,w *Write,label,batchID string,scopes []string,authorTaskIDs ...string)(model.Principal,error){
 if !w.Principal.Has("admin"){return model.Principal{},Fail(403,"scope_denied","仅设备管理会话可以签发凭据。")}
 if strings.TrimSpace(label)==""||len(label)>80{return model.Principal{},Fail(400,"invalid_label","请输入会话名称。")}
 authorTaskID:="";if len(authorTaskIDs)>0{authorTaskID=authorTaskIDs[0]}
 if batchID!=""&&authorTaskID!=""{return model.Principal{},Fail(400,"task_conflict","凭据只能绑定一种授权任务。")}
 duration:=30*24*time.Hour
 if batchID!=""||authorTaskID!=""{read:=false;for _,scope:=range scopes{if scope=="read"{read=true}};if !read{return model.Principal{},Fail(400,"read_scope_required","任务凭据必须明确包含read权限。")}}
 if authorTaskID!=""{
  task,err:=s.authorTask(ctx,authorTaskID);if err!=nil{return model.Principal{},err}
  if task.State!="authorized"||!task.ExpiresAt.After(time.Now()){return model.Principal{},Fail(409,"author_task_stale","资料维护授权已失效。")}
  duration=time.Until(task.ExpiresAt)
  for _,scope:=range scopes{if scope!="read"&&scope!="author.profile.write"&&scope!="research"{return model.Principal{},Fail(400,"invalid_scope","资料维护凭据只能授予read、research和author.profile.write。")}}
 }else if batchID!=""{if _,err:=s.Batch(ctx,batchID);err!=nil{return model.Principal{},err};duration=24*time.Hour;for _,scope:=range scopes{if scope!="read"&&scope!="ingest"&&scope!="research"&&scope!="organize"{return model.Principal{},Fail(400,"invalid_scope","不支持的 AI 权限。")}}}else{scopes=[]string{"admin"}}
 p:=model.Principal{ID:ID(),Label:label,BatchID:batchID,AuthorTaskID:authorTaskID,Scopes:scopes,ExpiresAt:time.Now().UTC().Add(duration)}
 secret:=token();raw,err:=store.Encode(p);if err!=nil{return p,err};q,err:=w.Q();if err!=nil{return p,err}
 _,err=q.ExecContext(ctx,"INSERT INTO credentials(id,token_hash,expires_at,data) VALUES(?,?,?,?)",p.ID,digest([]byte(secret)),p.ExpiresAt.Format(time.RFC3339Nano),raw);p.Token=secret;return p,err
}
func(s *Service)Revoke(ctx context.Context,w *Write,id string)error{
 if !w.Principal.Has("admin")||id=="owner"{return Fail(403,"scope_denied","无法撤销该凭据。")};q,err:=w.Q();if err!=nil{return err};_,err=q.ExecContext(ctx,"DELETE FROM credentials WHERE id=?",id);return err
}
