package httpapi

import (
 "bytes"
 "net/http"
 "time"
 "qimeng.local/next/server/internal/service"
)

func(a *API)researchRoutes(){
 a.get("/research/context",func(r *http.Request)(any,error){return a.s.ResearchProviderContext(r.Context(),principal(r))})
 a.get("/research/queries/{id}",func(r *http.Request)(any,error){return a.s.PublicResearchResult(r.Context(),principal(r),r.PathValue("id"))})
 a.mux.HandleFunc("POST /api/v1/research/providers/{provider}/query",a.auth(func(w http.ResponseWriter,r *http.Request){
  in,_,e:=decode[service.PublicResearchInput](w,r);if e!=nil{fail(w,e);return}
  out,e:=a.s.QueryResearchProvider(r.Context(),principal(r),r.PathValue("provider"),r.Header.Get("Idempotency-Key"),in)
  if e!=nil{fail(w,e);return};result(w,out)
 }))
 a.get("/asset-tasks/{id}",func(r *http.Request)(any,error){return a.s.AssetTaskContext(r.Context(),principal(r),r.PathValue("id"))})
 a.get("/asset-tasks/{id}/evidence",func(r *http.Request)(any,error){n,e:=limit(r);if e!=nil{return nil,e};return a.s.AssetTaskEvidence(r.Context(),principal(r),r.PathValue("id"),r.URL.Query().Get("cursor"),n)})
 a.get("/asset-tasks/{id}/materials",func(r *http.Request)(any,error){return a.s.AssetTaskMaterials(r.Context(),principal(r),r.PathValue("id"))})
 write[service.AssetMaintenanceUpdate](a,"PATCH","/asset-tasks/{id}/metadata",true,func(r *http.Request,w *service.Write,in service.AssetMaintenanceUpdate)(any,int,error){v,e:=a.s.ApplyAssetMaintenance(r.Context(),w,r.PathValue("id"),in);return v,200,e})
 write[service.AssetTaskRename](a,"POST","/asset-tasks/{id}/rename",true,func(r *http.Request,w *service.Write,in service.AssetTaskRename)(any,int,error){v,e:=a.s.RenameAssetForTask(r.Context(),w,r.PathValue("id"),in);return v,200,e})
 a.get("/uploads/{id}/research-materials",func(r *http.Request)(any,error){return a.s.UploadResearchMaterials(r.Context(),principal(r),r.PathValue("id"),r.URL.Query().Get("expectedHash"))})
 a.mux.HandleFunc("GET /api/v1/uploads/{id}/content",a.auth(func(w http.ResponseWriter,r *http.Request){f,u,e:=a.s.UploadContent(r.Context(),principal(r),r.PathValue("id"),r.URL.Query().Get("expectedHash"));if e!=nil{fail(w,e);return};defer f.Close();w.Header().Set("Content-Type",u.MimeType);w.Header().Set("X-Content-Type-Options","nosniff");w.Header().Set("Cache-Control","private, no-store");w.Header().Set("ETag",`"`+u.Hash+`"`);http.ServeContent(w,r,u.OriginalFileName,u.CreatedAt,f)}))
 a.get("/research-artifacts/{id}",func(r *http.Request)(any,error){artifact,_,e:=a.s.ResearchArtifact(r.Context(),principal(r),r.PathValue("id"));return artifact,e})
 a.mux.HandleFunc("GET /api/v1/research-artifacts/{id}/content",a.auth(func(w http.ResponseWriter,r *http.Request){artifact,content,e:=a.s.ResearchArtifact(r.Context(),principal(r),r.PathValue("id"));if e!=nil{fail(w,e);return};w.Header().Set("Content-Type",artifact.MimeType);w.Header().Set("X-Content-Type-Options","nosniff");w.Header().Set("Cache-Control","private, no-store");w.Header().Set("ETag",`"`+artifact.SHA256+`"`);http.ServeContent(w,r,"research-preview.jpg",artifact.ExpiresAt.Add(-24*time.Hour),bytes.NewReader(content))}))
}
