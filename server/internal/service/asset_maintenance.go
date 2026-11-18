package service

import (
	"context"
	"path"
	"strings"
	"time"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/store"
)

var maintenanceFields=[]string{"authors","taxonomy","tags","rename"}

type AssetMaintenanceTask struct {
	ID string `json:"id"`
	AssetID string `json:"assetId"`
	LibraryID string `json:"libraryId"`
	ContentVersion int64 `json:"contentVersion"`
	Hash string `json:"hash"`
	ExpectedRevision int64 `json:"expectedRevision"`
	AssetFields []string `json:"assetFields"`
	Research bool `json:"research"`
	UsedUpdates int `json:"usedUpdates"`
	MaxUpdates int `json:"maxUpdates"`
	ExpiresAt time.Time `json:"expiresAt"`
}

func validateAssetPolicy(d *DelegationPolicy) error {
	if d.AssetFields!=nil {
		fields,err:=cleanStrings(d.AssetFields,4,32);if err!=nil{return err}
		if !withinStrings(fields,maintenanceFields){return Fail(400,"unknown_asset_field","作品维护权限包含未知字段。")}
		d.AssetFields=fields
	}
	if d.CanResearchAssets&&!d.CanResearch{return Fail(400,"research_required","已有作品研究需要同时开启研究权限。")}
	if (d.CanResearchAssets||len(d.AssetFields)>0)&&len(d.LibraryIDs)==0{return Fail(400,"library_required","已有作品维护须明确选择可操作的库。")}
	return nil
}
func(s *Service) prepareAssetMaintenance(ctx context.Context,id string,d Delegation,in DelegatedTaskStart)(AssetMaintenanceTask,error){
	var t AssetMaintenanceTask
	a,err:=s.Asset(ctx,in.AssetID);if err!=nil{return t,err}
	if (in.LibraryID!=""&&in.LibraryID!=a.LibraryID)||!containsString(d.LibraryIDs,a.LibraryID){return t,Fail(403,"library_scope_denied","当前授权未包含作品所在库。")}
	allowed:=[]string{}
	if in.AssetFields!=nil{allowed,err=cleanStrings(in.AssetFields,4,32);if err!=nil{return t,err}}
	if !withinStrings(allowed,d.AssetFields)||(!d.CanResearchAssets&&len(allowed)==0){return t,Fail(403,"asset_scope_denied","此作品任务超出了首次配置的权限。")}
	lib,err:=s.library(ctx,a.LibraryID);if err!=nil{return t,err}
	if !lib.Enabled||a.Trashed{return t,Fail(409,"asset_unavailable","库已停用或作品已进入回收站。")}
	if a.Revision!=in.ExpectedRevision||a.ContentVersion!=in.ExpectedContentVersion{return t,Conflict(a.Revision)}
	updates:=in.MaxUpdates;if updates==0{updates=d.MaxUpdates};if updates<1||updates>d.MaxUpdates{return t,Fail(403,"update_budget_denied","作品修改次数超出授权。")}
	return AssetMaintenanceTask{ID:id,AssetID:a.ID,LibraryID:a.LibraryID,ContentVersion:a.ContentVersion,Hash:a.Hash,
		ExpectedRevision:a.Revision,AssetFields:allowed,Research:d.CanResearchAssets,MaxUpdates:updates,ExpiresAt:limitExpiry(d,time.Duration(d.TaskSeconds)*time.Second)},nil
}
func (s *Service) assetMaintenance(ctx context.Context,id string)(AssetMaintenanceTask,error){
	t,err:=store.Get[AssetMaintenanceTask](ctx,s.DB.DB,"SELECT data FROM asset_maintenance_tasks WHERE id=?",id)
	return t,Normalize(err)
}
func saveAssetMaintenance(ctx context.Context,q store.Querier,t AssetMaintenanceTask)error{
	raw,err:=store.Encode(t);if err!=nil{return err}
	_,err=q.ExecContext(ctx,"INSERT INTO asset_maintenance_tasks(id,asset_id,data) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET data=excluded.data",t.ID,t.AssetID,raw)
	return err
}
func(s *Service) AssetTaskContext(ctx context.Context,p model.Principal,id string)(AssetMaintenanceTask,error){
	if !p.Has("admin")&&(p.AssetTaskID!=id||id==""){return AssetMaintenanceTask{},Fail(403,"asset_task_scope_denied","当前凭据未绑定这个作品任务。")}
	return s.assetMaintenance(ctx,id)
}
func(s *Service) activeAssetTask(ctx context.Context,p model.Principal,id string)(AssetMaintenanceTask,model.Asset,error){
	var a model.Asset
	t,err:=s.AssetTaskContext(ctx,p,id);if err!=nil{return t,a,err}
	if !t.ExpiresAt.After(time.Now()){return t,a,Fail(409,"asset_task_expired","作品维护任务已到期。")}
	a,err=s.Asset(ctx,t.AssetID);if err!=nil{return t,a,err}
	lib,err:=s.library(ctx,a.LibraryID);if err!=nil{return t,a,err}
	if !lib.Enabled||a.Trashed||a.LibraryID!=t.LibraryID||a.ContentVersion!=t.ContentVersion||a.Hash!=t.Hash{return t,a,Fail(409,"asset_version_changed","作品或其来源范围已变化，请重新核对。")}
	return t,a,nil
}
func protectHumanField(ctx context.Context,q store.Querier,id,field string)error{
	_,err:=q.ExecContext(ctx,"INSERT OR IGNORE INTO asset_human_fields(asset_id,field) VALUES(?,?)",id,field)
	return err
}
func(s *Service) allowAssetField(ctx context.Context,t AssetMaintenanceTask,field string)error{
	if !containsString(t.AssetFields,field){return Fail(403,"asset_field_denied","此任务未授权修改该作品字段。")}
	var locked bool
	if err:=s.DB.DB.QueryRowContext(ctx,"SELECT EXISTS(SELECT 1 FROM asset_human_fields WHERE asset_id=? AND field=?)",t.AssetID,field).Scan(&locked);err!=nil{return err}
	if locked{return Fail(409,"human_field_protected","该字段有人工修改或清空记录，请保留现有意图。")}
	return nil
}

