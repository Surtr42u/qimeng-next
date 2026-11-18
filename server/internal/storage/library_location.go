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
  candidate,e:=filepath.Rel(location.absolute,absolute)
  if e!=nil {continue}
  candidate=filepath.ToSlash(candidate);if candidate=="." {candidate=""}
  if !cleanRelative(candidate) {continue}
  if len(location.absolute)>longest {storageID,relative,longest=id,candidate,len(location.absolute)}
 }
 if storageID=="" {return "","",fs.ErrPermission}
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
