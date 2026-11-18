package service

import(
 "bytes"
 "context"
 "encoding/xml"
 "errors"
 "io"
 "net/http"
 "net/url"
 "strings"
 "time"
 "unicode/utf8"
)

const maxAuthorFeedBytes=1<<20
const atomNamespace="http://www.w3.org/2005/Atom"

type workFeedEntry struct{
 Title string `xml:"title"`
 Links []struct{Href string `xml:"href,attr"`;Rel string `xml:"rel,attr"`;Text string `xml:",chardata"`} `xml:"link"`
 GUID string `xml:"guid"`
 Published string `xml:"published"`
 PubDate string `xml:"pubDate"`
 Updated string `xml:"updated"`
}
func workPageURL(base *url.URL,value string)string{
 if value==""||len(value)>4096{return ""}
 u,e:=url.Parse(strings.TrimSpace(value));if e!=nil{return ""};u=base.ResolveReference(u)
 clean:=*u;clean.Fragment="";if _,e=validateAvatarURL(clean.String());e!=nil{return ""}
 return u.String()
}
func feedDate(candidates ...string)*time.Time{
 for _,raw:=range candidates{
  for _,layout:=range []string{time.RFC3339Nano,time.RFC1123Z,time.RFC1123,time.RFC822Z,time.RFC822,time.RFC850}{
   if at,e:=time.Parse(layout,strings.TrimSpace(raw));e==nil{utc:=at.UTC();return &utc}
  }
 }
 return nil
}
func parseAuthorFeed(body []byte,base *url.URL)([]AuthorUpdateEntry,error){
 entries:=[]AuthorUpdateEntry{};seen:=map[string]bool{}
 invalid:=func()error{return Fail(422,"invalid_works_feed","订阅地址须返回有效的 RSS 2.0 或 Atom 作品源。")}
 if len(body)>maxAuthorFeedBytes||!utf8.Valid(body){return nil,invalid()}
 decoder:=xml.NewDecoder(bytes.NewReader(body));stack:=[]string{};format:="";channel:=false;closed:=false
 for{
  token,e:=decoder.Token();if e==io.EOF{break};if e!=nil{return nil,invalid()}
  switch t:=token.(type){
  case xml.Directive:return nil,invalid()
  case xml.CharData:if len(stack)==0&&len(bytes.TrimSpace(t))!=0{return nil,invalid()}
  case xml.StartElement:
   if closed||len(stack)>=64{return nil,invalid()}
   if len(stack)==0{
    switch{case t.Name.Local=="rss"&&t.Name.Space=="":format="rss";case t.Name.Local=="feed"&&t.Name.Space==atomNamespace:format="atom";default:return nil,invalid()}
   }
   if format=="rss"&&len(stack)==1&&t.Name.Local=="channel"&&t.Name.Space==""{channel=true}
   rss:=format=="rss"&&len(stack)==2&&stack[1]=="channel"&&t.Name.Local=="item"&&t.Name.Space==""
   atom:=format=="atom"&&len(stack)==1&&t.Name.Local=="entry"&&t.Name.Space==atomNamespace
   if !rss&&!atom{stack=append(stack,t.Name.Local);continue}
   if len(entries)==30{if e=decoder.Skip();e!=nil{return nil,invalid()};continue}
   var raw workFeedEntry;if e=decoder.DecodeElement(&raw,&t);e!=nil{return nil,invalid()}
   link:="";for _,candidate:=range raw.Links{
    if rss{link=workPageURL(base,candidate.Text)}else if candidate.Rel==""||candidate.Rel=="alternate"{link=workPageURL(base,candidate.Href)}
    if link!=""{break}
   }
   if rss&&link==""&&strings.HasPrefix(raw.GUID,"https://"){link=workPageURL(base,raw.GUID)}
   title:=strings.Join(strings.Fields(raw.Title)," ");if link==""||title==""||seen[link]{continue}
   if runes:=[]rune(title);len(runes)>500{title=string(runes[:500])}
   seen[link]=true;entries=append(entries,AuthorUpdateEntry{ID:digest([]byte(link))[:32],Title:title,URL:link,PublishedAt:feedDate(raw.Published,raw.PubDate,raw.Updated)})
  case xml.EndElement:
   if len(stack)==0{return nil,invalid()};stack=stack[:len(stack)-1];if len(stack)==0{closed=true}
  }
 }
 if !closed||format=="rss"&&!channel{return nil,invalid()}
 return entries,nil
}
func fetchAuthorEntries(ctx context.Context,source string)([]AuthorUpdateEntry,error){
 return fetchAuthorFeed(ctx,source,avatarHTTPClient(defaultAvatarNetwork()))
}
func fetchAuthorFeed(ctx context.Context,source string,client *http.Client)([]AuthorUpdateEntry,error){
 u,e:=validateAvatarURL(source);if e!=nil{return nil,Fail(400,"invalid_feed_url","订阅源须为 HTTPS 公网地址。")}
 request,e:=http.NewRequestWithContext(ctx,http.MethodGet,u.String(),nil);if e!=nil{return nil,e}
 request.Header.Set("Accept","application/rss+xml, application/atom+xml, application/xml, text/xml");request.Header.Set("User-Agent","Qimeng-Works-Feed/1")
 response,e:=client.Do(request);if e!=nil{
  if errors.Is(e,context.DeadlineExceeded)||errors.Is(e,context.Canceled){return nil,Fail(504,"feed_timeout","订阅读取超时或已取消。")}
  return nil,Fail(502,"feed_fetch_failed","无法安全读取已保存的公开订阅源。")
 };defer response.Body.Close()
 if response.StatusCode!=200{return nil,Fail(502,"feed_http_status","订阅源没有返回可读取的作品列表。")}
 if encoding:=response.Header.Get("Content-Encoding");encoding!=""&&encoding!="identity"{return nil,Fail(422,"feed_encoding_denied","订阅源返回了不支持的编码。")}
 if response.ContentLength>maxAuthorFeedBytes{return nil,Fail(413,"feed_too_large","订阅正文最多 1 MiB。")}
 body,e:=io.ReadAll(io.LimitReader(response.Body,maxAuthorFeedBytes+1));if e!=nil{return nil,Fail(502,"feed_read_failed","读取订阅时中断。")}
 if len(body)>maxAuthorFeedBytes{return nil,Fail(413,"feed_too_large","订阅正文最多 1 MiB。")}
 return parseAuthorFeed(body,response.Request.URL)
}
