package httpapi_test

import (
 "net/url"
 "strconv"
 "testing"
 "time"

 "qimeng.local/next/server/internal/model"
 "qimeng.local/next/server/internal/service"
)

func createAuthor(t *testing.T,f *fixture,name string)model.Author{t.Helper();r:=f.json("POST","/authors","author-query-"+name,map[string]any{"name":name},201);var author model.Author;decode(t,r,&author);return author}
func assetsAt(t *testing.T,f *fixture,query string)model.Page[model.Asset]{t.Helper();r:=f.json("GET","/assets"+query,"",nil,200);var page model.Page[model.Asset];decode(t,r,&page);if page.TotalMatched==nil{t.Fatal("asset response omitted matching count")};return page}

func TestBrowseUsesOnePredicateForSearchMultipleDimensionsAndFacets(t *testing.T){
 f:=setup(t);alpha:=createAuthor(t,f,"Alpha");beta:=createAuthor(t,f,"Beta");gamma:=createAuthor(t,f,"Gamma")
 r:=f.json("POST","/sources","query-source-one",map[string]any{"name":"原作甲"},201);var first service.Source;decode(t,r,&first);r=f.json("POST","/sources","query-source-two",map[string]any{"name":"原作乙"},201);var second service.Source;decode(t,r,&second)
 r=f.json("POST","/characters","query-character-one",map[string]any{"name":"朱诺","sourceId":first.ID},201);var character service.Character;decode(t,r,&character)
 u,_:=f.upload("朱诺 11 (2).png",81,map[string]any{"authorIds":[]string{alpha.ID,beta.ID},"sourceIds":[]string{first.ID},"characterIds":[]string{character.ID}});a:=*f.complete(u).Asset
 u,_=f.upload("朱诺 110.png",82,map[string]any{"authorIds":[]string{beta.ID},"sourceIds":[]string{second.ID}});b:=*f.complete(u).Asset
 u,_=f.upload("朱诺 2011.png",83,map[string]any{"authorIds":[]string{gamma.ID},"sourceIds":[]string{first.ID}});_ = f.complete(u)
 for _,test:=range []struct{q string;count int64}{{"朱诺 11",1},{"朱诺 2",0},{"朱诺 【2】",1},{"朱诺",3}}{page:=assetsAt(t,f,"?q="+url.QueryEscape(test.q));if *page.TotalMatched!=test.count||int64(len(page.Items))!=test.count{t.Fatalf("query %q count=%d rows=%d",test.q,*page.TotalMatched,len(page.Items))}}
 page:=assetsAt(t,f,"?authorIds="+alpha.ID+"&authorIds="+gamma.ID+"&sourceIds="+first.ID);if *page.TotalMatched!=2{t.Fatal("OR within authors and AND with source were not preserved")}
 page=assetsAt(t,f,"?authorIds="+beta.ID+"&sourceIds="+first.ID+"&sourceIds="+second.ID);if *page.TotalMatched!=2{t.Fatal("source multi-selection is not OR")}
 f.json("PUT","/assets/"+a.ID+"/tags","query-tags-both",map[string]any{"expectedRevision":a.TagsRevision,"tags":[]string{"one","two"}},200);f.json("PUT","/assets/"+b.ID+"/tags","query-tags-one",map[string]any{"expectedRevision":b.TagsRevision,"tags":[]string{"one"}},200)
 if *assetsAt(t,f,"?tags=one&tags=two&tagMode=all").TotalMatched!=1||*assetsAt(t,f,"?tags=one&tags=two&tagMode=any").TotalMatched!=2{t.Fatal("tag modes diverged")}
 if *assetsAt(t,f,"?tag=two&tags=one&tags=missing&tagMode=any").TotalMatched!=1{t.Fatal("fixed tag route became an optional OR term")}
 r=f.json("GET","/assets/facets?row=works&tag=two&tags=one&tags=missing","",nil,200);var constrained model.Page[service.AssetFacet];decode(t,r,&constrained);if constrained.TotalMatched==nil||*constrained.TotalMatched!=1{t.Fatal("facet did not preserve the fixed tag constraint")}
 r=f.json("GET","/assets/facets?row=works&authorId="+beta.ID,"",nil,200);var facets model.Page[service.AssetFacet];decode(t,r,&facets);if facets.TotalMatched==nil||*facets.TotalMatched!=2||facets.TotalOptions==nil||*facets.TotalOptions!=2||len(facets.Items)!=2{t.Fatal("facet ignored fixed author or counted a different subset")};for _,facet:=range facets.Items{if facet.Kind!="source"||facet.Count!=1{t.Fatal("facet counted incompatible source dimensions")}}
 r=f.json("GET","/assets/facets?row=works&authorId="+beta.ID+"&limit=1","",nil,200);decode(t,r,&facets);if len(facets.Items)!=1||*facets.TotalOptions!=2||facets.NextCursor==""{t.Fatal("paginated facet used loaded length as total options")}
 r=f.json("GET","/authors?kind=regular&sort=files","",nil,200);var authors model.Page[model.Author];decode(t,r,&authors);if authors.Items[0].ID!=beta.ID||authors.Items[0].FileCount!=2{t.Fatal("author file sorting was not server-derived")}
 r=f.json("GET","/search/suggestions?q="+url.QueryEscape("朱诺"),"",nil,200);var suggestions struct{Items []service.SearchSuggestion `json:"items"`};decode(t,r,&suggestions);if len(suggestions.Items)!=1||suggestions.Items[0].Type!="character"{t.Fatal("search suggestions did not use actual character vocabulary")}
}

