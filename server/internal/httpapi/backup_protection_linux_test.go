//go:build linux

package httpapi_test

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "errors"
 "os"
 "strings"
 "testing"
 "time"

 "qimeng.local/next/server/internal/model"
 "qimeng.local/next/server/internal/service"
 "qimeng.local/next/server/internal/store"
)

func awaitProtection(t *testing.T,f *fixture,batch,id string)service.BatchProtection{t.Helper();deadline:=time.Now().Add(20*time.Second);for time.Now().Before(deadline){r:=f.json("GET","/batches/"+batch+"/protections/"+id,"",nil,200);var p service.BatchProtection;decode(t,r,&p);if p.State=="completed"||p.State=="failed"||p.State=="interrupted"{return p};time.Sleep(10*time.Millisecond)};t.Fatal("protection did not finish");return service.BatchProtection{}}
func TestDistinctVolumeProtectionReconcilesManifestAndNewLeaseRecordsOutcome(t *testing.T){
 f:=setup(t);target,e:=os.MkdirTemp("/dev/shm","qimeng-synthetic-proof-");if e!=nil{t.Skip("isolated tmpfs unavailable")};t.Cleanup(func(){os.RemoveAll(target)});f.s.Config.BackupTargets=map[string]service.BackupTarget{"synthetic":{Path:target,FailureDomain:"synthetic-test-volume"}};f.s.Config.DefaultBackupTarget="synthetic"
 r:=f.json("GET","/backup-targets","",nil,200);var targets model.Page[service.BackupTargetInfo];decode(t,r,&targets);if len(targets.Items)!=1||!targets.Items[0].DistinctVolume{t.Skip("test runner has no separate synthetic volume")}
 r=f.json("POST","/batches","proof-original-selection",map[string]any{"libraryId":f.library,"mode":"ordinary","label":"Synthetic complete selection"},201);var batch model.Batch;decode(t,r,&batch);inputs:=f.declareInputs(batch.ID,"protected.png","not-received.png")
 u,_:=f.upload("protected.png",201,map[string]any{"batchId":batch.ID,"inputId":inputs["protected.png"]});asset:=*f.complete(u).Asset
 r=f.json("POST","/batches/"+batch.ID+"/protections","protect-original-selection",struct{}{},202);var p service.BatchProtection;decode(t,r,&p);p=awaitProtection(t,f,batch.ID,p.ID);if p.State!="completed"||len(p.Items)!=2||p.Items[0].State!="protected"||p.Items[1].State!="retained"||p.Items[1].Reason!="pending"{t.Fatalf("original manifest was not reconciled: %+v",p)}
 r=f.json("GET","/batches/"+batch.ID,"",nil,200);var detail model.BatchDetail;decode(t,r,&detail);if detail.Batch.BackupState!="partially_covered"||detail.Batch.State=="completed"{t.Fatal("received uploads falsely proved whole selection complete")}
 credential:=model.Principal{ID:"synthetic-recycler",Label:"Synthetic executor",Scopes:[]string{"read","source.cleanup"},BatchID:batch.ID,Generation:1,ExpiresAt:time.Now().UTC().Add(time.Hour)};secret:="synthetic-recycler-token-not-a-real-secret";hash:=sha256.Sum256([]byte(secret));raw,e:=store.Encode(credential);if e!=nil{t.Fatal(e)};if _,e=f.s.DB.DB.Exec("INSERT INTO credentials(id,token_hash,expires_at,data) VALUES(?,?,?,?)",credential.ID,hex.EncodeToString(hash[:]),credential.ExpiresAt.Format(time.RFC3339Nano),raw);e!=nil{t.Fatal(e)}
 body,_:=json.Marshal(service.CleanupPermitInput{InputID:inputs["protected.png"],ExpectedHash:asset.Hash,SourceSize:asset.Size});url:="/batches/"+batch.ID+"/protections/"+p.ID+"/cleanup-permits";r=f.request("POST",url,"source-recycle-permit",secret,body,nil);if r.Code!=201{t.Fatal(r.Body.String())};var permit service.CleanupPermit;decode(t,r,&permit);if permit.Generation!=1||permit.CredentialID!=credential.ID||permit.Hash!=asset.Hash{t.Fatal("permit did not bind input and executor generation")}
 old:=credential;credential.Generation=2;raw,e=store.Encode(credential);if e!=nil{t.Fatal(e)};if _,e=f.s.DB.DB.Exec("UPDATE credentials SET data=? WHERE id=?",raw,credential.ID);e!=nil{t.Fatal(e)}
 _,e=f.s.Run(context.Background(),old,"stale-source-receipt","stale-source-receipt",nil,func(w *service.Write)(any,int,error){v,e:=f.s.RecordCleanupReceipt(context.Background(),w,permit.ID,service.CleanupReceiptInput{Outcome:"recycled",ProviderReceipt:"synthetic_provider_receipt"});return v,200,e});var app *service.Error;if !errors.As(e,&app)||app.Code!="lease_changed"{t.Fatal("old worker was not rejected")}
 receiptBody:=[]byte(`{"outcome":"recycled","providerReceipt":"synthetic_provider_receipt"}`);r=f.request("POST","/cleanup-permits/"+permit.ID+"/receipt","source-recycle-receipt",secret,receiptBody,nil);if r.Code!=200{t.Fatal(r.Body.String())};decode(t,r,&permit);if permit.State!="recorded"||permit.Receipt==nil||permit.Receipt.Generation!=2{t.Fatal("new lease could not record completed source outcome")}
 r=f.request("POST",url,"duplicate-source-permit",secret,body,nil);if r.Code!=409{t.Fatal("second source recycle was authorized")};current,e:=f.s.Asset(context.Background(),asset.ID);if e!=nil||current.Hash!=asset.Hash||current.Trashed{t.Fatal("source receipt touched server original")}
}

