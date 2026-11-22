package main

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// startupFailureDetailLimit bounds the text the supervised Android host can show. Release
// builds discard the child's output, so this document is the only diagnosis channel besides
// the exit code: a malformed catalog, another owner of the data directory and an unfinished
// restore all end in the same exit status 1.
const startupFailureDetailLimit = 400

// startupFailureDocument is written next to the startup receipt as <receipt>.failed. The host
// accepts it only for the launch identity of the attempt that just exited.
type startupFailureDocument struct {
	LaunchID string `json:"launchId"`
	Detail   string `json:"detail"`
}

// privatePathPattern matches an Android data or storage path that is not one of the configured
// roots (those are removed by exact value first). The one-byte prefix is preserved so sentence
// spacing and punctuation stay readable.
var privatePathPattern = regexp.MustCompile(`(^|[^A-Za-z0-9_])(/(?:data|storage)/[^\s:;,()\[\]{}"']*)`)

// startupFailureRedactions collects the configured values whose literal text must never reach
// the failure detail: credentials, private roots and packaged helper paths. It must run before
// run(), which unsets QIMENG_ADMIN_PASSWORD.
func startupFailureRedactions(getenv func(string) string) []string {
	values := []string{
		getenv("QIMENG_DATA_DIR"),
		getenv("QIMENG_ADMIN_TOKEN"),
		getenv("QIMENG_ADMIN_PASSWORD"),
		getenv("QIMENG_FFMPEG"),
		getenv("QIMENG_FFPROBE"),
	}
	if raw := getenv("QIMENG_STORAGE_ROOTS"); raw != "" {
		var roots map[string]string
		if e := json.Unmarshal([]byte(raw), &roots); e == nil {
			for _, root := range roots {
				values = append(values, root)
			}
		}
		// The raw declaration is not normally part of an error, but it is the only form that
		// contains every root when the JSON itself is rejected.
		values = append(values, raw)
	}
	redactions := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" {
			redactions = append(redactions, value)
		}
	}
	// Longest first: a media root can live inside the data directory, and replacing the short
	// value first would leave the rest of the longer path in the message.
	sort.Slice(redactions, func(i, j int) bool { return len(redactions[i]) > len(redactions[j]) })
	return redactions
}

// sanitizeStartupFailure turns the run() error into one bounded line. Configured values are
// replaced before generic path matching so a known root never survives as a suffix. Single
// pass replacement avoids re-redacting text that was just inserted.
func sanitizeStartupFailure(detail string, redactions []string) string {
	sanitized := detail
	if len(redactions) > 0 {
		pairs := make([]string, 0, len(redactions)*2)
		for _, value := range redactions {
			pairs = append(pairs, value, "<redacted>")
		}
		sanitized = strings.NewReplacer(pairs...).Replace(sanitized)
	}
	sanitized = privatePathPattern.ReplaceAllString(sanitized, "${1}<path>")
	sanitized = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, sanitized)
	sanitized = strings.Join(strings.Fields(sanitized), " ")
	if runes := []rune(sanitized); len(runes) > startupFailureDetailLimit {
		sanitized = string(runes[:startupFailureDetailLimit])
	}
	if sanitized == "" {
		return "本机服务未能启动，请核对本机数据与配置"
	}
	return sanitized
}

// publishStartupFailure writes <receipt>.failed for the supervised host. Without a receipt and
// launch identity the process is not supervised and nothing is written.
func publishStartupFailure(receiptPath, launchID, detail string) error {
	if receiptPath == "" || launchID == "" {
		return nil
	}
	if !filepath.IsAbs(receiptPath) {
		return errors.New("startup failure receipt path must be absolute")
	}
	return publishPrivateJSON(receiptPath+".failed", startupFailureDocument{LaunchID: launchID, Detail: detail})
}
