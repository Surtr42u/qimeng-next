package service

import (
 "context"
 "database/sql"
 "errors"
 "time"

 "qimeng.local/next/server/internal/model"
 "qimeng.local/next/server/internal/store"
)

func profileBlock(ctx context.Context,q store.Querier,authorID,block string)(AuthorProfileBlock,error) {
 if !validProfileBlock(block) { return AuthorProfileBlock{},Fail(400,"invalid_profile_block","资料块无效。") }
 value,err:=store.Get[AuthorProfileBlock](ctx,q,"SELECT data FROM author_profile_blocks WHERE author_id=? AND block=?",authorID,block)
 if errors.Is(err,sql.ErrNoRows) {
  return AuthorProfileBlock{AuthorID:authorID,Block:block,Evidence:[]ProfileEvidence{},Source:"empty"},nil
 }
 return value,err
}

func saveProfileBlock(ctx context.Context,q store.Querier,value AuthorProfileBlock) error {
 raw,err:=store.Encode(value);if err!=nil { return err }
 _,err=q.ExecContext(ctx,`INSERT INTO author_profile_blocks(author_id,block,revision,updated_at,data) VALUES(?,?,?,?,?)
 ON CONFLICT(author_id,block) DO UPDATE SET revision=excluded.revision,updated_at=excluded.updated_at,data=excluded.data`,value.AuthorID,value.Block,value.Revision,dateKey(*value.UpdatedAt),raw)
 return err
}

func (s *Service) AuthorProfile(ctx context.Context,p model.Principal,authorID string)(AuthorProfile,error) {
 result:=AuthorProfile{AuthorID:authorID,Blocks:[]AuthorProfileBlock{}}
 tx,err:=s.DB.DB.BeginTx(ctx,&sql.TxOptions{ReadOnly:true});if err!=nil { return result,err };defer tx.Rollback()
 author,err:=store.Get[model.Author](ctx,tx,"SELECT data FROM authors WHERE id=?",authorID);if err!=nil { return result,Normalize(err) }
 result.Editable=p.Has("admin")&&!author.Placeholder
 for _,name:=range profileBlocks {
  block,err:=profileBlock(ctx,tx,authorID,name);if err!=nil { return result,err }
  result.Blocks=append(result.Blocks,block)
 }
 return result,tx.Commit()
}

func (s *Service) AuthorProfileSummary(ctx context.Context,p model.Principal,authorID string)(AuthorProfileSummary,error) {
 profile,err:=s.AuthorProfile(ctx,p,authorID);if err!=nil { return AuthorProfileSummary{},err }
 block:=profile.Blocks[0]
 return AuthorProfileSummary{AuthorID:authorID,XRevision:block.Revision,BiographyOriginal:profileString(block.Values.BiographyOriginal),BiographyZh:profileString(block.Values.BiographyZh),Avatar:block.Avatar},nil
}

func (s *Service) editableProfile(ctx context.Context,p model.Principal,authorID,block string,revision *int64)(AuthorProfileBlock,error) {
 if err:=Authorize(p,"admin","");err!=nil { return AuthorProfileBlock{},err }
 author,err:=s.Author(ctx,authorID);if err!=nil { return AuthorProfileBlock{},err }
 if author.Placeholder { return AuthorProfileBlock{},Fail(409,"placeholder_protected","占位作者没有可确认的个人资料。") }
 if revision==nil || *revision<0 { return AuthorProfileBlock{},Fail(400,"revision_required","必须提供当前资料块修订号。") }
 current,err:=profileBlock(ctx,s.DB.DB,authorID,block);if err!=nil { return current,err }
 if current.Revision!=*revision { return current,Conflict(current.Revision) }
 return current,nil
}

