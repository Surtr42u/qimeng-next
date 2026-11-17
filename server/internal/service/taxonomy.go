package service

import (
 "context"
 "strings"
 "time"

 "qimeng.local/next/server/internal/model"
 "qimeng.local/next/server/internal/store"
)

type Source struct{ID string `json:"id"`;Name string `json:"name"`;Aliases []string `json:"aliases"`;Revision int64 `json:"revision"`;CreatedAt time.Time `json:"createdAt"`;UpdatedAt time.Time `json:"updatedAt"`}
type Character struct{Source;SourceID string `json:"sourceId"`}
type TermInput struct{Name string `json:"name"`;Aliases []string `json:"aliases"`;SourceID string `json:"sourceId,omitempty"`;ExpectedRevision int64 `json:"expectedRevision,omitempty"`}
func(s *Service)Sources(ctx context.Context,query,cursor string,limit int)(model.Page[Source],error){n,e:=limited(limit,50);if e!=nil{return model.Page[Source]{},e};key:="sources:"+query;o,e:=pageOffset(cursor,key);if e!=nil{return model.Page[Source]{},e};a,e:=store.List[Source](ctx,s.DB.DB,"SELECT data FROM sources WHERE name LIKE ? ESCAPE '\\' OR json_extract(data,'$.aliases') LIKE ? ESCAPE '\\' ORDER BY name,id LIMIT ? OFFSET ?",like(query),like(query),n+1,o);return paged(a,n,o,key),e}
func(s *Service)Characters(ctx context.Context,sourceID,query,cursor string,limit int)(model.Page[Character],error){n,e:=limited(limit,50);if e!=nil{return model.Page[Character]{},e};key:="characters:"+sourceID+":"+query;o,e:=pageOffset(cursor,key);if e!=nil{return model.Page[Character]{},e};a,e:=store.List[Character](ctx,s.DB.DB,"SELECT data FROM characters WHERE (?='' OR source_id=?) AND (name LIKE ? ESCAPE '\\' OR json_extract(data,'$.aliases') LIKE ? ESCAPE '\\') ORDER BY name,id LIMIT ? OFFSET ?",sourceID,sourceID,like(query),like(query),n+1,o);return paged(a,n,o,key),e}
func(s *Service)SaveSource(ctx context.Context,w *Write,id string,in TermInput)(Source,error){
 if e:=Authorize(w.Principal,"admin","");e!=nil{return Source{},e};name:=strings.TrimSpace(in.Name);if name==""||len(name)>200{return Source{},Fail(400,"invalid_source","出处名称无效。")};if in.Aliases==nil{in.Aliases=[]string{}};aliases,e:=cleanStrings(in.Aliases,100,200);if e!=nil{return Source{},e};now:=time.Now().UTC();v:=Source{ID:w.ID("source"),Name:name,Aliases:aliases,Revision:1,CreatedAt:now,UpdatedAt:now}
 if id!=""{old,e:=store.Get[Source](ctx,s.DB.DB,"SELECT data FROM sources WHERE id=?",id);if e!=nil{return v,Normalize(e)};if old.Revision!=in.ExpectedRevision{return v,Conflict(old.Revision)};v.ID=id;v.CreatedAt=old.CreatedAt;v.Revision=old.Revision+1};raw,e:=store.Encode(v);if e!=nil{return v,e};q,e:=w.Q();if e!=nil{return v,e};_,e=q.ExecContext(ctx,"INSERT INTO sources(id,name,data) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,data=excluded.data",v.ID,v.Name,raw);return v,e
}
func(s *Service)SaveCharacter(ctx context.Context,w *Write,id string,in TermInput)(Character,error){
 if e:=Authorize(w.Principal,"admin","");e!=nil{return Character{},e};if _,e:=store.Get[Source](ctx,s.DB.DB,"SELECT data FROM sources WHERE id=?",in.SourceID);e!=nil{return Character{},Normalize(e)};name:=strings.TrimSpace(in.Name);if name==""||len(name)>200{return Character{},Fail(400,"invalid_character","角色名称无效。")};if in.Aliases==nil{in.Aliases=[]string{}};aliases,e:=cleanStrings(in.Aliases,100,200);if e!=nil{return Character{},e};now:=time.Now().UTC();v:=Character{Source:Source{ID:w.ID("character"),Name:name,Aliases:aliases,Revision:1,CreatedAt:now,UpdatedAt:now},SourceID:in.SourceID}
 if id!=""{old,e:=store.Get[Character](ctx,s.DB.DB,"SELECT data FROM characters WHERE id=?",id);if e!=nil{return v,Normalize(e)};if old.Revision!=in.ExpectedRevision{return v,Conflict(old.Revision)};v.ID=id;v.CreatedAt=old.CreatedAt;v.Revision=old.Revision+1};raw,e:=store.Encode(v);if e!=nil{return v,e};q,e:=w.Q();if e!=nil{return v,e};_,e=q.ExecContext(ctx,"INSERT INTO characters(id,source_id,name,data) VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET source_id=excluded.source_id,name=excluded.name,data=excluded.data",v.ID,v.SourceID,v.Name,raw);return v,e
}
func(s *Service)resolveTaxonomy(ctx context.Context,a *model.Asset,sourceIDs,characterIDs []string)error{
 if sourceIDs==nil{sourceIDs=[]string{}};if characterIDs==nil{characterIDs=[]string{}};sources,e:=cleanStrings(sourceIDs,20,100);if e!=nil{return e};characters,e:=cleanStrings(characterIDs,50,100);if e!=nil{return e};names:=[]string{};characterNames:=[]string{};seen:=map[string]bool{}
 for _,id:=range characters{c,e:=store.Get[Character](ctx,s.DB.DB,"SELECT data FROM characters WHERE id=?",id);if e!=nil{return Normalize(e)};characterNames=append(characterNames,c.Name);sources=appendUnique(sources,c.SourceID)}
 for _,id:=range sources{if seen[id]{continue};seen[id]=true;v,e:=store.Get[Source](ctx,s.DB.DB,"SELECT data FROM sources WHERE id=?",id);if e!=nil{return Normalize(e)};names=append(names,v.Name)}
 a.SourceIDs=sources;a.CharacterIDs=characters;a.Source=strings.Join(names,"+");a.Characters=characterNames;return nil
}
