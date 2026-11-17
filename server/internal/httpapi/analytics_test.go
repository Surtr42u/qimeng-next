package httpapi_test

import (
 "net/url"
 "testing"

 "qimeng.local/next/server/internal/model"
 "qimeng.local/next/server/internal/service"
)

func TestActivityProjectionsRecommendationPaginationAndPreferenceConflict(t *testing.T){
 f:=setup(t);var assets []model.Asset
 for index,shade:=range []uint8{11,45,76}{u,_:=f.upload([]string{"alpha.png","beta.png","gamma.png"}[index],shade,nil);result:=f.complete(u);assets=append(assets,*result.Asset)}
 first:=assets[0]
 for _,body:=range []map[string]any{
  {"eventId":"synthetic-view-1","sessionId":"synthetic-session","kind":"view","contentVersion":first.ContentVersion},
  {"eventId":"synthetic-view-2","sessionId":"synthetic-session","kind":"view","contentVersion":first.ContentVersion},
  {"eventId":"synthetic-dwell-1","sessionId":"synthetic-session","kind":"dwell","seconds":12.9,"contentVersion":first.ContentVersion},
 }{f.json("POST","/assets/"+first.ID+"/events","",body,200)}
 var overview service.Overview;decode(t,f.json("GET","/stats/overview?range=7d","",nil,200),&overview)
 if overview.Totals.Views!=1||overview.Totals.Seconds!=12||overview.ViewedAssets!=1||overview.AverageViewsPerFile==nil||*overview.AverageViewsPerFile!=1{t.Fatalf("bad activity: %+v",overview)}
 var review service.Review;decode(t,f.json("GET","/stats/review?range=7d","",nil,200),&review)
 if review.FirstViews!=1||review.ReturnViews!=0||review.TotalSeconds!=12||len(review.Hours)!=24{t.Fatalf("bad review: %+v",review)}
 var recommendation service.RecommendationPage;decode(t,f.json("GET","/recommendations?seed=42&limit=1","",nil,200),&recommendation)
 seen:=map[string]bool{};for{for _,asset:=range recommendation.Items{if seen[asset.ID]{t.Fatal("recommendation page repeated an asset")};seen[asset.ID]=true};if recommendation.NextCursor==""{break};cursor:=recommendation.NextCursor;recommendation=service.RecommendationPage{};decode(t,f.json("GET","/recommendations?seed=42&limit=1&cursor="+url.QueryEscape(cursor),"",nil,200),&recommendation)}
 if len(seen)!=3{t.Fatalf("recommendation omitted input: %d",len(seen))}
 var preference service.RecommendationPreferences;decode(t,f.json("GET","/preferences/recommendation","",nil,200),&preference)
 body:=map[string]any{"expectedRevision":preference.Revision,"weights":preference.Presets["explore"]}
 f.json("PUT","/preferences/recommendation","preference-operation-one",body,200)
 f.json("PUT","/preferences/recommendation","preference-operation-one",body,200)
 f.json("PUT","/preferences/recommendation","preference-operation-two",body,409)
 var ranking model.Page[service.RankedAsset];decode(t,f.json("GET","/rankings?period=week&mediaType=image","",nil,200),&ranking)
 if len(ranking.Items)!=1||ranking.Items[0].Asset.ID!=first.ID||ranking.Items[0].Score!=1{t.Fatalf("bad ranking: %+v",ranking)}
}

func TestActivityRankingsAndSharedTrendDimensions(t *testing.T){
 f:=setup(t);firstUpload,_:=f.upload("rank-a.png",19,nil);first:=f.complete(firstUpload).Asset
 secondUpload,_:=f.upload("rank-b.png",65,nil);second:=f.complete(secondUpload).Asset
 var author model.Author;decode(t,f.json("POST","/authors","rank-author-create",map[string]any{"name":"Rank author","kind":"regular"},201),&author)
 for _,asset:=range []*model.Asset{first,second}{
  f.json("PATCH","/assets/"+asset.ID,"rank-authors-"+asset.ID,map[string]any{"expectedRevision":asset.Revision,"authorIds":[]string{author.ID}},200)
  f.json("PUT","/assets/"+asset.ID+"/tags","rank-tags-"+asset.ID,map[string]any{"expectedRevision":asset.TagsRevision,"tags":[]string{"共享标签"}},200)
  f.json("POST","/assets/"+asset.ID+"/events","",map[string]any{"eventId":"rank-view-"+asset.ID,"sessionId":"rank-session-"+asset.ID,"kind":"view","contentVersion":asset.ContentVersion},200)
 }
 f.json("POST","/assets/"+first.ID+"/events","",map[string]any{"eventId":"rank-dwell-synthetic","kind":"dwell","seconds":9.9,"contentVersion":first.ContentVersion},200)
 for _,route:=range []string{"top-authors","top-tags"}{var page service.ActivityRankingPage;decode(t,f.json("GET","/stats/"+route+"?range=7d","",nil,200),&page);if len(page.Items)!=1||page.Items[0].Views!=2||page.Items[0].Assets!=2||page.Items[0].Seconds!=9{t.Fatalf("%s aggregate: %+v",route,page)}}
 var dwell service.ActivityRankingPage;decode(t,f.json("GET","/stats/most-viewed?range=7d&metric=seconds","",nil,200),&dwell);if len(dwell.Items)!=1||dwell.Items[0].Asset==nil||dwell.Items[0].ID!=first.ID||dwell.Items[0].Value!=9{t.Fatalf("dwell ranking: %+v",dwell)}
 var page service.ActivityRankingPage;decode(t,f.json("GET","/stats/most-viewed?range=7d&limit=1","",nil,200),&page);if page.NextCursor==""{t.Fatal("missing ranking cursor")};firstID:=page.Items[0].ID;cursor:=page.NextCursor;page=service.ActivityRankingPage{}
 decode(t,f.json("GET","/stats/most-viewed?range=7d&limit=1&cursor="+url.QueryEscape(cursor),"",nil,200),&page);if len(page.Items)!=1||page.Items[0].ID==firstID||page.NextCursor!=""{t.Fatal("ranking page repeated/skipped")}
 f.json("GET","/stats/most-viewed?range=90d&cursor="+url.QueryEscape(cursor),"",nil,400)
 var trends service.Trends;decode(t,f.json("GET","/stats/trends?range=7d","",nil,200),&trends);var views,seconds int64
 for _,bucket:=range trends.Items{if len(bucket.MediaTypes)!=3||len(bucket.Sources)!=2{t.Fatal("missing dimension zeros")};views+=bucket.MediaTypes[0].Views;seconds+=bucket.Sources[0].Seconds}
 if views!=2||seconds!=9{t.Fatalf("same-axis projections: %d %d",views,seconds)}
}
