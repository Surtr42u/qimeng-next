// Package sysmon samples this service and its host. It has no access to library
// metadata, user credentials or application request content.
package sysmon

import (
	"io"
	"net/http"
	"sync/atomic"
)

type Traffic struct{ received, sent, requests atomic.Uint64 }
func(t *Traffic) Counts()(uint64,uint64,uint64){return t.received.Load(),t.sent.Load(),t.requests.Load()}

type meteredBody struct{io.ReadCloser;bytes *atomic.Uint64}
func(b *meteredBody)Read(p []byte)(int,error){n,err:=b.ReadCloser.Read(p);b.bytes.Add(uint64(n));return n,err}
type meteredWriter struct{http.ResponseWriter;bytes *atomic.Uint64;head bool;status int}
func(w *meteredWriter)Unwrap()http.ResponseWriter{return w.ResponseWriter}
func(w *meteredWriter)WriteHeader(status int){
	if w.status==0&&status>=200{w.status=status}
	w.ResponseWriter.WriteHeader(status)
}
func(w *meteredWriter)count(n int64){if !w.head&&w.status!=http.StatusNoContent&&w.status!=http.StatusNotModified{w.bytes.Add(uint64(n))}}
func(w *meteredWriter)Write(p []byte)(int,error){
	if w.status==0{w.status=http.StatusOK}
	n,err:=w.ResponseWriter.Write(p);w.count(int64(n));return n,err
}
func(w *meteredWriter)ReadFrom(r io.Reader)(int64,error){
	if w.status==0{w.status=http.StatusOK}
	if rf,ok:=w.ResponseWriter.(io.ReaderFrom);ok{n,err:=rf.ReadFrom(r);w.count(n);return n,err}
	return io.Copy(struct{io.Writer}{w},r)
}
func(w *meteredWriter)FlushError()error{if w.status==0{w.status=http.StatusOK};return http.NewResponseController(w.ResponseWriter).Flush()}
func(w *meteredWriter)Flush(){_ = w.FlushError()}
func(t *Traffic) Handler(next http.Handler)http.Handler{
	return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
		t.requests.Add(1)
		if r.Body!=nil{r.Body=&meteredBody{r.Body,&t.received}}
		next.ServeHTTP(&meteredWriter{ResponseWriter:w,bytes:&t.sent,head:r.Method==http.MethodHead},r)
	})
}
