package service

import (
	"context"
	"database/sql"
	"time"

	"qimeng.local/next/server/internal/model"
)

type MaintenanceJob struct{
	ID string `json:"id"`;Kind string `json:"kind"`;State string `json:"state"`;RawState string `json:"rawState"`
	Label string `json:"label"`;CreatedAt *time.Time `json:"createdAt,omitempty"`;UpdatedAt *time.Time `json:"updatedAt,omitempty"`
	Cancellable bool `json:"cancellable"`;BatchID string `json:"batchId,omitempty"`;CompletedSteps *int64 `json:"completedSteps,omitempty"`;TotalSteps *int64 `json:"totalSteps,omitempty"`
}

// This view reads the existing durable workflows. It contains no second job
// queue, and cancellation goes through each workflow's original rules.
const maintenanceJobsQuery=`WITH all_jobs(kind,id,raw,label,created,updated,parent,steps,total) AS (
 SELECT 'scan',s.id,s.state,coalesce(l.name,'媒体库扫描'),json_extract(s.data,'$.createdAt'),json_extract(s.data,'$.updatedAt'),'',json_extract(s.data,'$.processed'),NULL FROM scans s LEFT JOIN libraries l ON l.id=s.library_id
 UNION ALL SELECT 'batch',id,json_extract(data,'$.state'),coalesce(nullif(json_extract(data,'$.label'),''),'媒体接收'),created_at,json_extract(data,'$.updatedAt'),'',NULL,CASE WHEN json_extract(data,'$.manifestState')='sealed' THEN json_extract(data,'$.expectedInputs') END FROM batches
 UNION ALL SELECT 'backup',id,coalesce(json_extract(data,'$.exportState'),'not_started'),'业务备份',json_extract(data,'$.createdAt'),json_extract(data,'$.exportedAt'),'',json_extract(data,'$.exportedFiles'),CASE WHEN json_extract(data,'$.exportState') IN ('running','completed') THEN json_extract(data,'$.totalFiles') END FROM backups
 UNION ALL SELECT 'protection',id,state,'批次独立备份',json_extract(data,'$.createdAt'),json_extract(data,'$.verifiedAt'),batch_id,NULL,json_array_length(json_extract(data,'$.items')) FROM batch_protections
 UNION ALL SELECT 'author',id,CASE WHEN state='authorized' AND julianday(json_extract(data,'$.expiresAt'))<=julianday('now') THEN 'expired' ELSE state END,'作者资料维护',json_extract(data,'$.createdAt'),NULL,'',json_extract(data,'$.usedUpdates'),json_extract(data,'$.maxUpdates') FROM author_tasks
 UNION ALL SELECT 'asset',id,CASE WHEN json_extract(data,'$.state')='revoked' THEN 'revoked' WHEN json_extract(data,'$.usedUpdates')>=json_extract(data,'$.maxUpdates') THEN 'completed' WHEN julianday(json_extract(data,'$.expiresAt'))<=julianday('now') THEN 'expired' ELSE 'authorized' END,'已有作品维护',json_extract(data,'$.createdAt'),NULL,'',json_extract(data,'$.usedUpdates'),json_extract(data,'$.maxUpdates') FROM asset_maintenance_tasks
), jobs AS (
 SELECT *,coalesce(qimeng_time_key(created),'') stamp,kind||':'||id identity,
 CASE WHEN raw IN ('completed','done','covered') THEN 'completed'
 WHEN raw IN ('partial','partially_covered') THEN 'partial'
 WHEN raw IN ('failed','interrupted','unavailable','snapshot_not_included') THEN 'failed'
 WHEN raw IN ('cancelled','revoked','expired','historical') THEN 'cancelled'
 WHEN raw IN ('running','processing','receiving','committing') THEN 'running' ELSE 'waiting' END status FROM all_jobs
) SELECT kind,id,raw,label,stamp,qimeng_time_key(updated),parent,steps,total,status,identity FROM jobs
 WHERE (?='' OR kind=?) AND (?='' OR status=?) AND (?='' OR stamp<? OR(stamp=? AND identity>?))
 ORDER BY stamp DESC,identity ASC LIMIT ?`

func(s *Service) MaintenanceJobs(ctx context.Context,p model.Principal,kind,state,cursor string,count int)(model.Page[MaintenanceJob],error){
	page:=model.Page[MaintenanceJob]{Items:[]MaintenanceJob{}}
	if err:=Authorize(p,"admin","");err!=nil{return page,err}
	if kind!=""&&!containsString([]string{"scan","batch","backup","protection","author","asset"},kind)||state!=""&&!containsString([]string{"waiting","running","completed","partial","failed","cancelled"},state){return page,Fail(400,"invalid_job_filter","未知的维护任务类别或状态。")}
	n,err:=limited(count,50);if err!=nil{return page,err}
	filter:="maintenance-jobs/"+kind+"/"+state
	key,err:=readSeek(cursor,filter,"created");if err!=nil{return page,err}
	rows,err:=s.DB.DB.QueryContext(ctx,maintenanceJobsQuery,kind,kind,state,state,key.ID,key.Value,key.Value,key.ID,n+1);if err!=nil{return page,err};defer rows.Close()
	lastTime,lastID:="",""
	for rows.Next(){
		if len(page.Items)==n{page.NextCursor=makeSeek(filter,"created",lastTime,lastID);break}
		var job MaintenanceJob;var created,identity string;var updated sql.NullString
		if err=rows.Scan(&job.Kind,&job.ID,&job.RawState,&job.Label,&created,&updated,&job.BatchID,&job.CompletedSteps,&job.TotalSteps,&job.State,&identity);err!=nil{return page,err}
		if created!=""{t,e:=time.Parse(time.RFC3339Nano,created);if e!=nil{return page,e};job.CreatedAt=&t}
		if updated.Valid{t,e:=time.Parse(time.RFC3339Nano,updated.String);if e!=nil{return page,e};job.UpdatedAt=&t}
		job.Cancellable=(job.Kind=="scan"&&(job.RawState=="queued"||job.RawState=="running"))||
			(job.Kind=="batch"&&(job.State=="waiting"||job.State=="running"||job.State=="partial"))||job.Kind=="author"&&job.RawState=="authorized"
		lastTime,lastID=created,identity;page.Items=append(page.Items,job)
	}
	return page,rows.Err()
}
