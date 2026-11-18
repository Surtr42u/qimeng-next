package service

import (
 "context"
 "database/sql"
 "encoding/json"
 "errors"
 "strings"
 "time"

 "qimeng.local/next/server/internal/analytics"
 legacy "qimeng.local/next/server/internal/migration"
 "qimeng.local/next/server/internal/model"
 "qimeng.local/next/server/internal/store"
)

func importedWeights(raw json.RawMessage)(*analytics.Weights,error){
 if len(raw)==0||string(raw)=="null"{return nil,nil};var prefs map[string]json.RawMessage;if json.Unmarshal(raw,&prefs)!=nil||prefs==nil{return nil,Fail(400,"invalid_imported_preferences","旧偏好必须是 JSON 对象。")};value,ok:=prefs["recommendationPrefs"];if !ok||string(value)=="null"{return nil,nil};var fields map[string]json.RawMessage;if json.Unmarshal(value,&fields)!=nil||fields==nil{return nil,Fail(400,"invalid_imported_preferences","旧推荐权重必须是 JSON 对象。")};weights:=analytics.DefaultWeights;if json.Unmarshal(value,&weights)!=nil||!weights.Valid(){return nil,Fail(400,"invalid_imported_preferences","旧推荐权重不是 0–1 的有效数值。")};return &weights,nil
}

