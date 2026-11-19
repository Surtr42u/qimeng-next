package catalog

import (
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"qimeng.local/next/server/internal/service"
)

const errPrivate = sourceProblem("nonportable_business_value")

var httpScheme = regexp.MustCompile(`(?i)https?://`)
var escapedOctet = regexp.MustCompile(`%[0-9a-fA-F]{2}`)
var privateText = regexp.MustCompile(`(?i)(?:[a-z]:[/\\]|\\\\[^\\]+\\|file://|-----BEGIN [A-Z ]*PRIVATE KEY-----|\b(?:bearer|basic)\s+[a-z0-9+/_=.-]{8,}|\b(?:access[_-]?token|api[_-]?key|password|secret)[ \t]*[:=]\s*\S)`)
var homePath = regexp.MustCompile(`(?i)/(?:home|users)/[^/ \t\r\n]+/(?:\.ssh|\.config|desktop|documents|downloads|library)(?:/|$)`)
var assignment = regexp.MustCompile(`(?i)\b([a-z][a-z0-9_.-]*)["'“”‘’]?[ \t]*[:=：＝]`)

func sensitiveKey(key string) bool {
	k := strings.ToLower(strings.NewReplacer("-", "", "_", "", ".", "").Replace(key))
	return strings.Contains(k, "password") || strings.Contains(k, "secret") || strings.Contains(k, "credential") ||
		strings.HasSuffix(k, "token") || strings.HasSuffix(k, "apikey") || k == "authorization" || k == "cookie" ||
		k == "privatekey" || k == "signature" || k == "sig" || strings.HasPrefix(k, "xamz") || strings.HasPrefix(k, "xgoog")
}

func (i *importer) portableText(text string) error {
	_, _, err := i.inspectText(text, strictText)
	return err
}

// Only a known descriptive profile field can request removal. Ordinary text,
// structured URL fields and inner URLs always require an already portable value.
type textPolicy int

const (
	strictText textPolicy = iota
	biographyText
	fragmentText
)

func (i *importer) inspectText(text string, policy textPolicy) (string, int, error) {
	if !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
		return "", 0, sourceProblem("business_text_invalid")
	}
	var out strings.Builder
	end, removed := 0, 0
	for {
		span := httpScheme.FindStringIndex(text[end:])
		if span == nil {
			break
		}
		start := end + span[0]
		// A URL used as the VALUE of a plain credential assignment remains
		// private regardless of the URL's own host and query.
		if err := i.nonURLContent(text[end:start], true); err != nil {
			return "", 0, err
		}
		token := proseLink(text[start:])
		safe, count, err := i.publicReference(token, policy, 0)
		if err != nil {
			return "", 0, err
		}
		if policy == biographyText {
			out.WriteString(text[end:start])
			out.WriteString(safe)
		}
		removed += count
		end = start + len(token)
	}
	if err := i.nonURLText(text[end:]); err != nil {
		return "", 0, err
	}
	if removed == 0 {
		return text, 0, nil
	}
	out.WriteString(text[end:])
	return out.String(), removed, nil
}

func proseLink(rest string) string {
	// A closing Markdown delimiter belongs to the surrounding text, even
	// when another sentence or link immediately follows it. Balanced brackets
	// in the URL (IPv6 authorities, parenthesized titles) belong to the URL.
	var opens [3]int
	end := len(rest)
scan:
	for n, r := range rest {
		switch {
		case unicode.IsSpace(r) || unicode.In(r, unicode.Pi, unicode.Pf) || strings.ContainsRune("<>\"'\u0060，。；：！？、（）《》【】「」", r):
			end = n
			break scan
		case strings.ContainsRune("([{", r):
			opens[strings.IndexRune("([{", r)]++
		case strings.ContainsRune(")]}", r):
			slot := strings.IndexRune(")]}", r)
			if opens[slot] == 0 {
				end = n
				break scan
			}
			opens[slot]--
		}
	}
	return strings.TrimRight(rest[:end], ".,!;:")
}

func publicHost(host string) bool {
	if !service.ColdPublicHostname(host) {
		return false
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return true
	}
	// Browsers can reinterpret a numeric final DNS label as a non-canonical
	// IPv4 address (octal, hex, or fewer components). A DNS lookup is forbidden.
	labels := strings.Split(strings.TrimSuffix(host, "."), ".")
	last := strings.ToLower(labels[len(labels)-1])
	if strings.Trim(last, "0123456789") == "" || strings.HasPrefix(last, "0x") {
		return false
	}
	for _, label := range labels {
		if label == "" || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
		for _, r := range label {
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' {
				return false
			}
		}
	}
	return true
}

