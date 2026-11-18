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
 "strconv"
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
 var backupTargets map[string]service.BackupTarget;if raw:=os.Getenv("QIMENG_BACKUP_TARGETS");raw!=""{if e:=json.Unmarshal([]byte(raw),&backupTargets);e!=nil{return errors.New("QIMENG_BACKUP_TARGETS must contain trusted backup target configuration")}}
 password:=os.Getenv("QIMENG_ADMIN_PASSWORD");if e:=os.Unsetenv("QIMENG_ADMIN_PASSWORD");e!=nil{return e}
 localSync:=service.LocalSyncConfig{Root:os.Getenv("QIMENG_LOCAL_SYNC_ROOT")}
 for key,field:=range map[string]*time.Duration{"QIMENG_LOCAL_SYNC_INTERVAL":&localSync.Interval,"QIMENG_LOCAL_SYNC_STABLE_AGE":&localSync.StableAge}{
  if value:=os.Getenv(key);value!=""{d,e:=time.ParseDuration(value);if e!=nil||d<=0{return errors.New("invalid local input folder timing")};*field=d}
 }
 scanner:=service.LibraryMonitorConfig{WatchEnabled:true,PollInterval:5*time.Minute}
 researchEnabled:=false
 if value:=os.Getenv("QIMENG_RESEARCH_ENABLED");value!=""{enabled,e:=strconv.ParseBool(value);if e!=nil{return errors.New("invalid public research setting")};researchEnabled=enabled}
 if value:=os.Getenv("QIMENG_SCANNER_WATCH_ENABLED");value!=""{on,e:=strconv.ParseBool(value);if e!=nil{return errors.New("invalid scanner watch setting")};scanner.WatchEnabled=on}
 if value:=os.Getenv("QIMENG_SCANNER_POLL_INTERVAL");value!=""{duration,e:=time.ParseDuration(value);if e!=nil{return errors.New("invalid scanner polling interval")};if duration>0{scanner.PollInterval=duration}}
 s,e:=service.New(service.Config{DataDir:os.Getenv("QIMENG_DATA_DIR"),AdminToken:os.Getenv("QIMENG_ADMIN_TOKEN"),AdminPassword:password,AIToken:os.Getenv("QIMENG_AI_TOKEN"),StorageRoots:storages,BackupTargets:backupTargets,DefaultBackupTarget:os.Getenv("QIMENG_DEFAULT_BACKUP_TARGET"),SecureCookies:os.Getenv("QIMENG_SECURE_COOKIES")=="true"||os.Getenv("QIMENG_EPHEMERAL_TLS")=="true",ResearchEnabled:researchEnabled,AIConfigured:os.Getenv("QIMENG_AI_CONFIGURED")=="true",LocalSync:localSync,Scanner:&scanner});password="";if e!=nil{return e};defer s.Close()
 address:=os.Getenv("QIMENG_LISTEN");if address==""{address="127.0.0.1:8421"};api:=httpapi.New(s);var handler http.Handler=api
 if web:=os.Getenv("QIMENG_WEB_DIR");web!=""{
  files:=http.FileServer(http.Dir(web))
  handler=http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
   if strings.HasPrefix(r.URL.Path,"/api/")||r.URL.Path=="/healthz"||r.URL.Path=="/metrics"{api.ServeHTTP(w,r);return}
   if r.Method!="GET"&&r.Method!="HEAD"{http.Error(w,"method not allowed",405);return}
   local:=filepath.Clean(filepath.FromSlash(strings.TrimPrefix(r.URL.Path,"/")))
   if local==".."||strings.HasPrefix(local,".."+string(os.PathSeparator))||filepath.IsAbs(local){http.Error(w,"invalid path",400);return}
   if _,e:=os.Stat(filepath.Join(web,local));errors.Is(e,os.ErrNotExist){http.ServeFile(w,r,filepath.Join(web,"index.html"));return}
   files.ServeHTTP(w,r)
  })
 }
 server:=&http.Server{Addr:address,Handler:s.Traffic.Handler(handler),ReadHeaderTimeout:10*time.Second,IdleTimeout:90*time.Second,MaxHeaderBytes:32<<10}
 listener,e:=supervisedListener(address,s.InstanceID,os.Getenv("QIMENG_STARTUP_RECEIPT"),os.Getenv("QIMENG_LAUNCH_ID"),os.Getenv("QIMENG_EPHEMERAL_TLS")=="true");if e!=nil{return e};defer listener.Close()
 discovery,e:=readDiscoveryConfig(os.Getenv);if e!=nil{return e}
 if os.Getenv("QIMENG_EPHEMERAL_TLS")=="true"{discovery.Enabled=false}
 stopDiscovery,e:=startDiscovery(listener.Addr(),s.InstanceID,discovery)
 if e!=nil{slog.Warn("LAN discovery unavailable","detail","check configured interface and multicast support")}else{defer stopDiscovery()}
 ctx,stop:=signal.NotifyContext(context.Background(),os.Interrupt,syscall.SIGTERM);defer stop();done:=make(chan error,1)
 go func(){done<-server.Serve(listener)}();select{case e:=<-done:if errors.Is(e,http.ErrServerClosed){return nil};return e;case <-ctx.Done():shutdown,cancel:=context.WithTimeout(context.Background(),20*time.Second);defer cancel();return server.Shutdown(shutdown)}
}
