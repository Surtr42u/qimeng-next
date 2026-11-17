package service

import (
 "context"
 "crypto/sha256"
 "database/sql"
 "encoding/hex"
 "errors"
 "io"
 "os"
 "path/filepath"
 "time"

 archive "qimeng.local/next/server/internal/backup"
 "qimeng.local/next/server/internal/model"
 "qimeng.local/next/server/internal/store"
)

func(s *Service)Backup(ctx context.Context,p model.Principal,id string)(Backup,error){if e:=Authorize(p,"admin","");e!=nil{return Backup{},e};b,e:=store.Get[Backup](ctx,s.DB.DB,"SELECT data FROM backups WHERE id=?",id);if e==nil&&b.ExportState==""{b.ExportState="not_started"};return b,Normalize(e)}
func(s *Service)Backups(ctx context.Context,p model.Principal,cursor string,limit int)(model.Page[Backup],error){
 result:=model.Page[Backup]{Items:[]Backup{}};if e:=Authorize(p,"admin","");e!=nil{return result,e};n,e:=limited(limit,20);if e!=nil{return result,e};after,e:=readSeek(cursor,"backups:1","id");if e!=nil{return result,e}
 items,e:=store.List[Backup](ctx,s.DB.DB,"SELECT data FROM backups WHERE id>? ORDER BY id LIMIT ?",after.ID,n+1);if e!=nil{return result,e};if len(items)>n{result.NextCursor=makeSeek("backups:1","id","",items[n-1].ID);items=items[:n]};for i:=range items{if items[i].ExportState==""{items[i].ExportState="not_started"}};result.Items=items;return result,nil
}
func saveBackup(ctx context.Context,q store.Querier,b Backup)error{raw,e:=store.Encode(b);if e!=nil{return e};_,e=q.ExecContext(ctx,"UPDATE backups SET data=? WHERE id=?",raw,b.ID);return e}
func(s *Service)StartBackupExport(ctx context.Context,w *Write,id string)(Backup,error){
 b,e:=s.Backup(ctx,w.Principal,id);if e!=nil{return b,e};if b.State=="snapshot_not_included"{return b,Fail(409,"backup_snapshot_not_included","此项是迁入的历史备份记录，原检查点归档未包含在当前实例。")};if b.ExportState=="completed"||b.ExportState=="queued"||b.ExportState=="running"{return b,nil}
 var active int;if e=s.DB.DB.QueryRowContext(ctx,"SELECT COUNT(*) FROM backups WHERE json_extract(data,'$.exportState') IN ('queued','running')").Scan(&active);e!=nil{return b,e};if active>0{return b,Fail(409,"backup_export_busy","已有完整备份正在导出，请回读其进度。")}
 b.ExportState="queued";b.ExportError="";b.ExportedFiles=0;b.TotalFiles=0;q,e:=w.Q();if e!=nil{return b,e};if e=saveBackup(ctx,q,b);e!=nil{return b,e}
 w.after=append(w.after,func()error{s.workers.Add(1);go func(){defer s.workers.Done();s.exportBackup(s.workCtx,b)}();return nil});return b,nil
}
func(s *Service)setBackupProgress(ctx context.Context,b Backup)error{s.mu.Lock();defer s.mu.Unlock();return saveBackup(ctx,s.DB.DB,b)}

