package service

import (
 "context"
 "database/sql"
 "encoding/json"
 "strings"

 "qimeng.local/next/server/internal/model"
)

const authorCountsCTE=`WITH author_counts AS (SELECT aa.author_id,COUNT(DISTINCT a.id) files,COALESCE(sum(st.views),0) views FROM asset_authors aa JOIN assets a ON a.id=aa.asset_id LEFT JOIN (SELECT asset_id,sum(views) views FROM asset_daily_stats GROUP BY asset_id) st ON st.asset_id=a.id WHERE a.trashed=0 GROUP BY aa.author_id)`
const authorFrom=` FROM authors au LEFT JOIN author_counts ac ON ac.author_id=au.id `
const authorJSON=`json_set(au.data,'$.fileCount',COALESCE(ac.files,0),'$.viewCount',COALESCE(ac.views,0),'$.acquisitionSources',json(COALESCE(json_extract(au.data,'$.acquisitionSources'),'[]')))`

func(s *Service)Authors(ctx context.Context,query,cursor string,limit int,following bool,options ...string)(model.Page[model.Author],error){
 result:=model.Page[model.Author]{Items:[]model.Author{}};kind,sorting:="","default";if len(options)>0{kind=options[0]};if len(options)>1&&options[1]!=""{sorting=options[1]};if kind!=""&&kind!="regular"&&kind!="cos"{return result,Fail(400,"invalid_author_kind","作者分区无效。")};if len(query)>800{return result,Fail(400,"query_too_long","作者搜索词过长。")};n,e:=limited(limit,20);if e!=nil{return result,e}
 order:=browseSort{Name:sorting,Expression:"au.name"};switch sorting{case "default":case "views":order.Expression="COALESCE(ac.views,0)";order.Numeric=true;order.Descending=true;case "files":order.Expression="COALESCE(ac.files,0)";order.Numeric=true;order.Descending=true;case "addedAsc","addedDesc":order.Expression="qimeng_time_key(json_extract(au.data,'$.createdAt'))";order.Nullable=true;order.Descending=sorting=="addedDesc";default:return result,Fail(400,"invalid_sort","作者排序无效。")}
 keyRaw,_:=json.Marshal(struct{Q,Kind,Sort string;Following bool}{strings.Join(strings.Fields(query)," "),kind,sorting,following});key:=string(keyRaw);seek,e:=readSeek(cursor,key,sorting);if e!=nil{return result,e};where:=[]string{"(?='' OR json_extract(au.data,'$.kind')=? OR au.id='unknown')","(?=0 OR au.following=1)"};args:=[]any{kind,kind,following}
 for _,token:=range strings.Fields(query){where=append(where,"qimeng_search_token(au.name||char(10)||COALESCE(json_extract(au.data,'$.aliases'),''),?)=1");args=append(args,token)}
 clause,values,e:=seekClause(seek,order,"au.id");if e!=nil{return result,e};tx,e:=s.DB.DB.BeginTx(ctx,&sql.TxOptions{ReadOnly:true});if e!=nil{return result,e};defer tx.Rollback();var count int64;if e=tx.QueryRowContext(ctx,authorCountsCTE+" SELECT count(*)"+authorFrom+"WHERE "+strings.Join(where," AND "),args...).Scan(&count);e!=nil{return result,e};result.TotalMatched=&count
 args=append(args,values...);args=append(args,n+1);rows,e:=tx.QueryContext(ctx,authorCountsCTE+" SELECT "+authorJSON+",CAST("+order.Expression+" AS TEXT)"+authorFrom+"WHERE "+strings.Join(where," AND ")+clause+" ORDER BY "+sortSQL(order,order.Expression,"au.id")+" LIMIT ?",args...);if e!=nil{return result,e};defer rows.Close();keys:=[]string{}
 for rows.Next(){var raw string;var value sql.NullString;var author model.Author;if e=rows.Scan(&raw,&value);e!=nil{return result,e};if e=json.Unmarshal([]byte(raw),&author);e!=nil{return result,e};result.Items=append(result.Items,author);key:="~null";if value.Valid{key=value.String};keys=append(keys,key)};if e=rows.Err();e!=nil{return result,e};if len(result.Items)>n{result.Items=result.Items[:n];last:=result.Items[n-1];result.NextCursor=makeSeek(key,sorting,keys[n-1],last.ID)};return result,nil
}
