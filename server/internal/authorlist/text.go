// Package authorlist implements the documented author-list interchange grammar.
// It parses declarations and matches names, but never assigns database IDs or
// performs filesystem/API operations.
package authorlist

import (
 "errors"
 "fmt"
 "path"
 "regexp"
 "strconv"
 "strings"
 "unicode"
 "unicode/utf8"
)

const MaxTextBytes=8<<20
type Block struct{Names []string `json:"names"`;Sources []string `json:"sources"`;Works []string `json:"works"`;Order int `json:"order"`;Header string `json:"header,omitempty"`}
type Issue struct{Line int `json:"line"`;Code string `json:"code"`}
type Document struct{Format string `json:"format"`;Blocks []Block `json:"blocks"`;Issues []Issue `json:"issues"`}
var numericPage=regexp.MustCompile(`\([0-9]+\)`)
var trailingPage=regexp.MustCompile(`\([0-9]+\)$`)

func mediaExtension(value string)string{ext:=strings.ToLower(path.Ext(value));switch ext{case ".jpg",".jpeg",".png",".gif",".webp",".avif",".heic",".heif",".mp4",".m4v",".mov",".mkv",".webm",".avi",".wmv",".bmp",".svg",".tif",".tiff":return ext};return ""}
func allDigits(s string)bool{if s==""{return false};for _,r:=range s{if !unicode.IsDigit(r){return false}};return true}
func words(value string)[]string{
 runes:=[]rune(value);start:=0;out:=[]string{}
 for i:=0;i<len(runes);{if !unicode.IsSpace(runes[i]){i++;continue};end:=i;for end<len(runes)&&unicode.IsSpace(runes[end]){end++};if end-i>=2{if part:=strings.TrimSpace(string(runes[start:i]));part!=""{out=append(out,part)};start=end};i=end};if part:=strings.TrimSpace(string(runes[start:]));part!=""{out=append(out,part)};return out
}
func trimNotes(value string)string{
 pairs:=map[rune]rune{')':'(','）':'（',']':'[','】':'【'}
 for{runes:=[]rune(strings.TrimSpace(value));if len(runes)==0{return ""};open,ok:=pairs[runes[len(runes)-1]];if !ok{return string(runes)};depth:=0;cut:=-1;for i:=len(runes)-1;i>=0;i--{if runes[i]==runes[len(runes)-1]{depth++};if runes[i]==open{depth--;if depth==0{cut=i;break}}};if cut<0{return string(runes)};value=string(runes[:cut])}
}
// IdentityKey is a lookup key, never a persistent identity. A unique existing
// author may be reused; ambiguous names require an explicit server-side choice.
func IdentityKey(value string)string{var out strings.Builder;for _,r:=range value{switch{case r==' ':out.WriteByte('_');case r=='_'||unicode.IsLetter(r)||unicode.IsDigit(r):out.WriteRune(unicode.ToLower(r))}};return out.String()}
func Names(value string)([]string,error){
 if !utf8.ValidString(value)||utf8.RuneCountInString(value)>200{return nil,errors.New("author name exceeds text budget")};for _,r:=range value{if unicode.IsControl(r)&&r!='\t'{return nil,errors.New("author name contains control characters")}}
 result:=[]string{};seen:=map[string]bool{};for _,part:=range words(strings.ReplaceAll(value," / ","  ")){name:=trimNotes(part);if name==""||allDigits(name)||mediaExtension(name)!=""||name=="来源"||name=="出处"||name=="作品"||strings.Contains(name,"://")||IdentityKey(name)==""{return nil,errors.New("invalid author name")};for _,r:=range name{if unicode.IsControl(r){return nil,errors.New("author alias contains a control character")}};key:=IdentityKey(name);if !seen[key]{seen[key]=true;result=append(result,name)}};if len(result)==0||len(result)>20{return nil,errors.New("author aliases must contain 1 to 20 names")};return result,nil
}
func header(line string)([]string,bool,error){
 runes:=[]rune(line);i:=0;for i<len(runes)&&runes[i]>='0'&&runes[i]<='9'{i++};if i==0||i==len(runes){return nil,false,nil};start:=i;for i<len(runes)&&unicode.IsSpace(runes[i]){i++};if i-start<2||i==len(runes)||mediaExtension(line)!=""{return nil,false,nil};names,e:=Names(string(runes[i:]));return names,true,e
}
func marker(line,name string)(string,bool){if line==name{return "",true};if !strings.HasPrefix(line,name){return "",false};rest:=strings.TrimPrefix(line,name);r,_:=utf8.DecodeRuneInString(rest);if !unicode.IsSpace(r){return "",false};return strings.TrimSpace(rest),true}
func unique(values []string,value string)[]string{value=strings.TrimSpace(value);if value==""{return values};for _,old:=range values{if old==value{return values}};return append(values,value)}
func Parse(text string)(Document,error){
 result:=Document{Format:"names",Blocks:[]Block{},Issues:[]Issue{}};if len(text)>MaxTextBytes||!utf8.ValidString(text)||strings.ContainsRune(text,0){return result,errors.New("author text must be bounded UTF-8")};text=strings.TrimPrefix(text,"\ufeff");lines:=strings.Split(strings.ReplaceAll(text,"\r\n","\n"),"\n");current:=-1;section:="";foundHeader:=false
 for number,raw:=range lines{line:=strings.TrimSpace(raw);if line==""||allDigits(line){continue};if len(line)>16384{return result,fmt.Errorf("line %d exceeds text budget",number+1)};names,isHeader,e:=header(line);if e!=nil{return result,fmt.Errorf("line %d: %w",number+1,e)};if isHeader{foundHeader=true;result.Format="blocks";result.Blocks=append(result.Blocks,Block{Names:names,Sources:[]string{},Works:[]string{},Order:len(result.Blocks),Header:line});current=len(result.Blocks)-1;section="";if len(result.Blocks)>20000{return result,errors.New("too many author blocks")};continue};if current<0{continue}
  if rest,ok:=marker(line,"来源");ok{section="sources";for _,source:=range words(rest){result.Blocks[current].Sources=unique(result.Blocks[current].Sources,source)};continue};if rest,ok:=marker(line,"出处");ok{section="sources";for _,source:=range words(rest){result.Blocks[current].Sources=unique(result.Blocks[current].Sources,source)};continue};if rest,ok:=marker(line,"作品");ok{section="works";if rest!=""{result.Blocks[current].Works=unique(result.Blocks[current].Works,rest)};continue}
  switch section{case "sources":result.Blocks[current].Sources=unique(result.Blocks[current].Sources,strings.Trim(line,"`"));case "works":result.Blocks[current].Works=unique(result.Blocks[current].Works,line);default:if len(result.Issues)<100{result.Issues=append(result.Issues,Issue{number+1,"line_outside_section"})}}
 }
 if !foundHeader{seen:=map[string]bool{};for number,raw:=range lines{line:=strings.TrimSpace(raw);if line==""||allDigits(line){continue};names,e:=Names(line);if e!=nil{return result,fmt.Errorf("line %d: %w",number+1,e)};key:=IdentityKey(names[0]);if !seen[key]{seen[key]=true;result.Blocks=append(result.Blocks,Block{Names:names,Sources:[]string{},Works:[]string{},Order:len(result.Blocks)})}}}
 if len(result.Blocks)==0{return result,errors.New("author text contains no valid authors")};return result,nil
}
func normalized(value string)string{return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value)," ",""))}
func Matches(work,fileName string)bool{
 work=strings.TrimSpace(work);if work==""{return false};workExtension:=mediaExtension(work);fileExtension:=strings.ToLower(path.Ext(fileName));fileStem:=strings.TrimSuffix(fileName,path.Ext(fileName));workStem:=work;if workExtension!=""{if workExtension!=fileExtension{return false};workStem=strings.TrimSuffix(work,path.Ext(work))};key:=normalized(workStem);candidate:=normalized(fileStem);if key==candidate{return true};if workExtension!=""||numericPage.MatchString(workStem){return false};return trailingPage.ReplaceAllString(candidate,"")==key
}
// Render writes the user-confirmed block layout: three empty lines between
// blocks and one source marker with source terms separated by two spaces.
func Render(blocks []Block)string{
 var out strings.Builder;for index,block:=range blocks{if index>0{out.WriteString("\n\n\n")};if block.Header!=""{out.WriteString(block.Header)}else{out.WriteString(strconv.Itoa(index+1));out.WriteString("  ");out.WriteString(strings.Join(block.Names,"  "))};out.WriteByte('\n');if len(block.Sources)>0{out.WriteString("来源  ");out.WriteString(strings.Join(block.Sources,"  "));out.WriteByte('\n')};if len(block.Works)>0{out.WriteString("作品\n");for _,work:=range block.Works{out.WriteString(work);out.WriteByte('\n')}}};return out.String()
}
