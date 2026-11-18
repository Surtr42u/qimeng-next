package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestLensSidecarPinsLoopbackWithoutProxyOrRedirect(t *testing.T) {
	for _, origin := range []string{"http://localhost:18421", "http://192.0.2.5:18421", "http://127.0.0.1:0", "http://127.0.0.1:18421/search", "http://127.0.0.1:18421?request=other"} {
		if _, err := newLensSidecar(origin, "synthetic"); err == nil {
			t.Error("noncanonical sidecar origin accepted", origin)
		}
	}
	if _, err := newLensSidecar("http://127.0.0.1:18421", ""); err == nil {
		t.Fatal("configured receiver requires a bearer secret")
	}
	var redirected atomic.Int32
	second := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirected.Add(1) }))
	defer second.Close()
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, second.URL, http.StatusTemporaryRedirect)
	}))
	defer first.Close()
	lens, err := newLensSidecar(first.URL, "synthetic")
	if err != nil || lens.client.Transport.(*http.Transport).Proxy != nil {
		t.Fatal("loopback transport must be direct")
	}
	state, _, _ := lens.lookup(context.Background(), []byte("synthetic"), "fixture")
	if state != "unavailable" || redirected.Load() != 0 {
		t.Fatal("frame or credentials followed a redirect")
	}
}
