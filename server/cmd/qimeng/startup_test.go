package main

import (
 "crypto/tls"
 "crypto/x509"
 "encoding/base64"
 "encoding/json"
 "io"
 "net"
 "net/http"
 "os"
 "path/filepath"
 "testing"
 "time"
)

func TestSupervisedTLSReceiptAuthenticatesSameListener(t *testing.T){
 filename:=filepath.Join(t.TempDir(),"ready.json")
 listener,e:=supervisedListener("127.0.0.1:0","synthetic-instance",filename,"synthetic-launch-identity",true);if e!=nil{t.Fatal(e)};defer listener.Close()
 var receipt startupReceipt;raw,e:=os.ReadFile(filename);if e!=nil{t.Fatal(e)};if e=json.Unmarshal(raw,&receipt);e!=nil{t.Fatal(e)}
 if receipt.LaunchID!="synthetic-launch-identity"||receipt.InstanceID!="synthetic-instance"||receipt.ListenAddress!=listener.Addr().String(){t.Fatal("receipt did not bind launch and actual listener")}
 der,e:=base64.StdEncoding.DecodeString(receipt.CertificateDERBase64);if e!=nil{t.Fatal(e)};certificate,e:=x509.ParseCertificate(der);if e!=nil{t.Fatal(e)}
 pool:=x509.NewCertPool();pool.AddCert(certificate)
 server:=&http.Server{Handler:http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){if r.TLS==nil{t.Error("host connection was not TLS")};w.Write([]byte("synthetic-ready"))}),ReadHeaderTimeout:time.Second}
 done:=make(chan error,1);go func(){done<-server.Serve(listener)}();defer func(){server.Close();<-done}()
 client:=&http.Client{Transport:&http.Transport{TLSClientConfig:&tls.Config{RootCAs:pool,MinVersion:tls.VersionTLS12}},Timeout:3*time.Second}
 response,e:=client.Get("https://"+receipt.ListenAddress);if e!=nil{t.Fatal(e)};data,_:=io.ReadAll(response.Body);response.Body.Close();if string(data)!="synthetic-ready"{t.Fatal("wrong supervised service")}
}

func TestOccupiedListenerCannotOverwriteStartupReceipt(t *testing.T){
 filename:=filepath.Join(t.TempDir(),"ready.json");original:=[]byte("previous private receipt");if e:=os.WriteFile(filename,original,0600);e!=nil{t.Fatal(e)}
 occupied,e:=net.Listen("tcp","127.0.0.1:0");if e!=nil{t.Fatal(e)};defer occupied.Close()
 listener,e:=supervisedListener(occupied.Addr().String(),"different-instance",filename,"synthetic-launch-other",true);if e==nil{listener.Close();t.Fatal("occupied listener was accepted")}
 after,e:=os.ReadFile(filename);if e!=nil||string(after)!=string(original){t.Fatal("failed launch overwrote old receipt")}
 if listener,e=supervisedListener("0.0.0.0:0","instance",filename,"synthetic-public-launch",true);e==nil{listener.Close();t.Fatal("ephemeral private trust exposed on public interfaces")}
}
