package httpapi

import (
 "bytes"
 "net/http"
 "time"
)

func(a *API)researchRoutes(){
 a.get("/uploads/{id}/research-materials",func(r *http.Request)(any,error){return a.s.UploadResearchMaterials(r.Context(),principal(r),r.PathValue("id"),r.URL.Query().Get("expectedHash"))})
 a.mux.HandleFunc("GET /api/v1/uploads/{id}/content",a.auth(func(w http.ResponseWriter,r *http.Request){f,u,e:=a.s.UploadContent(r.Context(),principal(r),r.PathValue("id"),r.URL.Query().Get("expectedHash"));if e!=nil{fail(w,e);return};defer f.Close();w.Header().Set("Content-Type",u.MimeType);w.Header().Set("X-Content-Type-Options","nosniff");w.Header().Set("Cache-Control","private, no-store");w.Header().Set("ETag",`"`+u.Hash+`"`);http.ServeContent(w,r,u.OriginalFileName,u.CreatedAt,f)}))
 a.get("/research-artifacts/{id}",func(r *http.Request)(any,error){artifact,_,e:=a.s.ResearchArtifact(r.Context(),principal(r),r.PathValue("id"));return artifact,e})
 a.mux.HandleFunc("GET /api/v1/research-artifacts/{id}/content",a.auth(func(w http.ResponseWriter,r *http.Request){artifact,content,e:=a.s.ResearchArtifact(r.Context(),principal(r),r.PathValue("id"));if e!=nil{fail(w,e);return};w.Header().Set("Content-Type",artifact.MimeType);w.Header().Set("X-Content-Type-Options","nosniff");w.Header().Set("Cache-Control","private, no-store");w.Header().Set("ETag",`"`+artifact.SHA256+`"`);http.ServeContent(w,r,"research-preview.jpg",artifact.ExpiresAt.Add(-24*time.Hour),bytes.NewReader(content))}))
}
