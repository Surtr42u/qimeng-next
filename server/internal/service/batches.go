package service

import (
 "context"
 "database/sql"
 "encoding/json"
 "errors"
 "fmt"
 "path"
 "sort"
 "strconv"
 "strings"
 "time"

 "qimeng.local/next/server/internal/domain"
 "qimeng.local/next/server/internal/model"
 "qimeng.local/next/server/internal/store"
)

func newBatch(id,library,mode,label string,budget int)model.Batch{now:=time.Now().UTC();return model.Batch{ID:id,LibraryID:library,Mode:mode,Label:label,MaxResearchCalls:budget,State:"receiving",Revision:1,ManifestRevision:1,ManifestState:"open",BackupState:"not_covered",CleanupState:"retained",CreatedAt:now,UpdatedAt:now}}
func(s *Service)Batch(ctx context.Context,id string)(model.Batch,error){b,e:=store.Get[model.Batch](ctx,s.DB.DB,"SELECT data FROM batches WHERE id=?",id);return b,Normalize(e)}
func(s *Service)CreateBatch(ctx context.Context,w *Write,in model.Batch)(model.Batch,error){
 if e:=Authorize(w.Principal,"admin","");e!=nil{return in,e};b,e:=s.prepareBatch(ctx,w.ID("batch"),in);if e!=nil{return b,e};q,e:=w.Q();if e!=nil{return b,e};return b,store.SaveBatch(ctx,q,b)
}
func(s *Service)prepareBatch(ctx context.Context,id string,in model.Batch)(model.Batch,error){
 library,e:=s.library(ctx,in.LibraryID);if e!=nil{return in,e};if !library.Enabled{return in,Fail(409,"library_disabled","目标库已停用。")};if (in.Mode!="ordinary"&&in.Mode!="intelligent")||len(in.Label)>200||strings.TrimSpace(in.Label)==""||in.MaxResearchCalls<0||in.MaxResearchCalls>100{return in,Fail(400,"invalid_batch","批次名称、模式或预算无效。")}
 b:=newBatch(id,in.LibraryID,in.Mode,in.Label,in.MaxResearchCalls);if in.Mode=="intelligent"{b.State="waiting_executor"};return b,nil
}
func(s *Service)Batches(ctx context.Context,p model.Principal,cursor string,limit int)(model.Page[model.Batch],error){n,e:=limited(limit,50);if e!=nil{return model.Page[model.Batch]{},e};key:="batches:"+p.BatchID;o,e:=pageOffset(cursor,key);if e!=nil{return model.Page[model.Batch]{},e};a,e:=store.List[model.Batch](ctx,s.DB.DB,"SELECT data FROM batches WHERE (?='' OR id=?) ORDER BY created_at DESC,id DESC LIMIT ? OFFSET ?",p.BatchID,p.BatchID,n+1,o);return paged(a,n,o,key),e}
func(s *Service)BatchDetail(ctx context.Context,p model.Principal,id string)(model.BatchDetail,error){
 if e:=Authorize(p,"read",id);e!=nil{return model.BatchDetail{},e};b,e:=s.Batch(ctx,id);if e!=nil{return model.BatchDetail{},e}
 inputs,e:=s.batchInputs(ctx,s.DB.DB,id);if e!=nil{return model.BatchDetail{},e}
 uploads,e:=store.List[model.Upload](ctx,s.DB.DB,"SELECT u.data FROM uploads u LEFT JOIN batch_inputs i ON i.batch_id=u.batch_id AND i.upload_id=u.id WHERE u.batch_id=? ORDER BY i.ordinal,u.id LIMIT 100",id);if e!=nil{return model.BatchDetail{},e};items:=make([]model.BatchItem,0,len(uploads))
 for _,u:=range uploads{r,e:=store.Get[model.Research](ctx,s.DB.DB,"SELECT data FROM research WHERE upload_id=?",u.ID);if e!=nil&&!errors.Is(e,sql.ErrNoRows){return model.BatchDetail{},e};if r.Candidates==nil{r.Candidates=[]model.ResearchCandidate{}};items=append(items,model.BatchItem{Upload:u,ResearchOutcome:r.Outcome,Candidates:r.Candidates})}
 plans,e:=store.List[model.PlanSummary](ctx,s.DB.DB,"SELECT json_object('id',id,'state',json_extract(data,'$.state'),'fingerprint',json_extract(data,'$.fingerprint')) FROM plans WHERE batch_id=? ORDER BY id LIMIT 100",id);if e!=nil{return model.BatchDetail{},e};return model.BatchDetail{Batch:b,Inputs:inputs,Items:items,Plans:plans},nil
}
func(s *Service)refreshBatch(ctx context.Context,q store.Querier,id string)error{
 b,e:=store.Get[model.Batch](ctx,q,"SELECT data FROM batches WHERE id=?",id);if e!=nil{return e};if b.State=="cancelled"{return nil}
 var total,ingested,staged,receiving,problems int
 e=q.QueryRowContext(ctx,"SELECT count(*),COALESCE(sum(state='ingested'),0),COALESCE(sum(state='staged'),0),COALESCE(sum(state='receiving'),0),COALESCE(sum(state IN ('duplicate','failed','cancelled') OR (state='staged' AND COALESCE(json_extract(data,'$.errorCode'),'')!='')),0) FROM uploads WHERE batch_id=?",id).Scan(&total,&ingested,&staged,&receiving,&problems);if e!=nil{return e}
 var declared,bound,cancelled int
 if e=q.QueryRowContext(ctx,"SELECT count(*),count(upload_id),coalesce(sum(cancelled),0) FROM batch_inputs WHERE batch_id=?",id).Scan(&declared,&bound,&cancelled);e!=nil{return e}
 if b.ManifestState=="sealed"&&(declared!=b.ExpectedInputs||total!=bound){return Fail(500,"manifest_corrupt","已收文件与原始输入映射不一致，不能报告完成。")}
 switch{case b.ManifestState=="sealed"&&declared>0&&total==declared&&ingested==declared&&cancelled==0:b.State="completed";case problems>0||ingested>0||cancelled>0:b.State="partial";case receiving>0||b.ManifestState!="sealed"||bound<declared:b.State="receiving";case staged>0:b.State="waiting_executor";default:b.State="receiving"};b.Revision++;b.UpdatedAt=time.Now().UTC();return store.SaveBatch(ctx,q,b)
}
func(s *Service)CancelBatch(ctx context.Context,w *Write,id string,revision int64)(model.Batch,error){
 if e:=Authorize(w.Principal,"admin",id);e!=nil{return model.Batch{},e};b,e:=s.Batch(ctx,id);if e!=nil{return b,e};if b.Revision!=revision{return b,Conflict(b.Revision)};b.State="cancelled";b.Revision++;b.UpdatedAt=time.Now().UTC();q,e:=w.Q();if e!=nil{return b,e};return b,store.SaveBatch(ctx,q,b)
}
func(s *Service)Context(ctx context.Context,p model.Principal,id,cursor string,limit int)(map[string]any,error){
 d,e:=s.BatchDetail(ctx,p,id);if e!=nil{return nil,e};n,e:=limited(limit,20);if e!=nil{return nil,e};key:=fmt.Sprintf("context:%s:%d",id,d.Batch.Revision);o,e:=pageOffset(cursor,key);if e!=nil{return nil,e};total:=len(d.Inputs);if d.Batch.ManifestState=="legacy_unverified"{total=len(d.Items)};if o>total{return nil,Fail(400,"invalid_cursor","分页游标超出批次范围。")};end:=min(o+n,total)
 inputs:=[]model.BatchInput{};items:=[]model.BatchItem{}
 if d.Batch.ManifestState=="legacy_unverified"{items=d.Items[o:end]}else{inputs=d.Inputs[o:end];selected:=map[string]bool{};for _,input:=range inputs{selected[input.UploadID]=true};for _,item:=range d.Items{if selected[item.Upload.ID]{items=append(items,item)}}}
 actions:=[]string{};if d.Batch.State!="cancelled"&&d.Batch.State!="completed"{for _,item:=range d.Items{switch item.Upload.State{case "receiving":actions=appendUnique(actions,"appendUpload","completeUpload");case "staged":actions=appendUnique(actions,"preparePlan");if item.ResearchOutcome==""&&d.Batch.ResearchCalls<d.Batch.MaxResearchCalls{actions=appendUnique(actions,"submitResearch")};case "duplicate":actions=appendUnique(actions,"awaitUserDuplicateDecision")}};if len(d.Plans)>0{actions=appendUnique(actions,"getPlan","commitPlan")}}
 if d.Batch.State=="cancelled"||d.Batch.ManifestState=="legacy_unverified"{actions=[]string{}}else if d.Batch.ManifestState=="open"{actions=[]string{"intake.register"}}else if d.Batch.ManifestState=="sealed"{for _,input:=range d.Inputs{if input.State=="pending"{actions=appendUnique(actions,"upload.file")}}}
 result:=map[string]any{"batch":d.Batch,"policyVersion":"2","nextActions":actions,"remainingResearchCalls":d.Batch.MaxResearchCalls-d.Batch.ResearchCalls,"inputs":inputs,"items":items,"limitations":[]string{"研究候选不等于确认归属；未知作者可入其他搁置。","批次外、人工修正、替换与删除不在新增授权内。","外部模型调用消费由可信执行器控制并回报；服务端不声称限制未接入的外部消费。","独立备份和来源回收尚未接线，来源必须保留。"}}
 if end<total{result["nextCursor"]=nextCursor(end,key)};return result,nil
}
func appendUnique(values []string,more ...string)[]string{for _,v:=range more{found:=false;for _,x:=range values{if x==v{found=true;break}};if !found{values=append(values,v)}};return values}
func(s *Service)SubmitResearch(ctx context.Context,w *Write,id string,in model.Research)(model.Batch,error){
 if e:=Authorize(w.Principal,"research",id);e!=nil{return model.Batch{},e};b,e:=s.Batch(ctx,id);if e!=nil{return b,e};if b.Revision!=in.ExpectedRevision{return b,Conflict(b.Revision)};if b.State=="cancelled"||b.State=="completed"{return b,Fail(409,"batch_closed","批次已结束，不会启动再次研究。")}
 u,e:=s.Upload(ctx,w.Principal,in.UploadID);if e!=nil{return b,e};if u.BatchID!=id||u.Hash==""||u.Hash!=in.ExpectedHash||u.State!="staged"{return b,Fail(409,"research_scope_conflict","研究结果不适用于当前批次输入版本。")}
 if in.CallsUsed<1||b.ResearchCalls+in.CallsUsed>b.MaxResearchCalls{return b,Fail(409,"research_budget_exhausted","研究额度已用尽，请保留现有证据并停止。")}
 if (in.Outcome!="candidates"&&in.Outcome!="unknown"&&in.Outcome!="conflict")||in.Candidates==nil||len(in.Candidates)>20{return b,Fail(400,"invalid_research","研究结果格式无效。")}
 for _,c:=range in.Candidates{if c.SourceURL==""||!validURL(c.SourceURL)||len(c.SourceURL)>2000||c.Evidence==""||len(c.Evidence)>4000||len(c.Name)>200{return b,Fail(400,"invalid_evidence","候选必须有有效来源及有界证据。")}}
 old,e:=store.Get[model.Research](ctx,s.DB.DB,"SELECT data FROM research WHERE upload_id=?",in.UploadID);if e!=nil&&!errors.Is(e,sql.ErrNoRows){return b,e};if e==nil{a,_:=json.Marshal(old.Candidates);c,_:=json.Marshal(in.Candidates);if old.Outcome==in.Outcome&&string(a)==string(c){return b,Fail(409,"no_research_progress","相同证据已经保存；停止无进展重复研究。")}}
 raw,e:=store.Encode(in);if e!=nil{return b,e};q,e:=w.Q();if e!=nil{return b,e};if _,e=q.ExecContext(ctx,"INSERT INTO research(upload_id,data) VALUES(?,?) ON CONFLICT(upload_id) DO UPDATE SET data=excluded.data",in.UploadID,raw);e!=nil{return b,e};b.ResearchCalls+=in.CallsUsed;b.Revision++;b.UpdatedAt=time.Now().UTC();return b,store.SaveBatch(ctx,q,b)
}
func(s *Service)Plan(ctx context.Context,p model.Principal,batchID,id string)(model.Plan,error){if e:=Authorize(p,"read",batchID);e!=nil{return model.Plan{},e};v,e:=store.Get[model.Plan](ctx,s.DB.DB,"SELECT data FROM plans WHERE id=? AND batch_id=?",id,batchID);return v,Normalize(e)}
// namespaceFingerprint protects the authoritative namespace and content versions.
// Actual disk occupancy and original content are separately verified at publication.
func(s *Service)namespaceFingerprint(ctx context.Context,library string,ignore map[string]bool)(string,error){
 rows,e:=s.DB.DB.QueryContext(ctx,"SELECT id,relative_path,hash,revision FROM assets WHERE library_id=? AND trashed=0 ORDER BY id",library);if e!=nil{return "",e};entries:=[]string{}
 for rows.Next(){var id,p,h string;var rev int64;if e=rows.Scan(&id,&p,&h,&rev);e!=nil{rows.Close();return "",e};if !ignore[id]{entries=append(entries,id+"\x00"+p+"\x00"+h+"\x00"+strconv.FormatInt(rev,10))}};e=rows.Err();rows.Close();if e!=nil{return "",e};sort.Strings(entries);raw,_:=json.Marshal(entries);return digest(raw),nil
}
func(s *Service)PreparePlan(ctx context.Context,w *Write,id string,in model.PlanCreate)(model.Plan,error){
 if e:=Authorize(w.Principal,"organize",id);e!=nil{return model.Plan{},e};b,e:=s.Batch(ctx,id);if e!=nil{return model.Plan{},e};if b.ManifestState!="sealed"{return model.Plan{},Fail(409,"manifest_required","须先完整登记原始输入，不能依据实际上传项自证没有遗漏。")};if b.Revision!=in.ExpectedRevision{return model.Plan{},Conflict(b.Revision)};if b.Mode!="intelligent"||b.State=="cancelled"||b.State=="completed"{return model.Plan{},Fail(409,"batch_closed","本批不能再准备智能入库计划。")};if len(in.Items)==0||len(in.Items)>100{return model.Plan{},Fail(400,"invalid_plan","计划须包含 1–100 项。")}
 var planCount int;if e=s.DB.DB.QueryRowContext(ctx,"SELECT count(*) FROM plans WHERE batch_id=?",id).Scan(&planCount);e!=nil{return model.Plan{},e};if planCount>=100{return model.Plan{},Fail(409,"plan_limit","本批计划数量达到上限。")}
 fingerprint,e:=s.namespaceFingerprint(ctx,b.LibraryID,nil);if e!=nil{return model.Plan{},e}
 p:=model.Plan{PlanSummary:model.PlanSummary{ID:w.ID("plan"),State:"prepared"},BatchID:id,BatchRevision:b.Revision,DirectoryFingerprint:fingerprint,Items:[]model.PlanItem{}}
 type group struct{family,class,dir,collection,sep string;number,width,lastPage int}
 groups:=map[string]*group{};inventories:=map[string][]string{};seen:=map[string]bool{};collections:=[]model.Collection{};dramaCounts:=map[string]int{}
 for _,input:=range in.Items{
  if seen[input.UploadID]||!domain.ValidName(input.Family){return p,Fail(400,"invalid_plan","重复输入或无效命名基名。")};seen[input.UploadID]=true
  u,e:=s.Upload(ctx,w.Principal,input.UploadID);if e!=nil{return p,e};if u.BatchID!=id||u.State!="staged"||u.Hash==""||u.ReplaceAssetID!=""{return p,Fail(409,"plan_input_conflict","计划只能引用本批已完整接收的新文件。")}
  r,e:=store.Get[model.Research](ctx,s.DB.DB,"SELECT data FROM research WHERE upload_id=?",u.ID);if e!=nil&&!errors.Is(e,sql.ErrNoRows){return p,e};if e==nil&&r.Outcome=="conflict"{return p,Fail(409,"research_conflict","此项证据冲突，须由用户解决后再准备计划。")}
  authorIDs:=input.AuthorIDs;if len(authorIDs)==0&&input.AuthorID!=""{authorIDs=[]string{input.AuthorID}};if len(authorIDs)==0{authorIDs=u.AuthorIDs};authors,e:=s.resolveAuthors(ctx,b.LibraryID,authorIDs);if e!=nil{return p,e};authorIDs=[]string{};for _,a:=range authors{authorIDs=append(authorIDs,a.ID)};authorID:=authors[0].ID
  sourceIDs:=input.SourceIDs;characterIDs:=input.CharacterIDs;if sourceIDs==nil{sourceIDs=u.SourceIDs};if characterIDs==nil{characterIDs=u.CharacterIDs};taxonomy:=model.Asset{};if e=s.resolveTaxonomy(ctx,&taxonomy,sourceIDs,characterIDs);e!=nil{return p,e}
  input.Family=domain.NamingFamily(taxonomy.Source,taxonomy.Characters,input.Family);if !domain.ValidName(input.Family){return p,Fail(400,"invalid_name","规范出处与角色不能生成合法目标基名。")}
  class:=domain.NumberClass(u.FileName);directory:=u.RelativeDirectory;collectionID:=input.CollectionID;isDrama:=false;appendGroup:=false;var existingWork domain.WorkName;lastExistingPage:=0
  if collectionID!=""{
   c,e:=store.Get[model.Collection](ctx,s.DB.DB,"SELECT data FROM collections WHERE id=?",collectionID);if e!=nil{return p,Normalize(e)};if c.LibraryID!=b.LibraryID{return p,Fail(409,"collection_library_mismatch","集合属于其他库。")}
   if c.Kind=="short_drama"{if class!="video"{return p,Fail(400,"drama_requires_video","短剧须为视频。")};input.Family=c.Name;directory=c.Name;isDrama=true}else{
    if class!="image"{return p,Fail(400,"mixed_group","组图只能追加图片。")};sequence,sequenceErr:=s.sequence(ctx,collectionID)
    if sequenceErr!=nil{var problem *Error;if !errors.As(sequenceErr,&problem)||problem.Code!="empty_group"||input.GroupKey==""{return p,sequenceErr}}else{existingWork=domain.WorkName{Family:sequence.Family,Separator:sequence.Separator,Number:sequence.Number,Width:sequence.Width};directory=sequence.Directory;input.Family=sequence.Family;lastExistingPage=sequence.MaxPage;appendGroup=true;input.GroupKey="append:"+collectionID}
   }
  }
  names,ok:=inventories[directory];if !ok{names,e=s.directoryNames(ctx,b.LibraryID,directory);if e!=nil{return p,e}}
  groupKey:=input.GroupKey;if groupKey==""{groupKey="single:"+u.ID};g,exists:=groups[groupKey]
  if !exists{n,width,sep:=domain.Allocate(input.Family,class,names);g=&group{family:input.Family,class:class,dir:directory,collection:collectionID,number:n,width:width,sep:sep}
   if isDrama{next,e:=s.nextEpisode(ctx,collectionID);if e!=nil{return p,e};g.number=next+dramaCounts[collectionID];dramaCounts[collectionID]++}
   if appendGroup{g.number=existingWork.Number;g.width=existingWork.Width;g.sep=existingWork.Separator;g.lastPage=lastExistingPage}else if input.GroupKey!=""{if class!="image"||input.Page!=1{return p,Fail(400,"group_order_required","新组图须按已确认顺序从第 1 页提交；不猜测页序。")};if collectionID==""{g.collection=digest([]byte(p.ID+":"+groupKey))[:32];collections=append(collections,model.Collection{ID:g.collection,LibraryID:b.LibraryID,Name:input.Family,Kind:"image_set",Aliases:[]string{},Revision:1,CreatedAt:time.Now().UTC()})}};groups[groupKey]=g
  }else if g.family!=input.Family||g.class!=class||g.dir!=directory||(collectionID!=""&&g.collection!=collectionID){return p,Fail(400,"mixed_group","同组不能混合目标集合、命名族、目录或图片/视频类别。")}
  pageNumber:=0;if input.GroupKey!=""{if appendGroup&&input.Page==0{input.Page=g.lastPage+1};if input.Page!=g.lastPage+1{return p,Fail(400,"group_order_required","组图页码必须按确认顺序连续提交。")};pageNumber=input.Page;g.lastPage=pageNumber}
  filename:=domain.WorkFilename(input.Family,g.sep,g.number,g.width,pageNumber,path.Ext(u.FileName));if collectionID!=""&&class=="video"&&u.Note!=""{ext:=path.Ext(filename);filename=strings.TrimSuffix(filename,ext)+" "+safeNote(u.Note)+ext}
  if !domain.ValidName(filename){return p,Fail(400,"invalid_name","最终名称无效，未发布文件。")};relative:=path.Join(directory,filename);if e=s.checkPath(ctx,b.LibraryID,relative,"");e!=nil{return p,e};for _,existing:=range names{if existing==filename{return p,Fail(409,"path_occupied","计划内部目标名称重复。")}}
  if isDrama{pageNumber=g.number};p.Items=append(p.Items,model.PlanItem{UploadID:u.ID,FileName:filename,RelativePath:relative,AuthorID:authorID,AuthorIDs:authorIDs,SourceIDs:taxonomy.SourceIDs,CharacterIDs:taxonomy.CharacterIDs,CollectionID:g.collection,Page:pageNumber,ExpectedHash:u.Hash});inventories[directory]=append(names,filename)
 }
 raw,e:=json.Marshal(p);if e!=nil{return p,e};p.Fingerprint=digest(raw);q,e:=w.Q();if e!=nil{return p,e}
 for _,c:=range collections{raw,e:=store.Encode(c);if e!=nil{return p,e};if _,e=q.ExecContext(ctx,"INSERT INTO collections(id,library_id,name,kind,data) VALUES(?,?,?,?,?)",c.ID,c.LibraryID,c.Name,c.Kind,raw);e!=nil{return p,e}}
 if e=store.SavePlan(ctx,q,p);e!=nil{return p,e};return p,nil
}