func(s *Service)CommitV2Migration(ctx context.Context,w *Write,id,fingerprint string,acceptSkipped bool)(V2MigrationPlan,error){
 plan,e:=s.V2MigrationPlan(ctx,w.Principal,id);if e!=nil{return plan,e};if plan.Fingerprint!=fingerprint{return plan,Fail(409,"migration_plan_changed","提交的计划指纹不匹配。")};if plan.State!="prepared"{return plan,nil}
 if plan.SkippedCount>0&&!acceptSkipped{return plan,Fail(409,"migration_has_skipped_items","请先核对计划中的未匹配或冲突项，再明确接受跳过。")}
 metadata,input,e:=s.loadV2(ctx,plan.ImportID);if e!=nil{return plan,e};profiles,avatarBytes,e:=validateV2Profiles(ctx,input);if e!=nil{return plan,e};weights,e:=importedWeights(input.Data.AppPrefs);if e!=nil{return plan,e}
 items,e:=store.List[migrationItemRecord](ctx,s.DB.DB,"SELECT data FROM migration_items WHERE plan_id=? ORDER BY ordinal",plan.ID);if e!=nil{return plan,e};authors,e:=store.List[V2MigrationAuthor](ctx,s.DB.DB,"SELECT data FROM migration_authors WHERE plan_id=? ORDER BY ordinal",plan.ID);if e!=nil{return plan,e}
 assets:=map[string]model.Asset{};authorModels:=map[string]model.Author{};sourceAuthors:=map[string]legacy.Author{};for _,source:=range input.Data.Authors{sourceAuthors[source.AuthorID]=source}
 // All target versions are checked before the checkpoint and before opening the
 // write transaction. The publication lock is shared with every ordinary write.
 for _,item:=range items{if item.State!="ready"{continue};a,e:=s.Asset(ctx,item.AssetID);if e!=nil{return plan,e};if a.Revision!=item.ExpectedRevision||a.ContentVersion!=item.ExpectedContentVersion||a.Hash!=item.TargetHash||a.Trashed{return plan,Fail(409,"migration_target_changed","计划中的目标媒体已变化，请重新生成计划。")};if e=s.verify(assetPath(a),item.TargetHash,a.Size);e!=nil{return plan,Fail(409,"migration_original_changed","当前原件不再符合计划摘要；未迁入元数据。")};assets[item.RecordKey]=a
  if item.TagAction=="apply"{for _,name:=range item.Tags.State.Names{var deleted int;if e=s.DB.DB.QueryRowContext(ctx,"SELECT COALESCE((SELECT deleted FROM tags WHERE name=?),0)",name).Scan(&deleted);e!=nil{return plan,e};if deleted!=0{return plan,Fail(409,"tag_deleted","计划中的标签已被删除，请重新核对，迁移不会复活词条。")}}}
 }
 for _,item:=range authors{if item.State=="conflict"{continue};if item.State=="existing"{a,e:=s.Author(ctx,item.AuthorID);if e!=nil{return plan,e};if a.Revision!=item.ExpectedRevision{return plan,Fail(409,"migration_author_changed","计划中的作者已变化，请重新生成计划。")};authorModels[a.ID]=a;continue}
  var exists int;if e=s.DB.DB.QueryRowContext(ctx,"SELECT COUNT(*) FROM authors WHERE id=? OR (name=? AND (json_extract(data,'$.kind')=? OR id='unknown'))",item.AuthorID,item.Name,item.Kind).Scan(&exists);e!=nil{return plan,e};if exists>0{return plan,Fail(409,"migration_author_changed","计划的新作者身份或名称已被占用，请重新生成计划。")}
  source:=sourceAuthors[item.SourceAuthorID];created:=time.Now().UTC();if source.CreatedAtMillis>0{created=time.UnixMilli(source.CreatedAtMillis).UTC()};authorModels[item.AuthorID]=model.Author{ID:item.AuthorID,Name:item.Name,Kind:item.Kind,Aliases:strings.Split(item.Name," / "),Revision:1,FieldStatus:map[string]string{"identity":"confirmed","biography":"missing","homepage":"missing"},CreatedAt:created,UpdatedAt:created}
 }
 free,e:=freeBytes(s.Config.DataDir);if e!=nil{return plan,e};if free-avatarBytes-metadata.Bytes-(4<<20)<s.Config.ReserveBytes{return plan,Fail(507,"insufficient_storage","空间不足以保留迁移档案与业务余量。")}
 checkpoint,e:=s.CreateBackup(ctx,w);if e!=nil{return plan,e};plan.CheckpointID=checkpoint.ID;q,e:=w.Q();if e!=nil{return plan,e};now:=time.Now().UTC();partial:=plan.SkippedCount>0
 followed:=map[string]bool{};for _,id:=range input.Data.FollowedAuthorIDs{followed[id]=true}
 for index,item:=range authors{
  if item.State=="conflict"{partial=true;continue};a:=authorModels[item.AuthorID];changed:=item.State=="create"
  if followed[item.SourceAuthorID]&&!a.Following{if a.FollowingUpdatedAt==nil{a.Following=true;a.FollowingUpdatedAt=&now;changed=true;plan.Counts["followsAdded"]++}else{migrationIssue(&plan,"existing_unfollow_kept")}}
  if changed{if item.State!="create"{a.Revision++};a.UpdatedAt=now;raw,e:=store.Encode(a);if e!=nil{return plan,e};if _,e=q.ExecContext(ctx,"INSERT INTO authors(id,name,following,data) VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET following=excluded.following,data=excluded.data",a.ID,a.Name,a.Following,raw);e!=nil{return plan,e};if item.State=="create"{plan.Counts["authorsCreated"]++}}
  authorModels[a.ID]=a
  if item.State=="create"{if e=refreshAuthorKeys(ctx,q,a);e!=nil{return plan,e}}
  if _,e=q.ExecContext(ctx,"INSERT INTO migration_author_aliases(source_id,source_author_id,author_id) VALUES(?,?,?) ON CONFLICT(source_id,source_author_id) DO UPDATE SET author_id=excluded.author_id",metadata.SourceID,item.SourceAuthorID,item.AuthorID);e!=nil{return plan,e}
  archiveID:=digest([]byte("v2-profile:"+metadata.ID+":"+item.SourceAuthorID))[:32];var archived int;if e=q.QueryRowContext(ctx,"SELECT COUNT(*) FROM author_profile_archives WHERE id=?",archiveID).Scan(&archived);e!=nil{return plan,e};if archived==0&&profiles[item.SourceAuthorID]!=nil{result,e:=RestoreImportedAuthorProfile(ctx,q,a.ID,a.Name,archiveID,profiles[item.SourceAuthorID]);if e!=nil{return plan,e};plan.Counts["profileBlocksRestored"]+=int64(len(result.RestoredBlocks));plan.Counts["profileCandidatesPreserved"]+=int64(len(result.CandidateBlocks));if result.AvatarRestored{plan.Counts["avatarsRestored"]++};for _,issue:=range result.Issues{migrationIssue(&plan,issue)}}
  authors[index].State="applied";raw,e:=store.Encode(authors[index]);if e!=nil{return plan,e};if _,e=q.ExecContext(ctx,"UPDATE migration_authors SET data=? WHERE plan_id=? AND ordinal=?",raw,plan.ID,item.Ordinal);e!=nil{return plan,e}
 }
 favorites:=map[string]bool{};for _,key:=range input.Data.Favorites{favorites[key]=true}
 if e=restoreV2TextLists(ctx,q,metadata,input.Data.TxtFragments,authors,&plan);e!=nil{return plan,e}
 for _,source:=range input.Data.Tags{names,e:=cleanStrings([]string{source.Name},1,100);if e!=nil{return plan,e};name:=names[0];created:=now;if source.CreatedAtMillis>0{created=time.UnixMilli(source.CreatedAtMillis).UTC()};r,e:=q.ExecContext(ctx,"INSERT INTO tags(name,revision,created_at,updated_at) VALUES(?,1,?,?) ON CONFLICT(name) DO NOTHING",name,dateKey(created),dateKey(created));if e!=nil{return plan,e};n,e:=r.RowsAffected();if e!=nil{return plan,e};plan.Counts["vocabularyTagsAdded"]+=n}
 sourceMedia:=map[string]legacy.Media{};for _,source:=range input.Data.MediaFiles{sourceMedia[source.RecordKey]=source}
 for index,item:=range items{
  if item.State!="ready"{continue};a:=assets[item.RecordKey];changed:=false
  if item.TagAction=="apply"{a.Tags=append([]string{},item.Tags.State.Names...);a.TagsRevision++;a.TagSetSource=item.Tags.State.Source;a.TagSetUpdatedAt=nil;if value:=item.Tags.State.UpdatedAtMillis;value!=nil&&*value>0{updated:=time.UnixMilli(*value).UTC();a.TagSetUpdatedAt=&updated};changed=true;plan.Counts["tagSetsApplied"]++}else if item.TagAction=="conflict"{partial=true}
  if item.AuthorsAction=="apply"{targets:=[]model.Author{};for _,target:=range item.AuthorIDs{author,ok:=authorModels[target];if !ok{if target!=a.COSDirectoryAuthorID{return plan,Fail(409,"migration_author_missing","计划中的作者映射不完整。")};author,e=store.Get[model.Author](ctx,q,"SELECT data FROM authors WHERE id=?",target);if e!=nil{return plan,e}};targets=append(targets,author)};setAuthors(&a,targets);a.AuthorsRevision++;changed=true;plan.Counts["authorSetsApplied"]++}else if item.AuthorsAction=="conflict"{partial=true}
  if favorites[item.RecordKey]&&!a.Favorite{if a.FavoriteUpdatedAt==nil{a.Favorite=true;a.FavoriteUpdatedAt=&now;changed=true;plan.Counts["favoritesAdded"]++}else{migrationIssue(&plan,"existing_unfavorite_kept")}}
  if changed{a.Revision++;a.UpdatedAt=now;if e=store.SaveAsset(ctx,q,a);e!=nil{return plan,e}}
  if item.AuthorsAction=="apply"{for _,id:=range a.AuthorIDs{if e=s.projectLegacyAuthor(ctx,q,a,id);e!=nil{return plan,e}}}
  assets[item.RecordKey]=a;source:=sourceMedia[item.RecordKey];if source.AssetID!=""{if _,e=q.ExecContext(ctx,"INSERT INTO migration_media_aliases(source_id,source_asset_id,asset_id) VALUES(?,?,?) ON CONFLICT(source_id,source_asset_id) DO UPDATE SET asset_id=excluded.asset_id",metadata.SourceID,source.AssetID,a.ID);e!=nil{return plan,e}}
  items[index].State="applied";raw,e:=store.Encode(items[index]);if e!=nil{return plan,e};if _,e=q.ExecContext(ctx,"UPDATE migration_items SET data=? WHERE plan_id=? AND ordinal=?",raw,plan.ID,item.Ordinal);e!=nil{return plan,e};plan.Counts["assetsMatched"]++
 }
 for _,event:=range input.Data.ViewEvents{a,ok:=assets[event.RecordKey];if !ok{plan.Counts["eventsSkipped"]++;continue};added,e:=importV2Event(ctx,q,metadata.SourceID,event,a);if e!=nil{return plan,e};if added{plan.Counts["eventsImported"]++}else{plan.Counts["eventsAlreadyPresent"]++}}
 likeActions:=map[string]string{};for _,item:=range items{likeActions[item.RecordKey]=item.LikeAction}
 for _,like:=range input.Data.Likes{if e=preserveV2Relation(ctx,q,metadata.ID,"like",like.RecordKey,like);e!=nil{return plan,e};a,ok:=assets[like.RecordKey];if !ok{continue};action:=likeActions[like.RecordKey];if action=="conflict"{partial=true;plan.Counts["likeConflicts"]++;migrationIssue(&plan,"legacy_like_overlap_unknown");continue};updated,changed,e:=importLegacyLikes(ctx,q,metadata,sourceMedia[like.RecordKey],like,a,action);if e!=nil{return plan,e};if changed{assets[like.RecordKey]=updated;plan.Counts["likeAggregatesPreserved"]++;plan.Counts["legacyLikesPreserved"]+=like.LikeCount}}
 for _,mark:=range input.Data.TimelineTags{a,ok:=assets[mark.RecordKey];if !ok{continue};added,e:=importV2Mark(ctx,q,metadata.SourceID,mark,a);if e!=nil{return plan,e};if added{plan.Counts["timelineMarksImported"]++}}
 for _,ref:=range input.Data.AuthorMediaRefs{if e=preserveV2Relation(ctx,q,metadata.ID,"author",ref.RecordKey+"\x00"+ref.AuthorID,ref);e!=nil{return plan,e}}
 for _,ref:=range input.Data.MediaTagRefs{if e=preserveV2Relation(ctx,q,metadata.ID,"tag",ref.RecordKey+"\x00"+ref.TagName,ref);e!=nil{return plan,e}}
 if weights!=nil{var existing string;e=q.QueryRowContext(ctx,"SELECT value FROM settings WHERE key='recommendation_preferences'").Scan(&existing);if errors.Is(e,sql.ErrNoRows){value:=RecommendationPreferences{Revision:2,Weights:*weights};raw,e:=store.Encode(value);if e!=nil{return plan,e};if _,e=q.ExecContext(ctx,"INSERT INTO settings(key,value) VALUES('recommendation_preferences',?)",raw);e!=nil{return plan,e};plan.Counts["recommendationPreferencesRestored"]++}else if e!=nil{return plan,e}else{migrationIssue(&plan,"existing_recommendation_preferences_kept")}}
 if _,e=q.ExecContext(ctx,"DELETE FROM recommendation_snapshots");e!=nil{return plan,e};plan.State="committed";if partial{plan.State="partial"};if e=saveV2Plan(ctx,q,plan);e!=nil{return plan,e};return plan,nil
}
