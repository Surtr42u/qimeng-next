package service

import (
 "context"
 "database/sql"
 "encoding/json"
 "sort"
 "strconv"
 "strings"
 "time"
 "unicode/utf8"

 "qimeng.local/next/server/internal/domain"
 "qimeng.local/next/server/internal/model"
)

const browseCTE=`WITH activity AS (SELECT asset_id,sum(views) views,sum(plays) plays FROM asset_daily_stats GROUP BY asset_id)`
const browseFrom=` FROM assets a JOIN libraries l ON l.id=a.library_id LEFT JOIN activity st ON st.asset_id=a.id `
const assetSearchText=`(a.file_name||char(10)||substr(a.relative_path,1,length(a.relative_path)-length(a.file_name))||char(10)||COALESCE((SELECT group_concat(tag,char(10)) FROM asset_tags WHERE asset_id=a.id),'')||char(10)||COALESCE((SELECT group_concat(au.name||' '||COALESCE(json_extract(au.data,'$.aliases'),''),char(10)) FROM asset_authors aa JOIN authors au ON au.id=aa.author_id WHERE aa.asset_id=a.id),'')||char(10)||COALESCE((SELECT group_concat(s.name,char(10)) FROM asset_sources x JOIN sources s ON s.id=x.source_id WHERE x.asset_id=a.id),json_extract(a.data,'$.source'),'')||char(10)||COALESCE((SELECT group_concat(c.name,char(10)) FROM asset_characters x JOIN characters c ON c.id=x.character_id WHERE x.asset_id=a.id),json_extract(a.data,'$.characters'),''))`
type browsePredicate struct{Where string;Args []any;FilterKey string}
type browseSort struct{Expression,Name string;Descending,Numeric,Nullable bool}

