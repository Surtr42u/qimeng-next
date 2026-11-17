package migration

import "sort"

type TagState struct{Names []string `json:"names"`;UpdatedAtMillis *int64 `json:"updatedAtMillis,omitempty"`;Source string `json:"source"`;Explicit bool `json:"explicit"`}
type TagDecision struct{Action string `json:"action"`;State TagState `json:"state"`;Reason string `json:"reason,omitempty"`}
func canonicalTags(input []string)[]string{out:=[]string{};seen:=map[string]bool{};for _,name:=range input{if !seen[name]{seen[name]=true;out=append(out,name)}};sort.Strings(out);return out}
func SameTags(left,right []string)bool{a,b:=canonicalTags(left),canonicalTags(right);if len(a)!=len(b){return false};for i:=range a{if a[i]!=b[i]{return false}};return true}
func knownTime(value *int64)bool{return value!=nil&&*value>0}
// ResolveTags never invents an ordering for unknown clocks and never unions
// conflicting sets. A timestamped empty set is as explicit as a nonempty set.
func ResolveTags(current TagState,candidates []TagState)TagDecision{
 selected:=current;fromCurrent:=true
 for _,candidate:=range candidates{
  if !candidate.Explicit{continue}
  if !selected.Explicit{selected=candidate;fromCurrent=false;continue}
  equal:=SameTags(selected.Names,candidate.Names)
  if !knownTime(selected.UpdatedAtMillis)||!knownTime(candidate.UpdatedAtMillis){if !equal{return TagDecision{Action:"conflict",State:current,Reason:"unknown_tag_timestamp"}};if !knownTime(selected.UpdatedAtMillis)&&knownTime(candidate.UpdatedAtMillis){selected=candidate;fromCurrent=false};continue}
  if *candidate.UpdatedAtMillis==*selected.UpdatedAtMillis{if !equal{return TagDecision{Action:"conflict",State:current,Reason:"same_time_different_tags"}};continue}
  if *candidate.UpdatedAtMillis>*selected.UpdatedAtMillis{selected=candidate;fromCurrent=false}
 }
 if !selected.Explicit{return TagDecision{Action:"none",State:current}}
 if fromCurrent&&current.Explicit{return TagDecision{Action:"keep",State:current}}
 return TagDecision{Action:"apply",State:selected}
}
