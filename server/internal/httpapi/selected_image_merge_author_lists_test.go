package httpapi_test

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"testing"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/service"
	"qimeng.local/next/server/internal/store"
)

func TestSelectedImageMergeAuthorListRebuildKeepsOneBindingAndItsSource(t *testing.T) {
	f := setup(t)
	const name = "artists.txt"
	const originalText = "1  Original artist\n作品\nvariant.png\n"
	initial := importList(t, f, "seed-artists", name, originalText, 0)
	rep, donor, _ := selectFixtureImages(t, f)
	if initial.List == nil || donor.AuthorID == "unknown" || donor.FileName != "variant.png" {
		t.Fatal("ordinary source list did not bind the original member")
	}

	// Model the native projection consumed by a restored service: the logical
	// binding follows the representative, while its original spelling and
	// physical owner remain in the immutable member receipt.
	ctx := context.Background()
	db := f.s.DB.DB
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.Exec("UPDATE author_list_matches SET asset_id=? WHERE asset_id=? AND list_id=?", rep.ID, donor.ID, initial.List.ID); err != nil {
		t.Fatal(err)
	}
	rep.AuthorID, rep.AuthorIDs, rep.AuthorStatus = donor.AuthorID, donor.AuthorIDs, donor.AuthorStatus
	rep.AuthorsRevision++
	rep.Revision++
	if err = store.SaveAssetEdits(ctx, tx, rep, true, nil); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	checkBinding := func() {
		t.Helper()
		var canonical, originalSpelling, archived int
		err := db.QueryRow(`SELECT count(*) FILTER(WHERE asset_id=?1),count(*) FILTER(WHERE asset_id=?1 AND author_id=?2 AND work=?3),
			count(*) FILTER(WHERE asset_id=?4) FROM author_list_matches WHERE list_id=?5`,
			rep.ID, donor.AuthorID, donor.FileName, donor.ID, initial.List.ID).Scan(&canonical, &originalSpelling, &archived)
		if err != nil || canonical != 1 || originalSpelling != 1 || archived != 0 {
			t.Fatal("expected one source-bound relationship for the reviewed item", canonical, originalSpelling, archived, err)
		}
		current, err := store.Get[model.Asset](ctx, db, "SELECT data FROM assets WHERE id=?", donor.ID)
		if err != nil || current.AuthorsRevision != donor.AuthorsRevision || current.Revision != donor.Revision ||
			!slices.Equal(current.AuthorIDs, donor.AuthorIDs) {
			t.Fatal("author rebuild edited the archived member", err)
		}
		var source bool
		err = db.QueryRow(`SELECT EXISTS(SELECT 1 FROM selected_image_merge_members WHERE physical_asset_id=? AND representative_id=?
			AND json_extract(original_asset,'$.authorId')=? AND json_extract(original_relations,'$.listMatches[0].listId')=?
			AND json_extract(original_relations,'$.listMatches[0].authorId')=? AND json_extract(original_relations,'$.listMatches[0].work')=?)`,
			donor.ID, rep.ID, donor.AuthorID, initial.List.ID, donor.AuthorID, donor.FileName).Scan(&source)
		if err != nil || !source {
			t.Fatal("the archived source declaration no longer identifies its original member", err)
		}
	}
	checkBinding()

	u, _ := f.upload("variant.png", 72, map[string]any{"relativeDirectory": "independent"})
	live := *f.complete(u).Asset
	u, _ = f.upload("ordinary-trash.png", 73, nil)
	trashed := *f.complete(u).Asset
	f.json("DELETE", fmt.Sprintf("/assets/%s?expectedRevision=%d", trashed.ID, trashed.Revision), "trash-list-control", struct{}{}, 200)
	var rebuilt service.AuthorListResult
	decode(t, f.json("POST", "/author-lists/rebuild", "rebuild-reviewed-list", struct{}{}, 200), &rebuilt)
	if rebuilt.MatchedAssets != 2 || rebuilt.ChangedAssets != 0 {
		t.Fatal("rebuild must count the representative and the independent equal name", rebuilt)
	}
	checkBinding()

	const editedText = originalText + "2  Additional artist\n作品\nvariant.png\nordinary-trash.png\n"
	in := map[string]any{"name": name, "content": editedText, "expectedRevision": initial.List.Revision}
	first := f.json("POST", "/author-lists", "reimport-reviewed-list", in, 200)
	var updated service.AuthorListResult
	decode(t, first, &updated)
	if updated.List == nil || updated.MatchedAssets != 3 || updated.ChangedAssets != 2 || updated.AuthorsCreated != 1 {
		t.Fatal("reimport did not preserve the independent and ordinary trashed matches", updated)
	}
	replay := f.json("POST", "/author-lists", "reimport-reviewed-list", in, 200)
	if !bytes.Equal(first.Body.Bytes(), replay.Body.Bytes()) {
		t.Fatal("replaying the reimport changed its receipt")
	}
	checkBinding()
	repeated := importList(t, f, "repeat-reviewed-list", name, editedText, updated.List.Revision)
	if repeated.MatchedAssets != 3 || repeated.ChangedAssets != 0 || repeated.AuthorsCreated != 0 {
		t.Fatal("unchanged declarations rebuilt a second relationship", repeated)
	}
	checkBinding()

	var reviewed, independent, ordinaryTrash model.Asset
	decode(t, f.json("GET", "/assets/"+donor.ID, "", nil, 200), &reviewed)
	decode(t, f.json("GET", "/assets/"+live.ID, "", nil, 200), &independent)
	decode(t, f.json("GET", "/assets/"+trashed.ID, "", nil, 200), &ordinaryTrash)
	if reviewed.ID != rep.ID || !slices.Equal(reviewed.AuthorIDs, rep.AuthorIDs) || reviewed.AuthorsRevision != rep.AuthorsRevision ||
		len(independent.AuthorIDs) != 2 || !slices.Contains(independent.AuthorIDs, ordinaryTrash.AuthorID) ||
		!ordinaryTrash.Trashed || ordinaryTrash.AuthorName != "Additional artist" {
		t.Fatal("logical author assignments changed an unrelated item or lost a normal trash match")
	}
}
