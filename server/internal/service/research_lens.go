package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/netip"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"time"

	"qimeng.local/next/server/internal/researchprovider"
)

type lensSidecar struct {
	endpoint, token string
	client          *http.Client
}

func newLensSidecar(origin, token string) (*lensSidecar, error) {
	if origin == "" {
		return nil, nil
	}
	u, err := url.Parse(origin)
	if err != nil || u.Scheme != "http" || u.User != nil || u.Path != "" && u.Path != "/" || u.RawQuery != "" || u.ForceQuery || strings.Contains(origin, "#") {
		return nil, errors.New("QIMENG_RESEARCH_LENS_ENDPOINT requires an HTTP loopback origin with an explicit port")
	}
	address, err := netip.ParseAddr(u.Hostname())
	port, portErr := strconv.ParseUint(u.Port(), 10, 16)
	if err != nil || !address.IsLoopback() || address.Zone() != "" || portErr != nil || port == 0 {
		return nil, errors.New("QIMENG_RESEARCH_LENS_ENDPOINT requires a numeric loopback address and port")
	}
	if len(token) == 0 || len(token) > 4096 || strings.IndexFunc(token, func(r rune) bool { return r < 33 || r > 126 }) >= 0 {
		return nil, errors.New("QIMENG_RESEARCH_SIDECAR_TOKEN requires a nonempty printable bearer secret")
	}
	u.Path = "/lens"
	transport := &http.Transport{DialContext: (&net.Dialer{Timeout: 3 * time.Second}).DialContext, DisableCompression: true, DisableKeepAlives: true, MaxResponseHeaderBytes: 16 << 10}
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return &lensSidecar{endpoint: u.String(), token: token, client: client}, nil
}

func (l *lensSidecar) lookup(ctx context.Context, frame []byte, sha string) (string, []researchprovider.Item, []researchprovider.ImageHint) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreatePart(textproto.MIMEHeader{"Content-Disposition": {`form-data; name="file"; filename="frame.jpg"`}, "Content-Type": {"image/jpeg"}})
	if err != nil {
		return "unavailable", nil, nil
	}
	if _, err = part.Write(frame); err != nil || w.Close() != nil {
		return "unavailable", nil, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, l.endpoint, &body)
	if err != nil {
		return "unavailable", nil, nil
	}
	req.Header.Set("Authorization", "Bearer "+l.token)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Accept", "application/json")
	resp, err := l.client.Do(req)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return "interrupted", nil, nil
		}
		return "unavailable", nil, nil
	}
	defer resp.Body.Close()
	const maxReply = 256 << 10
	media, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || media != "application/json" || resp.ContentLength > maxReply || resp.Header.Get("Content-Encoding") != "" {
		return "unavailable", nil, nil
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxReply+1))
	if err != nil || len(raw) > maxReply {
		if errors.Is(ctx.Err(), context.Canceled) {
			return "interrupted", nil, nil
		}
		return "unavailable", nil, nil
	}
	return researchprovider.ParseLens(resp.StatusCode, raw, sha)
}