func TestProtectionReceiptRequiresIndependentBytesAtPermitTime(t *testing.T){
 f:=setup(t);target,e:=os.MkdirTemp("/dev/shm","qimeng-synthetic-recheck-");if e!=nil{t.Skip("isolated tmpfs unavailable")};t.Cleanup(func(){os.RemoveAll(target)});f.s.Config.BackupTargets=map[string]service.BackupTarget{"synthetic":{Path:target,FailureDomain:"synthetic-test-volume"}};f.s.Config.DefaultBackupTarget="synthetic";r:=f.json("GET","/backup-targets","",nil,200);var targets model.Page[service.BackupTargetInfo];decode(t,r,&targets);if !targets.Items[0].DistinctVolume{t.Skip("test runner has no separate synthetic volume")}
 u,_:=f.upload("target-corruption.png",202,nil);out:=f.complete(u);r=f.json("POST","/batches/"+out.Upload.BatchID+"/protections","protect-before-corruption",struct{}{},202);var p service.BatchProtection;decode(t,r,&p);p=awaitProtection(t,f,out.Upload.BatchID,p.ID);if p.State!="completed"{t.Fatal(p.ErrorCode)}
 object:=target+"/objects/sha256/"+out.Asset.Hash[:2]+"/"+out.Asset.Hash;if e=os.WriteFile(object,[]byte(strings.Repeat("x",int(out.Asset.Size))),0600);e!=nil{t.Fatal(e)}
 f.json("POST","/batches/"+out.Upload.BatchID+"/protections/"+p.ID+"/cleanup-permits","reject-corrupt-independent-copy",service.CleanupPermitInput{InputID:p.Items[0].InputID,ExpectedHash:out.Asset.Hash,SourceSize:out.Asset.Size},409)
 var permits int;if e=f.s.DB.DB.QueryRow("SELECT count(*) FROM source_cleanup_permits").Scan(&permits);e!=nil||permits!=0{t.Fatal("old proof timestamp authorized cleanup after copy corruption")}
}
