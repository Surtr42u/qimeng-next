package service

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"sync"
	"time"

	"qimeng.local/next/server/internal/researchprovider"
)

type providerFlight struct{credential,delegation,task,library string;stop context.CancelFunc}
type publicResearchGateway struct{
	slots chan struct{}
	mu sync.Mutex
	inFlight map[string]providerFlight
	client *http.Client
}
func(g *publicResearchGateway)init(){
	g.slots=make(chan struct{},2)
	g.inFlight=make(map[string]providerFlight)
	g.client=avatarHTTPClient(defaultAvatarNetwork())
	// Even another public host has not been approved for this adapter's query.
	g.client.CheckRedirect=func(*http.Request,[]*http.Request)error{return http.ErrUseLastResponse}
}
func(g *publicResearchGateway)cancel(credential,delegation string){
	g.mu.Lock();defer g.mu.Unlock()
	for _,f:=range g.inFlight{if credential!=""&&credential==f.credential||delegation!=""&&delegation==f.delegation{f.stop()}}
}
func(g *publicResearchGateway)cancelSubject(task,library string){
	g.mu.Lock();defer g.mu.Unlock()
	for _,f:=range g.inFlight{if task!=""&&task==f.task||library!=""&&library==f.library{f.stop()}}
}
func(g *publicResearchGateway)fetch(ctx context.Context,q researchprovider.Query)(string,[]researchprovider.Item){
	var body io.Reader
	if q.Form!=""{body=strings.NewReader(q.Form)}
	request,err:=http.NewRequestWithContext(ctx,q.Method,q.URL,body)
	if err!=nil{return "unavailable",nil}
	request.Header.Set("User-Agent","QimengNext/0.1 (public metadata research)")
	request.Header.Set("Accept","application/json")
	if q.Name=="bing"{request.Header.Set("Accept","text/html")}
	if q.Referer!=""{request.Header.Set("Referer",q.Referer)}
	if q.Form!=""{request.Header.Set("Content-Type","application/x-www-form-urlencoded")}
	response,err:=g.client.Do(request)
	if err!=nil{if errors.Is(ctx.Err(),context.Canceled){return "interrupted",nil};return "unavailable",nil}
	defer response.Body.Close()
	if state:=researchprovider.HTTPState(response.StatusCode);state!="ok"{return state,nil}
	const maxBytes=2<<20
	encoding:=response.Header.Get("Content-Encoding")
	if response.ContentLength>maxBytes||encoding!=""&&!strings.EqualFold(encoding,"identity"){return "unavailable",nil}
	media,_,err:=mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err!=nil||q.Name=="bing"&&media!="text/html"||q.Name!="bing"&&media!="application/json"&&!strings.HasSuffix(media,"+json"){return "unavailable",nil}
	raw,err:=io.ReadAll(io.LimitReader(response.Body,maxBytes+1))
	if err!=nil||len(raw)>maxBytes{if errors.Is(ctx.Err(),context.Canceled){return "interrupted",nil};return "unavailable",nil}
	return researchprovider.Parse(q,raw)
}

func(g *publicResearchGateway)register(id string,f providerFlight){
	g.mu.Lock();defer g.mu.Unlock()
	g.inFlight[id]=f
}
func(g *publicResearchGateway)finish(id string){
	g.mu.Lock();defer g.mu.Unlock()
	delete(g.inFlight,id)
}

type PublicResearchInput struct{
	Operation string `json:"operation"`
	Target string `json:"target"`
	Limit int `json:"limit,omitempty"`
	UploadID string `json:"uploadId,omitempty"`
	ExpectedHash string `json:"expectedHash,omitempty"`
}
type PublicResearchResult struct{
	ID string `json:"id"`
	TaskID string `json:"taskId"`
	SubjectID string `json:"subjectId"`
	SubjectHash string `json:"subjectHash,omitempty"`
	SubjectRevision int64 `json:"subjectRevision,omitempty"`
	ContentVersion int64 `json:"contentVersion,omitempty"`
	Provider string `json:"provider"`
	Operation string `json:"operation"`
	State string `json:"state"`
	EvidenceKind string `json:"evidenceKind"`
	Items []researchprovider.Item `json:"items"`
	StartedAt time.Time `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
}
type PublicResearchContext struct{
	Enabled bool `json:"enabled"`
	Providers []researchprovider.Info `json:"providers"`
	AllowedProviders []string `json:"allowedProviders"`
	TaskID string `json:"taskId,omitempty"`
	RemainingResearchCalls *int `json:"remainingResearchCalls,omitempty"`
	Queries []PublicResearchRef `json:"queries,omitempty"`
}
type PublicResearchRef struct{
	ID string `json:"id"`
	SubjectID string `json:"subjectId"`
	Provider string `json:"provider"`
	Operation string `json:"operation"`
	State string `json:"state"`
}
