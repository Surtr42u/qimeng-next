//go:build windows

package service

import("encoding/binary";"fmt";"os";"syscall";"golang.org/x/sys/windows")

const syncReadFlags=0

func syncFileStamp(file *os.File,info os.FileInfo)(string,error){
 var id syscall.ByHandleFileInformation;if e:=syscall.GetFileInformationByHandle(syscall.Handle(file.Fd()),&id);e!=nil{return "",e}
 // FILE_BASIC_INFO contains four 64-bit timestamps followed by attributes.
 // ChangeTime also detects rewrites whose original last-write time was restored.
 var basic [40]byte
 if e:=windows.GetFileInformationByHandleEx(windows.Handle(file.Fd()),windows.FileBasicInfo,&basic[0],uint32(len(basic)));e!=nil{return "",e}
 return fmt.Sprintf("%d/%d/%d/%d/%d/%d",id.VolumeSerialNumber,id.FileIndexHigh,id.FileIndexLow,binary.LittleEndian.Uint64(basic[24:32]),info.Size(),info.ModTime().UnixNano()),nil
}
