package main

import (
 "context"
 "errors"
 "encoding/json"
 "log/slog"
 "net/http"
 "os"
 "os/signal"
 "path/filepath"
 "strings"
 "syscall"
 "time"
 _ "time/tzdata"

 "qimeng.local/next/server/internal/httpapi"
 "qimeng.local/next/server/internal/service"
)

func main(){if e:=run();e!=nil{slog.Error("service stopped","reason","configuration, storage or listener failure");os.Exit(1)}}
func run()error{
 if zone:=os.Getenv("QIMENG_TIMEZONE");zone!=""{location,e:=time.LoadLocation(zone);if e!=nil{return e};time.Local=location}
 var storages map[string]string;if raw:=os.Getenv("QIMENG_STORAGE_ROOTS");raw!=""{if e:=json.Unmarshal([]byte(raw),&storages);e!=nil{return errors.New("QIMENG_STORAGE_ROOTS must be a JSON object of logical names to trusted root paths")}}
 s,e:=service.New(service.Config{DataDir:os.Getenv("QIMENG_DATA_DIR"),AdminToken:os.Getenv("QIMENG_ADMIN_TOKEN"),AIToken:os.Getenv("QIMENG_AI_TOKEN"),StorageRoots:storages,SecureCookies:os.Getenv("QIMENG_SECURE_COOKIES")=="true",AIConfigured:os.Getenv("QIMENG_AI_CONFIGURED")=="true"});if e!=nil{return e};defer s.Close()
 address:=os.Getenv("QIMENG_LISTEN");if address==""{address="127.0.0.1:8421"};api:=httpapi.New(s);var handler http.Handler=api
 if web:=os.Getenv("QIMENG_WEB_DIR");web!=""{
  files:=http.FileServer(http.Dir(web))
  handler=http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
   if strings.HasPrefix(r.URL.Path,"/api/")||r.URL.Path=="/healthz"{api.ServeHTTP(w,r);return}
   if r.Method!="GET"&&r.Method!="HEAD"{http.Error(w,"method not allowed",405);return}
   local:=filepath.Clean(filepath.FromSlash(strings.TrimPrefix(r.URL.Path,"/")))
   if local==".."||strings.HasPrefix(local,".."+string(os.PathSeparator))||filepath.IsAbs(local){http.Error(w,"invalid path",400);return}
   if _,e:=os.Stat(filepath.Join(web,local));errors.Is(e,os.ErrNotExist){http.ServeFile(w,r,filepath.Join(web,"index.html"));return}
   files.ServeHTTP(w,r)
  })
 }
 server:=&http.Server{Addr:address,Handler:handler,ReadHeaderTimeout:10*time.Second,IdleTimeout:90*time.Second,MaxHeaderBytes:32<<10}
 ctx,stop:=signal.NotifyContext(context.Background(),os.Interrupt,syscall.SIGTERM);defer stop();done:=make(chan error,1)
 go func(){done<-server.ListenAndServe()}();select{case e:=<-done:if errors.Is(e,http.ErrServerClosed){return nil};return e;case <-ctx.Done():shutdown,cancel:=context.WithTimeout(context.Background(),20*time.Second);defer cancel();return server.Shutdown(shutdown)}
}
