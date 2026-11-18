package service

import(
 "context"
 "encoding/json"
 "fmt"
 "net/url"
 "strings"
 "testing"

 "qimeng.local/next/server/internal/model"
)

func TestExplicitFeedParsingUsesPublishedItemsAndBoundedDistinctLinks(t *testing.T){
 base,_:=url.Parse("https://example.org/feed/main.xml")
 rss:="<rss version='2.0'><channel><item><title>Work A</title><link>../art/a</link><pubDate>Fri, 09 Oct 2026 16:01:02 +0000</pubDate></item><item><title>Duplicate</title><guid>https://example.org/art/a</guid></item><item><title>Only an enclosure</title><enclosure url='https://example.org/video.mp4'/></item></channel></rss>"
 works,e:=parseAuthorFeed([]byte(rss),base);if e!=nil||len(works)!=1||works[0].URL!="https://example.org/art/a"||works[0].PublishedAt==nil{t.Fatal("RSS work metadata changed",works,e)}
 var atom strings.Builder;atom.WriteString("<feed xmlns='http://www.w3.org/2005/Atom'>")
 for i:=0;i<40;i++{fmt.Fprintf(&atom,"<entry><title>Entry %d</title><link rel='enclosure' href='https://example.org/content.mp4'/><link rel='alternate' href='/posts/%d'/></entry>",i,i)}
 atom.WriteString("</feed>");works,e=parseAuthorFeed([]byte(atom.String()),base);if e!=nil||len(works)!=30||works[29].URL!="https://example.org/posts/29"{t.Fatal("Atom output did not respect article links or row limit",e)}
 if _,e=parseAuthorFeed([]byte("<html><a href='/media'>ordinary homepage</a></html>"),base);e==nil{t.Fatal("regular publishing page was reported as an RSS feed")}
}
func TestManualFeedCheckRechecksRevisionAndReplaysWithoutRefetching(t *testing.T){
 s,e:=New(Config{DataDir:t.TempDir(),AdminToken:"synthetic-author-feed-owner-token-000",ReserveBytes:1});if e!=nil{t.Fatal(e)};defer s.Close()
 ctx:=context.Background();p:=model.Principal{ID:"owner",Scopes:[]string{"admin"}}
 created:=testMutation(t,s,"create-feed-test-author",func(w *Write)(any,int,error){v,e:=s.CreateAuthor(ctx,w,AuthorInput{Name:"Synthetic feed creator"});return v,201,e})
 var author model.Author;if e=json.Unmarshal(created.Body,&author);e!=nil{t.Fatal(e)}
 verified:="verified"
 testMutation(t,s,"verify-feed-test-author",func(w *Write)(any,int,error){rev:=int64(0);v,e:=s.UpdateAuthorProfile(ctx,w,author.ID,"accounts",ProfileWrite{ExpectedRevision:&rev,Values:ProfileValues{IdentityStatus:&verified}});return v,200,e})
 apply:=func(enable bool,rev int64,source string,key string){t.Helper();testMutation(t,s,key,func(w *Write)(any,int,error){v,e:=s.SetAuthorUpdates(ctx,w,author.ID,AuthorUpdatesInput{ExpectedRevision:&rev,TrackUpdates:&enable,FeedURL:&source});return v,200,e})}
 apply(true,0,"https://example.org/rss","enable-works-feed")
 calls:=0;fetch:=func(context.Context,string)([]AuthorUpdateEntry,error){calls++;return []AuthorUpdateEntry{{ID:"a",Title:"Work A",URL:"https://example.org/post/a"}},nil}
 in:=AuthorUpdateCheck{ExpectedRevision:1};raw:=[]byte("revision-one")
 result,e:=s.checkAuthorUpdates(ctx,p,author.ID,"check-same-feed-once",raw,in,fetch);if e!=nil{t.Fatal(e)};var first AuthorUpdates;e=json.Unmarshal(result.Body,&first);if e!=nil||first.Revision!=2||len(first.Items)!=1{t.Fatal(first,e)}
 if _,e=s.checkAuthorUpdates(ctx,p,author.ID,"check-same-feed-once",raw,in,fetch);e!=nil||calls!=1{t.Fatal("replay refetched external content",e)}
 _,e=s.checkAuthorUpdates(ctx,p,author.ID,"delayed-feed-owner-check",[]byte("revision-two"),AuthorUpdateCheck{ExpectedRevision:2},func(context.Context,string)([]AuthorUpdateEntry,error){
  apply(false,2,"https://example.org/rss","disable-during-feed-fetch");return first.Items,nil
 });if e==nil{t.Fatal("late feed response resurrected disabled entries")}
 current,e:=s.AuthorUpdates(ctx,author.ID);if e!=nil||current.TrackUpdates||len(current.Items)!=0||current.CheckedAt!=nil{t.Fatal("disabled feed still displayed entries",e)}
}
