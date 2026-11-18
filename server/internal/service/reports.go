package service

import (
	"context"
	"time"

	"qimeng.local/next/server/internal/analytics"
)

type ActivityTotals struct {
	Views   int64 `json:"views"`
	Plays   int64 `json:"plays"`
	Seconds int64 `json:"seconds"`
}
type ActivityDimension struct {
	Key string `json:"key"`
	ActivityTotals
	Assets int64 `json:"assets"`
	Bytes  int64 `json:"bytes"`
}
type Overview struct {
	Range               string              `json:"range"`
	Inventory           map[string]int64    `json:"inventory"`
	Totals              ActivityTotals      `json:"totals"`
	ActiveDays          int                 `json:"activeDays"`
	ViewedAssets        int                 `json:"viewedAssets"`
	ViewedAuthors       int                 `json:"viewedAuthors"`
	ViewedTags          int                 `json:"viewedTags"`
	AverageViewsPerFile *float64            `json:"averageViewsPerFile"`
	MediaTypes          []ActivityDimension `json:"mediaTypes"`
	Sources             []ActivityDimension `json:"sources"`
}

func rangeStart(value string) (time.Time, error) {
	start, ok := analytics.WindowStart(time.Now(), value)
	if !ok {
		return time.Time{}, Fail(400, "invalid_range", "统计窗口无效。")
	}
	return start, nil
}
func dayString(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format("2006-01-02")
}

func (s *Service) StatsOverview(ctx context.Context, window string) (Overview, error) {
	result := Overview{Range: window, MediaTypes: []ActivityDimension{}, Sources: []ActivityDimension{}}
	start, e := rangeStart(window)
	if e != nil {
		return result, e
	}
	day := dayString(start)
	result.Inventory, e = s.Stats(ctx)
	if e != nil {
		return result, e
	}
	// Totals retain historical activity even when an asset was permanently removed.
	e = s.DB.DB.QueryRowContext(ctx, `SELECT coalesce(sum(views),0),coalesce(sum(plays),0),coalesce(sum(seconds),0),count(DISTINCT CASE WHEN views+plays+seconds>0 THEN day END) FROM asset_daily_stats WHERE day>=?`, day).Scan(&result.Totals.Views, &result.Totals.Plays, &result.Totals.Seconds, &result.ActiveDays)
	if e != nil {
		return result, e
	}
	var liveViews int64
	e = s.DB.DB.QueryRowContext(ctx, `WITH viewed AS(SELECT d.asset_id,sum(d.views) AS views FROM asset_daily_stats d JOIN assets a ON a.id=d.asset_id AND a.trashed=0 WHERE d.day>=? AND d.views>0 GROUP BY d.asset_id)
 SELECT coalesce(sum(views),0),count(*),(SELECT count(DISTINCT aa.author_id) FROM viewed v JOIN asset_authors aa ON aa.asset_id=v.asset_id),(SELECT count(DISTINCT at.tag) FROM viewed v JOIN asset_tag_names at ON at.asset_id=v.asset_id) FROM viewed`, day).Scan(&liveViews, &result.ViewedAssets, &result.ViewedAuthors, &result.ViewedTags)
	if e != nil {
		return result, e
	}
	if result.ViewedAssets > 0 {
		average := float64(liveViews) / float64(result.ViewedAssets)
		result.AverageViewsPerFile = &average
	}
	result.MediaTypes, e = s.activityDimensions(ctx, day, false)
	if e != nil {
		return result, e
	}
	result.Sources, e = s.activityDimensions(ctx, day, true)
	return result, e
}

func (s *Service) activityDimensions(ctx context.Context, day string, source bool) ([]ActivityDimension, error) {
	expression := "a.media_type"
	keys := []string{"image", "animated_image", "video"}
	if source {
		expression = assetPartition
		keys = []string{"regular", "cos"}
	}
	// expression is selected only from these internal literals, never caller input.
	rows, e := s.DB.DB.QueryContext(ctx, `WITH totals AS(SELECT asset_id,sum(views) AS views,sum(plays) AS plays,sum(seconds) AS seconds FROM asset_daily_stats WHERE day>=? GROUP BY asset_id)
 SELECT `+expression+`,count(*),coalesce(sum(a.size),0),coalesce(sum(t.views),0),coalesce(sum(t.plays),0),coalesce(sum(t.seconds),0) FROM assets a JOIN libraries lib ON lib.id=a.library_id LEFT JOIN totals t ON t.asset_id=a.id WHERE a.trashed=0 GROUP BY `+expression, day)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	values := map[string]ActivityDimension{}
	for rows.Next() {
		var value ActivityDimension
		if e = rows.Scan(&value.Key, &value.Assets, &value.Bytes, &value.Views, &value.Plays, &value.Seconds); e != nil {
			return nil, e
		}
		values[value.Key] = value
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	output := make([]ActivityDimension, 0, len(keys))
	for _, key := range keys {
		value := values[key]
		value.Key = key
		output = append(output, value)
	}
	return output, nil
}

type TrendDimension struct {
	Key string `json:"key"`
	ActivityTotals
}
type TrendBucket struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
	Label string    `json:"label"`
	ActivityTotals
	MediaTypes []TrendDimension `json:"mediaTypes"`
	Sources    []TrendDimension `json:"sources"`
}
type Trends struct {
	Range       string        `json:"range"`
	Granularity string        `json:"granularity"`
	Items       []TrendBucket `json:"items"`
}

