package service

import (
 "context"
 "database/sql"
 "encoding/base64"
 "encoding/json"
 "errors"
 "fmt"
 "strconv"
 "strings"
 "time"

 "qimeng.local/next/server/internal/analytics"
 "qimeng.local/next/server/internal/model"
 "qimeng.local/next/server/internal/store"
)

type RecommendationPreferences struct {
 Revision int64 `json:"revision"`
 Weights analytics.Weights `json:"weights"`
 Presets map[string]analytics.Weights `json:"presets"`
}
type RecommendationUpdate struct { ExpectedRevision int64 `json:"expectedRevision"`; Weights analytics.Weights `json:"weights"` }

func(s *Service)RecommendationPreferences(ctx context.Context)(RecommendationPreferences,error){
 value,e:=store.Get[RecommendationPreferences](ctx,s.DB.DB,"SELECT value FROM settings WHERE key='recommendation_preferences'")
 if errors.Is(e,sql.ErrNoRows){value=RecommendationPreferences{Revision:1,Weights:analytics.DefaultWeights}}else if e!=nil{return value,e}
 value.Presets=analytics.Presets();return value,nil
}
func(s *Service)UpdateRecommendationPreferences(ctx context.Context,w *Write,in RecommendationUpdate)(RecommendationPreferences,error){
 if e:=Authorize(w.Principal,"admin","");e!=nil{return RecommendationPreferences{},e}
 current,e:=s.RecommendationPreferences(ctx);if e!=nil{return current,e};if current.Revision!=in.ExpectedRevision{return current,Conflict(current.Revision)}
 if !in.Weights.Valid(){return current,Fail(400,"invalid_weights","推荐权重必须为0到1的有限数。")}
 current.Revision++;current.Weights=in.Weights;data,e:=store.Encode(current);if e!=nil{return current,e};q,e:=w.Q();if e!=nil{return current,e}
 if _,e=q.ExecContext(ctx,"INSERT INTO settings(key,value) VALUES('recommendation_preferences',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value",data);e!=nil{return current,e}
 _,e=q.ExecContext(ctx,"DELETE FROM recommendation_snapshots");return current,e
}

type RecommendationFilter struct {Kind,MediaType,Cursor string;Seed int64;Limit int}
type RecommendationPage struct { Items []model.Asset `json:"items"`;NextCursor string `json:"nextCursor,omitempty"`;SnapshotID string `json:"snapshotId"` }

