package service

import (
 "context"
 "encoding/json"
 "errors"
 "os"
 "path/filepath"
 "slices"
 "strings"
 "sync"
 "time"

 "qimeng.local/next/server/internal/model"
 "qimeng.local/next/server/internal/store"
)

type LocalSyncConfig struct{Root string;Interval,StableAge time.Duration}
type LocalSyncItem struct{
 Path string `json:"path"`
 Kind string `json:"kind"`
 LibraryName string `json:"libraryName,omitempty"`
 State string `json:"state"`
 Error string `json:"error"`
 SizeBytes int64 `json:"sizeBytes"`
 Attempts int `json:"attempts"`
 LastAttemptAt *time.Time `json:"lastAttemptAt,omitempty"`
}
type LocalSyncCounts struct{WaitingStable int `json:"waitingStable"`;Failed int `json:"failed"`;Ignored int `json:"ignored"`}
type LocalSyncStatus struct{
 Enabled bool `json:"enabled"`
 Root string `json:"root"`
 IntervalSeconds int `json:"intervalSeconds"`
 Paused bool `json:"paused"`
 Running bool `json:"running"`
 LastError string `json:"lastError"`
 LastCycleAt *time.Time `json:"lastCycleAt,omitempty"`
 SyncedTotal int64 `json:"syncedTotal"`
 Counts LocalSyncCounts `json:"counts"`
 Items []LocalSyncItem `json:"items"`
 RecentSynced []LocalSyncItem `json:"recentSynced"`
}
type localSyncRunner struct{config LocalSyncConfig;rootID string;wake chan struct{};mu sync.Mutex;status LocalSyncStatus}
func initializeLocalSync(config LocalSyncConfig)(*localSyncRunner,error){
 if config.Interval==0{config.Interval=30*time.Second};if config.StableAge==0{config.StableAge=60*time.Second}
 if config.Interval<0||config.StableAge<0{return nil,errors.New("invalid local sync timing")}
 if config.Root!=""&&!filepath.IsAbs(config.Root){return nil,errors.New("local sync root must be absolute")}
 r:=&localSyncRunner{config:config,rootID:digest([]byte(filepath.Clean(config.Root))),wake:make(chan struct{},1)}
 r.status=LocalSyncStatus{Root:config.Root,IntervalSeconds:int(config.Interval.Seconds()),Items:[]LocalSyncItem{},RecentSynced:[]LocalSyncItem{}}
 return r,nil
}
func syncRootsOverlap(a,b string)bool{
 within:=func(parent,child string)bool{rel,e:=filepath.Rel(parent,child);return e==nil&&(rel=="."||rel!=".."&&!strings.HasPrefix(rel,".."+string(filepath.Separator)))}
 return within(a,b)||within(b,a)
}
func(s *Service)openLocalSyncRoot(ctx context.Context)(*os.Root,error){
 configured:=s.localSync.config.Root
 if configured==""{return nil,Fail(409,"local_sync_disabled","服务器目录自动导入未启用。")}
 if !syncPlatformSupported{return nil,Fail(501,"local_sync_platform_unsupported","当前服务平台尚未提供可靠的文件变动版本。")}
 // Windows may expand an ordinary 8.3 name while resolving the root. Inspect
 // actual entries (including junctions) instead of equating spelling with links.
 for part:=filepath.Clean(configured);;part=filepath.Dir(part){
  info,e:=os.Lstat(part);if e!=nil{return nil,Fail(409,"local_sync_unavailable","导入目录不存在或不可读取。")}
  if syncPathLink(info){return nil,Fail(409,"local_sync_root_link","导入目录及其父路径不能包含链接。")}
  if filepath.Dir(part)==part{break}
 }
 resolved,e:=filepath.EvalSymlinks(configured);if e!=nil{return nil,Fail(409,"local_sync_unavailable","导入目录不存在或不可读取。")}
 control,e:=filepath.Abs(s.Config.DataDir);if e!=nil{return nil,e}
 control,e=filepath.EvalSymlinks(control);if e!=nil{return nil,e}
 targets:=[]string{control}
 for _,target:=range s.Config.BackupTargets{backup:=target.Path;if path,err:=filepath.EvalSymlinks(backup);err==nil{backup=path};targets=append(targets,backup)}
 mirror,e:=readAuthorMirror(ctx,s.DB.DB);if e!=nil{return nil,e};if mirror.Path!=""&&syncRootsOverlap(resolved,mirror.Path){return nil,Fail(409,"local_sync_root_overlap","导入目录内存在作者镜像目的地。")}
 libs,e:=store.List[model.Library](ctx,s.DB.DB,"SELECT data FROM libraries");if e!=nil{return nil,e}
 for _,lib:=range libs{physical,e:=s.Root.LibraryPath(lib.ID);if e!=nil{return nil,e};targets=append(targets,physical)}
 for _,target:=range targets{if syncRootsOverlap(resolved,target){return nil,Fail(409,"local_sync_root_overlap","导入目录与媒体库、备份或业务目录重叠。")}}
 root,e:=os.OpenRoot(resolved);if e!=nil{return nil,Fail(409,"local_sync_unavailable","导入目录不可读取。")};return root,nil
}
func(s *Service)LocalSyncStatus(ctx context.Context,p model.Principal)(LocalSyncStatus,error){
 if e:=Authorize(p,"admin","");e!=nil{return LocalSyncStatus{},e}
 r:=s.localSync;r.mu.Lock();v:=r.status;r.mu.Unlock()
 if v.Root!=""{settings,e:=s.Settings(ctx);if e!=nil{return v,e};v.Paused=!settings.Upload.AutoAccept}
 return v,nil
}
func(s *Service)TriggerLocalSync(ctx context.Context,w *Write)(LocalSyncStatus,error){
 if e:=Authorize(w.Principal,"admin","");e!=nil{return LocalSyncStatus{},e}
 root,e:=s.openLocalSyncRoot(ctx);if e!=nil{return LocalSyncStatus{},e};root.Close()
 w.after=append(w.after,func()error{select{case s.localSync.wake<-struct{}{}:default:};return nil})
 return s.LocalSyncStatus(ctx,w.Principal)
}
func(s *Service)localSyncWorker(){
 defer s.workers.Done();r:=s.localSync;ticker:=time.NewTicker(r.config.Interval);defer ticker.Stop()
 for{
  s.localSyncCycle(s.workCtx)
  select{case <-s.workCtx.Done():return;case <-r.wake:case <-ticker.C:}
 }
}
func publicSyncError(e error)string{var app *Error;if errors.As(e,&app){return app.Message};return "目录或持久记录暂不可用。"}
func(s *Service)localSyncCycle(ctx context.Context){
 r:=s.localSync;r.mu.Lock();r.status.Running=true;v:=r.status;r.mu.Unlock()
 v.Items=[]LocalSyncItem{};v.Counts=LocalSyncCounts{};v.LastError=""
 root,e:=s.openLocalSyncRoot(ctx);v.Enabled=e==nil
 if e==nil{
  cycle:=ID();settings,err:=s.Settings(ctx);if err==nil{v.Paused=!settings.Upload.AutoAccept;var catalog map[string][]model.Library;catalog,err=s.localSyncLibraries(ctx);if err==nil{err=s.walkSyncDirectory(ctx,root,"",0,cycle,catalog,&v)}};root.Close();e=err
  if e==nil{e=s.forgetMissingSyncSources(ctx,cycle)}
 }
 if e!=nil{v.LastError=publicSyncError(e)}
 if r.config.Root!=""{recent,err:=store.List[LocalSyncItem](ctx,s.DB.DB,"SELECT data FROM local_sync_sources WHERE root_id=? AND state='synced' ORDER BY completed_at DESC,path LIMIT 20",r.rootID);if err!=nil{v.LastError=publicSyncError(err)}else{v.RecentSynced=recent}}
 now:=time.Now().UTC();v.LastCycleAt=&now;v.Running=false;slices.SortFunc(v.Items,func(a,b LocalSyncItem)int{return strings.Compare(a.Path,b.Path)})
 r.mu.Lock();r.status=v;r.mu.Unlock()
}
func(s *Service)forgetMissingSyncSources(ctx context.Context,cycle string)error{
 r:=s.localSync
 for{
  rows,e:=s.DB.DB.QueryContext(ctx,"SELECT path,upload_id FROM local_sync_sources WHERE root_id=? AND seen_cycle!=? AND state!='synced' LIMIT 64",r.rootID,cycle);if e!=nil{return e}
  type missing struct{path,upload string};items:=[]missing{}
  for rows.Next(){var item missing;if e=rows.Scan(&item.path,&item.upload);e!=nil{rows.Close();return e};items=append(items,item)};e=rows.Err();rows.Close();if e!=nil{return e};if len(items)==0{return nil}
  for _,item:=range items{if e=s.cancelSyncUpload(ctx,item.upload);e!=nil{return e};if _,e=s.DB.DB.ExecContext(ctx,"DELETE FROM local_sync_sources WHERE root_id=? AND path=?",r.rootID,item.path);e!=nil{return e}}
 }
}
func syncOwner()model.Principal{return model.Principal{ID:"owner",Scopes:[]string{"admin"},ExpiresAt:time.Now().UTC().Add(time.Hour)}}
func(s *Service)cancelSyncUpload(ctx context.Context,id string)error{
 if id==""{return nil}
 u,e:=s.Upload(ctx,syncOwner(),id);if e!=nil{return e}
 if u.State!="staged"&&u.State!="receiving"{return nil}
 _,e=s.Run(ctx,syncOwner(),"localSyncCancel","",nil,func(w *Write)(any,int,error){return nil,204,s.CancelUpload(ctx,w,id)});return e
}
func syncResult[T any](r Result,e error)(T,error){var v T;if e!=nil{return v,e};e=json.Unmarshal(r.Body,&v);return v,e}
