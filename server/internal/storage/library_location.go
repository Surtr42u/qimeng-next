package storage

import (
 "errors"
 "io/fs"
 "os"
 "path/filepath"
 "strings"
)

// ResolveLibraryPath translates an owner's text field; it never registers a new
// trusted root. The returned relative path is revalidated on actual creation.
func (r *Root) ResolveLibraryPath(value string) (string, string, error) {
 if !filepath.IsAbs(value)||strings.ContainsRune(value,0)||len(value)>4096 {return "","",fs.ErrInvalid}
 absolute:=filepath.Clean(value)
 r.mu.RLock();defer r.mu.RUnlock()
 if overlaps(absolute,r.locations["data"].absolute) {return "","",fs.ErrPermission}
 storageID,relative:="",""
 longest:=-1
 for id,location:=range r.locations {
  if id=="data" {continue}
  // The operator's path can legitimately contain a Windows short name or a
  // trusted mount alias. All subsequent filesystem access uses the opened root.
  for _,prefix:=range []string{location.configured,location.absolute} {
   candidate,e:=filepath.Rel(prefix,absolute)
   if e!=nil {continue}
   candidate=filepath.ToSlash(candidate);if candidate=="." {candidate=""}
   if !cleanRelative(candidate) {continue}
   if len(prefix)>longest || len(prefix)==longest && id<storageID {storageID,relative,longest=id,candidate,len(prefix)}
  }
 }
 if storageID=="" {return "","",fs.ErrPermission}
 canonical:=filepath.Join(r.locations[storageID].absolute,filepath.FromSlash(relative))
 if overlaps(canonical,r.locations["data"].absolute) {return "","",fs.ErrPermission}
 root:=r.locations[storageID].root
 if relative!="" {
  parts:=strings.Split(relative,"/")
  for index:=range parts {
   info,e:=root.Lstat(strings.Join(parts[:index+1],"/"))
   if errors.Is(e,os.ErrNotExist) {break}
   if e!=nil {return "","",e}
   if info.Mode()&os.ModeSymlink!=0||!info.IsDir() {return "","",fs.ErrPermission}
  }
 }
 return storageID,relative,nil
}

func (r *Root) DisplayLibraryPath(id string) (string,error) {
 r.mu.RLock();defer r.mu.RUnlock()
 library,ok:=r.libraries[id]
 if !ok {return "",ErrUnavailable}
 root:=r.locations[library.storageID]
 return filepath.Join(root.configured,filepath.FromSlash(library.relative)),nil
}
