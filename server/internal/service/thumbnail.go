package service

import (
 "bytes"
 "context"
 "errors"
 "image"
 _ "image/jpeg"
 _ "image/gif"
 _ "image/png"
 "io"
 "os"
 "os/exec"
 "path"
 "strconv"
 "time"

 "golang.org/x/image/draw"
 "qimeng.local/next/server/internal/model"
)

var thumbnailSlots=make(chan struct{},2)
type boundedOutput struct{bytes.Buffer;limit int}
func(w *boundedOutput)Write(b []byte)(int,error){if len(b)>w.limit-w.Len(){return 0,errors.New("media tool output exceeded limit")};return w.Buffer.Write(b)}
func builtinImage(mime string)bool{return mime=="image/jpeg"||mime=="image/png"||mime=="image/gif"}
func mediaDemuxer(mime string)string{switch mime{case "image/png":return "png_pipe";case "image/jpeg":return "mjpeg";case "image/gif":return "gif";case "image/webp":return "webp_pipe";case "image/avif","video/mp4","video/quicktime":return "mov";case "video/webm","video/x-matroska":return "matroska";case "video/x-msvideo":return "avi"};return ""}
func toolPath(env,name string)string{if configured:=os.Getenv(env);configured!=""{return configured};value,e:=exec.LookPath(name);if e!=nil{return ""};return value}
func(s *Service)probe(ctx context.Context,name string,a *model.Asset)error{
 a.ProbeState="unavailable";a.ThumbnailURL="";a.Width=0;a.Height=0;a.DurationMs=0
 if builtinImage(a.MimeType){
  f,e:=s.Root.Open(name);if e!=nil{return e};defer f.Close()
  cfg,_,e:=image.DecodeConfig(f);if e!=nil{return Fail(415,"invalid_image","文件不是可解码的图片。")}
  a.Width,a.Height=cfg.Width,cfg.Height
  if a.MimeType=="image/jpeg"{if _,e=f.Seek(0,io.SeekStart);e!=nil{return e};if jpegOrientation(f)>=5{a.Width,a.Height=a.Height,a.Width}}
  a.ProbeState="ready";a.ThumbnailURL="/api/v1/assets/"+a.ID+"/thumbnail";return nil
 }
 if ffmpeg:=toolPath("QIMENG_FFMPEG","ffmpeg");ffmpeg!=""{a.ThumbnailURL="/api/v1/assets/"+a.ID+"/thumbnail"}
 ffprobe:=toolPath("QIMENG_FFPROBE","ffprobe");if ffprobe==""{return nil}
 demuxer:=mediaDemuxer(a.MimeType);if demuxer==""{return Fail(415,"media_unsupported","此格式没有受限媒体探测通道。")};sourcePath,e:=s.Root.AbsolutePath(name);if e!=nil{return e};deadline,cancel:=context.WithTimeout(ctx,15*time.Second);defer cancel()
 result,e:=probeStreams(deadline,ffprobe,demuxer,sourcePath);if e!=nil{if ctx.Err()!=nil{return ctx.Err()};a.ProbeState="failed";return nil}
 stream,found:=result.primary();if !found{a.ProbeState="failed";return nil};a.Width,a.Height=stream.Width,stream.Height
 if result.Format.Duration!=""&&result.Format.Duration!="N/A"{duration,e:=strconv.ParseFloat(result.Format.Duration,64);if e!=nil{return e};if duration>=0&&duration<1e9{a.DurationMs=int64(duration*1000)}};a.ProbeState="ready";return nil
}
func(s *Service)thumbnailSource(ctx context.Context,id string,version int64,edge int)(*os.File,model.Asset,string,string,error){
 s.mu.Lock();defer s.mu.Unlock();a,e:=s.Asset(ctx,id);if e!=nil{return nil,a,"","",e};if a.Trashed{return nil,a,"","",Fail(404,"not_found","媒体位于回收站。")}
 if version>0&&version!=a.ContentVersion{return nil,a,"","",Fail(409,"content_changed","缩略图对应的内容版本已变化，请刷新条目。")}
 if e=s.currentThumbnailSource(a);e!=nil{return nil,a,"","",e}
 cache:="cache/"+a.ID+"/v3-"+strconv.FormatInt(a.ContentVersion,10)+"-"+strconv.Itoa(edge)+".jpg"
 if e=s.safe(cache);e!=nil{return nil,a,"","",e};if f,e:=s.Root.Open(cache);e==nil{return f,a,cache,"",nil}else if !errors.Is(e,os.ErrNotExist){return nil,a,"","",e}
 snapshot:="libraries/"+a.LibraryID+"/.qimeng-internal/cache-sources/"+ID()+path.Ext(a.FileName)
 if e=s.linkNoReplace(assetPath(a),snapshot);e!=nil{return nil,a,"","",e};return nil,a,cache,snapshot,nil
}
func(s *Service)Thumbnail(ctx context.Context,id string,versions ...int64)(*os.File,model.Asset,error){
 version:=int64(0);if len(versions)>0{version=versions[0]};return s.ThumbnailSize(ctx,id,version,"")
}
func(s *Service)currentThumbnailSource(a model.Asset)error{
 info,e:=s.Root.Stat(assetPath(a));if e!=nil{return e};if !info.Mode().IsRegular()||info.Size()!=a.Size||a.ContentMtime!=nil&&!info.ModTime().Equal(*a.ContentMtime){return Fail(409,"source_changed","原件已变化，请扫描后重新读取对应版本。")};return nil
}
func(s *Service)ThumbnailSize(ctx context.Context,id string,version int64,size string)(*os.File,model.Asset,error){
 edge:=512;switch size{case "","md":case "sm":edge=256;case "lg":edge=1024;default:return nil,model.Asset{},Fail(400,"invalid_thumbnail_size","缩略图尺寸必须是sm、md或lg。")}
 // Acquire before creating the temporary source link; queued requests must not
 // create an unbounded number of retained originals.
 select{case thumbnailSlots<-struct{}{}:defer func(){<-thumbnailSlots}();case <-ctx.Done():return nil,model.Asset{},ctx.Err()}
 cached,a,cache,source,e:=s.thumbnailSource(ctx,id,version,edge);if e!=nil{return nil,a,e};if cached!=nil{return cached,a,nil};defer s.Root.Remove(source)
 var data []byte
 if builtinImage(a.MimeType){
  data,e=s.scaledStill(source,a.MimeType,edge,92,draw.CatmullRom)
 }else{
  data,e=s.externalPreview(ctx,source,a,edge)
 }
 if e!=nil{return nil,a,e}
 if e=s.currentThumbnailSource(a);e!=nil{return nil,a,e}
 current,e:=s.Asset(ctx,id);if e!=nil{return nil,a,e};if current.Trashed||current.ContentVersion!=a.ContentVersion{return nil,a,Fail(409,"content_changed","生成期间媒体版本已变化，请刷新条目。")}
 if e=s.ensureParent(cache);e!=nil{return nil,a,e};tmp:=path.Dir(cache)+"/"+ID()+".tmp";f,e:=s.Root.OpenFile(tmp,os.O_CREATE|os.O_EXCL|os.O_WRONLY,0600);if e!=nil{return nil,a,e};defer s.Root.Remove(tmp)
 if _,e=f.Write(data);e!=nil{f.Close();return nil,a,e};if e=f.Sync();e!=nil{f.Close();return nil,a,e};if e=f.Close();e!=nil{return nil,a,e}
 if e=s.Root.Link(tmp,cache);e!=nil&&!errors.Is(e,os.ErrExist){return nil,a,e};f,e=s.Root.Open(cache);return f,a,e
}
