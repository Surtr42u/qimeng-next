package httpapi_test

import (
 "bytes"
 "context"
 "fmt"
 "os"
 "path/filepath"
 "testing"

 "qimeng.local/next/server/internal/httpapi"
 "qimeng.local/next/server/internal/model"
 "qimeng.local/next/server/internal/service"
)

func TestDetachLibraryRetainsOriginalHistoryAndTerminalUploadBackup(t *testing.T){
 media:=t.TempDir();cfg:=service.Config{DataDir:t.TempDir(),AdminToken:testSecret,StorageRoots:map[string]string{"synthetic":media},ReserveBytes:1}
 s,e:=service.New(cfg);if e!=nil{t.Fatal(e)};f:=&fixture{t:t,s:s,h:httpapi.New(s)};t.Cleanup(func(){f.s.Close()})
 var library model.Library;request:=map[string]any{"name":"Synthetic external index","kind":"regular","storageId":"synthetic","rootRelativePath":"media"}
 decode(t,f.json("POST","/libraries","library-detach-create",request,201),&library);f.library=library.ID
 if library.FileCount!=0||library.ScanState!="idle"{t.Fatal("new library omitted its empty index state")}
 source,contents:=f.upload("preserved.png",122,nil);asset:=*f.complete(source).Asset
 f.json("POST","/assets/"+asset.ID+"/events","library-history-before-detach",map[string]any{"eventId":"detached-library-history","kind":"view","contentVersion":1},200)
 staged,prefix:=f.upload("cancel-me.png",123,nil)
 url:="/libraries/"+library.ID+"?expectedRevision=1"
 denied:=f.json("DELETE",url,"detach-while-uploading",struct{}{},409);if !bytes.Contains(denied.Body.Bytes(),[]byte("library_upload_active")){t.Fatal(denied.Body.String())}
 f.json("DELETE","/uploads/"+staged.ID,"cancel-detached-prefix",struct{}{},204)
 var current model.Asset;decode(t,f.json("GET","/assets/"+asset.ID,"",nil,200),&current)
 decode(t,f.json("DELETE",fmt.Sprintf("/assets/%s?expectedRevision=%d",asset.ID,current.Revision),"trash-before-unregister",struct{}{},200),&current)
 denied=f.json("DELETE",url,"detach-while-trashed",struct{}{},409);if !bytes.Contains(denied.Body.Bytes(),[]byte("library_has_trash")){t.Fatal(denied.Body.String())}
 f.json("POST","/trash/"+asset.ID+"/restore","restore-before-remove",map[string]any{"expectedRevision":current.Revision},200)
 f.json("DELETE",url,"detach-library-once",struct{}{},204);f.json("DELETE",url,"detach-library-once",struct{}{},204)
 if b,e:=os.ReadFile(filepath.Join(media,"media","preserved.png"));e!=nil||!bytes.Equal(contents,b){t.Fatal("detachment changed original",e)}
 f.json("GET","/assets/"+asset.ID,"",nil,404);f.json("GET","/batches/"+source.BatchID,"",nil,200)
 var events int;if e=f.s.DB.DB.QueryRow("SELECT count(*) FROM events WHERE event_id='detached-library-history'").Scan(&events);e!=nil||events!=1{t.Fatal("historical evidence lost",e)}
 if e=f.s.Close();e!=nil{t.Fatal(e)};f.s,e=service.New(cfg);if e!=nil{t.Fatal(e)};f.h=httpapi.New(f.s)
 f.json("POST","/libraries","reregister-same-physical-root",request,201)
 var backup service.Backup;decode(t,f.json("POST","/backups","detached-input-backup",struct{}{},201),&backup)
 f.json("POST","/backups/"+backup.ID+"/export","detached-input-export",struct{}{},202);backup=awaitBackup(t,f,backup.ID);if backup.ExportState!="completed"{t.Fatal(backup.ExportError)}
 into:=filepath.Join(t.TempDir(),"restore");report,e:=service.RestoreBackup(context.Background(),service.RestoreOptions{ArchivePath:filepath.Join(cfg.DataDir,"backups",backup.ID,"archive.qnext"),Destination:into,ExpectedSHA256:backup.ArchiveHash,ReserveBytes:1});if e!=nil||report.UploadCount!=1{t.Fatal("detached input was lost in backup",report,e)}
 out,e:=os.ReadFile(filepath.Join(into,"uploads",staged.ID+".part"));if e!=nil||!bytes.Equal(out,prefix){t.Fatal("restored prefix changed",e)}
}

func TestLibraryDirectoryCountsAndStableTraversal(t *testing.T){
 f:=setup(t)
 for _,entry:=range []struct{path,name string;shade uint8}{{"","根.png",124},{"b","一级.png",125},{"b/nested","二级.png",126},{"c","第三.png",127}}{
  u,_:=f.upload(entry.name,entry.shade,map[string]any{"relativeDirectory":entry.path});_ = f.complete(u)
 }
 scan:=f.scan("library-counting-scan");var page model.Page[model.Library];decode(t,f.json("GET","/libraries","",nil,200),&page)
 if len(page.Items)!=1||page.Items[0].FileCount!=4||page.Items[0].ScanState!="idle"||page.Items[0].LatestScanID!=scan.ID{t.Fatal("library projection did not return current counts/scan")}
 first:="/libraries/"+f.library+"/directories?limit=1";var dirs service.DirectoryPage;decode(t,f.json("GET",first,"",nil,200),&dirs)
 if len(dirs.Items)!=1||dirs.Items[0].Name!="b"||dirs.Items[0].FileCount!=1||dirs.DirectFileCount!=1||dirs.NextCursor==""{t.Fatal("direct files were counted recursively",dirs)}
 f.json("POST","/libraries/"+f.library+"/directories","insert-dir-before-cursor",map[string]string{"relativePath":"a"},201)
 decode(t,f.json("GET",first+"&cursor="+dirs.NextCursor,"",nil,200),&dirs);if len(dirs.Items)!=1||dirs.Items[0].Name!="c"{t.Fatal("inserting an earlier directory broke paging")}
 var nested service.DirectoryPage;decode(t,f.json("GET","/libraries/"+f.library+"/directories?parent=b","",nil,200),&nested);if nested.DirectFileCount!=1||len(nested.Items)!=1||nested.Items[0].FileCount!=1{t.Fatal("nested counts inconsistent")}
}
