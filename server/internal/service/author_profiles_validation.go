package service

import (
 "net/url"
 "strings"
 "unicode/utf8"
)

var profileBlocks = []string{"x", "basic", "accounts"}

func validProfileBlock(block string) bool {
 return block == "x" || block == "basic" || block == "accounts"
}

func profileString(value *string) string {
 if value == nil { return "" }
 return *value
}

func profileText(value string, limit int) bool {
 return utf8.ValidString(value) && utf8.RuneCountInString(value) <= limit && !strings.ContainsRune(value, '\x00')
}

// Links are stored as evidence, never fetched by this function. The avatar
// fetcher has a separate, stricter public-network policy.
func profileLink(value string) bool {
 if value == "" { return true }
 if !profileText(value,4096) { return false }
 u,err:=url.Parse(value)
 return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Hostname() != "" && u.User == nil && u.Opaque == ""
}

func profileFields(values ProfileValues) map[string]bool {
 return map[string]bool{
  "handle": values.Handle != nil, "displayName": values.DisplayName != nil,
  "profileUrl": values.ProfileURL != nil,
  "biographyOriginal": values.BiographyOriginal != nil, "biographyZh": values.BiographyZh != nil,
  "avatarSourceUrl": values.AvatarSourceURL != nil, "summary": values.Summary != nil,
  "note": values.Note != nil, "accounts": values.Accounts != nil,
  "identityStatus": values.IdentityStatus != nil,
  "publicName": values.PublicName != nil, "remark": values.Remark != nil,
 }
}

func fieldBelongs(block, field string) bool {
 switch block {
 case "x": return field == "handle" || field == "profileUrl" || field == "displayName" || field == "biographyOriginal" || field == "biographyZh" || field == "avatarSourceUrl"
 case "basic": return field == "publicName" || field == "summary" || field == "remark" || field == "note"
 case "accounts": return field == "accounts" || field == "identityStatus"
 }
 return false
}

func validateProfileValues(block string, values ProfileValues) (int,error) {
 if !validProfileBlock(block) { return 0,Fail(400,"invalid_profile_block","资料块无效。") }
 count:=0
 for field,present:=range profileFields(values) {
  if !present { continue }
  if !fieldBelongs(block,field) { return 0,Fail(400,"wrong_profile_block","字段不属于当前资料块。") }
  count++
 }
 for _,item:=range []struct{value *string; limit int}{
  {values.Handle,200},{values.DisplayName,200},{values.BiographyOriginal,20000},
  {values.BiographyZh,20000},{values.AvatarSourceURL,4096},{values.Summary,20000},{values.Note,10000},
  {values.ProfileURL,4096},{values.PublicName,200},{values.Remark,500},
 } { if item.value != nil && !profileText(*item.value,item.limit) { return 0,Fail(400,"invalid_profile_text","资料文本超过限制或包含无效字符。") } }
 if values.AvatarSourceURL != nil && *values.AvatarSourceURL != "" {
  if _,err:=validateAvatarURL(*values.AvatarSourceURL);err!=nil { return 0,err }
 }
 if values.ProfileURL!=nil && !profileLink(*values.ProfileURL) { return 0,Fail(400,"invalid_profile_url","账号主页网址无效。") }
 if values.Accounts != nil {
  if len(*values.Accounts)>32 { return 0,Fail(400,"account_limit","最多保存 32 个公开账号。") }
  for _,account:=range *values.Accounts {
   if strings.TrimSpace(account.Platform)=="" || !profileText(account.Platform,80) || !profileText(account.Identifier,200) || !profileLink(account.URL) || !profileLink(account.PublishingURL) {
    return 0,Fail(400,"invalid_profile_account","账号平台、标识或公开网址无效。")
   }
  }
 }
 if values.IdentityStatus!=nil && *values.IdentityStatus!="empty" && *values.IdentityStatus!="candidate" && *values.IdentityStatus!="verified" { return 0,Fail(400,"invalid_identity_status","身份核实状态无效。") }
 return count,nil
}

func validateProfileEvidence(block string,evidence []ProfileEvidence,candidate bool) error {
 if len(evidence)>20 || candidate && len(evidence)==0 { return Fail(400,"evidence_limit","候选须有来源证据，每块最多 20 条。") }
 for _,item:=range evidence {
  if item.URL=="" || !profileLink(item.URL) || !profileText(item.Title,400) || !profileText(item.Note,4000) || len(item.Fields)==0 || len(item.Fields)>8 {
   return Fail(400,"invalid_profile_evidence","证据需要有效的公开网址和对应字段。")
  }
  if item.Status!="unverified" && item.Status!="candidate" && item.Status!="confirmed" { return Fail(400,"invalid_evidence_status","证据核实状态无效。") }
  if candidate && item.Status=="confirmed" { return Fail(400,"candidate_not_confirmed","候选证据不能冒充人工已确认资料。") }
  for _,field:=range item.Fields { if !fieldBelongs(block,field) { return Fail(400,"invalid_evidence_field","证据字段不属于此资料块。") } }
 }
 return nil
}

func mergeProfileValues(current,patch ProfileValues) ProfileValues {
 if patch.Handle!=nil { current.Handle=patch.Handle }
 if patch.ProfileURL!=nil { current.ProfileURL=patch.ProfileURL }
 if patch.DisplayName!=nil { current.DisplayName=patch.DisplayName }
 if patch.BiographyOriginal!=nil { current.BiographyOriginal=patch.BiographyOriginal }
 if patch.BiographyZh!=nil { current.BiographyZh=patch.BiographyZh }
 if patch.AvatarSourceURL!=nil { current.AvatarSourceURL=patch.AvatarSourceURL }
 if patch.Summary!=nil { current.Summary=patch.Summary }
 if patch.PublicName!=nil { current.PublicName=patch.PublicName }
 if patch.Remark!=nil { current.Remark=patch.Remark }
 if patch.Note!=nil { current.Note=patch.Note }
 if patch.Accounts!=nil { current.Accounts=patch.Accounts }
 if patch.IdentityStatus!=nil { current.IdentityStatus=patch.IdentityStatus }
 return current
}
