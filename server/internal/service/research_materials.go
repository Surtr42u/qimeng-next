package service

import (
 "bytes"
 "context"
 "database/sql"
 "encoding/json"
 "errors"
 "image/jpeg"
 "io"
 "os"
 "os/exec"
 "path"
 "strconv"
 "time"

 "golang.org/x/image/draw"
 "qimeng.local/next/server/internal/model"
 "qimeng.local/next/server/internal/store"
)

type ResearchArtifact struct{ID string `json:"id"`;ArtifactRef string `json:"artifactRef"`;UploadID string `json:"uploadId,omitempty"`;AssetID string `json:"assetId,omitempty"`;ContentVersion int64 `json:"contentVersion,omitempty"`;SourceSHA256 string `json:"sourceSha256"`;SHA256 string `json:"sha256"`;MimeType string `json:"mimeType"`;Size int64 `json:"size"`;PositionSeconds float64 `json:"positionSeconds"`;ExpiresAt time.Time `json:"expiresAt"`}
type ResearchMaterials struct{UploadID string `json:"uploadId,omitempty"`;AssetID string `json:"assetId,omitempty"`;ContentVersion int64 `json:"contentVersion,omitempty"`;SourceSHA256 string `json:"sourceSha256"`;Width int `json:"width"`;Height int `json:"height"`;DurationMs int64 `json:"durationMs"`;ProbeState string `json:"probeState"`;Artifacts []ResearchArtifact `json:"artifacts"`;BusinessWrites int `json:"businessWrites"`;Limitations []string `json:"limitations"`}
var researchSlots=make(chan struct{},2)

func(s *Service)researchVisible(ctx context.Context,u model.Upload)error{
 if u.State=="cancelled"{return Fail(404,"research_input_unavailable","输入已取消。")}
 if u.State=="ingested"{asset,e:=s.Asset(ctx,u.AssetID);if e!=nil{return e};if asset.Trashed{return Fail(404,"research_input_unavailable","媒体位于回收站，研究预览不可读取。")};if u.PublishedContentVersion<1||u.PublishedContentVersion!=asset.ContentVersion||u.Hash!=asset.Hash||u.Size!=asset.Size{return Fail(409,"original_input_unavailable","原上传的发布版本与现有作品不一致。")};return s.currentThumbnailSource(asset)}
 return nil
}

func(s *Service)researchInput(ctx context.Context,p model.Principal,id,expectedHash string)(model.Upload,string,error){
 u,e:=s.Upload(ctx,p,id);if e!=nil{return u,"",e};if expectedHash==""||u.Hash==""||expectedHash!=u.Hash{return u,"",Fail(409,"research_input_changed","输入尚未核验或内容摘要已变化。")}
 if u.State=="cancelled"{return u,"",Fail(409,"upload_cancelled","输入已取消。")}
 source:=uploadPath(u.ID)
 if u.State=="ingested"{a,e:=s.Asset(ctx,u.AssetID);if e!=nil{return u,"",e};if a.Trashed||u.PublishedContentVersion<1||u.PublishedContentVersion!=a.ContentVersion||a.Hash!=u.Hash||a.Size!=u.Size{return u,"",Fail(409,"original_input_unavailable","原输入已替换或移入回收站，不能把当前内容冒充原输入。")};if e=s.currentThumbnailSource(a);e!=nil{return u,"",e};source=assetPath(a)}
 if e=s.safe(source);e!=nil{return u,"",e};return u,source,nil
}
func(s *Service)UploadContent(ctx context.Context,p model.Principal,id,expectedHash string)(*os.File,model.Upload,error){
 s.mu.Lock();defer s.mu.Unlock();u,source,e:=s.researchInput(ctx,p,id,expectedHash);if e!=nil{return nil,u,e};f,e:=s.Root.Open(source);return f,u,e
}

