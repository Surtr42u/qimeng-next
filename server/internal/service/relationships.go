package service

import (
 "context"

 "qimeng.local/next/server/internal/model"
 "qimeng.local/next/server/internal/store"
)

func(s *Service)resolveAuthors(ctx context.Context,libraryID string,ids []string)([]model.Author,error){
 library,e:=s.library(ctx,libraryID);if e!=nil{return nil,e};if len(ids)>20{return nil,Fail(400,"too_many_authors","每项最多关联 20 位作者。")}
 actual:=[]model.Author{};seen:=map[string]bool{}
 for _,id:=range ids{if seen[id]||id=="unknown"{continue};seen[id]=true;a,e:=store.Get[model.Author](ctx,s.DB.DB,"SELECT data FROM authors WHERE id=?",id);if e!=nil{return nil,Normalize(e)};if a.Kind=="cos"&&library.Kind!="cos"{return nil,Fail(409,"author_kind_conflict","目录 COS 身份只能属于相应库；常规作者可作为额外关联。")};actual=append(actual,a)}
 if len(actual)==0{a,e:=store.Get[model.Author](ctx,s.DB.DB,"SELECT data FROM authors WHERE id='unknown'");if e!=nil{return nil,e};actual=append(actual,a)}
 return actual,nil
}
// Editing regular creator associations cannot remove or manufacture the fixed
// COS identities which were established by the directory reconciliation use case.
func(s *Service)replaceAssetAuthors(ctx context.Context,w *Write,q store.Querier,a *model.Asset,in AssetUpdate)error{
 if in.ExpectedAuthorsRevision==0 && len(in.NewAuthorNames)>0{return Fail(400,"authors_revision_required","按名挂靠须提供当前作者修订号。")}
 if in.ExpectedAuthorsRevision!=0&&in.ExpectedAuthorsRevision!=a.AuthorsRevision{return Conflict(a.AuthorsRevision)}
 if len(in.NewAuthorNames)>0&&w.key==""{return Fail(400,"idempotency_required","按名挂靠作者须携带本次操作的幂等键。")}
 if in.AuthorIDs!=nil&&in.AuthorID!=nil{return Fail(400,"author_selection_conflict","请只提交一种已有作者选择。")}
 requested:=[]string{}
 if in.AuthorIDs!=nil{requested=*in.AuthorIDs}else if in.AuthorID!=nil{requested=[]string{*in.AuthorID}}
 if len(requested)>20{return Fail(400,"too_many_authors","每项最多关联20位作者。")}
 get:=func(id string)(model.Author,error){return store.Get[model.Author](ctx,q,"SELECT data FROM authors WHERE id=?",id)}
 selected:=[]model.Author{};seen:=map[string]bool{}
 appendAuthor:=func(author model.Author){if !seen[author.ID]&&!author.Placeholder{selected=append(selected,author);seen[author.ID]=true}}
 for _,id:=range a.AuthorIDs{author,e:=get(id);if e!=nil{return e};if author.Kind=="cos"{appendAuthor(author)}}
 for _,id:=range requested{author,e:=get(id);if e!=nil{return Normalize(e)};if author.Kind=="cos"&&!seen[id]{return Fail(409,"cos_directory_managed","作者编辑不能添加其他目录的 COS 身份。")};appendAuthor(author)}
 named,created,e:=prepareRegularNames(ctx,q,w.ID,in.NewAuthorNames);if e!=nil{return e}
 for _,author:=range named{appendAuthor(author)}
 if len(selected)>20{return Fail(400,"too_many_authors","每项最多关联20位作者。")}
 if len(selected)==0{unknown,e:=get("unknown");if e!=nil{return e};selected=append(selected,unknown)}
 for _,author:=range created{if e=saveNewRegularAuthor(ctx,q,author);e!=nil{return e}}
 oldIDs:=a.AuthorIDs;setAuthors(a,selected);if !sameAuthorSet(oldIDs,a.AuthorIDs)||oldIDs[0]!=a.AuthorID{a.AuthorsRevision++}
 return nil
}
func setAuthors(a *model.Asset,authors []model.Author){a.AuthorIDs=[]string{};for _,author:=range authors{a.AuthorIDs=append(a.AuthorIDs,author.ID)};a.AuthorID=authors[0].ID;a.AuthorName=authors[0].Name;a.AuthorStatus="confirmed";if authors[0].Placeholder{a.AuthorStatus="unknown"}}
