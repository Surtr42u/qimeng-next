package service

import (
 "context"
 "database/sql"
 "errors"
 "log/slog"
 "path/filepath"
 "strings"
 "time"

 "qimeng.local/next/server/internal/domain"
 "qimeng.local/next/server/internal/model"
 "qimeng.local/next/server/internal/store"
)

type AuthorListMirrorUpdate struct{
 ExpectedRevision *int64 `json:"expectedRevision"`
 Path string `json:"path"`
 Mode string `json:"mode"`
 ListID string `json:"listId,omitempty"`
}
type AuthorListMirror struct{
 Revision int64 `json:"revision"`
 Path string `json:"path"`
 Mode string `json:"mode"`
 ListID string `json:"listId,omitempty"`
 State string `json:"state"`
 ContentHash string `json:"contentHash,omitempty"`
 ErrorCode string `json:"errorCode,omitempty"`
 PublishedAt *time.Time `json:"publishedAt,omitempty"`
 version int64
}

func readAuthorMirror(ctx context.Context,q store.Querier)(AuthorListMirror,error){
 var m AuthorListMirror;var published sql.NullString
 e:=q.QueryRowContext(ctx,`SELECT revision,path,mode,list_id,state,content_hash,error_code,published_at,version FROM author_list_mirror WHERE id=1`).Scan(&m.Revision,&m.Path,&m.Mode,&m.ListID,&m.State,&m.ContentHash,&m.ErrorCode,&published,&m.version)
 if errors.Is(e,sql.ErrNoRows){return AuthorListMirror{Mode:"master",State:"disabled"},nil}
 if e==nil&&published.Valid{at,err:=time.Parse(time.RFC3339Nano,published.String);if err!=nil{return m,err};m.PublishedAt=&at}
 return m,e
}

func(s *Service)AuthorListMirror(ctx context.Context,p model.Principal)(AuthorListMirror,error){
 if e:=Authorize(p,"admin","");e!=nil{return AuthorListMirror{},e}
 return readAuthorMirror(ctx,s.DB.DB)
}

func(s *Service)UpdateAuthorListMirror(ctx context.Context,w *Write,in AuthorListMirrorUpdate)(AuthorListMirror,error){
 if e:=Authorize(w.Principal,"admin","");e!=nil{return AuthorListMirror{},e}
 m,e:=readAuthorMirror(ctx,s.DB.DB);if e!=nil{return m,e}
 if in.ExpectedRevision==nil||*in.ExpectedRevision<0{return m,Fail(400,"revision_required","请提供当前镜像配置修订号。")}
 if *in.ExpectedRevision!=m.Revision{return m,Conflict(m.Revision)}
 if in.Mode!="master"&&in.Mode!="fragment"{return m,Fail(400,"invalid_mirror_mode","镜像模式无效。")}
 state:="disabled"
 if in.Path!=""{
  base:=filepath.Base(in.Path)
  if !filepath.IsAbs(in.Path)||filepath.Clean(in.Path)!=in.Path||len(in.Path)>4096||!domain.ValidName(base)||!strings.EqualFold(filepath.Ext(base),".txt"){return m,Fail(400,"invalid_mirror_path","镜像须使用受信存储根内的绝对 TXT 路径。")}
  if _,_,e=s.Root.ResolveLibraryPath(filepath.Dir(in.Path));e!=nil{return m,Fail(400,"invalid_mirror_path","镜像须位于已配置的外部存储根内。")}
  if in.Mode=="fragment"{if _,e=s.AuthorList(ctx,in.ListID);e!=nil{return m,e}}
  state="pending"
 }
 if in.Mode=="master"||in.Path==""{in.ListID=""}
 q,e:=w.Q();if e!=nil{return m,e}
 _,e=q.ExecContext(ctx,`INSERT INTO author_list_mirror(id,revision,path,mode,list_id,version,state) VALUES(1,1,?,?,?,1,?)
  ON CONFLICT(id) DO UPDATE SET revision=revision+1,path=excluded.path,mode=excluded.mode,list_id=excluded.list_id,version=version+1,state=excluded.state,error_code='',content_hash='',published_at=NULL`,in.Path,in.Mode,in.ListID,state)
 if e!=nil{return m,e};return readAuthorMirror(ctx,q)
}

func(s *Service)wakeAuthorMirror(){select{case s.mirrorWake<-struct{}{}:default:}}

func(s *Service)authorMirrorWorker(){
 defer s.workers.Done()
 for{
  select{case <-s.workCtx.Done():return;case <-s.mirrorWake:}
  if e:=s.publishPendingAuthorMirror(s.workCtx);e!=nil&&s.workCtx.Err()==nil{slog.Warn("author list mirror publication requires attention")}
 }
}

func(s *Service)publishPendingAuthorMirror(ctx context.Context)error{
 tx,e:=s.DB.DB.BeginTx(ctx,&sql.TxOptions{ReadOnly:true});if e!=nil{return e};defer tx.Rollback()
 m,e:=readAuthorMirror(ctx,tx);if e!=nil||m.State!="pending"{return e}
 var content string
 if m.Mode=="master"{content,e=renderMasterList(ctx,tx)}else{var list authorListRecord;list,e=authorListRecordAt(ctx,tx,m.ListID);if e==nil{content,e=renderAuthorList(ctx,tx,list)}}
 if endErr:=tx.Commit();endErr!=nil{return endErr}
 // A late export cannot publish to a superseded owner path or overwrite newer
 // business data. The business write has already committed before this worker.
 s.mu.Lock();defer s.mu.Unlock()
 current,readErr:=readAuthorMirror(ctx,s.DB.DB);if readErr!=nil{return readErr}
 if current.version!=m.version{return nil}
 if e==nil{e=s.writeAuthorMirrorFile(m,content)}
 if e!=nil{
  code:="mirror_write_failed";var app *Error;if errors.As(e,&app){code=app.Code};if errors.Is(e,sql.ErrNoRows){code="mirror_list_missing"}
  if _,saveErr:=s.DB.DB.ExecContext(ctx,"UPDATE author_list_mirror SET state='error',error_code=? WHERE id=1",code);saveErr!=nil{return saveErr};return e
 }
 _,e=s.DB.DB.ExecContext(ctx,"UPDATE author_list_mirror SET state='current',error_code='',content_hash=?,published_at=? WHERE id=1",digest([]byte(content)),dateKey(time.Now().UTC()));return e
}
