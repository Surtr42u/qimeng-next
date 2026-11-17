//go:build windows

package service

import("fmt";"os";"syscall")
func volumeIdentity(name string)(string,error){file,e:=os.Open(name);if e!=nil{return "",e};defer file.Close();var info syscall.ByHandleFileInformation;if e=syscall.GetFileInformationByHandle(syscall.Handle(file.Fd()),&info);e!=nil{return "",e};return fmt.Sprintf("volume:%08x",info.VolumeSerialNumber),nil}
