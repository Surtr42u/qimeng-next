package catalog

import (
	"bytes"
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"qimeng.local/next/server/internal/service"
)

const errPrivate = sourceProblem("nonportable_business_value")

var publicLinks = regexp.MustCompile("(?i)https?://[^\\s<>\"'\u0060]+")
var privateText = regexp.MustCompile(`(?i)(?:[a-z]:[/\\]|\\\\[^\\]+\\|file://|-----BEGIN [A-Z ]*PRIVATE KEY-----|\b(?:bearer|basic)\s+[a-z0-9+/_=.-]{8,}|\b(?:access[_-]?token|api[_-]?key|password|secret)[ \t]*[:=]\s*\S)`)

func sensitiveKey(key string) bool {
	k := strings.ToLower(strings.NewReplacer("-", "", "_", "", ".", "").Replace(key))
	return strings.Contains(k, "password") || strings.Contains(k, "secret") || strings.Contains(k, "credential") ||
		strings.HasSuffix(k, "token") || strings.HasSuffix(k, "apikey") || k == "authorization" || k == "cookie" ||
		k == "privatekey" || k == "signature" || k == "sig" || strings.HasPrefix(k, "xamz") || strings.HasPrefix(k, "xgoog")
}

// The old mirror, diagnostics and authority tables are never copied. Also
// check portable business text, where a hand-written TXT or URL can embed a
// local mount or an authorization parameter. Diagnostics omit the input.
func (i *importer) portableText(text string) error {
	if !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
		return sourceProblem("business_text_invalid")
	}
	// Check links before removing them from the filesystem-path check. Public
	// URL paths can legitimately begin with the same directory as a mount.
	end := 0
	for _, span := range publicLinks.FindAllStringIndex(text, -1) {
		if err := i.nonURLText(text[end:span[0]]); err != nil {
			return err
		}
		link, err := url.Parse(text[span[0]:span[1]])
		if err != nil || link.User != nil || !service.ColdPublicHostname(link.Hostname()) {
			return errPrivate
		}
		params, err := url.ParseQuery(link.RawQuery)
		if err != nil {
			return errPrivate
		}
		for key, values := range params {
			if sensitiveKey(key) {
				return errPrivate
			}
			for _, value := range values {
				if err := i.nonURLText(value); err != nil {
					return err
				}
			}
		}
		if privateText.MatchString(link.Fragment) || privateText.MatchString(link.Path) {
			return errPrivate
		}
		end = span[1]
	}
	return i.nonURLText(text[end:])
}

func (i *importer) nonURLText(text string) error {
	if privateText.MatchString(text) {
		return errPrivate
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

// Decode numbers as numbers to avoid binary-float loss in source records.
// The actual archive stores the original json.RawMessage.
func (i *importer) portableJSON(raw json.RawMessage, profile bool) error {
	if !json.Valid(raw) || !utf8.Valid(raw) {
		return sourceProblem("business_json_invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return sourceProblem("business_json_invalid")
	}
	var visit func(any, bool) error
	visit = func(v any, top bool) error {
		switch x := v.(type) {
		case map[string]any:
			for key, child := range x {
				if sensitiveKey(key) {
					return errPrivate
				}
				if err := i.portableText(key); err != nil {
					return err
				}
				if top && profile && key == "avatarData" {
					continue // decoded and validated as an offline image before commit
				}
				if err := visit(child, false); err != nil {
					return err
				}
			}
		case []any:
			for _, child := range x {
				if err := visit(child, false); err != nil {
					return err
				}
			}
		case string:
			return i.portableText(x)
		}
		return nil
	}
	return visit(value, true)
}
