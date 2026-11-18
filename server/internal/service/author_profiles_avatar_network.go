package service

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

const maxProfileAvatarBytes = 4 << 20

var avatarDecodeSlots = make(chan struct{}, 2)

var avatarDeniedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"), netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"), netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),
	// Conservatively exclude IPv6 protocol-assignment, transition and
	// documentation spaces. Only native global-unicast 2000::/3 is accepted.
	netip.MustParsePrefix("2001::/23"), netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"), netip.MustParsePrefix("3fff::/20"),
}

func publicAvatarAddress(address netip.Addr) bool {
	address = address.Unmap()
	if !address.IsValid() || address.Zone() != "" || !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() {
		return false
	}
	if address.Is6() && !netip.MustParsePrefix("2000::/3").Contains(address) {
		return false
	}
	for _, prefix := range avatarDeniedPrefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}

func validateAvatarURL(value string) (*url.URL, error) {
	u, err := url.Parse(value)
	if err != nil || !profileText(value, 4096) || u.Scheme != "https" || u.Opaque != "" || u.User != nil || u.Fragment != "" || u.Hostname() == "" || (u.Port() != "" && u.Port() != "443") || strings.HasSuffix(u.Host, ":") {
		return nil, Fail(400, "invalid_avatar_url", "头像只接受无凭据、无片段的 HTTPS 公网网址，端口必须为 443。")
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") || strings.Contains(host, "%") {
		return nil, Fail(400, "avatar_address_denied", "不能从本机或内部网络获取头像。")
	}
	if address, err := netip.ParseAddr(host); err == nil && !publicAvatarAddress(address) {
		return nil, Fail(400, "avatar_address_denied", "不能从保留或内部地址获取头像。")
	}
	return u, nil
}

type avatarNetwork struct {
	lookup func(context.Context, string) ([]netip.Addr, error)
	dial   func(context.Context, string, string) (net.Conn, error)
}

func defaultAvatarNetwork() avatarNetwork {
	resolver := net.DefaultResolver
	dialer := &net.Dialer{Timeout: 3 * time.Second}
	return avatarNetwork{
		lookup: func(ctx context.Context, host string) ([]netip.Addr, error) {
			return resolver.LookupNetIP(ctx, "ip", host)
		},
		dial: dialer.DialContext,
	}
}

// Resolution and numeric-IP dialing are one operation. Transport cannot resolve
// the name a second time after validation; TLS still verifies the original host.
func (network avatarNetwork) dialPublic(ctx context.Context, protocol, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil || port != "443" {
		return nil, Fail(400, "avatar_address_denied", "头像连接目标无效。")
	}
	var addresses []netip.Addr
	if literal, err := netip.ParseAddr(host); err == nil {
		addresses = []netip.Addr{literal}
	} else {
		addresses, err = network.lookup(ctx, host)
		if err != nil {
			return nil, Fail(502, "avatar_dns_failed", "无法解析头像来源。")
		}
	}
	if len(addresses) == 0 {
		return nil, Fail(502, "avatar_dns_failed", "头像来源没有可连接的地址。")
	}
	for _, address := range addresses {
		if !publicAvatarAddress(address) {
			return nil, Fail(400, "avatar_address_denied", "来源解析包含内部或保留地址，已拒绝连接。")
		}
	}
	for _, address := range addresses {
		connection, err := network.dial(ctx, "tcp", net.JoinHostPort(address.Unmap().String(), "443"))
		if err == nil {
			return connection, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	return nil, Fail(502, "avatar_connect_failed", "无法连接头像来源。")
}

func avatarHTTPClient(network avatarNetwork) *http.Client {
	transport := &http.Transport{
		Proxy: nil, DialContext: network.dialPublic,
		TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout: 4 * time.Second, ResponseHeaderTimeout: 5 * time.Second,
		DisableCompression: true, DisableKeepAlives: true, ForceAttemptHTTP2: false,
		MaxResponseHeaderBytes: 32 << 10,
	}
	return &http.Client{Transport: transport, Timeout: 10 * time.Second, CheckRedirect: func(request *http.Request, via []*http.Request) error {
		if len(via) > 3 {
			return Fail(400, "avatar_redirect_limit", "头像来源重定向过多。")
		}
		_, err := validateAvatarURL(request.URL.String())
		return err
	}}
}

func fetchAvatarBytes(ctx context.Context, source string, client *http.Client) ([]byte, string, error) {
	u, err := validateAvatarURL(source)
	if err != nil {
		return nil, "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, "", Fail(400, "invalid_avatar_url", "头像来源网址无效。")
	}
	request.Header.Set("Accept", "image/png,image/jpeg,image/webp,image/gif")
	request.Header.Set("User-Agent", "Qimeng-Avatar/1")
	response, err := client.Do(request)
	if err != nil {
		// net/http wraps errors with the request URL; never expose that wrapper to
		// logs or callers because signed source URLs can contain private material.
		var app *Error
		if errors.As(err, &app) {
			return nil, "", app
		}
		if ctx.Err() != nil {
			return nil, "", Fail(504, "avatar_timeout", "头像来源读取超时，原头像保持不变。")
		}
		return nil, "", Fail(502, "avatar_fetch_failed", "头像来源无法读取，原头像保持不变。")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, "", Fail(502, "avatar_http_status", "头像来源没有返回可用图片。")
	}
	if response.Header.Get("Content-Encoding") != "" && response.Header.Get("Content-Encoding") != "identity" {
		return nil, "", Fail(400, "avatar_encoding_denied", "头像来源的传输编码不受支持。")
	}
	if response.ContentLength > maxProfileAvatarBytes {
		return nil, "", Fail(413, "avatar_too_large", "头像原图最多 4 MiB。")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxProfileAvatarBytes+1))
	if err != nil {
		return nil, "", Fail(502, "avatar_read_failed", "头像原图读取中断，原头像保持不变。")
	}
	if len(body) > maxProfileAvatarBytes {
		return nil, "", Fail(413, "avatar_too_large", "头像原图最多 4 MiB。")
	}
	return body, response.Request.URL.String(), nil
}
