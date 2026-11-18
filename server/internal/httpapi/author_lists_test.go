package httpapi_test

import (
 "bytes"
 "context"
 "os"
 "path/filepath"
 "slices"
 "strings"
 "testing"
 "time"

 "qimeng.local/next/server/internal/httpapi"
 "qimeng.local/next/server/internal/model"
 "qimeng.local/next/server/internal/service"
)

func importList(t *testing.T,f *fixture,key,name,content string,rev int64)service.AuthorListResult{
 t.Helper();var result service.AuthorListResult
 decode(t,f.json("POST","/author-lists",key,map[string]any{"name":name,"content":content,"expectedRevision":rev},200),&result);return result
}
func listContent(t *testing.T,f *fixture,url string)string{
 t.Helper();r:=f.request("GET",url,"",testSecret,nil,nil);if r.Code!=200||r.Header().Get("ETag")==""{t.Fatal("text export",r.Code,r.Body.String())};return r.Body.String()
}

func TestRenamingOneSharedOrPageRulePreservesSiblingAndExportsNewBinding(t *testing.T){
 f:=setup(t)
 names:=[]string{"left/shared.png","right/shared.png","pages/Pages (1).png","pages/Pages (2).png"}
 for i,name:=range names{
  file,err:=f.s.Root.OpenFile("libraries/"+f.library+"/"+name,os.O_CREATE|os.O_EXCL|os.O_WRONLY,0600)
  if err!=nil{t.Fatal(err)}
  _,err=file.Write(pngBytes(t,uint8(110+i)));closeErr:=file.Close()
  if err!=nil||closeErr!=nil{t.Fatal(err,closeErr)}
 }
 if scan:=f.scan("scan-shared-author-works");scan.Added!=4{t.Fatal(scan)}
 initialText:="1  Synthetic author\n作品\nshared.png\nPages\n"
 imported:=importList(t,f,"import-shared-author-text","shared.txt",initialText,0)
 var page model.Page[model.Asset]
 decode(t,f.json("GET","/assets","",nil,200),&page)
 if imported.List==nil||len(page.Items)!=4||imported.MatchedAssets!=4{t.Fatal("setup did not match all works",imported)}
 byPath:=map[string]model.Asset{}
 for _,a:=range page.Items{byPath[a.RelativePath]=a}
 moves:=[]struct{old,new,key string}{{names[0],"left/renamed.png","move-one-shared-work"},{names[2],"pages/edited.png","move-one-page-work"}}
 for _,m:=range moves{
  a:=byPath[m.old]
  decode(t,f.json("POST","/assets/"+a.ID+"/move",m.key,map[string]any{"expectedRevision":a.Revision,"relativePath":m.new},200),&a)
 }
 exported:=listContent(t,f,"/author-lists/"+imported.List.ID+"/content")
 for _,line:=range []string{"shared.png","renamed.png","Pages","edited.png"}{if strings.Count(exported,"\n"+line+"\n")!=1{t.Fatal("fragment lost or repeated work declaration",exported)}}
 f.json("POST","/author-lists/rebuild","rebuild-shared-renames",struct{}{},200)
 decode(t,f.json("GET","/assets","",nil,200),&page)
 for _,a:=range page.Items{if a.AuthorName!="Synthetic author"{t.Fatal("rebuild lost a sibling or renamed work",a.RelativePath)}}
 var current service.AuthorList
 decode(t,f.json("GET","/author-lists/"+imported.List.ID,"",nil,200),&current)
 importList(t,f,"remove-renamed-text-declarations","shared.txt",initialText,current.Revision)
 decode(t,f.json("GET","/assets","",nil,200),&page)
 for _,a:=range page.Items{
  edited:=a.ID==byPath[names[0]].ID||a.ID==byPath[names[2]].ID
  if edited!=(a.AuthorID=="unknown"){t.Fatalf("asset retained an obsolete rule or lost its own: %s %s",a.RelativePath,a.AuthorID)}
 }
}