func(s *Service)backupSource(ctx context.Context,f archive.File)(*os.File,error){
 s.mu.Lock();defer s.mu.Unlock();name:=f.StoragePath
 if f.Kind=="asset"{a,e:=store.Get[model.Asset](ctx,s.DB.DB,"SELECT data FROM assets WHERE id=?",f.ObjectID);if e!=nil{return nil,Fail(409,"backup_source_unavailable","检查点中的原件已移除，不能生成完整备份。")};if a.Hash!=f.SHA256||a.Size!=f.Size||a.ContentVersion!=f.ContentVersion{return nil,Fail(409,"backup_source_changed","检查点中的原件版本已变化，请重新建立检查点。")};name=assetPath(a)
 }else if f.Kind=="upload"{u,e:=store.Get[model.Upload](ctx,s.DB.DB,"SELECT data FROM uploads WHERE id=?",f.ObjectID);if e!=nil{return nil,e};name=uploadPath(u.ID);if u.State=="ingested"{a,e:=store.Get[model.Asset](ctx,s.DB.DB,"SELECT data FROM assets WHERE id=?",u.AssetID);if e!=nil{return nil,e};if a.Hash!=u.Hash||a.Size!=u.Size{return nil,Fail(409,"backup_input_changed","检查点中的未完成输入已无法读取原始字节。")};name=assetPath(a)}}
 if e:=s.safe(name);e!=nil{return nil,e};file,e:=s.Root.Open(name);if e!=nil{return nil,e};info,e:=file.Stat();if e!=nil{file.Close();return nil,e};if !info.Mode().IsRegular()||info.Size()<f.Size||f.Kind!="upload"&&info.Size()!=f.Size{file.Close();return nil,Fail(409,"backup_length_changed","备份输入的长度与检查点不一致。")};return file,nil
}
func(s *Service)prepareArchive(ctx context.Context,b Backup)(archive.Manifest,error){
 manifest:=archive.Manifest{Format:archive.Format,Version:archive.Version,BackupID:b.ID,InstanceID:s.InstanceID,CreatedAt:b.CreatedAt,Files:[]archive.File{}}
 snapshot:="backups/"+b.ID+"/snapshot.sqlite";info,e:=s.Root.Stat(snapshot);if e!=nil{return manifest,e};if e=s.verify(snapshot,b.SnapshotHash,info.Size());e!=nil{return manifest,e};manifest.Snapshot=archive.File{Kind:"snapshot",ObjectID:b.ID,StoragePath:"qimeng.sqlite",SHA256:b.SnapshotHash,Size:info.Size()}
 db,e:=sql.Open("sqlite",store.FileURI(filepath.Join(s.Config.DataDir,filepath.FromSlash(snapshot)))+"?mode=ro&_pragma=trusted_schema(0)");if e!=nil{return manifest,e};defer db.Close();db.SetMaxOpenConns(1)
 add:=func(f archive.File)error{if len(manifest.Files)>=archive.MaxFiles{return Fail(422,"backup_manifest_budget","完整备份文件清单超过当前资源预算，未截断导出。")};manifest.Files=append(manifest.Files,f);return nil}
 last:="";for{assets,e:=store.List[model.Asset](ctx,db,"SELECT data FROM assets WHERE id>? ORDER BY id LIMIT 256",last);if e!=nil{return manifest,e};for _,a:=range assets{if e=add(archive.File{Kind:"asset",ObjectID:a.ID,ContentVersion:a.ContentVersion,StoragePath:assetPath(a),SHA256:a.Hash,Size:a.Size,ModifiedAt:a.ContentMtime});e!=nil{return manifest,e};last=a.ID};if len(assets)<256{break}}
 last="";for{uploads,e:=store.List[model.Upload](ctx,db,"SELECT data FROM uploads WHERE id>? AND state!='ingested' ORDER BY id LIMIT 128",last);if e!=nil{return manifest,e};for _,u:=range uploads{last=u.ID;file:=archive.File{Kind:"upload",ObjectID:u.ID,StoragePath:uploadPath(u.ID),Size:u.Offset};source,e:=s.backupSource(ctx,file);if e!=nil{return manifest,e};hash:=sha256.New();_,e=io.CopyN(hash,source,u.Offset);closeErr:=source.Close();if e!=nil{return manifest,e};if closeErr!=nil{return manifest,closeErr};file.SHA256=hex.EncodeToString(hash.Sum(nil));if u.Hash!=""&&(u.Offset!=u.Size||file.SHA256!=u.Hash){return manifest,Fail(409,"backup_input_changed","封存输入内容与检查点摘要不一致。")};if e=add(file);e!=nil{return manifest,e}};if len(uploads)<128{break}}
 return manifest,manifest.Validate()
}
type reserveWriter struct{ctx context.Context;s *Service;file *os.File;since int64}
func(w *reserveWriter)Write(p []byte)(int,error){if e:=w.ctx.Err();e!=nil{return 0,e};if w.since==0||w.since>=8<<20{free,e:=freeBytes(w.s.Config.DataDir);if e!=nil{return 0,e};if free-w.s.Config.ReserveBytes<int64(len(p))+(8<<20){return 0,Fail(507,"insufficient_storage","完整备份已保留存储余量，未发布不完整归档。")};w.since=0};n,e:=w.file.Write(p);w.since+=int64(n);return n,e}

