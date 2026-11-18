package httpapi

import "net/http"

func (a *API) libraryLocationRoutes() {
 a.get("/libraries/{id}/location",func(r *http.Request)(any,error){return a.s.LibraryLocation(r.Context(),principal(r),r.PathValue("id"))})
 a.mux.HandleFunc("POST /api/v1/storages/resolve-library-root",a.auth(func(w http.ResponseWriter,r *http.Request){
  in,_,e:=decode[struct{RootPath string `json:"rootPath"`}](w,r)
  if e!=nil {fail(w,e);return}
  location,e:=a.s.ResolveLibraryRoot(principal(r),in.RootPath)
  if e!=nil {fail(w,e);return}
  respond(w,http.StatusOK,location)
 }))
}
