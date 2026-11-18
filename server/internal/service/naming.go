package service

import (
 "context"
 "encoding/json"
 "errors"
 "path"
 "sort"
 "strings"

 "qimeng.local/next/server/internal/domain"
 "qimeng.local/next/server/internal/model"
 "qimeng.local/next/server/internal/store"
)

const NamingPolicyVersion="2"
type NamingContext struct{PolicyVersion string `json:"policyVersion"`;LibraryID string `json:"libraryId"`;Directory string `json:"directory"`;Family string `json:"family"`;NumberClass string `json:"numberClass"`;NextWorkNumber int `json:"nextWorkNumber"`;CurrentWorkNumber int `json:"currentWorkNumber,omitempty"`;NextPage int `json:"nextPage,omitempty"`;NextChapter int `json:"nextChapter,omitempty"`;Width int `json:"width"`;Separator string `json:"separator"`;NamespaceFingerprint string `json:"namespaceFingerprint"`;Reserved bool `json:"reserved"`}
type NamingInputItem struct{FileName string `json:"fileName"`;GroupKey string `json:"groupKey,omitempty"`;Page int `json:"page,omitempty"`;Note string `json:"note,omitempty"`}
type NamingPreviewInput struct{Intent string `json:"intent"`;Directory string `json:"directory"`;Family string `json:"family"`;MediaType string `json:"mediaType"`;SourceIDs []string `json:"sourceIds"`;CharacterIDs []string `json:"characterIds"`;CollectionID string `json:"collectionId"`;AssetID string `json:"assetId"`;ExpectedRevision int64 `json:"expectedRevision"`;Items []NamingInputItem `json:"items"`}
type NamingPreviewItem struct{FileName string `json:"fileName"`;RelativePath string `json:"relativePath"`;NumberClass string `json:"numberClass"`;WorkNumber int `json:"workNumber,omitempty"`;Page int `json:"page,omitempty"`}
type NamingPreview struct{PolicyVersion string `json:"policyVersion"`;NamespaceFingerprint string `json:"namespaceFingerprint"`;Reserved bool `json:"reserved"`;Items []NamingPreviewItem `json:"items"`}
type NameSuggestion struct{Family string `json:"family"`;SuggestedName string `json:"suggestedName"`;NumberClass string `json:"numberClass"`;WorkNumber int `json:"workNumber"`}
type collectionSequence struct{Collection model.Collection;Directory,Family,Separator string;Number,Width,MaxPage int}
func numberClass(mediaType string)(string,error){switch mediaType{case "","image","animated_image":return "image",nil;case "video":return "video",nil};return "",Fail(400,"invalid_media_type","命名媒体类别无效。")}
func namingFingerprint(libraryID,directory string,names []string)string{copyNames:=append([]string(nil),names...);sort.Strings(copyNames);raw,_:=json.Marshal(struct{Library,Directory string;Names []string}{libraryID,directory,copyNames});return digest(raw)}
func(s *Service)sequence(ctx context.Context,collectionID string)(collectionSequence,error){
 var sequence collectionSequence;c,e:=store.Get[model.Collection](ctx,s.DB.DB,"SELECT data FROM collections WHERE id=?",collectionID);if e!=nil{return sequence,Normalize(e)};sequence.Collection=c
 if c.Kind=="short_drama"{sequence.Directory=c.Name;sequence.Family=c.Name;sequence.Width=1;sequence.Separator=" ";sequence.Number,e=s.nextEpisode(ctx,c.ID);return sequence,e}
 members,e:=store.List[model.Asset](ctx,s.DB.DB,"SELECT data FROM assets WHERE collection_id=? AND trashed=0 ORDER BY relative_path LIMIT 10001",collectionID);if e!=nil{return sequence,e};if len(members)==0{return sequence,Fail(409,"empty_group","空组图需要先创建首个作品，不能猜测续页。")};if len(members)>10000{return sequence,Fail(422,"group_budget","组图成员超过单次命名预算。")}
 for i,member:=range members{work,ok:=domain.ParseWork(member.FileName);if !ok||work.Page<1||domain.NumberClass(member.FileName)!="image"{return sequence,Fail(409,"group_name_conflict","已有组图命名需先核实。")};if i==0{sequence.Directory=path.Dir(member.RelativePath);if sequence.Directory=="."{sequence.Directory=""};sequence.Family=work.Family;sequence.Number=work.Number;sequence.Width=work.Width;sequence.Separator=work.Separator}else if sequence.Family!=work.Family||sequence.Number!=work.Number||path.Clean(sequence.Directory)!=path.Dir(member.RelativePath){return sequence,Fail(409,"group_name_conflict","已有组图包含不同作品号或目录。")};if work.Page>sequence.MaxPage{sequence.MaxPage=work.Page}}
 names,e:=s.directoryNames(ctx,c.LibraryID,sequence.Directory);if e!=nil{return sequence,e};for _,name:=range names{work,ok:=domain.ParseWork(name);if ok&&domain.NumberClass(name)=="image"&&work.Family==sequence.Family&&work.Number==sequence.Number&&work.Page>sequence.MaxPage{sequence.MaxPage=work.Page}}
 return sequence,nil
}
// An empty collection has no previous work to extend. Allocate its first work
// with the same namespace used for ordinary upload, then append pages from one.
func(s *Service)appendSequence(ctx context.Context,collectionID,directory,family string)(collectionSequence,error){
 sequence,e:=s.sequence(ctx,collectionID);if e==nil{return sequence,nil};var problem *Error;if !errors.As(e,&problem)||problem.Code!="empty_group"{return sequence,e}
 if family==""{family=sequence.Collection.Name};if !domain.ValidName(family)||!domain.ValidMediaPath(directory){return sequence,Fail(400,"invalid_name","空组图需要合法的基名与目录。")};names,e:=s.directoryNames(ctx,sequence.Collection.LibraryID,directory);if e!=nil{return sequence,e};sequence.Directory=directory;sequence.Family=family;sequence.Number,sequence.Width,sequence.Separator=domain.Allocate(family,"image",names);return sequence,nil
}
func(s *Service)namingContext(ctx context.Context,libraryID,directory,family,mediaType,collectionID string)(NamingContext,error){
 result:=NamingContext{PolicyVersion:NamingPolicyVersion,LibraryID:libraryID,Directory:directory,Family:strings.TrimSpace(family)};if _,e:=s.library(ctx,libraryID);e!=nil{return result,e};if !domain.ValidMediaPath(directory){return result,Fail(400,"invalid_path","命名目录无效。")};class,e:=numberClass(mediaType);if e!=nil{return result,e};result.NumberClass=class
 if collectionID!=""{sequence,e:=s.appendSequence(ctx,collectionID,directory,result.Family);if e!=nil{return result,e};if sequence.Collection.LibraryID!=libraryID{return result,Fail(409,"collection_library_mismatch","集合属于其他库。")};result.Directory=sequence.Directory;result.Family=sequence.Family;result.Width=sequence.Width;result.Separator=sequence.Separator;if sequence.Collection.Kind=="short_drama"{result.NumberClass="video";result.NextChapter=sequence.Number}else{result.NumberClass="image";result.CurrentWorkNumber=sequence.Number;result.NextPage=sequence.MaxPage+1}}
 if !domain.ValidName(result.Family){return result,Fail(400,"family_required","请提供合法基名或选择已有集合。")};names,e:=s.directoryNames(ctx,libraryID,result.Directory);if e!=nil{return result,e};number,width,separator:=domain.Allocate(result.Family,result.NumberClass,names);result.NextWorkNumber=number;if collectionID==""{result.Width=width;result.Separator=separator};if result.NextChapter>0{result.NextWorkNumber=result.NextChapter};result.NamespaceFingerprint=namingFingerprint(libraryID,result.Directory,names);return result,nil
}
func(s *Service)NamingContext(ctx context.Context,libraryID,directory,family,mediaType,collectionID string)(NamingContext,error){s.mu.Lock();defer s.mu.Unlock();return s.namingContext(ctx,libraryID,directory,family,mediaType,collectionID)}
func(s *Service)SuggestNames(ctx context.Context,libraryID,directory,query,mediaType string)(map[string]any,error){
 s.mu.Lock();defer s.mu.Unlock();if _,e:=s.library(ctx,libraryID);e!=nil{return nil,e};if !domain.ValidMediaPath(directory)||len(query)>200{return nil,Fail(400,"invalid_query","命名目录或查询无效。")};class,e:=numberClass(mediaType);if e!=nil{return nil,e};names,e:=s.directoryNames(ctx,libraryID,directory);if e!=nil{return nil,e};query=strings.ToLower(strings.TrimSpace(query));if query==""{return map[string]any{"policyVersion":NamingPolicyVersion,"items":[]NameSuggestion{},"truncated":false},nil}
 sort.Strings(names);type candidate struct{family,extension string;count,tier int};byFamily:=map[string]*candidate{}
 for _,name:=range names{if domain.NumberClass(name)!=class{continue};work,ok:=domain.ParseWork(name);if !ok||!domain.ValidName(work.Family){continue};lower:=strings.ToLower(work.Family);tier:=0;if strings.HasPrefix(lower,query){tier=1}else{all:=true;for _,token:=range strings.Fields(query){if !strings.Contains(lower,token){all=false;break}};if all{tier=2}else if strings.Contains(lower,query){tier=3}};if tier==0{continue};c:=byFamily[work.Family];if c==nil{c=&candidate{family:work.Family,extension:path.Ext(name),tier:tier};byFamily[work.Family]=c};c.count++}
 values:=[]*candidate{};for _,c:=range byFamily{values=append(values,c)};sort.Slice(values,func(i,j int)bool{if values[i].tier!=values[j].tier{return values[i].tier<values[j].tier};if values[i].count!=values[j].count{return values[i].count>values[j].count};return values[i].family<values[j].family})
 clusters:=map[string][]*candidate{};order:=[]string{};for _,c:=range values{key:=c.family;for at,r:=range c.family{if r==' '&&byFamily[c.family[:at]]!=nil{key=c.family[:at];break}};if _,ok:=clusters[key];!ok{order=append(order,key)};clusters[key]=append(clusters[key],c)}
 suggestions:=[]NameSuggestion{};for round:=0;len(suggestions)<8;round++{added:=false;for _,key:=range order{if round>=len(clusters[key]){continue};c:=clusters[key][round];number,width,separator:=domain.Allocate(c.family,class,names);suggestions=append(suggestions,NameSuggestion{Family:c.family,SuggestedName:domain.WorkFilename(c.family,separator,number,width,0,c.extension),NumberClass:class,WorkNumber:number});added=true;if len(suggestions)==8{break}};if !added{break}}
 return map[string]any{"policyVersion":NamingPolicyVersion,"items":suggestions,"truncated":len(values)>len(suggestions)},nil
}
func(s *Service)PreviewNames(ctx context.Context,libraryID string,in NamingPreviewInput)(NamingPreview,error){
 s.mu.Lock();defer s.mu.Unlock();return s.previewNamesLocked(ctx,libraryID,in)
}
func(s *Service)previewNamesLocked(ctx context.Context,libraryID string,in NamingPreviewInput)(NamingPreview,error){
 out:=NamingPreview{PolicyVersion:NamingPolicyVersion,Items:[]NamingPreviewItem{}};if _,e:=s.library(ctx,libraryID);e!=nil{return out,e};if len(in.Items)<1||len(in.Items)>100||!domain.ValidMediaPath(in.Directory){return out,Fail(400,"invalid_preview","命名预览需要有效目录和 1–100 项输入。")}
 taxonomy:=model.Asset{};if e:=s.resolveTaxonomy(ctx,&taxonomy,in.SourceIDs,in.CharacterIDs);e!=nil{return out,e};family:=domain.NamingFamily(taxonomy.Source,taxonomy.Characters,in.Family);directory:=in.Directory
 var sequence collectionSequence
 if in.Intent=="append_pages"||in.Intent=="append_drama"{var e error;sequence,e=s.appendSequence(ctx,in.CollectionID,in.Directory,family);if e!=nil{return out,e};if sequence.Collection.LibraryID!=libraryID{return out,Fail(409,"collection_library_mismatch","集合属于其他库。")};if (in.Intent=="append_pages"&&sequence.Collection.Kind!="image_set")||(in.Intent=="append_drama"&&sequence.Collection.Kind!="short_drama"){return out,Fail(409,"collection_kind_conflict","命名意图与集合类型不一致。")};family=sequence.Family;directory=sequence.Directory}
 names,e:=s.directoryNames(ctx,libraryID,directory);if e!=nil{return out,e};out.NamespaceFingerprint=namingFingerprint(libraryID,directory,names)
 if in.Intent=="rename_exact"{if len(in.Items)!=1{return out,Fail(400,"rename_single_asset","精确改名只作用于一个明确资产。")};asset,e:=s.Asset(ctx,in.AssetID);if e!=nil{return out,e};if asset.LibraryID!=libraryID||asset.Revision!=in.ExpectedRevision{return out,Conflict(asset.Revision)};name:=in.Items[0].FileName;_,mime,ok:=domain.MediaType(name);if !domain.ValidName(name)||!ok||mime!=asset.MimeType{return out,Fail(400,"invalid_name","精确改名不得改变文件格式。")};relative:=path.Join(directory,name);if e=s.checkPath(ctx,libraryID,relative,asset.ID);e!=nil{return out,e};if relative!=asset.RelativePath{for _,existing:=range names{if existing==name{return out,Fail(409,"path_occupied","目标已被占用。")}}};out.Items=append(out.Items,NamingPreviewItem{FileName:name,RelativePath:relative,NumberClass:domain.NumberClass(name)});return out,nil}
 if in.Intent!="new_work"&&in.Intent!="append_pages"&&in.Intent!="append_drama"{return out,Fail(400,"invalid_naming_intent","请明确新增作品、补页、短剧追加或精确改名。")};if !domain.ValidName(family){return out,Fail(400,"family_required","缺少合法命名基名。")}
 type group struct{number,width,lastPage int;separator,class string};groups:=map[string]*group{}
 for index,input:=range in.Items{if input.FileName==""||len(input.FileName)>1024||strings.ContainsAny(input.FileName,"/\\:\x00")||len(input.Note)>10000||len(input.GroupKey)>200||input.Page<0{return out,Fail(400,"invalid_name","原名、组标识、页序或备注无效。")};class:=domain.NumberClass(input.FileName);if class==""{return out,Fail(415,"unsupported_media","不支持该命名媒体类型。")};if in.MediaType!=""{expected,e:=numberClass(in.MediaType);if e!=nil{return out,e};if expected!=class{return out,Fail(409,"media_type_conflict","输入文件与指定命名类别不一致。")}};pageNumber:=0;number,width,separator:=domain.Allocate(family,class,names)
  if in.Intent=="append_pages"{if class!="image"{return out,Fail(400,"mixed_group","补页只能接收图片。")};number=sequence.Number;width=sequence.Width;separator=sequence.Separator;pageNumber=sequence.MaxPage+index+1;if input.Page>0&&input.Page!=pageNumber{return out,Fail(409,"page_conflict","补页从已有最大页之后追加，不补洞。")}}
  if in.Intent=="append_drama"{if class!="video"{return out,Fail(400,"drama_requires_video","短剧追加只能接收视频。")};number=sequence.Number+index;width=sequence.Width;separator=sequence.Separator}
  if in.Intent=="new_work"&&input.GroupKey!=""{g:=groups[input.GroupKey];if g==nil{g=&group{number:number,width:width,separator:separator,class:class};groups[input.GroupKey]=g};if class!="image"||g.class!=class||input.Page!=g.lastPage+1{return out,Fail(400,"group_order_required","组图必须明确页序并保持图片类别。")};g.lastPage=input.Page;number=g.number;width=g.width;separator=g.separator;pageNumber=input.Page}
  name:=domain.WorkFilename(family,separator,number,width,pageNumber,path.Ext(input.FileName));if in.Intent=="append_drama"&&input.Note!=""{extension:=path.Ext(name);name=strings.TrimSuffix(name,extension)+" "+safeNote(input.Note)+extension};if !domain.ValidName(name){return out,Fail(400,"invalid_name","规范名称过长或包含非法字符。")};for _,existing:=range names{if existing==name{return out,Fail(409,"path_occupied","目标已被占用。")}};out.Items=append(out.Items,NamingPreviewItem{FileName:name,RelativePath:path.Join(directory,name),NumberClass:class,WorkNumber:number,Page:pageNumber});names=append(names,name)
 }
 return out,nil
}
