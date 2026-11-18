package service

import "context"

func(s *Service)checkPublishedInput(ctx context.Context,batchID string,item ProtectedInput)error{
 var verified bool
 e:=s.DB.DB.QueryRowContext(ctx,`SELECT EXISTS(
  SELECT 1 FROM batch_inputs i JOIN uploads u ON u.id=i.upload_id AND u.batch_id=i.batch_id
  WHERE i.batch_id=? AND i.input_id=? AND u.id=? AND u.state='ingested'
   AND json_extract(u.data,'$.publishedContentVersion')=? AND ?>0
   AND json_extract(u.data,'$.assetId')=? AND json_extract(u.data,'$.hash')=? AND json_extract(u.data,'$.size')=?)`,
  batchID,item.InputID,item.UploadID,item.ContentVersion,item.ContentVersion,item.AssetID,item.Hash,item.Size).Scan(&verified)
 if e!=nil{return e};if !verified{return Fail(409,"input_version_unverified","此保护项缺少原始上传对应版本的入库证据，来源应保留。")};return nil
}
