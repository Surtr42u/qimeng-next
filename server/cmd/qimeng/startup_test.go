package main

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSupervisedTLSReceiptAuthenticatesSameListener(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "ready.json")
	listener, e := openStartupListener("127.0.0.1:0", filename, "synthetic-launch-identity", true)
	if e != nil {
		t.Fatal(e)
	}
	defer listener.Close()
	if _, e := os.Stat(filename); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("binding published a receipt before service initialization")
	}
	if e := listener.publishReceipt("synthetic-instance"); e != nil {
		t.Fatal(e)
	}
	var receipt startupReceipt
	raw, e := os.ReadFile(filename)
	if e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(raw, &receipt); e != nil {
		t.Fatal(e)
	}
	if receipt.LaunchID != "synthetic-launch-identity" || receipt.InstanceID != "synthetic-instance" || receipt.ListenAddress != listener.Addr().String() {
		t.Fatal("receipt did not bind launch and actual listener")
	}
	der, e := base64.StdEncoding.DecodeString(receipt.CertificateDERBase64)
	if e != nil {
		t.Fatal(e)
	}
	certificate, e := x509.ParseCertificate(der)
	if e != nil {
		t.Fatal(e)
	}
	pool := x509.NewCertPool()
	pool.AddCert(certificate)
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil {
			t.Error("host connection was not TLS")
		}
		w.Write([]byte("synthetic-ready"))
	}), ReadHeaderTimeout: time.Second}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	defer func() { server.Close(); <-done }()
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}, Timeout: 3 * time.Second}
	response, e := client.Get("https://" + receipt.ListenAddress)
	if e != nil {
		t.Fatal(e)
	}
	data, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if string(data) != "synthetic-ready" {
		t.Fatal("wrong supervised service")
	}
}

func TestOccupiedListenerCannotOverwriteStartupReceipt(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "ready.json")
	original := []byte("previous private receipt")
	if e := os.WriteFile(filename, original, 0600); e != nil {
		t.Fatal(e)
	}
	occupied, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer occupied.Close()
	listener, e := openStartupListener(occupied.Addr().String(), filename, "synthetic-launch-other", true)
	if e == nil {
		listener.Close()
		t.Fatal("occupied listener was accepted")
	}
	after, e := os.ReadFile(filename)
	if e != nil || string(after) != string(original) {
		t.Fatal("failed launch overwrote old receipt")
	}
	if listener, e = openStartupListener("0.0.0.0:0", filename, "synthetic-public-launch", true); e == nil {
		listener.Close()
		t.Fatal("ephemeral private trust exposed on public interfaces")
	}
}

func TestFailedRunKeepsPriorReceiptAndReleasesPort(t *testing.T) {
	dir := t.TempDir()
	receipt := filepath.Join(dir, "ready.json")
	if e := os.WriteFile(receipt, []byte("previous receipt"), 0600); e != nil {
		t.Fatal(e)
	}
	t.Setenv("QIMENG_DATA_DIR", filepath.Join(dir, "data"))
	t.Setenv("QIMENG_STARTUP_RECEIPT", receipt)
	t.Setenv("QIMENG_LAUNCH_ID", "synthetic-launch-identity")
	for _, tc := range []struct{ name, discovery, tlsSetting, token, want string }{
		{"invalid discovery flag", "invalid", "false", strings.Repeat("x", 32), "QIMENG_DISCOVERY"},
		{"invalid TLS flag", "false", "invalid", strings.Repeat("x", 32), "QIMENG_EPHEMERAL_TLS"},
		{"service initialization fails", "false", "false", "", "QIMENG_ADMIN_TOKEN"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			port, e := net.Listen("tcp", "127.0.0.1:0")
			if e != nil {
				t.Fatal(e)
			}
			address := port.Addr().String()
			if e := port.Close(); e != nil {
				t.Fatal(e)
			}
			t.Setenv("QIMENG_LISTEN", address)
			t.Setenv("QIMENG_DISCOVERY", tc.discovery)
			t.Setenv("QIMENG_EPHEMERAL_TLS", tc.tlsSetting)
			t.Setenv("QIMENG_ADMIN_TOKEN", tc.token)
			if e := run(); e == nil || !strings.Contains(e.Error(), tc.want) {
				t.Fatalf("expected %s configuration error, got %v", tc.want, e)
			}
			after, e := os.ReadFile(receipt)
			if e != nil || string(after) != "previous receipt" {
				t.Fatal("failed startup replaced the prior receipt")
			}
			if _, e := os.Stat(filepath.Join(dir, "data")); !errors.Is(e, os.ErrNotExist) {
				t.Fatal("failed startup opened the data directory")
			}
			again, e := net.Listen("tcp", address)
			if e != nil {
				t.Fatal("failed startup retained the port", e)
			}
			again.Close()
		})
	}
}
