package service

import (
 "context"
 "database/sql"
 "time"

 "qimeng.local/next/server/internal/model"
 "qimeng.local/next/server/internal/store"
)

func(s *Service)FilteredHistory(ctx context.Context,filter AssetFilter)(model.Page[model.Event],error){
 page:=model.Page[model.Event]{Items:[]model.Event{}};f,e:=normalizeAssetFilter(filter);if e!=nil{return page,e};n,e:=limited(f.Limit,50);if e!=nil{return page,e};predicate,e:=assetPredicate(f);if e!=nil{return page,e};order:=browseSort{Expression:"ev.created_at",Name:"history_desc",Descending:true};cursor,e:=readSeek(f.Cursor,predicate.FilterKey,order.Name);if e!=nil{return page,e};seek,values,e:=seekClause(cursor,order,"ev.event_id");if e!=nil{return page,e}
 cte:=browseCTE+`, latest AS (SELECT event_id,asset_id,created_at,data,row_number() OVER(PARTITION BY asset_id ORDER BY created_at DESC,event_id DESC) position FROM events WHERE json_extract(data,'$.kind')='view')`;from:=browseFrom+"JOIN latest ev ON ev.asset_id=a.id AND ev.position=1 "
 tx,e:=s.DB.DB.BeginTx(ctx,&sql.TxOptions{ReadOnly:true});if e!=nil{return page,e};defer tx.Rollback();var count int64
 if f.DateCounts&&f.Cursor==""{
  args:=append([]any{f.TZOffsetMinutes*60},predicate.Args...)
  buckets,e:=tx.QueryContext(ctx,cte+" SELECT date(unixepoch(substr(ev.created_at,1,19)||'Z')+?,'unixepoch') day,count(*)"+from+"WHERE "+predicate.Where+" GROUP BY day ORDER BY day DESC",args...);if e!=nil{return page,e}
  dates:=[]model.DateCountBucket{};for buckets.Next(){var item model.DateCountBucket;if e=buckets.Scan(&item.Date,&item.FileCount);e!=nil{buckets.Close();return page,e};dates=append(dates,item);count+=item.FileCount}
  e=buckets.Err();buckets.Close();if e!=nil{return page,e};page.DateCounts=&dates
 }else if e=tx.QueryRowContext(ctx,cte+" SELECT COUNT(*)"+from+"WHERE "+predicate.Where,predicate.Args...).Scan(&count);e!=nil{return page,e}
 page.TotalMatched=&count
 args:=append([]any{},predicate.Args...);args=append(args,values...);args=append(args,n+1,time.Now().Format("2006-01-02"));query:=cte+", page AS (SELECT ev.data event_data,ev.event_id,ev.created_at,a.id"+from+"WHERE "+predicate.Where+seek+" ORDER BY ev.created_at DESC,ev.event_id DESC LIMIT ?) SELECT json_set(p.event_data,'$.asset',json("+assetJSON+")) FROM page p JOIN assets a ON a.id=p.id JOIN authors au ON au.id=a.author_id ORDER BY p.created_at DESC,p.event_id DESC"
 valuesPage,e:=store.List[model.Event](ctx,tx,query,args...);if e!=nil{return page,e};page.Items=valuesPage;if len(valuesPage)>n{page.Items=valuesPage[:n];last:=page.Items[n-1];page.NextCursor=makeSeek(predicate.FilterKey,order.Name,dateKey(last.CreatedAt),last.EventID)};return page,nil
}
