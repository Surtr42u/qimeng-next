package httpapi_test

import (
 "bytes"
 "encoding/json"
 "os"
 "path/filepath"
 "testing"

 "qimeng.local/next/server/internal/httpapi"
 "qimeng.local/next/server/internal/model"
 "qimeng.local/next/server/internal/service"
)

func TestOwnerRootTextResolvesWithoutGrantingModelFilesystemAccess(t *testing.T) {
 mediaRoot,dataRoot:=t.TempDir(),t.TempDir()
 readSecret:="synthetic-reader-secret-with-32-characters"
 s,e:=service.New(service.Config{DataDir:dataRoot,AdminToken:testSecret,AIToken:readSecret,StorageRoots:map[string]string{"media":mediaRoot},ReserveBytes:1})
 if e!=nil {t.Fatal(e)};defer s.Close()
 f:=&fixture{t:t,s:s,h:httpapi.New(s)}
 requested:=filepath.Join(mediaRoot,"new-folder","nested")
 var location service.LibraryLocation
 decode(t,f.json("POST","/storages/resolve-library-root","",map[string]any{"rootPath":requested},200),&location)
 if location.StorageID!="media"||location.RootRelativePath!="new-folder/nested"||location.RootPath!=requested {t.Fatal("owner root was not translated to a trusted location")}
 if _,e:=os.Stat(requested);!os.IsNotExist(e) {t.Fatal("resolving a text field created a directory")}
 var library model.Library
 decode(t,f.json("POST","/libraries","create-from-owner-root",map[string]any{"name":"Owner path","kind":"regular","storageId":location.StorageID,"rootRelativePath":location.RootRelativePath},201),&library)
 var displayed service.LibraryLocation
 decode(t,f.json("GET","/libraries/"+library.ID+"/location","",nil,200),&displayed)
 if displayed!=location {t.Fatal("owner root changed after library registration")}
 body,_:=json.Marshal(map[string]any{"rootPath":requested})
 response:=f.request("POST","/storages/resolve-library-root","",readSecret,body,nil)
 if response.Code!=403||bytes.Contains(response.Body.Bytes(),[]byte(mediaRoot)) {t.Fatal("read-only model learned private roots")}
 response=f.request("GET","/libraries/"+library.ID+"/location","",readSecret,nil,nil)
 if response.Code!=403 {t.Fatal("read credential gained private location access")}
 response=f.request("GET","/libraries","",readSecret,nil,nil)
 if response.Code!=200||bytes.Contains(response.Body.Bytes(),[]byte(mediaRoot)) {t.Fatal("general library metadata exposed a host path")}
 for _,denied:=range []string{dataRoot,filepath.Join(dataRoot,"child"),t.TempDir(),"../outside"} {
  f.json("POST","/storages/resolve-library-root","",map[string]any{"rootPath":denied},400)
 }
}

func TestRootResolutionRejectsLinksWithinTheTrustedRoot(t *testing.T) {
 mediaRoot,dataRoot:=t.TempDir(),t.TempDir()
 linked:=filepath.Join(mediaRoot,"linked")
 if e:=os.Symlink(t.TempDir(),linked);e!=nil {t.Skip("platform does not permit a synthetic symlink")}
 s,e:=service.New(service.Config{DataDir:dataRoot,AdminToken:testSecret,StorageRoots:map[string]string{"media":mediaRoot},ReserveBytes:1})
 if e!=nil {t.Fatal(e)};defer s.Close()
 f:=&fixture{t:t,s:s,h:httpapi.New(s)}
 f.json("POST","/storages/resolve-library-root","",map[string]any{"rootPath":filepath.Join(linked,"child")},400)
}

func TestOwnerMayUseOperatorConfiguredRootSpelling(t *testing.T) {
 actual,aliasDir:=t.TempDir(),t.TempDir()
 alias:=filepath.Join(aliasDir,"trusted-alias")
 if e:=os.Symlink(actual,alias);e!=nil {t.Skip("platform does not permit a synthetic root alias")}
 s,e:=service.New(service.Config{DataDir:t.TempDir(),AdminToken:testSecret,StorageRoots:map[string]string{"media":alias},ReserveBytes:1})
 if e!=nil {t.Fatal(e)};defer s.Close()
 f:=&fixture{t:t,s:s,h:httpapi.New(s)}
 var location service.LibraryLocation
 decode(t,f.json("POST","/storages/resolve-library-root","",map[string]any{"rootPath":filepath.Join(alias,"new")},200),&location)
 if location.StorageID!="media"||location.RootRelativePath!="new" {t.Fatal("configured root spelling did not map to the opened storage")}
}
