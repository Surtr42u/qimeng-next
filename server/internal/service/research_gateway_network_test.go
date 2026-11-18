package service

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"testing"

	"qimeng.local/next/server/internal/researchprovider"
)

type researchRoundTripper func(*http.Request)(*http.Response,error)
func(f researchRoundTripper)RoundTrip(r *http.Request)(*http.Response,error){return f(r)}

func TestPublicLookupNeverFollowsRedirectOrConnectsToMixedPublicPrivateDNS(t *testing.T){
	q,e:=researchprovider.Build("fxtwitter","author","fixture",1);if e!=nil{t.Fatal(e)}
	var gateway publicResearchGateway;gateway.init()
	transport:=gateway.client.Transport.(*http.Transport)
	if transport.Proxy!=nil{t.Fatal("provider queries must not use an environment proxy")}
	dials:=0
	network:=avatarNetwork{
		lookup:func(context.Context,string)([]netip.Addr,error){return []netip.Addr{netip.MustParseAddr("8.8.8.8"),netip.MustParseAddr("127.0.0.1")},nil},
		dial:func(context.Context,string,string)(net.Conn,error){dials++;return nil,errors.New("unexpected connection")},
	}
	transport.DialContext=network.dialPublic
	if state,items:=gateway.fetch(context.Background(),q);state!="unavailable"||len(items)!=0||dials!=0{t.Fatal("mixed DNS reached a network connection")}
	sent:=0;gateway.client.Transport=researchRoundTripper(func(r *http.Request)(*http.Response,error){
		sent++
		return &http.Response{StatusCode:307,Header:http.Header{"Location":{"https://example.org/elsewhere"}},Body:io.NopCloser(strings.NewReader("")),Request:r},nil
	})
	if state,items:=gateway.fetch(context.Background(),q);state!="unavailable"||len(items)!=0||sent!=1{t.Fatal("provider redirect forwarded the caller's query")}
}