func TestAuthorListsCrossListUnionsAndExplicitRemovals(t *testing.T){
 f:=setup(t);u,_:=f.upload("合成画集 (2).png",41,nil);asset:=*f.complete(u).Asset
 a:=importList(t,f,"first-text-import","第一份.txt","1  Painter A  Alias\n来源  甲  渠道 B\n作品\n合成画集\n",0)
 b:=importList(t,f,"second-text-import","第二份.txt","7  Painter B\n出处  乙\n作品\n合成画集 (2).png\n",0)
 if a.List==nil||b.List==nil||b.ChangedAssets!=1{t.Fatal("cross-list change was not reported")}
 decode(t,f.json("GET","/assets/"+asset.ID,"",nil,200),&asset);if len(asset.AuthorIDs)!=2{t.Fatal("cross-list union lost a creator")}
 var creator model.Author;decode(t,f.json("GET","/authors/"+asset.AuthorIDs[0],"",nil,200),&creator)
 if creator.Name!="Painter A / Alias"||!slices.Equal(creator.AcquisitionSources,[]string{"甲","渠道 B"}){t.Fatal("author aliases or sources not recovered")}
 decode(t,f.json("PATCH","/assets/"+asset.ID,"manual-source-detach",map[string]any{"expectedRevision":asset.Revision,"expectedAuthorsRevision":asset.AuthorsRevision,"authorIds":[]string{asset.AuthorIDs[1]}},200),&asset)
 f.json("POST","/author-lists/rebuild","rebuild-after-manual-detach",struct{}{},200)
 var current model.Asset;decode(t,f.json("GET","/assets/"+asset.ID,"",nil,200),&current)
 if !slices.Equal(current.AuthorIDs,asset.AuthorIDs)||current.AuthorsRevision!=asset.AuthorsRevision{t.Fatal("rebuild resurrected a manually removed creator")}
 u,_=f.upload("合成画集 (3).png",42,nil);next:=*f.complete(u).Asset
 if next.AuthorID!=creator.ID||next.AuthorStatus!="confirmed"{t.Fatal("new upload did not receive the matching declaration")}
 plain:=importList(t,f,"plain-author-import","names.txt","Studio only\n",0);if plain.Format!="names"||plain.List!=nil||plain.AuthorsCreated!=1{t.Fatal("plain names created an active matching fragment")}
}

func TestNamedAuthorAtomicCASUploadSourcesAndReimportChoice(t *testing.T){
 f:=setup(t);list:=importList(t,f,"name-active-list","合成.txt","1  Existing\n来源  初始渠道\n作品\nunmatched.png\n",0)
 u,_:=f.upload("手工.png",45,nil);asset:=*f.complete(u).Asset
 url:="/assets/"+asset.ID
 change:=map[string]any{"expectedRevision":asset.Revision,"expectedAuthorsRevision":asset.AuthorsRevision,"authorIds":[]string{},"newAuthorNames":[]string{"New Name / Alias"}}
 f.json("PATCH",url,"",change,400)
 change["expectedAuthorsRevision"]=99;f.json("PATCH",url,"stale-create-name",change,409)
 var hits model.Page[model.Author];decode(t,f.json("GET","/authors?q=New","",nil,200),&hits);if len(hits.Items)!=0{t.Fatal("stale edit left an orphan creator")}
 change["expectedAuthorsRevision"]=asset.AuthorsRevision
 first:=f.json("PATCH",url,"commit-new-name",change,200);decode(t,first,&asset)
 replay:=f.json("PATCH",url,"commit-new-name",change,200);if !bytes.Equal(first.Body.Bytes(),replay.Body.Bytes()){t.Fatal("replay changed author identities")}
 if asset.AuthorName!="New Name / Alias"{t.Fatal("new author was not linked")}
 selected:=asset.AuthorID
 u,_=f.upload("渠道.png",46,map[string]any{"authorIds":[]string{selected},"authorAcquisitionSources":[]string{"作者渠道","作者渠道"},"acquisitionSources":[]string{"资产线索"}});second:=*f.complete(u).Asset
 if !slices.Equal(second.AcquisitionSources,[]string{"资产线索"}){t.Fatal("asset clues and author vocabulary were conflated")}
 u,_=f.upload("无选择.png",47,map[string]any{"authorAcquisitionSources":[]string{"无人选择"}});_ = f.complete(u)
 var author model.Author;decode(t,f.json("GET","/authors/"+selected,"",nil,200),&author);if !slices.Equal(author.AcquisitionSources,[]string{"作者渠道"}){t.Fatal("upload sources changed an unselected creator")}
 projected:=listContent(t,f,"/author-lists/"+list.List.ID+"/content");if !strings.Contains(projected,"来源  作者渠道")||!strings.Contains(projected,"手工.png"){t.Fatal("file completion omitted the author-list projection")}
 var meta service.AuthorList;decode(t,f.json("GET","/author-lists/"+list.List.ID,"",nil,200),&meta)
 reimport:=map[string]any{"name":meta.Name,"content":"3  Existing\n作品\nunmatched.png\n","expectedRevision":meta.Revision}
 conflict:=f.json("POST","/author-lists","missing-manual-check",reimport,409);if !strings.Contains(conflict.Body.String(),"totalMissing"){t.Fatal("no structured missing entries")}
 reimport["conflictResolution"]="keep";var kept service.AuthorListResult;decode(t,f.json("POST","/author-lists","keep-manual-entries",reimport,200),&kept)
 if !strings.Contains(listContent(t,f,"/author-lists/"+meta.ID+"/content"),"手工.png"){t.Fatal("keep dropped the manually added entry")}
 reimport["expectedRevision"]=kept.List.Revision;reimport["conflictResolution"]="remove";f.json("POST","/author-lists","remove-manual-entries",reimport,200)
 decode(t,f.json("GET",url,"",nil,200),&asset);if asset.AuthorID!="unknown"{t.Fatal("explicitly removed entry survived")}
 if _,e:=f.s.Root.Stat("libraries/"+f.library+"/手工.png");e!=nil{t.Fatal("clearing a declaration removed media")}
}

