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
 seen:=map[string]bool{};for{for _,asset:=range recommendation.Items{if seen[asset.ID]{t.Fatal("recommendation page repeated an asset")};seen[asset.ID]=true};if recommendation.NextCursor==""{break};decode(t,f.json("GET","/recommendations?seed=42&limit=1&cursor="+url.QueryEscape(recommendation.NextCursor),"",nil,200),&recommendation)}
 if len(seen)!=3{t.Fatalf("recommendation omitted input: %d",len(seen))}
 var preference service.RecommendationPreferences;decode(t,f.json("GET","/preferences/recommendation","",nil,200),&preference)
 body:=map[string]any{"expectedRevision":preference.Revision,"weights":preference.Presets["explore"]}
 f.json("PUT","/preferences/recommendation","preference-operation-one",body,200)
 f.json("PUT","/preferences/recommendation","preference-operation-one",body,200)
 f.json("PUT","/preferences/recommendation","preference-operation-two",body,409)
 var ranking model.Page[service.RankedAsset];decode(t,f.json("GET","/rankings?period=week&mediaType=image","",nil,200),&ranking)
 if len(ranking.Items)!=1||ranking.Items[0].Asset.ID!=first.ID||ranking.Items[0].Score!=1{t.Fatalf("bad ranking: %+v",ranking)}
}
