package service

import (
 "context"
 "database/sql"
 "errors"
 "os"
 "path"
 "strings"
 "sync"

 "qimeng.local/next/server/internal/store"
)

// Rebuildable previews live outside the business database and its backups.
// The cache is opened lazily so ordinary uploads do not depend on AI tooling.
const researchCacheLimit int64=256<<20
var researchProcess=ID()
var activeResearchSources sync.Map

func(s *Service)openResearchCache(ctx context.Context)(*sql.DB,error){
 name:="cache/research.sqlite";if e:=s.ensureParent(name);e!=nil{return nil,e};absolute,e:=s.Root.AbsolutePath(name);if e!=nil{return nil,e}
 db,e:=store.OpenDatabase(absolute,store.ResearchDatabase);if e!=nil{return nil,e}
 fail:=func(err error)(*sql.DB,error){db.Close();return nil,err}
 var version int;if e=db.QueryRowContext(ctx,"PRAGMA user_version").Scan(&version);e!=nil{return fail(e)};if version>1{return fail(Fail(409,"research_cache_version","研究缓存版本较新，不能由旧服务覆盖。"))}
 _,e=db.ExecContext(ctx,`CREATE TABLE IF NOT EXISTS materials(upload_id TEXT NOT NULL,source_hash TEXT NOT NULL,expires INTEGER NOT NULL,data TEXT NOT NULL,PRIMARY KEY(upload_id,source_hash));
 CREATE TABLE IF NOT EXISTS frames(id TEXT PRIMARY KEY,upload_id TEXT NOT NULL,source_hash TEXT NOT NULL,expires INTEGER NOT NULL,bytes INTEGER NOT NULL,data TEXT NOT NULL,content BLOB NOT NULL);
 CREATE INDEX IF NOT EXISTS frame_expiry ON frames(expires);
 CREATE TABLE IF NOT EXISTS work_sources(id TEXT PRIMARY KEY,process_id TEXT NOT NULL,snapshot TEXT NOT NULL);
 PRAGMA user_version=1;`);if e!=nil{return fail(e)}
 rows,e:=db.QueryContext(ctx,"SELECT id,snapshot,process_id FROM work_sources LIMIT 1000");if e!=nil{return fail(e)}
 type source struct{id,name,process string};stale:=[]source{}
 for rows.Next(){var item source;if e=rows.Scan(&item.id,&item.name,&item.process);e!=nil{rows.Close();return fail(e)};if _,active:=activeResearchSources.Load(item.id);item.process==researchProcess&&active{continue};stale=append(stale,item)};e=rows.Err();rows.Close();if e!=nil{return fail(e)}
 for _,item:=range stale{
  parts:=strings.Split(item.name,"/")
  if len(parts)!=5||parts[0]!="libraries"||parts[2]!=".qimeng-internal"||parts[3]!="research-sources"||strings.TrimSuffix(parts[4],path.Ext(parts[4]))!=item.id{return fail(Fail(409,"research_cache_invalid","研究缓存恢复路径无效，保留现场。"))}
  if e=s.safe(item.name);e!=nil{return fail(e)};if e=s.Root.Remove(item.name);e!=nil&&!errors.Is(e,os.ErrNotExist){return fail(e)}
  if _,e=db.ExecContext(ctx,"DELETE FROM work_sources WHERE id=?",item.id);e!=nil{return fail(e)}
 }
 return db,nil
}
