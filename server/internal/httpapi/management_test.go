package httpapi_test

import (
 "os"
 "path"
 "strconv"
 "testing"

 "qimeng.local/next/server/internal/model"
 "qimeng.local/next/server/internal/service"
)

func TestMultipleAuthorsAndGlobalTagDeletionProtectStaleEdits(t *testing.T){
 f:=setup(t);r:=f.json("POST","/authors","create-author-alpha",map[string]any{"name":"Synthetic Alpha","aliases":[]string{}},201);var first model.Author;decode(t,r,&first);r=f.json("POST","/authors","create-author-beta",map[string]any{"name":"Synthetic Beta","aliases":[]string{}},201);var second model.Author;decode(t,r,&second)
 u,_:=f.upload("authors.png",70,map[string]any{"authorIds":[]string{first.ID,"unknown",second.ID}});a:=*f.complete(u).Asset;if len(a.AuthorIDs)!=2{t.Fatalf("placeholder or author lost: %v",a.AuthorIDs)}
 r=f.json("PUT","/assets/"+a.ID+"/tags","global-tag-association",map[string]any{"expectedRevision":a.TagsRevision,"tags":[]string{"removable"}},200);decode(t,r,&a);oldTagRevision:=a.TagsRevision
 r=f.json("GET","/tag-vocabulary","",nil,200);var page model.Page[service.Tag];decode(t,r,&page);if len(page.Items)!=1{t.Fatal("tag missing from vocabulary")}
 f.json("DELETE","/tags/removable?expectedRevision="+strconv.FormatInt(page.Items[0].Revision,10),"global-tag-delete",struct{}{},204)
 f.json("PUT","/assets/"+a.ID+"/tags","global-tag-stale",map[string]any{"expectedRevision":oldTagRevision,"tags":[]string{"removable"}},409)
 other,_:=f.upload("unassociated.png",71,nil);untagged:=*f.complete(other).Asset
 f.json("PUT","/assets/"+untagged.ID+"/tags","global-tag-stale-unassociated",map[string]any{"expectedRevision":untagged.TagsRevision,"tags":[]string{"removable"}},409)
 f.json("DELETE","/authors/"+first.ID+"?expectedRevision="+strconv.FormatInt(first.Revision,10),"delete-first-author",struct{}{},204)
 r=f.json("GET","/assets/"+a.ID,"",nil,200);decode(t,r,&a);if len(a.AuthorIDs)!=1||a.AuthorIDs[0]!=second.ID||len(a.Tags)!=0{t.Fatal("author deletion changed unrelated data")}
}
func TestStableTaxonomyAndExplicitChannelSeparation(t *testing.T){
 f:=setup(t);r:=f.json("POST","/sources","source-create-1",map[string]any{"name":"Synthetic Work","aliases":[]string{"SW"}},201);var source service.Source;decode(t,r,&source)
 r=f.json("POST","/characters","character-create-1",map[string]any{"name":"Synthetic Character","sourceId":source.ID,"aliases":[]string{"SC"}},201);var character service.Character;decode(t,r,&character)
 u,_:=f.upload("taxonomy.png",90,map[string]any{"characterIds":[]string{character.ID},"acquisitionSources":[]string{"Synthetic channel"}});a:=*f.complete(u).Asset;if len(a.SourceIDs)!=1||a.SourceIDs[0]!=source.ID||a.Source!="Synthetic Work"||a.AcquisitionSources[0]!="Synthetic channel"{t.Fatal("taxonomy confused identity with acquisition")}
 f.json("PUT","/sources/"+source.ID,"source-rename-1",map[string]any{"name":"Renamed Work","aliases":source.Aliases,"expectedRevision":source.Revision},200)
 r=f.json("GET","/assets/"+a.ID,"",nil,200);decode(t,r,&a);if a.Source!="Renamed Work"||a.CharacterIDs[0]!=character.ID{t.Fatal("stable taxonomy display did not follow rename")}
 r=f.json("PATCH","/libraries/"+f.library,"disable-library-1",map[string]any{"expectedRevision":1,"enabled":false},200);var library model.Library;decode(t,r,&library);if library.Enabled{t.Fatal("library stayed enabled")}
 f.json("POST","/uploads","upload-disabled-library",map[string]any{"libraryId":f.library,"fileName":"blocked.png","size":100,"mimeType":"image/png","mode":"ordinary"},409)
}

func TestPlanContinuesIndependentInputsAfterPathConflict(t *testing.T){
 f:=setup(t);r:=f.json("POST","/batches","partial-batch-create",map[string]any{"libraryId":f.library,"mode":"intelligent","label":"Partial synthetic batch","maxResearchCalls":0},201);var batch model.Batch;decode(t,r,&batch)
 inputs:=f.declareInputs(batch.ID,"blocked-input.png","working-input.png")
 first,_:=f.upload("blocked-input.png",110,map[string]any{"mode":"intelligent","batchId":batch.ID,"inputId":inputs["blocked-input.png"]});f.complete(first)
 second,_:=f.upload("working-input.png",120,map[string]any{"mode":"intelligent","batchId":batch.ID,"inputId":inputs["working-input.png"]});f.complete(second)
 r=f.json("GET","/batches/"+batch.ID,"",nil,200);var before model.BatchDetail;decode(t,r,&before)
 r=f.json("POST","/batches/"+batch.ID+"/plans","prepare-partial-plan",map[string]any{"expectedRevision":before.Batch.Revision,"items":[]any{map[string]any{"uploadId":first.ID,"family":"Blocked"},map[string]any{"uploadId":second.ID,"family":"Available"}}},201);var plan model.Plan;decode(t,r,&plan)
 occupied,e:=f.s.Root.OpenFile(path.Join("libraries",f.library,plan.Items[0].RelativePath),os.O_CREATE|os.O_EXCL|os.O_WRONLY,0600);if e!=nil{t.Fatal(e)};if _,e=occupied.Write([]byte("synthetic external occupant"));e!=nil{t.Fatal(e)};if e=occupied.Close();e!=nil{t.Fatal(e)}
 r=f.json("POST","/batches/"+batch.ID+"/plans/"+plan.ID+"/commit","commit-partial-plan",map[string]any{"fingerprint":plan.Fingerprint},200);var report model.BatchDetail;decode(t,r,&report);if report.Batch.State!="partial"{t.Fatalf("state: %s",report.Batch.State)}
 states:=map[string]model.Upload{};for _,item:=range report.Items{states[item.Upload.ID]=item.Upload};if states[first.ID].State!="staged"||states[first.ID].ErrorCode==""||states[second.ID].State!="ingested"{t.Fatalf("independent result was blocked: %+v",states)}
}
func TestBackupPreservesStorageReserve(t *testing.T){f:=setup(t);f.s.Config.ReserveBytes=1<<62;f.json("POST","/backups","backup-space-budget",struct{}{},507)}