func (s *Service) UpdateAuthorProfile(ctx context.Context,w *Write,authorID,block string,in ProfileWrite)(AuthorProfileBlock,error) {
 current,err:=s.editableProfile(ctx,w.Principal,authorID,block,in.ExpectedRevision);if err!=nil { return current,err }
 count,err:=validateProfileValues(block,in.Values);if err!=nil { return current,err }
 if count==0 && in.Evidence==nil { return current,Fail(400,"changes_required","请提交具体资料字段或证据变更。") }
 if in.Evidence!=nil { if err=validateProfileEvidence(block,*in.Evidence,false);err!=nil { return current,err } }
 candidate:=ProfileCandidate{}
 if in.CandidateID!="" {
  candidate,err=store.Get[ProfileCandidate](ctx,s.DB.DB,"SELECT data FROM author_profile_candidates WHERE id=? AND author_id=? AND archive_id=''",in.CandidateID,authorID)
  if err!=nil { return current,Normalize(err) }
  if candidate.Block!=block || candidate.State!="pending" { return current,Fail(409,"candidate_unavailable","候选不属于本块或已处理，请重新核对。") }
 }
 before:=current.Values
 originalChanged:=in.Values.BiographyOriginal!=nil && *in.Values.BiographyOriginal!=profileString(before.BiographyOriginal)
 accountChanged:=in.Values.ProfileURL!=nil && *in.Values.ProfileURL!=profileString(before.ProfileURL)
 if block=="x" && (originalChanged||accountChanged) {
  if in.Values.BiographyZh!=nil && *in.Values.BiographyZh!="" {
   if !in.TranslationReviewed { return current,Fail(409,"translation_review_required","原文已变化，请核对译文对应关系后明确保存。") }
  } else {
   empty:="";in.Values.BiographyZh=&empty
  }
 }
 current.Values=mergeProfileValues(current.Values,in.Values)
 if profileString(current.Values.BiographyOriginal)=="" && profileString(current.Values.BiographyZh)!="" {
  return current,Fail(400,"translation_without_original","译文必须对应一份非空原文。")
 }
 if in.Evidence!=nil { current.Evidence=*in.Evidence;if current.Evidence==nil { current.Evidence=[]ProfileEvidence{} } }
 oldAvatar:=current.Avatar
 if block=="x" && in.Values.AvatarSourceURL!=nil && *in.Values.AvatarSourceURL!=profileString(before.AvatarSourceURL) { current.Avatar=nil }
 now:=time.Now().UTC();current.Revision++;current.Source="human";current.UpdatedAt=&now;current.CandidateID=in.CandidateID
 q,err:=w.Q();if err!=nil { return current,err }
 if err=saveProfileBlock(ctx,q,current);err!=nil { return current,err }
 if in.CandidateID!="" {
  candidate.State="applied";candidate.AppliedRevision=current.Revision
  if err=saveProfileCandidate(ctx,q,candidate);err!=nil { return current,err }
 }
 if oldAvatar!=nil && current.Avatar==nil { err=pruneProfileAvatar(ctx,q,oldAvatar.ID) }
 return current,err
}

func saveProfileCandidate(ctx context.Context,q store.Querier,candidate ProfileCandidate) error {
 raw,err:=store.Encode(candidate);if err!=nil { return err }
 _,err=q.ExecContext(ctx,`INSERT INTO author_profile_candidates(id,author_id,block,state,created_at,archive_id,data) VALUES(?,?,?,?,?,?,?)
 ON CONFLICT(id) DO UPDATE SET state=excluded.state,archive_id=excluded.archive_id,data=excluded.data`,candidate.ID,candidate.AuthorID,candidate.Block,candidate.State,dateKey(candidate.CreatedAt),candidate.ArchiveID,raw)
 return err
}

