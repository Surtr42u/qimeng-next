// Package sourcerules matches filenames against an immutable reviewed vocabulary.
package sourcerules

import (
	_ "embed"
	"encoding/json"
	"errors"
	"path"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Only the approved vocabulary data was transcribed from the previous project.
//
//go:embed baseline.json
var baselineJSON []byte

type Character struct {
	Canonical string   `json:"canonical"`
	Aliases   []string `json:"aliases"`
}
type Group struct {
	Canonical  string      `json:"canonical"`
	Variants   []string    `json:"variants"`
	Characters []Character `json:"characters"`
}
type Layer struct {
	Groups    []Group  `json:"groups"`
	StopWords []string `json:"stopWords"`
}
type Match struct {
	Source     string
	Characters []string
}
type Result struct {
	Matches    []Match
	Candidates []string
}
type spelling struct {
	key    string
	groups []string
}
type Index struct {
	prefixes   []spelling
	characters map[string]map[string][]string
	stop       map[string]bool
}

var frozen []Group

func init() {
	if err := json.Unmarshal(baselineJSON, &frozen); err != nil {
		panic(err)
	}
}

func cleanName(v string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" || utf8.RuneCountInString(v) > 100 || strings.IndexFunc(v, unicode.IsControl) >= 0 {
		return "", errors.New("invalid vocabulary term")
	}
	return v, nil
}
func names(in []string, limit int) ([]string, error) {
	if in == nil || len(in) > limit {
		return nil, errors.New("invalid vocabulary list")
	}
	out := []string{}
	seen := map[string]bool{}
	for _, v := range in {
		if strings.TrimSpace(v) == "" {
			continue
		}
		v, err := cleanName(v)
		if err != nil {
			return nil, err
		}
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out, nil
}
func Normalize(in Layer) (Layer, error) {
	out := Layer{Groups: []Group{}}
	var err error
	if in.Groups == nil || len(in.Groups) > 256 {
		return out, errors.New("invalid group count")
	}
	out.StopWords, err = names(in.StopWords, 256)
	if err != nil {
		return out, err
	}
	seen := map[string]bool{}
	for _, g := range in.Groups {
		g.Canonical, err = cleanName(g.Canonical)
		if err != nil {
			return out, err
		}
		g.Variants, err = names(g.Variants, 64)
		if err != nil {
			return out, err
		}
		if g.Characters == nil || len(g.Characters) > 128 {
			return out, errors.New("invalid character count")
		}
		chars := []Character{}
		used := map[string]bool{}
		for _, c := range g.Characters {
			c.Canonical, err = cleanName(c.Canonical)
			if err != nil {
				return out, err
			}
			c.Aliases, err = names(c.Aliases, 32)
			if err != nil {
				return out, err
			}
			if !used[c.Canonical] {
				used[c.Canonical] = true
				chars = append(chars, c)
			}
		}
		g.Characters = chars
		if !seen[g.Canonical] {
			seen[g.Canonical] = true
			out.Groups = append(out.Groups, g)
		}
	}
	return out, nil
}
func folded(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return unicode.ToLower(r)
	}, s)
}
func add(v []string, name string) []string {
	for _, old := range v {
		if old == name {
			return v
		}
	}
	return append(v, name)
}
func Build(layer Layer) *Index {
	index := &Index{characters: map[string]map[string][]string{}, stop: map[string]bool{}}
	prefixes := map[string][]string{}
	for _, word := range append([]string{"触手", "白丝", "婚纱", "多角色", "多角色酒吧"}, layer.StopWords...) {
		index.stop[strings.TrimSpace(word)] = true
	}
	for _, set := range [][]Group{frozen, layer.Groups} {
		for _, g := range set {
			for _, v := range append([]string{g.Canonical}, g.Variants...) {
				if key := folded(v); key != "" {
					prefixes[key] = add(prefixes[key], g.Canonical)
				}
			}
			aliases := index.characters[g.Canonical]
			if aliases == nil {
				aliases = map[string][]string{}
				index.characters[g.Canonical] = aliases
			}
			for _, c := range g.Characters {
				for _, a := range append([]string{c.Canonical}, c.Aliases...) {
					if key := folded(a); key != "" {
						aliases[key] = add(aliases[key], c.Canonical)
					}
				}
			}
		}
	}
	for k, g := range prefixes {
		sort.Strings(g)
		index.prefixes = append(index.prefixes, spelling{k, g})
	}
	sort.Slice(index.prefixes, func(a, b int) bool {
		l, r := index.prefixes[a].key, index.prefixes[b].key
		if len(l) != len(r) {
			return len(l) > len(r)
		}
		return l < r
	})
	return index
}

