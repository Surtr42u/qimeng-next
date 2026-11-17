package service

import (
 "context"
 "database/sql"
 "strconv"
 "strings"

 "qimeng.local/next/server/internal/model"
 "qimeng.local/next/server/internal/store"
)

type AssetFacet struct{Key string `json:"key"`;Kind string `json:"kind"`;ID string `json:"id"`;Name string `json:"name"`;Count int64 `json:"count"`}
const fixedFacetOrder="CASE c.id WHEN 'all' THEN 0 WHEN 'regular' THEN 1 WHEN 'cos' THEN 2 WHEN 'image' THEN 1 WHEN 'animated_image' THEN 2 WHEN 'video' THEN 3 END"
func fixedFacetPosition(id string)int{switch id{case "all":return 0;case "regular","image":return 1;case "cos","animated_image":return 2;default:return 3}}
func facetCandidates(row string)(string,error){
 switch row{
 case "partitions":return `SELECT 'all' key,'partition' kind,'all' id,'全部' name,count(*) n FROM matched UNION ALL SELECT 'regular','partition','regular','常规',count(*) FROM matched WHERE kind='regular' UNION ALL SELECT 'cos','partition','cos','COS',count(*) FROM matched WHERE kind='cos'`,nil
 case "types":return `SELECT 'all' key,'type' kind,'all' id,'全部' name,count(*) n FROM matched UNION ALL SELECT 'image','type','image','图片',count(*) FROM matched WHERE media_type='image' UNION ALL SELECT 'animated_image','type','animated_image','动图',count(*) FROM matched WHERE media_type='animated_image' UNION ALL SELECT 'video','type','video','视频',count(*) FROM matched WHERE media_type='video'`,nil
 case "works":return `SELECT 'source:'||s.id key,'source' kind,s.id,s.name,count(DISTINCT m.id) n FROM matched m JOIN asset_sources x ON x.asset_id=m.id JOIN sources s ON s.id=x.source_id WHERE m.kind='regular' GROUP BY s.id UNION ALL SELECT 'source:__other__','source','__other__','其他',count(*) FROM matched m WHERE m.kind='regular' AND NOT EXISTS(SELECT 1 FROM asset_sources x WHERE x.asset_id=m.id) HAVING count(*)>0 UNION ALL SELECT 'author:'||au.id,'author',au.id,au.name,count(DISTINCT m.id) FROM matched m JOIN asset_authors x ON x.asset_id=m.id JOIN authors au ON au.id=x.author_id WHERE m.kind='cos' AND json_extract(au.data,'$.kind')='cos' GROUP BY au.id`,nil
 case "roles":return `SELECT 'character:'||c.id key,'character' kind,c.id,c.name,count(DISTINCT m.id) n FROM matched m JOIN asset_characters x ON x.asset_id=m.id JOIN characters c ON c.id=x.character_id WHERE m.kind='regular' GROUP BY c.id UNION ALL SELECT 'work:'||w.id,'work',w.id,w.name,count(*) FROM matched m JOIN cos_works w ON w.id=m.cos_work_id WHERE m.kind='cos' GROUP BY w.id`,nil
 default:return "",Fail(400,"invalid_facet_row","相册维度必须是分区、作者、角色或类型。")
 }
}
func(s *Service)AssetFacets(ctx context.Context,filter AssetFilter,row,query string)(model.Page[AssetFacet],error){
 page:=model.Page[AssetFacet]{Items:[]AssetFacet{}};f,e:=normalizeAssetFilter(filter);if e!=nil{return page,e};n,e:=limited(f.Limit,50);if e!=nil{return page,e};if len(query)>800{return page,Fail(400,"query_too_long","候选搜索词过长。")};predicate,e:=assetPredicate(f);if e!=nil{return page,e};candidates,e:=facetCandidates(row);if e!=nil{return page,e};key:=predicate.FilterKey+"\x00"+row+"\x00"+query;fixed:=row=="partitions"||row=="types";sorting:="facet_count";ordering:="c.n DESC,c.name,c.key";if fixed{sorting="fixed_row";ordering=fixedFacetOrder};cursor,e:=readSeek(f.Cursor,key,sorting);if e!=nil{return page,e}
 cte:=browseCTE+", matched AS (SELECT a.id,a.library_id,a.media_type,a.cos_work_id,"+assetPartition+" kind"+browseFrom+"WHERE "+predicate.Where+"), candidates AS ("+candidates+")"
 where:="instr(lower(c.name),lower(?))>0";args:=append(append([]any{},predicate.Args...),query)
 if cursor.ID!=""{if fixed{position,e:=strconv.Atoi(cursor.Value);if e!=nil||position<0||position>3{return page,Fail(400,"invalid_cursor","固定维度游标无效。")};where+=" AND "+fixedFacetOrder+">?";args=append(args,position)}else{parts:=strings.SplitN(cursor.Value,"\x00",2);if len(parts)!=2{return page,Fail(400,"invalid_cursor","候选游标无效。")};count,e:=strconv.ParseInt(parts[0],10,64);if e!=nil||count<0{return page,Fail(400,"invalid_cursor","候选游标计数无效。")};where+=" AND (c.n<? OR (c.n=? AND (c.name>? OR (c.name=? AND c.key>?))))";args=append(args,count,count,parts[1],parts[1],cursor.ID)}};args=append(args,n+1)
 tx,e:=s.DB.DB.BeginTx(ctx,&sql.TxOptions{ReadOnly:true});if e!=nil{return page,e};defer tx.Rollback();var count int64;if e=tx.QueryRowContext(ctx,browseCTE+" SELECT count(*)"+browseFrom+"WHERE "+predicate.Where,predicate.Args...).Scan(&count);e!=nil{return page,e};page.TotalMatched=&count
 var options int64;if e=tx.QueryRowContext(ctx,cte+" SELECT count(*) FROM candidates c WHERE instr(lower(c.name),lower(?))>0",append(append([]any{},predicate.Args...),query)...).Scan(&options);e!=nil{return page,e};page.TotalOptions=&options
 values,e:=store.List[AssetFacet](ctx,tx,cte+" SELECT json_object('key',c.key,'kind',c.kind,'id',c.id,'name',c.name,'count',c.n) FROM candidates c WHERE "+where+" ORDER BY "+ordering+" LIMIT ?",args...);if e!=nil{return page,e};page.Items=values;if len(values)>n{page.Items=values[:n];last:=page.Items[n-1];value:=strconv.FormatInt(last.Count,10)+"\x00"+last.Name;if fixed{value=strconv.Itoa(fixedFacetPosition(last.ID))};page.NextCursor=makeSeek(key,sorting,value,last.Key)};return page,nil
}
type SearchSuggestion struct{ID string `json:"id"`;Type string `json:"type"`;Name string `json:"name"`}
func(s *Service)SearchSuggestions(ctx context.Context,query string,recommend bool,limit int)(any,error){
 if limit==0{limit=10};if limit<1||limit>50||len(query)>800{return nil,Fail(400,"invalid_query","建议数量或搜索词无效。")};query=strings.TrimSpace(query);out:=struct{Items []SearchSuggestion `json:"items"`}{Items:[]SearchSuggestion{}};if query==""&&!recommend{return out,nil}
 works,_:=facetCandidates("works");roles,_:=facetCandidates("roles");cte:=`WITH matched AS (SELECT a.id,a.library_id,a.media_type,a.cos_work_id,`+assetPartition+` kind FROM assets a JOIN libraries l ON l.id=a.library_id WHERE a.trashed=0 AND COALESCE(json_extract(l.data,'$.enabled'),1)=1), facet AS (`+works+` UNION ALL `+roles+`), names AS (SELECT id,CASE kind WHEN 'author' THEN 'cosAuthor' WHEN 'work' THEN 'cosWork' ELSE kind END type,name FROM facet WHERE id!='__other__' UNION ALL SELECT au.id,'author',au.name FROM matched m JOIN asset_authors x ON x.asset_id=m.id JOIN authors au ON au.id=x.author_id WHERE json_extract(au.data,'$.kind')='regular'), unique_names AS (SELECT CASE WHEN count(DISTINCT id)=1 THEN min(id) ELSE '' END id,type,name FROM names GROUP BY type,name)`
 args:=[]any{};sqlQuery:="";if query==""{sqlQuery=cte+`, shuffled AS (SELECT id,type,name,row_number() OVER(PARTITION BY type ORDER BY random()) position FROM unique_names) SELECT json_object('id',id,'type',type,'name',name) FROM shuffled ORDER BY position,type LIMIT ?`;args=append(args,limit)}else{sqlQuery=cte+` SELECT json_object('id',id,'type',type,'name',name) FROM unique_names WHERE instr(lower(name),lower(?))>0 ORDER BY length(name),name,type LIMIT ?`;args=append(args,query,limit)}
 values,e:=store.List[SearchSuggestion](ctx,s.DB.DB,sqlQuery,args...);if e!=nil{return nil,e};out.Items=values;return out,nil
}
