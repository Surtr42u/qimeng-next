package service

import (
 "context"
 "database/sql"
 "encoding/json"
 "errors"
 "os"
 "path/filepath"
 "time"

 "qimeng.local/next/server/internal/model"
 "qimeng.local/next/server/internal/store"
)

type Backup struct{
 ID string `json:"id"`
 State string `json:"state"`
 CreatedAt time.Time `json:"createdAt"`
 SnapshotHash string `json:"snapshotHash"`
 AssetCount int `json:"assetCount"`
 ManifestHash string `json:"manifestHash"`
 ExportState string `json:"exportState"`
 ExportError string `json:"exportError,omitempty"`
 ArchiveHash string `json:"archiveHash,omitempty"`
 ArchiveBytes int64 `json:"archiveBytes,omitempty"`
 ExportedFiles int `json:"exportedFiles"`
 TotalFiles int `json:"totalFiles"`
 ExportedAt *time.Time `json:"exportedAt,omitempty"`
}
type backupEntry struct{AssetID string `json:"assetId"`;ContentVersion int64 `json:"contentVersion"`;Revision int64 `json:"revision"`;RelativeFile string `json:"relativeFile"`;Hash string `json:"hash"`;Size int64 `json:"size"`}
func(s *Service)CreateBackup(ctx context.Context,w *Write)(Backup,error){
 if e:=Authorize(w.Principal,"admin","");e!=nil{return Backup{},e};b:=Backup{ID:w.ID("backup"),State:"awaiting_external_backup",ExportState:"not_started",CreatedAt:time.Now().UTC()};directory:="backups/"+b.ID
 var pending int;if e:=s.DB.DB.QueryRowContext(ctx,"SELECT COUNT(*) FROM file_intents WHERE state='prepared'").Scan(&pending);e!=nil{return b,e};if pending>0{return b,Fail(409,"publication_pending","文件发布尚未完成恢复，暂不能生成与原件一致的备份清单。")}
 if e:=s.Root.MkdirAll(directory,0700);e!=nil{return b,e};snapshot:=directory+"/snapshot.sqlite"
 // A previous uncommitted attempt may have completed its snapshot. Its manifest
 // must be rebuilt against that same snapshot, never relabelled as a newer one.
 if _,e:=s.Root.Stat(snapshot);errors.Is(e,os.ErrNotExist){
  var pages,pageSize int64;if e=s.DB.DB.QueryRowContext(ctx,"PRAGMA page_count").Scan(&pages);e!=nil{return b,e};if e=s.DB.DB.QueryRowContext(ctx,"PRAGMA page_size").Scan(&pageSize);e!=nil{return b,e};free,e:=freeBytes(s.Config.DataDir);if e!=nil{return b,e};if free-pages*pageSize-(4<<20)<s.Config.ReserveBytes{return b,Fail(507,"insufficient_storage","空间不足以生成业务快照并保留存储余量。")}
  if _,e=s.DB.DB.ExecContext(ctx,"VACUUM INTO ?",filepath.Join(s.Config.DataDir,filepath.FromSlash(snapshot)));e!=nil{return b,e}
 }else if e!=nil{return b,e}
 snapshotDB,e:=sql.Open("sqlite",store.FileURI(filepath.Join(s.Config.DataDir,filepath.FromSlash(snapshot)))+"?mode=ro");if e!=nil{return b,e};defer snapshotDB.Close()
 assets,e:=store.List[model.Asset](ctx,snapshotDB,"SELECT data FROM assets ORDER BY id");if e!=nil{return b,e};entries:=make([]backupEntry,0,len(assets));for _,a:=range assets{entries=append(entries,backupEntry{a.ID,a.ContentVersion,a.Revision,assetPath(a),a.Hash,a.Size})}
 b.SnapshotHash,_,e=s.hashFile(snapshot);if e!=nil{return b,e};manifest,e:=json.Marshal(struct{InstanceID,SnapshotHash string;Entries []backupEntry}{s.InstanceID,b.SnapshotHash,entries});if e!=nil{return b,e};b.ManifestHash=digest(manifest);b.AssetCount=len(entries)
 f,e:=s.Root.OpenFile(directory+"/manifest.json",os.O_WRONLY|os.O_CREATE|os.O_TRUNC,0600);if e!=nil{return b,e};if _,e=f.Write(manifest);e!=nil{f.Close();return b,e};if e=f.Sync();e!=nil{f.Close();return b,e};if e=f.Close();e!=nil{return b,e}
 raw,e:=store.Encode(b);if e!=nil{return b,e};q,e:=w.Q();if e!=nil{return b,e};_,e=q.ExecContext(ctx,"INSERT INTO backups(id,data) VALUES(?,?)",b.ID,raw);return b,e
}
// Operation never exposes another principal's receipt. A key may be reused for
// distinct operations; callers must select the operation rather than guess.
func(s *Service)Operation(ctx context.Context,p model.Principal,key,operation string)(map[string]any,error){
 rows,e:=s.DB.DB.QueryContext(ctx,"SELECT operation,response,status FROM idempotency WHERE principal_id=? AND key=? AND (?='' OR operation=?) ORDER BY operation LIMIT 2",p.ID,key,operation,operation);if e!=nil{return nil,e};defer rows.Close()
 type receipt struct{op string;body []byte;status int};items:=[]receipt{};for rows.Next(){var v receipt;if e=rows.Scan(&v.op,&v.body,&v.status);e!=nil{return nil,e};items=append(items,v)};if e=rows.Err();e!=nil{return nil,e};if len(items)==0{return map[string]any{"state":"not_recorded","idempotencyKey":key},nil};if len(items)>1{return nil,Fail(409,"operation_ambiguous","该键用于多个操作，请同时指定 operation。")}
 v:=items[0];var body any;if len(v.body)>0{if e=json.Unmarshal(v.body,&body);e!=nil{return nil,e}};return map[string]any{"state":"committed","operation":v.op,"idempotencyKey":key,"status":v.status,"response":body},nil
}
