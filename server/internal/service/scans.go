package service

import (
 "context"
 "database/sql"
 "errors"
 "io"
 "log/slog"
 "os"
 "path"
 "strings"
 "time"

 "qimeng.local/next/server/internal/domain"
 "qimeng.local/next/server/internal/model"
 "qimeng.local/next/server/internal/store"
)

type ScanIssue struct{RelativePath string `json:"relativePath"`;Code string `json:"code"`}
type ScanJob struct{ID string `json:"id"`;LibraryID string `json:"libraryId"`;State string `json:"state"`;Processed int `json:"processed"`;Added int `json:"added"`;Moved int `json:"moved"`;Changed int `json:"changed"`;Skipped int `json:"skipped"`;Missing int `json:"missing"`;IssueCount int `json:"issueCount"`;Issues []ScanIssue `json:"issues"`;CreatedAt time.Time `json:"createdAt"`;UpdatedAt time.Time `json:"updatedAt"`}
func(s *Service)Scan(ctx context.Context,id string)(ScanJob,error){j,e:=store.Get[ScanJob](ctx,s.DB.DB,"SELECT data FROM scans WHERE id=?",id);return j,Normalize(e)}
func saveScan(ctx context.Context,q store.Querier,j ScanJob)error{raw,e:=store.Encode(struct{ScanJob;CreatedAt string `json:"createdAt"`;UpdatedAt string `json:"updatedAt"`}{j,dateKey(j.CreatedAt),dateKey(j.UpdatedAt)});if e!=nil{return e};_,e=q.ExecContext(ctx,"INSERT INTO scans(id,library_id,state,data) VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET state=excluded.state,data=excluded.data",j.ID,j.LibraryID,j.State,raw);return e}
func(s *Service)Scans(ctx context.Context,libraryID,state,cursor string,limit int)(model.Page[ScanJob],error){n,e:=limited(limit,50);if e!=nil{return model.Page[ScanJob]{},e};filter:=libraryID+"\x00"+state;seek,e:=readSeek(cursor,filter,"created_desc");if e!=nil{return model.Page[ScanJob]{},e};where:="(?='' OR library_id=?) AND (?='' OR state=?)";args:=[]any{libraryID,libraryID,state,state};if seek.ID!=""{where+=" AND (json_extract(data,'$.createdAt')<? OR (json_extract(data,'$.createdAt')=? AND id<?))";args=append(args,seek.Value,seek.Value,seek.ID)};args=append(args,n+1);items,e:=store.List[ScanJob](ctx,s.DB.DB,"SELECT data FROM scans WHERE "+where+" ORDER BY json_extract(data,'$.createdAt') DESC,id DESC LIMIT ?",args...);if e!=nil{return model.Page[ScanJob]{},e};page:=model.Page[ScanJob]{Items:items};if len(items)>n{page.Items=items[:n];last:=page.Items[n-1];page.NextCursor=makeSeek(filter,"created_desc",dateKey(last.CreatedAt),last.ID)};return page,nil}
func(s *Service)StartScan(ctx context.Context,w *Write,libraryID string,revision int64)(ScanJob,error){
 if e:=Authorize(w.Principal,"admin","");e!=nil{return ScanJob{},e};l,e:=s.library(ctx,libraryID);if e!=nil{return ScanJob{},e};if l.Revision!=revision{return ScanJob{},Conflict(l.Revision)};if !l.Enabled{return ScanJob{},Fail(409,"library_disabled","库已停用。")};var active int;if e=s.DB.DB.QueryRowContext(ctx,"SELECT count(*) FROM scans WHERE state IN ('queued','running')").Scan(&active);e!=nil{return ScanJob{},e};if active>0{return ScanJob{},Fail(429,"scan_capacity","已有扫描正在执行，请等待其结果。")}
 now:=time.Now().UTC();job:=ScanJob{ID:w.ID("scan"),LibraryID:libraryID,State:"queued",Issues:[]ScanIssue{},CreatedAt:now,UpdatedAt:now};q,e:=w.Q();if e!=nil{return job,e};if e=saveScan(ctx,q,job);e!=nil{return job,e};w.after=append(w.after,func()error{s.workers.Add(1);go func(){defer s.workers.Done();s.scanLibrary(s.workCtx,job.ID)}();return nil});return job,nil
}
func(s *Service)CancelScan(ctx context.Context,w *Write,id string)(ScanJob,error){if e:=Authorize(w.Principal,"admin","");e!=nil{return ScanJob{},e};j,e:=s.Scan(ctx,id);if e!=nil{return j,e};if j.State=="completed"||j.State=="partial"||j.State=="failed"||j.State=="interrupted"{return j,nil};j.State="cancelled";j.UpdatedAt=time.Now().UTC();q,e:=w.Q();if e!=nil{return j,e};return j,saveScan(ctx,q,j)}
func scanIssue(j *ScanJob,relative,code string){j.IssueCount++;if len(j.Issues)<100{j.Issues=append(j.Issues,ScanIssue{RelativePath:relative,Code:code})}}
func(s *Service)scanProgress(ctx context.Context,id,relative,code string,skipped bool)error{
 s.mu.Lock();defer s.mu.Unlock();j,e:=s.Scan(ctx,id);if e!=nil{return e};if j.State=="cancelled"{return context.Canceled};j.Processed++;if skipped{j.Skipped++};if code=="missing_original"{j.Missing++};if code!=""{scanIssue(&j,relative,code)};j.UpdatedAt=time.Now().UTC();return saveScan(ctx,s.DB.DB,j)
}
func(s *Service)scanLibrary(ctx context.Context,id string){
 s.mu.Lock();j,e:=s.Scan(ctx,id);if e==nil&&j.State!="cancelled"{j.State="running";j.UpdatedAt=time.Now().UTC();e=saveScan(ctx,s.DB.DB,j)};s.mu.Unlock();if e!=nil||j.State=="cancelled"{return}
 e=s.walkLibrary(ctx,id,j.LibraryID,"",0)
 if e==nil{e=s.scanMissing(ctx,id,j.LibraryID)}
 finishCtx,cancel:=context.WithTimeout(context.Background(),5*time.Second);defer cancel();s.mu.Lock();defer s.mu.Unlock();j,readErr:=s.Scan(finishCtx,id);if readErr!=nil||j.State=="cancelled"{return}
 if errors.Is(e,context.Canceled){j.State="interrupted"}else if e!=nil{j.State="failed";scanIssue(&j,"","scan_io_failed")}else if j.IssueCount>0{j.State="partial"}else{j.State="completed"};j.UpdatedAt=time.Now().UTC();if e=saveScan(finishCtx,s.DB.DB,j);e!=nil{slog.Error("scan terminal persistence failed","scanId",id)}
}
func(s *Service)scanMissing(ctx context.Context,id,libraryID string)error{last:="";for{items,e:=store.List[model.Asset](ctx,s.DB.DB,"SELECT data FROM assets WHERE library_id=? AND trashed=0 AND id>? ORDER BY id LIMIT 200",libraryID,last);if e!=nil{return e};if len(items)==0{return nil};for _,a:=range items{if e:=ctx.Err();e!=nil{return e};if _,e=s.Root.Stat(assetPath(a));errors.Is(e,os.ErrNotExist){if e=s.scanProgress(ctx,id,a.RelativePath,"missing_original",false);e!=nil{return e}}else if e!=nil{return e};last=a.ID}}}
func(s *Service)walkLibrary(ctx context.Context,jobID,libraryID,relative string,depth int)error{
 if e:=ctx.Err();e!=nil{return e};if depth>32{return s.scanProgress(ctx,jobID,relative,"directory_depth_limit",true)};full:=path.Join("libraries",libraryID,relative);if e:=s.safe(full);e!=nil{return e};directory,e:=s.Root.Open(full);if e!=nil{return e};defer directory.Close()
 for{entries,readErr:=directory.ReadDir(128);for _,entry:=range entries{if e:=ctx.Err();e!=nil{return e};if entry.Name()==".qimeng-internal"{continue};child:=path.Join(relative,entry.Name());if entry.Type()&os.ModeSymlink!=0{if e=s.scanProgress(ctx,jobID,child,"symlink_denied",true);e!=nil{return e};continue};if !domain.ValidMediaPath(child){if e=s.scanProgress(ctx,jobID,child,"invalid_path",true);e!=nil{return e};continue};if entry.IsDir(){if e=s.walkLibrary(ctx,jobID,libraryID,child,depth+1);e!=nil{return e};continue};if e=s.scanFile(ctx,jobID,libraryID,child);e!=nil{return e}};if readErr!=nil{if errors.Is(readErr,io.EOF){return nil};return readErr}}
}
func(s *Service)scanFile(ctx context.Context,jobID,libraryID,relative string)error{
 kind,mime,ok:=domain.MediaType(relative);if !ok{return s.scanProgress(ctx,jobID,relative,"",true)};full:=path.Join("libraries",libraryID,relative);if e:=s.safe(full);e!=nil{return s.scanProgress(ctx,jobID,relative,"symlink_denied",true)};initial,e:=s.Root.Stat(full);if e!=nil{return s.scanProgress(ctx,jobID,relative,"stat_failed",true)};if !initial.Mode().IsRegular(){return s.scanProgress(ctx,jobID,relative,"not_regular_file",true)}
 previous,e:=store.Get[model.Asset](ctx,s.DB.DB,"SELECT data FROM assets WHERE library_id=? AND relative_path=? AND trashed=0",libraryID,relative);if e!=nil&&!errors.Is(e,sql.ErrNoRows){return e};if e==nil&&previous.Size==initial.Size()&&previous.ContentMtime!=nil&&previous.ContentMtime.Equal(initial.ModTime()){return s.scanProgress(ctx,jobID,relative,"",true)}
 hash,size,e:=s.hashFile(full);if e!=nil{return s.scanProgress(ctx,jobID,relative,"read_failed",true)};probe:=model.Asset{ID:"scan",MimeType:mime};if e=s.probe(ctx,full,&probe);e!=nil{return s.scanProgress(ctx,jobID,relative,"invalid_media",true)}
 s.mu.Lock();defer s.mu.Unlock();j,e:=s.Scan(ctx,jobID);if e!=nil{return e};if j.State=="cancelled"{return context.Canceled};library,e:=s.library(ctx,libraryID);if e!=nil{return e};if !library.Enabled{return Fail(409,"library_disabled","库已停用。")}
 nowInfo,e:=s.Root.Stat(full);if e!=nil{return e};if !os.SameFile(initial,nowInfo)||initial.Size()!=size||nowInfo.Size()!=size||!initial.ModTime().Equal(nowInfo.ModTime()){j.Processed++;j.Skipped++;scanIssue(&j,relative,"source_changed");return saveScan(ctx,s.DB.DB,j)}
 var pending int;if e=s.DB.DB.QueryRowContext(ctx,"SELECT count(*) FROM file_intents WHERE state!='done' AND json_extract(data,'$.Target')=?",full).Scan(&pending);e!=nil{return e};if pending>0{j.Processed++;j.Skipped++;scanIssue(&j,relative,"publication_pending");return saveScan(ctx,s.DB.DB,j)}
 a,e:=store.Get[model.Asset](ctx,s.DB.DB,"SELECT data FROM assets WHERE library_id=? AND relative_path=? AND trashed=0",libraryID,relative);isNew:=errors.Is(e,sql.ErrNoRows);if e!=nil&&!isNew{return e};moved:=false
 if isNew{
  candidates,e:=store.List[model.Asset](ctx,s.DB.DB,"SELECT data FROM assets WHERE library_id=? AND hash=? AND size=? AND trashed=0 ORDER BY id LIMIT 101",libraryID,hash,size);if e!=nil{return e};missing:=[]model.Asset{}
  if len(candidates)<=100{for _,candidate:=range candidates{if _,e=s.Root.Stat(assetPath(candidate));errors.Is(e,os.ErrNotExist){missing=append(missing,candidate)}else if e!=nil{return e}}}
  if len(missing)==1{a=missing[0];isNew=false;moved=true}else if len(missing)>1||len(candidates)>100{j.Processed++;j.Skipped++;scanIssue(&j,relative,"ambiguous_move");return saveScan(ctx,s.DB.DB,j)}
 }
 now:=time.Now().UTC();if isNew{a=model.Asset{ID:ID(),LibraryID:libraryID,OriginalFileName:path.Base(relative),AuthorID:"unknown",AuthorIDs:[]string{"unknown"},AuthorsRevision:1,AuthorStatus:"unknown",TagsRevision:1,Revision:1,ContentVersion:1,Tags:[]string{},SourceIDs:[]string{},CharacterIDs:[]string{},Characters:[]string{},AcquisitionSources:[]string{},MetadataSource:"scanner",CreatedAt:now}}
 if !isNew{a.Revision++;if a.Hash!=hash{a.ContentVersion++}};a.RelativePath=relative;a.FileName=path.Base(relative);a.Size=size;a.Hash=hash;a.MediaType=kind;a.MimeType=mime;a.UpdatedAt=now;modified:=nowInfo.ModTime().UTC();a.ContentMtime=&modified;a.Width=probe.Width;a.Height=probe.Height;a.DurationMs=probe.DurationMs;a.ProbeState=probe.ProbeState;a.ContentURL="/api/v1/assets/"+a.ID+"/content";a.ThumbnailURL="";if probe.ThumbnailURL!=""{a.ThumbnailURL="/api/v1/assets/"+a.ID+"/thumbnail"}
 tx,e:=s.DB.DB.BeginTx(ctx,nil);if e!=nil{return e};defer tx.Rollback()
 if library.Kind=="cos"&&a.MetadataSource!="human"{if e=s.scanCOSAuthor(ctx,tx,&a);e!=nil{return e}}
 if e=store.SaveAsset(ctx,tx,a);e!=nil{return e};j.Processed++;if isNew{j.Added++}else if moved{j.Moved++}else{j.Changed++};j.UpdatedAt=now;if e=saveScan(ctx,tx,j);e!=nil{return e};return tx.Commit()
}
func(s *Service)scanCOSAuthor(ctx context.Context,q store.Querier,a *model.Asset)error{
 parts:=strings.Split(a.RelativePath,"/");if len(parts)<2{return nil};name:=parts[0];authors,e:=store.List[model.Author](ctx,q,"SELECT data FROM authors WHERE name=? AND json_extract(data,'$.kind')='cos' LIMIT 2",name);if e!=nil{return e};if len(authors)>1{return nil}
 if len(authors)==0{now:=time.Now().UTC();author:=model.Author{ID:digest([]byte("cos-directory:"+a.LibraryID+":"+name))[:32],Name:name,Kind:"cos",Aliases:[]string{},Revision:1,FieldStatus:map[string]string{"identity":"candidate","biography":"missing","homepage":"missing"},CreatedAt:now,UpdatedAt:now};raw,e:=store.Encode(author);if e!=nil{return e};if _,e=q.ExecContext(ctx,"INSERT INTO authors(id,name,data) VALUES(?,?,?)",author.ID,author.Name,raw);e!=nil{return e};authors=append(authors,author)}
 if len(a.AuthorIDs)!=1||a.AuthorIDs[0]!=authors[0].ID{setAuthors(a,authors);a.AuthorsRevision++};return nil
}