func (s *Service) SubmitAuthorProfileCandidate(ctx context.Context,w *Write,authorID string,in ProfileCandidateCreate)(ProfileCandidate,error) {
 var result ProfileCandidate
 count,err:=validateProfileValues(in.Block,in.Values);if err!=nil { return result,err }
 if count==0 { return result,Fail(400,"changes_required","候选必须含具体资料字段。") }
 if err=validateProfileEvidence(in.Block,in.Evidence,true);err!=nil { return result,err }
 grant,err:=s.AuthorCandidateGrant(ctx,w.Principal,authorID,in.Block);if err!=nil { return result,err }
 author,err:=s.Author(ctx,authorID);if err!=nil { return result,err };if author.Placeholder { return result,Fail(409,"placeholder_protected","请先确认作者身份。") }
 if in.BaseRevision==nil || *in.BaseRevision<0 { return result,Fail(400,"revision_required","候选必须基于当前资料块版本。") }
 block,err:=profileBlock(ctx,s.DB.DB,authorID,in.Block);if err!=nil { return result,err }
 if block.Revision!=*in.BaseRevision { return result,Conflict(block.Revision) }
 var pending int
 if err=s.DB.DB.QueryRowContext(ctx,"SELECT COUNT(*) FROM author_profile_candidates WHERE author_id=? AND archive_id='' AND state='pending'",authorID).Scan(&pending);err!=nil { return result,err }
 if pending>=50 { return result,Fail(409,"candidate_queue_full","此作者已有 50 条未处理候选，请先核对。") }
 result=ProfileCandidate{ID:w.ID("profile-candidate"),AuthorID:authorID,Block:in.Block,BaseRevision:block.Revision,Values:in.Values,Evidence:in.Evidence,State:"pending",CreatedAt:time.Now().UTC()}
 if grant!=nil { result.AuthorTaskID=grant.ID }
 q,err:=w.Q();if err!=nil { return result,err }
 if err=saveProfileCandidate(ctx,q,result);err!=nil { return result,err }
 return result,ConsumeAuthorCandidate(ctx,q,grant)
}

func (s *Service) DismissAuthorProfileCandidate(ctx context.Context,w *Write,authorID,id string)(ProfileCandidate,error) {
 var candidate ProfileCandidate
 if err:=Authorize(w.Principal,"admin","");err!=nil { return candidate,err }
 candidate,err:=store.Get[ProfileCandidate](ctx,s.DB.DB,"SELECT data FROM author_profile_candidates WHERE id=? AND author_id=? AND archive_id=''",id,authorID);if err!=nil { return candidate,Normalize(err) }
 if candidate.State=="applied" { return candidate,Fail(409,"candidate_applied","此候选已保存为资料，不能再忽略。") }
 candidate.State="dismissed";q,err:=w.Q();if err!=nil { return candidate,err }
 return candidate,saveProfileCandidate(ctx,q,candidate)
}

func (s *Service) AuthorProfileCandidates(ctx context.Context,authorID,archiveID,cursor string,limit int)(model.Page[ProfileCandidate],error) {
 page:=model.Page[ProfileCandidate]{Items:[]ProfileCandidate{}}
 n,err:=limited(limit,20);if err!=nil { return page,err }
 filter:="profile-candidates:"+authorID+":"+archiveID
 seek,err:=readSeek(cursor,filter,"created_desc");if err!=nil { return page,err }
 if archiveID=="" { if _,err=s.Author(ctx,authorID);err!=nil { return page,err } } else {
  if _,err=s.AuthorProfileArchive(ctx,authorID,archiveID);err!=nil { return page,err }
 }
 query:="SELECT data FROM author_profile_candidates WHERE author_id=? AND archive_id=?"
 args:=[]any{authorID,archiveID}
 // An archive reached through its merge target still contains the source's candidates.
 if archiveID!="" { query="SELECT data FROM author_profile_candidates WHERE archive_id=?";args=[]any{archiveID} }
 if seek.ID!="" { query+=" AND (created_at<? OR (created_at=? AND id<?))";args=append(args,seek.Value,seek.Value,seek.ID) }
 query+=" ORDER BY created_at DESC,id DESC LIMIT ?";args=append(args,n+1)
 page.Items,err=store.List[ProfileCandidate](ctx,s.DB.DB,query,args...);if err!=nil { return page,err }
 if len(page.Items)>n { last:=page.Items[n-1];page.Items=page.Items[:n];page.NextCursor=makeSeek(filter,"created_desc",dateKey(last.CreatedAt),last.ID) }
 return page,nil
}

func pruneProfileAvatar(ctx context.Context,q store.Querier,id string) error {
 _,err:=q.ExecContext(ctx,`DELETE FROM author_profile_avatars WHERE id=?
 AND NOT EXISTS(SELECT 1 FROM author_profile_blocks WHERE json_extract(data,'$.avatar.id')=?)
 AND NOT EXISTS(SELECT 1 FROM author_profile_archive_avatars WHERE avatar_id=?)`,id,id,id)
 return err
}
