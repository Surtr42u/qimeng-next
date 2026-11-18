package service

import (
	"context"
	"errors"
	"os"
	"testing"
)

func TestInheritedDecoderReadsTheSelectedObjectAfterPathReused(t *testing.T){
	if !inheritedMediaFD{t.Skip("inherited descriptor platform")}
	s,err:=New(Config{DataDir:t.TempDir(),AdminToken:"synthetic-cloud-descriptor-credential",ReserveBytes:1});if err!=nil{t.Fatal(err)};defer s.Close()
	source,shadow:="media-fixture/original.png","media-fixture/should-not-be-created.png"
	body:=syntheticPNG(t,132)
	write:=func(data []byte){t.Helper();file,e:=s.Root.OpenFile(source,os.O_CREATE|os.O_EXCL|os.O_WRONLY,0600);if e!=nil{t.Fatal(e)};if _,e=file.Write(data);e!=nil{t.Fatal(e)};if e=file.Close();e!=nil{t.Fatal(e)}}
	write(body)
	input,err:=s.pinMedia(source,shadow,int64(len(body)));if err!=nil{t.Fatal(err)};defer input.release(s)
	if _,err=s.Root.Stat(shadow);!errors.Is(err,os.ErrNotExist){t.Fatal("descriptor pin created a shadow file")}
	if err=s.Root.Remove(source);err!=nil{t.Fatal(err)};write([]byte("unrelated replacement at the original name"))
	if err=input.verify(context.Background(),s,digest(body),int64(len(body)));err!=nil{t.Fatal(err)}
	if binary:=toolPath("QIMENG_FFPROBE","ffprobe");binary!=""{
		probe,err:=s.probeStreams(context.Background(),binary,"png_pipe",input);if err!=nil{t.Fatal(err)}
		video,ok:=probe.primary();if !ok||video.Width!=2||video.Height!=2{t.Fatal("decoder followed the replacement at the original path")}
	}
}
