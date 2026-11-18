package service

import (
	"testing"
	"time"
)

func TestRecommendationOrderBudgetEvictsOnlyEnoughColdOrders(t *testing.T) {
	var cache recommendationOrders
	cache.init()
	now:=time.Now()
	scope:=func(seed int64) orderScope {return orderScope{Generation:1,Seed:seed,Day:"2026-11-18"}}
	put:=func(seed int64,n int){cache.remember(scope(seed),&recommendationOrder{created:now,ids:make([]string,n)},now)}
	put(1,50_000);put(2,40_000);put(3,10_000)
	cache.lookup(scope(1),now);cache.lookup(scope(2),now)
	put(4,10_000)
	if cache.lookup(scope(3),now)!=nil||cache.lookup(scope(1),now)==nil||cache.total!=maxOrderIDs {t.Fatal("cold order eviction lost hot entries or exceeded the shared budget")}
	put(5,maxOrderIDs+1)
	if cache.lookup(scope(5),now)!=nil||cache.lookup(scope(1),now)==nil {t.Fatal("an uncached catalog displaced reusable orders")}
	if cache.lookup(scope(1),now.Add(orderTTL))!=nil {t.Fatal("access extended creation TTL")}
}

func TestOversizeRecommendationCursorBindsGenerationSeedAndExpiry(t *testing.T) {
	s:=Service{Config:Config{AdminToken:"synthetic-ephemeral-cache-only"}}
	now:=time.Now().Truncate(time.Millisecond)
	key:=orderScope{Generation:5,Day:"2026-11-18",Kind:"regular",Seed:7,Preferences:1}
	cursor:=s.uncachedOrderID(key,now)
	if at,err:=s.orderTime(key,cursor,now.Add(time.Second));err!=nil||!at.Equal(now){t.Fatal("current continuation was lost")}
	changed:=key;changed.Generation++
	if _,err:=s.orderTime(changed,cursor,now);err==nil {t.Fatal("old catalog order replayed")}
	changed=key;changed.Seed++
	if _,err:=s.orderTime(changed,cursor,now);err==nil {t.Fatal("cursor crossed streams")}
	if _,err:=s.orderTime(key,cursor,now.Add(orderTTL));err==nil {t.Fatal("expired uncached order replayed")}
}
