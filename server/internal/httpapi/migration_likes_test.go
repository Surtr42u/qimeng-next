package httpapi_test

import (
 "testing"
 "time"

 "qimeng.local/next/server/internal/model"
 "qimeng.local/next/server/internal/service"
)

func commitLikeImport(t *testing.T,f *fixture,a model.Asset,key string,count int64,day any)service.V2MigrationPlan{t.Helper();input:=stageV2(t,f,"stage-"+key,v2Document(t,a,map[string]any{"likes":[]any{map[string]any{"recordKey":"source-record","likeCount":count,"lastLikeDate":day}}}));plan:=planV2(t,f,input,"plan-"+key);if plan.SkippedCount!=0{t.Fatal("like fixture unexpectedly contains skipped media")};r:=f.json("POST","/migration/v2/plans/"+plan.ID+"/commit","commit-"+key,map[string]any{"fingerprint":plan.Fingerprint,"acceptSkipped":false},200);decode(t,r,&plan);return plan}
func TestV2LikesKeepLifetimeBaselineAndTodayDeduplication(t *testing.T){
 f:=setup(t);u,_:=f.upload("like-baseline.png",203,nil);a:=*f.complete(u).Asset;today:=time.Now().Format("2006-01-02");plan:=commitLikeImport(t,f,a,"lifetime-today",27,today);if plan.Counts["legacyLikesPreserved"]!=27{t.Fatal("legacy lifetime count was not recorded")}
 r:=f.json("GET","/assets/"+a.ID,"",nil,200);decode(t,r,&a);if a.LikeCount!=27||a.UnattributedLikeCount!=26||!a.Liked{t.Fatalf("legacy lifetime/today facts changed: %+v",a)}
 r=f.json("PUT","/assets/"+a.ID+"/reaction","same-day-after-migration",map[string]any{"favorite":false,"liked":true},200);decode(t,r,&a);if a.LikeCount!=27{t.Fatal("known last-day vote was counted twice")}
 commitLikeImport(t,f,a,"lifetime-repeat",27,today);r=f.json("GET","/assets/"+a.ID,"",nil,200);decode(t,r,&a);if a.LikeCount!=27{t.Fatal("reimport added the baseline again")}
}
func TestV2LikesKeepUnknownDaysWithoutInventingHistory(t *testing.T){
 f:=setup(t);u,_:=f.upload("undated-baseline.png",204,nil);a:=*f.complete(u).Asset;commitLikeImport(t,f,a,"undated-lifetime",13,nil)
 r:=f.json("GET","/assets/"+a.ID,"",nil,200);decode(t,r,&a);if a.LikeCount!=13||a.UnattributedLikeCount!=13||a.Liked{t.Fatal("unknown daily history was fabricated")};var rows int;if e:=f.s.DB.DB.QueryRow("SELECT count(*) FROM likes WHERE asset_id=?",a.ID).Scan(&rows);e!=nil||rows!=0{t.Fatal("aggregate was expanded into fake daily rows")}
 f.json("PUT","/assets/"+a.ID+"/reaction","unknown-day-conservative-vote",map[string]any{"favorite":false,"liked":true},409)
 yesterday:=time.Now().AddDate(0,0,-1).Format("2006-01-02");if _,e:=f.s.DB.DB.Exec("UPDATE like_baselines SET observed_day=? WHERE asset_id=?",yesterday,a.ID);e!=nil{t.Fatal(e)}
 r=f.json("PUT","/assets/"+a.ID+"/reaction","new-day-after-undated-migration",map[string]any{"favorite":false,"liked":true},200);decode(t,r,&a);if a.LikeCount!=14||!a.Liked{t.Fatal("new local vote did not increment preserved baseline")}
}
func TestV2LikesUnknownOverlapBecomesConflictAndKeepsBothEvidence(t *testing.T){
 f:=setup(t);u,_:=f.upload("overlap-baseline.png",205,nil);a:=*f.complete(u).Asset
 if _,e:=f.s.DB.DB.Exec("INSERT INTO likes(asset_id,day,created_at) VALUES(?,?,?)",a.ID,"2025-01-01","2025-01-01T00:00:00Z");e!=nil{t.Fatal(e)}
 plan:=commitLikeImport(t,f,a,"overlap-lifetime",9,"2025-02-01");if plan.State!="partial"||plan.Counts["likeConflicts"]!=1{t.Fatal("overlapping aggregates were silently merged")}
 r:=f.json("GET","/assets/"+a.ID,"",nil,200);decode(t,r,&a);if a.LikeCount!=1||a.UnattributedLikeCount!=0{t.Fatal("target votes were overwritten")};var archived int;if e:=f.s.DB.DB.QueryRow("SELECT count(*) FROM migration_relation_origins WHERE kind='like' AND json_extract(original,'$.likeCount')=9").Scan(&archived);e!=nil||archived!=1{t.Fatal("source conflict evidence was discarded")}
}