// CommitPlan gives each input a separate atomic receipt and publishes one file at
// a time. Failures remain visible without rolling back unrelated committed items.
func(s *Service)CommitPlan(ctx context.Context,principal model.Principal,batchID,planID,fingerprint,key string)(Result,error){
 if e:=Authorize(principal,"organize",batchID);e!=nil{return Result{},e};if e:=Authorize(principal,"ingest",batchID);e!=nil{return Result{},e}
 var oldHash string;var oldBody []byte;var oldStatus int
 receiptErr:=s.DB.DB.QueryRowContext(ctx,"SELECT input_hash,response,status FROM idempotency WHERE principal_id=? AND operation=? AND key=?",principal.ID,"commitPlan:"+planID,key).Scan(&oldHash,&oldBody,&oldStatus)
 if receiptErr==nil{if oldHash!=digest([]byte(fingerprint)){return Result{},Fail(409,"idempotency_conflict","同一计划提交键的输入已改变。")};return Result{Body:oldBody,Status:oldStatus},nil};if !errors.Is(receiptErr,sql.ErrNoRows){return Result{},receiptErr}
 p,e:=s.Plan(ctx,principal,batchID,planID);if e!=nil{return Result{},e};if p.Fingerprint!=fingerprint{return Result{},Fail(409,"plan_fingerprint_conflict","计划摘要不匹配，必须使用服务端原计划。")}
 for _,item:=range p.Items{
  _,e=s.Run(ctx,principal,"commitPlanItem:"+planID+":"+item.UploadID,fingerprint,[]byte(fingerprint),func(w *Write)(any,int,error){
   b,e:=s.Batch(ctx,batchID);if e!=nil{return nil,0,e};if b.State=="cancelled"{return nil,0,Fail(409,"batch_cancelled","批次已取消，已提交项目保留。")}
   ignore:=map[string]bool{};applied:=int64(0)
   for _,i:=range p.Items{ignore[digest([]byte("asset:"+i.UploadID))[:32]]=true;existing,e:=s.Upload(ctx,principal,i.UploadID);if e!=nil{return nil,0,e};if existing.State=="ingested"{a,e:=s.Asset(ctx,existing.AssetID);if e!=nil{return nil,0,e};actualAuthors,_:=json.Marshal(a.AuthorIDs);expectedAuthors,_:=json.Marshal(i.AuthorIDs);if a.RelativePath!=i.RelativePath||a.Hash!=i.ExpectedHash||a.CollectionID!=i.CollectionID||a.Trashed||string(actualAuthors)!=string(expectedAuthors){return nil,0,Fail(409,"plan_result_changed","已有结果与本计划不一致，不能重复提交覆盖。")};applied++}else if existing.State=="duplicate"{applied++}}
   if b.Revision!=p.BatchRevision+applied{return nil,0,Conflict(b.Revision)}
   current,e:=s.namespaceFingerprint(ctx,b.LibraryID,ignore);if e!=nil{return nil,0,e};if current!=p.DirectoryFingerprint{return nil,0,Fail(409,"namespace_changed","目标目录或对象版本已变化，请重新准备剩余项目。")}
   u,e:=s.Upload(ctx,principal,item.UploadID);if e!=nil{return nil,0,e};if u.State=="ingested"{a,e:=s.Asset(ctx,u.AssetID);return model.UploadResult{Upload:u,Asset:&a},200,e};if u.State=="duplicate"{return model.UploadResult{Upload:u},200,nil};if u.State!="staged"{return nil,0,Fail(409,"plan_input_conflict","输入状态已改变。")}
   r,e:=s.ingest(ctx,w,u,&item);return r,200,e
  })
  if e!=nil{
   var application *Error
   if errors.As(e,&application){switch application.Code{case "namespace_changed","revision_conflict","batch_cancelled","scope_denied","batch_scope_denied","expired","revoked":return Result{},e}}
   code,message:="file_operation_failed","此项未完成，原件保留；请回读后重试。"
   if application!=nil{code=application.Code;message=application.Message}
   _,recordErr:=s.Run(ctx,principal,"planItemFailure:"+planID+":"+item.UploadID,"",nil,func(w *Write)(any,int,error){
    u,e:=s.Upload(ctx,w.Principal,item.UploadID);if e!=nil{return nil,0,e};u.ErrorCode=code;u.ErrorMessage=message
    b,e:=s.Batch(ctx,batchID);if e!=nil{return nil,0,e};q,e:=w.Q();if e!=nil{return nil,0,e};if e=store.SaveUpload(ctx,q,u);e!=nil{return nil,0,e}
    // A diagnostic does not change the planned inputs or namespace revision.
    // Keeping that revision allows unrelated inputs in this plan to continue.
    if b.State!="cancelled"&&b.State!="completed"{b.State="partial";b.UpdatedAt=time.Now().UTC();if e=store.SaveBatch(ctx,q,b);e!=nil{return nil,0,e}};return nil,204,nil
   })
   if recordErr!=nil{return Result{},recordErr}
  }
 }
 return s.Run(ctx,principal,"commitPlan:"+planID,key,[]byte(fingerprint),func(w *Write)(any,int,error){
  detail,e:=s.BatchDetail(ctx,principal,batchID);if e!=nil{return nil,0,e};p.State="completed";for _,i:=range detail.Items{if i.Upload.State!="ingested"{p.State="partial";break}};q,e:=w.Q();if e!=nil{return nil,0,e};if e=store.SavePlan(ctx,q,p);e!=nil{return nil,0,e};for i:=range detail.Plans{if detail.Plans[i].ID==p.ID{detail.Plans[i]=p.PlanSummary}};return detail,200,nil
 })
}