func(s *Service)Recommendations(ctx context.Context,f RecommendationFilter)(RecommendationPage,error){
 result:=RecommendationPage{Items:[]model.Asset{}}
 n,e:=limited(f.Limit,50);if e!=nil{return result,e}
 if f.Kind!=""&&f.Kind!="regular"&&f.Kind!="cos"{return result,Fail(400,"invalid_filter","未知来源分区。")}
 if f.MediaType!=""&&f.MediaType!="image"&&f.MediaType!="animated_image"&&f.MediaType!="video"{return result,Fail(400,"invalid_filter","未知媒体类别。")}
 // Serializes snapshot publication and impression counts with library mutations;
 // no background goroutine or second authoritative recommendation index exists.
 s.mu.Lock();defer s.mu.Unlock()
 preferences,e:=s.RecommendationPreferences(ctx);if e!=nil{return result,e}
 now:=time.Now();day:=now.Format("2006-01-02")
 filter:=fmt.Sprintf("%s/%s/%d/%d/%s",f.Kind,f.MediaType,f.Seed,preferences.Revision,day)
 var ids []string;offset:=0;var snapshot string
 if f.Cursor!=""{
  raw,e:=base64.RawURLEncoding.DecodeString(f.Cursor);if e!=nil{return result,Fail(400,"invalid_cursor","推荐游标无效。")}
  parts:=strings.Split(string(raw),":");if len(parts)!=2{return result,Fail(400,"invalid_cursor","推荐游标无效。")}
  snapshot=parts[0];offset,e=strconv.Atoi(parts[1]);if e!=nil||offset<0{return result,Fail(400,"invalid_cursor","推荐偏移无效。")}
  var saved,encoded string;var created int64
  e=s.DB.DB.QueryRowContext(ctx,"SELECT filter,created_at,ids FROM recommendation_snapshots WHERE id=?",snapshot).Scan(&saved,&created,&encoded)
  if errors.Is(e,sql.ErrNoRows)||e==nil&&(saved!=filter||now.Unix()-created>=60){return result,Fail(409,"recommendation_expired","推荐顺序已更新，请从第一页继续。")};if e!=nil{return result,e}
  if e=json.Unmarshal([]byte(encoded),&ids);e!=nil{return result,e}
 }else{
  inputs,e:=s.recommendationInputs(ctx,f,day);if e!=nil{return result,e}
  scores:=analytics.RankRecommendations(inputs,preferences.Weights,f.Seed,now)
  ids=make([]string,len(scores));for i,item:=range scores{ids[i]=item.ID}
  snapshot=ID();encoded,e:=json.Marshal(ids);if e!=nil{return result,e}
  if _,e=s.DB.DB.ExecContext(ctx,"DELETE FROM recommendation_snapshots WHERE created_at<=?",now.Unix()-60);e!=nil{return result,e}
  // Bound the number of retained snapshots, without truncating any candidate set.
  if _,e=s.DB.DB.ExecContext(ctx,"DELETE FROM recommendation_snapshots WHERE id IN (SELECT id FROM recommendation_snapshots ORDER BY created_at DESC LIMIT -1 OFFSET 31)");e!=nil{return result,e}
  if _,e=s.DB.DB.ExecContext(ctx,"INSERT INTO recommendation_snapshots VALUES(?,?,?,?)",snapshot,filter,now.Unix(),string(encoded));e!=nil{return result,e}
 }
 result.SnapshotID=snapshot
 if offset>len(ids){return result,Fail(400,"invalid_cursor","推荐偏移超出顺序范围。")}
 end:=min(offset+n,len(ids))
 for _,id:=range ids[offset:end]{a,e:=s.Asset(ctx,id);if e!=nil{return result,e};if a.Trashed{continue};result.Items=append(result.Items,a)}
 tx,e:=s.DB.DB.BeginTx(ctx,nil);if e!=nil{return result,e};defer tx.Rollback()
 for _,a:=range result.Items{if _,e=tx.ExecContext(ctx,"INSERT INTO recommendation_shown VALUES(?,?,1) ON CONFLICT(day,asset_id) DO UPDATE SET count=count+1",day,a.ID);e!=nil{return result,e}}
 if _,e=tx.ExecContext(ctx,"DELETE FROM recommendation_shown WHERE day<>?",day);e!=nil{return result,e};if e=tx.Commit();e!=nil{return result,e}
 if end<len(ids){result.NextCursor=base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%d",snapshot,end)))}
 return result,nil
}

func(s *Service)recommendationInputs(ctx context.Context,f RecommendationFilter,day string)([]analytics.Candidate,error){
 rows,e:=s.DB.DB.QueryContext(ctx,`WITH totals AS (SELECT asset_id,sum(views) AS views,sum(plays) AS plays,sum(seconds) AS seconds FROM asset_daily_stats GROUP BY asset_id), last_views AS (SELECT asset_id,max(created_at) AS last_view FROM events WHERE json_extract(data,'$.kind')='view' GROUP BY asset_id), likes_total AS (SELECT asset_id,count(*) AS likes FROM likes GROUP BY asset_id)
 SELECT a.id,a.media_type,a.created_at,json_extract(a.data,'$.tags'),coalesce(t.views,0),coalesce(t.plays,0),coalesce(t.seconds,0),coalesce(l.likes,0),coalesce(v.last_view,''),coalesce(sh.count,0)
 FROM assets a JOIN libraries lib ON lib.id=a.library_id LEFT JOIN totals t ON t.asset_id=a.id LEFT JOIN likes_total l ON l.asset_id=a.id LEFT JOIN last_views v ON v.asset_id=a.id LEFT JOIN recommendation_shown sh ON sh.asset_id=a.id AND sh.day=?
 WHERE a.trashed=0 AND (?='' OR lib.kind=?) AND (?='' OR a.media_type=?) ORDER BY a.id`,day,f.Kind,f.Kind,f.MediaType,f.MediaType)
 if e!=nil{return nil,e};defer rows.Close();items:=[]analytics.Candidate{}
 for rows.Next(){var a analytics.Candidate;var added,last,tags string;if e=rows.Scan(&a.ID,&a.MediaType,&added,&tags,&a.Views,&a.Plays,&a.Seconds,&a.Likes,&last,&a.Shown);e!=nil{return nil,e}
  if e=json.Unmarshal([]byte(tags),&a.Tags);e!=nil{return nil,e};if a.Added,e=time.Parse(time.RFC3339Nano,added);e!=nil{return nil,e};if last!=""{if a.LastViewed,e=time.Parse(time.RFC3339Nano,last);e!=nil{return nil,e}};items=append(items,a)
 }
 return items,rows.Err()
}

type RankedAsset struct { Asset model.Asset `json:"asset"`;Score int64 `json:"score"` }
func(s *Service)Rankings(ctx context.Context,period,kind,cursor string,count int)(model.Page[RankedAsset],error){
 result:=model.Page[RankedAsset]{Items:[]RankedAsset{}}
 n,e:=limited(count,50);if e!=nil{return result,e};if kind!=""&&kind!="regular"&&kind!="cos"{return result,Fail(400,"invalid_filter","来源分区无效。")}
 days,ok:=map[string]int{"day":1,"week":7,"month":30,"quarter":90,"year":365,"all":0}[period];if !ok{return result,Fail(400,"invalid_period","排行周期无效。")}
 cutoff:="";if days>0{cutoff=time.Now().AddDate(0,0,-days).Format("2006-01-02")}
 scope:="rank/"+period+"/"+kind+"/"+cutoff;offset,e:=pageOffset(cursor,scope);if e!=nil{return result,e}
 rows,e:=s.DB.DB.QueryContext(ctx,`WITH activity AS (SELECT asset_id,sum(views+plays) AS score FROM asset_daily_stats WHERE day>=? GROUP BY asset_id),votes AS(SELECT asset_id,count(*) AS score FROM likes WHERE day>=? GROUP BY asset_id)
 SELECT a.id,coalesce(activity.score,0)+coalesce(votes.score,0) AS heat FROM assets a JOIN libraries lib ON lib.id=a.library_id LEFT JOIN activity ON activity.asset_id=a.id LEFT JOIN votes ON votes.asset_id=a.id
 WHERE a.trashed=0 AND (?='' OR lib.kind=?) AND (?='all' OR coalesce(activity.score,0)+coalesce(votes.score,0)>0)
 ORDER BY heat DESC,coalesce(json_extract(a.data,'$.sourceMtime'),a.created_at) DESC,a.id LIMIT ? OFFSET ?`,cutoff,cutoff,kind,kind,period,n+1,offset)
 if e!=nil{return result,e};type row struct{id string;score int64};values:=[]row{}
 for rows.Next(){var item row;if e=rows.Scan(&item.id,&item.score);e!=nil{rows.Close();return result,e};values=append(values,item)}
 e=rows.Err();rows.Close();if e!=nil{return result,e}
 for _,item:=range values{a,e:=s.Asset(ctx,item.id);if e!=nil{return result,e};result.Items=append(result.Items,RankedAsset{a,item.score})}
 return paged(result.Items,n,offset,scope),nil
}
