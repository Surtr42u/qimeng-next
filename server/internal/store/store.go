// Package store owns SQLite schema and record persistence. Only the service owns writes.
package store

import (
 "context"
 "database/sql"
 "crypto/sha256"
 "embed"
 "encoding/json"
 "fmt"
 "net/url"
 "path/filepath"
 "strconv"
 "strings"

 "qimeng.local/next/server/internal/model"
 _ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

type Store struct { DB *sql.DB }
func Open(path string) (*Store,error) {
 u := &url.URL{Scheme:"file",Path:filepath.ToSlash(path)}
 db,err := sql.Open("sqlite",u.String()+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(FULL)")
 if err != nil { return nil,err }; db.SetMaxOpenConns(1)
 fail:=func(e error)(*Store,error){ db.Close(); return nil,e }
 if err=db.Ping(); err!=nil{return fail(err)}
 if _,err=db.Exec("CREATE TABLE IF NOT EXISTS schema_migrations(version INTEGER PRIMARY KEY,name TEXT NOT NULL,sha256 TEXT NOT NULL)");err!=nil{return fail(err)}
 files,err:=migrations.ReadDir("migrations");if err!=nil{return fail(err)}
 known:=map[int]bool{}
 for _,file:=range files{
  if file.IsDir()||!strings.HasSuffix(file.Name(),".sql"){continue};prefix,_,ok:=strings.Cut(file.Name(),"_");if !ok{return fail(fmt.Errorf("invalid migration name %s",file.Name()))};version,e:=strconv.Atoi(prefix);if e!=nil{return fail(e)};known[version]=true
  body,e:=migrations.ReadFile("migrations/"+file.Name());if e!=nil{return fail(e)};hash:=fmt.Sprintf("%x",sha256.Sum256(body));var recorded string
  e=db.QueryRow("SELECT sha256 FROM schema_migrations WHERE version=?",version).Scan(&recorded)
  if e==nil{if recorded!=hash{return fail(fmt.Errorf("migration %d checksum changed",version))};continue};if e!=sql.ErrNoRows{return fail(e)}
  tx,e:=db.Begin();if e!=nil{return fail(e)};if _,e=tx.Exec(string(body));e!=nil{tx.Rollback();return fail(e)}
  if _,e=tx.Exec("INSERT INTO schema_migrations(version,name,sha256) VALUES(?,?,?)",version,file.Name(),hash);e!=nil{tx.Rollback();return fail(e)};if e=tx.Commit();e!=nil{return fail(e)}
 }
 rows,err:=db.Query("SELECT version FROM schema_migrations");if err!=nil{return fail(err)};for rows.Next(){var v int;if err=rows.Scan(&v);err!=nil{rows.Close();return fail(err)};if !known[v]{rows.Close();return fail(fmt.Errorf("database schema %d requires a newer server",v))}};err=rows.Err();rows.Close();if err!=nil{return fail(err)}
 return &Store{DB:db},nil
}
type Querier interface {
 QueryRowContext(context.Context,string,...any)*sql.Row
 QueryContext(context.Context,string,...any)(*sql.Rows,error)
 ExecContext(context.Context,string,...any)(sql.Result,error)
}
func Encode(v any)(string,error){b,e:=json.Marshal(v);return string(b),e}
func Get[T any](ctx context.Context,q Querier,query string,args ...any)(T,error){
 var value T;var raw string
 if err:=q.QueryRowContext(ctx,query,args...).Scan(&raw);err!=nil{return value,err}
 err:=json.Unmarshal([]byte(raw),&value);return value,err
}
func List[T any](ctx context.Context,q Querier,query string,args ...any)([]T,error){
 rows,err:=q.QueryContext(ctx,query,args...);if err!=nil{return nil,err};defer rows.Close()
 values:=[]T{}
 for rows.Next(){var raw string;var v T;if err=rows.Scan(&raw);err!=nil{return nil,err};if err=json.Unmarshal([]byte(raw),&v);err!=nil{return nil,err};values=append(values,v)}
 return values,rows.Err()
}
func SaveAsset(ctx context.Context,q Querier,a model.Asset)error{
 raw,err:=Encode(a);if err!=nil{return err}
 _,err=q.ExecContext(ctx,`INSERT INTO assets(id,library_id,relative_path,file_name,media_type,author_id,collection_id,hash,size,revision,created_at,updated_at,favorite,liked,trashed,data) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET relative_path=excluded.relative_path,file_name=excluded.file_name,media_type=excluded.media_type,author_id=excluded.author_id,collection_id=excluded.collection_id,hash=excluded.hash,size=excluded.size,revision=excluded.revision,updated_at=excluded.updated_at,favorite=excluded.favorite,liked=excluded.liked,trashed=excluded.trashed,data=excluded.data`,a.ID,a.LibraryID,a.RelativePath,a.FileName,a.MediaType,a.AuthorID,a.CollectionID,a.Hash,a.Size,a.Revision,a.CreatedAt.Format("2006-01-02T15:04:05.000000000Z"),a.UpdatedAt.Format("2006-01-02T15:04:05.000000000Z"),a.Favorite,a.Liked,a.Trashed,raw)
 if err!=nil{return err}
 if _,err=q.ExecContext(ctx,"DELETE FROM asset_authors WHERE asset_id=?",a.ID);err!=nil{return err}
 for _,authorID:=range a.AuthorIDs{if _,err=q.ExecContext(ctx,"INSERT INTO asset_authors(asset_id,author_id) VALUES(?,?)",a.ID,authorID);err!=nil{return err}}
 if _,err=q.ExecContext(ctx,"DELETE FROM asset_tags WHERE asset_id=?",a.ID);err!=nil{return err}
 for _,tag:=range a.Tags{
  if _,err=q.ExecContext(ctx,"INSERT INTO tags(name,revision,created_at,updated_at) VALUES(?,1,?,?) ON CONFLICT(name) DO UPDATE SET deleted=0,revision=tags.revision+1,updated_at=excluded.updated_at WHERE tags.deleted=1",tag,a.UpdatedAt.Format("2006-01-02T15:04:05.000000000Z"),a.UpdatedAt.Format("2006-01-02T15:04:05.000000000Z"));err!=nil{return err}
  if _,err=q.ExecContext(ctx,"INSERT INTO asset_tags(asset_id,tag) VALUES(?,?)",a.ID,tag);err!=nil{return err}
 }
 if _,err=q.ExecContext(ctx,"DELETE FROM asset_sources WHERE asset_id=?",a.ID);err!=nil{return err}
 for _,sourceID:=range a.SourceIDs{if _,err=q.ExecContext(ctx,"INSERT INTO asset_sources(asset_id,source_id) VALUES(?,?)",a.ID,sourceID);err!=nil{return err}}
 if _,err=q.ExecContext(ctx,"DELETE FROM asset_characters WHERE asset_id=?",a.ID);err!=nil{return err}
 for _,characterID:=range a.CharacterIDs{if _,err=q.ExecContext(ctx,"INSERT INTO asset_characters(asset_id,character_id) VALUES(?,?)",a.ID,characterID);err!=nil{return err}}
 return nil
}
func SaveUpload(ctx context.Context,q Querier,u model.Upload)error{
 raw,err:=Encode(u);if err!=nil{return err};_,err=q.ExecContext(ctx,"INSERT INTO uploads(id,batch_id,state,data) VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET state=excluded.state,data=excluded.data",u.ID,u.BatchID,u.State,raw);return err
}
func SaveBatch(ctx context.Context,q Querier,b model.Batch)error{
 raw,err:=Encode(b);if err!=nil{return err};_,err=q.ExecContext(ctx,"INSERT INTO batches(id,created_at,data) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET data=excluded.data",b.ID,b.CreatedAt.Format("2006-01-02T15:04:05.000000000Z"),raw);return err
}
func SavePlan(ctx context.Context,q Querier,p model.Plan)error{
 raw,err:=Encode(p);if err!=nil{return err};_,err=q.ExecContext(ctx,"INSERT INTO plans(id,batch_id,data) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET data=excluded.data",p.ID,p.BatchID,raw);return err
}