func(s *Service)exportBackup(ctx context.Context,b Backup){
 b.ExportState="running";if e:=s.setBackupProgress(ctx,b);e!=nil{return};directory:="backups/"+b.ID;final:=directory+"/archive.qnext";temporary:=directory+"/archive.partial"
 fail:=func(e error){b.ExportState="failed";b.ExportError="backup_export_failed";if errors.Is(e,context.Canceled){b.ExportState="interrupted";b.ExportError="backup_export_interrupted"};var typed *Error;if errors.As(e,&typed){b.ExportError=typed.Code};_ = s.setBackupProgress(context.Background(),b)}
 // A process may stop after publishing the verified archive but before saving
 // its completion record. Recovery validates that exact archive instead of
 // silently replacing it with another snapshot.
 if existing,e:=s.Root.Open(final);e==nil{
  hash:=sha256.New();manifest,verifyErr:=archive.Read(ctx,io.TeeReader(existing,hash),nil);info,statErr:=existing.Stat();closeErr:=existing.Close();if verifyErr!=nil{fail(verifyErr);return};if statErr!=nil{fail(statErr);return};if closeErr!=nil{fail(closeErr);return};if manifest.BackupID!=b.ID||manifest.Snapshot.SHA256!=b.SnapshotHash{fail(Fail(409,"backup_archive_conflict","已有归档与此检查点不一致，已保留供核对。"));return};b.ArchiveHash=hex.EncodeToString(hash.Sum(nil));b.ArchiveBytes=info.Size();b.TotalFiles=len(manifest.Files);b.ExportedFiles=b.TotalFiles
 }else if !errors.Is(e,os.ErrNotExist){fail(e);return}else{
  manifest,e:=s.prepareArchive(ctx,b);if e!=nil{fail(e);return};b.TotalFiles=len(manifest.Files);if e=s.setBackupProgress(ctx,b);e!=nil{fail(e);return}
  file,e:=s.Root.OpenFile(temporary,os.O_WRONLY|os.O_CREATE|os.O_TRUNC,0600);if e!=nil{fail(e);return};hash:=sha256.New();writer:=io.MultiWriter(&reserveWriter{ctx:ctx,s:s,file:file},hash)
  open:=func(f archive.File)(io.ReadCloser,error){if f.Kind=="snapshot"{f.StoragePath=directory+"/snapshot.sqlite"};return s.backupSource(ctx,f)}
  e=archive.Write(ctx,writer,manifest,open,func(count int)error{b.ExportedFiles=count;if count%32==0||count==b.TotalFiles{return s.setBackupProgress(ctx,b)};return nil});if e==nil{e=file.Sync()};info,statErr:=file.Stat();closeErr:=file.Close();if e==nil{e=statErr};if e==nil{e=closeErr};if e!=nil{fail(e);return}
  if e=s.Root.Link(temporary,final);e!=nil{fail(e);return};if e=s.syncParent(final);e!=nil{fail(e);return};if e=s.Root.Remove(temporary);e!=nil{fail(e);return};b.ArchiveHash=hex.EncodeToString(hash.Sum(nil));b.ArchiveBytes=info.Size()
 }
 now:=time.Now().UTC();b.ExportedAt=&now;b.ExportState="completed";b.ExportError="";if e:=s.setBackupProgress(context.Background(),b);e!=nil{return}
}
func(s *Service)BackupArchive(ctx context.Context,p model.Principal,id string)(*os.File,Backup,error){b,e:=s.Backup(ctx,p,id);if e!=nil{return nil,b,e};if b.ExportState!="completed"{return nil,b,Fail(409,"backup_not_ready","完整归档尚未生成，请回读备份状态。")};f,e:=s.Root.Open("backups/"+b.ID+"/archive.qnext");return f,b,e}