func TestDateCountsUseRequestedLocalDayAndExcludeCursorFromWholeCounts(t *testing.T){
 f:=setup(t);for index,hour:=range []int{15,17}{mtime:=time.Date(2025,1,1,hour,0,0,0,time.UTC);u,_:=f.upload("local-date-"+strconv.Itoa(index)+".png",uint8(120+index),map[string]any{"sourceMtime":mtime});_ = f.complete(u)}
 first:=assetsAt(t,f,"?dateCounts=true&tzOffsetMinutes=480&limit=1");if len(first.Items)!=1||*first.TotalMatched!=2||first.DateCounts==nil||len(*first.DateCounts)!=2{t.Fatal("local day counts were based on loaded items or UTC days")};if (*first.DateCounts)[0].Date!="2025-01-02"||(*first.DateCounts)[0].FileCount!=1||(*first.DateCounts)[1].Date!="2025-01-01"{t.Fatal("east-of-UTC offset used the wrong sign")}
 next:=assetsAt(t,f,"?dateCounts=true&tzOffsetMinutes=480&limit=1&cursor="+first.NextCursor);if next.DateCounts!=nil||len(next.Items)!=1{t.Fatal("continuation recalculated day buckets or broke the stable cursor")}
 utc:=assetsAt(t,f,"?dateCounts=true");if utc.DateCounts==nil||len(*utc.DateCounts)!=1||(*utc.DateCounts)[0].FileCount!=2{t.Fatal("default date-count offset was not UTC")}
 r:=f.json("GET","/assets/facets?row=partitions","",nil,200);var facets model.Page[service.AssetFacet];decode(t,r,&facets);if len(facets.Items)!=3||facets.Items[0].ID!="all"||facets.Items[1].ID!="regular"||facets.Items[2].ID!="cos"{t.Fatal("fixed partition order changed with counts")}
}

func TestDirectoryFilesAreExactAndDisabledLibrariesStayBrowsableOnlyExplicitly(t *testing.T){
 f:=setup(t);u,_:=f.upload("root-file.png",124,nil);root:=*f.complete(u).Asset;u,_=f.upload("direct.png",125,map[string]any{"relativeDirectory":"目录%"});_ = f.complete(u);u,_=f.upload("nested.png",126,map[string]any{"relativeDirectory":"目录%/子目录"});_ = f.complete(u)
 page:=assetsAt(t,f,"?libraryId="+f.library+"&directory=");if *page.TotalMatched!=1||page.Items[0].ID!=root.ID{t.Fatal("empty directory did not mean exact library root")}
 query:="?libraryId="+f.library+"&directory="+url.QueryEscape("目录%");page=assetsAt(t,f,query);if *page.TotalMatched!=1||page.Items[0].FileName!="direct.png"{t.Fatal("directory matching treated Unicode length or percent as a wildcard")};if *assetsAt(t,f,query+"&recursive=true").TotalMatched!=2{t.Fatal("explicit recursive directory omitted descendants")}
 f.json("GET","/assets?directory=","",nil,400);f.json("GET","/assets?libraryId="+f.library+"&directory=../outside","",nil,400)
 f.json("PATCH","/libraries/"+f.library,"disable-library-visibility",map[string]any{"expectedRevision":1,"enabled":false},200);if *assetsAt(t,f,"").TotalMatched!=0||*assetsAt(t,f,"?includeDisabled=true").TotalMatched!=3{t.Fatal("disabled library visibility was inconsistent")};f.json("GET","/assets/"+root.ID,"",nil,200)
 r:=f.json("GET","/stats","",nil,200);var stats map[string]int64;decode(t,r,&stats);if stats["assets"]!=3{t.Fatal("disabling a library erased inventory counts")}
}

