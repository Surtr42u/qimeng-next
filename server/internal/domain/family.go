package domain

import "strings"

// NamingFamily separates canonical display names from confirmed disk spelling.
// Identity lookup and evidence decisions stay outside this pure formatter.
func NamingFamily(source string,characters []string,fallback string)string{
 sources:=strings.Split(source,"+");for i,name:=range sources{switch name{case "死或生","生或死5":sources[i]="生或死"}}
 work:=strings.Join(sources,"+");if work!=""&&len(characters)>0{return work+"  "+strings.Join(characters,"&")};if work!=""{return work};return strings.TrimSpace(fallback)
}
