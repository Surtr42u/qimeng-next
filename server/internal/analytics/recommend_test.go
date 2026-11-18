package analytics

import (
	"math"
	"reflect"
	"testing"
	"time"
)

func TestColdStartWeightsAndDailyPenalty(t *testing.T) {
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	w := DefaultWeights
	w.MaxRandom = 0
	input := []Candidate{{ID: "new", MediaType: "image", Tags: []string{"tag"}, Added: now}, {ID: "shown", MediaType: "image", Tags: []string{"tag"}, Added: now, Shown: 2}}
	result := RankRecommendations(input, w, 0, now)
	scores := map[string]float64{}
	for _, v := range result {
		scores[v.ID] = v.Score
	}
	if math.Abs(scores["new"]-.5115) > 1e-10 {
		t.Fatalf("cold-start score=%v, want .5115", scores["new"])
	}
	if math.Abs(scores["new"]-scores["shown"]-1.6) > 1e-10 {
		t.Fatal("daily penalty changed")
	}
}

func TestRecommendationSeedIsReproducibleAndPreservesInputs(t *testing.T) {
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	input := []Candidate{{ID: "a", MediaType: "image", Added: now}, {ID: "b", MediaType: "video", Added: now}, {ID: "c", MediaType: "animated_image", Added: now}}
	original := append([]Candidate{}, input...)
	first := RankRecommendations(input, DefaultWeights, 42, now)
	second := RankRecommendations(input, DefaultWeights, 42, now)
	if !reflect.DeepEqual(first, second) || !reflect.DeepEqual(input, original) {
		t.Fatal("non-deterministic or mutated input")
	}
	seen := map[string]bool{}
	for _, item := range first {
		if seen[item.ID] {
			t.Fatal("duplicate candidate")
		}
		seen[item.ID] = true
	}
	if len(seen) != len(input) {
		t.Fatal("candidate lost")
	}
}

func TestCalendarWindowsUseCalendarMonthsAndWeeks(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.FixedZone("test", 8*3600))
	tests := map[string]string{"7d": "2026-02-23", "30d": "2026-01-31", "month": "2024-04-01", "year": "2022-01-01"}
	for window, want := range tests {
		got, ok := WindowStart(now, window)
		if !ok || got.Format("2006-01-02") != want {
			t.Fatalf("%s: %s", window, got)
		}
	}
	week, _ := WindowStart(now, "week")
	if week.Weekday() != time.Monday {
		t.Fatal("week must begin on Monday")
	}
}

func TestInvalidWeightsRejected(t *testing.T) {
	w := DefaultWeights
	w.MaxRandom = math.NaN()
	if w.Valid() {
		t.Fatal("NaN accepted")
	}
	w = DefaultWeights
	w.Discovery = -1
	if w.Valid() {
		t.Fatal("negative weight accepted")
	}
	for _, w = range Presets() {
		if !w.Valid() {
			t.Fatal("invalid built-in preset")
		}
	}
}
