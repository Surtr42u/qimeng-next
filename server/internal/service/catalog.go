package service

import (
 "context"
 "encoding/base64"
 "encoding/json"
 "fmt"
 "net/url"
 "strings"
 "time"

 "qimeng.local/next/server/internal/domain"
 "qimeng.local/next/server/internal/model"
 "qimeng.local/next/server/internal/store"
)

func(s *Service)Libraries(ctx context.Context)(model.Page[model.Library],error){a,e:=store.List[model.Library](ctx,s.DB.DB,"SELECT data FROM libraries ORDER BY name,id LIMIT 100");return model.Page[model.Library]{Items:a},e}
func(s *Service)CreateLibrary(ctx context.Context,w *Write,name,kind string,location ...string)(model.Library,error){
 if err:=Authorize(w.Principal,"admin","");err!=nil{return model.Library{},err}
 name=strings.TrimSpace(name);if name==""||len(name)>120||(kind!="regular"&&kind!="cos"){return model.Library{},Fail(400,"invalid_library","库名称或分类无效。")}
 storageID,relative:="data","";if len(location)>0&&location[0]!=""{storageID=location[0]};if len(location)>1{relative=location[1]};if !domain.ValidMediaPath(relative){return model.Library{},Fail(400,"invalid_path","库位置须为受信逻辑存储根内的相对目录。")}
 l:=model.Library{ID:w.ID("library"),Name:name,Kind:kind,Enabled:true,StorageID:storageID,RootRelativePath:relative,Revision:1,CreatedAt:time.Now().UTC()};if storageID=="data"{l.RootRelativePath="libraries/"+l.ID}
 if e:=s.Root.RegisterLibrary(l.ID,l.StorageID,l.RootRelativePath);e!=nil{return l,Fail(409,"storage_registration_conflict","逻辑存储未配置、路径不可用或与已登记库重叠。")}
 w.rollback=append(w.rollback,func(){s.Root.UnregisterLibrary(l.ID)})
 if err:=s.Root.MkdirAll("libraries/"+l.ID,0700);err!=nil{return l,err}
 raw,e:=store.Encode(l);if e!=nil{return l,e};q,e:=w.Q();if e!=nil{return l,e};_,e=q.ExecContext(ctx,"INSERT INTO libraries(id,name,kind,data) VALUES(?,?,?,?)",l.ID,l.Name,l.Kind,raw);return l,e
}
func(s *Service)Storages()any{return map[string]any{"items":s.Root.Storages()}}
func(s *Service)library(ctx context.Context,id string)(model.Library,error){a,e:=store.Get[model.Library](ctx,s.DB.DB,"SELECT data FROM libraries WHERE id=?",id);return a,Normalize(e)}
const assetJSON="json_set(a.data,'$.authorName',au.name,'$.liked',json(CASE WHEN EXISTS(SELECT 1 FROM likes lk WHERE lk.asset_id=a.id AND lk.day=?) THEN 'true' ELSE 'false' END),'$.likeCount',(SELECT count(*) FROM likes lk WHERE lk.asset_id=a.id),'$.source',CASE WHEN json_array_length(json_extract(a.data,'$.sourceIds'))>0 THEN (SELECT group_concat(name,'+') FROM (SELECT s.name FROM json_each(a.data,'$.sourceIds') j JOIN sources s ON s.id=j.value ORDER BY j.key)) ELSE json_extract(a.data,'$.source') END,'$.characters',json(CASE WHEN json_array_length(json_extract(a.data,'$.characterIds'))>0 THEN (SELECT json_group_array(name) FROM (SELECT c.name FROM json_each(a.data,'$.characterIds') j JOIN characters c ON c.id=j.value ORDER BY j.key)) ELSE COALESCE(json_extract(a.data,'$.characters'),'[]') END))"
func(s *Service)Asset(ctx context.Context,id string)(model.Asset,error){a,e:=store.Get[model.Asset](ctx,s.DB.DB,"SELECT "+assetJSON+" FROM assets a JOIN authors au ON au.id=a.author_id WHERE a.id=?",time.Now().Format("2006-01-02"),id);return a,Normalize(e)}
type AssetFilter struct { LibraryID,Q,MediaType,Kind,AuthorID,CollectionID,Tag,Sort,Cursor string; Favorite,Liked *bool; Trashed bool; Limit int }
type cursorValue struct { Offset int `json:"o"`; Filter string `json:"f"` }
func pageOffset(cursor,filter string)(int,error){
 if cursor==""{return 0,nil};b,e:=base64.RawURLEncoding.DecodeString(cursor);if e!=nil{return 0,Fail(400,"invalid_cursor","分页游标无效。")};var c cursorValue;if json.Unmarshal(b,&c)!=nil||c.Offset<0||c.Offset>100000000||c.Filter!=digest([]byte(filter)){return 0,Fail(400,"invalid_cursor","分页条件已变化，请重新加载。")};return c.Offset,nil
}
func nextCursor(offset int,filter string)string{b,_:=json.Marshal(cursorValue{offset,digest([]byte(filter))});return base64.RawURLEncoding.EncodeToString(b)}
func limited(n,defaultN int)(int,error){if n==0{return defaultN,nil};if n<1||n>100{return 0,Fail(400,"invalid_limit","每页须为 1–100 项。")};return n,nil}
func paged[T any](items []T,limit,offset int,filter string)model.Page[T]{p:=model.Page[T]{Items:items};if len(items)>limit{p.Items=items[:limit];p.NextCursor=nextCursor(offset+limit,filter)};return p}
func like(v string)string{return "%"+strings.NewReplacer("\\","\\\\","%","\\%","_","\\_").Replace(v)+"%"}
func(s *Service)Assets(ctx context.Context,f AssetFilter)(model.Page[model.Asset],error){
 n,e:=limited(f.Limit,50);if e!=nil{return model.Page[model.Asset]{},e};keyf:=f;keyf.Cursor="";keyf.Limit=0;b,_:=json.Marshal(keyf);key:=string(b);offset,e:=pageOffset(f.Cursor,key);if e!=nil{return model.Page[model.Asset]{},e}
 where:=[]string{"a.trashed=?"};args:=[]any{time.Now().Format("2006-01-02"),f.Trashed}
 for _,v:=range []struct{col,val string}{{"a.library_id",f.LibraryID},{"a.media_type",f.MediaType},{"a.collection_id",f.CollectionID},{"l.kind",f.Kind}}{if v.val!=""{where=append(where,v.col+"=?");args=append(args,v.val)}}
 if f.AuthorID!=""{where=append(where,"EXISTS(SELECT 1 FROM asset_authors aa WHERE aa.asset_id=a.id AND aa.author_id=?)");args=append(args,f.AuthorID)}
 if f.Q!=""{if len(f.Q)>200{return model.Page[model.Asset]{},Fail(400,"query_too_long","搜索词过长。")};where=append(where,"(a.file_name LIKE ? ESCAPE '\\' OR au.name LIKE ? ESCAPE '\\' OR json_extract(a.data,'$.source') LIKE ? ESCAPE '\\' OR json_extract(a.data,'$.characters') LIKE ? ESCAPE '\\')");for i:=0;i<4;i++{args=append(args,like(f.Q))}}
 if f.Tag!=""{where=append(where,"EXISTS(SELECT 1 FROM asset_tags t WHERE t.asset_id=a.id AND t.tag=?)");args=append(args,f.Tag)}
 if f.Favorite!=nil{where=append(where,"a.favorite=?");args=append(args,*f.Favorite)};if f.Liked!=nil{where=append(where,"EXISTS(SELECT 1 FROM likes lk WHERE lk.asset_id=a.id)=?");args=append(args,*f.Liked)}
 order:="a.created_at DESC,a.id DESC";switch f.Sort{case "","added_desc":case "updated_desc":order="a.updated_at DESC,a.id DESC";case "name_asc":order="a.file_name,a.id";default:return model.Page[model.Asset]{},Fail(400,"invalid_sort","不支持该排序方式。")}
 args=append(args,n+1,offset)
 values,e:=store.List[model.Asset](ctx,s.DB.DB,"SELECT "+assetJSON+" FROM assets a JOIN libraries l ON l.id=a.library_id JOIN authors au ON au.id=a.author_id WHERE "+strings.Join(where," AND ")+" ORDER BY "+order+" LIMIT ? OFFSET ?",args...)
 return paged(values,n,offset,key),e
}
type AssetUpdate struct { ExpectedRevision int64 `json:"expectedRevision"`; ExpectedAuthorsRevision int64 `json:"expectedAuthorsRevision,omitempty"`; AuthorID,Note,Source *string; AuthorIDs *[]string `json:"authorIds"`; Characters *[]string `json:"characters"`; SourceIDs *[]string `json:"sourceIds"`;CharacterIDs *[]string `json:"characterIds"`;AcquisitionSources *[]string `json:"acquisitionSources"` }
func(s *Service)UpdateAsset(ctx context.Context,w *Write,id string,in AssetUpdate)(model.Asset,error){
 if e:=Authorize(w.Principal,"admin","");e!=nil{return model.Asset{},e};a,e:=s.Asset(ctx,id);if e!=nil{return a,e};if a.Revision!=in.ExpectedRevision{return a,Conflict(a.Revision)}
 if a.Trashed{return a,Fail(409,"asset_trashed","请先恢复媒体。")}
 if in.AuthorID!=nil||in.AuthorIDs!=nil{if in.ExpectedAuthorsRevision>0&&in.ExpectedAuthorsRevision!=a.AuthorsRevision{return a,Conflict(a.AuthorsRevision)};ids:=[]string{};if in.AuthorIDs!=nil{ids=*in.AuthorIDs}else{ids=[]string{*in.AuthorID}};authors,e:=s.resolveAuthors(ctx,a.LibraryID,ids);if e!=nil{return a,e};setAuthors(&a,authors);a.AuthorsRevision++}
 if in.Note!=nil{if len(*in.Note)>10000{return a,Fail(400,"invalid_note","备注过长。")};a.Note=*in.Note};if in.Source!=nil{if len(*in.Source)>200{return a,Fail(400,"invalid_source","出处名称过长。")};a.Source=*in.Source}
 if in.Characters!=nil{v,e:=cleanStrings(*in.Characters,50,200);if e!=nil{return a,e};a.Characters=v}
 if in.SourceIDs!=nil||in.CharacterIDs!=nil{sources:=a.SourceIDs;characters:=a.CharacterIDs;if in.SourceIDs!=nil{sources=*in.SourceIDs};if in.CharacterIDs!=nil{characters=*in.CharacterIDs};if e=s.resolveTaxonomy(ctx,&a,sources,characters);e!=nil{return a,e}}
 if in.AcquisitionSources!=nil{a.AcquisitionSources,e=cleanStrings(*in.AcquisitionSources,100,2000);if e!=nil{return a,e}}
 a.MetadataSource="human";a.Revision++;a.UpdatedAt=time.Now().UTC();q,e:=w.Q();if e!=nil{return a,e};return a,store.SaveAsset(ctx,q,a)
}
func cleanStrings(values []string,maxCount,maxLen int)([]string,error){if values==nil{return nil,Fail(400,"missing_array","必须显式提供数组；清空时提供空数组。")};if len(values)>maxCount{return nil,Fail(400,"too_many_values","项目数量超出限制。")};out:=[]string{};seen:=map[string]bool{};for _,v:=range values{v=strings.TrimSpace(v);if v==""||len(v)>maxLen||strings.ContainsRune(v,'\x00'){return nil,Fail(400,"invalid_value","名称为空或过长。")};if !seen[v]{seen[v]=true;out=append(out,v)}};return out,nil}
func(s *Service)Tags(ctx context.Context,cursor string,limit int)(model.Page[string],error){page,e:=s.TagVocabulary(ctx,cursor,limit);if e!=nil{return model.Page[string]{},e};out:=[]string{};for _,tag:=range page.Items{out=append(out,tag.Name)};return model.Page[string]{Items:out,NextCursor:page.NextCursor},nil}
func(s *Service)UpdateTags(ctx context.Context,w *Write,id string,revision int64,tags []string)(model.Asset,error){
 if e:=Authorize(w.Principal,"admin","");e!=nil{return model.Asset{},e};a,e:=s.Asset(ctx,id);if e!=nil{return a,e};if a.Trashed{return a,Fail(409,"asset_trashed","请先恢复媒体。")};if a.TagsRevision!=revision{return a,Conflict(a.TagsRevision)}
 a.Tags,e=cleanStrings(tags,100,100);if e!=nil{return a,e}
 for _,tag:=range a.Tags{var deleted int;e=s.DB.DB.QueryRowContext(ctx,"SELECT COALESCE((SELECT deleted FROM tags WHERE name=?),0)",tag).Scan(&deleted);if e!=nil{return a,e};if deleted!=0{return a,Fail(409,"tag_deleted","标签已被删除，请刷新词表；重新创建须使用明确的新建标签操作。")}}
 a.TagsRevision++;a.Revision++;a.UpdatedAt=time.Now().UTC();a.TagSetUpdatedAt=&a.UpdatedAt;a.TagSetSource="human";q,e:=w.Q();if e!=nil{return a,e};return a,store.SaveAsset(ctx,q,a)
}
func(s *Service)Reaction(ctx context.Context,w *Write,id string,favorite,liked bool)(model.Asset,error){
 if e:=Authorize(w.Principal,"admin","");e!=nil{return model.Asset{},e};a,e:=s.Asset(ctx,id);if e!=nil{return a,e};if a.Trashed{return a,Fail(409,"asset_trashed","请先恢复媒体。")};now:=time.Now().UTC();if a.Favorite!=favorite{a.FavoriteUpdatedAt=&now};a.Favorite=favorite;a.Revision++;a.UpdatedAt=now;q,e:=w.Q();if e!=nil{return a,e}
 if liked{r,e:=q.ExecContext(ctx,"INSERT OR IGNORE INTO likes(asset_id,day,created_at) VALUES(?,?,?)",a.ID,time.Now().Format("2006-01-02"),time.Now().UTC().Format(time.RFC3339Nano));if e!=nil{return a,e};n,e:=r.RowsAffected();if e!=nil{return a,e};a.LikeCount+=n;a.Liked=true}
 return a,store.SaveAsset(ctx,q,a)
}
func(s *Service)Author(ctx context.Context,id string)(model.Author,error){a,e:=store.Get[model.Author](ctx,s.DB.DB,"SELECT data FROM authors WHERE id=?",id);return a,Normalize(e)}
func(s *Service)Authors(ctx context.Context,query,cursor string,limit int,following bool)(model.Page[model.Author],error){n,e:=limited(limit,20);if e!=nil{return model.Page[model.Author]{},e};key:=fmt.Sprintf("authors:%s:%t",query,following);o,e:=pageOffset(cursor,key);if e!=nil{return model.Page[model.Author]{},e};a,e:=store.List[model.Author](ctx,s.DB.DB,"SELECT data FROM authors WHERE (name LIKE ? ESCAPE '\\' OR json_extract(data,'$.aliases') LIKE ? ESCAPE '\\') AND (?=0 OR following=1) ORDER BY name,id LIMIT ? OFFSET ?",like(query),like(query),following,n+1,o);return paged(a,n,o,key),e}
type AuthorInput struct{Name string `json:"name"`;Kind string `json:"kind"`;Biography string `json:"biography"`;Aliases []string `json:"aliases"`;Homepage string `json:"homepage"`;BatchID string `json:"batchId"`}
func validURL(value string)bool{if value==""{return true};u,e:=url.Parse(value);return e==nil&&(u.Scheme=="https"||u.Scheme=="http")&&u.Host!=""&&u.User==nil}
func(s *Service)CreateAuthor(ctx context.Context,w *Write,in AuthorInput)(model.Author,error){
 if e:=Authorize(w.Principal,"organize",in.BatchID);e!=nil{return model.Author{},e}
 if in.BatchID!=""{b,e:=s.Batch(ctx,in.BatchID);if e!=nil{return model.Author{},e};if b.State=="cancelled"||b.State=="completed"{return model.Author{},Fail(409,"batch_closed","接收批次已结束。")};l,e:=s.library(ctx,b.LibraryID);if e!=nil{return model.Author{},e};if in.Kind!=""&&in.Kind!=l.Kind{return model.Author{},Fail(409,"author_kind_conflict","作者分区须属于本批库。")};in.Kind=l.Kind}
 if in.Kind==""{in.Kind="regular"};if in.Kind!="regular"&&in.Kind!="cos"{return model.Author{},Fail(400,"invalid_author_kind","作者分区无效。")}
 in.Name=strings.TrimSpace(in.Name);if in.Name==""||len(in.Name)>200||len(in.Biography)>20000||!validURL(in.Homepage)||len(in.Homepage)>2000{return model.Author{},Fail(400,"invalid_author","作者名称或资料无效。")}
 if in.Aliases==nil{in.Aliases=[]string{}};aliases,e:=cleanStrings(in.Aliases,100,200);if e!=nil{return model.Author{},e}
 var exists int;if e=s.DB.DB.QueryRowContext(ctx,"SELECT COUNT(*) FROM authors WHERE name=? AND (json_extract(data,'$.kind')=? OR id='unknown')",in.Name,in.Kind).Scan(&exists);e!=nil{return model.Author{},e};if exists>0{return model.Author{},Fail(409,"ambiguous_author","已存在同名作者或受保护的未知占位，请选择稳定身份，不自动创建。")}
 if !w.Principal.Has("admin"){
  var available,created int
  if e=s.DB.DB.QueryRowContext(ctx,"SELECT COUNT(*) FROM uploads WHERE batch_id=? AND state='staged'",in.BatchID).Scan(&available);e!=nil{return model.Author{},e}
  if e=s.DB.DB.QueryRowContext(ctx,"SELECT COUNT(*) FROM authors WHERE json_extract(data,'$.originBatchId')=?",in.BatchID).Scan(&created);e!=nil{return model.Author{},e}
  if available==0||created>=available{return model.Author{},Fail(409,"author_scope_limit","本批没有可关联的新输入，不能额外创建作者。")}
 }
 now:=time.Now().UTC();a:=model.Author{ID:w.ID("author"),Kind:in.Kind,Name:in.Name,Biography:in.Biography,Aliases:aliases,Homepage:in.Homepage,Revision:1,CreatedAt:now,UpdatedAt:now,FieldStatus:map[string]string{"identity":"confirmed","biography":"missing","homepage":"missing"}}
 if in.Biography!=""{a.FieldStatus["biography"]="confirmed"};if in.Homepage!=""{a.FieldStatus["homepage"]="confirmed"}
 raw,e:=store.Encode(a);if e!=nil{return a,e};q,e:=w.Q();if e!=nil{return a,e};_,e=q.ExecContext(ctx,"INSERT INTO authors(id,name,following,data) VALUES(?,?,0,json_set(?,'$.originBatchId',?))",a.ID,a.Name,raw,in.BatchID);return a,e
}
type AuthorUpdate struct{ExpectedRevision int64 `json:"expectedRevision"`;Name,Biography,Homepage *string;Aliases *[]string;Following *bool}
func(s *Service)UpdateAuthor(ctx context.Context,w *Write,id string,in AuthorUpdate)(model.Author,error){
 task,e:=s.authorizeAuthorUpdate(ctx,w.Principal,id,in);if e!=nil{return model.Author{},e};a,e:=s.Author(ctx,id);if e!=nil{return a,e};if a.Revision!=in.ExpectedRevision{return a,Conflict(a.Revision)};if a.Placeholder{return a,Fail(409,"placeholder_protected","未知作者占位不能改为真实作者。请修改媒体关联。")}
 if in.Name!=nil{v:=strings.TrimSpace(*in.Name);if v==""||len(v)>200{return a,Fail(400,"invalid_author","作者名称无效。")};var exists int;if e=s.DB.DB.QueryRowContext(ctx,"SELECT COUNT(*) FROM authors WHERE name=? AND id!=? AND (json_extract(data,'$.kind')=? OR id='unknown')",v,a.ID,a.Kind).Scan(&exists);e!=nil{return a,e};if exists>0{return a,Fail(409,"ambiguous_author","名称与现有作者或未知占位冲突，请保留稳定身份。")};a.Name=v}
 if in.Biography!=nil{if len(*in.Biography)>20000{return a,Fail(400,"invalid_author","简介过长。")};a.Biography=*in.Biography;a.FieldStatus["biography"]="confirmed"}
 if in.Homepage!=nil{if !validURL(*in.Homepage)||len(*in.Homepage)>2000{return a,Fail(400,"invalid_author","主页地址无效。")};a.Homepage=*in.Homepage;a.FieldStatus["homepage"]="confirmed"}
 if in.Aliases!=nil{a.Aliases,e=cleanStrings(*in.Aliases,100,200);if e!=nil{return a,e}};if in.Following!=nil{now:=time.Now().UTC();a.FollowingUpdatedAt=&now;a.Following=*in.Following}
 if task!=nil{if in.Biography!=nil{a.FieldStatus["biography"]="candidate"};if in.Homepage!=nil{a.FieldStatus["homepage"]="candidate"}}
 a.Revision++;a.UpdatedAt=time.Now().UTC();raw,e:=store.Encode(a);if e!=nil{return a,e};q,e:=w.Q();if e!=nil{return a,e};if _,e=q.ExecContext(ctx,"UPDATE authors SET name=?,following=?,data=? WHERE id=?",a.Name,a.Following,raw,a.ID);e!=nil{return a,e};return a,finishAuthorTask(ctx,q,task,a.Revision)
}
func(s *Service)Collections(ctx context.Context,libraryID,query,cursor string,limit int)(model.Page[model.Collection],error){n,e:=limited(limit,50);if e!=nil{return model.Page[model.Collection]{},e};key:="collections:"+libraryID+":"+query;o,e:=pageOffset(cursor,key);if e!=nil{return model.Page[model.Collection]{},e};a,e:=store.List[model.Collection](ctx,s.DB.DB,"SELECT data FROM collections WHERE (?='' OR library_id=?) AND (name LIKE ? ESCAPE '\\' OR json_extract(data,'$.aliases') LIKE ? ESCAPE '\\') ORDER BY name,id LIMIT ? OFFSET ?",libraryID,libraryID,like(query),like(query),n+1,o);return paged(a,n,o,key),e}
func(s *Service)CreateCollection(ctx context.Context,w *Write,in model.Collection)(model.Collection,error){
 if e:=Authorize(w.Principal,"admin","");e!=nil{return in,e};if _,e:=s.library(ctx,in.LibraryID);e!=nil{return in,e};if !domain.ValidName(in.Name)||!domain.ValidMediaPath(in.Name)||(in.Kind!="image_set"&&in.Kind!="short_drama"){return in,Fail(400,"invalid_collection","集合名称或类型无效。")};if in.Aliases==nil{in.Aliases=[]string{}};var e error;in.Aliases,e=cleanStrings(in.Aliases,100,200);if e!=nil{return in,e}
 in.ID=w.ID("collection");in.Revision=1;in.CreatedAt=time.Now().UTC();raw,e:=store.Encode(in);if e!=nil{return in,e};q,e:=w.Q();if e!=nil{return in,e};_,e=q.ExecContext(ctx,"INSERT INTO collections(id,library_id,name,kind,data) VALUES(?,?,?,?,?)",in.ID,in.LibraryID,in.Name,in.Kind,raw);return in,e
}
