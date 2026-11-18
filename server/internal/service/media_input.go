package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"runtime"
)

// Linux and Android supply a seekable inherited regular-file descriptor to
// each decoder. An asset can be renamed or replaced without changing the
// selected inode. Android does not grant ordinary apps hard-link privileges.
const inheritedMediaFD = runtime.GOOS=="android" || runtime.GOOS=="linux"
type mediaInput struct {name string;file *os.File;size int64}
type mediaReader interface{io.ReadSeeker;io.Closer}
type sectionInput struct{*io.SectionReader}
func(sectionInput)Close()error{return nil}
func(s *Service)pinMedia(source,shadow string,size int64)(mediaInput,error){
	if !inheritedMediaFD {
		if err:=s.linkNoReplace(source,shadow);err!=nil{return mediaInput{},err}
		return mediaInput{name:shadow,size:size},nil
	}
	if err:=s.safe(source);err!=nil{return mediaInput{},err}
	file,err:=s.Root.Open(source);if err!=nil{return mediaInput{},err}
	info,err:=file.Stat()
	if err!=nil||!info.Mode().IsRegular()||info.Size()!=size{
		file.Close();if err!=nil{return mediaInput{},err};return mediaInput{},Fail(409,"media_input_changed","原件长度或类型已变化。")
	}
	return mediaInput{name:source,file:file,size:size},nil
}
func(m mediaInput)reader(s *Service)(mediaReader,error){
	if m.file!=nil{return sectionInput{io.NewSectionReader(m.file,0,m.size)},nil}
	return s.Root.Open(m.name)
}
func(m mediaInput)argument(s *Service)(string,error){
	if m.file!=nil{return "/proc/self/fd/3",nil}
	return s.Root.AbsolutePath(m.name)
}
func(m mediaInput)configure(cmd *exec.Cmd){
	configureMediaProcess(cmd)
	if m.file!=nil{cmd.ExtraFiles=[]*os.File{m.file}}
}
func(m mediaInput)release(s *Service)error{
	if m.file!=nil{return m.file.Close()}
	err:=s.Root.Remove(m.name);if errors.Is(err,os.ErrNotExist){return nil};return err
}
func(m mediaInput)verify(ctx context.Context,s *Service,hash string,size int64)error{
	if m.file==nil{return s.verify(m.name,hash,size)}
	digest:=sha256.New();reader:=io.NewSectionReader(m.file,0,size)
	buffer:=make([]byte,128<<10)
	for{
		if err:=ctx.Err();err!=nil{return err}
		n,err:=reader.Read(buffer);if n>0{digest.Write(buffer[:n])}
		if err!=nil{if errors.Is(err,io.EOF){break};return err}
	}
	info,err:=m.file.Stat();if err!=nil{return err}
	if info.Size()!=size||hex.EncodeToString(digest.Sum(nil))!=hash{return Fail(409,"content_mismatch","研究原件的长度或摘要已变化。")}
	return nil
}
