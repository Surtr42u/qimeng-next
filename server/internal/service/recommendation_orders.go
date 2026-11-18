package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"qimeng.local/next/server/internal/analytics"
	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/store"
)

const maxOrderIDs = 100_000
const maxOrders = 32
const orderTTL = time.Minute

type orderScope struct {
	Generation int64
	Day, Kind, MediaType string
	Seed, Preferences int64
}
type recommendationOrder struct {
	id string
	ids []string
	created time.Time
	used uint64
}

// The catalog is authoritative. Only IDs are reused, so a cache hit cannot
// expose stale author metadata, a disabled library or an old file URL.
type recommendationOrders struct {
	mu sync.Mutex
	cold chan struct{}
	byScope map[orderScope]*recommendationOrder
	total int
	clock uint64
}
func (orders *recommendationOrders) init() {
	orders.cold=make(chan struct{},1)
	orders.byScope=make(map[orderScope]*recommendationOrder)
}
func (orders *recommendationOrders) lookup(scope orderScope,now time.Time) *recommendationOrder {
	orders.mu.Lock();defer orders.mu.Unlock()
	order:=orders.byScope[scope]
	if order==nil{return nil}
	if !now.Before(order.created.Add(orderTTL)){
		orders.total-=len(order.ids);delete(orders.byScope,scope);return nil
	}
	orders.clock++;order.used=orders.clock
	return order
}
func (orders *recommendationOrders) remember(scope orderScope,order *recommendationOrder,now time.Time) {
	if len(order.ids)>maxOrderIDs||!now.Before(order.created.Add(orderTTL)){return}
	orders.mu.Lock();defer orders.mu.Unlock()
	for key,existing:=range orders.byScope {
		if key==scope||key.Generation<scope.Generation||!now.Before(existing.created.Add(orderTTL)){
			orders.total-=len(existing.ids);delete(orders.byScope,key)
		}
	}
	for len(orders.byScope)>=maxOrders||orders.total+len(order.ids)>maxOrderIDs{
		var oldestKey orderScope
		var oldest *recommendationOrder
		for key,existing:=range orders.byScope{if oldest==nil||existing.used<oldest.used{oldestKey,oldest=key,existing}}
		orders.total-=len(oldest.ids);delete(orders.byScope,oldestKey)
	}
	orders.clock++;order.used=orders.clock;orders.total+=len(order.ids);orders.byScope[scope]=order
}

func (s *Service) orderGeneration(ctx context.Context) (int64,error) {
	var generation int64
	err:=s.DB.DB.QueryRowContext(ctx,"SELECT generation FROM recommendation_epoch WHERE id=1").Scan(&generation)
	return generation,err
}
func expiredRecommendation() error {
	return Fail(409,"recommendation_expired","推荐顺序已更新，请从第一页继续。")
}
func parseOrderCursor(value string)(string,int,error){
	if value==""{return "",0,nil}
	if len(value)>256{return "",0,Fail(400,"invalid_cursor","推荐游标无效。")}
	raw,err:=base64.RawURLEncoding.DecodeString(value)
	if err!=nil{return "",0,Fail(400,"invalid_cursor","推荐游标无效。")}
	id,index,ok:=strings.Cut(string(raw),":");offset,err:=strconv.Atoi(index)
	if !ok||id==""||offset<0||err!=nil{return "",0,Fail(400,"invalid_cursor","推荐偏移无效。")}
	return id,offset,nil
}

// A single unusually large catalog can still be paged without retaining an
// unbounded ID list between requests or displacing a smaller, frequently used
// order. Its short-lived cursor binds the recomputation to its original scope.
func (s *Service) uncachedOrderID(scope orderScope,created time.Time) string {
	stamp:=strconv.FormatInt(created.UnixMilli(),36)
	body,_:=json.Marshal(scope)
	mac:=hmac.New(sha256.New,[]byte(s.Config.AdminToken))
	mac.Write([]byte("recommendations/v1/"));mac.Write(body);mac.Write([]byte(stamp))
	return "u."+stamp+"."+base64.RawURLEncoding.EncodeToString(mac.Sum(nil)[:16])
}
func (s *Service) orderTime(scope orderScope,id string,now time.Time)(time.Time,error){
	parts:=strings.Split(id,".")
	if len(parts)!=3||parts[0]!="u"{return time.Time{},expiredRecommendation()}
	millis,err:=strconv.ParseInt(parts[1],36,64)
	if err!=nil{return time.Time{},expiredRecommendation()}
	created:=time.UnixMilli(millis)
	if created.After(now)||!now.Before(created.Add(orderTTL))||!hmac.Equal([]byte(id),[]byte(s.uncachedOrderID(scope,created))){return time.Time{},expiredRecommendation()}
	return created,nil
}
func (s *Service) rankOrder(ctx context.Context,scope orderScope,prefs RecommendationPreferences,inputs []analytics.Candidate,when time.Time)(*recommendationOrder,error) {
	scored:=analytics.RankRecommendations(inputs,prefs.Weights,scope.Seed,when)
	ids:=make([]string,len(scored));for i,item:=range scored{ids[i]=item.ID}
	order:=&recommendationOrder{ids:ids,id:ID(),created:when}
	if len(ids)>maxOrderIDs{order.id=s.uncachedOrderID(scope,when);return order,nil}
	generation,err:=s.orderGeneration(ctx);if err!=nil{return nil,err}
	if scope.Generation==generation{s.recommendations.remember(scope,order,time.Now())}
	return order,nil
}
func (s *Service) recommendationOrder(ctx context.Context,scope orderScope,prefs RecommendationPreferences,id string,now time.Time)(*recommendationOrder,error){
	if order:=s.recommendations.lookup(scope,now);order!=nil&&(id==""||id==order.id){return order,nil}
	when:=now
	if id!=""{var err error;when,err=s.orderTime(scope,id,now);if err!=nil{return nil,err}}
	select {case s.recommendations.cold<-struct{}{}:defer func(){<-s.recommendations.cold}();case <-ctx.Done():return nil,ctx.Err()}
	if order:=s.recommendations.lookup(scope,time.Now());order!=nil&&(id==""||id==order.id){return order,nil}
	if id!=""&&!time.Now().Before(when.Add(orderTTL)){return nil,expiredRecommendation()}
	inputs,err:=s.recommendationInputs(ctx,RecommendationFilter{Kind:scope.Kind,MediaType:scope.MediaType},scope.Day)
	if err!=nil{return nil,err}
	return s.rankOrder(ctx,scope,prefs,inputs,when)
}