func bucketStart(value time.Time, granularity string) time.Time {
	d := time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, value.Location())
	switch granularity {
	case "week":
		return d.AddDate(0, 0, -(int(d.Weekday())+6)%7)
	case "month":
		return time.Date(d.Year(), d.Month(), 1, 0, 0, 0, 0, d.Location())
	case "quarter":
		return time.Date(d.Year(), time.Month((int(d.Month())-1)/3*3+1), 1, 0, 0, 0, 0, d.Location())
	case "year":
		return time.Date(d.Year(), 1, 1, 0, 0, 0, 0, d.Location())
	}
	return d
}
func nextBucket(value time.Time, granularity string) time.Time {
	switch granularity {
	case "week":
		return value.AddDate(0, 0, 7)
	case "month":
		return value.AddDate(0, 1, 0)
	case "quarter":
		return value.AddDate(0, 3, 0)
	case "year":
		return value.AddDate(1, 0, 0)
	}
	return value.AddDate(0, 0, 1)
}
func previousBucket(value time.Time, granularity string) time.Time {
	switch granularity {
	case "week":
		return value.AddDate(0, 0, -7)
	case "month":
		return value.AddDate(0, -1, 0)
	case "quarter":
		return value.AddDate(0, -3, 0)
	case "year":
		return value.AddDate(-1, 0, 0)
	}
	return value.AddDate(0, 0, -1)
}

func (s *Service) StatsTrends(ctx context.Context, window string) (Trends, error) {
	result := Trends{Range: window, Granularity: "day", Items: []TrendBucket{}}
	start, e := rangeStart(window)
	if e != nil {
		return result, e
	}
	now := time.Now()
	if window == "week" || window == "month" || window == "quarter" || window == "year" {
		result.Granularity = window
	}
	rows, e := s.DB.DB.QueryContext(ctx, "SELECT day,sum(views),sum(plays),sum(seconds) FROM asset_daily_stats WHERE day>=? GROUP BY day ORDER BY day", dayString(start))
	if e != nil {
		return result, e
	}
	type dayRow struct {
		day time.Time
		ActivityTotals
	}
	days := []dayRow{}
	for rows.Next() {
		var text string
		var row dayRow
		if e = rows.Scan(&text, &row.Views, &row.Plays, &row.Seconds); e != nil {
			rows.Close()
			return result, e
		}
		row.day, e = time.ParseInLocation("2006-01-02", text, time.Local)
		if e != nil {
			rows.Close()
			return result, e
		}
		days = append(days, row)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return result, e
	}
	if len(days) == 0 {
		return result, nil
	}
	if window == "all" {
		start = days[0].day
		if !start.Before(now.AddDate(0, 0, -84)) {
			result.Granularity = "week"
		} else if !start.Before(now.AddDate(0, -24, 0)) {
			result.Granularity = "month"
		} else {
			result.Granularity = "quarter"
		}
	}
	first, last := bucketStart(start, result.Granularity), bucketStart(now, result.Granularity)
	if first.Equal(last) {
		first = previousBucket(first, result.Granularity)
	}
	indexed := map[string]int{}
	for current := first; !current.After(last); current = nextBucket(current, result.Granularity) {
		if len(result.Items) >= 10000 {
			return result, Fail(422, "history_range_too_large", "历史跨度过大，请使用固定窗口。")
		}
		key := current.Format("2006-01-02")
		indexed[key] = len(result.Items)
		result.Items = append(result.Items, TrendBucket{Start: current, End: nextBucket(current, result.Granularity), Label: key,
			MediaTypes: []TrendDimension{{Key: "image"}, {Key: "animated_image"}, {Key: "video"}}, Sources: []TrendDimension{{Key: "regular"}, {Key: "cos"}}})
	}
	for _, row := range days {
		key := bucketStart(row.day, result.Granularity).Format("2006-01-02")
		index, ok := indexed[key]
		if !ok {
			continue
		}
		result.Items[index].Views += row.Views
		result.Items[index].Plays += row.Plays
		result.Items[index].Seconds += row.Seconds
	}
	e = s.trendDimensions(ctx, dayString(start), indexed, &result)
	return result, e
}

