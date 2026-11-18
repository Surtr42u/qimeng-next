package service

import (
 "context"
 "qimeng.local/next/server/internal/model"
 "qimeng.local/next/server/internal/store"
)

func removeAssetAuthorWorks(ctx context.Context,q store.Querier,a model.Asset)error{
 for _,id:=range a.AuthorIDs{
  if id=="unknown"{continue}
  if e:=removeLiteralAuthorWork(ctx,q,id,a.ID,a.FileName);e!=nil{return e}
 }
 return nil
}

func(s *Service)finalizeAuthorTextForFile(ctx context.Context,q store.Querier,j *fileIntent)error{
 a:=&j.After
 if j.Before!=nil&&j.Before.FileName!=a.FileName{
  if e:=s.renameAuthorListWorks(ctx,q,*a,j.Before.FileName);e!=nil{return e}
 }
 if j.Kind=="ingest"{
  // The authenticated upload or accepted AI plan is an explicit assignment.
  // Filename matches discovered below must never inherit that provenance.
  if e:=s.recordExplicitAuthorSet(ctx,q,nil,*a);e!=nil{return e}
 }
 if j.UploadID!=""{
  u,e:=store.Get[model.Upload](ctx,q,"SELECT data FROM uploads WHERE id=?",j.UploadID);if e!=nil{return e}
  if e=s.appendUploadAuthorSources(ctx,q,*a,u);e!=nil{return e}
 }
 if j.Kind!="trash"{return s.matchAuthorListsForAsset(ctx,q,a)}
 return nil
}
