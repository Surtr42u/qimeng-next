package service

import (
 "context"
 "database/sql"
 "slices"

 "qimeng.local/next/server/internal/authorlist"
 "qimeng.local/next/server/internal/model"
 "qimeng.local/next/server/internal/store"
)

func verifiedAuthorLinks(ctx context.Context,q store.Querier,id string)([]string,error){
 accounts,e:=profileBlock(ctx,q,id,"accounts");if e!=nil{return nil,e}
 links:=[]string{}
 if profileString(accounts.Values.IdentityStatus)!="verified"{return links,nil}
 if accounts.Values.Accounts!=nil{for _,account:=range *accounts.Values.Accounts{if account.URL!=""{links=appendUnique(links,account.URL)}}}
 x,e:=profileBlock(ctx,q,id,"x");if e!=nil{return nil,e}
 if url:=profileString(x.Values.ProfileURL);url!=""{links=appendUnique(links,url)}
 for _,block:=range []AuthorProfileBlock{accounts,x}{for _,evidence:=range block.Evidence{if evidence.Status=="confirmed"&&evidence.URL!=""{links=appendUnique(links,evidence.URL)}}}
 return links,nil
}

func renderAuthorList(ctx context.Context,q store.Querier,list authorListRecord)(string,error){
 out:=make([]authorlist.Block,0,len(list.Blocks));enriched:=map[string]bool{}
 for _,entry:=range list.Blocks{
  b:=entry.Declaration
  if !enriched[entry.AuthorID]{
   author,e:=store.Get[model.Author](ctx,q,"SELECT data FROM authors WHERE id=?",entry.AuthorID);if e!=nil{return "",e}
   var explicit bool;if e=q.QueryRowContext(ctx,"SELECT EXISTS(SELECT 1 FROM author_source_overrides WHERE author_id=?)",author.ID).Scan(&explicit);e!=nil{return "",e}
   if explicit{b.Sources=slices.Clone(author.AcquisitionSources)}
   links,e:=verifiedAuthorLinks(ctx,q,author.ID);if e!=nil{return "",e};b.Sources=appendUnique(b.Sources,links...);enriched[author.ID]=true
  }
  out=append(out,b)
 }
 return boundedAuthorText(out)
}

func boundedAuthorText(blocks []authorlist.Block)(string,error){
 total:=0
 for _,b:=range blocks{total+=len(b.Header)+24;for _,group:=range [][]string{b.Names,b.Works,b.Sources}{for _,value:=range group{total+=len(value)+2}};if total>maxAuthorListTextBytes{return "",Fail(422,"author_list_export_budget","导出清单超过当前资源预算。")}}
 return authorlist.Render(blocks),nil
}

func renderMasterList(ctx context.Context,q store.Querier)(string,error){
 var bytes int64
 if e:=q.QueryRowContext(ctx,`SELECT COALESCE((SELECT SUM(length(CAST(data AS BLOB))) FROM author_lists),0) +
   COALESCE((SELECT SUM(length(CAST(a.file_name AS BLOB))+1) FROM asset_authors x JOIN assets a ON a.id=x.asset_id),0) +
   COALESCE((SELECT SUM(length(CAST(name AS BLOB))+1) FROM authors),0)`).Scan(&bytes);e!=nil{return "",e}
 if bytes>maxAuthorListTextBytes{return "",Fail(422,"author_list_export_budget","合成作者总表超过当前资源预算。")}
 lists,e:=store.List[authorListRecord](ctx,q,"SELECT data FROM author_lists ORDER BY position");if e!=nil{return "",e}
 blocks:=[]authorlist.Block{};positions:=map[string]int{}
 works:=map[string]map[string]bool{}
 addWork:=func(id string,at int,value string){if works[id]==nil{works[id]=map[string]bool{}};if !works[id][value]{works[id][value]=true;blocks[at].Works=append(blocks[at].Works,value)}}
 for _,list:=range lists{for _,entry:=range list.Blocks{
  at,ok:=positions[entry.AuthorID];if !ok{at=len(blocks);positions[entry.AuthorID]=at;blocks=append(blocks,authorlist.Block{Names:entry.Declaration.Names})}
  b:=&blocks[at];b.Sources=appendUnique(b.Sources,entry.Declaration.Sources...);for _,work:=range entry.Declaration.Works{addWork(entry.AuthorID,at,work)}
 }}
 authors,e:=store.List[model.Author](ctx,q,"SELECT data FROM authors WHERE json_extract(data,'$.kind')='regular' OR id='unknown' ORDER BY name,id");if e!=nil{return "",e}
 for _,author:=range authors{
  at,ok:=positions[author.ID];if !ok{if author.Placeholder{continue};declaration,e:=textDeclaration(author);if e!=nil{return "",e};at=len(blocks);positions[author.ID]=at;blocks=append(blocks,declaration)}
  b:=&blocks[at];b.Sources=slices.Clone(author.AcquisitionSources)
  links,e:=verifiedAuthorLinks(ctx,q,author.ID);if e!=nil{return "",e};b.Sources=appendUnique(b.Sources,links...)
 }
 rows,e:=q.QueryContext(ctx,"SELECT x.author_id,a.file_name FROM asset_authors x JOIN assets a ON a.id=x.asset_id WHERE a.trashed=0 ORDER BY x.author_id,a.file_name,a.id");if e!=nil{return "",e};defer rows.Close()
 for rows.Next(){var id,work string;if e=rows.Scan(&id,&work);e!=nil{return "",e};if at,ok:=positions[id];ok{addWork(id,at,work)}};if e=rows.Err();e!=nil{return "",e}
 return boundedAuthorText(blocks)
}

func(s *Service)ExportAuthorList(ctx context.Context,p model.Principal,id string)(string,error){
 if e:=Authorize(p,"admin","");e!=nil{return "",e}
 tx,e:=s.DB.DB.BeginTx(ctx,&sql.TxOptions{ReadOnly:true});if e!=nil{return "",e};defer tx.Rollback()
 var content string
 if id==""{content,e=renderMasterList(ctx,tx)}else{var list authorListRecord;list,e=authorListRecordAt(ctx,tx,id);if e==nil{content,e=renderAuthorList(ctx,tx,list)}}
 if e!=nil{return "",Normalize(e)};return content,tx.Commit()
}
