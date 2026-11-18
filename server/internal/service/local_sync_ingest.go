package service

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "errors"
 "io"
 "os"
 "path"
 "strings"

 "qimeng.local/next/server/internal/authorlist"
 "qimeng.local/next/server/internal/domain"
 "qimeng.local/next/server/internal/model"
)

func verifySyncStamp(root *os.Root,name,stamp string)error{
 f,info,e:=openSyncSource(root,name);if e!=nil{return e};defer f.Close()
 actual,e:=syncFileStamp(f,info);if e!=nil{return e}
 if actual!=stamp{return Fail(409,"local_sync_changed","来源在复制期间变化，本次不发布。")};return nil
}

func(s *Service)importSyncSource(ctx context.Context,root *os.Root,file *os.File,info os.FileInfo,stamp string,lib model.Library,cycle string,item *LocalSyncItem,obs *syncObservation)error{
 key:=digest([]byte(s.localSync.rootID+"\x00"+item.Path+"\x00"+stamp+"\x00"+lib.ID))
 owner:=syncOwner()
 if item.Kind=="txt"{
  if info.Size()>authorlist.MaxTextBytes{return Fail(413,"author_list_too_large","TXT 超过投放通道容量。")}
  body,e:=io.ReadAll(io.LimitReader(file,authorlist.MaxTextBytes+1));if e!=nil{return e}
  if len(body)>authorlist.MaxTextBytes||int64(len(body))!=info.Size(){return Fail(409,"local_sync_changed","TXT 在读取期间变化。")}
  if e=verifySyncStamp(root,item.Path,stamp);e!=nil{return e}
  _,e=s.Run(ctx,owner,"localSyncAuthorList",key,[]byte(digest(body)),func(w *Write)(any,int,error){
   var revision int64
   if e:=s.DB.DB.QueryRowContext(ctx,"SELECT COALESCE((SELECT revision FROM author_lists WHERE name=?),0)",item.Path).Scan(&revision);e!=nil{return nil,0,e}
   value,e:=s.ImportAuthorList(ctx,w,AuthorListInput{Name:item.Path,Content:string(body),ExpectedRevision:&revision,ConflictResolution:"keep"})
   return value,200,e
  });return e
 }
 var u model.Upload
 var e error
 if obs.uploadID!=""{
  u,e=s.Upload(ctx,owner,obs.uploadID);if e!=nil{return e}
  if u.LibraryID!=lib.ID{return Fail(409,"local_sync_target_changed","来源已经绑定另一目标库，请核对原接收记录。")}
  switch u.State{case "ingested":return nil;case "duplicate":return Fail(409,"duplicate_content","已有相同内容，来源保留。");case "cancelled":return Fail(409,"upload_cancelled","原接收任务已取消，来源保留。")}
 }else{
  _,relative,_:=strings.Cut(item.Path,"/");directory:=path.Dir(relative);if directory=="."{directory=""}
  // Hash the observed version before creating a streaming upload. Completion
  // independently verifies those bytes even if a writer overwrites the source.
  hash:=sha256.New();buffer:=make([]byte,1<<20)
  for{if e=ctx.Err();e!=nil{return e};n,readErr:=file.Read(buffer);if n>0{hash.Write(buffer[:n])};if errors.Is(readErr,io.EOF){break};if readErr!=nil{return readErr}}
  if e=verifySyncStamp(root,item.Path,stamp);e!=nil{return e}
  _,mime,_:=domain.MediaType(relative);modified:=info.ModTime().UTC()
  in:=model.UploadInput{LibraryID:lib.ID,FileName:path.Base(relative),OriginalFileName:path.Base(relative),RelativeDirectory:directory,SourceRelativePath:item.Path,Size:info.Size(),ExpectedHash:hex.EncodeToString(hash.Sum(nil)),MimeType:mime,Mode:"ordinary",SourceMtime:&modified}
  u,e=syncResult[model.Upload](s.Run(ctx,owner,"localSyncCreateUpload",key,[]byte(in.ExpectedHash),func(w *Write)(any,int,error){v,e:=s.CreateUpload(ctx,w,in);return v,201,e}));if e!=nil{return e}
  // The creation receipt can predate a lost local observation write.
  u,e=s.Upload(ctx,owner,u.ID);if e!=nil{return e};obs.uploadID=u.ID
  item.State="waiting-stable";item.Attempts=obs.attempts
  if e=s.saveSyncObservation(ctx,*item,*obs,cycle);e!=nil{return e}
 }
 if u.State=="receiving"{
  if _,e=file.Seek(u.Offset,io.SeekStart);e!=nil{return e}
  buf:=make([]byte,2<<20)
  for offset:=u.Offset;offset<u.Size;{
   size:=min(int64(len(buf)),u.Size-offset);if _,e=io.ReadFull(file,buf[:size]);e!=nil{return Fail(409,"local_sync_changed","来源变短，未完成发布。")}
   _,e=s.Run(ctx,owner,"localSyncAppend","",nil,func(w *Write)(any,int,error){v,e:=s.AppendUpload(ctx,w,u.ID,offset,buf[:size]);return v,200,e});if e!=nil{return e};offset+=size
  }
 }
 if e=verifySyncStamp(root,item.Path,stamp);e!=nil{return e}
 completed,e:=syncResult[model.UploadResult](s.Run(ctx,owner,"localSyncComplete",key,[]byte(u.ID),func(w *Write)(any,int,error){
  libs,e:=s.localSyncLibraries(ctx);if e!=nil{return nil,0,e};target,e:=findSyncLibrary(libs,item.Path);if e!=nil{return nil,0,e}
  if lib.ID!=target.ID{return nil,0,Fail(409,"local_sync_target_changed","投放目标在复制时变化。")}
  v,e:=s.CompleteUpload(ctx,w,u.ID);return v,200,e
 }))
 if e!=nil{
  var bad *Error;if errors.As(e,&bad)&&bad.Status==415{if cancelErr:=s.cancelSyncUpload(ctx,u.ID);cancelErr!=nil{return cancelErr}}
  return e
 }
 if completed.Upload.State=="duplicate"{return Fail(409,"duplicate_content","已有相同内容，来源保留。")}
 if completed.Asset==nil||completed.Upload.State!="ingested"||completed.Asset.Hash!=u.ExpectedHash{return Fail(409,"local_sync_result_unconfirmed","入库回读未确认，来源保留。")}
 return nil
}