func (s *Service) warmRecommendationOrders() {
	defer s.workers.Done()
	ctx:=s.workCtx
	if err:=s.warmRegularOrders(ctx);err!=nil&&ctx.Err()==nil {slog.Warn("recommendation warmup unavailable")}
}
func (s *Service) warmRegularOrders(ctx context.Context) error {
	select {case s.recommendations.cold<-struct{}{}:defer func(){<-s.recommendations.cold}();case <-ctx.Done():return ctx.Err()}
	when:=time.Now()
	generation,err:=s.orderGeneration(ctx);if err!=nil{return err}
	prefs,err:=s.RecommendationPreferences(ctx);if err!=nil{return err}
	scope:=orderScope{Generation:generation,Day:when.Format("2006-01-02"),Kind:"regular",Preferences:prefs.Revision}
	var inputs []analytics.Candidate
	for _,seed:=range []int64{1,0}{
		scope.Seed=seed
		if s.recommendations.lookup(scope,when)!=nil {continue}
		current,err:=s.orderGeneration(ctx);if err!=nil{return err};if current!=generation{return nil}
		if inputs==nil {
			inputs,err=s.recommendationInputs(ctx,RecommendationFilter{Kind:"regular"},scope.Day);if err!=nil{return err}
			if len(inputs)>maxOrderIDs{return nil}
		}
		if _,err=s.rankOrder(ctx,scope,prefs,inputs,when);err!=nil{return err}
	}
	return nil
}

func (s *Service) Recommendations(ctx context.Context,f RecommendationFilter)(RecommendationPage,error) {
	result:=RecommendationPage{Items:[]model.Asset{}}
	n,err:=limited(f.Limit,50);if err!=nil{return result,err}
	if f.Kind!=""&&f.Kind!="regular"&&f.Kind!="cos"{return result,Fail(400,"invalid_filter","未知来源分区。")}
	if f.MediaType!=""&&f.MediaType!="image"&&f.MediaType!="animated_image"&&f.MediaType!="video"{return result,Fail(400,"invalid_filter","未知媒体类别。")}
	id,offset,err:=parseOrderCursor(f.Cursor);if err!=nil{return result,err}
	now:=time.Now()
	prefs,err:=s.RecommendationPreferences(ctx);if err!=nil{return result,err}
	generation,err:=s.orderGeneration(ctx);if err!=nil{return result,err}
	scope:=orderScope{generation,now.Format("2006-01-02"),f.Kind,f.MediaType,f.Seed,prefs.Revision}
	order,err:=s.recommendationOrder(ctx,scope,prefs,id,now);if err!=nil{return result,err}
	if offset>len(order.ids){return result,Fail(400,"invalid_cursor","推荐偏移超出顺序范围。")}
	end:=offset+min(n,len(order.ids)-offset)
	slice,err:=json.Marshal(order.ids[offset:end]);if err!=nil{return result,err}
	tx,err:=s.DB.DB.BeginTx(ctx,nil);if err!=nil{return result,err};defer tx.Rollback()
	result.Items,err=store.List[model.Asset](ctx,tx,"SELECT "+assetJSON+" FROM json_each(?) page JOIN assets a ON a.id=page.value JOIN authors au ON au.id=a.author_id JOIN libraries lib ON lib.id=a.library_id WHERE a.trashed=0 AND coalesce(json_extract(lib.data,'$.enabled'),1)=1 AND (?='' OR "+assetPartition+"=?) AND (?='' OR a.media_type=?) ORDER BY page.key",scope.Day,string(slice),f.Kind,f.Kind,f.MediaType,f.MediaType)
	if err!=nil{return result,err}
	shown:=make([]string,len(result.Items));for i,item:=range result.Items{shown[i]=item.ID}
	returned,err:=json.Marshal(shown);if err!=nil{return result,err}
	if _,err=tx.ExecContext(ctx,"INSERT INTO recommendation_shown(day,asset_id,count) SELECT ?,value,1 FROM json_each(?) WHERE true ON CONFLICT(day,asset_id) DO UPDATE SET count=count+1",scope.Day,string(returned));err!=nil{return result,err}
	if _,err=tx.ExecContext(ctx,"DELETE FROM recommendation_shown WHERE day<>?",scope.Day);err!=nil{return result,err}
	if err=tx.Commit();err!=nil{return result,err}
	result.SnapshotID=order.id
	if end<len(order.ids){result.NextCursor=base64.RawURLEncoding.EncodeToString([]byte(order.id+":"+strconv.Itoa(end)))}
	return result,nil
}
