package service

import(
 "context"
 "errors"
 "os"

 "qimeng.local/next/server/internal/store"
)

func(s *Service)validatePublicationBase(ctx context.Context,q store.Querier,j fileIntent)error{
 before:=j.Before;if before==nil&&j.Kind=="purge"{before=&j.After}
 if before==nil{return nil}
 var revision,version int64;var relative,hash string;var trashed bool
 e:=q.QueryRowContext(ctx,"SELECT revision,relative_path,hash,trashed,json_extract(data,'$.contentVersion') FROM assets WHERE id=?",before.ID).Scan(&revision,&relative,&hash,&trashed,&version)
 if e!=nil{return e}
 if revision!=before.Revision||version!=before.ContentVersion||relative!=before.RelativePath||hash!=before.Hash||trashed!=before.Trashed{
  return Fail(409,"publication_base_changed","未决文件操作的原业务版本已改变，已保留现有字段和原件。")
 }
 return nil
}

func(s *Service)managedFileIdentity(name string)(string,error){
 if e:=s.safe(name);e!=nil{return "",e};file,e:=s.Root.Open(name);if e!=nil{return "",e};defer file.Close()
 info,e:=file.Stat();if e!=nil{return "",e};if !info.Mode().IsRegular(){return "",Fail(409,"original_changed","文件对象已经改变。")};identity,e:=fileObjectIdentity(file,info);if e!=nil{return "",e};return digest([]byte(identity)),nil
}
func(s *Service)verifyFileIdentity(name,expected string)error{
 if expected==""{return nil}
 actual,e:=s.managedFileIdentity(name);if e!=nil{return e};if actual!=expected{return Fail(409,"original_object_changed","同名路径指向了其他文件对象，已保留供核对。")};return nil
}
func(s *Service)removeFileObject(name,expected,hash string,size int64)error{
 if _,e:=s.Root.Lstat(name);errors.Is(e,os.ErrNotExist){return nil}else if e!=nil{return e}
 if e:=s.verify(name,hash,size);e!=nil{return e};if e:=s.verifyFileIdentity(name,expected);e!=nil{return e}
 if e:=s.Root.Remove(name);e!=nil{return e};return s.syncParent(name)
}

// If a crash left the source link intact, require the target to be that same
// object. Restore a deleted target from the original verified link first.
func(s *Service)finishPublishedLink(j fileIntent)error{
 source,e:=s.Root.Lstat(j.Source)
 if errors.Is(e,os.ErrNotExist){
  if e=s.verify(j.Target,j.After.Hash,j.After.Size);e!=nil{return e};return s.verifyFileIdentity(j.Target,j.SourceObject)
 }
 if e!=nil{return e};if e=s.verifyFileIdentity(j.Source,j.SourceObject);e!=nil{return e}
 target,e:=s.Root.Lstat(j.Target)
 if errors.Is(e,os.ErrNotExist){
  if e=s.verify(j.Source,j.After.Hash,j.After.Size);e!=nil{return e}
  if e=s.linkNoReplace(j.Source,j.Target);e!=nil{return e};target,e=s.Root.Lstat(j.Target)
 }
 if e!=nil{return e}
 if !os.SameFile(source,target){return Fail(409,"publication_target_changed","发布落点与已保留的源不再是同一文件，两者均已保留。")}
 if e=s.verify(j.Target,j.After.Hash,j.After.Size);e!=nil{return e}
 identity,e:=s.managedFileIdentity(j.Target);if e!=nil{return e}
 if j.SourceObject!=""&&j.SourceObject!=identity{return Fail(409,"publication_target_changed","发布后的文件对象已改变。")}
 if e=s.verifyFileIdentity(j.Source,identity);e!=nil{return e}
 if e=s.Root.Remove(j.Source);e!=nil{return e};return s.syncParent(j.Source)
}

func(s *Service)reservePublication(ctx context.Context,j fileIntent)error{
 var occupied bool
 e:=s.DB.DB.QueryRowContext(ctx,`SELECT EXISTS(SELECT 1 FROM file_intents f WHERE f.state IN ('prepared','committed') AND (
  (?!='' AND json_extract(f.data,'$.After.id')=?) OR
  (?!='' AND ? IN (json_extract(f.data,'$.Source'),json_extract(f.data,'$.Target'))) OR
  (?!='' AND ? IN (json_extract(f.data,'$.Source'),json_extract(f.data,'$.Target')))))`,j.After.ID,j.After.ID,j.Source,j.Source,j.Target,j.Target).Scan(&occupied)
 if e!=nil{return e};if occupied{return Fail(409,"publication_pending","该资产或路径已有未收尾的文件操作，请先恢复原操作。")};return nil
}
