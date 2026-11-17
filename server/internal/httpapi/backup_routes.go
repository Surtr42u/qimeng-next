package httpapi

import (
 "net/http"
 "qimeng.local/next/server/internal/service"
)

func(a *API)backupRoutes(){
 a.get("/backups",func(r *http.Request)(any,error){n,e:=limit(r);if e!=nil{return nil,e};return a.s.Backups(r.Context(),principal(r),r.URL.Query().Get("cursor"),n)})
 a.get("/backups/{id}",func(r *http.Request)(any,error){return a.s.Backup(r.Context(),principal(r),r.PathValue("id"))})
 write[struct{}](a,"POST","/backups/{id}/export",true,func(r *http.Request,w *service.Write,_ struct{})(any,int,error){b,e:=a.s.StartBackupExport(r.Context(),w,r.PathValue("id"));return b,202,e})
 a.mux.HandleFunc("GET /api/v1/backups/{id}/archive",a.auth(func(w http.ResponseWriter,r *http.Request){file,b,e:=a.s.BackupArchive(r.Context(),principal(r),r.PathValue("id"));if e!=nil{fail(w,e);return};defer file.Close();w.Header().Set("Content-Type","application/octet-stream");w.Header().Set("Content-Disposition",`attachment; filename="qimeng-`+b.ID+`.qnext"`);w.Header().Set("Cache-Control","private, no-store");w.Header().Set("ETag",`"`+b.ArchiveHash+`"`);http.ServeContent(w,r,"backup.qnext",*b.ExportedAt,file)}))
}
