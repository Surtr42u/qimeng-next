// Package researchprovider defines the public text lookup matrix. It never
// accepts an HTTP method, destination, credential, original file or frame.
package researchprovider

import (
	"errors"
	"net"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Info struct {
	Name         string   `json:"name"`
	Operations   []string `json:"operations"`
	EvidenceKind string   `json:"evidenceKind"`
}

var providers = []Info{
	{"fxtwitter", []string{"author", "post"}, "platform_record"},
	{"pixiv", []string{"author", "work"}, "platform_record"},
	{"yandere", []string{"artist", "tags"}, "platform_record"},
	{"e621", []string{"post", "tags"}, "platform_record"},
	{"wayback", []string{"url"}, "lead"},
	{"steam", []string{"workshop"}, "platform_record"},
	{"bing", []string{"search"}, "lead"},
}

func Matrix() []Info { return slices.Clone(providers) }
func Known(name string) bool {
	return slices.ContainsFunc(providers, func(i Info) bool { return i.Name == name })
}

var digits = regexp.MustCompile(`^[0-9]{1,20}$`)
var handle = regexp.MustCompile(`^[A-Za-z0-9_]{1,15}$`)
var hostName = regexp.MustCompile(`^[a-z0-9.-]+$`)

type Query struct {
	Name, Operation, Target, EvidenceKind, Method, URL, Form, Referer string
	Limit                                                             int
}
type Item struct {
	URL         string   `json:"url"`
	ID          string   `json:"id,omitempty"`
	Title       string   `json:"title,omitempty"`
	Author      string   `json:"author,omitempty"`
	AuthorID    string   `json:"authorId,omitempty"`
	PublishedAt string   `json:"publishedAt,omitempty"`
	Sources     []string `json:"sources,omitempty"`
}

func publicURL(raw string) *url.URL {
	u, e := url.Parse(raw)
	if e != nil || len(raw) > 1600 || u.Scheme != "https" || u.User != nil || u.Opaque != "" || u.Port() != "" && u.Port() != "443" || strings.ContainsAny(raw, "\\\r\n\x00") || strings.Contains(u.Path, "\\") || strings.IndexFunc(u.Path, unicode.IsControl) >= 0 {
		return nil
	}
	host := strings.ToLower(u.Hostname())
	if !hostName.MatchString(host) || !strings.Contains(host, ".") || net.ParseIP(host) != nil || strings.HasSuffix(host, ".") ||
		strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".home.arpa") {
		return nil
	}
	labels := strings.Split(host, ".")
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return nil
		}
	}
	last := labels[len(labels)-1]
	if last[0] < 'a' || last[0] > 'z' || strings.HasPrefix(last, "0x") {
		return nil
	}
	u.Host = host
	return u
}
func cleanLink(raw string) string {
	u := publicURL(raw)
	if u == nil {
		return ""
	}
	id := u.Query().Get("id")
	u.RawQuery = ""
	u.ForceQuery = false
	u.Fragment = ""
	if u.Host == "steamcommunity.com" && u.Path == "/sharedfiles/filedetails/" && digits.MatchString(id) {
		u.RawQuery = "id=" + id
	}
	return u.String()
}
func publicTerms(s string) bool {
	return !strings.ContainsAny(s, "/\\=?") && !strings.HasPrefix(s, "~") && !strings.HasPrefix(strings.ToLower(s), "file:")
}
func publicXTarget(target, op string) string {
	if strings.HasPrefix(target, "https://") {
		u := publicURL(target)
		if u == nil || !slices.Contains([]string{"x.com", "www.x.com", "twitter.com", "www.twitter.com"}, u.Host) || u.RawQuery != "" || u.Fragment != "" {
			return ""
		}
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if op == "author" && len(parts) == 1 {
			target = parts[0]
		} else if op == "post" && len(parts) == 3 && handle.MatchString(parts[0]) && parts[1] == "status" {
			target = parts[2]
		} else {
			return ""
		}
	}
	if op == "author" {
		target = strings.TrimPrefix(target, "@")
		if handle.MatchString(target) {
			return target
		}
	} else if digits.MatchString(target) {
		return target
	}
	return ""
}
func Build(name, operation, target string, limit int) (Query, error) {
	var q Query
	if !utf8.ValidString(target) || utf8.RuneCountInString(target) > 500 || strings.IndexFunc(target, unicode.IsControl) >= 0 {
		return q, errors.New("invalid lookup target")
	}
	target = strings.TrimSpace(target)
	if limit == 0 {
		limit = 10
	}
	if target == "" || limit < 1 || limit > 20 {
		return q, errors.New("invalid lookup limit or target")
	}
	for _, info := range providers {
		if name == info.Name && slices.Contains(info.Operations, operation) {
			q.EvidenceKind = info.EvidenceKind
			break
		}
	}
	if q.EvidenceKind == "" {
		return q, errors.New("unsupported provider operation")
	}
	q.Name, q.Operation, q.Method, q.Limit = name, operation, "GET", limit
	values := url.Values{}
	switch name {
	case "fxtwitter":
		target = publicXTarget(target, operation)
		if target == "" {
			return q, errors.New("invalid public X identifier")
		}
		part := "status"
		if operation == "author" {
			part = "profile"
		}
		q.URL = "https://api.fxtwitter.com/2/" + part + "/" + target
	case "pixiv":
		if !digits.MatchString(target) {
			return q, errors.New("invalid numeric identifier")
		}
		if operation == "author" {
			q.URL = "https://www.pixiv.net/ajax/user/" + target
			values.Set("full", "1")
		} else {
			q.URL = "https://www.pixiv.net/ajax/illust/" + target
		}
		q.Referer = "https://www.pixiv.net/"
	case "e621", "yandere":
		if name == "e621" && operation == "post" {
			if !digits.MatchString(target) {
				return q, errors.New("invalid numeric identifier")
			}
			q.URL = "https://e621.net/posts/" + target + ".json"
			break
		}
		if !publicTerms(target) {
			return q, errors.New("invalid public artist or tags")
		}
		if operation == "artist" {
			q.URL = "https://yande.re/artist.json"
			values.Set("name", target)
		} else {
			q.URL = "https://yande.re/post.json"
			if name == "e621" {
				q.URL = "https://e621.net/posts.json"
			}
			values.Set("tags", target)
		}
		values.Set("limit", strconv.Itoa(limit))
	case "wayback":
		u := publicURL(target)
		if u == nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
			return q, errors.New("archive lookup requires an unsigned public HTTPS URL")
		}
		target = u.String()
		q.URL = "https://web.archive.org/cdx/search/cdx"
		values.Set("url", target)
		values.Set("matchType", "exact")
		values.Set("output", "json")
		values.Set("fl", "timestamp,original,statuscode")
		values.Set("filter", "statuscode:200")
		values.Set("collapse", "digest")
		values.Set("limit", strconv.Itoa(limit))
	case "steam":
		if !digits.MatchString(target) {
			return q, errors.New("invalid numeric identifier")
		}
		q.Method = "POST"
		q.URL = "https://api.steampowered.com/ISteamRemoteStorage/GetPublishedFileDetails/v1/"
		q.Form = url.Values{"itemcount": {"1"}, "publishedfileids[0]": {target}}.Encode()
	case "bing":
		if !publicTerms(target) {
			return q, errors.New("search accepts public words only")
		}
		q.URL = "https://www.bing.com/search"
		values.Set("q", target)
		values.Set("count", strconv.Itoa(limit))
	}
	if len(values) > 0 {
		q.URL += "?" + values.Encode()
	}
	q.Target = target
	return q, nil
}