func (s *Service) trendDimensions(ctx context.Context, day string, indexed map[string]int, result *Trends) error {
	rows, e := s.DB.DB.QueryContext(ctx, `SELECT d.day,a.media_type,`+assetPartition+` AS source,sum(d.views),sum(d.plays),sum(d.seconds) FROM asset_daily_stats d JOIN assets a ON a.id=d.asset_id AND a.trashed=0 JOIN libraries lib ON lib.id=a.library_id WHERE d.day>=? GROUP BY d.day,a.media_type,source`, day)
	if e != nil {
		return e
	}
	defer rows.Close()
	add := func(values []TrendDimension, key string, total ActivityTotals) {
		for i := range values {
			if values[i].Key == key {
				values[i].Views += total.Views
				values[i].Plays += total.Plays
				values[i].Seconds += total.Seconds
				return
			}
		}
	}
	for rows.Next() {
		var text, media, source string
		var total ActivityTotals
		if e = rows.Scan(&text, &media, &source, &total.Views, &total.Plays, &total.Seconds); e != nil {
			return e
		}
		at, err := time.ParseInLocation("2006-01-02", text, time.Local)
		if err != nil {
			return err
		}
		index, ok := indexed[bucketStart(at, result.Granularity).Format("2006-01-02")]
		if !ok {
			continue
		}
		add(result.Items[index].MediaTypes, media, total)
		add(result.Items[index].Sources, source, total)
	}
	return rows.Err()
}

type ReviewHour struct {
	Hour            int   `json:"hour"`
	Views           int64 `json:"views"`
	Seconds         int64 `json:"seconds"`
	ImageSeconds    int64 `json:"imageSeconds"`
	AnimatedSeconds int64 `json:"animatedSeconds"`
	VideoSeconds    int64 `json:"videoSeconds"`
}
type InterestChange struct {
	Kind          string  `json:"kind"`
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	CurrentShare  float64 `json:"currentShare"`
	PreviousShare float64 `json:"previousShare"`
	ChangePoints  float64 `json:"changePoints"`
	CurrentViews  int64   `json:"currentViews"`
	PreviousViews int64   `json:"previousViews"`
}
type Review struct {
	Range               string           `json:"range"`
	FromDay             string           `json:"fromDay"`
	ToDay               string           `json:"toDay"`
	PreviousFromDay     string           `json:"previousFromDay"`
	PreviousToDay       string           `json:"previousToDay"`
	ViewCount           int64            `json:"viewCount"`
	PreviousViewCount   int64            `json:"previousViewCount"`
	FirstViews          int64            `json:"firstViews"`
	ReturnViews         int64            `json:"returnViews"`
	DistinctAssets      int              `json:"distinctAssets"`
	ActiveDays          int              `json:"activeDays"`
	TotalSeconds        int64            `json:"totalSeconds"`
	ImageSeconds        int64            `json:"imageSeconds"`
	AnimatedSeconds     int64            `json:"animatedSeconds"`
	VideoSeconds        int64            `json:"videoSeconds"`
	ComparisonAvailable bool             `json:"comparisonAvailable"`
	Hours               []ReviewHour     `json:"hours"`
	Changes             []InterestChange `json:"changes"`
}