type researchSubject struct {
 key,uploadID,assetID,libraryID,fileName,mime,hash,source string
 version,size int64
}
func(s *Service) UploadResearchMaterials(ctx context.Context,p model.Principal,id,expectedHash string)(ResearchMaterials,error){
 return s.buildResearchMaterials(ctx,func()(researchSubject,error){
  u,source,e:=s.researchInput(ctx,p,id,expectedHash);if e!=nil{return researchSubject{},e}
  if e=Authorize(p,"research",u.BatchID);e!=nil{return researchSubject{},e}
  return researchSubject{key:u.ID,uploadID:u.ID,libraryID:u.LibraryID,fileName:u.FileName,mime:u.MimeType,hash:u.Hash,size:u.Size,source:source},nil
 })
}
func(s *Service) AssetTaskMaterials(ctx context.Context,p model.Principal,id string)(ResearchMaterials,error){
 return s.buildResearchMaterials(ctx,func()(researchSubject,error){
  t,a,e:=s.activeAssetTask(ctx,p,id);if e!=nil{return researchSubject{},e}
  if !t.Research||!p.Has("research"){return researchSubject{},Fail(403,"asset_research_denied","该作品任务没有研究材料权限。")}
  if e=s.currentThumbnailSource(a);e!=nil{return researchSubject{},e}
  return researchSubject{key:"asset:"+a.ID+":"+strconv.FormatInt(a.ContentVersion,10),assetID:a.ID,version:a.ContentVersion,libraryID:a.LibraryID,fileName:a.FileName,mime:a.MimeType,hash:a.Hash,size:a.Size,source:assetPath(a)},nil
 })
}
func(s *Service) buildResearchMaterials(ctx context.Context,resolve func()(researchSubject,error))(result ResearchMaterials,err error){
 subject,e:=resolve();if e!=nil{return result,e}
 cache,e:=s.openResearchCache(ctx);if e!=nil{return result,e};defer cache.Close()
 cached,e:=store.Get[ResearchMaterials](ctx,cache,"SELECT data FROM materials WHERE upload_id=? AND source_hash=? AND expires>?",subject.key,subject.hash,time.Now().Unix());if e==nil{return cached,nil};if !errors.Is(e,sql.ErrNoRows){return result,e}
 select{case researchSlots<-struct{}{}:defer func(){<-researchSlots}();case <-ctx.Done():return result,ctx.Err()}
 // A seekable handle on Linux/Android, or a journalled same-volume link on
 // Windows, keeps a large video stable without copying it to the cache.
 sourceID:=ID();snapshot:="libraries/"+subject.libraryID+"/.qimeng-internal/research-sources/"+sourceID+path.Ext(subject.fileName)
 if !inheritedMediaFD{
  activeResearchSources.Store(sourceID,struct{}{})
  defer activeResearchSources.Delete(sourceID)
  if _,e=cache.ExecContext(ctx,"INSERT INTO work_sources VALUES(?,?,?)",sourceID,researchProcess,snapshot);e!=nil{return result,e}
 }
 var source mediaInput
 s.mu.Lock();current,e:=resolve();if e==nil{source,e=s.pinMedia(current.source,snapshot,subject.size)};s.mu.Unlock();if e!=nil{return result,e}
 defer func(){
  cleanup:=source.release(s)
  if cleanup==nil&&!inheritedMediaFD{_,cleanup=cache.ExecContext(context.Background(),"DELETE FROM work_sources WHERE id=?",sourceID)}
  if err==nil&&cleanup!=nil{err=Fail(500,"research_cleanup_pending","研究材料已生成，临时引用尚待清理。")}
 }()
 if e=source.verify(ctx,s,subject.hash,subject.size);e!=nil{return result,e}
 free,e:=freeBytes(s.Config.DataDir);if e!=nil{return result,e};if free-(8<<20)<s.Config.ReserveBytes{return result,Fail(507,"research_space","研究缓存空间不足，原件保持不变。")}
 metadata:=model.Asset{ID:subject.assetID,MimeType:subject.mime};if e=s.probeMedia(ctx,source,&metadata);e!=nil{return result,e}
 if metadata.ProbeState!="ready"{if metadata.ProbeState=="unavailable"{return result,Fail(501,"media_tool_unavailable","研究需要可用的媒体探测工具，未把能力缺失视为研究无结果。")};return result,Fail(422,"research_probe_failed","未能读取有效研究元数据，原件保留，不能视为已经研究但没有结果。")}
 result=ResearchMaterials{UploadID:subject.uploadID,AssetID:subject.assetID,ContentVersion:subject.version,SourceSHA256:subject.hash,Width:metadata.Width,Height:metadata.Height,DurationMs:metadata.DurationMs,ProbeState:metadata.ProbeState,Artifacts:[]ResearchArtifact{},Limitations:[]string{"预览不证明作者归属。","外部作品来源仍需独立核实。"}}
 if subject.mime=="image/gif"{result.Limitations=append(result.Limitations,"动图预览仅覆盖首帧，不能据此宣称核对了全部帧。")}
 positions:=[]float64{0};if metadata.DurationMs>0&&subject.mime!="image/gif"{duration:=float64(metadata.DurationMs)/1000;positions=[]float64{duration*.15,duration*.5,duration*.85}}
 contents:=[][]byte{};expires:=time.Now().UTC().Add(24*time.Hour);var total int64
 for _,position:=range positions{
  data,e:=s.researchFrame(ctx,source,subject.mime,metadata.Width,metadata.Height,position);if e!=nil{return result,e}
  hash:=digest(data);artifactID:=digest([]byte(subject.key+":"+subject.hash+":"+strconv.FormatFloat(position,'f',6,64)+":"+hash))
  item:=ResearchArtifact{ID:artifactID,ArtifactRef:"sa_"+artifactID,UploadID:subject.uploadID,AssetID:subject.assetID,ContentVersion:subject.version,SourceSHA256:subject.hash,SHA256:hash,MimeType:"image/jpeg",Size:int64(len(data)),PositionSeconds:position,ExpiresAt:expires}
  result.Artifacts=append(result.Artifacts,item);contents=append(contents,data);total+=item.Size
 }
 if e=source.verify(ctx,s,subject.hash,subject.size);e!=nil{return result,e}
 if _,e=resolve();e!=nil{return result,e}
 // Keep cache publication atomic; it contains no business authority or original media.
 tx,e:=cache.BeginTx(ctx,nil);if e!=nil{return result,e};defer tx.Rollback()
 if _,e=tx.ExecContext(ctx,"DELETE FROM frames WHERE expires<=?",time.Now().Unix());e!=nil{return result,e};if _,e=tx.ExecContext(ctx,"DELETE FROM materials WHERE expires<=?",time.Now().Unix());e!=nil{return result,e}
 var used,count int64;if e=tx.QueryRowContext(ctx,"SELECT coalesce(sum(bytes),0),count(*) FROM frames").Scan(&used,&count);e!=nil{return result,e}
 additional:=int64(len(result.Artifacts));for _,item:=range result.Artifacts{var existing int64;e=tx.QueryRowContext(ctx,"SELECT bytes FROM frames WHERE id=?",item.ID).Scan(&existing);if e==nil{total-=existing;additional--}else if !errors.Is(e,sql.ErrNoRows){return result,e}}
 if used+total>researchCacheLimit||count+additional>10000{return result,Fail(429,"research_cache_full","研究预览缓存已满，原件和业务任务保留；等待过期后重试。")}
 for i,item:=range result.Artifacts{raw,e:=store.Encode(item);if e!=nil{return result,e};if _,e=tx.ExecContext(ctx,"INSERT OR REPLACE INTO frames VALUES(?,?,?,?,?,?,?)",item.ID,subject.key,item.SourceSHA256,expires.Unix(),item.Size,raw,contents[i]);e!=nil{return result,e}}
 raw,e:=store.Encode(result);if e!=nil{return result,e};if _,e=tx.ExecContext(ctx,"INSERT OR REPLACE INTO materials VALUES(?,?,?,?)",subject.key,subject.hash,expires.Unix(),raw);e!=nil{return result,e};return result,tx.Commit()
}

