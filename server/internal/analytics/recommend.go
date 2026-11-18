// Package analytics contains deterministic recommendation and reporting rules, without IO.
package analytics

import (
	"hash/fnv"
	"math"
	"math/rand"
	"sort"
	"time"
)

// Weights are the eight weighted dimensions and the separately bounded random term.
type Weights struct {
	TagRelevance  float64 `json:"tagRelevance"`
	TagCollection float64 `json:"tagCollection"`
	Engagement    float64 `json:"engagement"`
	Recency       float64 `json:"recency"`
	LikeScore     float64 `json:"likeScore"`
	Discovery     float64 `json:"discovery"`
	Freshness     float64 `json:"freshness"`
	BrowseDepth   float64 `json:"browseDepth"`
	MaxRandom     float64 `json:"maxRandom"`
}

var DefaultWeights = Weights{.22, .15, .10, .15, .05, .20, .05, .03, .30}

func Presets() map[string]Weights {
	return map[string]Weights{
		"balanced": DefaultWeights,
		"familiar": {.15, .10, .20, .25, .10, .05, .05, .05, .10},
		"explore":  {.30, .20, .05, .05, .02, .30, .02, .05, .40},
		"fresh":    {.10, .05, .05, .10, .02, .25, .20, .03, .35},
	}
}

func (w Weights) Valid() bool {
	for _, v := range []float64{w.TagRelevance, w.TagCollection, w.Engagement, w.Recency, w.LikeScore, w.Discovery, w.Freshness, w.BrowseDepth, w.MaxRandom} {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 1 {
			return false
		}
	}
	return true
}

type Candidate struct {
	ID                                  string
	MediaType                           string
	Tags                                []string
	Views, Plays, Likes, Seconds, Shown int64
	Added, LastViewed                   time.Time
}
type Scored struct {
	ID    string  `json:"id"`
	Score float64 `json:"score"`
}

func ratio(value, maximum int64) float64 {
	if maximum == 0 {
		return 0
	}
	return float64(value) / float64(maximum)
}
func decayed(days float64, values [5]float64) float64 {
	for i, edge := range []float64{1, 3, 7, 30} {
		if days < edge {
			return values[i]
		}
	}
	return values[4]
}

