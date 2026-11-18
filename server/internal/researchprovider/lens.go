package researchprovider

import (
	"encoding/json"
	"net/http"
	"strings"
)

type ImageHint struct {
	Domain string `json:"domain"`
	Title  string `json:"title,omitempty"`
}

// ParseLens accepts the installed sidecar's wire format. Page text can suggest a
// domain without providing a link. Keep that distinction in the receipt.
func ParseLens(status int, raw []byte, expectedSHA string) (string, []Item, []ImageHint) {
	var response struct {
		OK           bool   `json:"ok"`
		Status       string `json:"status"`
		FrameSHA256  string `json:"frame_sha256"`
		Links, Cites []struct{ URL, Title string }
		TopTitles    []ImageHint
		TopDomains   []string
		Error        struct{ Code string }
	}
	if json.Unmarshal(raw, &response) != nil {
		return "unavailable", nil, nil
	}
	if status != http.StatusOK || !response.OK || response.Status != "ok" {
		switch {
		case status == 503 && response.Error.Code == "BLOCKED_BOT":
			return "blocked", nil, nil
		case status == 429 && response.Error.Code == "BUSY":
			return "rate_limited", nil, nil
		default:
			return "unavailable", nil, nil
		}
	}
	if response.FrameSHA256 != expectedSHA || expectedSHA == "" {
		return "unavailable", nil, nil
	}
	items := []Item{}
	seenLinks := map[string]bool{}
	for _, candidate := range append(response.Links, response.Cites...) {
		link := cleanLink(candidate.URL)
		if link != "" && !seenLinks[link] {
			seenLinks[link] = true
			items = append(items, Item{URL: link, Title: short(candidate.Title, 300)})
			if len(items) == 20 {
				break
			}
		}
	}
	for _, domain := range response.TopDomains {
		response.TopTitles = append(response.TopTitles, ImageHint{Domain: domain})
	}
	hints := []ImageHint{}
	seenHints := map[ImageHint]bool{}
	for _, hint := range response.TopTitles {
		hint.Domain = strings.ToLower(strings.TrimSpace(hint.Domain))
		host := publicURL("https://"+hint.Domain, false)
		if host == nil || host.Host != hint.Domain || len(hint.Domain) > 253 {
			continue
		}
		hint.Title = short(hint.Title, 300)
		if !seenHints[hint] {
			seenHints[hint] = true
			hints = append(hints, hint)
			if len(hints) == 20 {
				break
			}
		}
	}
	if len(items) == 0 && len(hints) == 0 {
		return "not_found", nil, nil
	}
	return "ok", items, hints
}
