package service

import (
 "bytes"
 "context"
 "database/sql"
 "encoding/json"
 "errors"
 "io"
 "log/slog"
 "os"
 "time"

 archive "qimeng.local/next/server/internal/backup"
 "qimeng.local/next/server/internal/model"
 "qimeng.local/next/server/internal/store"
)

type ProtectedInput struct{InputID string `json:"inputId"`;Ordinal int `json:"ordinal"`;State string `json:"state"`;Reason string `json:"reason"`;UploadID string `json:"uploadId,omitempty"`;AssetID string `json:"assetId,omitempty"`;ContentVersion int64 `json:"contentVersion,omitempty"`;Hash string `json:"hash,omitempty"`;Size int64 `json:"size,omitempty"`}
type BatchProtection struct{ID string `json:"id"`;InstanceID string `json:"instanceId"`;BatchID string `json:"batchId"`;ManifestFingerprint string `json:"manifestFingerprint"`;CheckpointID string `json:"checkpointId"`;SnapshotHash string `json:"snapshotHash"`;SnapshotBytes int64 `json:"snapshotBytes"`;TargetID string `json:"targetId"`;FailureDomain string `json:"failureDomain"`;State string `json:"state"`;ErrorCode string `json:"errorCode,omitempty"`;ProofHash string `json:"proofHash,omitempty"`;CreatedAt time.Time `json:"createdAt"`;VerifiedAt *time.Time `json:"verifiedAt,omitempty"`;Items []ProtectedInput `json:"items"`}
func(s *Service)BatchProtection(ctx context.Context,p model.Principal,batchID,id string)(BatchProtection,error){if e:=Authorize(p,"read",batchID);e!=nil{return BatchProtection{},e};value,e:=store.Get[BatchProtection](ctx,s.DB.DB,"SELECT data FROM batch_protections WHERE id=? AND batch_id=?",id,batchID);return value,Normalize(e)}
func(s *Service)BatchProtections(ctx context.Context,p model.Principal,batchID,cursor string,limit int)(model.Page[BatchProtection],error){
 result:=model.Page[BatchProtection]{Items:[]BatchProtection{}};if e:=Authorize(p,"read",batchID);e!=nil{return result,e};n,e:=limited(limit,20);if e!=nil{return result,e};seek,e:=readSeek(cursor,"protections:"+batchID,"id");if e!=nil{return result,e};items,e:=store.List[BatchProtection](ctx,s.DB.DB,"SELECT data FROM batch_protections WHERE batch_id=? AND id>? ORDER BY id LIMIT ?",batchID,seek.ID,n+1);if e!=nil{return result,e};if len(items)>n{result.NextCursor=makeSeek("protections:"+batchID,"id","",items[n-1].ID);items=items[:n]};result.Items=items;return result,nil
}
func saveProtection(ctx context.Context,q store.Querier,p BatchProtection)error{raw,e:=store.Encode(p);if e!=nil{return e};_,e=q.ExecContext(ctx,"INSERT INTO batch_protections(id,batch_id,state,created_at,data) VALUES(?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET state=excluded.state,data=excluded.data",p.ID,p.BatchID,p.State,dateKey(p.CreatedAt),raw);return e}
func(s *Service)protectionCapacity(ctx context.Context,batchID string)error{var active,count int;if e:=s.DB.DB.QueryRowContext(ctx,"SELECT count(*) FROM batch_protections WHERE state IN ('queued','running')").Scan(&active);e!=nil{return e};if active>0{return Fail(409,"protection_busy","已有独立保护正在执行，请先回读进度。")};if e:=s.DB.DB.QueryRowContext(ctx,"SELECT count(*) FROM batch_protections WHERE batch_id=?",batchID).Scan(&count);e!=nil{return e};if count>=100{return Fail(409,"protection_limit","本批保护记录已达上限，请恢复已有记录。")};return nil}
func(s *Service)StartBatchProtection(ctx context.Context,w *Write,batchID string)(BatchProtection,error){
 value:=BatchProtection{ID:w.ID("protection"),InstanceID:s.InstanceID,BatchID:batchID,State:"queued",CreatedAt:time.Now().UTC(),Items:[]ProtectedInput{}};if e:=Authorize(w.Principal,"ingest",batchID);e!=nil{return value,e};batch,e:=s.Batch(ctx,batchID);if e!=nil{return value,e};if batch.ManifestState!="sealed"||batch.State=="cancelled"{return value,Fail(409,"manifest_required","必须以有效封存的原始清单逐项保护，取消或旧未核实批次不能申请。")};inputs,e:=s.batchInputs(ctx,s.DB.DB,batchID);if e!=nil{return value,e};if len(inputs)!=batch.ExpectedInputs{return value,Fail(409,"manifest_corrupt","原始清单数量不一致。")};value.ManifestFingerprint=batch.ManifestFingerprint
 target,info,e:=s.backupTarget(ctx,s.Config.DefaultBackupTarget);if e!=nil{return value,e};if !info.DistinctVolume{return value,Fail(409,"backup_target_same_volume","备份目标与业务数据或媒体位于同一卷，不能作为独立保护。")};value.TargetID=info.ID;value.FailureDomain=target.FailureDomain;if e=s.protectionCapacity(ctx,batchID);e!=nil{return value,e};ready:=0
 for _,input:=range inputs{item:=ProtectedInput{InputID:input.ID,Ordinal:input.Ordinal,State:"retained",Reason:input.State,UploadID:input.UploadID};if input.State=="ingested"{
  upload,e:=store.Get[model.Upload](ctx,s.DB.DB,"SELECT data FROM uploads WHERE id=?",input.UploadID);if e!=nil{return value,e};asset,e:=store.Get[model.Asset](ctx,s.DB.DB,"SELECT data FROM assets WHERE id=?",upload.AssetID);if errors.Is(e,sql.ErrNoRows){item.Reason="input_version_unavailable";value.Items=append(value.Items,item);continue};if e!=nil{return value,e};item.AssetID=asset.ID;item.ContentVersion=asset.ContentVersion;item.Hash=upload.Hash;item.Size=upload.Size
  if asset.Trashed||asset.Hash!=upload.Hash||asset.Size!=upload.Size{item.Reason="input_version_unavailable"}else{item.State="ready";item.Reason="";ready++}}
  value.Items=append(value.Items,item)
 };if ready==0{return value,Fail(409,"no_protectable_inputs","本批暂无可核验的成功输入，所有来源应保留。")}
 checkpoint,e:=s.createBackupCheckpoint(ctx,w);if e!=nil{return value,e};value.CheckpointID=checkpoint.ID;value.SnapshotHash=checkpoint.SnapshotHash;infoFile,e:=s.Root.Stat("backups/"+checkpoint.ID+"/snapshot.sqlite");if e!=nil{return value,e};value.SnapshotBytes=infoFile.Size();q,e:=w.Q();if e!=nil{return value,e};if e=saveProtection(ctx,q,value);e!=nil{return value,e};batch.BackupState="awaiting_external_backup";batch.Revision++;batch.UpdatedAt=time.Now().UTC();if e=store.SaveBatch(ctx,q,batch);e!=nil{return value,e};s.launchProtection(w,value);return value,nil
}
func(s *Service)RetryBatchProtection(ctx context.Context,w *Write,batchID,id string)(BatchProtection,error){
 if e:=Authorize(w.Principal,"ingest",batchID);e!=nil{return BatchProtection{},e};p,e:=s.BatchProtection(ctx,w.Principal,batchID,id);if e!=nil{return p,e};if p.InstanceID!=s.InstanceID||p.State=="historical"{return p,Fail(409,"protection_instance_changed","旧实例的保护报告不能在当前实例继续执行。")};if p.State=="completed"||p.State=="running"||p.State=="queued"{return p,nil};var active int;if e=s.DB.DB.QueryRowContext(ctx,"SELECT count(*) FROM batch_protections WHERE state IN ('queued','running')").Scan(&active);e!=nil{return p,e};if active>0{return p,Fail(409,"protection_busy","已有独立保护正在执行。")};p.State="queued";p.ErrorCode="";q,e:=w.Q();if e!=nil{return p,e};if e=saveProtection(ctx,q,p);e!=nil{return p,e};s.launchProtection(w,p);return p,nil
}
func(s *Service)launchProtection(w *Write,p BatchProtection){w.after=append(w.after,func()error{s.workers.Add(1);go func(){defer s.workers.Done();s.protectInputs(s.workCtx,p)}();return nil})}
func protectionEvidence(p BatchProtection)([]byte,error){
 // Stable facts only: retry timestamps/progress never rewrite the immutable
 // evidence object, so publishing it before the database receipt is recoverable.
 return json.Marshal(struct{Format string `json:"format"`;InstanceID string `json:"instanceId"`;BatchID string `json:"batchId"`;ManifestFingerprint string `json:"manifestFingerprint"`;SnapshotHash string `json:"snapshotHash"`;SnapshotBytes int64 `json:"snapshotBytes"`;TargetID string `json:"targetId"`;FailureDomain string `json:"failureDomain"`;Items []ProtectedInput `json:"items"`}{"qimeng-intake-protection-1",p.InstanceID,p.BatchID,p.ManifestFingerprint,p.SnapshotHash,p.SnapshotBytes,p.TargetID,p.FailureDomain,p.Items})
}
func protectionSnapshot(p BatchProtection)string{return "instances/"+p.InstanceID+"/snapshots/"+p.SnapshotHash+".sqlite"}
func protectionObject(hash string)string{return "objects/sha256/"+hash[:2]+"/"+hash}
func protectionReceipt(p BatchProtection)string{return "instances/"+p.InstanceID+"/receipts/"+p.ID+".json"}
func(s *Service)protectInputs(ctx context.Context,p BatchProtection){
 persist:=func()error{s.mu.Lock();defer s.mu.Unlock();return saveProtection(context.Background(),s.DB.DB,p)};fail:=func(e error){p.State="failed";p.ErrorCode="protection_failed";if errors.Is(e,context.Canceled){p.State="interrupted";p.ErrorCode="protection_interrupted"};var app *Error;if errors.As(e,&app){p.ErrorCode=app.Code};if e=persist();e!=nil{slog.Error("protection status persistence failed; restart recovery required")}};p.State="running";if e:=persist();e!=nil{fail(e);return}
 target,info,e:=s.backupTarget(ctx,p.TargetID);if e!=nil{fail(e);return};if !info.DistinctVolume||target.FailureDomain!=p.FailureDomain{fail(Fail(409,"backup_target_changed","独立备份目标配置或卷归属已变化。"));return};root,e:=os.OpenRoot(target.Path);if e!=nil{fail(e);return};defer root.Close()
 sourceSnapshot:="backups/"+p.CheckpointID+"/snapshot.sqlite";e=copyTargetObject(ctx,root,target.Path,"temporary/"+p.ID+"-snapshot.partial",protectionSnapshot(p),p.SnapshotHash,p.SnapshotBytes,s.Config.ReserveBytes,func()(io.ReadCloser,error){return s.Root.Open(sourceSnapshot)});if e!=nil{fail(e);return}
 for index,item:=range p.Items{if item.State=="retained"{continue};file:=archive.File{Kind:"asset",ObjectID:item.AssetID,ContentVersion:item.ContentVersion,SHA256:item.Hash,Size:item.Size}
  e=copyTargetObject(ctx,root,target.Path,"temporary/"+p.ID+"-"+item.InputID+".partial",protectionObject(item.Hash),item.Hash,item.Size,s.Config.ReserveBytes,func()(io.ReadCloser,error){return s.backupSource(ctx,file)});if e!=nil{fail(e);return};p.Items[index].State="protected";if e=persist();e!=nil{fail(e);return}
 }
 evidence,e:=protectionEvidence(p);if e!=nil{fail(e);return};p.ProofHash=digest(evidence);e=copyTargetObject(ctx,root,target.Path,"temporary/"+p.ID+"-receipt.partial",protectionReceipt(p),p.ProofHash,int64(len(evidence)),s.Config.ReserveBytes,func()(io.ReadCloser,error){return io.NopCloser(bytes.NewReader(evidence)),nil});if e!=nil{fail(e);return}
 _,info,e=s.backupTarget(ctx,p.TargetID);if e!=nil||!info.DistinctVolume{if e==nil{e=Fail(409,"backup_target_changed","备份目标卷归属已变化。")};fail(e);return};now:=time.Now().UTC();p.VerifiedAt=&now;p.State="completed";p.ErrorCode=""
 finish:=func()error{s.mu.Lock();defer s.mu.Unlock();tx,e:=s.DB.DB.BeginTx(context.Background(),nil);if e!=nil{return e};defer tx.Rollback();batch,e:=store.Get[model.Batch](context.Background(),tx,"SELECT data FROM batches WHERE id=?",p.BatchID);if e!=nil{return e};if batch.ManifestState!="sealed"||batch.ManifestFingerprint!=p.ManifestFingerprint{return Fail(409,"manifest_changed","保护完成时原始清单已不一致。")};batch.BackupState="covered";for _,item:=range p.Items{if item.State!="protected"{batch.BackupState="partially_covered";break}};batch.Revision++;batch.UpdatedAt=now;if e=saveProtection(context.Background(),tx,p);e!=nil{return e};if e=store.SaveBatch(context.Background(),tx,batch);e!=nil{return e};return tx.Commit()};if e=finish();e!=nil{fail(e)}
}
