package sysmon

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTrafficCountsConsumedBodyAndWrittenRangeOnly(t *testing.T){
	var counter Traffic
	handler:=counter.Handler(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
		if r.Method=="POST"{part:=make([]byte,2);_,_=io.ReadFull(r.Body,part);w.WriteHeader(201);_,_=w.Write([]byte("saved"));return}
		if r.URL.Path=="/same"{w.WriteHeader(304);return}
		http.ServeContent(w,r,"fixture.bin",time.Time{},strings.NewReader("0123456789"))
	}))
	request:=httptest.NewRequest("POST","/input",strings.NewReader("only two bytes count"))
	handler.ServeHTTP(httptest.NewRecorder(),request)
	get:=httptest.NewRequest("GET","/original",nil);get.Header.Set("Range","bytes=2-4");handler.ServeHTTP(httptest.NewRecorder(),get)
	handler.ServeHTTP(httptest.NewRecorder(),httptest.NewRequest("HEAD","/original",nil))
	handler.ServeHTTP(httptest.NewRecorder(),httptest.NewRequest("GET","/same",nil))
	received,sent,requests:=counter.Counts();if received!=2||sent!=8||requests!=4{t.Fatalf("body traffic and request counts: %d %d %d",received,sent,requests)}
}
