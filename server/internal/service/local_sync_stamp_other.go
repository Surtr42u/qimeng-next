//go:build !windows && !linux

package service

import "os"

const syncReadFlags=0
const syncPlatformSupported=false
func syncFileStamp(_ *os.File,_ os.FileInfo)(string,error){return "",Fail(501,"local_sync_platform_unsupported","当前服务平台尚未提供可靠的文件变动版本。")}
