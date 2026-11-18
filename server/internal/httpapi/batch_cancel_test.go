package httpapi_test

import (
	"bytes"
	"io"
	"testing"

	"qimeng.local/next/server/internal/model"
)

func TestCancellingBatchClosesOnlyUnfinishedInputsAndRetainsAcceptedBytes(t *testing.T) {
	f:=setup(t)
	var batch model.Batch
	decode(t,f.json("POST","/batches","begin-cancellation-set",map[string]string{
		"libraryId":f.library,"mode":"ordinary","label":"Synthetic cancelled selection",
	},201),&batch)
	ids:=f.declareInputs(batch.ID,"published.png","identical.png","partial.png","never-sent.png")
	member:=func(name string)map[string]any{return map[string]any{"batchId":batch.ID,"inputId":ids[name]}}
	published,_:=f.upload("published.png",100,member("published.png"))
	asset:=f.complete(published).Asset
	if asset==nil{t.Fatal("missing initial publication")}
	identical,_:=f.upload("identical.png",100,member("identical.png"))
	if duplicate:=f.complete(identical);duplicate.Upload.State!="duplicate"{t.Fatal("duplicate setup",duplicate)}
	raw:=pngBytes(t,101)
	var partial model.Upload
	decode(t,f.json("POST","/uploads","receiving-cancellation-item",map[string]any{
		"batchId":batch.ID,"inputId":ids["partial.png"],"libraryId":f.library,"mode":"ordinary",
		"fileName":"partial.png","size":len(raw),"mimeType":"image/png",
	},201),&partial)
	prefix:=raw[:17]
	r:=f.request("PUT","/uploads/"+partial.ID+"/content","",testSecret,prefix,
		map[string]string{"Content-Type":"application/octet-stream","Upload-Offset":"0"})
	if r.Code!=200{t.Fatal(r.Body.String())}
	var detail model.BatchDetail
	decode(t,f.json("GET","/batches/"+batch.ID,"",nil,200),&detail)
	body:=map[string]any{"expectedRevision":detail.Batch.Revision}
	f.json("POST","/batches/"+batch.ID+"/cancel","cancel-mixed-inputs",body,200)
	f.json("POST","/batches/"+batch.ID+"/cancel","cancel-mixed-inputs",body,200)
	decode(t,f.json("GET","/batches/"+batch.ID,"",nil,200),&detail)
	if detail.Batch.State!="cancelled"||detail.Batch.ExpectedInputs!=4||len(detail.Inputs)!=4{
		t.Fatalf("whole cancellation lost selection: %+v",detail)
	}
	for i,state:=range []string{"ingested","duplicate","cancelled","cancelled"}{
		if detail.Inputs[i].State!=state{t.Fatalf("input %d reported %s; expected %s",i,detail.Inputs[i].State,state)}
	}
	if detail.Inputs[0].AssetID!=asset.ID||detail.Inputs[1].ErrorCode!="duplicate_content"||
		detail.Inputs[2].UploadID!=partial.ID||detail.Inputs[3].UploadID!=""{
		t.Fatal("cancellation changed existing or missing input identities",detail.Inputs)
	}
	decode(t,f.json("GET","/uploads/"+partial.ID,"",nil,200),&partial)
	if partial.State!="cancelled"||partial.Offset!=int64(len(prefix)){t.Fatal("stranded reservation or changed prefix",partial)}
	file,err:=f.s.Root.Open("uploads/"+partial.ID+".part")
	if err!=nil{t.Fatal(err)}
	got,err:=io.ReadAll(file)
	file.Close()
	if err!=nil||!bytes.Equal(got,prefix){t.Fatal("cancellation modified accepted bytes",err)}
	f.json("POST","/uploads/"+partial.ID+"/complete","closed-item-cannot-complete",struct{}{},409)
	f.json("GET","/assets/"+asset.ID,"",nil,200)
	f.json("DELETE","/libraries/"+f.library+"?expectedRevision=1","detach-cancelled-library",struct{}{},204)
}
