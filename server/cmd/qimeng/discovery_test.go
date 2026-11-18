package main

import (
 "net"
 "reflect"
 "strings"
 "testing"
)

func TestDiscoveryUsesInstanceIdentityActualPortAndExplicitProxyOrigin(t *testing.T){
 id:="0123456789abcdef0123456789abcdef"
 listen:=&net.TCPAddr{IP:net.IPv4zero,Port:19031}
 record,e:=makeDiscoveryRecord(id,listen,discoveryConfig{})
 if e!=nil {t.Fatal(e)}
 expected:=[]string{"txtvers=1","api=0.1.0","id="+id,"scheme=http","path=/api/v1"}
 if record.Port!=19031||!reflect.DeepEqual(record.TXT,expected)||record.Host!="qimeng-"+id+".local." {t.Fatal("advertisement guessed a port or leaked host identity")}
 record,e=makeDiscoveryRecord(id,listen,discoveryConfig{Name:"Synthetic library",PublicOrigin:"https://library.example:19443"})
 if e!=nil||record.Port!=19443||record.TXT[3]!="scheme=https"||record.TXT[5]!="origin=https://library.example:19443" {t.Fatal("explicit proxy origin was not preserved")}
 for _,origin:=range []string{"http://library.example","https://owner:secret@library.example","https://library.example/api","https://library.example?token=secret","https://library.example:0","https://library.example:65536"} {
  if _,e:=makeDiscoveryRecord(id,listen,discoveryConfig{PublicOrigin:origin});e==nil {t.Fatal("unsafe discovery origin accepted")}
 }
 for _,name:=range []string{"a.b","private\nvalue",strings.Repeat("界",22)} {if _,e:=makeDiscoveryRecord(id,listen,discoveryConfig{Name:name});e==nil {t.Fatal("invalid DNS instance label accepted")}}
}

func TestDiscoveryDisabledAndLoopbackUseNoMulticast(t *testing.T){
 id:="0123456789abcdef0123456789abcdef"
 for _,config:=range []discoveryConfig{{},{Enabled:true}} {
  stop,e:=startDiscovery(&net.TCPAddr{IP:net.IPv4(127,0,0,1),Port:18431},id,config)
  if e!=nil {t.Fatal(e)};stop()
 }
 if _,e:=readDiscoveryConfig(func(key string)string{if key=="QIMENG_DISCOVERY" {return "treu"};return ""});e==nil {t.Fatal("mistyped discovery switch silently enabled a default")}
}
