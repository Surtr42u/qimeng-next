package httpapi

import (
 "net/http"
 "qimeng.local/next/server/internal/service"
)

func(a *API)backupRoutes(){
 a.get("/backups",func(r *http.Request)(any,error){n,e:=limit(r);if e!=nil{return nil,e};return a.s.Backups(r.Context(),principal(r),r.URL.Query().Get("cursor"),n)})
 a.get("/backup-targets",func(r *http.Request)(any,error){return a.s.BackupTargets(r.Context(),principal(r))})
 a.get("/batches/{id}/cleanup-permits",func(r *http.Request)(any,error){n,e:=limit(r);if e!=nil{return nil,e};return a.s.CleanupPermits(r.Context(),principal(r),r.PathValue("id"),r.URL.Query().Get("inputId"),r.URL.Query().Get("cursor"),n)})
 a.get("/cleanup-permits/{id}",func(r *http.Request)(any,error){return a.s.CleanupPermit(r.Context(),principal(r),r.PathValue("id"))})
 a.mux.HandleFunc("POST /api/v1/batches/{id}/protections/{protectionId}/cleanup-permits",a.auth(func(w http.ResponseWriter,r *http.Request){in,raw,e:=decode[service.CleanupPermitInput](w,r);if e!=nil{fail(w,e);return};out,e:=a.s.IssueCleanupPermit(r.Context(),principal(r),r.PathValue("id"),r.PathValue("protectionId"),r.Header.Get("Idempotency-Key"),raw,in);if e!=nil{fail(w,e);return};result(w,out)}))
 write[service.CleanupReceiptInput](a,"POST","/cleanup-permits/{id}/receipt",true,func(r *http.Request,w *service.Write,in service.CleanupReceiptInput)(any,int,error){p,e:=a.s.RecordCleanupReceipt(r.Context(),w,r.PathValue("id"),in);return p,200,e})
 a.get("/batches/{id}/protections",func(r *http.Request)(any,error){n,e:=limit(r);if e!=nil{return nil,e};return a.s.BatchProtections(r.Context(),principal(r),r.PathValue("id"),r.URL.Query().Get("cursor"),n)})
 a.get("/batches/{id}/protections/{protectionId}",func(r *http.Request)(any,error){return a.s.BatchProtection(r.Context(),principal(r),r.PathValue("id"),r.PathValue("protectionId"))})
 write[struct{}](a,"POST","/batches/{id}/protections",true,func(r *http.Request,w *service.Write,_ struct{})(any,int,error){p,e:=a.s.StartBatchProtection(r.Context(),w,r.PathValue("id"));return p,202,e})
 write[struct{}](a,"POST","/batches/{id}/protections/{protectionId}/retry",true,func(r *http.Request,w *service.Write,_ struct{})(any,int,error){p,e:=a.s.RetryBatchProtection(r.Context(),w,r.PathValue("id"),r.PathValue("protectionId"));return p,202,e})
 a.get("/backups/{id}",func(r *http.Request)(any,error){return a.s.Backup(r.Context(),principal(r),r.PathValue("id"))})
 write[struct{}](a,"POST","/backups/{id}/export",true,func(r *http.Request,w *service.Write,_ struct{})(any,int,error){b,e:=a.s.StartBackupExport(r.Context(),w,r.PathValue("id"));return b,202,e})
 a.mux.HandleFunc("GET /api/v1/backups/{id}/archive",a.auth(func(w http.ResponseWriter,r *http.Request){file,b,e:=a.s.BackupArchive(r.Context(),principal(r),r.PathValue("id"));if e!=nil{fail(w,e);return};defer file.Close();w.Header().Set("Content-Type","application/octet-stream");w.Header().Set("Content-Disposition",`attachment; filename="qimeng-`+b.ID+`.qnext"`);w.Header().Set("Cache-Control","private, no-store");w.Header().Set("ETag",`"`+b.ArchiveHash+`"`);http.ServeContent(w,r,"backup.qnext",*b.ExportedAt,file)}))
}
