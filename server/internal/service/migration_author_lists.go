package service

import (
 "context"
 "database/sql"
 "errors"
 "path"
 "strings"
 "time"

 "qimeng.local/next/server/internal/authorlist"
 "qimeng.local/next/server/internal/domain"
 legacy "qimeng.local/next/server/internal/migration"
 "qimeng.local/next/server/internal/store"
)

// Restore missing numbered declarations only when every block can be bound to
// an author in the reviewed mapping. Raw fragments always remain retrievable.
// Publishing the declarations does not edit unrelated assets in this migration.
func restoreV2TextLists(ctx context.Context,q store.Querier,metadata V2Import,fragments []legacy.Fragment,authors []V2MigrationAuthor,plan *V2MigrationPlan)error{
 identities:=map[string]string{}
 for _,a:=range authors{
  if a.State=="conflict"||a.Kind!="regular"{continue};names,e:=authorlist.Names(a.Name);if e!=nil{continue}
  key:=authorlist.IdentityKey(names[0]);old,seen:=identities[key];if seen&&old!=a.AuthorID{identities[key]=""}else{identities[key]=a.AuthorID}
 }
 affected:=map[string]bool{}
 for _,fragment:=range fragments{
  hash:=digest([]byte(fragment.Content));when:=time.Now().UTC();if fragment.ImportedAtMillis>0{when=time.UnixMilli(fragment.ImportedAtMillis).UTC()}
  id:=digest([]byte(metadata.SourceID+"\x00"+fragment.Filename+"\x00"+hash))[:32]
  r,e:=q.ExecContext(ctx,"INSERT OR IGNORE INTO author_txt_fragments(id,name,sha256,content,created_at,source_id) VALUES(?,?,?,?,?,?)",id,fragment.Filename,hash,fragment.Content,dateKey(when),metadata.SourceID);if e!=nil{return e};n,e:=r.RowsAffected();if e!=nil{return e};plan.Counts["txtFragmentsPreserved"]+=n
  if !domain.ValidName(fragment.Filename)||!strings.EqualFold(path.Ext(fragment.Filename),".txt"){migrationIssue(plan,"txt_fragment_archived");continue}
  doc,e:=authorlist.Parse(fragment.Content);if e!=nil||doc.Format!="blocks"{migrationIssue(plan,"txt_fragment_archived");continue}
  var current string;e=q.QueryRowContext(ctx,"SELECT json_extract(data,'$.inputHash') FROM author_lists WHERE name=?",fragment.Filename).Scan(&current)
  if e==nil{if current!=hash{migrationIssue(plan,"existing_txt_list_kept")};continue};if !errors.Is(e,sql.ErrNoRows){return e}
  var deleted bool;if e=q.QueryRowContext(ctx,"SELECT EXISTS(SELECT 1 FROM author_list_tombstones WHERE name=?)",fragment.Filename).Scan(&deleted);e!=nil{return e};if deleted{migrationIssue(plan,"deleted_txt_list_kept");continue}
  blocks:=make([]authorListBlock,0,len(doc.Blocks))
  for _,block:=range doc.Blocks{
   authorID:=identities[authorlist.IdentityKey(block.Names[0])];if authorID==""{break}
   if _,e=authorSourceTerms(block.Sources);e!=nil{break}
   blocks=append(blocks,authorListBlock{AuthorID:authorID,Declaration:block})
  }
  if len(blocks)!=len(doc.Blocks){migrationIssue(plan,"txt_fragment_owner_or_source_unresolved");continue}
  list,e:=nextAuthorList(ctx,q,digest([]byte("v2-list:"+id))[:32],fragment.Filename);if e!=nil{return e}
  list.Blocks=blocks;list.CreatedAt=when;list.InputHash=hash;if e=saveAuthorList(ctx,q,&list);e!=nil{return e}
  for _,b:=range blocks{affected[b.AuthorID]=true};plan.Counts["txtListsActivated"]++
 }
 ids:=[]string{};for id:=range affected{ids=append(ids,id)};_,e:=reconcileImportedSources(ctx,q,ids);return e
}
