package service

import (
 "context"
 "database/sql"
 "encoding/base64"
 "encoding/json"
 "errors"
 "regexp"
 "strconv"
 "time"

 legacy "qimeng.local/next/server/internal/migration"
 "qimeng.local/next/server/internal/model"
 "qimeng.local/next/server/internal/store"
)

type V2Import struct {
 ID string `json:"id"`
 SourceID string `json:"sourceId"`
 SHA256 string `json:"sha256"`
 Bytes int64 `json:"bytes"`
 CreatedAt time.Time `json:"createdAt"`
 MediaCount int `json:"mediaCount"`
 AuthorCount int `json:"authorCount"`
 EventCount int `json:"eventCount"`
 FragmentCount int `json:"fragmentCount"`
 SourceHashesAvailable bool `json:"sourceHashesAvailable"`
}
type V2MediaMapping struct {RecordKey string `json:"recordKey"`;AssetID string `json:"assetId"`}
type V2AuthorMapping struct {SourceAuthorID string `json:"sourceAuthorId"`;AuthorID string `json:"authorId"`}
type V2PlanCreate struct {MediaMappings []V2MediaMapping `json:"mediaMappings"`;AuthorMappings []V2AuthorMapping `json:"authorMappings"`}
type V2MigrationPlan struct {
 ID string `json:"id"`
 ImportID string `json:"importId"`
 Fingerprint string `json:"fingerprint"`
 State string `json:"state"`
 CreatedAt time.Time `json:"createdAt"`
 CheckpointID string `json:"checkpointId,omitempty"`
 MediaCount int `json:"mediaCount"`
 MatchedCount int `json:"matchedCount"`
 SkippedCount int `json:"skippedCount"`
 AuthorCount int `json:"authorCount"`
 IssueCount int `json:"issueCount"`
 Issues []string `json:"issues"`
 Counts map[string]int64 `json:"counts"`
}
type V2MigrationItem struct {
 Ordinal int `json:"ordinal"`
 RecordKey string `json:"recordKey"`
 FileName string `json:"fileName"`
 AssetID string `json:"assetId,omitempty"`
 State string `json:"state"`
 Reason string `json:"reason"`
 ExpectedRevision int64 `json:"expectedRevision,omitempty"`
 ExpectedContentVersion int64 `json:"expectedContentVersion,omitempty"`
 TargetHash string `json:"targetHash,omitempty"`
 TagAction string `json:"tagAction"`
 Issues []string `json:"issues"`
}
type migrationItemRecord struct {
 V2MigrationItem
 Tags legacy.TagDecision `json:"tags"`
 AuthorsAction string `json:"authorsAction"`
 AuthorIDs []string `json:"authorIds"`
}
type V2MigrationAuthor struct {
 Ordinal int `json:"ordinal"`
 SourceAuthorID string `json:"sourceAuthorId"`
 Name string `json:"name"`
 Kind string `json:"kind"`
 AuthorID string `json:"authorId,omitempty"`
 ExpectedRevision int64 `json:"expectedRevision,omitempty"`
 State string `json:"state"`
 Reason string `json:"reason"`
}

