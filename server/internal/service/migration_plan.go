package service

import (
 "context"
 "database/sql"
 "encoding/json"
 "errors"
 "strings"
 "time"
 "unicode/utf8"

 legacy "qimeng.local/next/server/internal/migration"
 "qimeng.local/next/server/internal/model"
 "qimeng.local/next/server/internal/store"
)

// Match proposals never use an ordering to choose among ambiguous files. A
// source ID previously mapped by this installation takes priority over names.
func(s *Service)matchV2Media(ctx context.Context,sourceID string,in legacy.Media,override string,explicit bool)(model.Asset,string,error){
 if explicit&&override==""{return model.Asset{},"explicit_skip",nil}
 id:=override
 if !explicit&&in.AssetID!=""{e:=s.DB.DB.QueryRowContext(ctx,"SELECT asset_id FROM migration_media_aliases WHERE source_id=? AND source_asset_id=?",sourceID,in.AssetID).Scan(&id);if e!=nil&&!errors.Is(e,sql.ErrNoRows){return model.Asset{},"",e};if id==""{id=in.AssetID}}
 if id!=""{a,e:=store.Get[model.Asset](ctx,s.DB.DB,"SELECT data FROM assets WHERE id=?",id);if e==nil{return a,"identity",nil};if !errors.Is(e,sql.ErrNoRows){return a,"",e};if explicit||id!=in.AssetID{return a,"mapped_asset_missing",nil}}
 where:="a.file_name=? AND a.trashed=0";args:=[]any{in.FileName}
 for stage:=0;stage<3;stage++{
  rows,e:=store.List[model.Asset](ctx,s.DB.DB,"SELECT a.data FROM assets a JOIN libraries l ON l.id=a.library_id WHERE "+where+" ORDER BY a.id LIMIT 2",args...);if e!=nil{return model.Asset{},"",e};if len(rows)==1{return rows[0],"unique_metadata_match",nil};if len(rows)==0{return model.Asset{},"unmatched",nil}
  if stage==0{if in.RelativePath==""{continue};where+=" AND a.relative_path=?";args=append(args,in.RelativePath)}else if stage==1{if in.LibraryName==""{continue};where+=" AND l.name=?";args=append(args,in.LibraryName)}
 }
 return model.Asset{},"ambiguous_media",nil
}
func(s *Service)planV2Author(ctx context.Context,sourceID string,source legacy.Author,kind,override string,ordinal int)(V2MigrationAuthor,error){
 result:=V2MigrationAuthor{Ordinal:ordinal,SourceAuthorID:source.AuthorID,Name:source.DisplayName,Kind:kind,State:"conflict",Reason:""}
 if kind=="mixed"{result.Reason="author_crosses_partitions";return result,nil};if strings.TrimSpace(source.DisplayName)==""||utf8.RuneCountInString(source.DisplayName)>200||strings.ContainsRune(source.DisplayName,'\x00'){result.Reason="invalid_author_name";return result,nil}
 unknown,e:=s.Author(ctx,"unknown");if e!=nil{return result,e};if source.DisplayName==unknown.Name{result.AuthorID=unknown.ID;result.State="existing";result.ExpectedRevision=unknown.Revision;result.Reason="unknown_placeholder";return result,nil}
 id:=override
 if id==""{e=s.DB.DB.QueryRowContext(ctx,"SELECT author_id FROM migration_author_aliases WHERE source_id=? AND source_author_id=?",sourceID,source.AuthorID).Scan(&id);if e!=nil&&!errors.Is(e,sql.ErrNoRows){return result,e};if id==""{id=source.AuthorID}}
 if id!=""{a,e:=store.Get[model.Author](ctx,s.DB.DB,"SELECT data FROM authors WHERE id=?",id);if e==nil{if !a.Placeholder&&a.Kind!=kind{result.Reason="author_partition_conflict";return result,nil};result.AuthorID=a.ID;result.ExpectedRevision=a.Revision;result.State="existing";return result,nil};if !errors.Is(e,sql.ErrNoRows){return result,e};if override!=""||id!=source.AuthorID{result.Reason="mapped_author_missing";return result,nil}}
 matches,e:=store.List[model.Author](ctx,s.DB.DB,"SELECT data FROM authors WHERE name=? AND json_extract(data,'$.kind')=? ORDER BY id LIMIT 2",source.DisplayName,kind);if e!=nil{return result,e};if len(matches)>1{result.Reason="ambiguous_author";return result,nil};if len(matches)==1{result.AuthorID=matches[0].ID;result.ExpectedRevision=matches[0].Revision;result.State="existing";result.Reason="unique_name_proposal";return result,nil}
 result.AuthorID=digest([]byte("v2-author:"+sourceID+":"+source.AuthorID))[:32];result.State="create";return result,nil
}
func currentTagState(a model.Asset)legacy.TagState{
 state:=legacy.TagState{Names:a.Tags,Source:a.TagSetSource,Explicit:a.TagSetUpdatedAt!=nil||a.TagSetSource!=""||a.TagsRevision>1||len(a.Tags)>0};if a.TagSetUpdatedAt!=nil{millis:=a.TagSetUpdatedAt.UnixMilli();state.UpdatedAtMillis=&millis};return state
}
func(s *Service)PrepareV2Migration(ctx context.Context,w *Write,inputID string,in V2PlanCreate)(V2MigrationPlan,error){
 plan:=V2MigrationPlan{ID:w.ID("v2-plan"),ImportID:inputID,State:"prepared",CreatedAt:time.Now().UTC(),Issues:[]string{},Counts:map[string]int64{}}
 if e:=Authorize(w.Principal,"admin","");e!=nil{return plan,e};if len(in.MediaMappings)>10000||len(in.AuthorMappings)>10000{return plan,Fail(400,"migration_mapping_budget","单次映射覆盖最多 10000 个媒体或作者身份。")}
 metadata,input,e:=s.loadV2(ctx,inputID);if e!=nil{return plan,e};plan.MediaCount=len(input.Data.MediaFiles);plan.AuthorCount=len(input.Data.Authors)
 mediaByKey:=map[string]legacy.Media{};for _,source:=range input.Data.MediaFiles{mediaByKey[source.RecordKey]=source}
 mediaOverrides:=map[string]string{};for _,mapping:=range in.MediaMappings{if _,ok:=mediaByKey[mapping.RecordKey];!ok{return plan,Fail(400,"unknown_record_key","映射引用了输入中不存在的媒体。")};if _,duplicate:=mediaOverrides[mapping.RecordKey];duplicate{return plan,Fail(400,"duplicate_mapping","同一源记录不能提供多个映射。")};mediaOverrides[mapping.RecordKey]=mapping.AssetID}
 authorsByID:=map[string]legacy.Author{};for _,source:=range input.Data.Authors{authorsByID[source.AuthorID]=source};authorOverrides:=map[string]string{};for _,mapping:=range in.AuthorMappings{if _,ok:=authorsByID[mapping.SourceAuthorID];!ok||mapping.AuthorID==""{return plan,Fail(400,"unknown_author_mapping","作者映射须引用有效源身份和明确目标。")};if _,duplicate:=authorOverrides[mapping.SourceAuthorID];duplicate{return plan,Fail(400,"duplicate_mapping","同一源作者不能提供多个映射。")};authorOverrides[mapping.SourceAuthorID]=mapping.AuthorID}
 kinds:=map[string]string{};refs:=map[string][]string{};for _,ref:=range input.Data.AuthorMediaRefs{kind:="regular";if mediaByKey[ref.RecordKey].IsCOS{kind="cos"};if old:=kinds[ref.AuthorID];old!=""&&old!=kind{kinds[ref.AuthorID]="mixed"}else{kinds[ref.AuthorID]=kind};refs[ref.RecordKey]=appendUnique(refs[ref.RecordKey],ref.AuthorID)}
 authors:=[]V2MigrationAuthor{};authorPlans:=map[string]V2MigrationAuthor{};for index,source:=range input.Data.Authors{kind:=kinds[source.AuthorID];if kind==""{kind="regular";if strings.HasPrefix(source.AuthorID,"cos_"){kind="cos"}};candidate,e:=s.planV2Author(ctx,metadata.SourceID,source,kind,authorOverrides[source.AuthorID],index);if e!=nil{return plan,e};authors=append(authors,candidate);authorPlans[source.AuthorID]=candidate;if candidate.State=="conflict"{migrationIssue(&plan,candidate.Reason)}}
 proposedNames:=map[string][]int{};for index,author:=range authors{if author.State=="create"{key:=author.Kind+"\x00"+author.Name;proposedNames[key]=append(proposedNames[key],index)}};for _,indices:=range proposedNames{if len(indices)<2{continue};for _,index:=range indices{authors[index].State="conflict";authors[index].Reason="ambiguous_source_authors";authorPlans[authors[index].SourceAuthorID]=authors[index];migrationIssue(&plan,"ambiguous_source_authors")}}
 tags:=map[string][]string{};for _,ref:=range input.Data.MediaTagRefs{tags[ref.RecordKey]=appendUnique(tags[ref.RecordKey],strings.TrimSpace(ref.TagName))}
 items:=[]migrationItemRecord{};targets:=map[string][]int{}
 for index,source:=range input.Data.MediaFiles{
  item:=migrationItemRecord{V2MigrationItem:V2MigrationItem{Ordinal:index,RecordKey:source.RecordKey,FileName:source.FileName,State:"skipped",TagAction:"none",Issues:[]string{}},AuthorsAction:"keep",AuthorIDs:[]string{}}
  override,explicit:=mediaOverrides[source.RecordKey];a,reason,e:=s.matchV2Media(ctx,metadata.SourceID,source,override,explicit);if e!=nil{return plan,e};item.Reason=reason
  if a.ID!=""{
   item.AssetID=a.ID;item.ExpectedRevision=a.Revision;item.ExpectedContentVersion=a.ContentVersion;item.TargetHash=a.Hash;item.State="ready";l,e:=s.library(ctx,a.LibraryID);if e!=nil{return plan,e}
   kind:="regular";if source.IsCOS{kind="cos"};switch{case a.Trashed:item.State="conflict";item.Reason="target_trashed";case l.Kind!=kind:item.State="conflict";item.Reason="media_partition_conflict";case source.SizeBytes!=a.Size||source.MediaType!=a.MediaType:item.State="conflict";item.Reason="media_facts_conflict"}
   incoming,e:=cleanStrings(append([]string{},tags[source.RecordKey]...),100,100);if e!=nil{return plan,e};item.Tags=legacy.ResolveTags(currentTagState(a),[]legacy.TagState{{Names:incoming,UpdatedAtMillis:source.TagsUpdatedAtMillis,Source:"v2:"+metadata.ID,Explicit:true}});item.TagAction=item.Tags.Action
   for _,name:=range incoming{var deleted int;if e=s.DB.DB.QueryRowContext(ctx,"SELECT COALESCE((SELECT deleted FROM tags WHERE name=?),0)",name).Scan(&deleted);e!=nil{return plan,e};if deleted!=0{item.TagAction="conflict";item.Tags.Action="conflict";item.Tags.Reason="tag_deleted";break}}
   if item.TagAction=="conflict"{item.Issues=append(item.Issues,item.Tags.Reason)}
   conflict:=false;for _,id:=range refs[source.RecordKey]{author:=authorPlans[id];if author.State=="conflict"{conflict=true;continue};item.AuthorIDs=appendUnique(item.AuthorIDs,author.AuthorID)}
   if len(item.AuthorIDs)>1{real:=[]string{};for _,id:=range item.AuthorIDs{if id!="unknown"{real=append(real,id)}};item.AuthorIDs=real}
   if conflict{item.AuthorsAction="conflict";item.Issues=append(item.Issues,"unresolved_author_identity")}else if len(item.AuthorIDs)>0&&!legacy.SameTags(a.AuthorIDs,item.AuthorIDs){if a.AuthorsRevision==1&&len(a.AuthorIDs)==1&&a.AuthorIDs[0]=="unknown"{item.AuthorsAction="apply"}else{item.AuthorsAction="conflict";item.Issues=append(item.Issues,"existing_author_relationships_kept")}}
   targets[a.ID]=append(targets[a.ID],index)
  }
  items=append(items,item)
 }
 for _,indices:=range targets{if len(indices)>1{for _,index:=range indices{items[index].State="conflict";items[index].Reason="multiple_sources_one_target"}}}
 for _,item:=range items{if item.State=="ready"{plan.MatchedCount++}else{plan.SkippedCount++;migrationIssue(&plan,item.Reason)};for _,issue:=range item.Issues{migrationIssue(&plan,issue)}}
 migrationIssue(&plan,"legacy_source_has_no_content_hashes")
 if len(input.Data.Settings)>0{migrationIssue(&plan,"source_runtime_settings_archived_not_activated")};if len(input.Data.AppPrefs)>0{migrationIssue(&plan,"only_missing_recommendation_preferences_are_restored")}
 for _,like:=range input.Data.Likes{if like.LikeCount>1{migrationIssue(&plan,"legacy_like_aggregate_has_no_daily_identity")};if like.LikeCount>0&&(like.LastLikeDate==nil||*like.LastLikeDate==""){migrationIssue(&plan,"legacy_like_day_unknown")}}
 raw,e:=json.Marshal(struct{SourceID,SHA256 string;Items []migrationItemRecord;Authors []V2MigrationAuthor}{metadata.SourceID,metadata.SHA256,items,authors});if e!=nil{return plan,e};plan.Fingerprint=digest(raw)
 q,e:=w.Q();if e!=nil{return plan,e};if e=saveV2Plan(ctx,q,plan);e!=nil{return plan,e}
 for _,item:=range items{raw,e:=store.Encode(item);if e!=nil{return plan,e};if _,e=q.ExecContext(ctx,"INSERT INTO migration_items(plan_id,ordinal,record_key,asset_id,data) VALUES(?,?,?,?,?)",plan.ID,item.Ordinal,item.RecordKey,item.AssetID,raw);e!=nil{return plan,e}}
 for _,author:=range authors{raw,e:=store.Encode(author);if e!=nil{return plan,e};if _,e=q.ExecContext(ctx,"INSERT INTO migration_authors(plan_id,ordinal,source_author_id,author_id,data) VALUES(?,?,?,?,?)",plan.ID,author.Ordinal,author.SourceAuthorID,author.AuthorID,raw);e!=nil{return plan,e}}
 return plan,nil
}
