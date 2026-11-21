package service

import (
	"testing"
	"time"

	"qimeng.local/next/server/internal/analytics"
)

func TestOrderTTLAllowsNormalReading(t *testing.T) {
	if orderTTL < 10*time.Minute {
		t.Fatalf("order TTL %s is shorter than a normal reading session", orderTTL)
	}
}

func TestRecommendationInputSnapshotSharesOnlyMatchingQueryDimensions(t *testing.T) {
	var cache recommendationInputCache
	now := time.Now()
	scope := orderScope{Generation: 3, Day: "2026-11-18", Kind: "regular"}
	cache.remember(scope, []analytics.Candidate{{ID: "one"}}, now)
	otherSeed := scope
	otherSeed.Seed, otherSeed.Preferences = 9, 9
	if items, ok := cache.lookup(otherSeed, now); !ok || len(items) != 1 || items[0].ID != "one" {
		t.Fatal("seed or preference revision split a reusable input snapshot")
	}
	for name, changed := range map[string]orderScope{
		"generation": {Generation: 4, Day: scope.Day, Kind: scope.Kind},
		"day":        {Generation: scope.Generation, Day: "2026-11-19", Kind: scope.Kind},
		"partition":  {Generation: scope.Generation, Day: scope.Day, Kind: "cos"},
		"media type": {Generation: scope.Generation, Day: scope.Day, Kind: scope.Kind, MediaType: "video"},
	} {
		if _, ok := cache.lookup(changed, now); ok {
			t.Fatalf("%s reused an input snapshot from another query", name)
		}
	}
	if _, ok := cache.lookup(scope, now.Add(recommendationInputTTL)); ok {
		t.Fatal("expired input snapshot survived")
	}
}

func TestRecommendationInputSnapshotEvictsOldestAndSkipsOversize(t *testing.T) {
	var cache recommendationInputCache
	now := time.Now()
	scope := func(day string) orderScope {
		return orderScope{Generation: 1, Day: day, Kind: "regular"}
	}
	for i := 0; i < maxRecommendationInputs; i++ {
		cache.remember(scope(string(rune('a'+i))), []analytics.Candidate{{ID: "item"}}, now.Add(time.Duration(i)*time.Second))
	}
	cache.remember(scope("next"), []analytics.Candidate{{ID: "next"}}, now.Add(time.Duration(maxRecommendationInputs)*time.Second))
	if _, ok := cache.lookup(scope("a"), now.Add(time.Duration(maxRecommendationInputs)*time.Second)); ok {
		t.Fatal("newest snapshot did not evict the coldest entry")
	}
	if items, ok := cache.lookup(scope("next"), now.Add(time.Duration(maxRecommendationInputs)*time.Second)); !ok || len(items) != 1 {
		t.Fatal("new snapshot was not published")
	}
	cache.remember(scope("large"), make([]analytics.Candidate, maxRecommendationInputCandidates+1), now.Add(time.Duration(maxRecommendationInputs)*time.Second))
	if _, ok := cache.lookup(scope("large"), now.Add(time.Duration(maxRecommendationInputs)*time.Second)); ok {
		t.Fatal("oversize input snapshot was cached")
	}
}

func TestRecommendationOrderBudgetEvictsOnlyEnoughColdOrders(t *testing.T) {
	var cache recommendationOrders
	cache.init()
	now := time.Now()
	scope := func(seed int64) orderScope { return orderScope{Generation: 1, Seed: seed, Day: "2026-11-18"} }
	put := func(seed int64, n int) {
		cache.remember(scope(seed), &recommendationOrder{created: now, ids: make([]string, n)}, now)
	}
	put(1, 50_000)
	put(2, 40_000)
	put(3, 10_000)
	cache.lookup(scope(1), now)
	cache.lookup(scope(2), now)
	put(4, 10_000)
	if cache.lookup(scope(3), now) != nil || cache.lookup(scope(1), now) == nil || cache.total != maxOrderIDs {
		t.Fatal("cold order eviction lost hot entries or exceeded the shared budget")
	}
	put(5, maxOrderIDs+1)
	if cache.lookup(scope(5), now) != nil || cache.lookup(scope(1), now) == nil {
		t.Fatal("an uncached catalog displaced reusable orders")
	}
	if cache.lookup(scope(1), now.Add(orderTTL)) != nil {
		t.Fatal("access extended creation TTL")
	}
}

func TestOrderTTLBoundaryHonoursJustBeforeAndRejectsAtExpiry(t *testing.T) {
	s := Service{Config: Config{AdminToken: "synthetic-ephemeral-cache-only"}}
	scope := orderScope{Generation: 2, Day: "2026-11-18", Kind: "regular", Seed: 3, Preferences: 1}
	created := time.Now().Truncate(time.Millisecond)
	cursor := s.uncachedOrderID(scope, created)
	if at, err := s.orderTime(scope, cursor, created.Add(orderTTL-time.Nanosecond)); err != nil || !at.Equal(created) {
		t.Fatalf("order expired before its TTL: %v", err)
	}
	if _, err := s.orderTime(scope, cursor, created.Add(orderTTL)); err == nil {
		t.Fatal("order survived its TTL boundary")
	}
	if _, err := s.orderTime(scope, cursor, created.Add(orderTTL+time.Nanosecond)); err == nil {
		t.Fatal("order was replayed past its TTL")
	}

	var orders recommendationOrders
	orders.init()
	order := &recommendationOrder{ids: []string{"one"}, created: created}
	orders.remember(scope, order, created)
	if orders.lookup(scope, created.Add(orderTTL-time.Nanosecond)) == nil {
		t.Fatal("cached order expired before its TTL")
	}
	if orders.lookup(scope, created.Add(orderTTL)) != nil {
		t.Fatal("cached order survived its TTL boundary")
	}
	orders.remember(scope, order, created)
	if orders.lookup(scope, created.Add(orderTTL+time.Nanosecond)) != nil {
		t.Fatal("cached order was replayed past its TTL")
	}
}

func TestOversizeRecommendationCursorBindsGenerationSeedAndExpiry(t *testing.T) {
	s := Service{Config: Config{AdminToken: "synthetic-ephemeral-cache-only"}}
	now := time.Now().Truncate(time.Millisecond)
	key := orderScope{Generation: 5, Day: "2026-11-18", Kind: "regular", Seed: 7, Preferences: 1}
	cursor := s.uncachedOrderID(key, now)
	if at, err := s.orderTime(key, cursor, now.Add(time.Second)); err != nil || !at.Equal(now) {
		t.Fatal("current continuation was lost")
	}
	changed := key
	changed.Generation++
	if _, err := s.orderTime(changed, cursor, now); err == nil {
		t.Fatal("old catalog order replayed")
	}
	changed = key
	changed.Seed++
	if _, err := s.orderTime(changed, cursor, now); err == nil {
		t.Fatal("cursor crossed streams")
	}
	if _, err := s.orderTime(key, cursor, now.Add(orderTTL)); err == nil {
		t.Fatal("expired uncached order replayed")
	}
}