var migrationSourceID=regexp.MustCompile(`^[A-Za-z0-9._-]{8,128}$`)
func validateV2Profiles(ctx context.Context,input legacy.V2)(map[string]*ImportedAuthorProfile,int64,error){
 profiles:=map[string]*ImportedAuthorProfile{};var bytes int64
 for _,author:=range input.Data.Authors{if author.ProfileJSON==""{continue};avatar,e:=base64.StdEncoding.DecodeString(author.AvatarData);if e!=nil{return nil,0,Fail(400,"invalid_imported_avatar","迁入离线头像不是有效标准 Base64。")};profile,e:=PrepareImportedAuthorProfile(ctx,author.AuthorID,json.RawMessage(author.ProfileJSON),avatar);if e!=nil{return nil,0,e};profiles[author.AuthorID]=profile;bytes+=profile.AvatarStorageBytes()}
 return profiles,bytes,nil
}
func(s *Service)StageV2Import(ctx context.Context,w *Write,sourceID string,raw []byte)(V2Import,error){
 var result V2Import;if e:=Authorize(w.Principal,"admin","");e!=nil{return result,e};if !migrationSourceID.MatchString(sourceID){return result,Fail(400,"invalid_migration_source","来源安装标识须为 8–128 位字母、数字、点、横线或下划线。")}
 input,e:=legacy.DecodeV2(raw);if e!=nil{return result,Fail(400,"invalid_v2_document",e.Error())};_,profileBytes,e:=validateV2Profiles(ctx,input);if e!=nil{return result,e};if _,e=importedWeights(input.Data.AppPrefs);e!=nil{return result,e}
 hash:=digest(raw);old,e:=store.Get[V2Import](ctx,s.DB.DB,"SELECT data FROM migration_inputs WHERE source_id=? AND sha256=?",sourceID,hash);if e==nil{return old,nil};if !errors.Is(e,sql.ErrNoRows){return result,e}
 free,e:=freeBytes(s.Config.DataDir);if e!=nil{return result,e};if free-int64(len(raw))*2-profileBytes-(4<<20)<s.Config.ReserveBytes{return result,Fail(507,"insufficient_storage","空间不足以持久保留迁移输入与业务余量。")}
 result=V2Import{ID:w.ID("v2-input"),SourceID:sourceID,SHA256:hash,Bytes:int64(len(raw)),CreatedAt:time.Now().UTC(),MediaCount:len(input.Data.MediaFiles),AuthorCount:len(input.Data.Authors),EventCount:len(input.Data.ViewEvents),FragmentCount:len(input.Data.TxtFragments)}
 data,e:=store.Encode(result);if e!=nil{return result,e};q,e:=w.Q();if e!=nil{return result,e};_,e=q.ExecContext(ctx,"INSERT INTO migration_inputs(id,source_id,sha256,created_at,data,original) VALUES(?,?,?,?,?,?)",result.ID,result.SourceID,hash,dateKey(result.CreatedAt),data,raw);return result,e
}
func(s *Service)V2Import(ctx context.Context,p model.Principal,id string)(V2Import,error){if e:=Authorize(p,"admin","");e!=nil{return V2Import{},e};v,e:=store.Get[V2Import](ctx,s.DB.DB,"SELECT data FROM migration_inputs WHERE id=?",id);return v,Normalize(e)}
func(s *Service)V2Imports(ctx context.Context,p model.Principal,cursor string,limit int)(model.Page[V2Import],error){
 var page model.Page[V2Import];if e:=Authorize(p,"admin","");e!=nil{return page,e};n,e:=limited(limit,20);if e!=nil{return page,e};seek,e:=readSeek(cursor,"v2-inputs","created_desc");if e!=nil{return page,e}
 values,e:=store.List[V2Import](ctx,s.DB.DB,"SELECT data FROM migration_inputs WHERE (?='' OR (created_at,id)<(?,?)) ORDER BY created_at DESC,id DESC LIMIT ?",seek.ID,seek.Value,seek.ID,n+1);if e!=nil{return page,e};page.Items=values;if len(values)>n{page.Items=values[:n];last:=page.Items[n-1];page.NextCursor=makeSeek("v2-inputs","created_desc",dateKey(last.CreatedAt),last.ID)};return page,nil
}
func(s *Service)V2Document(ctx context.Context,p model.Principal,id string)([]byte,error){if e:=Authorize(p,"admin","");e!=nil{return nil,e};var raw []byte;e:=s.DB.DB.QueryRowContext(ctx,"SELECT original FROM migration_inputs WHERE id=?",id).Scan(&raw);return raw,Normalize(e)}
func(s *Service)loadV2(ctx context.Context,id string)(V2Import,legacy.V2,error){
 metadata,e:=store.Get[V2Import](ctx,s.DB.DB,"SELECT data FROM migration_inputs WHERE id=?",id);if e!=nil{return metadata,legacy.V2{},Normalize(e)};var raw []byte;if e=s.DB.DB.QueryRowContext(ctx,"SELECT original FROM migration_inputs WHERE id=?",id).Scan(&raw);e!=nil{return metadata,legacy.V2{},e};if digest(raw)!=metadata.SHA256{return metadata,legacy.V2{},Fail(409,"migration_input_changed","已保存迁移输入的摘要不符，拒绝继续。")};input,e:=legacy.DecodeV2(raw);return metadata,input,e
}
func(s *Service)V2MigrationPlan(ctx context.Context,p model.Principal,id string)(V2MigrationPlan,error){if e:=Authorize(p,"admin","");e!=nil{return V2MigrationPlan{},e};v,e:=store.Get[V2MigrationPlan](ctx,s.DB.DB,"SELECT data FROM migration_plans WHERE id=?",id);return v,Normalize(e)}
func migrationOrdinal(cursor,key string)(int,error){seek,e:=readSeek(cursor,key,"ordinal");if e!=nil{return 0,e};if seek.ID==""{return -1,nil};n,e:=strconv.Atoi(seek.Value);if e!=nil||n<0{return 0,Fail(400,"invalid_cursor","迁移清单游标无效。")};return n,nil}
func(s *Service)V2MigrationItems(ctx context.Context,p model.Principal,id,cursor string,limit int)(model.Page[V2MigrationItem],error){
 page:=model.Page[V2MigrationItem]{Items:[]V2MigrationItem{}};if _,e:=s.V2MigrationPlan(ctx,p,id);e!=nil{return page,e};n,e:=limited(limit,50);if e!=nil{return page,e};key:="v2-items:"+id;o,e:=migrationOrdinal(cursor,key);if e!=nil{return page,e};rows,e:=store.List[migrationItemRecord](ctx,s.DB.DB,"SELECT data FROM migration_items WHERE plan_id=? AND ordinal>? ORDER BY ordinal LIMIT ?",id,o,n+1);if e!=nil{return page,e};for _,row:=range rows[:min(n,len(rows))]{page.Items=append(page.Items,row.V2MigrationItem)};if len(rows)>n{last:=page.Items[n-1];page.NextCursor=makeSeek(key,"ordinal",strconv.Itoa(last.Ordinal),last.RecordKey)};return page,nil
}
func(s *Service)V2MigrationAuthors(ctx context.Context,p model.Principal,id,cursor string,limit int)(model.Page[V2MigrationAuthor],error){
 page:=model.Page[V2MigrationAuthor]{Items:[]V2MigrationAuthor{}};if _,e:=s.V2MigrationPlan(ctx,p,id);e!=nil{return page,e};n,e:=limited(limit,20);if e!=nil{return page,e};key:="v2-authors:"+id;o,e:=migrationOrdinal(cursor,key);if e!=nil{return page,e};rows,e:=store.List[V2MigrationAuthor](ctx,s.DB.DB,"SELECT data FROM migration_authors WHERE plan_id=? AND ordinal>? ORDER BY ordinal LIMIT ?",id,o,n+1);if e!=nil{return page,e};page.Items=rows[:min(n,len(rows))];if len(rows)>n{last:=page.Items[n-1];page.NextCursor=makeSeek(key,"ordinal",strconv.Itoa(last.Ordinal),last.SourceAuthorID)};return page,nil
}
func migrationIssue(plan *V2MigrationPlan,code string){plan.IssueCount++;if len(plan.Issues)<100{plan.Issues=append(plan.Issues,code)}}
func saveV2Plan(ctx context.Context,q store.Querier,plan V2MigrationPlan)error{raw,e:=store.Encode(plan);if e!=nil{return e};_,e=q.ExecContext(ctx,"INSERT INTO migration_plans(id,input_id,created_at,data) VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET data=excluded.data",plan.ID,plan.ImportID,dateKey(plan.CreatedAt),raw);return e}
