package service

import (
 "context"
 "encoding/base64"
 "encoding/json"

 "qimeng.local/next/server/internal/model"
)

type ActivityRankingItem struct {
 ID string `json:"id"`
 Name string `json:"name"`
 ActivityTotals
 Assets int64 `json:"assets"`
 Value int64 `json:"value"`
 Asset *model.Asset `json:"asset,omitempty"`
}
type ActivityRankingPage struct { Items []ActivityRankingItem `json:"items"`; NextCursor string `json:"nextCursor,omitempty"`; Range string `json:"range"`; Metric string `json:"metric"` }
type activityRankingCursor struct { Value int64 `json:"value"`; ID string `json:"id"`; Filter string `json:"filter"` }

// Aggregate once per asset before expanding many-to-many authors/tags. Disabled
// libraries remain in historical reports; trashed or deleted assets do not.
func(s *Service)ActivityRanking(ctx context.Context,kind,window,metric,cursor string,count int)(ActivityRankingPage,error){
 result:=ActivityRankingPage{Items:[]ActivityRankingItem{},Range:window,Metric:metric}
 if metric==""{metric="views";result.Metric=metric}
 if metric!="views"&&metric!="seconds"||kind!="asset"&&metric!="views"{return result,Fail(400,"invalid_metric","该榜单不支持此统计指标。")}
 start,e:=rangeStart(window);if e!=nil{return result,e};day:=dayString(start)
 n,e:=limited(count,20);if e!=nil{return result,e}
 binding,_:=json.Marshal([]string{kind,window,metric,day});filter:=digest(binding)
 var after activityRankingCursor
 if cursor!=""{raw,err:=base64.RawURLEncoding.DecodeString(cursor);if err!=nil||len(raw)>2048||json.Unmarshal(raw,&after)!=nil||after.Filter!=filter||after.ID==""||after.Value<0{return result,Fail(400,"invalid_cursor","榜单游标与查询条件或日期窗口不符。")}}
 projection:=""
 switch kind {
 case "author":projection=`SELECT au.id,au.name,sum(t.views) AS views,sum(t.plays) AS plays,sum(t.seconds) AS seconds,count(*) AS assets,'' AS data FROM totals t JOIN asset_authors aa ON aa.asset_id=t.asset_id JOIN authors au ON au.id=aa.author_id GROUP BY au.id`
 case "tag":projection=`SELECT at.tag AS id,at.tag AS name,sum(t.views) AS views,sum(t.plays) AS plays,sum(t.seconds) AS seconds,count(*) AS assets,'' AS data FROM totals t JOIN asset_tags at ON at.asset_id=t.asset_id GROUP BY at.tag`
 case "asset":projection=`SELECT a.id,a.file_name AS name,t.views,t.plays,t.seconds,1 AS assets,a.data FROM totals t JOIN assets a ON a.id=t.asset_id`
 default:return result,Fail(400,"invalid_ranking","榜单类别无效。")
 }
 // metric and projection are selected from internal literals above, never raw SQL input.
 query:=`WITH totals AS(SELECT d.asset_id,sum(d.views) AS views,sum(d.plays) AS plays,sum(d.seconds) AS seconds FROM asset_daily_stats d JOIN assets a ON a.id=d.asset_id AND a.trashed=0 WHERE d.day>=? GROUP BY d.asset_id), ranked AS (`+projection+`)
 SELECT id,name,views,plays,seconds,assets,data,`+metric+` FROM ranked WHERE `+metric+`>0 AND (?='' OR `+metric+`<? OR (`+metric+`=? AND id>?)) ORDER BY `+metric+` DESC,id ASC LIMIT ?`
 rows,e:=s.DB.DB.QueryContext(ctx,query,day,after.ID,after.Value,after.Value,after.ID,n+1);if e!=nil{return result,e};defer rows.Close()
 for rows.Next(){var item ActivityRankingItem;var data string;if e=rows.Scan(&item.ID,&item.Name,&item.Views,&item.Plays,&item.Seconds,&item.Assets,&data,&item.Value);e!=nil{return result,e};if kind=="asset"{item.Asset=&model.Asset{};if e=json.Unmarshal([]byte(data),item.Asset);e!=nil{return result,e}};result.Items=append(result.Items,item)}
 if e=rows.Err();e!=nil{return result,e};if len(result.Items)>n{result.Items=result.Items[:n];last:=result.Items[n-1];raw,_:=json.Marshal(activityRankingCursor{last.Value,last.ID,filter});result.NextCursor=base64.RawURLEncoding.EncodeToString(raw)}
 return result,nil
}
