package httpapi_test

import (
 "net/url"
 "testing"

 "qimeng.local/next/server/internal/model"
 "qimeng.local/next/server/internal/service"
)

func TestPartitionUsesCOSAuthorMembershipAcrossBrowseFacetsAndActivity(t *testing.T) {
 f:=setup(t)
 regular:=createAuthor(t,f,"Additional regular creator")
 var library model.Library
 decode(t,f.json("POST","/libraries","partition-cos-library",map[string]any{"name":"Synthetic COS","kind":"cos"},201),&library)
 f.library=library.ID
 rootInput,_:=f.upload("unclassified-root.png",211,nil)
 root:=*f.complete(rootInput).Asset
 cosInput,_:=f.upload("mixed-cos.png",212,map[string]any{"relativeDirectory":"Directory creator/Work"})
 cos:=*f.complete(cosInput).Asset
 fixed:=cos.COSDirectoryAuthorID
 decode(t,f.json("PATCH","/assets/"+cos.ID,"partition-add-regular",map[string]any{"expectedRevision":cos.Revision,"expectedAuthorsRevision":cos.AuthorsRevision,"authorIds":[]string{regular.ID}},200),&cos)
 if len(cos.WorkKeys)!=1||cos.WorkKeys[0]!="author:"+fixed||len(cos.WorkFacets)!=1||cos.WorkFacets[0].ID!=fixed {t.Fatal("additional regular author leaked into COS work facets")}
 if len(root.WorkKeys)!=1||root.WorkKeys[0]!="source:__other__" {t.Fatal("unclassified root file acquired an invented COS identity")}
 for _,check:=range []struct{query,id string}{{"?kind=regular",root.ID},{"?kind=cos",cos.ID},{"?workKeys=source:__other__",root.ID},{"?workKeys=author:"+fixed,cos.ID}} {
  page:=assetsAt(t,f,check.query)
  if len(page.Items)!=1||page.Items[0].ID!=check.id {t.Fatalf("partition disagreement for %s",check.query)}
 }
 if len(assetsAt(t,f,"?workKeys=author:"+regular.ID).Items)!=0 {t.Fatal("a regular author key was accepted as a COS facet")}
 var facets model.Page[service.AssetFacet]
 decode(t,f.json("GET","/assets/facets?row=works","",nil,200),&facets)
 if len(facets.Items)!=2 {t.Fatal("facets lost a partition or invented a regular COS author")}
 for _,a:=range []model.Asset{root,cos} {f.json("POST","/assets/"+a.ID+"/events","",map[string]any{"eventId":"partition-view-"+a.ID,"sessionId":"partition-session-"+a.ID,"kind":"view","contentVersion":a.ContentVersion},200)}
 var overview service.Overview
 decode(t,f.json("GET","/stats/overview?range=7d","",nil,200),&overview)
 for _,source:=range overview.Sources {if source.Assets!=1||source.Views!=1 {t.Fatalf("statistics used the library kind: %+v",source)}}
 for _,check:=range []struct{kind,id string}{{"regular",root.ID},{"cos",cos.ID}} {
  var rec service.RecommendationPage
  decode(t,f.json("GET","/recommendations?kind="+check.kind,"",nil,200),&rec)
  if len(rec.Items)!=1||rec.Items[0].ID!=check.id {t.Fatal("recommendation partition differs from browsing")}
  var ranking model.Page[service.RankedAsset]
  decode(t,f.json("GET","/rankings?period=all&kind="+check.kind,"",nil,200),&ranking)
  if len(ranking.Items)!=1||ranking.Items[0].Asset.ID!=check.id {t.Fatal("ranking partition differs from browsing")}
 }
}

func TestRecommendationContinuationRechecksEnabledLibrary(t *testing.T) {
 f:=setup(t)
 for index,name:=range []string{"eligible-one.png","eligible-two.png"} {u,_:=f.upload(name,uint8(216+index),nil);f.complete(u)}
 var first service.RecommendationPage
 decode(t,f.json("GET","/recommendations?kind=regular&limit=1","",nil,200),&first)
 if first.NextCursor=="" {t.Fatal("missing continuation fixture")}
 f.json("PATCH","/libraries/"+f.library,"disable-after-recommendation",map[string]any{"expectedRevision":1,"enabled":false},200)
 var next service.RecommendationPage
 decode(t,f.json("GET","/recommendations?kind=regular&limit=1&cursor="+url.QueryEscape(first.NextCursor),"",nil,200),&next)
 if len(next.Items)!=0 {t.Fatal("continuation disclosed assets from a disabled library")}
}

func TestImportedLikeBaselineAffectsLifetimeButNotInventedWindows(t *testing.T) {
 f:=setup(t);u,_:=f.upload("unattributed-ranking.png",219,nil);a:=*f.complete(u).Asset
 commitLikeImport(t,f,a,"unattributed-ranking",17,nil)
 for _,period:=range []string{"day","week","month","all"} {
  var ranking model.Page[service.RankedAsset]
  decode(t,f.json("GET","/rankings?period="+period,"",nil,200),&ranking)
  if period=="all" {if len(ranking.Items)!=1||ranking.Items[0].Score!=17 {t.Fatal("lifetime likes were lost from ranking")}} else if len(ranking.Items)!=0 {t.Fatal("unattributed likes became fabricated window activity")}
 }
}
