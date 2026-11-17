package service

import (
 "context"

 "qimeng.local/next/server/internal/model"
)

func(s *Service)resolveAuthors(ctx context.Context,libraryID string,ids []string)([]model.Author,error){
 library,e:=s.library(ctx,libraryID);if e!=nil{return nil,e};if len(ids)>20{return nil,Fail(400,"too_many_authors","每项最多关联 20 位作者。")}
 actual:=[]model.Author{};seen:=map[string]bool{}
 for _,id:=range ids{if seen[id]||id=="unknown"{continue};seen[id]=true;a,e:=s.Author(ctx,id);if e!=nil{return nil,e};if a.Kind=="cos"&&library.Kind!="cos"{return nil,Fail(409,"author_kind_conflict","目录 COS 身份只能属于相应库；常规作者可作为额外关联。")};actual=append(actual,a)}
 if len(actual)==0{a,e:=s.Author(ctx,"unknown");if e!=nil{return nil,e};actual=append(actual,a)}
 return actual,nil
}
// Editing regular creator associations cannot remove or manufacture the fixed
// COS identities which were established by the directory reconciliation use case.
func(s *Service)authorsForAssetEdit(ctx context.Context,a model.Asset,requested []string)([]model.Author,error){
 fixed:=[]string{};for _,id:=range a.AuthorIDs{if id=="unknown"{continue};author,e:=s.Author(ctx,id);if e!=nil{return nil,e};if author.Kind=="cos"{fixed=appendUnique(fixed,id)}}
 ids:=append([]string{},fixed...);for _,id:=range requested{if id=="unknown"{continue};author,e:=s.Author(ctx,id);if e!=nil{return nil,e};if author.Kind=="cos"{found:=false;for _,old:=range fixed{if old==id{found=true;break}};if !found{return nil,Fail(409,"cos_directory_managed","作者编辑不能添加其他目录的 COS 身份。")};continue};ids=appendUnique(ids,id)};return s.resolveAuthors(ctx,a.LibraryID,ids)
}
func setAuthors(a *model.Asset,authors []model.Author){a.AuthorIDs=[]string{};for _,author:=range authors{a.AuthorIDs=append(a.AuthorIDs,author.ID)};a.AuthorID=authors[0].ID;a.AuthorName=authors[0].Name;a.AuthorStatus="confirmed";if authors[0].Placeholder{a.AuthorStatus="unknown"}}