func TestAuthorMirrorRotationAndExternalEditProtection(t *testing.T){
 allowed:=t.TempDir()
 s,e:=service.New(service.Config{DataDir:t.TempDir(),AdminToken:testSecret,ReserveBytes:1,StorageRoots:map[string]string{"owner":allowed}});if e!=nil{t.Fatal(e)};t.Cleanup(func(){s.Close()})
 f:=&fixture{t:t,s:s,h:httpapi.New(s)}
 _=importList(t,f,"mirror-import-one","fixture.txt","1  合成甲\n作品\n原图.png\n",0)
 target:=filepath.Join(allowed,"maintenance","current","lists","master.txt")
 input:=map[string]any{"expectedRevision":0,"mode":"master","path":target}
 f.json("PUT","/author-lists/mirror","mirror-configure",input,200)
 wait:=func(state string)service.AuthorListMirror{t.Helper();for deadline:=time.Now().Add(10*time.Second);time.Now().Before(deadline);{
   m,e:=s.AuthorListMirror(context.Background(),model.Principal{ID:"owner",Scopes:[]string{"admin"}});if e!=nil{t.Fatal(e)};if m.State==state{return m};time.Sleep(10*time.Millisecond)
  };t.Fatal("mirror state did not reach",state);return service.AuthorListMirror{}}
 _=wait("current");old,e:=os.ReadFile(target);if e!=nil{t.Fatal(e)}
 _=importList(t,f,"mirror-import-two","another.txt","9  合成乙\n作品\n别图.png\n",0)
 _=wait("current");previous,e:=os.ReadFile(filepath.Join(allowed,"maintenance","previous","lists","master.txt"));if e!=nil||!bytes.Equal(previous,old){t.Fatal("previous generation did not preserve exact bytes",e)}
 external:=[]byte("Owner external changes");if e=os.WriteFile(target,external,0600);e!=nil{t.Fatal(e)}
 _=importList(t,f,"mirror-import-after-edit","third.txt","1  合成丙\n作品\n三.png\n",0)
 state:=wait("error");if state.ErrorCode!="mirror_external_change"{t.Fatal("missing external-change report")}
 read,e:=os.ReadFile(target);if e!=nil||!bytes.Equal(external,read){t.Fatal("external edit overwritten")}
 if body:=listContent(t,f,"/author-lists/master");!strings.Contains(body,"合成丙"){t.Fatal("mirror failure rolled back author edit")}
}
