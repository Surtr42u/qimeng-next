// Package migration validates the retired application's current v2 interchange
// format. It does not read live legacy databases or reinterpret aggregate stats
// as events. The service retains the original input bytes alongside the plan.
package migration

import (
 "encoding/json"
 "fmt"
 "strings"
 "time"
)

const MaxInputBytes=64<<20
type V2 struct{Format string `json:"format"`;SchemaVersion int `json:"schemaVersion"`;AppIdentifier string `json:"appIdentifier"`;ExportedAtMillis int64 `json:"exportedAtMillis"`;Data V2Data `json:"data"`}
type V2Data struct{
 MediaFiles []Media `json:"mediaFiles"`
 ViewEvents []Event `json:"viewEvents"`
 Authors []Author `json:"authors"`
 AuthorMediaRefs []AuthorRef `json:"authorMediaRefs"`
 Tags []Tag `json:"tags"`
 MediaTagRefs []TagRef `json:"mediaTagRefs"`
 Likes []Like `json:"likes"`
 Favorites []string `json:"favorites"`
 FollowedAuthorIDs []string `json:"followedAuthorIds"`
 TimelineTags []Timeline `json:"timelineTags"`
 TxtFragments []Fragment `json:"txtFragments"`
 AppPrefs json.RawMessage `json:"appPrefs"`
 Settings []Setting `json:"settings"`
}
type Media struct{RecordKey string `json:"recordKey"`;FileName string `json:"fileName"`;AssetID string `json:"assetId"`;LibraryName string `json:"libraryName"`;RelativePath string `json:"relPath"`;MediaType string `json:"mediaType"`;SizeBytes int64 `json:"sizeBytes"`;ModifiedAtMillis int64 `json:"modifiedAtMillis"`;IndexedAtMillis int64 `json:"indexedAtMillis"`;TagsUpdatedAtMillis *int64 `json:"tagsUpdatedAtMillis"`;IsCOS bool `json:"isCosFile"`}
type Event struct{EventID string `json:"eventId"`;RecordKey string `json:"recordKey"`;Kind string `json:"kind"`;SessionID string `json:"sessionId"`;StartedAt string `json:"startedAt"`;Seconds *int64 `json:"seconds"`;Day string `json:"day"`}
func(e *Event)UnmarshalJSON(raw []byte)error{type plain Event;var fields map[string]json.RawMessage;if err:=json.Unmarshal(raw,&fields);err!=nil{return err};for _,name:=range []string{"eventId","recordKey","kind","sessionId","startedAt","seconds"}{value,ok:=fields[name];if !ok||(name!="seconds"&&string(value)=="null"){return bad("event."+name,"required field is missing")}};var value plain;if err:=json.Unmarshal(raw,&value);err!=nil{return err};*e=Event(value);return nil}
type Author struct{AuthorID string `json:"authorId"`;DisplayName string `json:"displayName"`;CreatedAtMillis int64 `json:"createdAtMillis"`;UpdatedAtMillis int64 `json:"updatedAtMillis"`;Origin *string `json:"origin"`;ProfileJSON string `json:"profileJson"`;AvatarData string `json:"avatarData"`}
type AuthorRef struct{AuthorID string `json:"authorId"`;RecordKey string `json:"recordKey"`;FileName string `json:"fileName"`;CreatedAtMillis *int64 `json:"createdAtMillis"`;Origin *string `json:"origin"`}
type Tag struct{Name string `json:"name"`;CreatedAtMillis int64 `json:"createdAtMillis"`}
type TagRef struct{RecordKey string `json:"recordKey"`;TagName string `json:"tagName"`;CreatedAtMillis *int64 `json:"createdAtMillis"`;Origin *string `json:"origin"`}
type Like struct{RecordKey string `json:"recordKey"`;LikeCount int64 `json:"likeCount"`;LastLikeDate *string `json:"lastLikeDate"`}
type Timeline struct{RecordKey string `json:"recordKey"`;FileName string `json:"fileName"`;TimeMillis int64 `json:"timeMillis"`;Name string `json:"name"`;Color string `json:"color"`;CreatedAtMillis int64 `json:"createdAtMillis"`}
type Fragment struct{Filename string `json:"filename"`;Content string `json:"content"`;ImportedAtMillis int64 `json:"importedAtMillis"`}
type Setting struct{Key string `json:"key"`;Value string `json:"value"`;UpdatedAtMillis int64 `json:"updatedAtMillis"`}
type Invalid struct{Field string;Reason string}
func(e *Invalid)Error()string{return e.Field+": "+e.Reason}
func bad(field,reason string)error{return &Invalid{field,reason}}
func DecodeV2(raw []byte)(V2,error){
 var input V2;if len(raw)>MaxInputBytes{return input,bad("input","exceeds 64 MiB")};if e:=json.Unmarshal(raw,&input);e!=nil{return input,bad("input","invalid JSON")};if input.Format!="qimeng_backup"||input.SchemaVersion!=2{return input,bad("schemaVersion","only qimeng_backup v2 is accepted")};if input.AppIdentifier==""{return input,bad("appIdentifier","is required")};if input.Data.ViewEvents==nil{return input,bad("data.viewEvents","must be explicitly present, including an empty array")}
 media:=map[string]bool{};for i,m:=range input.Data.MediaFiles{field:=fmt.Sprintf("data.mediaFiles[%d]",i);if m.RecordKey==""||len(m.RecordKey)>4096||media[m.RecordKey]{return input,bad(field,"recordKey is missing or duplicated")};if m.FileName==""||len(m.FileName)>1024||m.SizeBytes<0{return input,bad(field,"invalid media facts")};if m.MediaType!="image"&&m.MediaType!="animated_image"&&m.MediaType!="video"{return input,bad(field,"invalid media type")};media[m.RecordKey]=true}
 events:=map[string]bool{};for i,event:=range input.Data.ViewEvents{field:=fmt.Sprintf("data.viewEvents[%d]",i);if event.EventID==""||len(event.EventID)>4096||events[event.EventID]||!media[event.RecordKey]{return input,bad(field,"event identity is missing, duplicated, or refers to unknown media")};if event.Kind!="open"&&event.Kind!="play"&&event.Kind!="dwell"{return input,bad(field,"invalid event kind")};if _,e:=time.Parse(time.RFC3339Nano,event.StartedAt);e!=nil{return input,bad(field,"invalid original event timestamp")};if event.Seconds!=nil&&*event.Seconds<0{return input,bad(field,"negative duration")};if event.Day!=""{if _,e:=time.Parse("2006-01-02",event.Day);e!=nil{return input,bad(field,"invalid original day key")}};events[event.EventID]=true}
 authors:=map[string]bool{};for i,author:=range input.Data.Authors{field:=fmt.Sprintf("data.authors[%d]",i);if author.AuthorID==""||authors[author.AuthorID]||strings.TrimSpace(author.DisplayName)==""{return input,bad(field,"author identity is missing or duplicated")};if len(author.ProfileJSON)>262144||len(author.AvatarData)>2796204{return input,bad(field,"profile or avatar exceeds v2 limits")};if author.ProfileJSON!=""{var profile map[string]json.RawMessage;if json.Unmarshal([]byte(author.ProfileJSON),&profile)!=nil||profile==nil{return input,bad(field,"profileJson must contain an object")}};if author.AvatarData!=""&&author.ProfileJSON==""{return input,bad(field,"offline avatar must be bound to profileJson")};authors[author.AuthorID]=true}
 for _,ref:=range input.Data.AuthorMediaRefs{if !media[ref.RecordKey]||!authors[ref.AuthorID]{return input,bad("data.authorMediaRefs","unresolved reference")}}
 for _,ref:=range input.Data.MediaTagRefs{if !media[ref.RecordKey]||strings.TrimSpace(ref.TagName)==""||len(strings.TrimSpace(ref.TagName))>100||strings.ContainsRune(ref.TagName,'\x00'){return input,bad("data.mediaTagRefs","unresolved or invalid reference")}}
 for _,tag:=range input.Data.Tags{if strings.TrimSpace(tag.Name)==""||len(strings.TrimSpace(tag.Name))>100||strings.ContainsRune(tag.Name,'\x00'){return input,bad("data.tags","invalid vocabulary name")}}
 for _,record:=range input.Data.Favorites{if !media[record]{return input,bad("data.favorites","unresolved reference")}}
 for _,id:=range input.Data.FollowedAuthorIDs{if !authors[id]{return input,bad("data.followedAuthorIds","unresolved reference")}}
 likeKeys:=map[string]bool{};for _,like:=range input.Data.Likes{if !media[like.RecordKey]||like.LikeCount<0||likeKeys[like.RecordKey]{return input,bad("data.likes","invalid or duplicated reference or count")};likeKeys[like.RecordKey]=true;if like.LastLikeDate!=nil&&*like.LastLikeDate!=""{if _,e:=time.Parse("2006-01-02",*like.LastLikeDate);e!=nil{return input,bad("data.likes","invalid last like date")}}}
 for _,mark:=range input.Data.TimelineTags{if !media[mark.RecordKey]||mark.TimeMillis<0||strings.TrimSpace(mark.Name)==""||len(mark.Name)>200{return input,bad("data.timelineTags","invalid reference or timestamp")}}
 for _,fragment:=range input.Data.TxtFragments{if fragment.Filename==""||len(fragment.Filename)>512||strings.ContainsAny(fragment.Filename,"/\\:\x00")||len(fragment.Content)>8<<20{return input,bad("data.txtFragments","invalid fragment name or size")}}
 return input,nil
}
