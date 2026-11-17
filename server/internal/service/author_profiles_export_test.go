package service

import "testing"

func TestAuthorTextProjectionDoesNotPromoteSavedCandidateAccounts(t *testing.T){
 candidate:="candidate";verified:="verified";current:="https://example.invalid/current";old:="https://example.invalid/old";avatar:="https://example.invalid/avatar.png";list:=[]ProfileAccount{{Platform:"Synthetic",URL:"https://example.invalid/account",PublishingURL:"https://example.invalid/feed"}}
 x:=AuthorProfileBlock{Values:ProfileValues{ProfileURL:&current,AvatarSourceURL:&avatar},Evidence:[]ProfileEvidence{{URL:old,Status:"confirmed"},{URL:"https://example.invalid/candidate",Status:"candidate"}}};accounts:=AuthorProfileBlock{Values:ProfileValues{IdentityStatus:&candidate,Accounts:&list}}
 links:=confirmedProfileLinks(x,AuthorProfileBlock{},accounts);if len(links)!=1||links[0]!=old{t.Fatal("saved candidate identity or avatar became confirmed source")}
 x.Evidence=append(x.Evidence,ProfileEvidence{URL:current,Status:"confirmed"});links=confirmedProfileLinks(x,AuthorProfileBlock{},accounts);if len(links)!=2||links[0]!=current{t.Fatal("matching confirmed X evidence was not projected first")}
 accounts.Values.IdentityStatus=&verified;links=confirmedProfileLinks(x,AuthorProfileBlock{},accounts);if len(links)!=4||links[1]!=list[0].URL||links[2]!=list[0].PublishingURL{t.Fatal("verified account ordering or deduplication changed")}
}
