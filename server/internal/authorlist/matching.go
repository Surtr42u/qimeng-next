package authorlist

import (
 "path"
 "strings"
)

type File struct{ID string;Name string}
type WorkRule struct{Key string;Extension string;Pages bool}
type FileKeys struct{Stem string;PageBase string;Extension string}
func CompileWork(work string)WorkRule{work=strings.TrimSpace(work);extension:=mediaExtension(work);stem:=work;if extension!=""{stem=strings.TrimSuffix(stem,path.Ext(stem))};return WorkRule{Key:normalized(stem),Extension:extension,Pages:extension==""&&!numericPage.MatchString(stem)}}
func Keys(fileName string)FileKeys{extension:=path.Ext(fileName);stem:=normalized(strings.TrimSuffix(fileName,extension));base:="";if trailingPage.MatchString(stem){base=trailingPage.ReplaceAllString(stem,"")};return FileKeys{Stem:stem,PageBase:base,Extension:strings.ToLower(extension)}}
type Index struct{exact map[string][]File;pages map[string][]File}
func BuildIndex(files []File)Index{
 index:=Index{exact:map[string][]File{},pages:map[string][]File{}};for _,file:=range files{key:=normalized(strings.TrimSuffix(file.Name,path.Ext(file.Name)));index.exact[key]=append(index.exact[key],file);if trailingPage.MatchString(key){base:=trailingPage.ReplaceAllString(key,"");index.pages[base]=append(index.pages[base],file)}};return index
}
func(index Index)Match(work string)[]File{
 work=strings.TrimSpace(work);if work==""{return []File{}};extension:=mediaExtension(work);stem:=work;if extension!=""{stem=strings.TrimSuffix(stem,path.Ext(stem))};key:=normalized(stem);result:=[]File{};seen:=map[string]bool{}
 add:=func(files []File){for _,file:=range files{if !seen[file.ID]&&Matches(work,file.Name){seen[file.ID]=true;result=append(result,file)}}};add(index.exact[key]);if extension==""&&!numericPage.MatchString(stem){add(index.pages[key])};return result
}
