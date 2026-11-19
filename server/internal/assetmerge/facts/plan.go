// Package facts projects an owner's selected image groups inside a private,
// offline native restore. The caller verifies the archive and physical originals.
package facts

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"
	"time"

	"qimeng.local/next/server/internal/domain"
)

type Plan struct {
	BaselineSHA256 string  `json:"baselineSHA256"`
	SourceID       string  `json:"sourceId"`
	Groups         []Group `json:"groups"`
}

type Group struct {
	RepresentativeID string   `json:"representativeId"`
	LibraryID        string   `json:"libraryId"`
	RelativePath     string   `json:"relativePath"`
	SHA256           string   `json:"sha256"`
	Members          []Member `json:"members"`
	Sources          []Source `json:"sources"`
}

type Member struct {
	AssetID              string   `json:"assetId"`
	SourceKeys           []string `json:"sourceKeys,omitempty"`
	ExpectedRelativePath string   `json:"expectedRelativePath,omitempty"`
	ExpectedSize         *int64   `json:"expectedSize,omitempty"`
	ExpectedVersion      *int64   `json:"expectedVersion,omitempty"`
}

type Source struct {
	Key      string `json:"key"`
	URL      string `json:"url"`
	At       string `json:"at"`
	Caption  string `json:"caption,omitempty"`
	MediaURL string `json:"mediaUrl,omitempty"`
}

type Result struct {
	Groups         int  `json:"groups"`
	Members        int  `json:"members"`
	Donors         int  `json:"donors"`
	AlreadyApplied bool `json:"alreadyApplied"`
}

func digest(value string) bool {
	if len(value) != sha256.Size*2 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func sourceURL(value string) bool {
	u, err := url.Parse(value)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Hostname() != "" && u.User == nil
}

func (g Group) validate() error {
	kind, _, ok := domain.MediaType(path.Base(g.RelativePath))
	if !domain.ValidName(g.LibraryID) || !domain.ValidMediaPath(g.RelativePath) || !ok || kind != "image" || !digest(g.SHA256) {
		return errors.New("invalid image destination or expected digest")
	}
	if len(g.Members) < 2 {
		return errors.New("a selected group requires at least two physical members")
	}
	sources := map[string]bool{}
	for _, source := range g.Sources {
		if strings.TrimSpace(source.Key) == "" || sources[source.Key] || !sourceURL(source.URL) || (source.MediaURL != "" && !sourceURL(source.MediaURL)) {
			return errors.New("invalid or duplicate source evidence")
		}
		if source.At != "" {
			if _, err := time.Parse(time.RFC3339Nano, source.At); err != nil {
				return errors.New("invalid source timestamp")
			}
		}
		sources[source.Key] = true
	}
	found := false
	for _, member := range g.Members {
		if !domain.ValidName(member.AssetID) {
			return errors.New("invalid physical asset identity")
		}
		found = found || member.AssetID == g.RepresentativeID
		keys := map[string]bool{}
		for _, key := range member.SourceKeys {
			if !sources[key] || keys[key] {
				return errors.New("unknown or repeated source evidence for a member")
			}
			keys[key] = true
		}
	}
	if !found {
		return errors.New("representative is missing from the reviewed members")
	}
	return nil
}

func (p Plan) validate() (Result, error) {
	sourceDigest, ok := strings.CutPrefix(p.SourceID, "legacy-cold-v26:")
	if !ok || !digest(sourceDigest) || !digest(p.BaselineSHA256) || len(p.Groups) == 0 {
		return Result{}, errors.New("a sealed v26 source, native archive digest and explicit groups are required")
	}
	members := map[string]bool{}
	destinations := map[string]bool{}
	for i, g := range p.Groups {
		if err := g.validate(); err != nil {
			return Result{}, fmt.Errorf("group %d: %w", i+1, err)
		}
		dest := strings.ToLower(g.LibraryID + "/" + g.RelativePath)
		if destinations[dest] {
			return Result{}, errors.New("two groups claim the same destination")
		}
		destinations[dest] = true
		for _, m := range g.Members {
			if members[m.AssetID] {
				return Result{}, errors.New("a physical member occurs more than once in the plan")
			}
			members[m.AssetID] = true
		}
	}
	return Result{Groups: len(p.Groups), Members: len(members), Donors: len(members) - len(p.Groups)}, nil
}