func(s *Service)researchFrame(ctx context.Context,source mediaInput,mime string,width,height int,position float64)([]byte,error){
 if width<1||height<1||int64(width)*int64(height)>40_000_000{return nil,Fail(422,"research_image_limit","画面超过研究预览解码预算，原件仍可正常保留。")}
 if builtinImage(mime){return s.scaledStill(source,mime,640,85,draw.ApproxBiLinear)}
 ffmpeg:=toolPath("QIMENG_FFMPEG","ffmpeg");if ffmpeg==""{return nil,Fail(501,"media_tool_unavailable","研究预览需要FFmpeg。")};absolute,e:=source.argument(s);if e!=nil{return nil,e}
 demuxer:=mediaDemuxer(mime);if demuxer==""{return nil,Fail(415,"media_unsupported","此格式没有受限研究通道。")};deadline,cancel:=context.WithTimeout(ctx,30*time.Second);defer cancel();cmd:=exec.CommandContext(deadline,ffmpeg,"-nostdin","-v","error","-threads","2","-max_alloc","67108864","-protocol_whitelist","file,pipe","-f",demuxer,"-ss",strconv.FormatFloat(position,'f',6,64),"-i",absolute,"-frames:v","1","-vf","scale=640:640:force_original_aspect_ratio=decrease","-threads","1","-f","image2pipe","-vcodec","mjpeg","pipe:1");source.configure(cmd)
 output:=&boundedOutput{limit:2<<20};cmd.Stdout=output;cmd.Stderr=io.Discard;if e=cmd.Run();e!=nil{return nil,Fail(422,"research_decode_failed","研究抽帧失败或超时，原件保留。")};if _,e=jpeg.DecodeConfig(bytes.NewReader(output.Bytes()));e!=nil{return nil,e};return output.Bytes(),nil
}

