package domain

import (
	"strings"
	"unicode/utf8"
)

func asciiFold(value string) string {
	bytes := []byte(value)
	for i, c := range bytes {
		if c >= 'A' && c <= 'Z' {
			bytes[i] = c + ('a' - 'A')
		}
	}
	return string(bytes)
}
func asciiDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, c := range []byte(value) {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
func searchBrackets(value string) string {
	return strings.NewReplacer("（", "(", "[", "(", "【", "(", "）", ")", "]", ")", "】", ")").Replace(value)
}

// SearchToken is shared by every SQL list/count/facet query. Numbers match
// complete ASCII digit runs; naked numbers cannot stand in for bracketed pages.
func SearchToken(text, token string) bool {
	token = asciiFold(token)
	text = asciiFold(text)
	page := searchBrackets(token)
	if len(page) >= 3 && page[0] == '(' && page[len(page)-1] == ')' && asciiDigits(page[1:len(page)-1]) {
		return strings.Contains(searchBrackets(text), page)
	}
	if !asciiDigits(token) {
		return strings.Contains(text, token)
	}
	for offset := 0; offset <= len(text)-len(token); {
		at := strings.Index(text[offset:], token)
		if at < 0 {
			return false
		}
		at += offset
		end := at + len(token)
		before := at == 0 || text[at-1] < '0' || text[at-1] > '9'
		after := end == len(text) || text[end] < '0' || text[end] > '9'
		if before && after {
			previous, _ := utf8.DecodeLastRuneInString(text[:at])
			if !strings.ContainsRune("(（[【", previous) {
				return true
			}
		}
		offset = at + 1
	}
	return false
}
