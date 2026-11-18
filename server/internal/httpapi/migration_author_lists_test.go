package httpapi_test

import (
 "encoding/json"
 "slices"
 "strings"
 "testing"

 "qimeng.local/next/server/internal/model"
 "qimeng.local/next/server/internal/service"
)

func TestV2RegularCreatorOnBothPartitionsRetainsFixedDirectoryAndSourceList(t *testing.T){
 f:=setup(t);regularUpload,_:=f.upload("普通.png",143,nil);reg:=*f.complete(regularUpload).Asset
 var lib model.Library;decode(t,f.json("POST","/libraries","v2-cos-partition",map[string]string{"name":"Costumes","kind":"cos"},201),&lib);f.library=lib.ID
 cosUpload,_:=f.upload("合拍.png",144,map[string]any{"relativeDirectory":"Folder/Work"});cos:=*f.complete(cosUpload).Asset;fixed:=cos.AuthorID
 doc:=map[string]any{"format":"qimeng_backup","schemaVersion":2,"appIdentifier":"synthetic.legacy","data":map[string]any{
  "mediaFiles":[]any{map[string]any{"recordKey":"regular","fileName":reg.FileName,"mediaType":"image","sizeBytes":reg.Size},map[string]any{"recordKey":"cos","fileName":cos.FileName,"mediaType":"image","sizeBytes":cos.Size,"isCosFile":true}},
  "viewEvents":[]any{},"authors":[]any{map[string]any{"authorId":"studio","displayName":"Studio"},map[string]any{"authorId":"cos_folder","displayName":"Folder"}},
  "authorMediaRefs":[]any{map[string]any{"recordKey":"regular","authorId":"studio","fileName":reg.FileName},map[string]any{"recordKey":"cos","authorId":"studio","fileName":cos.FileName},map[string]any{"recordKey":"cos","authorId":"cos_folder","fileName":cos.FileName}},
  "txtFragments":[]any{map[string]any{"filename":"合成清单.txt","content":"7  Studio\n来源  已购\n作品\n历史作品.png\n"}},
 }}
 raw,e:=json.Marshal(doc);if e!=nil{t.Fatal(e)}
 input:=stageV2(t,f,"v2-mixed-owner-data",raw);plan:=planV2(t,f,input,"v2-mixed-owner-plan")
 decode(t,f.json("POST","/migration/v2/plans/"+plan.ID+"/commit","v2-mixed-owner-commit",map[string]any{"fingerprint":plan.Fingerprint,"acceptSkipped":false},200),&plan)
 if plan.State!="committed"||plan.Counts["txtListsActivated"]!=1||plan.Counts["authorSetsApplied"]!=2{t.Fatalf("unexpected migration result: %+v",plan)}
 decode(t,f.json("GET","/assets/"+reg.ID,"",nil,200),&reg);decode(t,f.json("GET","/assets/"+cos.ID,"",nil,200),&cos)
 if reg.AuthorID=="unknown"||cos.AuthorID!=fixed||!slices.Equal(cos.AuthorIDs,[]string{fixed,reg.AuthorID}){t.Fatal("media partition was mistaken for creator identity")}
 var lists model.Page[service.AuthorList];decode(t,f.json("GET","/author-lists","",nil,200),&lists)
 if len(lists.Items)!=1{t.Fatal("migration added redundant automatic text lists")}
 body:=listContent(t,f,"/author-lists/"+lists.Items[0].ID+"/content");for _,term:=range []string{"7  Studio","已购","历史作品.png",reg.FileName,cos.FileName}{if !strings.Contains(body,term){t.Fatal("lost imported declaration",term)}}
 f.json("POST","/author-lists/rebuild","v2-mixed-rebuild",struct{}{},200);decode(t,f.json("GET","/assets/"+cos.ID,"",nil,200),&cos);if !slices.Equal(cos.AuthorIDs,[]string{fixed,reg.AuthorID}){t.Fatal("rebuild discarded imported mixed-creator relationship")}
}

func TestDeletedOrMergedCreatorRetainsOriginalMaintenanceTask(t *testing.T){
 for _,merged:=range []bool{false,true}{
  f:=setup(t);var a model.Author;decode(t,f.json("POST","/authors","author-with-history",map[string]string{"name":"Archived identity"},201),&a)
  var task service.AuthorTask;decode(t,f.json("POST","/author-tasks","history-author-grant",map[string]any{"authorId":a.ID,"expectedRevision":a.Revision,"allowedFields":[]string{"biography"}},201),&task)
  if merged{
   var target model.Author;decode(t,f.json("POST","/authors","history-merge-target",map[string]string{"name":"Retained creator"},201),&target)
   f.json("POST","/authors/merge","merge-author-with-tasks",map[string]any{"sourceId":a.ID,"targetId":target.ID,"expectedSourceRevision":a.Revision,"expectedTargetRevision":target.Revision},200)
  }else{f.json("DELETE","/authors/"+a.ID+"?expectedRevision=1","delete-author-with-tasks",struct{}{},204)}
  var context struct{Task service.AuthorTask;Author model.Author;NextActions []string}
  decode(t,f.json("GET","/author-tasks/"+task.ID,"",nil,200),&context);if context.Task.State!="cancelled"||context.Author.ID!=a.ID||context.Task.AuthorID!=a.ID||len(context.NextActions)!=0{t.Fatal("source task was transferred or its audit lost",context)}
  f.json("POST","/credentials","",map[string]any{"label":"stale task","authorTaskId":task.ID,"scopes":[]string{"read","author.profile.write"}},409)
 }
}

func TestAddedOrderUsesFirstTXTDeclarationAcrossPages(t *testing.T){
 f:=setup(t);_ = importList(t,f,"import-order-check","order.txt","9  Zeta\n作品\na.png\n\n1  Alpha\n作品\nb.png\n",0)
 read:=func(url string)model.Page[model.Author]{var p model.Page[model.Author];decode(t,f.json("GET",url,"",nil,200),&p);return p}
 asc:=read("/authors?sort=addedAsc&limit=1");if len(asc.Items)!=1||asc.Items[0].Name!="Zeta"||asc.Items[0].TXTOrder==nil||*asc.Items[0].TXTOrder!=1||asc.NextCursor==""{t.Fatal("first declaration was not first")}
 next:=read("/authors?sort=addedAsc&limit=1&cursor="+asc.NextCursor);if next.Items[0].Name!="Alpha"{t.Fatal("TXT ordering cursor skipped an author")}
 desc:=read("/authors?sort=addedDesc&limit=1");if desc.Items[0].Name!="Alpha"{t.Fatal("undocumented rows sorted ahead of the reversed TXT group")}
}
