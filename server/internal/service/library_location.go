package service

import (
 "context"
 "path/filepath"

 "qimeng.local/next/server/internal/domain"
 "qimeng.local/next/server/internal/model"
)

type LibraryLocation struct {
 StorageID string `json:"storageId"`
 RootRelativePath string `json:"rootRelativePath"`
 RootPath string `json:"rootPath"`
}

func (s *Service) ResolveLibraryRoot(p model.Principal,value string) (LibraryLocation,error) {
 result:=LibraryLocation{}
 if e:=Authorize(p,"admin","");e!=nil {return result,e}
 id,relative,e:=s.Root.ResolveLibraryPath(value)
 if e!=nil||!domain.ValidMediaPath(relative) {return result,Fail(400,"library_root_denied","目录必须位于服务已配置的媒体根内，且不能包含链接或业务控制目录。")}
 return LibraryLocation{StorageID:id,RootRelativePath:relative,RootPath:filepath.Clean(value)},nil
}

func (s *Service) LibraryLocation(ctx context.Context,p model.Principal,id string) (LibraryLocation,error) {
 result:=LibraryLocation{}
 if e:=Authorize(p,"admin","");e!=nil {return result,e}
 library,e:=s.library(ctx,id);if e!=nil {return result,e}
 location,e:=s.Root.DisplayLibraryPath(id);if e!=nil {return result,e}
 return LibraryLocation{StorageID:library.StorageID,RootRelativePath:library.RootRelativePath,RootPath:location},nil
}
