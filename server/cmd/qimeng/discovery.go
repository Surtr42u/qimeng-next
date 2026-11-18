package main

import (
	"errors"
	"log"
	"log/slog"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/hashicorp/mdns"
)

const discoveryServiceType = "_qimeng._tcp"

type discoveryConfig struct {
	Enabled                       bool
	Name, Interface, PublicOrigin string
}
type discoveryRecord struct {
	Name, Host string
	Port       int
	TXT        []string
}

func readDiscoveryConfig(getenv func(string) string) (discoveryConfig, error) {
	value := getenv("QIMENG_DISCOVERY")
	if value != "" && value != "true" && value != "false" {
		return discoveryConfig{}, errors.New("QIMENG_DISCOVERY must be true or false")
	}
	return discoveryConfig{Enabled: value == "true", Name: getenv("QIMENG_DISCOVERY_NAME"), Interface: getenv("QIMENG_DISCOVERY_INTERFACE"), PublicOrigin: getenv("QIMENG_PUBLIC_ORIGIN")}, nil
}

// Discovery is an optional host transport, not a domain dependency or trust
// channel. Neither machine/user names nor credentials belong in DNS records.
func makeDiscoveryRecord(instanceID string, listen net.Addr, c discoveryConfig) (discoveryRecord, error) {
	record := discoveryRecord{}
	if len(instanceID) != 32 || strings.Trim(instanceID, "0123456789abcdef") != "" {
		return record, errors.New("invalid discovery instance identity")
	}
	tcp, ok := listen.(*net.TCPAddr)
	if !ok || tcp.Port < 1 || tcp.Port > 65535 {
		return record, errors.New("discovery requires a TCP listener")
	}
	name := c.Name
	if name == "" {
		name = "Qimeng-" + instanceID[:12]
	}
	if !utf8.ValidString(name) || len(name) > 63 || strings.TrimSpace(name) == "" || strings.ContainsAny(name, ".\\") || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return record, errors.New("invalid discovery display name")
	}
	record = discoveryRecord{Name: name, Host: "qimeng-" + instanceID + ".local.", Port: tcp.Port, TXT: []string{"txtvers=1", "api=0.1.0", "id=" + instanceID, "scheme=http", "path=/api/v1"}}
	if c.PublicOrigin != "" {
		u, e := url.Parse(c.PublicOrigin)
		if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || len(c.PublicOrigin) > 240 {
			return discoveryRecord{}, errors.New("public discovery origin must be a bounded HTTPS origin without credentials or path")
		}
		port := 443
		if u.Port() != "" {
			port, e = strconv.Atoi(u.Port())
			if e != nil || port < 1 || port > 65535 {
				return discoveryRecord{}, errors.New("invalid public discovery port")
			}
		}
		record.Port = port
		record.TXT[3] = "scheme=https"
		record.TXT = append(record.TXT, "origin="+c.PublicOrigin)
	}
	return record, nil
}

type discoveryLog struct {
	mu   sync.Mutex
	last time.Time
}

func (d *discoveryLog) Write(raw []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if time.Since(d.last) >= time.Minute {
		d.last = time.Now()
		slog.Warn("discovery packet or network operation failed", "detail", "suppressed private network values")
	}
	return len(raw), nil
}

func startDiscovery(listen net.Addr, instanceID string, c discoveryConfig) (func(), error) {
	stop := func() {}
	if !c.Enabled {
		return stop, nil
	}
	record, e := makeDiscoveryRecord(instanceID, listen, c)
	if e != nil {
		return stop, e
	}
	tcp := listen.(*net.TCPAddr)
	// A supervised Android loopback host never advertises. A standalone reverse
	// proxy may explicitly advertise its HTTPS origin despite a loopback upstream.
	if tcp.IP.IsLoopback() && c.PublicOrigin == "" {
		return stop, nil
	}
	interfaces, e := net.Interfaces()
	if e != nil {
		return stop, e
	}
	servers := []*mdns.Server{}
	stop = func() {
		for _, server := range servers {
			if e := server.Shutdown(); e != nil {
				slog.Warn("discovery shutdown incomplete")
			}
		}
	}
	for _, network := range interfaces {
		if c.Interface != "" && network.Name != c.Interface {
			continue
		}
		if network.Flags&net.FlagUp == 0 || network.Flags&net.FlagMulticast == 0 || network.Flags&net.FlagLoopback != 0 {
			continue
		}
		addresses, e := network.Addrs()
		if e != nil {
			stop()
			return func() {}, e
		}
		ips := []net.IP{}
		for _, address := range addresses {
			ip, _, e := net.ParseCIDR(address.String())
			if e != nil || !ip.IsPrivate() || ip.IsLoopback() {
				continue
			}
			if !tcp.IP.IsUnspecified() && !tcp.IP.IsLoopback() && !tcp.IP.Equal(ip) {
				continue
			}
			ips = append(ips, ip)
		}
		if len(ips) == 0 {
			continue
		}
		zone, e := mdns.NewMDNSService(record.Name, discoveryServiceType, "local.", record.Host, record.Port, ips, record.TXT)
		if e != nil {
			stop()
			return func() {}, e
		}
		server, e := mdns.NewServer(&mdns.Config{Zone: zone, Iface: &network, Logger: log.New(&discoveryLog{}, "", 0)})
		if e != nil {
			stop()
			return func() {}, e
		}
		servers = append(servers, server)
	}
	if len(servers) == 0 {
		return stop, errors.New("no eligible LAN multicast interface for discovery")
	}
	slog.Info("LAN discovery enabled", "interfaces", len(servers))
	return stop, nil
}
