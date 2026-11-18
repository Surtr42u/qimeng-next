// Package storage resolves service-owned virtual paths into explicitly configured
// local roots. Host paths never enter API payloads or model-provided instructions.
package storage

import (
 "errors"
 "io/fs"
 "os"
 "path"
 "path/filepath"
 "sort"
 "strings"
 "sync"
 "time"
)

var ErrUnavailable=errors.New("logical storage is unavailable")
type location struct{root *os.Root;absolute,configured string}
type library struct{storageID,relative string}
type Info struct{ID string `json:"id"`;Available bool `json:"available"`}
type Root struct{mu sync.RWMutex;local *os.Root;locations map[string]location;libraries map[string]library;uploads map[string]string}
func Open(dataDir string,configured map[string]string)(*Root,error){
 absolute,e:=filepath.EvalSymlinks(dataDir);if e!=nil{return nil,e};absolute,e=filepath.Abs(absolute);if e!=nil{return nil,e};local,e:=os.OpenRoot(absolute);if e!=nil{return nil,e};r:=&Root{local:local,locations:map[string]location{"data":{root:local,absolute:absolute,configured:filepath.Clean(dataDir)}},libraries:map[string]library{},uploads:map[string]string{}}
 for id,value:=range configured{if id=="data"||id==""||strings.ContainsAny(id,"/\\:\x00")||len(id)>64||!filepath.IsAbs(value){r.Close();return nil,errors.New("invalid logical storage configuration")};root,e:=os.OpenRoot(value);if e!=nil{r.Close();return nil,ErrUnavailable};resolved,e:=filepath.EvalSymlinks(value);if e!=nil{root.Close();r.Close();return nil,e};r.locations[id]=location{root:root,absolute:resolved,configured:filepath.Clean(value)}}
 return r,nil
}
func(r *Root)Close()error{r.mu.Lock();defer r.mu.Unlock();var first error;for _,l:=range r.locations{if e:=l.root.Close();e!=nil&&first==nil{first=e}};return first}
func(r *Root)Storages()[]Info{r.mu.RLock();defer r.mu.RUnlock();items:=[]Info{};for id:=range r.locations{items=append(items,Info{ID:id,Available:true})};sort.Slice(items,func(i,j int)bool{return items[i].ID<items[j].ID});return items}
func cleanRelative(value string)bool{return value==""||(!strings.HasPrefix(value,"/")&&!strings.ContainsAny(value,"\\:\x00")&&path.Clean(value)==value&&value!=".."&&!strings.HasPrefix(value,"../"))}
func overlaps(first,second string)bool{within:=func(child,parent string)bool{relative,e:=filepath.Rel(parent,child);return e==nil&&(relative=="."||(relative!=".."&&!strings.HasPrefix(relative,".."+string(os.PathSeparator))))};return within(first,second)||within(second,first)}
func(r *Root)RegisterLibrary(id,storageID,relative string)error{
 r.mu.Lock();defer r.mu.Unlock();if storageID==""{storageID="data"};if !cleanRelative(relative){return fs.ErrInvalid};if _,ok:=r.locations[storageID];!ok{return ErrUnavailable};if storageID=="data"{relative="libraries/"+id}
 absolute:=filepath.Join(r.locations[storageID].absolute,filepath.FromSlash(relative));if storageID!="data"&&overlaps(absolute,r.locations["data"].absolute){return errors.New("media roots must be separate from the control data directory")}
 for existingID,existing:=range r.libraries{if existingID==id{continue};other:=filepath.Join(r.locations[existing.storageID].absolute,filepath.FromSlash(existing.relative));if overlaps(absolute,other){return errors.New("library roots overlap")}}
 r.libraries[id]=library{storageID,relative};return nil
}
func(r *Root)RegisterUpload(id,libraryID string){r.mu.Lock();defer r.mu.Unlock();r.uploads[id]=libraryID}
func(r *Root)UnregisterLibrary(id string){r.mu.Lock();defer r.mu.Unlock();delete(r.libraries,id)}
func(r *Root)resolve(name string)(location,string,error){
 if !cleanRelative(name){return location{},"",fs.ErrInvalid};r.mu.RLock();defer r.mu.RUnlock();local:=r.locations["data"];parts:=strings.Split(name,"/")
 route:=func(id string,rest []string)(location,string,error){lib,ok:=r.libraries[id];if !ok{return location{},"",ErrUnavailable};l,ok:=r.locations[lib.storageID];if !ok||l.root==nil{return location{},"",ErrUnavailable};relative:=path.Join(append([]string{lib.relative},rest...)...);parts:=strings.Split(relative,"/");for i:=range parts{info,e:=l.root.Lstat(strings.Join(parts[:i+1],"/"));if errors.Is(e,os.ErrNotExist){break};if e!=nil{return location{},"",e};if info.Mode()&os.ModeSymlink!=0{return location{},"",fs.ErrPermission}};return l,relative,nil}
 if len(parts)>=2&&parts[0]=="libraries"{if _,ok:=r.libraries[parts[1]];ok{return route(parts[1],parts[2:])}}
 if len(parts)>=2&&parts[0]=="library-trash"{return route(parts[1],append([]string{".qimeng-internal","trash"},parts[2:]...))}
 if len(parts)==2&&(parts[0]=="uploads"||parts[0]=="retired"){
  // Preserve pre-storage-layout temporary paths during an upgrade/recovery.
  if _,e:=local.root.Lstat(name);e==nil{return local,name,nil}else if !errors.Is(e,os.ErrNotExist){return location{},"",e}
  id:=strings.TrimSuffix(parts[1],".part");if libraryID,ok:=r.uploads[id];ok{return route(libraryID,[]string{".qimeng-internal",parts[0],parts[1]})}
 }
 return local,name,nil
}
func(r *Root)AbsolutePath(name string)(string,error){l,p,e:=r.resolve(name);if e!=nil{return "",e};return filepath.Join(l.absolute,filepath.FromSlash(p)),nil}
func(r *Root)LibraryPath(id string)(string,error){return r.AbsolutePath("libraries/"+id)}
func(r *Root)Open(name string)(*os.File,error){l,p,e:=r.resolve(name);if e!=nil{return nil,e};return l.root.Open(p)}
func(r *Root)OpenFile(name string,flag int,mode os.FileMode)(*os.File,error){l,p,e:=r.resolve(name);if e!=nil{return nil,e};if flag&os.O_CREATE!=0{if e=l.root.MkdirAll(path.Dir(p),0700);e!=nil{return nil,e}};return l.root.OpenFile(p,flag,mode)}
func(r *Root)OpenParent(name string)(*os.File,error){l,p,e:=r.resolve(name);if e!=nil{return nil,e};return l.root.Open(path.Dir(p))}
func(r *Root)Stat(name string)(os.FileInfo,error){l,p,e:=r.resolve(name);if e!=nil{return nil,e};return l.root.Stat(p)}
func(r *Root)Lstat(name string)(os.FileInfo,error){l,p,e:=r.resolve(name);if e!=nil{return nil,e};return l.root.Lstat(p)}
func(r *Root)MkdirAll(name string,mode os.FileMode)error{l,p,e:=r.resolve(name);if e!=nil{return e};return l.root.MkdirAll(p,mode)}
func(r *Root)Remove(name string)error{l,p,e:=r.resolve(name);if e!=nil{return e};return l.root.Remove(p)}
func(r *Root)Chtimes(name string,atime,mtime time.Time)error{l,p,e:=r.resolve(name);if e!=nil{return e};return l.root.Chtimes(p,atime,mtime)}
func(r *Root)Link(oldname,newname string)error{source,old,e:=r.resolve(oldname);if e!=nil{return e};target,newPath,e:=r.resolve(newname);if e!=nil{return e};if source.root!=target.root{return errors.New("cross-storage publication requires staging in target root")};if e=target.root.MkdirAll(path.Dir(newPath),0700);e!=nil{return e};return source.root.Link(old,newPath)}
