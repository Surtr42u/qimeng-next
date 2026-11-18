package service

import (
 "context"
 "database/sql"
 "errors"
 "time"

 "qimeng.local/next/server/internal/authorlist"
 "qimeng.local/next/server/internal/model"
 "qimeng.local/next/server/internal/store"
)

func authorListRecordAt(ctx context.Context,q store.Querier,id string)(authorListRecord,error){return store.Get[authorListRecord](ctx,q,"SELECT data FROM author_lists WHERE id=?",id)}
func saveAuthorList(ctx context.Context,q store.Querier,list *authorListRecord)error{
 authors:=map[string]bool{};works:=map[string]bool{};for _,block:=range list.Blocks{authors[block.AuthorID]=true;for _,work:=range block.Declaration.Works{works[block.AuthorID+"\x00"+work]=true}};list.AuthorCount=len(authors);list.WorkCount=len(works);raw,e:=store.Encode(list);if e!=nil{return e};if _,e=q.ExecContext(ctx,"INSERT INTO author_lists(id,name,position,revision,data) VALUES(?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,revision=excluded.revision,data=excluded.data",list.ID,list.Name,list.Position,list.Revision,raw);e!=nil{return e}
 if _,e=q.ExecContext(ctx,"DELETE FROM author_list_members WHERE list_id=?",list.ID);e!=nil{return e};ordinal:=0
 for index,block:=range list.Blocks{if _,e=q.ExecContext(ctx,"INSERT INTO author_list_members(list_id,ordinal,author_id) VALUES(?,?,?)",list.ID,index,block.AuthorID);e!=nil{return e};for _,work:=range block.Declaration.Works{rule:=authorlist.CompileWork(work);if rule.Key==""{continue};if _,e=q.ExecContext(ctx,"INSERT INTO author_list_rules(list_id,author_id,work,ordinal,match_key,extension,pages) VALUES(?,?,?,?,?,?,?) ON CONFLICT(list_id,author_id,work) DO UPDATE SET ordinal=excluded.ordinal",list.ID,block.AuthorID,work,ordinal,rule.Key,rule.Extension,rule.Pages);e!=nil{return e};ordinal++}}
 rows,e:=q.QueryContext(ctx,"SELECT author_id,work FROM author_list_rules WHERE list_id=?",list.ID);if e!=nil{return e};type ruleID struct{author,work string};removed:=[]ruleID{};for rows.Next(){var value ruleID;if e=rows.Scan(&value.author,&value.work);e!=nil{rows.Close();return e};if !works[value.author+"\x00"+value.work]{removed=append(removed,value)}};e=rows.Err();rows.Close();if e!=nil{return e};for _,value:=range removed{if _,e=q.ExecContext(ctx,"DELETE FROM author_list_rules WHERE list_id=? AND author_id=? AND work=?",list.ID,value.author,value.work);e!=nil{return e}};return nil
}
func(s *Service)AuthorLists(ctx context.Context,cursor string,limit int)(model.Page[AuthorList],error){
 result:=model.Page[AuthorList]{Items:[]AuthorList{}};n,e:=limited(limit,20);if e!=nil{return result,e};seek,e:=readSeek(cursor,"author-lists","name");if e!=nil{return result,e};items,e:=store.List[AuthorList](ctx,s.DB.DB,"SELECT data FROM author_lists WHERE (?='' OR (name,id)>(?,?)) ORDER BY name,id LIMIT ?",seek.ID,seek.Value,seek.ID,n+1);if e!=nil{return result,e};if len(items)>n{result.NextCursor=makeSeek("author-lists","name",items[n-1].Name,items[n-1].ID);items=items[:n]};result.Items=items;return result,nil
}
func(s *Service)AuthorList(ctx context.Context,id string)(AuthorList,error){list,e:=authorListRecordAt(ctx,s.DB.DB,id);return list.AuthorList,Normalize(e)}
func nextAuthorList(ctx context.Context,q store.Querier,id,name string)(authorListRecord,error){
 now:=time.Now().UTC();list:=authorListRecord{AuthorList:AuthorList{ID:id,Name:name,Revision:1,CreatedAt:now,UpdatedAt:now},Blocks:[]authorListBlock{}};if e:=q.QueryRowContext(ctx,"SELECT COALESCE(MAX(position),0)+1 FROM author_lists").Scan(&list.Position);e!=nil{return list,e};var previous int64;e:=q.QueryRowContext(ctx,"SELECT revision FROM author_list_tombstones WHERE name=?",name).Scan(&previous);if e!=nil&&!errors.Is(e,sql.ErrNoRows){return list,e};if previous>0{list.Revision=previous+1};return list,nil
}
func archiveAuthorListInput(ctx context.Context,q store.Querier,id,sourceID,name,content string,at time.Time)error{hash:=digest([]byte(content));_,e:=q.ExecContext(ctx,"INSERT OR IGNORE INTO author_txt_fragments(id,name,sha256,content,created_at,source_id) VALUES(?,?,?,?,?,?)",id,name,hash,content,dateKey(at),sourceID);return e}
