package service

import (
 "context"
 "database/sql"
 "errors"
 "slices"
 "time"

 "qimeng.local/next/server/internal/authorlist"
 "qimeng.local/next/server/internal/model"
 "qimeng.local/next/server/internal/store"
)

func textDeclaration(a model.Author)(authorlist.Block,error){names,e:=authorlist.Names(a.Name);if e!=nil{return authorlist.Block{},Fail(409,"author_text_identity_invalid","作者显示名不能安全导出为清单，请先修正该作者名称。")};return authorlist.Block{Names:names,Sources:[]string{},Works:[]string{}},nil}
func(s *Service)authorTextHome(ctx context.Context,q store.Querier,a model.Author)(authorListRecord,error){
 list,e:=store.Get[authorListRecord](ctx,q,"SELECT l.data FROM author_lists l JOIN author_list_members m ON m.list_id=l.id WHERE m.author_id=? ORDER BY l.position,m.ordinal LIMIT 1",a.ID);if e==nil{return list,nil};if !errors.Is(e,sql.ErrNoRows){return list,e}
 list,e=store.Get[authorListRecord](ctx,q,"SELECT data FROM author_lists ORDER BY position DESC LIMIT 1");if errors.Is(e,sql.ErrNoRows){list,e=nextAuthorList(ctx,q,digest([]byte("automatic-author-list:"+s.InstanceID))[:32],automaticAuthorListName)};if e!=nil{return list,e};declaration,e:=textDeclaration(a);if e!=nil{return list,e};list.Blocks=append(list.Blocks,authorListBlock{AuthorID:a.ID,Declaration:declaration});list.Revision++;list.UpdatedAt=time.Now().UTC();return list,saveAuthorList(ctx,q,&list)
}
func addListWork(list *authorListRecord,authorID,work string)bool{for index:=range list.Blocks{if list.Blocks[index].AuthorID!=authorID{continue};for _,existing:=range list.Blocks[index].Declaration.Works{if existing==work{return false}};list.Blocks[index].Declaration.Works=append(list.Blocks[index].Declaration.Works,work);return true};return false}
func(s *Service)projectExplicitAuthor(ctx context.Context,q store.Querier,a model.Asset,author model.Author)error{
 list,e:=s.authorTextHome(ctx,q,author);if e!=nil{return e};if addListWork(&list,author.ID,a.FileName){list.Revision++;list.UpdatedAt=time.Now().UTC();if e=saveAuthorList(ctx,q,&list);e!=nil{return e}}
 _,e=q.ExecContext(ctx,"INSERT INTO asset_author_overrides(asset_id,author_id,removed,list_id) VALUES(?,?,0,?) ON CONFLICT(asset_id,author_id) DO UPDATE SET removed=0,list_id=excluded.list_id",a.ID,author.ID,list.ID);return e
}
func(s *Service)initializeAuthorTextLinks(ctx context.Context)error{
 // Existing explicit links are projected once after schema expansion. New
 // associations use the same transaction helper at their actual write boundary.
 type pair struct{asset,author string};last:=pair{}
 for{
  rows,e:=s.DB.DB.QueryContext(ctx,"SELECT asset_id,author_id FROM asset_author_overrides WHERE removed=0 AND list_id='' AND (asset_id,author_id)>(?,?) ORDER BY asset_id,author_id LIMIT 128",last.asset,last.author);if e!=nil{return e}
  pending:=[]pair{};for rows.Next(){var value pair;if e=rows.Scan(&value.asset,&value.author);e!=nil{rows.Close();return e};pending=append(pending,value)};e=rows.Err();rows.Close();if e!=nil{return e}
  if len(pending)==0{return nil}
  tx,e:=s.DB.DB.BeginTx(ctx,nil);if e!=nil{return e}
  for _,v:=range pending{a,e:=store.Get[model.Asset](ctx,tx,"SELECT data FROM assets WHERE id=?",v.asset);if e==nil{e=s.projectLegacyAuthor(ctx,tx,a,v.author)};if e!=nil{tx.Rollback();return e};last=v}
  if e=tx.Commit();e!=nil{return e}
 }
}
func(s *Service)projectLegacyAuthor(ctx context.Context,q store.Querier,a model.Asset,id string)error{
 if id=="unknown"{return nil}
 author,e:=store.Get[model.Author](ctx,q,"SELECT data FROM authors WHERE id=?",id);if e!=nil{return e};if author.Kind!="regular"{return nil}
 if _,e=authorlist.Names(author.Name);e!=nil{
  // Historical display names are not always representable in the TXT grammar.
  // Retain the identity and relation in SQLite/qnext, never create a wrong block.
  _,e=q.ExecContext(ctx,"INSERT OR IGNORE INTO asset_author_overrides(asset_id,author_id,removed,list_id) VALUES(?,?,0,'')",a.ID,id);return e
 }
 return s.projectExplicitAuthor(ctx,q,a,author)
}
func(s *Service)recordExplicitAuthorSet(ctx context.Context,q store.Querier,before []string,a model.Asset)error{
 selected:=map[string]bool{};for _,id:=range a.AuthorIDs{selected[id]=true;if id=="unknown"{continue};author,e:=store.Get[model.Author](ctx,q,"SELECT data FROM authors WHERE id=?",id);if e!=nil{return e};if author.Kind=="regular"{if e=s.projectExplicitAuthor(ctx,q,a,author);e!=nil{return e}}}
 for _,id:=range before{if id=="unknown"||selected[id]{continue};author,e:=store.Get[model.Author](ctx,q,"SELECT data FROM authors WHERE id=?",id);if e!=nil{return e};if author.Kind!="regular"{continue};if _,e=q.ExecContext(ctx,"INSERT INTO asset_author_overrides(asset_id,author_id,removed,list_id) VALUES(?,?,1,'') ON CONFLICT(asset_id,author_id) DO UPDATE SET removed=1,list_id=''",a.ID,id);e!=nil{return e};if _,e=q.ExecContext(ctx,"DELETE FROM author_list_matches WHERE asset_id=? AND author_id=?",a.ID,id);e!=nil{return e};if e=removeLiteralAuthorWork(ctx,q,id,a.ID,a.FileName);e!=nil{return e}}
 return nil
}
func removeLiteralAuthorWork(ctx context.Context,q store.Querier,authorID,assetID,name string)error{
 lists,e:=store.List[authorListRecord](ctx,q,"SELECT DISTINCT l.data FROM author_lists l JOIN author_list_members m ON m.list_id=l.id WHERE m.author_id=? ORDER BY l.position",authorID);if e!=nil{return e}
 for _,list:=range lists{var shared int;if e=q.QueryRowContext(ctx,"SELECT count(*) FROM assets a JOIN asset_authors x ON x.asset_id=a.id WHERE x.author_id=? AND a.file_name=? AND a.id!=?",authorID,name,assetID).Scan(&shared);e!=nil{return e};if shared>0{continue};changed:=false;for index:=range list.Blocks{if list.Blocks[index].AuthorID!=authorID{continue};works:=[]string{};for _,work:=range list.Blocks[index].Declaration.Works{if work==name{changed=true;continue};works=append(works,work)};list.Blocks[index].Declaration.Works=works};if changed{list.Revision++;list.UpdatedAt=time.Now().UTC();if e=saveAuthorList(ctx,q,&list);e!=nil{return e}}};return nil
}
func touchAuthorListMetadata(ctx context.Context,q store.Querier,a model.Author,identityChanged,removed bool)error{
 lists,e:=store.List[authorListRecord](ctx,q,"SELECT DISTINCT l.data FROM author_lists l JOIN author_list_members m ON m.list_id=l.id WHERE m.author_id=? ORDER BY l.position",a.ID);if e!=nil{return e};for _,list:=range lists{blocks:=[]authorListBlock{};for _,block:=range list.Blocks{if block.AuthorID==a.ID{if removed{continue};if identityChanged{names,e:=authorlist.Names(a.Name);if e!=nil{return Fail(400,"invalid_author_name","作者名称不能安全写入清单。")};block.Declaration.Names=names;block.Declaration.Header=""}};blocks=append(blocks,block)};list.Blocks=blocks;list.Revision++;list.UpdatedAt=time.Now().UTC();if e=saveAuthorList(ctx,q,&list);e!=nil{return e}};return nil
}
func(s *Service)renameAuthorListWorks(ctx context.Context,q store.Querier,a model.Asset,oldName string)error{
 if oldName==a.FileName{return nil}
 for _,authorID:=range a.AuthorIDs{
  if authorID=="unknown"{continue}
  lists,e:=store.List[authorListRecord](ctx,q,`SELECT DISTINCT l.data FROM author_lists l
   JOIN author_list_members m ON m.list_id=l.id JOIN authors c ON c.id=m.author_id
   WHERE m.author_id=? AND json_extract(c.data,'$.kind')='regular' ORDER BY l.position`,authorID)
  if e!=nil{return e}
  if len(lists)==0{continue}
  var shared bool
  e=q.QueryRowContext(ctx,`SELECT EXISTS(SELECT 1 FROM assets a JOIN asset_authors x ON x.asset_id=a.id
   WHERE x.author_id=? AND a.file_name=? AND a.id!=?) OR EXISTS(
   SELECT 1 FROM author_list_matches WHERE author_id=? AND work=? AND asset_id!=?)`,
   authorID,oldName,a.ID,authorID,oldName,a.ID).Scan(&shared)
  if e!=nil{return e}
  for _,list:=range lists{
   bindings,e:=store.List[string](ctx,q,"SELECT json_quote(work) FROM author_list_matches WHERE list_id=? AND asset_id=? AND author_id=?",list.ID,a.ID,authorID)
   if e!=nil{return e}
   rebind:=map[string]bool{}
   for _,work:=range bindings{
    if work==oldName||!authorlist.Matches(work,a.FileName){rebind[work]=true}
   }
   changed:=false
   for index:=range list.Blocks{
    block:=&list.Blocks[index]
    if block.AuthorID!=authorID{continue}
    previous:=block.Declaration.Works
    seen:=make(map[string]bool,len(previous)+1)
    works:=make([]string,0,len(previous)+1)
    add:=func(name string){if !seen[name]{seen[name]=true;works=append(works,name)}}
    for _,work:=range previous{
     if work!=oldName||shared{add(work)}
     // Preserve page/base rules for siblings, then add the renamed file as
     // a literal so that an export can reconstruct it on a fresh instance.
     if work==oldName||rebind[work]{add(a.FileName)}
    }
    if !slices.Equal(previous,works){changed=true;block.Declaration.Works=works}
   }
   if changed{
    list.Revision++;list.UpdatedAt=time.Now().UTC()
    if e=saveAuthorList(ctx,q,&list);e!=nil{return e}
   }
   if len(rebind)==0{continue}
   if _,e=q.ExecContext(ctx,"INSERT OR IGNORE INTO author_list_matches(list_id,asset_id,author_id,work) VALUES(?,?,?,?)",list.ID,a.ID,authorID,a.FileName);e!=nil{return e}
   for work:=range rebind{
    if _,e=q.ExecContext(ctx,"DELETE FROM author_list_matches WHERE list_id=? AND asset_id=? AND author_id=? AND work=?",list.ID,a.ID,authorID,work);e!=nil{return e}
   }
  }
 }
 return nil
}
