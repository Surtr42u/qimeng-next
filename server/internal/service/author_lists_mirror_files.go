package service

import (
 "context"
 "errors"
 "io"
 "os"
 "path/filepath"
 "strings"
)

type mirrorGeneration struct{root *os.Root;name string}

func readMirrorFile(root *os.Root,name string,limit int64)([]byte,bool,error){
 info,e:=root.Lstat(name);if errors.Is(e,os.ErrNotExist){return nil,false,nil};if e!=nil{return nil,false,e}
 if !info.Mode().IsRegular()||info.Size()>limit{return nil,false,Fail(409,"mirror_external_change","镜像目标不是可覆盖的受管文件。")}
 f,e:=root.Open(name);if e!=nil{return nil,false,e};defer f.Close();opened,e:=f.Stat();if e!=nil{return nil,false,e};if !os.SameFile(opened,info){return nil,false,Fail(409,"mirror_external_change","镜像目标在读取时变化。")}
 raw,e:=io.ReadAll(io.LimitReader(f,limit+1));if e!=nil{return nil,false,e};if int64(len(raw))>limit{return nil,false,Fail(409,"mirror_external_change","镜像目标超出大小预算。")};return raw,true,nil
}

func replaceMirrorFile(root *os.Root,name string,raw []byte)error{
 temp:=".qimeng-export-"+ID()+".tmp"
 f,e:=root.OpenFile(temp,os.O_WRONLY|os.O_CREATE|os.O_EXCL,0600);if e!=nil{return e};defer root.Remove(temp)
 _,e=f.Write(raw);if e==nil{e=f.Sync()};closeErr:=f.Close();if e!=nil{return e};if closeErr!=nil{return closeErr}
 if e=root.Rename(temp,name);e!=nil{return e};parent,e:=root.Open(".");if e!=nil{return e};defer parent.Close();return syncDirectory(parent)
}

func(g mirrorGeneration)read()(data []byte,found bool,verified bool,err error){
 data,found,err=readMirrorFile(g.root,g.name,maxAuthorListTextBytes);if err!=nil{return}
 sum,present,e:=readMirrorFile(g.root,g.name+".sha256",4096);if e!=nil{err=e;return}
 if !found{if present{err=Fail(409,"mirror_external_change","镜像正文缺失，已有摘要需核对。")};return}
 fields:=strings.Fields(string(sum));verified=present&&len(fields)>0&&strings.EqualFold(fields[0],digest(data));return
}

func(g mirrorGeneration)publish(value []byte,allowedHash string)error{
 current,found,verified,e:=g.read();if e!=nil{return e}
 same:=found&&string(current)==string(value)
 if found&&!same&&!verified&&digest(current)!=allowedHash{return Fail(409,"mirror_external_change","发现未经校验的外部编辑，请先导入或核对。")}
 if !same{if e=replaceMirrorFile(g.root,g.name,value);e!=nil{return e}}
 if !same||!verified{return replaceMirrorFile(g.root,g.name+".sha256",[]byte(digest(value)+"\n"))}
 return nil
}

func previousMirrorDirectory(directory,relative string)(string,bool){
 parts:=strings.Split(relative,"/");index:=-1;previous:=""
 for i,p:=range parts{switch p{case "当前","current":if index>=0{return "",false};index=i;if p=="当前"{previous="上次"}else{previous="previous"}}}
 if index<0{return "",false}
 dir:=directory;for i:=len(parts)-1;i>=index;i--{dir=filepath.Dir(dir)}
 if filepath.Dir(dir)==dir{return "",false}
 return filepath.Join(append([]string{dir,previous},parts[index+1:]...)...),true
}

func(s *Service)writeAuthorMirrorFile(m AuthorListMirror,content string)error{
 directory:=filepath.Dir(m.Path);root,relative,e:=s.Root.TextExportRoot(directory);if e!=nil{return e};defer root.Close()
 current:=mirrorGeneration{root,filepath.Base(m.Path)};old,found,verified,e:=current.read();if e!=nil{return e}
 if !found||string(old)==content{return current.publish([]byte(content),"")}
 known:=verified
 if !known{if e=s.DB.DB.QueryRowContext(context.Background(),"SELECT EXISTS(SELECT 1 FROM author_txt_fragments WHERE sha256=?)",digest(old)).Scan(&known);e!=nil{return e}}
 if !known{return Fail(409,"mirror_external_change","当前镜像有未经导入的更改，已保留原文件。")}
 if m.Mode=="master"{
  if previous,ok:=previousMirrorDirectory(directory,relative);ok{
   prevRoot,_,e:=s.Root.TextExportRoot(previous);if e!=nil{return e};defer prevRoot.Close()
   if e=(mirrorGeneration{prevRoot,current.name}).publish(old,"");e!=nil{return e}
  }else{
   ext:=filepath.Ext(current.name);stem:=strings.TrimSuffix(current.name,ext)
   previous:=mirrorGeneration{root,stem+"（上次）"+ext}
   last,exists,previousOK,e:=previous.read();if e!=nil{return e}
   if exists&&string(last)!=string(old){
    if !previousOK{return Fail(409,"mirror_external_change","上次镜像摘要不符，已保留全部版本。")}
    if e=(mirrorGeneration{root,stem+"（上上次）"+ext}).publish(last,"");e!=nil{return e}
   }
   if e=previous.publish(old,"");e!=nil{return e}
  }
 }
 return current.publish([]byte(content),digest(old))
}