func normalizeAssetFilter(in AssetFilter)(AssetFilter,error){
 if in.TimeRange=="year_range" && in.FromYear>in.ToYear {in.FromYear,in.ToYear=in.ToYear,in.FromYear}
 if in.TZOffsetMinutes < -840||in.TZOffsetMinutes>840{return in,Fail(400,"invalid_timezone_offset","客户端时区偏移必须在 UTC−14 至 UTC+14 之间。")}
 in.Q=strings.Join(strings.Fields(in.Q)," ");if utf8.RuneCountInString(in.Q)>200{return in,Fail(400,"query_too_long","搜索词最多 200 字。")};if in.Kind!=""&&in.Kind!="regular"&&in.Kind!="cos"{return in,Fail(400,"invalid_filter","来源分区无效。")};switch in.MediaType{case "","image","animated_image","video":default:return in,Fail(400,"invalid_filter","媒体类型无效。")}
 for _,field:=range []*[]string{&in.AuthorIDs,&in.SourceIDs,&in.CharacterIDs,&in.CosWorkIDs,&in.WorkKeys,&in.RoleKeys,&in.Tags}{max:=50;if field==&in.Tags{max=100};var e error;*field,e=cleanStrings(append([]string{},(*field)...),max,512);if e!=nil{return in,e};sort.Strings(*field)}
 if in.Tag!=""{names,e:=cleanStrings([]string{in.Tag},1,100);if e!=nil{return in,e};in.Tag=names[0]};if in.TagMode==""{in.TagMode="any"};if in.TagMode!="any"&&in.TagMode!="all"{return in,Fail(400,"invalid_filter","标签匹配模式无效。")}
 if in.Directory!=nil{if in.LibraryID==""{return in,Fail(400,"directory_requires_library","目录文件分页必须指定媒体库。")};if !domain.ValidMediaPath(*in.Directory){return in,Fail(400,"invalid_path","目录须为库内合法相对路径。")}}
 if in.Order==""{in.Order="desc"};if in.Order!="asc"&&in.Order!="desc"{return in,Fail(400,"invalid_sort","排序方向无效。")};if in.Sort==""{in.Sort="default"};switch in.Sort{case "added_desc":in.Sort="addedDate";in.Order="desc";case "name_asc":in.Sort="name";in.Order="asc";case "updated_desc":in.Order="desc";case "default","fileDate","addedDate","views","plays","size","name","favorited":default:return in,Fail(400,"invalid_sort","不支持该排序方式。")}
 if in.Sort=="favorited"&&(in.Favorite==nil||!*in.Favorite){return in,Fail(400,"favorite_sort_requires_subset","收藏时间排序须限定收藏子集。")}
 for _,rangeValue:=range []struct{name string;low,high **int64;size bool}{{in.ViewRange,&in.MinViews,&in.MaxViews,false},{in.PlayRange,&in.MinPlays,&in.MaxPlays,false},{in.SizeRange,&in.MinSize,&in.MaxSize,true}}{low,high,ok:=domain.CountInterval(rangeValue.name);if rangeValue.size{low,high,ok=domain.SizeInterval(rangeValue.name)};if !ok{return in,Fail(400,"invalid_filter_range","播放、观看或文件大小区间无效。")};if low!=nil&&(*rangeValue.low==nil||**rangeValue.low<*low){*rangeValue.low=low};if high!=nil&&(*rangeValue.high==nil||**rangeValue.high>*high){*rangeValue.high=high}}
 for _,pair:=range [][2]*int64{{in.MinViews,in.MaxViews},{in.MinPlays,in.MaxPlays},{in.MinSize,in.MaxSize}}{for _,value:=range pair{if value!=nil&&*value<0{return in,Fail(400,"invalid_filter","次数或文件大小不能为负。")}};if pair[0]!=nil&&pair[1]!=nil&&*pair[0]>*pair[1]{return in,Fail(400,"invalid_filter","筛选下限不能大于上限。")}}
 return in,nil
}
func queryArray(values []string)string{raw,_:=json.Marshal(values);return string(raw)}
func sourceSelection(ids []string) (string, []any) {
 selected := []string{}
 other := false
 for _, id := range ids { if id == "__other__" { other = true } else { selected = append(selected, id) } }
 parts := []string{}
 args := []any{}
 if len(selected) > 0 {
  parts = append(parts, "EXISTS(SELECT 1 FROM asset_sources x WHERE x.asset_id=a.id AND x.source_id IN (SELECT value FROM json_each(?)))")
  args = append(args, queryArray(selected))
 }
 if other { parts = append(parts, "(NOT "+assetIsCOS+" AND NOT EXISTS(SELECT 1 FROM asset_sources x WHERE x.asset_id=a.id))") }
 return "("+strings.Join(parts," OR ")+")", args
}
func mixedSelections(keys []string,row string)(string,[]any,error){
 groups:=map[string][]string{};for _,key:=range keys{kind,id,ok:=strings.Cut(key,":");if !ok||id==""||(row=="works"&&kind!="source"&&kind!="author")||(row=="roles"&&kind!="character"&&kind!="work"){return "",nil,Fail(400,"invalid_facet_key","筛选键不属于指定的相册维度。")};groups[kind]=append(groups[kind],id)}
 parts:=[]string{};args:=[]any{}
 for _,kind:=range []string{"source","author","character","work"}{
  ids:=groups[kind];if len(ids)==0{continue}
  switch kind{
  case "source":
   clause,values:=sourceSelection(ids);parts=append(parts,"(NOT "+assetIsCOS+" AND "+clause+")");args=append(args,values...)
  case "author":
   parts=append(parts,"EXISTS(SELECT 1 FROM asset_authors x JOIN authors selected_author ON selected_author.id=x.author_id WHERE x.asset_id=a.id AND json_extract(selected_author.data,'$.kind')='cos' AND x.author_id IN (SELECT value FROM json_each(?)))");args=append(args,queryArray(ids))
  case "character":
   parts=append(parts,"(NOT "+assetIsCOS+" AND EXISTS(SELECT 1 FROM asset_characters x WHERE x.asset_id=a.id AND x.character_id IN (SELECT value FROM json_each(?))))");args=append(args,queryArray(ids))
  case "work":
   parts=append(parts,"("+assetIsCOS+" AND a.cos_work_id IN (SELECT value FROM json_each(?)))");args=append(args,queryArray(ids))
  }
 }
 return "("+strings.Join(parts," OR ")+")",args,nil
}
func fileDateBounds(f AssetFilter)(*time.Time,*time.Time,error){
 zone:=time.FixedZone("client",f.TZOffsetMinutes*60);now:=time.Now().In(zone);today:=time.Date(now.Year(),now.Month(),now.Day(),0,0,0,0,zone);start,end:=today,today.AddDate(0,0,1)
 switch f.TimeRange{case "","all":return nil,nil,nil;case "today":case "week":weekday:=(int(today.Weekday())+6)%7;start=today.AddDate(0,0,-weekday);case "month":start=time.Date(now.Year(),now.Month(),1,0,0,0,0,zone);case "three_months":start=today.AddDate(0,-3,0);case "year":start=time.Date(now.Year(),1,1,0,0,0,0,zone);case "year_range":if f.FromYear<1970||f.ToYear<f.FromYear||f.ToYear>9999{return nil,nil,Fail(400,"invalid_year_range","年份范围无效。")};start=time.Date(f.FromYear,1,1,0,0,0,0,zone);end=time.Date(f.ToYear+1,1,1,0,0,0,0,zone);default:return nil,nil,Fail(400,"invalid_time_range","时间范围无效。")};return &start,&end,nil
}
func assetPredicate(f AssetFilter)(browsePredicate,error){
 where:=[]string{"a.trashed=?"};args:=[]any{f.Trashed}
 if !f.IncludeDisabled&&!f.Trashed{where=append(where,"COALESCE(json_extract(l.data,'$.enabled'),1)=1")}
 if f.Directory!=nil{directory:=*f.Directory;if directory==""{if !f.Recursive{where=append(where,"instr(a.relative_path,'/')=0")}}else{prefix:=directory+"/";where=append(where,"substr(a.relative_path,1,length(?))=?");args=append(args,prefix,prefix);if !f.Recursive{where=append(where,"instr(substr(a.relative_path,length(?)+1),'/')=0");args=append(args,prefix)}}}
 for _,value:=range []struct{column,value string}{{"a.library_id",f.LibraryID},{"a.media_type",f.MediaType},{"a.collection_id",f.CollectionID},{assetPartition,f.Kind}}{if value.value!=""{where=append(where,value.column+"=?");args=append(args,value.value)}}
 if f.AuthorID!=""{where=append(where,"EXISTS(SELECT 1 FROM asset_authors aa WHERE aa.asset_id=a.id AND aa.author_id=?)");args=append(args,f.AuthorID)}
 for _,dimension:=range []struct{table,column string;values []string}{{"asset_authors","author_id",f.AuthorIDs},{"asset_characters","character_id",f.CharacterIDs}}{if len(dimension.values)>0{where=append(where,"EXISTS(SELECT 1 FROM "+dimension.table+" x WHERE x.asset_id=a.id AND x."+dimension.column+" IN (SELECT value FROM json_each(?)))");args=append(args,queryArray(dimension.values))}}
 if len(f.SourceIDs)>0{clause,values:=sourceSelection(f.SourceIDs);where=append(where,clause);args=append(args,values...)};if len(f.CosWorkIDs)>0{where=append(where,"a.cos_work_id IN (SELECT value FROM json_each(?))");args=append(args,queryArray(f.CosWorkIDs))}
 for _,row:=range []struct{name string;keys []string}{{"works",f.WorkKeys},{"roles",f.RoleKeys}}{if len(row.keys)>0{clause,values,e:=mixedSelections(row.keys,row.name);if e!=nil{return browsePredicate{},e};where=append(where,clause);args=append(args,values...)}}
 for _,token:=range strings.Fields(f.Q){where=append(where,"qimeng_search_token("+assetSearchText+",?)=1");args=append(args,token)}
 if f.Tag!=""{where=append(where,"EXISTS(SELECT 1 FROM asset_tags WHERE asset_id=a.id AND tag=?)");args=append(args,f.Tag)}
 if len(f.Tags)>0{if f.TagMode=="all"{where=append(where,"(SELECT COUNT(DISTINCT tag) FROM asset_tags WHERE asset_id=a.id AND tag IN (SELECT value FROM json_each(?)))=?");args=append(args,queryArray(f.Tags),len(f.Tags))}else{where=append(where,"EXISTS(SELECT 1 FROM asset_tags WHERE asset_id=a.id AND tag IN (SELECT value FROM json_each(?)))");args=append(args,queryArray(f.Tags))}}
 if f.Favorite!=nil{where=append(where,"a.favorite=?");args=append(args,*f.Favorite)};if f.Liked!=nil{where=append(where,"(EXISTS(SELECT 1 FROM likes WHERE asset_id=a.id) OR EXISTS(SELECT 1 FROM like_baselines WHERE asset_id=a.id))=?");args=append(args,*f.Liked)};if f.History!=nil{where=append(where,"EXISTS(SELECT 1 FROM events WHERE asset_id=a.id AND json_extract(data,'$.kind')='view')=?");args=append(args,*f.History)}
 for _,rangeValue:=range []struct{expression string;low,high *int64}{{"COALESCE(st.views,0)",f.MinViews,f.MaxViews},{"COALESCE(st.plays,0)",f.MinPlays,f.MaxPlays},{"a.size",f.MinSize,f.MaxSize}}{if rangeValue.low!=nil{where=append(where,rangeValue.expression+">=?");args=append(args,*rangeValue.low)};if rangeValue.high!=nil{where=append(where,rangeValue.expression+"<=?");args=append(args,*rangeValue.high)}}
 start,end,e:=fileDateBounds(f);if e!=nil{return browsePredicate{},e};if start!=nil{where=append(where,"a.file_date>=? AND a.file_date<?");args=append(args,start.UnixMilli(),end.UnixMilli())}
 keyFilter:=f;keyFilter.Cursor="";keyFilter.Limit=0;keyFilter.DateCounts=false;if start==nil{keyFilter.TZOffsetMinutes=0};key,_:=json.Marshal(keyFilter);boundary:="";if start!=nil{boundary=start.String()+"/"+end.String()};return browsePredicate{Where:strings.Join(where," AND "),Args:args,FilterKey:"file-date-2:"+string(key)+boundary},nil
}
func assetSort(f AssetFilter)browseSort{
 value:=browseSort{Name:f.Sort+":"+f.Order,Descending:f.Order=="desc"}
 switch f.Sort{case "default","fileDate":value.Expression="a.file_date";value.Numeric=true;value.Nullable=true;case "addedDate":value.Expression="a.created_at";case "updated_desc":value.Expression="a.updated_at";case "name":value.Expression="a.file_name";case "size":value.Expression="a.size";value.Numeric=true;case "views":value.Expression="COALESCE(st.views,0)";value.Numeric=true;case "plays":value.Expression="COALESCE(st.plays,0)";value.Numeric=true;case "favorited":value.Expression="a.favorited_at";value.Nullable=true};return value
}
func seekClause(key seekCursor,order browseSort,idExpression string)(string,[]any,error){
 if key.ID==""{return "",nil,nil};comparison:=">";if order.Descending{comparison="<"};if key.Value=="~null"&&order.Nullable{return " AND "+order.Expression+" IS NULL AND "+idExpression+comparison+"?",[]any{key.ID},nil}
 var value any=key.Value;if order.Numeric{n,e:=strconv.ParseInt(key.Value,10,64);if e!=nil{return "",nil,Fail(400,"invalid_cursor","排序游标数值无效。")};value=n};clause:=" AND ("+order.Expression+comparison+"? OR ("+order.Expression+"=? AND "+idExpression+comparison+"?)";if order.Nullable{clause+=" OR "+order.Expression+" IS NULL"};return clause+")",[]any{value,value,key.ID},nil
}
func sortSQL(order browseSort,key,id string)string{direction:=" ASC";if order.Descending{direction=" DESC"};prefix:="";if order.Nullable{prefix=key+" IS NULL ASC,"};return prefix+key+direction+","+id+direction}
func(s *Service)Assets(ctx context.Context,filter AssetFilter)(model.Page[model.Asset],error){
 page:=model.Page[model.Asset]{Items:[]model.Asset{}};f,e:=normalizeAssetFilter(filter);if e!=nil{return page,e};n,e:=limited(f.Limit,50);if e!=nil{return page,e};predicate,e:=assetPredicate(f);if e!=nil{return page,e};order:=assetSort(f);cursor,e:=readSeek(f.Cursor,predicate.FilterKey,order.Name);if e!=nil{return page,e};seek,values,e:=seekClause(cursor,order,"a.id");if e!=nil{return page,e}
 tx,e:=s.DB.DB.BeginTx(ctx,&sql.TxOptions{ReadOnly:true});if e!=nil{return page,e};defer tx.Rollback();var count int64
 if f.DateCounts&&f.Cursor==""{
  buckets:=[]model.DateCountBucket{};unknown:=int64(0);args:=append([]any{f.TZOffsetMinutes*60},predicate.Args...)
  rows,e:=tx.QueryContext(ctx,browseCTE+" SELECT strftime('%Y-%m-%d',a.file_date/1000.0+?,'unixepoch') day,count(*)"+browseFrom+"WHERE "+predicate.Where+" GROUP BY day ORDER BY day DESC",args...);if e!=nil{return page,e}
  for rows.Next(){var day sql.NullString;var n int64;if e=rows.Scan(&day,&n);e!=nil{rows.Close();return page,e};count+=n;if day.Valid{buckets=append(buckets,model.DateCountBucket{Date:day.String,FileCount:n})}else{unknown+=n}}
  e=rows.Err();rows.Close();if e!=nil{return page,e};page.DateCounts=&buckets;page.UnknownDateCount=&unknown
 }else{if e=tx.QueryRowContext(ctx,browseCTE+" SELECT COUNT(*)"+browseFrom+"WHERE "+predicate.Where,predicate.Args...).Scan(&count);e!=nil{return page,e}}
 page.TotalMatched=&count
 args:=append([]any{},predicate.Args...);args=append(args,values...);args=append(args,n+1,time.Now().Format("2006-01-02"))
 query:=browseCTE+", page AS (SELECT a.id,"+order.Expression+" sort_key"+browseFrom+"WHERE "+predicate.Where+seek+" ORDER BY "+sortSQL(order,order.Expression,"a.id")+" LIMIT ?) SELECT "+assetJSON+",CAST(p.sort_key AS TEXT) FROM page p JOIN assets a ON a.id=p.id JOIN authors au ON au.id=a.author_id ORDER BY "+sortSQL(order,"p.sort_key","p.id")
 rows,e:=tx.QueryContext(ctx,query,args...);if e!=nil{return page,e};defer rows.Close();keys:=[]string{}
 for rows.Next(){var raw string;var key sql.NullString;var a model.Asset;if e=rows.Scan(&raw,&key);e!=nil{return page,e};if e=json.Unmarshal([]byte(raw),&a);e!=nil{return page,e};page.Items=append(page.Items,a);value:="~null";if key.Valid{value=key.String};keys=append(keys,value)};if e=rows.Err();e!=nil{return page,e}
 if len(page.Items)>n{page.Items=page.Items[:n];last:=page.Items[n-1];page.NextCursor=makeSeek(predicate.FilterKey,order.Name,keys[n-1],last.ID)};return page,nil
}