// RankRecommendations preserves the confirmed dimensions and post-ranking type mix.
// The caller supplies all eligible candidates, not a top-N prefiltered sample.
func RankRecommendations(input []Candidate, w Weights, seed int64, now time.Time) []Scored {
	tags := map[string]int64{}
	hasTags := false
	var maxEngagement, maxLike, maxDepth int64
	for _, a := range input {
		maxEngagement = max(maxEngagement, a.Views+a.Plays)
		maxLike = max(maxLike, a.Likes)
		maxDepth = max(maxDepth, a.Seconds)
		seen := map[string]bool{}
		for _, tag := range a.Tags {
			hasTags = true
			if a.Views > 0 && !seen[tag] {
				tags[tag]++
				seen[tag] = true
			}
		}
	}
	if !hasTags {
		recovered := w.TagRelevance + w.TagCollection
		w.TagRelevance = 0
		w.TagCollection = 0
		w.Discovery += recovered * .5
		w.MaxRandom += recovered * .5
	}
	if maxLike == 0 {
		w.Freshness += w.LikeScore
		w.LikeScore = 0
	}
	if maxEngagement == 0 {
		w.Discovery += w.Engagement
		w.Engagement = 0
	}
	names := make([]string, 0, len(tags))
	var maxFrequency int64
	for name, count := range tags {
		names = append(names, name)
		maxFrequency = max(maxFrequency, count)
	}
	sort.Slice(names, func(i, j int) bool {
		if tags[names[i]] == tags[names[j]] {
			return names[i] < names[j]
		}
		return tags[names[i]] > tags[names[j]]
	})
	top := map[string]bool{}
	for _, name := range names[:min(20, len(names))] {
		top[name] = true
	}
	type ranked struct {
		Scored
		video bool
	}
	rankedItems := make([]ranked, 0, len(input))
	for _, a := range input {
		relevance, collection := 0.0, 0.0
		if maxFrequency == 0 {
			relevance = .2
		} else if len(a.Tags) > 0 {
			for _, tag := range a.Tags {
				relevance += ratio(tags[tag], maxFrequency)
			}
			relevance /= float64(len(a.Tags))
		}
		if len(top) == 0 {
			collection = .15
		} else {
			seen := map[string]bool{}
			intersection := 0
			for _, tag := range a.Tags {
				if !seen[tag] {
					seen[tag] = true
					if top[tag] {
						intersection++
					}
				}
			}
			collection = float64(intersection) / float64(len(top)+len(seen)-intersection)
		}
		recency := .3
		if !a.LastViewed.IsZero() {
			recency = decayed(now.Sub(a.LastViewed).Hours()/24, [5]float64{1, .8, .5, .2, 0})
		}
		freshness := decayed(now.Sub(a.Added).Hours()/24, [5]float64{1, .7, .4, .2, 0})
		hash := fnv.New64a()
		_, _ = hash.Write([]byte(a.ID))
		random := float64((hash.Sum64()^uint64(seed))&65535) / 65535 * w.MaxRandom
		score := relevance*w.TagRelevance + collection*w.TagCollection + ratio(a.Views+a.Plays, maxEngagement)*w.Engagement +
			recency*w.Recency + ratio(a.Likes, maxLike)*w.LikeScore + (1-math.Min(float64(a.Views)/5, 1))*w.Discovery +
			freshness*w.Freshness + ratio(a.Seconds, maxDepth)*w.BrowseDepth + random - .8*float64(a.Shown)
		rankedItems = append(rankedItems, ranked{Scored{a.ID, score}, a.MediaType == "video"})
	}
	sort.SliceStable(rankedItems, func(i, j int) bool {
		if rankedItems[i].Score == rankedItems[j].Score {
			return rankedItems[i].ID < rankedItems[j].ID
		}
		return rankedItems[i].Score > rankedItems[j].Score
	})
	rng := rand.New(rand.NewSource(seed))
	if seed > 0 {
		for start := 0; start < len(rankedItems); {
			end := start + 1
			anchor := rankedItems[start].Score
			for end < len(rankedItems) && anchor-rankedItems[end].Score <= .05 {
				end++
			}
			rng.Shuffle(end-start, func(i, j int) {
				rankedItems[start+i], rankedItems[start+j] = rankedItems[start+j], rankedItems[start+i]
			})
			start = end
		}
	}
	images, videos := []Scored{}, []Scored{}
	for _, item := range rankedItems {
		if item.video {
			videos = append(videos, item.Scored)
		} else {
			images = append(images, item.Scored)
		}
	}
	output := make([]Scored, 0, len(input))
	i, v := 0, 0
	for i < len(images) || v < len(videos) {
		if v == len(videos) || (i < len(images) && rng.Intn(len(images)-i+len(videos)-v) < len(images)-i) {
			output = append(output, images[i])
			i++
		} else {
			output = append(output, videos[v])
			v++
		}
	}
	return output
}

// WindowStart uses server-local calendar days; all has no lower bound.
func WindowStart(now time.Time, window string) (time.Time, bool) {
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	switch window {
	case "all":
		return time.Time{}, true
	case "7d":
		return midnight.AddDate(0, 0, -6), true
	case "day", "30d":
		return midnight.AddDate(0, 0, -29), true
	case "90d":
		return midnight.AddDate(0, 0, -89), true
	case "week":
		return midnight.AddDate(0, 0, -(int(midnight.Weekday())+6)%7-11*7), true
	case "month":
		return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).AddDate(0, -23, 0), true
	case "quarter":
		return time.Date(now.Year(), time.Month((int(now.Month())-1)/3*3+1), 1, 0, 0, 0, 0, now.Location()).AddDate(0, -21, 0), true
	case "year":
		return time.Date(now.Year()-4, 1, 1, 0, 0, 0, 0, now.Location()), true
	default:
		return time.Time{}, false
	}
}
