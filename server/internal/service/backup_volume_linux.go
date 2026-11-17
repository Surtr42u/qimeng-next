//go:build linux

package service

import("fmt";"os";"syscall")
func volumeIdentity(name string)(string,error){info,e:=os.Stat(name);if e!=nil{return "",e};stat,ok:=info.Sys().(*syscall.Stat_t);if !ok{return "",fmt.Errorf("filesystem volume identity unavailable")};return fmt.Sprintf("dev:%d",stat.Dev),nil}