// consumePrefix maps a normalized prefix to a byte boundary in the original spelling.
func consumePrefix(original, key string) int {
	wanted := []rune(key)
	pos := 0
	for at, r := range original {
		if unicode.IsSpace(r) {
			continue
		}
		if pos >= len(wanted) || unicode.ToLower(r) != wanted[pos] {
			return -1
		}
		pos++
		if pos == len(wanted) {
			return at + utf8.RuneLen(r)
		}
	}
	return -1
}
func (i *Index) prefix(s string) ([]string, int) {
	f := folded(s)
	for _, p := range i.prefixes {
		if strings.HasPrefix(f, p.key) {
			return p.groups, consumePrefix(s, p.key)
		}
	}
	return nil, 0
}
func asciiDigit(r byte) bool  { return r >= '0' && r <= '9' }
func asciiLetter(r byte) bool { return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' }
func digits(s string) int {
	n := 0
	for n < len(s) && asciiDigit(s[n]) {
		n++
	}
	return n
}
func roleSeparator(r rune) bool { return unicode.IsSpace(r) || r == '+' || r == '&' }
func isSerial(v string) bool {
	if len(v) > 2 && v[0] == '(' && v[len(v)-1] == ')' {
		v = v[1 : len(v)-1]
	}
	return len(v) > 0 && digits(v) == len(v)
}
func beforeSerial(text string) string {
	start := -1
	for pos, r := range text {
		if roleSeparator(r) {
			if start >= 0 && isSerial(text[start:pos]) {
				return text[:start]
			}
			start = -1
		} else if start < 0 {
			start = pos
		}
	}
	if start >= 0 && isSerial(text[start:]) {
		return text[:start]
	}
	return text
}
func (i *Index) roles(source, rest string) ([]string, []string) {
	// A version can be attached directly to a source spelling. A separate number starts the owner's filename note.
	n := digits(rest)
	if n > 0 && (n == len(rest) || !asciiLetter(rest[n])) {
		rest = rest[n:]
	}
	rest = beforeSerial(rest)
	tokens := strings.FieldsFunc(rest, roleSeparator)
	aliases := i.characters[source]
	key := folded(rest)
	seen := map[string]bool{}
	ambiguous := []string{}
	for a, options := range aliases {
		if !strings.Contains(key, a) {
			continue
		}
		if len(options) > 1 {
			for _, name := range options {
				ambiguous = add(ambiguous, source+" / "+name)
			}
		} else {
			seen[options[0]] = true
		}
	}
	if len(ambiguous) > 0 {
		sort.Strings(ambiguous)
		return nil, ambiguous
	}
	if len(seen) == 0 {
		for _, v := range tokens {
			if strings.EqualFold(v, "x") {
				continue
			}
			n := digits(v)
			if n > 0 && n < len(v) && asciiLetter(v[n]) || i.stop[v] {
				continue
			}
			if name, e := cleanName(v); e == nil {
				seen[name] = true
			}
		}
	}
	out := make([]string, 0, len(seen))
	for v := range seen {
		out = append(out, v)
	}
	sort.Strings(out)
	return out, nil
}
func (i *Index) Match(filename string) Result {
	stem := strings.TrimSuffix(path.Base(filename), path.Ext(filename))
	segments := strings.Split(stem, "+")
	result := Result{}
	var group, rest string
	flush := func() {
		if group == "" {
			return
		}
		names, conflict := i.roles(group, rest)
		if len(conflict) > 0 {
			result.Candidates = append(result.Candidates, conflict...)
			return
		}
		for n, m := range result.Matches {
			if m.Source == group {
				for _, name := range names {
					result.Matches[n].Characters = add(result.Matches[n].Characters, name)
				}
				sort.Strings(result.Matches[n].Characters)
				return
			}
		}
		result.Matches = append(result.Matches, Match{group, names})
	}
	for _, piece := range segments {
		sources, end := i.prefix(piece)
		if len(sources) == 0 {
			if group != "" {
				rest += "+" + piece
			}
			continue
		}
		flush()
		group = ""
		rest = ""
		if len(sources) > 1 {
			for _, name := range sources {
				result.Candidates = add(result.Candidates, name)
			}
			continue
		}
		group = sources[0]
		rest = piece[end:]
	}
	flush()
	sort.Strings(result.Candidates)
	if len(result.Candidates) > 0 {
		result.Matches = nil
	}
	return result
}
