package service

import (
 "context"
 "crypto/sha256"
 "database/sql"
 "encoding/hex"
 "errors"
 "fmt"
 "io"
 "os"
 "path"
 "strings"
 "time"

 "qimeng.local/next/server/internal/domain"
 "qimeng.local/next/server/internal/model"
 "qimeng.local/next/server/internal/store"
)

type fileIntent struct {
 ID,Kind,Source,Target,Retired,UploadID string
 PrincipalID,Operation,Key,InputHash string
 Before *model.Asset
 After model.Asset
 COSAuthor *model.Author `json:"cosAuthor,omitempty"`
 COSWork *model.CosWork `json:"cosWork,omitempty"`
 State string
}
func assetPath(a model.Asset)string{if a.Trashed{if a.StorageLayoutVersion>=2{return "library-trash/"+a.LibraryID+"/"+a.ID+"/"+a.FileName};return "trash/"+a.ID+"/"+a.FileName};return "libraries/"+a.LibraryID+"/"+a.RelativePath}
func(s *Service)safe(pathname string)error{
 if !domain.ValidRelative(pathname){return Fail(400,"invalid_path","路径不合法。")}
 parts:=strings.Split(pathname,"/")
 for i:=range parts{info,e:=s.Root.Lstat(strings.Join(parts[:i+1],"/"));if errors.Is(e,os.ErrNotExist){return nil};if e!=nil{return e};if info.Mode()&os.ModeSymlink!=0{return Fail(400,"symlink_denied","受管文件路径不能包含链接。")}}
 return nil
}
func(s *Service)ensureParent(name string)error{if e:=s.safe(name);e!=nil{return e};return s.Root.MkdirAll(path.Dir(name),0700)}
func(s *Service)hashFile(name string)(string,int64,error){
 if e:=s.safe(name);e!=nil{return "",0,e};f,e:=s.Root.Open(name);if e!=nil{return "",0,e};defer f.Close();h:=sha256.New();n,e:=io.Copy(h,f);if e!=nil{return "",n,e};return hex.EncodeToString(h.Sum(nil)),n,nil
}
func(s *Service)verify(name,hash string,size int64)error{actual,n,e:=s.hashFile(name);if e!=nil{return e};if actual!=hash||n!=size{return Fail(409,"content_mismatch","文件内容发生变化，已保留文件供恢复核对。")};return nil}
func(s *Service)linkNoReplace(source,target string)error{
 if e:=s.safe(source);e!=nil{return e};if e:=s.ensureParent(target);e!=nil{return e}
 if e:=s.Root.Link(source,target);e!=nil{
  if !errors.Is(e,os.ErrExist){return e};a,ae:=s.Root.Stat(source);b,be:=s.Root.Stat(target);if ae!=nil{return ae};if be!=nil{return be};if !os.SameFile(a,b){return Fail(409,"path_occupied","目标已有其他文件，不会覆盖。")}
 }
 return s.syncParent(target)
}
func(s *Service)syncParent(name string)error{f,e:=s.Root.OpenParent(name);if e!=nil{return e};defer f.Close();return syncDirectory(f)}
func(s *Service)removeVerified(name,hash string,size int64)error{if _,e:=s.Root.Lstat(name);errors.Is(e,os.ErrNotExist){return nil}else if e!=nil{return e};if e:=s.verify(name,hash,size);e!=nil{return e};if e:=s.Root.Remove(name);e!=nil{return e};return s.syncParent(name)}
func(s *Service)saveIntent(ctx context.Context,j fileIntent)error{raw,e:=store.Encode(j);if e!=nil{return e};_,e=s.DB.DB.ExecContext(ctx,"INSERT INTO file_intents(id,kind,state,data) VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET state=excluded.state,data=excluded.data",j.ID,j.Kind,j.State,raw);return e}
func(s *Service)prepareFile(ctx context.Context,w *Write,j fileIntent)(fileIntent,error){
 if w.tx!=nil{return j,errors.New("file intent must precede transaction")}
 old,e:=store.Get[fileIntent](ctx,s.DB.DB,"SELECT data FROM file_intents WHERE id=?",j.ID)
 if e==nil{if old.Kind!=j.Kind||old.UploadID!=j.UploadID||old.Operation!=w.operation{return j,Fail(409,"publication_pending","此输入已有不同发布操作，须先回读原操作并恢复，不能用新计划覆盖。")};return old,nil};if !errors.Is(e,sql.ErrNoRows){return j,e}
 j.State="prepared";j.PrincipalID=w.Principal.ID;j.Operation=w.operation;j.Key=w.key;j.InputHash=w.inputHash;return j,s.saveIntent(ctx,j)
}
func(s *Service)applyFile(j fileIntent)error{
 if j.Kind=="purge"{return s.removeVerified(j.Source,j.After.Hash,j.After.Size)}
 if e:=s.verify(j.Source,j.After.Hash,j.After.Size);e!=nil{return e}
 if j.Kind=="replace"&&j.Before!=nil{
  oldPath:=assetPath(*j.Before)
  if info,e:=s.Root.Stat(oldPath);e==nil{
   sourceInfo,se:=s.Root.Stat(j.Source);if se!=nil{return se}
   if !os.SameFile(info,sourceInfo){
    if e=s.verify(oldPath,j.Before.Hash,j.Before.Size);e!=nil{return e}
    if e=s.linkNoReplace(oldPath,j.Retired);e!=nil{return e}
    if oldPath==j.Target{if e=s.removeVerified(oldPath,j.Before.Hash,j.Before.Size);e!=nil{return e}}
   }
  }else if !errors.Is(e,os.ErrNotExist){return e}else if e=s.verify(j.Retired,j.Before.Hash,j.Before.Size);e!=nil{return e}
 }
 if e:=s.linkNoReplace(j.Source,j.Target);e!=nil{return e};return s.verify(j.Target,j.After.Hash,j.After.Size)
}
func(s *Service)commitFile(ctx context.Context,q store.Querier,j fileIntent)error{
 var receipt any=j.After;status:=200
 if e:=saveCOSPlacement(ctx,q,j.COSAuthor,j.COSWork);e!=nil{return e}
 if j.Kind=="purge"{if _,e:=q.ExecContext(ctx,"DELETE FROM assets WHERE id=?",j.After.ID);e!=nil{return e}}else if e:=store.SaveAsset(ctx,q,j.After);e!=nil{return e}
 if j.UploadID!=""{
  u,e:=store.Get[model.Upload](ctx,q,"SELECT data FROM uploads WHERE id=?",j.UploadID);if e!=nil{return e};u.AssetID=j.After.ID;u.State="ingested";u.ErrorCode="";u.ErrorMessage="";if e=store.SaveUpload(ctx,q,u);e!=nil{return e}
  if u.BatchID!=""{if e=s.refreshBatch(ctx,q,u.BatchID);e!=nil{return e}}
  receipt=model.UploadResult{Upload:u,Asset:&j.After}
 }
 if j.Kind=="purge"{status=204}
 if j.Key!=""{body:=[]byte{};if status!=204{raw,e:=store.Encode(receipt);if e!=nil{return e};body=[]byte(raw)};if _,e:=q.ExecContext(ctx,"INSERT OR IGNORE INTO idempotency(principal_id,operation,key,input_hash,response,status) VALUES(?,?,?,?,?,?)",j.PrincipalID,j.Operation,j.Key,j.InputHash,body,status);e!=nil{return e}}
 j.State="committed";raw,e:=store.Encode(j);if e!=nil{return e};_,e=q.ExecContext(ctx,"UPDATE file_intents SET state=?,data=? WHERE id=?",j.State,raw,j.ID);return e
}
func(s *Service)finishFile(ctx context.Context,j fileIntent)error{
 if j.Kind!="purge"&&j.Source!=j.Target{if e:=s.removeVerified(j.Source,j.After.Hash,j.After.Size);e!=nil{return e}}
 if j.Kind=="replace"&&j.Before!=nil{
  old:=assetPath(*j.Before);if old!=j.Target{if e:=s.removeVerified(old,j.Before.Hash,j.Before.Size);e!=nil{return e}}
  if e:=s.removeVerified(j.Retired,j.Before.Hash,j.Before.Size);e!=nil{return e}
 }
 j.State="done";return s.saveIntent(ctx,j)
}
func(s *Service)runFile(ctx context.Context,w *Write,j fileIntent)(model.Asset,error){
 j.After.FileDate=j.After.EffectiveFileDate()
 if j.Kind=="ingest"||j.Kind=="move"||j.Kind=="restore"{var e error;j.COSAuthor,j.COSWork,e=s.planCOSPlacement(ctx,s.DB.DB,&j.After);if e!=nil{return j.After,e}}
 j,e:=s.prepareFile(ctx,w,j);if e!=nil{return j.After,e}
 if j.State=="prepared"{if e=s.applyFile(j);e!=nil{return j.After,e}}
 q,e:=w.Q();if e!=nil{return j.After,e};if e=s.commitFile(ctx,q,j);e!=nil{return j.After,e}
 w.after=append(w.after,func()error{return s.finishFile(context.Background(),j)});return j.After,nil
}
func(s *Service)recoverIntents(ctx context.Context)error{
 intents,e:=store.List[fileIntent](ctx,s.DB.DB,"SELECT data FROM file_intents WHERE state!='done' ORDER BY id");if e!=nil{return e}
 for _,j:=range intents{
  if j.State=="prepared"{if e=s.applyFile(j);e!=nil{return fmt.Errorf("intent %s: %w",j.ID,e)};tx,e:=s.DB.DB.BeginTx(ctx,nil);if e!=nil{return e};if e=s.commitFile(ctx,tx,j);e!=nil{tx.Rollback();return e};if e=tx.Commit();e!=nil{return e}}
  if e=s.finishFile(ctx,j);e!=nil{return fmt.Errorf("intent %s cleanup: %w",j.ID,e)}
 }
 return nil
}
func(s *Service)Move(ctx context.Context,w *Write,id,target string,rev int64)(model.Asset,error){
 if e:=Authorize(w.Principal,"admin","");e!=nil{return model.Asset{},e};a,e:=s.Asset(ctx,id);if e!=nil{return a,e};if a.Trashed{return a,Fail(409,"asset_trashed","请先恢复媒体。")};if a.Revision!=rev{return a,Conflict(a.Revision)}
 if !domain.ValidMediaPath(target)||target==""{return a,Fail(400,"invalid_path","目标相对路径无效。")};_,mime,ok:=domain.MediaType(target);if !ok||mime!=a.MimeType{return a,Fail(400,"extension_mismatch","改名不能改变媒体格式扩展名。")}
 if target==a.RelativePath{return a,nil};if e=s.checkPath(ctx,a.LibraryID,target,a.ID);e!=nil{return a,e}
 old:=a;a.RelativePath=target;a.FileName=path.Base(target);a.Revision++;a.UpdatedAt=time.Now().UTC()
 return s.runFile(ctx,w,fileIntent{ID:w.ID("file"),Kind:"move",Source:assetPath(old),Target:assetPath(a),Before:&old,After:a})
}
func(s *Service)Trash(ctx context.Context,w *Write,id string,rev int64)(model.Asset,error){
 if e:=Authorize(w.Principal,"admin","");e!=nil{return model.Asset{},e};a,e:=s.Asset(ctx,id);if e!=nil{return a,e};if a.Revision!=rev{return a,Conflict(a.Revision)};if a.Trashed{return a,nil};old:=a;a.Trashed=true;a.StorageLayoutVersion=2;a.Revision++;a.UpdatedAt=time.Now().UTC();return s.runFile(ctx,w,fileIntent{ID:w.ID("file"),Kind:"trash",Source:assetPath(old),Target:assetPath(a),Before:&old,After:a})
}
func(s *Service)Restore(ctx context.Context,w *Write,id string,rev int64)(model.Asset,error){
 if e:=Authorize(w.Principal,"admin","");e!=nil{return model.Asset{},e};a,e:=s.Asset(ctx,id);if e!=nil{return a,e};if a.Revision!=rev{return a,Conflict(a.Revision)};if !a.Trashed{return a,nil};old:=a;a.Trashed=false
 target,e:=s.availablePath(ctx,a.LibraryID,a.RelativePath);if e!=nil{return a,e};a.RelativePath=target;a.FileName=path.Base(target);a.Revision++;a.UpdatedAt=time.Now().UTC();return s.runFile(ctx,w,fileIntent{ID:w.ID("file"),Kind:"restore",Source:assetPath(old),Target:assetPath(a),Before:&old,After:a})
}
func(s *Service)Purge(ctx context.Context,w *Write,id string,rev int64)error{
 if e:=Authorize(w.Principal,"admin","");e!=nil{return e};a,e:=s.Asset(ctx,id);if e!=nil{return e};if !a.Trashed{return Fail(409,"trash_required","永久删除只能操作回收站内的资产。")};if a.Revision!=rev{return Conflict(a.Revision)};_,e=s.runFile(ctx,w,fileIntent{ID:w.ID("file"),Kind:"purge",Source:assetPath(a),After:a});return e
}
func(s *Service)checkPath(ctx context.Context,library,relative,except string)error{
 var count int;if e:=s.DB.DB.QueryRowContext(ctx,"SELECT COUNT(*) FROM assets WHERE library_id=? AND relative_path=? AND trashed=0 AND id!=?",library,relative,except).Scan(&count);e!=nil{return e};if count>0{return Fail(409,"path_occupied","目标名称已被占用。")};return nil
}
func(s *Service)availablePath(ctx context.Context,library,relative string)(string,error){
 ext:=path.Ext(relative);stem:=strings.TrimSuffix(relative,ext)
 for n:=1;n<100000;n++{candidate:=relative;if n>1{candidate=fmt.Sprintf("%s (%d)%s",stem,n,ext)};e:=s.checkPath(ctx,library,candidate,"");if e!=nil{var ae *Error;if errors.As(e,&ae)&&ae.Code=="path_occupied"{continue};return "",e};_,e=s.Root.Lstat("libraries/"+library+"/"+candidate);if errors.Is(e,os.ErrNotExist){return candidate,nil};if e!=nil{return "",e}}
 return "",Fail(409,"name_exhausted","无法分配可用文件名。")
}
func(s *Service)Content(ctx context.Context,id string,versions ...int64)(*os.File,model.Asset,error){s.mu.Lock();defer s.mu.Unlock();a,e:=s.Asset(ctx,id);if e!=nil{return nil,a,e};if len(versions)>0&&versions[0]>0&&versions[0]!=a.ContentVersion{return nil,a,Fail(409,"content_changed","媒体内容版本已变化，请刷新条目。")};if a.Trashed{return nil,a,Fail(404,"not_found","媒体位于回收站。")};name:=assetPath(a);if e=s.safe(name);e!=nil{return nil,a,e};f,e:=s.Root.Open(name);return f,a,e}
