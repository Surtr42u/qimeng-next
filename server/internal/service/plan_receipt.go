package service

import(
 "context"
 "database/sql"
 "encoding/json"
 "errors"
 "slices"
 "strings"

 "qimeng.local/next/server/internal/model"
 "qimeng.local/next/server/internal/store"
)

func savePublishedPlanItem(ctx context.Context,q store.Querier,j fileIntent)error{
 suffix,ok:=strings.CutPrefix(j.Operation,"commitPlanItem:");if !ok{return nil}
 planID,uploadID,ok:=strings.Cut(suffix,":");if !ok||uploadID!=j.UploadID{return Fail(409,"plan_identity_changed","计划与发布输入身份不匹配。")}
 raw,e:=store.Encode(j.After.AuthorIDs);if e!=nil{return e}
 _,e=q.ExecContext(ctx,`INSERT INTO published_plan_items(upload_id,plan_id,asset_id,content_version,authors_revision,authors) VALUES(?,?,?,?,?,?)`,uploadID,planID,j.After.ID,j.After.ContentVersion,j.After.AuthorsRevision,raw)
 return e
}

func(s *Service)verifyAppliedPlanItem(ctx context.Context,planID string,item model.PlanItem,u model.Upload)error{
 a,e:=s.Asset(ctx,u.AssetID);if e!=nil{return e}
 intended:=item.AuthorIDs;version:=u.PublishedContentVersion
 // TXT and directory authors are derived by the same publication transaction.
 // Compare their committed result, which may legitimately contain more than
 // the plan's editable suggestion. A later human edit must still stop replay.
 var authors string;var revision int64
 e=s.DB.DB.QueryRowContext(ctx,`SELECT authors,content_version,authors_revision FROM published_plan_items WHERE upload_id=? AND plan_id=? AND asset_id=?`,u.ID,planID,a.ID).Scan(&authors,&version,&revision)
 if e!=nil&&!errors.Is(e,sql.ErrNoRows){return e}
 if e==nil{
  if e=json.Unmarshal([]byte(authors),&intended);e!=nil{return e}
 }
 if a.RelativePath!=item.RelativePath||a.Hash!=item.ExpectedHash||a.CollectionID!=item.CollectionID||a.Trashed||a.ContentVersion!=version||a.Page!=item.Page||revision!=0&&a.AuthorsRevision!=revision||!slices.Equal(a.AuthorIDs,intended){
  return Fail(409,"plan_result_changed","已有结果与本计划不一致，不能重复提交覆盖。")
 }
 return nil
}

// The operation receipt records intermediate results, but only completion is
// final. A retry of the same plan/key advances the outstanding item receipts.
func(s *Service)finishPlanAttempt(ctx context.Context,p model.Principal,batch string,plan model.Plan,key string)(Result,error){
 op:="commitPlan:"+plan.ID;hash:=digest([]byte(plan.Fingerprint))
 return s.Run(ctx,p,op,"",nil,func(w *Write)(any,int,error){
  detail,e:=s.BatchDetail(ctx,p,batch);if e!=nil{return nil,0,e}
  remaining:=map[string]bool{};for _,item:=range plan.Items{remaining[item.UploadID]=true}
  for _,item:=range detail.Items{if item.Upload.State=="ingested"{delete(remaining,item.Upload.ID)}}
  plan.State="completed";if len(remaining)>0{plan.State="partial"}
  q,e:=w.Q();if e!=nil{return nil,0,e}
  var previousHash string;var previous []byte;e=q.QueryRowContext(ctx,"SELECT input_hash,response FROM idempotency WHERE principal_id=? AND operation=? AND key=?",p.ID,op,key).Scan(&previousHash,&previous)
  if e==nil{
   if previousHash!=hash{return nil,0,Fail(409,"idempotency_conflict","同一计划操作的输入已改变。")}
   var old model.BatchDetail;if e=json.Unmarshal(previous,&old);e!=nil{return nil,0,e}
   for _,entry:=range old.Plans{if entry.ID==plan.ID&&entry.State=="completed"{return old,200,nil}}
  }else if !errors.Is(e,sql.ErrNoRows){return nil,0,e}
  if e=store.SavePlan(ctx,q,plan);e!=nil{return nil,0,e}
  for i:=range detail.Plans{if detail.Plans[i].ID==plan.ID{detail.Plans[i]=plan.PlanSummary}}
  body,e:=json.Marshal(detail);if e!=nil{return nil,0,e}
  _,e=q.ExecContext(ctx,`INSERT INTO idempotency(principal_id,operation,key,input_hash,response,status) VALUES(?,?,?,?,?,200)
  ON CONFLICT(principal_id,operation,key) DO UPDATE SET response=excluded.response`,p.ID,op,key,hash,body)
  return detail,200,e
 })
}
