package service

import (
 "bytes"
 "context"
 "encoding/json"
 "errors"
 "image"
 "image/jpeg"
 _ "image/gif"
 _ "image/png"
 "io"
 "os"
 "os/exec"
 "path"
 "strconv"
 "time"

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
 if builtinImage(a.MimeType){f,e:=s.Root.Open(name);if e!=nil{return e};cfg,_,e:=image.DecodeConfig(f);f.Close();if e!=nil{return Fail(415,"invalid_image","文件不是可解码的图片。")};a.Width=cfg.Width;a.Height=cfg.Height;a.ProbeState="ready";a.ThumbnailURL="/api/v1/assets/"+a.ID+"/thumbnail";return nil}
 if ffmpeg:=toolPath("QIMENG_FFMPEG","ffmpeg");ffmpeg!=""{a.ThumbnailURL="/api/v1/assets/"+a.ID+"/thumbnail"}
 ffprobe:=toolPath("QIMENG_FFPROBE","ffprobe");if ffprobe==""{return nil}
 demuxer:=mediaDemuxer(a.MimeType);if demuxer==""{return Fail(415,"media_unsupported","此格式没有受限媒体探测通道。")};sourcePath,e:=s.Root.AbsolutePath(name);if e!=nil{return e};deadline,cancel:=context.WithTimeout(ctx,15*time.Second);defer cancel();cmd:=exec.CommandContext(deadline,ffprobe,"-v","error","-protocol_whitelist","file,pipe","-f",demuxer,"-select_streams","v:0","-show_entries","stream=width,height:format=duration","-of","json",sourcePath);configureMediaProcess(cmd)
 output:=&boundedOutput{limit:64<<10};cmd.Stdout=output;cmd.Stderr=io.Discard;if e:=cmd.Run();e!=nil{a.ProbeState="failed";return nil}
 var result struct{Streams []struct{Width,Height int};Format struct{Duration string}}
 if e:=json.Unmarshal(output.Bytes(),&result);e!=nil{return e};if len(result.Streams)==0{a.ProbeState="failed";return nil};a.Width=result.Streams[0].Width;a.Height=result.Streams[0].Height
 if result.Format.Duration!=""{duration,e:=strconv.ParseFloat(result.Format.Duration,64);if e!=nil{return e};if duration>=0&&duration<1e9{a.DurationMs=int64(duration*1000)}};a.ProbeState="ready";return nil
}
func(s *Service)thumbnailSource(ctx context.Context,id string,version int64)(*os.File,model.Asset,string,string,error){
 s.mu.Lock();defer s.mu.Unlock();a,e:=s.Asset(ctx,id);if e!=nil{return nil,a,"","",e};if a.Trashed{return nil,a,"","",Fail(404,"not_found","媒体位于回收站。")}
 if version>0&&version!=a.ContentVersion{return nil,a,"","",Fail(409,"content_changed","缩略图对应的内容版本已变化，请刷新条目。")}
 cache:="cache/"+a.ID+"/"+strconv.FormatInt(a.ContentVersion,10)+".jpg"
 if e=s.safe(cache);e!=nil{return nil,a,"","",e};if f,e:=s.Root.Open(cache);e==nil{return f,a,cache,"",nil}else if !errors.Is(e,os.ErrNotExist){return nil,a,"","",e}
 snapshot:="libraries/"+a.LibraryID+"/.qimeng-internal/cache-sources/"+ID()+path.Ext(a.FileName)
 if e=s.linkNoReplace(assetPath(a),snapshot);e!=nil{return nil,a,"","",e};return nil,a,cache,snapshot,nil
}
func(s *Service)Thumbnail(ctx context.Context,id string,versions ...int64)(*os.File,model.Asset,error){
 version:=int64(0);if len(versions)>0{version=versions[0]};cached,a,cache,source,e:=s.thumbnailSource(ctx,id,version);if e!=nil{return nil,a,e};if cached!=nil{return cached,a,nil};defer s.Root.Remove(source)
 select{case thumbnailSlots<-struct{}{}:defer func(){<-thumbnailSlots}();case <-ctx.Done():return nil,a,ctx.Err()}
 var data []byte
 if builtinImage(a.MimeType){
  if a.Width<=0||a.Height<=0||int64(a.Width)*int64(a.Height)>40_000_000{return nil,a,Fail(422,"thumbnail_resource_limit","图片超过缩略图解码预算，原件仍可查看。")}
  f,e:=s.Root.Open(source);if e!=nil{return nil,a,e};src,_,e:=image.Decode(f);f.Close();if e!=nil{return nil,a,Fail(422,"thumbnail_decode_failed","缩略图解码失败，原件保留。")}
  bounds:=src.Bounds();width,height:=bounds.Dx(),bounds.Dy();if width>480||height>480{if width>=height{height=height*480/width;width=480}else{width=width*480/height;height=480}};if width<1{width=1};if height<1{height=1};dst:=image.NewRGBA(image.Rect(0,0,width,height))
  for y:=0;y<height;y++{for x:=0;x<width;x++{dst.Set(x,y,src.At(bounds.Min.X+x*bounds.Dx()/width,bounds.Min.Y+y*bounds.Dy()/height))}}
  var out bytes.Buffer;if e=jpeg.Encode(&out,dst,&jpeg.Options{Quality:82});e!=nil{return nil,a,e};data=out.Bytes()
 }else{
  ffmpeg:=toolPath("QIMENG_FFMPEG","ffmpeg");if ffmpeg==""{return nil,a,Fail(501,"media_tool_unavailable","此格式的缩略图需要配置 FFmpeg。")}
  demuxer:=mediaDemuxer(a.MimeType);if demuxer==""{return nil,a,Fail(415,"media_unsupported","此格式没有受限预览通道。")};sourcePath,e:=s.Root.AbsolutePath(source);if e!=nil{return nil,a,e};deadline,cancel:=context.WithTimeout(ctx,30*time.Second);defer cancel();cmd:=exec.CommandContext(deadline,ffmpeg,"-nostdin","-v","error","-max_alloc","67108864","-protocol_whitelist","file,pipe","-f",demuxer,"-i",sourcePath,"-frames:v","1","-vf","scale=480:480:force_original_aspect_ratio=decrease","-f","image2pipe","-vcodec","mjpeg","pipe:1");configureMediaProcess(cmd)
  output:=&boundedOutput{limit:2<<20};cmd.Stdout=output;cmd.Stderr=io.Discard;if e=cmd.Run();e!=nil{return nil,a,Fail(422,"thumbnail_decode_failed","缩略图生成失败，原件保留。")};data=output.Bytes();if _,e=jpeg.DecodeConfig(bytes.NewReader(data));e!=nil{return nil,a,e}
 }
 if e=s.ensureParent(cache);e!=nil{return nil,a,e};tmp:=path.Dir(cache)+"/"+ID()+".tmp";f,e:=s.Root.OpenFile(tmp,os.O_CREATE|os.O_EXCL|os.O_WRONLY,0600);if e!=nil{return nil,a,e};defer s.Root.Remove(tmp)
 if _,e=f.Write(data);e!=nil{f.Close();return nil,a,e};if e=f.Sync();e!=nil{f.Close();return nil,a,e};if e=f.Close();e!=nil{return nil,a,e}
 if e=s.Root.Link(tmp,cache);e!=nil&&!errors.Is(e,os.ErrExist){return nil,a,e};f,e=s.Root.Open(cache);return f,a,e
}
