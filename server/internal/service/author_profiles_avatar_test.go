package service

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/netip"
	"testing"
)

func TestProfileAvatarPublicAddressPolicy(t *testing.T) {
	denied := []string{"127.0.0.1", "10.0.0.1", "172.20.0.1", "192.168.1.1", "100.64.0.1", "0.1.2.3", "169.254.169.254", "192.0.2.2", "198.18.0.1", "198.51.100.1", "203.0.113.1", "224.0.0.1", "240.0.0.1", "::", "::1", "::ffff:127.0.0.1", "fc00::1", "fe80::1", "64:ff9b::a00:1", "2002:7f00:1::", "2001:db8::1", "2001::1", "3fff::1"}
	for _, value := range denied {
		if publicAvatarAddress(netip.MustParseAddr(value)) {
			t.Errorf("allowed reserved address %s", value)
		}
	}
	for _, value := range []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111", "2001:4860:4860::8888"} {
		if !publicAvatarAddress(netip.MustParseAddr(value)) {
			t.Errorf("denied public address %s", value)
		}
	}
	for _, value := range []string{"http://public.example/a.png", "https://name:password@public.example/a.png", "https://public.example:444/a.png", "https://localhost/a.png", "https://machine.local/a.png", "https://127.0.0.1/a.png", "https://[::ffff:127.0.0.1]/a.png", "https://public.example/a.png#x"} {
		if _, err := validateAvatarURL(value); err == nil {
			t.Errorf("accepted invalid source %s", value)
		}
	}
}

func TestProfileAvatarDNSIsValidatedAndDialedOnce(t *testing.T) {
	calls, dials := 0, 0
	network := avatarNetwork{
		lookup: func(context.Context, string) ([]netip.Addr, error) {
			calls++
			if calls == 1 {
				return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
			}
			return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
		},
		dial: func(_ context.Context, protocol, address string) (net.Conn, error) {
			dials++
			if address != "8.8.8.8:443" {
				t.Fatalf("hostname re-resolved by dial: %s", address)
			}
			left, right := net.Pipe()
			right.Close()
			return left, nil
		},
	}
	connection, err := network.dialPublic(context.Background(), "tcp", "avatar.example:443")
	if err != nil {
		t.Fatal(err)
	}
	connection.Close()
	if calls != 1 || dials != 1 {
		t.Fatal("lookup and numeric dial were not pinned")
	}
	if _, err = network.dialPublic(context.Background(), "tcp", "avatar.example:443"); err == nil || dials != 1 {
		t.Fatal("DNS rebound to loopback was dialed")
	}
	network.lookup = func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("192.168.1.1")}, nil
	}
	if _, err = network.dialPublic(context.Background(), "tcp", "mixed.example:443"); err == nil || dials != 1 {
		t.Fatal("mixed public/private answer was dialed")
	}
	client := avatarHTTPClient(network)
	request, _ := http.NewRequest("GET", "https://127.0.0.1/secret", nil)
	if err = client.CheckRedirect(request, []*http.Request{request}); err == nil {
		t.Fatal("private redirect accepted")
	}
	if client.Transport.(*http.Transport).Proxy != nil {
		t.Fatal("environment proxy can bypass pinned dial")
	}
}

type profileRoundTrip func(*http.Request) (*http.Response, error)

func (f profileRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestProfileAvatarResponseLimitsAndErrorRedaction(t *testing.T) {
	client := &http.Client{Transport: profileRoundTrip(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}, ContentLength: -1, Body: io.NopCloser(bytes.NewReader(make([]byte, maxProfileAvatarBytes+1))), Request: r}, nil
	})}
	if _, _, err := fetchAvatarBytes(context.Background(), "https://public.example/a.png", client); err == nil {
		t.Fatal("oversize chunked body accepted")
	}
	client.Transport = profileRoundTrip(func(r *http.Request) (*http.Response, error) {
		return nil, errors.New("private URL should not be returned")
	})
	_, _, err := fetchAvatarBytes(context.Background(), "https://public.example/a.png?secret=synthetic", client)
	if err == nil || bytes.Contains([]byte(err.Error()), []byte("secret")) {
		t.Fatal("raw source leaked in error")
	}
}

func TestProfileAvatarDecodeLimitsAndNormalization(t *testing.T) {
	original := image.NewNRGBA(image.Rect(0, 0, 600, 300))
	original.Set(2, 3, color.NRGBA{R: 200, A: 255})
	var source bytes.Buffer
	if err := png.Encode(&source, original); err != nil {
		t.Fatal(err)
	}
	normalized, err := normalizeProfileAvatar(source.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(normalized.body))
	if err != nil || format != "png" || config.Width != 512 || config.Height != 256 {
		t.Fatal("avatar was not bounded PNG")
	}
	if normalized.sourceHash != digest(source.Bytes()) {
		t.Fatal("source snapshot hash lost")
	}
	// A valid IHDR advertising enormous pixels is rejected before image allocation.
	bomb := append([]byte(nil), source.Bytes()...)
	binary.BigEndian.PutUint32(bomb[16:20], 4096)
	binary.BigEndian.PutUint32(bomb[20:24], 4096)
	binary.BigEndian.PutUint32(bomb[29:33], crc32.ChecksumIEEE(bomb[12:29]))
	if _, err = normalizeProfileAvatar(bomb); err == nil {
		t.Fatal("pixel bomb accepted")
	}
	if _, err = normalizeProfileAvatar([]byte("<svg onload='x'/>")); err == nil {
		t.Fatal("active format accepted")
	}
}
