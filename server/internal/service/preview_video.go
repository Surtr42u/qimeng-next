package service

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"os/exec"
	"strconv"
	"time"

	"qimeng.local/next/server/internal/model"
)

type streamMetadata struct {
	Index int
	Width int
	Height int
	Disposition struct{ Attached int `json:"attached_pic"` }
}

type mediaMetadata struct {
	Streams []streamMetadata
	Format struct{Duration string}
}

func (s *Service) probeStreams(ctx context.Context, binary, demuxer string, input mediaInput) (mediaMetadata, error) {
	var metadata mediaMetadata
 source,err:=input.argument(s);if err!=nil{return metadata,err}
	cmd:=exec.CommandContext(ctx,binary,"-v","error","-protocol_whitelist","file,pipe","-f",demuxer,
		"-select_streams","v","-show_entries","stream=index,width,height:stream_disposition=attached_pic:format=duration",
		"-of","json",source)
	input.configure(cmd)
	output:=&boundedOutput{limit:64<<10}
	cmd.Stdout,cmd.Stderr=output,io.Discard
	if err:=cmd.Run();err!=nil{return metadata,err}
	err=json.Unmarshal(output.Bytes(),&metadata)
	return metadata,err
}

func (m mediaMetadata) primary() (streamMetadata,bool) {
	for _,stream:=range m.Streams {if stream.Disposition.Attached==0 {return stream,true}}
	if len(m.Streams)>0{return m.Streams[0],true}
	return streamMetadata{},false
}

func representativePositions(durationMs int64) []int64 {
	if durationMs<=0{return []int64{0}}
	positions:=make([]int64,0,8)
	seen:=make(map[int64]bool,8)
	for _,percent:=range []int64{35,25,45,15,55,5,65,0}{
		millis:=durationMs*percent/100
		if !seen[millis]{seen[millis]=true;positions=append(positions,millis)}
	}
	return positions
}

func extremeFrame(frame image.Image) bool {
	bounds:=frame.Bounds()
	black,white:=true,true
	for y:=0;y<10;y++{
		for x:=0;x<10;x++{
			point:=frame.At(bounds.Min.X+(2*x+1)*bounds.Dx()/20,bounds.Min.Y+(2*y+1)*bounds.Dy()/20)
			value:=color.GrayModel.Convert(point).(color.Gray).Y
			black=black&&value<15
			white=white&&value>240
			if !black&&!white{return false}
		}
	}
	return black||white
}

func (s *Service) extractPreview(ctx context.Context,binary string,input mediaInput,demuxer,stream string,millis int64,edge int)([]byte,error){
 source,err:=input.argument(s);if err!=nil{return nil,err}
	args:=[]string{"-nostdin","-v","error","-threads","2","-max_alloc","67108864","-protocol_whitelist","file,pipe","-f",demuxer}
	if millis>0 {args=append(args,"-ss",strconv.FormatFloat(float64(millis)/1000,'f',3,64))}
	args=append(args,"-i",source,"-map",stream,"-frames:v","1","-vf","scale="+strconv.Itoa(edge)+":"+strconv.Itoa(edge)+":force_original_aspect_ratio=decrease","-threads","1","-q:v","2","-f","image2pipe","-vcodec","mjpeg","pipe:1")
	cmd:=exec.CommandContext(ctx,binary,args...)
	input.configure(cmd)
	buffer:=&boundedOutput{limit:2<<20}
	cmd.Stdout,cmd.Stderr=buffer,io.Discard
	if err:=cmd.Run();err!=nil{return nil,err}
	if _,err:=jpeg.DecodeConfig(bytes.NewReader(buffer.Bytes()));err!=nil{return nil,err}
	return buffer.Bytes(),nil
}

func (s *Service) externalPreview(ctx context.Context,source mediaInput,a model.Asset,edge int)([]byte,error){
	ffmpeg:=toolPath("QIMENG_FFMPEG","ffmpeg")
	if ffmpeg==""{return nil,Fail(501,"media_tool_unavailable","此格式需要配置 FFmpeg。")}
	demuxer:=mediaDemuxer(a.MimeType)
	if demuxer==""{return nil,Fail(415,"media_unsupported","此格式没有可用的预览通道。")}
	deadline,cancel:=context.WithTimeout(ctx,30*time.Second);defer cancel()
	if a.MediaType!="video"{
		data,err:=s.extractPreview(deadline,ffmpeg,source,demuxer,"0:v:0",0,edge)
		if err!=nil{return nil,Fail(422,"thumbnail_decode_failed","预览生成失败，原件保留。")}
		return data,nil
	}
	if ffprobe:=toolPath("QIMENG_FFPROBE","ffprobe");ffprobe!=""{
		metadata,err:=s.probeStreams(deadline,ffprobe,demuxer,source)
		if err==nil{
			for _,stream:=range metadata.Streams{
				if stream.Disposition.Attached!=0{
					if cover,err:=s.extractPreview(deadline,ffmpeg,source,demuxer,"0:"+strconv.Itoa(stream.Index),0,edge);err==nil{return cover,nil}
					break
				}
			}
		}
	}
	var first []byte
	for _,millis:=range representativePositions(a.DurationMs){
		if deadline.Err()!=nil{break}
		data,err:=s.extractPreview(deadline,ffmpeg,source,demuxer,"0:V:0",millis,edge)
		if err!=nil{continue}
		frame,err:=jpeg.Decode(bytes.NewReader(data))
		if err!=nil{continue}
		if !extremeFrame(frame){return data,nil}
		if first==nil{first=data}
	}
	if ctx.Err()!=nil{return nil,ctx.Err()}
	if first!=nil{return first,nil}
	return nil,Fail(422,"thumbnail_decode_failed","无法读取视频封面或代表帧，原件保留。")
}
