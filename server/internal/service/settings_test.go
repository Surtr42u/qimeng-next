package service

import (
 "context"
 "encoding/json"
 "testing"
 "time"

 "qimeng.local/next/server/internal/model"
)

func TestConfiguredScanAdmissionCanIncreaseWithoutInterruptingRunningJob(t *testing.T){
 ctx:=context.Background();s,e:=New(Config{DataDir:t.TempDir(),AdminToken:"synthetic-scan-settings-00000000000000000",ReserveBytes:1});if e!=nil{t.Fatal(e)};defer s.Close()
 result:=testMutation(t,s,"scan-test-library",func(w *Write)(any,int,error){v,e:=s.CreateLibrary(ctx,w,"Synthetic scans","regular");return v,201,e});var library model.Library;if e=json.Unmarshal(result.Body,&library);e!=nil{t.Fatal(e)}
 change:=func(workers int){t.Helper();current,e:=s.Settings(ctx);if e!=nil{t.Fatal(e)};raw,_:=json.Marshal(map[string]any{"expectedRevision":current.Revision,"scan":map[string]int{"workers":workers,"thumbEdge":800},"upload":map[string]bool{"autoAccept":true}});var in SettingsUpdate;if e=json.Unmarshal(raw,&in);e!=nil{t.Fatal(e)};testMutation(t,s,"scan-setting-"+ID(),func(w *Write)(any,int,error){v,e:=s.UpdateSettings(ctx,w,in);return v,200,e})}
 change(1)
 for _,id:=range []string{"first-scan","second-scan"}{if e=saveScan(ctx,s.DB.DB,ScanJob{ID:id,LibraryID:library.ID,State:"queued",Issues:[]ScanIssue{},CreatedAt:time.Now().UTC(),UpdatedAt:time.Now().UTC()});e!=nil{t.Fatal(e)}}
 if _,e=s.admitScan(ctx,"first-scan");e!=nil{t.Fatal(e)}
 blocked,stop:=context.WithTimeout(ctx,50*time.Millisecond);_,e=s.admitScan(blocked,"second-scan");stop();if e!=context.DeadlineExceeded{t.Fatalf("configured concurrency was ignored: %v",e)}
 change(2)
 waiting,cancel:=context.WithTimeout(ctx,3*time.Second);defer cancel();if _,e=s.admitScan(waiting,"second-scan");e!=nil{t.Fatal(e)}
 change(1)
 for _,id:=range []string{"first-scan","second-scan"}{job,e:=s.Scan(ctx,id);if e!=nil||job.State!="running"{t.Fatal("lowering concurrency interrupted existing work")}}
 s.releaseScan();s.releaseScan()
}

func TestAutomaticScanBudgetUsesMemoryAndCPUConservatively(t *testing.T){
 for _,row:=range []struct{cpu int;memory uint64;want int}{{64,0,1},{64,128<<20,1},{8,512<<20,2},{32,8<<30,8},{1,8<<30,1}}{if got:=automaticScanWorkers(row.cpu,row.memory);got!=row.want{t.Fatalf("cpu=%d memory=%d: %d != %d",row.cpu,row.memory,got,row.want)}}
}
