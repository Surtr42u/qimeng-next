package catalog

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"qimeng.local/next/server/internal/authorlist"
	"qimeng.local/next/server/internal/domain"
	"qimeng.local/next/server/internal/service"
	"qimeng.local/next/server/internal/store"
)

type fragment struct {
	Filename   string `json:"filename"`
	Content    string `json:"content"`
	ImportedAt string `json:"importedAt"`
}

type protectedEntry struct {
	AuthorID    string   `json:"authorId"`
	DisplayName string   `json:"displayName"`
	Names       []string `json:"names"`
	Works       []string `json:"works"`
	Sources     []string `json:"sources"`
}

type protectedKey struct{ author, work string }
type protectedWork struct {
	list    string
	matched bool
}

// A v26 numbered header may use a single space. Existing work lines, their
// spacing, and the archived original bytes must never be rewritten.
var shortHeader = regexp.MustCompile(`^([0-9]+)[\t ](\S.*)$`)

func v26Document(input string) (authorlist.Document, error) {
	lines := strings.Split(strings.TrimPrefix(input, "\ufeff"), "\n")
	for n, line := range lines {
		parts := shortHeader.FindStringSubmatch(strings.TrimSpace(line))
		if parts != nil && authorlist.CompileWork(parts[2]).Extension == "" {
			lines[n] = parts[1] + "  " + parts[2]
		}
	}
	return authorlist.Parse(strings.Join(lines, "\n"))
}

func (i *importer) importLists() error {
	i.protected = map[protectedKey][]*protectedWork{}
	var fragments []fragment
	protections := map[string][]protectedEntry{}
	table := i.settings["imported_txt_sources"]
	if table.Raw != nil && string(table.Raw) != "null" {
		if err := decode(table.Raw, &fragments); err != nil {
			return err
		}
	}
	if v := i.settings["imported_txt_upload_entries"]; v.Raw != nil && string(v.Raw) != "null" {
		if err := decode(v.Raw, &protections); err != nil {
			return err
		}
	}

	counts := map[string]int{}
	for _, f := range fragments {
		counts[f.Filename]++
	}
	homes := map[string]bool{}
	for position, f := range fragments {
		if !domain.ValidName(f.Filename) {
			return errors.New("cold catalog: invalid TXT fragment filename")
		}
		observed := f.ImportedAt
		if observed == "" {
			observed = table.At
		}
		at, err := stamp(observed)
		if err != nil {
			return err
		}
		sum := sha256.Sum256([]byte(f.Content))
		hash := hex.EncodeToString(sum[:])
		index := strconv.Itoa(position)
		if err := i.exec(`INSERT INTO author_txt_fragments(id,name,sha256,content,created_at,source_id) VALUES(?,?,?,?,?,?)
			ON CONFLICT(source_id,name,sha256) DO NOTHING`, stableID(i.sourceID, "fragment", index), f.Filename, hash, f.Content, observed, i.sourceID); err != nil {
			return err
		}
		doc, err := v26Document(f.Content)
		if err != nil || counts[f.Filename] > 1 || !strings.EqualFold(path.Ext(f.Filename), ".txt") {
			if err := i.origin("unresolved-list", []string{index}, map[string]any{"file": f.Filename, "position": position, "reason": "ambiguous_name_or_unsupported_grammar"}); err != nil {
				return err
			}
			continue
		}

		listID := stableID(i.sourceID, "list", f.Filename)
		members := make([]service.ColdListMember, 0, len(doc.Blocks))
		for blockIndex, block := range doc.Blocks {
			oldID, resolvable := i.blockAuthor(block, protections[f.Filename])
			if !resolvable {
				if err := i.origin("unresolved-list-member", []string{index, strconv.Itoa(blockIndex)},
					map[string]any{"file": f.Filename, "block": blockIndex, "reason": "identity_unresolved"}); err != nil {
					return err
				}
				continue
			}
			a := i.authors[oldID]
			members = append(members, service.ColdListMember{AuthorID: a.ID, Declaration: block})
			if doc.Format != "blocks" || homes[a.ID] {
				continue
			}
			// v26 reads the FIRST numbered declaration, including an explicitly
			// empty source section. It kept no separate "edited" flag. Preserve
			// that exact result as an override; unioning subsequent fragments
			// could undo a user's replacement/clear.
			homes[a.ID] = true
			a.AcquisitionSources = append([]string{}, block.Sources...)
			aliases := append([]string{}, a.Aliases...)
			for _, alias := range block.Names {
				if !slices.Contains(aliases, alias) {
					aliases = append(aliases, alias)
				}
			}
			a.Aliases = aliases
			raw, err := store.Encode(a)
			if err != nil {
				return err
			}
			if err := i.exec("UPDATE authors SET data=? WHERE id=?", raw, a.ID); err != nil {
				return err
			}
			i.authors[oldID] = a
			if err := i.exec("INSERT INTO author_source_overrides(author_id) VALUES(?)", a.ID); err != nil {
				return err
			}
		}
		// Plain lists have membership/order but no work rules. The raw input
		// preserves their line layout for byte-identical recovery.
		if len(members) > 0 {
			if err := service.InsertColdAuthorList(i.ctx, i.target, listID, f.Filename, hash, at, members); err != nil {
				return err
			}
		}
		bound := map[string]bool{}
		for _, m := range members {
			bound[m.AuthorID] = true
		}
		for n, entry := range protections[f.Filename] {
			a, ok := i.authors[entry.AuthorID]
			if !ok || !bound[a.ID] {
				if err := i.origin("unresolved-list-protection", []string{index, strconv.Itoa(n)},
					map[string]any{"file": f.Filename, "entry": n, "reason": "owner_not_bound"}); err != nil {
					return err
				}
				continue
			}
			for _, work := range entry.Works {
				key := protectedKey{a.ID, work}
				i.protected[key] = append(i.protected[key], &protectedWork{list: listID})
			}
		}
	}
	for file := range protections {
		if counts[file] == 0 {
			if err := i.origin("unresolved-list-protection", []string{file},
				map[string]string{"file": file, "reason": "owning_fragment_absent"}); err != nil {
				return err
			}
		}
	}
	return nil
}

func (i *importer) blockAuthor(block authorlist.Block, entries []protectedEntry) (string, bool) {
	key := authorlist.IdentityKey(block.Names[0])
	id := ""
	// The v26 first-alias key is the declared author ID, not a request to
	// merge equal display names. An explicit registration can bind an opaque
	// historical ID. Conflicting evidence stays in the original fragment.
	if a, ok := i.authors[key]; ok && (a.Kind == "regular" || a.Placeholder) {
		id = key
	}
	for _, e := range entries {
		names := e.Names
		if len(names) == 0 {
			names, _ = authorlist.Names(e.DisplayName)
		}
		if len(names) == 0 || authorlist.IdentityKey(names[0]) != key {
			continue
		}
		a, exists := i.authors[e.AuthorID]
		if !exists || (a.Kind != "regular" && !a.Placeholder) || (id != "" && id != e.AuthorID) {
			return "", false
		}
		id = e.AuthorID
	}
	return id, id != ""
}
