//go:build linux

package service

import("errors";"fmt";"os";"syscall")

const syncReadFlags=syscall.O_NONBLOCK
const syncPlatformSupported=true
func syncPathLink(info os.FileInfo)bool{return info.Mode()&os.ModeSymlink!=0}

func syncFileStamp(_ *os.File,info os.FileInfo)(string,error){
 st,ok:=info.Sys().(*syscall.Stat_t);if !ok{return "",errors.New("file identity unavailable")}
 return fmt.Sprintf("%d/%d/%d/%d/%d/%d",st.Dev,st.Ino,st.Ctim.Sec,st.Ctim.Nsec,info.Size(),info.ModTime().UnixNano()),nil
}
func fileObjectIdentity(_ *os.File,info os.FileInfo)(string,error){
 st,ok:=info.Sys().(*syscall.Stat_t);if !ok{return "",errors.New("file identity unavailable")}
 return fmt.Sprintf("%d/%d",st.Dev,st.Ino),nil
}
