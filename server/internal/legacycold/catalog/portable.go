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

var publicLinks = regexp.MustCompile("(?i)https?://[^\\s<>\"'\u0060，。；！？、（）《》【】「」]+")
var httpScheme = regexp.MustCompile(`(?i)https?://`)
var escapedOctet = regexp.MustCompile(`%[0-9a-fA-F]{2}`)
var privateText = regexp.MustCompile(`(?i)(?:[a-z]:[/\\]|\\\\[^\\]+\\|file://|-----BEGIN [A-Z ]*PRIVATE KEY-----|\b(?:bearer|basic)\s+[a-z0-9+/_=.-]{8,})`)
var homePath = regexp.MustCompile(`(?i)/(?:home|users)/[^/ \t\r\n]+/(?:\.ssh|\.config|desktop|documents|downloads|library)(?:/|$)`)
var assignment = regexp.MustCompile(`(?i)(?:^|[\s?&#;/,(\[])([a-z][a-z0-9_.-]*)[ \t]*[:=][ \t]*\S`)

func sensitiveKey(key string) bool {
	k := strings.ToLower(strings.NewReplacer("-", "", "_", "", ".", "").Replace(key))
	return strings.Contains(k, "password") || strings.Contains(k, "secret") || strings.Contains(k, "credential") ||
		strings.HasSuffix(k, "token") || strings.HasSuffix(k, "apikey") || k == "authorization" || k == "cookie" ||
		k == "privatekey" || k == "signature" || k == "sig" || strings.HasPrefix(k, "xamz") || strings.HasPrefix(k, "xgoog")
}

func (i *importer) portableText(text string) error {
	_, _, err := i.inspectText(text, false)
	return err
}

// Only a known descriptive profile field can request removal. Ordinary text,
// structured URL fields and inner URLs always require an already portable value.
func (i *importer) inspectText(text string, removeQuery bool) (string, int, error) {
	if !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
		return "", 0, sourceProblem("business_text_invalid")
	}
	var out strings.Builder
	end, removed := 0, 0
	for _, span := range publicLinks.FindAllStringIndex(text, -1) {
		if err := i.nonURLText(text[end:span[0]]); err != nil {
			return "", 0, err
		}
		token := text[span[0]:span[1]]
		if removeQuery {
			token = trimProsePunctuation(token)
		}
		safe, count, err := i.publicReference(token, removeQuery, 0)
		if err != nil {
			return "", 0, err
		}
		if removeQuery {
			out.WriteString(text[end:span[0]])
			out.WriteString(safe)
		}
		removed += count
		end = span[0] + len(token)
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

func trimProsePunctuation(link string) string {
	for len(link) > 0 {
		switch last := link[len(link)-1]; last {
		case '.', ',', '!', ';', ':':
		case ')', ']', '}':
			open := map[byte]string{')': "(", ']': "[", '}': "{"}[last]
			if strings.Count(link, string(last)) <= strings.Count(link, open) {
				return link
			}
		default:
			return link
		}
		link = link[:len(link)-1]
	}
	return link
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

func (i *importer) publicReference(raw string, removeQuery bool, depth int) (string, int, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Opaque != "" || u.User != nil ||
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
		if _, _, err := i.publicReference(u.Path[at[0]:], false, depth+1); err != nil {
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
			if !removeQuery {
				return "", 0, errPrivate
			}
			count++
			continue
		}
		if removeQuery {
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

func (i *importer) nonURLText(text string) error {
	if privateText.MatchString(text) || homePath.MatchString(text) {
		return errPrivate
	}
	for _, pair := range assignment.FindAllStringSubmatch(text, -1) {
		if sensitiveKey(pair[1]) {
			return errPrivate
		}
	}
	if escapedOctet.MatchString(text) {
		// Outside a parsed public URL, percent-encoding cannot confer on a
		// local path or credential the privileges of a validated reference.
		decoded, err := url.PathUnescape(text)
		if err != nil || escapedOctet.MatchString(decoded) || !utf8.ValidString(decoded) {
			return errPrivate
		}
		return i.nonURLText(decoded)
	}
	normalized := strings.ToLower(strings.ReplaceAll(text, "\\", "/"))
	for _, root := range i.roots {
		rest := normalized
		for {
			_, after, found := strings.Cut(rest, root)
			if !found {
				break
			}
			if after == "" || strings.ContainsAny(after[:1], "/ \t\r\n,;)'\"]}") {
				return errPrivate
			}
			rest = after
		}
	}
	return nil
}