func (i *importer) publicReference(raw string, policy textPolicy, depth int) (string, int, error) {
	u, err := url.Parse(raw)
	if err != nil || strings.ContainsFunc(raw, unicode.IsSpace) || (u.Scheme != "http" && u.Scheme != "https") || u.Opaque != "" || u.User != nil || strings.HasSuffix(u.Host, ":") ||
		!publicHost(u.Hostname()) || depth > 4 {
		return "", 0, errPrivate
	}
	// Path is unescaped once by net/url. Only a complete URL at a segment
	// boundary gets its own validation, including its own authority and query.
	// The unescaping is for inspection only. The original spellings survive.
	prefix := u.Path
	if at := httpScheme.FindStringIndex(u.Path); at != nil {
		if at[0] == 0 || u.Path[at[0]-1] != '/' {
			return "", 0, errPrivate
		}
		prefix = u.Path[:at[0]]
		if _, _, err := i.publicReference(u.Path[at[0]:], strictText, depth+1); err != nil {
			return "", 0, err
		}
	}
	if strings.Contains(prefix, "//") || strings.ContainsAny(prefix, "?#") {
		return "", 0, errPrivate
	}
	if err := i.urlComponent(prefix); err != nil {
		return "", 0, err
	}
	if err := i.urlComponent(u.Fragment); err != nil {
		return "", 0, err
	}
	if u.RawQuery == "" {
		return raw, 0, nil
	}
	// A v26 TXT citation can name a public relay whose entire query is one
	// percent-encoded HTTPS URL. Reject literal URI/query separators here;
	// ordinary keys, assigned values, and other JSON fields keep their rules.
	if policy == fragmentText && u.Scheme == "https" && !strings.ContainsAny(u.RawQuery, ":/?@&=;\\") {
		inner, err := url.QueryUnescape(u.RawQuery)
		if err == nil && len(inner) >= len("https://") && strings.EqualFold(inner[:len("https://")], "https://") {
			if _, _, err := i.publicReference(inner, fragmentText, depth+1); err != nil {
				return "", 0, err
			}
			return raw, 0, nil
		}
	}
	var kept []string
	count := 0
	for pair := range strings.SplitSeq(u.RawQuery, "&") {
		if strings.ContainsRune(pair, ';') {
			return "", 0, errPrivate
		}
		keyRaw, valueRaw, _ := strings.Cut(pair, "=")
		key, err := url.QueryUnescape(keyRaw)
		if err != nil || i.urlComponent(key) != nil || strings.ContainsAny(key, "?#&;=") {
			return "", 0, errPrivate
		}
		value, err := url.QueryUnescape(valueRaw)
		if err != nil || i.urlComponent(value) != nil {
			return "", 0, errPrivate
		}
		if sensitiveKey(key) {
			if policy != biographyText {
				return "", 0, errPrivate
			}
			count++
			continue
		}
		if policy == biographyText {
			kept = append(kept, pair)
		}
	}
	if count == 0 {
		return raw, 0, nil
	}
	beforeFragment, fragment, hasFragment := strings.Cut(raw, "#")
	base, _, _ := strings.Cut(beforeFragment, "?")
	if len(kept) > 0 {
		base += "?" + strings.Join(kept, "&")
	}
	if hasFragment {
		base += "#" + fragment
	}
	return base, count, nil
}

func (i *importer) urlComponent(s string) error {
	if !utf8.ValidString(s) || strings.ContainsFunc(s, unicode.IsControl) || strings.ContainsRune(s, '\\') || escapedOctet.MatchString(s) {
		// Escapes still present after consuming the containing URL are
		// ambiguous, except inside a recursively parsed complete inner URL.
		return errPrivate
	}
	return i.nonURLText(s)
}

func checkAssignments(text string, followedByValue bool) error {
	lastValue := len(strings.TrimRightFunc(text, unicode.IsSpace))
	for _, pair := range assignment.FindAllStringSubmatchIndex(text, -1) {
		if (followedByValue || pair[1] < lastValue) && sensitiveKey(text[pair[2]:pair[3]]) {
			return errPrivate
		}
	}
	return nil
}

func (i *importer) nonURLText(text string) error {
	return i.nonURLContent(text, false)
}

func (i *importer) nonURLContent(text string, followedByValue bool) error {
	if privateText.MatchString(text) || homePath.MatchString(text) {
		return errPrivate
	}
	if err := checkAssignments(text, followedByValue); err != nil {
		return err
	}
	if escapedOctet.MatchString(text) {
		// Outside a parsed public URL, percent-encoding cannot confer on a
		// local path or credential the privileges of a validated reference.
		decoded, err := url.PathUnescape(text)
		if err != nil || escapedOctet.MatchString(decoded) || !utf8.ValidString(decoded) {
			return errPrivate
		}
		return i.nonURLContent(decoded, followedByValue)
	}
	normalized := strings.ToLower(strings.ReplaceAll(text, "\\", "/"))
	for _, root := range i.roots {
		rest := normalized
		for {
			_, after, found := strings.Cut(rest, root)
			if !found {
				break
			}
			next, _ := utf8.DecodeRuneInString(after)
			if after == "" || unicode.IsSpace(next) || strings.ContainsRune("/,;)'\"]}，。；！？、）》】」”’", next) {
				return errPrivate
			}
			rest = after
		}
	}
	return nil
}
