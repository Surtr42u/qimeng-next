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

// SearchTokenKind classifies a query token. SQL predicates reuse the same
// classification as SearchToken so the materialised-text probe and the Go
// function cannot drift.
type SearchTokenKind int

const (
	SearchTokenContains SearchTokenKind = iota // literal substring
	SearchTokenPage                            // whole bracketed page number
	SearchTokenNumber                          // standalone ASCII digit run
)

// SearchTokenPattern ASCII-folds token once and reports how it must be
// matched. folded is the literal a SQL prefilter may require; page is the
// bracket-normalised page form for SearchTokenPage.
func SearchTokenPattern(token string) (kind SearchTokenKind, folded, page string) {
	folded = asciiFold(token)
	page = searchBrackets(folded)
	if len(page) >= 3 && page[0] == '(' && page[len(page)-1] == ')' && asciiDigits(page[1:len(page)-1]) {
		return SearchTokenPage, folded, page
	}
	if !asciiDigits(folded) {
		return SearchTokenContains, folded, ""
	}
	return SearchTokenNumber, folded, ""
}

// SearchTokenDigits reports whether folded is a non-empty run of ASCII
// digits. The SQL digit-run GLOB is only safe for such tokens.
func SearchTokenDigits(folded string) bool { return asciiDigits(folded) }

// SearchToken is shared by every SQL list/count/facet query. Numbers match
// complete ASCII digit runs; naked numbers cannot stand in for bracketed pages.
func SearchToken(text, token string) bool {
	kind, folded, page := SearchTokenPattern(token)
	text = asciiFold(text)
	switch kind {
	case SearchTokenPage:
		return strings.Contains(searchBrackets(text), page)
	case SearchTokenContains:
		return strings.Contains(text, folded)
	}
	for offset := 0; offset <= len(text)-len(folded); {
		at := strings.Index(text[offset:], folded)
		if at < 0 {
			return false
		}
		at += offset
		end := at + len(folded)
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