type AssetMaintenanceEvidence struct{ Field string `json:"field"`; URL string `json:"url"`; Observation string `json:"observation"` }
type AssetMaintenanceRecord struct{TaskID string `json:"taskId"`;Revision int64 `json:"revision"`;Evidence []AssetMaintenanceEvidence `json:"evidence"`}
func(s *Service) AssetTaskEvidence(ctx context.Context,p model.Principal,id,cursor string,count int)(model.Page[AssetMaintenanceRecord],error){
	var page model.Page[AssetMaintenanceRecord]
	t,err:=s.AssetTaskContext(ctx,p,id);if err!=nil{return page,err}
	n,err:=limited(count,20);if err!=nil{return page,err}
	filter:="asset-evidence:"+t.ID
	offset,err:=pageOffset(cursor,filter);if err!=nil{return page,err}
	items,err:=store.List[AssetMaintenanceRecord](ctx,s.DB.DB,`SELECT json_object('taskId',e.task_id,'revision',e.revision,'evidence',json(e.data))
	 FROM asset_maintenance_evidence e JOIN asset_maintenance_tasks t ON e.task_id=t.id WHERE t.asset_id=? AND json_extract(t.data,'$.contentVersion')=?
	 ORDER BY e.revision,e.task_id LIMIT ? OFFSET ?`,t.AssetID,t.ContentVersion,n+1,offset);if err!=nil{return page,err}
	return paged(items,n,offset,filter),nil
}
type AssetMaintenanceUpdate struct {
	ExpectedRevision int64 `json:"expectedRevision"`
	AuthorIDs *[]string `json:"authorIds"`
	SourceIDs *[]string `json:"sourceIds"`
	CharacterIDs *[]string `json:"characterIds"`
	Tags *[]string `json:"tags"`
	Evidence []AssetMaintenanceEvidence `json:"evidence"`
}
func(t AssetMaintenanceTask) checkWrite(p model.Principal,a model.Asset,revision int64)error{
	if !p.Has("asset.metadata.write"){return Fail(403,"asset_write_denied","未授予作品资料写入权限。")}
	if a.Revision!=t.ExpectedRevision||a.Revision!=revision{return Conflict(a.Revision)}
	if t.UsedUpdates>=t.MaxUpdates{return Fail(409,"asset_updates_exhausted","此任务的修改次数已用完。")}
	return nil
}
func checkAssetEvidence(fields []string,evidence []AssetMaintenanceEvidence)error{
	if len(fields)==0||len(evidence)<1||len(evidence)>12{return Fail(400,"asset_evidence_required","请提交实际字段、来源及核对依据。")}
	present:=map[string]bool{}
	for _,e:=range evidence {
		if !containsString(fields,e.Field)||e.URL==""||!validURL(e.URL)||len(e.URL)>2000||strings.TrimSpace(e.Observation)==""||len(e.Observation)>2000{return Fail(400,"invalid_asset_evidence","每条证据须对应本次字段且包含有效出处与观察。")}
		present[e.Field]=true
	}
	for _,field:=range fields{if !present[field]{return Fail(400,"asset_evidence_required","有字段缺少自己的核对依据。")}}
	return nil
}
func(s *Service) ApplyAssetMaintenance(ctx context.Context,w *Write,id string,in AssetMaintenanceUpdate)(AssetMaintenanceTask,error){
	t,a,err:=s.activeAssetTask(ctx,w.Principal,id);if err!=nil{return t,err}
	if err=t.checkWrite(w.Principal,a,in.ExpectedRevision);err!=nil{return t,err}
	fields:=[]string{}
	if in.AuthorIDs!=nil{fields=append(fields,"authors")}
	if in.SourceIDs!=nil||in.CharacterIDs!=nil{fields=append(fields,"taxonomy")}
	if in.Tags!=nil{fields=append(fields,"tags")}
	if err=checkAssetEvidence(fields,in.Evidence);err!=nil{return t,err}
	for _,field:=range fields{if err=s.allowAssetField(ctx,t,field);err!=nil{return t,err}}
	notMissing:=func()error{return Fail(409,"asset_field_occupied","该字段已填写或曾被清空，本任务只补全缺失资料。")}
	if in.AuthorIDs!=nil&&(len(*in.AuthorIDs)==0||a.AuthorID!="unknown"||containsString(*in.AuthorIDs,"unknown")){return t,notMissing()}
	if in.SourceIDs!=nil||in.CharacterIDs!=nil {
		sources,characters:=a.SourceIDs,a.CharacterIDs
		if in.SourceIDs!=nil{if len(*in.SourceIDs)==0||len(sources)>0||a.Source!=""{return t,notMissing()};sources=*in.SourceIDs}
		if in.CharacterIDs!=nil{if len(*in.CharacterIDs)==0||len(characters)>0||len(a.Characters)>0{return t,notMissing()};characters=*in.CharacterIDs}
		previous:=append([]string(nil),a.SourceIDs...)
		if err=s.resolveTaxonomy(ctx,&a,sources,characters);err!=nil{return t,err}
		if len(previous)>0&&!withinStrings(a.SourceIDs,previous){return t,notMissing()}
	}
	if in.Tags!=nil {
		if len(*in.Tags)==0||len(a.Tags)>0||a.TagSetSource!=""{return t,notMissing()}
		a.Tags,err=s.validateAssetTags(ctx,*in.Tags);if err!=nil{return t,err}
		a.TagsRevision++;a.TagSetSource="ai"
	}
	q,err:=w.Q();if err!=nil{return t,err}
	previousAuthors:=a.AuthorIDs
	if in.AuthorIDs!=nil{
		if err=s.replaceAssetAuthors(ctx,w,q,&a,AssetUpdate{AuthorIDs:in.AuthorIDs,ExpectedAuthorsRevision:a.AuthorsRevision});err!=nil{return t,err}
	}
	a.Revision++;a.UpdatedAt=time.Now().UTC()
	if in.Tags!=nil{a.TagSetUpdatedAt=&a.UpdatedAt}
	if err=store.SaveAsset(ctx,q,a);err!=nil{return t,err}
	if in.AuthorIDs!=nil{if err=s.recordExplicitAuthorSet(ctx,q,previousAuthors,a);err!=nil{return t,err}}
	raw,err:=store.Encode(in.Evidence);if err!=nil{return t,err}
	if _,err=q.ExecContext(ctx,"INSERT INTO asset_maintenance_evidence(task_id,revision,data) VALUES(?,?,?)",t.ID,a.Revision,raw);err!=nil{return t,err}
	t.ExpectedRevision=a.Revision;t.UsedUpdates++
	return t,saveAssetMaintenance(ctx,q,t)
}