func (s *Service) BrowsingReview(ctx context.Context, window string) (Review, error) {
	result := Review{Range: window, Hours: make([]ReviewHour, 24), Changes: []InterestChange{}}
	for i := range result.Hours {
		result.Hours[i].Hour = i
	}
	now := time.Now()
	start, ok := analytics.WindowStart(now, window)
	if !ok {
		return result, Fail(400, "invalid_range", "统计窗口无效。")
	}
	day := dayString(start)
	var total int64
	var e error
	today := bucketStart(now, "day")
	result.FromDay = day
	result.ToDay = dayString(today)
	e = s.DB.DB.QueryRowContext(ctx, `WITH first AS(SELECT asset_id,min(day) AS day FROM asset_daily_stats WHERE views>0 GROUP BY asset_id),activity AS(SELECT d.asset_id,d.day,d.views FROM asset_daily_stats d JOIN assets a ON a.id=d.asset_id AND a.trashed=0 WHERE d.day>=? AND d.views>0)
 SELECT coalesce(sum(views),0),count(DISTINCT asset_id),count(DISTINCT day),(SELECT count(*) FROM first JOIN assets a ON a.id=first.asset_id AND a.trashed=0 WHERE first.day>=?) FROM activity`, day, day).Scan(&total, &result.DistinctAssets, &result.ActiveDays, &result.FirstViews)
	if e != nil {
		return result, e
	}
	result.ViewCount = total
	result.ReturnViews = total - result.FirstViews
	rows, e := s.DB.DB.QueryContext(ctx, `SELECT h.hour,sum(h.views),sum(h.seconds),sum(CASE WHEN a.media_type='image' THEN h.seconds ELSE 0 END),sum(CASE WHEN a.media_type='animated_image' THEN h.seconds ELSE 0 END),sum(CASE WHEN a.media_type='video' THEN h.seconds ELSE 0 END) FROM asset_hourly_stats h JOIN assets a ON a.id=h.asset_id AND a.trashed=0 WHERE h.day>=? GROUP BY h.hour`, day)
	if e != nil {
		return result, e
	}
	for rows.Next() {
		var h ReviewHour
		if e = rows.Scan(&h.Hour, &h.Views, &h.Seconds, &h.ImageSeconds, &h.AnimatedSeconds, &h.VideoSeconds); e != nil {
			rows.Close()
			return result, e
		}
		if h.Hour >= 0 && h.Hour < 24 {
			result.Hours[h.Hour] = h
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return result, e
	}
	if e = s.DB.DB.QueryRowContext(ctx, `SELECT coalesce(sum(d.seconds),0),coalesce(sum(CASE WHEN a.media_type='image' THEN d.seconds ELSE 0 END),0),coalesce(sum(CASE WHEN a.media_type='animated_image' THEN d.seconds ELSE 0 END),0),coalesce(sum(CASE WHEN a.media_type='video' THEN d.seconds ELSE 0 END),0) FROM asset_daily_stats d JOIN assets a ON a.id=d.asset_id AND a.trashed=0 WHERE d.day>=?`, day).Scan(&result.TotalSeconds, &result.ImageSeconds, &result.AnimatedSeconds, &result.VideoSeconds); e != nil {
		return result, e
	}
	if start.IsZero() {
		return result, nil
	}
	days := 0
	for d := start; !d.After(today); d = d.AddDate(0, 0, 1) {
		days++
	}
	previous := start.AddDate(0, 0, -days)
	var previousTotal int64
	result.PreviousFromDay = dayString(previous)
	result.PreviousToDay = dayString(start.AddDate(0, 0, -1))
	e = s.DB.DB.QueryRowContext(ctx, `SELECT coalesce(sum(d.views),0) FROM asset_daily_stats d JOIN assets a ON a.id=d.asset_id AND a.trashed=0 WHERE d.day>=? AND d.day<?`, dayString(previous), day).Scan(&previousTotal)
	if e != nil {
		return result, e
	}
	result.PreviousViewCount = previousTotal
	if total == 0 || previousTotal == 0 {
		return result, nil
	}
	result.ComparisonAvailable = true
	result.Changes, e = s.interestChanges(ctx, dayString(previous), day, total, previousTotal)
	return result, e
}

func (s *Service) interestChanges(ctx context.Context, previous, current string, total, previousTotal int64) ([]InterestChange, error) {
	rows, e := s.DB.DB.QueryContext(ctx, `WITH totals AS(SELECT d.asset_id,sum(CASE WHEN d.day>=? THEN d.views ELSE 0 END) AS current,sum(CASE WHEN d.day<? THEN d.views ELSE 0 END) AS previous FROM asset_daily_stats d JOIN assets a ON a.id=d.asset_id AND a.trashed=0 WHERE d.day>=? GROUP BY d.asset_id), interests AS(
 SELECT 'author' AS kind,au.id,au.name,sum(t.current) AS current,sum(t.previous) AS previous FROM totals t JOIN asset_authors aa ON aa.asset_id=t.asset_id JOIN authors au ON au.id=aa.author_id GROUP BY au.id
 UNION ALL SELECT 'tag',at.tag,at.tag,sum(t.current),sum(t.previous) FROM totals t JOIN asset_tag_names at ON at.asset_id=t.asset_id GROUP BY at.tag), scored AS(SELECT *,100.0*current/?-100.0*previous/? AS change FROM interests)
 SELECT kind,id,name,current,previous FROM scored WHERE change!=0 ORDER BY abs(change) DESC,kind,id LIMIT 6`, current, current, previous, total, previousTotal)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	changes := []InterestChange{}
	for rows.Next() {
		var value InterestChange
		if e = rows.Scan(&value.Kind, &value.ID, &value.Name, &value.CurrentViews, &value.PreviousViews); e != nil {
			return nil, e
		}
		value.CurrentShare = float64(value.CurrentViews) / float64(total) * 100
		value.PreviousShare = float64(value.PreviousViews) / float64(previousTotal) * 100
		value.ChangePoints = value.CurrentShare - value.PreviousShare
		if value.ChangePoints != 0 {
			changes = append(changes, value)
		}
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	return changes, nil
}
