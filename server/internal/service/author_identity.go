package service

import (
 "context"
 "strings"
 "time"

 "qimeng.local/next/server/internal/authorlist"
 "qimeng.local/next/server/internal/model"
 "qimeng.local/next/server/internal/store"
)

func authorIdentityKeys(a model.Author)[]string{
 first:=a.Name;if names,e:=authorlist.Names(a.Name);e==nil{first=names[0]};if key:=authorlist.IdentityKey(first);key!=""{return []string{key}};return nil
}
func refreshAuthorKeys(ctx context.Context,q store.Querier,a model.Author)error{
 if _,e:=q.ExecContext(ctx,"DELETE FROM author_lookup_keys WHERE author_id=?",a.ID);e!=nil{return e};if a.Kind!="regular"&&!a.Placeholder{return nil};for _,key:=range authorIdentityKeys(a){if _,e:=q.ExecContext(ctx,"INSERT INTO author_lookup_keys(identity_key,author_id) VALUES(?,?)",key,a.ID);e!=nil{return e}};return nil
}
func(s *Service)initializeAuthorKeys(ctx context.Context)error{
 last:="";for{authors,e:=store.List[model.Author](ctx,s.DB.DB,"SELECT au.data FROM authors au WHERE au.id>? AND (au.id='unknown' OR json_extract(au.data,'$.kind')='regular') AND NOT EXISTS(SELECT 1 FROM author_lookup_keys k WHERE k.author_id=au.id) ORDER BY au.id LIMIT 128",last);if e!=nil{return e};if len(authors)==0{return nil};tx,e:=s.DB.DB.BeginTx(ctx,nil);if e!=nil{return e};for _,a:=range authors{last=a.ID;if e=refreshAuthorKeys(ctx,tx,a);e!=nil{tx.Rollback();return e}};if e=tx.Commit();e!=nil{return e}}
}
func lookupRegularAuthor(ctx context.Context,q store.Querier,key string)(model.Author,bool,error){
 authors,e:=store.List[model.Author](ctx,q,"SELECT au.data FROM author_lookup_keys k JOIN authors au ON au.id=k.author_id WHERE k.identity_key=? AND (au.id='unknown' OR json_extract(au.data,'$.kind')='regular') ORDER BY au.id LIMIT 2",key);if e!=nil{return model.Author{},false,e};if len(authors)>1{return model.Author{},false,Fail(409,"ambiguous_author","规范名称或别名对应多个作者，请先选择明确身份或合并。")};if len(authors)==1{return authors[0],true,nil};return model.Author{},false,nil
}
func newTextAuthor(id string,names []string,now time.Time)model.Author{return model.Author{ID:id,Kind:"regular",Name:strings.Join(names," / "),Aliases:append([]string{},names...),Revision:1,AcquisitionSources:[]string{},FieldStatus:map[string]string{"identity":"confirmed","biography":"missing","homepage":"missing"},CreatedAt:now,UpdatedAt:now}}
func saveNewRegularAuthor(ctx context.Context,q store.Querier,a model.Author)error{raw,e:=store.Encode(a);if e!=nil{return e};if _,e=q.ExecContext(ctx,"INSERT INTO authors(id,name,following,data) VALUES(?,?,?,?)",a.ID,a.Name,a.Following,raw);e!=nil{return e};return refreshAuthorKeys(ctx,q,a)}

// Planning names does not write. The calling use case can validate all fields,
// preserve fixed directory identities and commit new authors with their links.
func prepareRegularNames(ctx context.Context,q store.Querier,id func(string)string,values []string)([]model.Author,[]model.Author,error){
 if len(values)>20{return nil,nil,Fail(400,"too_many_authors","每项最多新增或复用20位常规作者。")};resolved:=[]model.Author{};created:=[]model.Author{};staged:=map[string]model.Author{};seen:=map[string]bool{};now:=time.Now().UTC()
 for _,value:=range values{names,e:=authorlist.Names(value);if e!=nil{return nil,nil,Fail(400,"invalid_author_name","作者名称、别名分隔或备注格式无效。")};key:=authorlist.IdentityKey(names[0]);a,ok:=staged[key];if !ok{a,ok,e=lookupRegularAuthor(ctx,q,key);if e!=nil{return nil,nil,e}};if !ok{a=newTextAuthor(id("regular-author:"+key),names,now);created=append(created,a);staged[key]=a};if !seen[a.ID]{seen[a.ID]=true;resolved=append(resolved,a)}};return resolved,created,nil
}