type AssetTaskRename struct {
	ExpectedRevision int64 `json:"expectedRevision"`
	NamespaceFingerprint string `json:"namespaceFingerprint"`
	Target string `json:"target"`
	Preview NamingPreviewInput `json:"preview"`
}
func(s *Service) RenameAssetForTask(ctx context.Context,w *Write,id string,in AssetTaskRename)(model.Asset,error){
	t,a,err:=s.activeAssetTask(ctx,w.Principal,id);if err!=nil{return a,err}
	if err=t.checkWrite(w.Principal,a,in.ExpectedRevision);err!=nil{return a,err}
	if err=s.allowAssetField(ctx,t,"rename");err!=nil{return a,err}
	if len(in.Preview.Items)!=1||in.Preview.AssetID!=""&&in.Preview.AssetID!=a.ID||path.Dir(in.Target)!=path.Dir(a.RelativePath){return a,Fail(400,"asset_rename_scope","改名仅适用于当前作品与目录。")}
	if in.Preview.Intent=="rename_exact" {in.Preview.AssetID=a.ID;in.Preview.ExpectedRevision=a.Revision}
	if in.Preview.Intent!="rename_exact"&&in.Preview.Intent!="new_work"{return a,Fail(400,"asset_rename_intent","现有单项改名请选择规范续号或精确名称。")}
	preview,err:=s.previewNamesLocked(ctx,a.LibraryID,in.Preview);if err!=nil{return a,err}
	if len(preview.Items)!=1||preview.NamespaceFingerprint!=in.NamespaceFingerprint||preview.Items[0].RelativePath!=in.Target{return a,Fail(409,"naming_namespace_changed","目录命名已变化，请重新读取 Go 命名预览。")}
	return s.moveAsset(ctx,w,a,in.Target,in.ExpectedRevision,&t)
}
