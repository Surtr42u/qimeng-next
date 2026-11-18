package domain

import (
	"path"
	"strconv"
	"strings"
)

// NamingFamily separates canonical display names from confirmed disk spelling.
// Identity lookup and evidence decisions stay outside this pure formatter.
func NamingFamily(source string, characters []string, fallback string) string {
	sources := strings.Split(source, "+")
	for i, name := range sources {
		switch name {
		case "死或生", "生或死5":
			sources[i] = "生或死"
		}
	}
	work := strings.Join(sources, "+")
	if work != "" && len(characters) > 0 {
		return work + "  " + strings.Join(characters, "&")
	}
	if work != "" {
		return work
	}
	return strings.TrimSpace(fallback)
}

// NextChapter reads the same collection's video names, including user notes.
// It advances beyond the maximum episode rather than filling image-work gaps.
func NextChapter(family string, names []string, committedMax int) int {
	maximum := committedMax
	for _, name := range names {
		if NumberClass(name) != "video" {
			continue
		}
		stem := strings.TrimSuffix(name, path.Ext(name))
		if !strings.HasPrefix(stem, family) {
			continue
		}
		tail := strings.TrimLeft(stem[len(family):], " ")
		end := 0
		for end < len(tail) && tail[end] >= '0' && tail[end] <= '9' {
			end++
		}
		if end == 0 {
			continue
		}
		number, e := strconv.Atoi(tail[:end])
		if e == nil && number > maximum {
			maximum = number
		}
	}
	return maximum + 1
}
