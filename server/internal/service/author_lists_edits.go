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
 for{rows,e:=s.DB.DB.QueryContext(ctx,"SELECT asset_id,author_id FROM asset_author_overrides WHERE removed=0 AND list_id='' ORDER BY asset_id,author_id LIMIT 128");if e!=nil{return e};type pair struct{asset,author string};pending:=[]pair{};for rows.Next(){var value pair;if e=rows.Scan(&value.asset,&value.author);e!=nil{rows.Close();return e};pending=append(pending,value)};e=rows.Err();rows.Close();if e!=nil{return e};if len(pending)==0{return nil};tx,e:=s.DB.DB.BeginTx(ctx,nil);if e!=nil{return e};for _,value:=range pending{asset,e:=store.Get[model.Asset](ctx,tx,"SELECT data FROM assets WHERE id=?",value.asset);if e!=nil{tx.Rollback();return e};author,e:=store.Get[model.Author](ctx,tx,"SELECT data FROM authors WHERE id=?",value.author);if e!=nil{tx.Rollback();return e};if e=s.projectExplicitAuthor(ctx,tx,asset,author);e!=nil{tx.Rollback();return e}};if e=tx.Commit();e!=nil{return e}}
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
 if oldName==a.FileName{return nil};for _,authorID:=range a.AuthorIDs{if authorID=="unknown"{continue};author,e:=store.Get[model.Author](ctx,q,"SELECT data FROM authors WHERE id=?",authorID);if e!=nil{return e};if author.Kind!="regular"{continue};lists,e:=store.List[authorListRecord](ctx,q,"SELECT DISTINCT l.data FROM author_lists l JOIN author_list_members m ON m.list_id=l.id WHERE m.author_id=? ORDER BY l.position",authorID);if e!=nil{return e};for _,list:=range lists{changed:=false;for index:=range list.Blocks{if list.Blocks[index].AuthorID!=authorID{continue};works:=[]string{};for _,work:=range list.Blocks[index].Declaration.Works{if work==oldName{work=a.FileName;changed=true};works=appendUnique(works,work)};list.Blocks[index].Declaration.Works=works};if changed{list.Revision++;list.UpdatedAt=time.Now().UTC();if e=saveAuthorList(ctx,q,&list);e!=nil{return e}}}}
 return nil
}
