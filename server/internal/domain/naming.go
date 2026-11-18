// Package domain holds deterministic rules without IO or application state.
package domain

import (
 "fmt"
 "path"
 "regexp"
 "sort"
 "strconv"
 "strings"
 "unicode"
 "unicode/utf8"
)

var workPattern=regexp.MustCompile(`^(.*?)([0-9]+)(?: \(([0-9]+)\))?$`)
type WorkName struct{ Family, Separator string; Number, Page, Width int }

// ParseWork distinguishes a work number from a trailing page. A bare '(2)'
// belongs to the historical collision/series syntax and consumes no work number.
func ParseWork(name string)(WorkName,bool){
 stem:=strings.TrimSuffix(path.Base(name),path.Ext(name));m:=workPattern.FindStringSubmatch(stem)
 if m==nil{return WorkName{},false}
 prefix:=m[1];family:=strings.TrimRight(prefix," ");if family==""||strings.HasSuffix(family,"("){return WorkName{},false}
 n,e:=strconv.Atoi(m[2]);if e!=nil||n<1{return WorkName{},false}
 p:=0;if m[3]!=""{p,e=strconv.Atoi(m[3]);if e!=nil||p<1{return WorkName{},false}}
 return WorkName{family,prefix[len(family):],n,p,len(m[2])},true
}
func MediaType(filename string)(string,string,bool){
 switch strings.ToLower(path.Ext(filename)){
 case ".jpg",".jpeg":return "image","image/jpeg",true
 case ".png":return "image","image/png",true
 case ".gif":return "animated_image","image/gif",true
 case ".webp":return "image","image/webp",true
 case ".avif":return "image","image/avif",true
 case ".mp4",".m4v":return "video","video/mp4",true
 case ".mov":return "video","video/quicktime",true
 case ".webm":return "video","video/webm",true
 case ".mkv":return "video","video/x-matroska",true
 case ".avi":return "video","video/x-msvideo",true
 }
 return "","",false
}
func NumberClass(filename string)string{m,_,ok:=MediaType(filename);if !ok{return ""};if m=="video"{return "video"};return "image"}
func ValidName(name string)bool{
 if name==""||len(name)>240||name=="."||name==".."||!utf8.ValidString(name)||strings.ContainsAny(name,"/\\<>:\"|?*\x00\ufffd"){return false}
 if strings.HasSuffix(name,".")||strings.HasSuffix(name," "){return false}
 for _,r:=range name{if unicode.IsControl(r){return false}}
 stem:=strings.ToUpper(strings.SplitN(name,".",2)[0]);switch stem{case "CON","PRN","AUX","NUL","COM1","COM2","COM3","COM4","COM5","COM6","COM7","COM8","COM9","LPT1","LPT2","LPT3","LPT4","LPT5","LPT6","LPT7","LPT8","LPT9":return false}
 return true
}
func ValidRelative(p string)bool{
 if p==""{return true};if strings.HasPrefix(p,"/")||strings.Contains(p,"\\")||path.Clean(p)!=p{return false}
 for _,part:=range strings.Split(p,"/"){if !ValidName(part){return false}}
 return len(p)<=500
}
func ValidMediaPath(p string)bool{if !ValidRelative(p){return false};for _,part:=range strings.Split(p,"/"){if strings.EqualFold(part,".qimeng-internal"){return false}};return true}

// Allocate returns the minimum free work number in one directory/family/media
// class and retains established width/separator. Different extensions share a class.
func Allocate(family,class string,names []string)(number,width int,separator string){
 occupied:=map[int]bool{};width=1;separator=" "
 sorted:=append([]string(nil),names...);sort.Strings(sorted)
 for _,name:=range sorted{if NumberClass(name)!=class{continue};w,ok:=ParseWork(name);if ok&&w.Family==family{occupied[w.Number]=true;if w.Width>width{width=w.Width};separator=w.Separator}}
 for number=1;occupied[number];number++{}
 return
}
func WorkFilename(family,separator string,number,width,page int,extension string)string{
 base:=fmt.Sprintf("%s%s%0*d",family,separator,width,number)
 if page>0{base+=fmt.Sprintf(" (%d)",page)}
 return base+strings.ToLower(extension)
}