func TestAssetAndAuthorKeysetSurviveInsertionAndDeletionBeforeCursor(t *testing.T){
 f:=setup(t);byName:=map[string]model.Asset{}
 for index,name:=range []string{"page-A.png","page-B.png","page-C.png","page-D.png","page-E.png"}{u,_:=f.upload(name,uint8(90+index),nil);byName[name]=*f.complete(u).Asset}
 first:=assetsAt(t,f,"?sort=name&order=asc&limit=2");if first.Items[0].FileName!="page-A.png"||first.NextCursor==""{t.Fatal("unexpected first page")};a:=byName["page-A.png"];f.json("DELETE","/assets/"+a.ID+"?expectedRevision="+strconv.FormatInt(a.Revision,10),"delete-before-cursor",struct{}{},200);u,_:=f.upload("page-0.png",99,nil);_ = f.complete(u)
 second:=assetsAt(t,f,"?sort=name&order=asc&limit=2&cursor="+first.NextCursor);if len(second.Items)!=2||second.Items[0].FileName!="page-C.png"||second.Items[1].FileName!="page-D.png"{t.Fatal("offset shifts duplicated or skipped surviving assets")};last:=assetsAt(t,f,"?sort=name&order=asc&limit=2&cursor="+second.NextCursor);if len(last.Items)!=1||last.Items[0].FileName!="page-E.png"{t.Fatal("keyset continuation lost final asset")}
 f.json("GET","/assets?sort=name&order=asc&favorite=true&cursor="+first.NextCursor,"",nil,400)
 aa:=createAuthor(t,f,"Aaa");_ = createAuthor(t,f,"Bbb");cc:=createAuthor(t,f,"Ccc");dd:=createAuthor(t,f,"Ddd");r:=f.json("GET","/authors?limit=2","",nil,200);var authors model.Page[model.Author];decode(t,r,&authors);cursor:=authors.NextCursor;f.json("DELETE","/authors/"+aa.ID+"?expectedRevision=1","delete-author-before-cursor",struct{}{},204);_ = createAuthor(t,f,"Abc");r=f.json("GET","/authors?limit=2&cursor="+cursor,"",nil,200);decode(t,r,&authors);if len(authors.Items)!=2||authors.Items[0].ID!=cc.ID||authors.Items[1].ID!=dd.ID{t.Fatal("author pagination still shifted by offsets")}
}

func TestDefaultDateFavoriteTimeAndHistoryUseServerFacts(t *testing.T){
 f:=setup(t);oldDate:=time.Date(2024,1,1,12,0,0,0,time.UTC);newDate:=oldDate.AddDate(1,0,0)
 u,_:=f.upload("newer-file.png",101,map[string]any{"sourceMtime":newDate});newer:=*f.complete(u).Asset;u,_=f.upload("later-import.png",102,map[string]any{"sourceMtime":oldDate});older:=*f.complete(u).Asset
 page:=assetsAt(t,f,"");if page.Items[0].ID!=newer.ID{t.Fatal("default sorting used added time instead of file date")}
 r:=f.json("PUT","/assets/"+older.ID+"/reaction","favorite-first",map[string]any{"favorite":true,"liked":false},200);decode(t,r,&older);originalTime:=*older.FavoritedAt
 f.json("PUT","/assets/"+newer.ID+"/reaction","favorite-second",map[string]any{"favorite":true,"liked":false},200);r=f.json("PUT","/assets/"+older.ID+"/reaction","favorite-repeat",map[string]any{"favorite":true,"liked":false},200);decode(t,r,&older);if !older.FavoritedAt.Equal(originalTime){t.Fatal("repeated favorite changed favorite time")};page=assetsAt(t,f,"?favorite=true&sort=favorited");if page.Items[0].ID!=newer.ID{t.Fatal("favorite order used generic asset update time")}
 for index,id:=range []string{older.ID,newer.ID,newer.ID}{f.json("POST","/assets/"+id+"/events","",map[string]any{"eventId":"history-view-"+strconv.Itoa(index),"kind":"view","sessionId":"history-session-"+strconv.Itoa(index),"contentVersion":1,"occurredAt":time.Date(2025,1,1,12,index,0,0,time.UTC)},200)}
 r=f.json("GET","/history?limit=1","",nil,200);var history model.Page[model.Event];decode(t,r,&history);if history.TotalMatched==nil||*history.TotalMatched!=2||len(history.Items)!=1||history.Items[0].Asset==nil||history.Items[0].Asset.ID!=newer.ID{t.Fatal("history did not return one latest view and its actual asset")};r=f.json("GET","/history?limit=1&cursor="+history.NextCursor,"",nil,200);decode(t,r,&history);if len(history.Items)!=1||history.Items[0].AssetID!=older.ID{t.Fatal("history cursor did not reach the other asset")}
}
