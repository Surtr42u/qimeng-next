package facts

import (
	"errors"
	"fmt"
	"slices"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/store"
)

func appendDistinct(list []string, values ...string) []string {
	for _, value := range values {
		if !slices.Contains(list, value) {
			list = append(list, value)
		}
	}
	return list
}

// A singleton field can carry an actually recorded value from a member.
// Two different nonempty values require a separate decision by the owner.
func takeValue(field string, target *string, value string) error {
	if value == "" {
		return nil
	}
	if *target != "" && *target != value {
		return fmt.Errorf("conflicting member values for %s", field)
	}
	*target = value
	return nil
}

type catalogUnion struct {
	authors, tags, channels, sources, characters, characterText []string
	note, sourceText, cosWork, cosName, cosAuthor               string
}

func (u *catalogUnion) add(m originalMember) error {
	a := m.asset
	for _, id := range a.AuthorIDs {
		if id != "unknown" {
			u.authors = appendDistinct(u.authors, id)
		}
	}
	u.tags = appendDistinct(u.tags, a.Tags...)
	u.channels = appendDistinct(u.channels, a.AcquisitionSources...)
	u.sources = appendDistinct(u.sources, m.native.ExplicitSources...)
	u.characters = appendDistinct(u.characters, a.CharacterIDs...)
	tax := m.native.Taxonomy
	if tax.Derived != 0 || len(tax.Candidates) != 0 {
		return errors.New("unsupported derived or candidate taxonomy")
	}
	u.characterText = appendDistinct(u.characterText, tax.CharacterText...)
	for _, field := range []struct {
		name   string
		target *string
		value  string
	}{
		{"free-text source", &u.sourceText, tax.SourceText},
		{"note", &u.note, a.Note},
		{"COS work", &u.cosWork, a.CosWorkID},
		{"COS work name", &u.cosName, a.CosWorkName},
		{"COS directory author", &u.cosAuthor, a.COSDirectoryAuthorID},
	} {
		if err := takeValue(field.name, field.target, field.value); err != nil {
			return err
		}
	}
	return nil
}

func (p projection) catalog(members []originalMember) error {
	u := catalogUnion{}
	for _, member := range members {
		if err := u.add(member); err != nil {
			return err
		}
	}
	if len(u.authors) == 0 {
		u.authors = []string{"unknown"}
	}
	if len(u.authors) > 20 || len(u.sources) > 20 || len(u.characters) > 50 || len(u.characterText) > 50 || len(u.tags) > 100 || len(u.channels) > 100 {
		return errors.New("combined catalog fields exceed native editing limits")
	}
	if (u.sourceText != "" && (len(u.sources) != 0 || len(u.characters) != 0)) || (len(u.characterText) != 0 && len(u.characters) != 0) {
		return errors.New("unbound taxonomy text cannot be combined with confirmed relationships")
	}
	rep := members[0].asset
	newAuthors := !slices.Equal(rep.AuthorIDs, u.authors)
	if newAuthors {
		rep.AuthorIDs, rep.AuthorID = u.authors, u.authors[0]
		rep.AuthorsRevision++
		if rep.AuthorID != "unknown" {
			rep.AuthorStatus = "confirmed"
		}
	}
	rep.Revision++
	rep.UpdatedAt = p.at
	rep.Note = u.note
	rep.AcquisitionSources = append([]string{}, u.channels...)
	rep.SourceIDs, rep.CharacterIDs = u.sources, u.characters
	rep.Source, rep.Characters = u.sourceText, u.characterText
	if err := store.SaveAssetEdits(p.ctx, p.tx, rep, newAuthors, store.CompleteTaxonomy(rep)); err != nil {
		return err
	}
	if !slices.Equal(rep.Tags, u.tags) {
		// This is an explicit union across previously distinct physical IDs.
		// Each original whole-set decision and its timestamp stay in the receipt.
		rep.Tags, rep.TagSetSource, rep.TagSetUpdatedAt = u.tags, "selected_merge", &p.at
		rep.TagsRevision++
		if err := store.SaveAssetTags(p.ctx, p.tx, rep); err != nil {
			return err
		}
	}
	if rep.CosWorkID != u.cosWork || rep.CosWorkName != u.cosName || rep.COSDirectoryAuthorID != u.cosAuthor {
		if err := p.exec(`UPDATE asset_records SET cos_work_id=?1,data=json_patch(data,json_object(
			'cosWorkId',nullif(?1,''),'cosWorkName',nullif(?2,''),'cosDirectoryAuthorId',nullif(?3,''))) WHERE id=?4`,
			u.cosWork, u.cosName, u.cosAuthor, rep.ID); err != nil {
			return err
		}
	}
	return p.authorDecisions(rep)
}

func (p projection) authorDecisions(rep model.Asset) error {
	var conflict bool
	if err := p.tx.QueryRowContext(p.ctx, `SELECT EXISTS(
		SELECT 1 FROM asset_author_overrides o JOIN asset_authors x ON x.author_id=o.author_id
		WHERE o.removed=1 AND o.asset_id IN `+groupIDs+` AND x.asset_id IN `+groupIDs+`
		UNION ALL SELECT 1 FROM asset_author_overrides WHERE asset_id IN `+groupIDs+`
		GROUP BY author_id HAVING count(DISTINCT nullif(list_id,''))>1)`, rep.ID).Scan(&conflict); err != nil {
		return err
	}
	if conflict {
		return errors.New("conflicting author exclusion or protected list ownership")
	}
	// Stable matches bind an existing TXT declaration to the asset even if
	// its reviewed filename changes. Transfer them without creating new rules.
	for _, query := range []string{
		`INSERT INTO author_list_matches(list_id,asset_id,author_id,work)
			SELECT DISTINCT list_id,?1,author_id,work FROM author_list_matches WHERE asset_id IN ` + groupIDs + ` ON CONFLICT DO NOTHING`,
		`INSERT INTO asset_author_overrides(asset_id,author_id,removed,list_id)
			SELECT ?1,author_id,min(removed),max(list_id) FROM asset_author_overrides WHERE asset_id IN ` + groupIDs + `
			GROUP BY author_id ON CONFLICT(asset_id,author_id) DO UPDATE SET removed=excluded.removed,list_id=excluded.list_id`,
		"DELETE FROM author_list_matches WHERE asset_id IN " + groupIDs + " AND asset_id!=?1",
		"DELETE FROM asset_author_overrides WHERE asset_id IN " + groupIDs + " AND asset_id!=?1",
		"INSERT INTO asset_human_fields(asset_id,field) SELECT DISTINCT ?1,field FROM asset_human_fields WHERE asset_id IN " + groupIDs + " ON CONFLICT DO NOTHING",
		"INSERT INTO asset_human_fields(asset_id,field) VALUES(?1,'rename') ON CONFLICT DO NOTHING",
	} {
		if err := p.exec(query, rep.ID); err != nil {
			return err
		}
	}
	return nil
}