func(s *Service)ResearchArtifact(ctx context.Context,p model.Principal,id string)(ResearchArtifact,[]byte,error){
 var artifact ResearchArtifact
 cache,e:=s.openResearchCache(ctx);if e!=nil{return artifact,nil,e};defer cache.Close();var raw string;var content []byte
 e=cache.QueryRowContext(ctx,"SELECT data,content FROM frames WHERE id=? AND expires>?",id,time.Now().Unix()).Scan(&raw,&content);if e!=nil{return artifact,nil,Normalize(e)};if e=json.Unmarshal([]byte(raw),&artifact);e!=nil{return artifact,nil,e}
 if artifact.AssetID!="" {
  if !p.Has("research"){return artifact,nil,Fail(403,"asset_research_denied","没有当前作品的研究权限。")}
  if !p.Has("admin"){
   if p.AssetTaskID==""{return artifact,nil,Fail(403,"asset_task_scope_denied","需要该作品的研究任务。")}
   t,_,e:=s.activeAssetTask(ctx,p,p.AssetTaskID);if e!=nil{return artifact,nil,e}
   if !t.Research||t.AssetID!=artifact.AssetID{return artifact,nil,Fail(403,"asset_task_scope_denied","材料不属于当前作品任务。")}
  }
  asset,e:=s.Asset(ctx,artifact.AssetID);if e!=nil{return artifact,nil,e}
  if asset.Trashed||asset.ContentVersion!=artifact.ContentVersion||asset.Hash!=artifact.SourceSHA256{return artifact,nil,Fail(409,"asset_version_changed","材料所属的作品版本已经变化。")}
  if e=s.currentThumbnailSource(asset);e!=nil{return artifact,nil,e}
 }else{
  u,e:=s.Upload(ctx,p,artifact.UploadID);if e!=nil{return artifact,nil,e};if e=Authorize(p,"research",u.BatchID);e!=nil{return artifact,nil,e};if e=s.researchVisible(ctx,u);e!=nil{return artifact,nil,e}
  if artifact.SourceSHA256!=u.Hash{return artifact,nil,Fail(409,"research_artifact_invalid","材料来源不匹配。")}
 }
 if artifact.Size!=int64(len(content))||artifact.Size>2<<20||digest(content)!=artifact.SHA256{return artifact,nil,Fail(409,"research_artifact_invalid","研究材料完整性或来源版本校验失败。")}
 return artifact,content,nil
}
