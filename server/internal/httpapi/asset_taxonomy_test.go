package httpapi_test

import (
	"context"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/service"
	"qimeng.local/next/server/internal/store"
)

func TestTaxonomySelectionOrderAndManualClearSurviveFileWrites(t *testing.T) {
	f := setup(t)
	sources := make([]service.Source, 2)
	roles := make([]service.Character, 2)
	for i, name := range []string{"Alpha", "Zulu"} {
		decode(t, f.json("POST", "/sources", "taxonomy-source-"+name, map[string]any{"name": name, "aliases": []string{}}, 201), &sources[i])
		decode(t, f.json("POST", "/characters", "taxonomy-role-"+name, map[string]any{"name": name + " role", "aliases": []string{}, "sourceId": sources[i].ID}, 201), &roles[i])
	}
	if sources[0].ID < sources[1].ID {
		sources[0], sources[1] = sources[1], sources[0]
	}
	if roles[0].ID < roles[1].ID {
		roles[0], roles[1] = roles[1], roles[0]
	}
	sourcesOrdered := []string{sources[0].ID, sources[1].ID}
	rolesOrdered := []string{roles[0].ID, roles[1].ID}
	u, _ := f.upload("taxonomy-order.png", 109, map[string]any{"sourceIds": sourcesOrdered, "characterIds": rolesOrdered, "acquisitionSources": []string{"archive channel"}})
	a := *f.complete(u).Asset
	base := "/assets/" + a.ID
	check := func(asset model.Asset, sourceIDs, characterIDs []string) {
		t.Helper()
		if !slices.Equal(asset.SourceIDs, sourceIDs) || !slices.Equal(asset.CharacterIDs, characterIDs) || len(asset.WorkKeys) != len(sourceIDs) ||
			len(asset.WorkFacets) != len(sourceIDs) || len(asset.RoleKeys) != len(characterIDs) || len(asset.RoleFacets) != len(characterIDs) {
			t.Fatal("asset and facet memberships disagree", asset)
		}
		names := []string{}
		for i, id := range sourceIDs {
			if asset.WorkKeys[i] != "source:"+id || asset.WorkFacets[i].ID != id {
				t.Fatal("work facet order", asset.WorkFacets)
			}
			names = append(names, asset.WorkFacets[i].Name)
		}
		if asset.Source != strings.Join(names, "+") {
			t.Fatal("canonical source labels", asset.Source, names)
		}
		for i, id := range characterIDs {
			if asset.RoleKeys[i] != "character:"+id || asset.RoleFacets[i].ID != id || asset.Characters[i] != asset.RoleFacets[i].Name {
				t.Fatal("role facet order", asset.RoleFacets)
			}
		}
	}
	check(a, sourcesOrdered, rolesOrdered)
	var page model.Page[model.Asset]
	decode(t, f.json("GET", "/assets?sourceIds="+url.QueryEscape(sources[0].ID), "", nil, 200), &page)
	if len(page.Items) != 1 {
		t.Fatal("asset missing from source selection", page)
	}
	check(page.Items[0], sourcesOrdered, rolesOrdered)
	var conflict struct{ Code string }
	decode(t, f.json("PATCH", base, "taxonomy-text-conflict", map[string]any{"expectedRevision": a.Revision, "source": "", "note": "must roll back"}, 409), &conflict)
	if conflict.Code != "taxonomy_ids_required" {
		t.Fatal(conflict.Code)
	}
	decode(t, f.json("GET", base, "", nil, 200), &a)
	if a.Note != "" {
		t.Fatal("conflicting request partially changed the asset")
	}
	slices.Reverse(sourcesOrdered)
	slices.Reverse(rolesOrdered)
	decode(t, f.json("PATCH", base, "taxonomy-reorder", map[string]any{"expectedRevision": a.Revision, "sourceIds": sourcesOrdered, "characterIds": rolesOrdered}, 200), &a)
	check(a, sourcesOrdered, rolesOrdered)
	stale := a
	clear := map[string]any{"expectedRevision": a.Revision, "sourceIds": []string{}, "characterIds": []string{}}
	a = model.Asset{}
	decode(t, f.json("PATCH", base, "taxonomy-clear-all", clear, 200), &a)
	f.json("PATCH", base, "taxonomy-clear-all", clear, 200)
	if len(a.SourceIDs) != 0 || a.Source != "" || len(a.Characters) != 0 || len(a.RoleKeys) != 0 || !slices.Equal(a.WorkKeys, []string{"source:__other__"}) {
		t.Fatal("clear receipt retained a membership", a)
	}
	// A file snapshot taken before the edit has the old IDs, names and protection metadata.
	// Keep its unrelated metadata at the current revision so this probes only taxonomy ownership.
	stale.Revision, stale.UpdatedAt, stale.MetadataSource = a.Revision, a.UpdatedAt, a.MetadataSource
	if err := store.SaveAsset(context.Background(), f.s.DB.DB, stale); err != nil {
		t.Fatal(err)
	}
	decode(t, f.json("POST", base+"/move", "taxonomy-move-cleared", map[string]any{"expectedRevision": a.Revision, "relativePath": "taxonomy-renamed.png"}, 200), &a)
	decode(t, f.json("DELETE", base+"?expectedRevision="+strconv.FormatInt(a.Revision, 10), "taxonomy-trash-cleared", nil, 200), &a)
	decode(t, f.json("POST", "/trash/"+a.ID+"/restore", "taxonomy-restore-cleared", map[string]any{"expectedRevision": a.Revision}, 200), &a)
	if job := f.scan("taxonomy-scan-cleared-file"); job.State != "completed" {
		t.Fatal("scan did not verify the restored file", job)
	}
	decode(t, f.json("GET", base, "", nil, 200), &a)
	if len(a.SourceIDs) != 0 || len(a.CharacterIDs) != 0 || a.Source != "" || len(a.Characters) != 0 || !slices.Equal(a.AcquisitionSources, []string{"archive channel"}) {
		t.Fatal("unrelated catalog writes restored obsolete taxonomy", a)
	}
	var locked bool
	if err := f.s.DB.DB.QueryRow("SELECT EXISTS(SELECT 1 FROM asset_human_fields WHERE asset_id=? AND field='taxonomy')", a.ID).Scan(&locked); err != nil || !locked {
		t.Fatal("published clear lost its human protection", locked, err)
	}
}

func TestUnboundTextAndCharacterParentKeepSeparateOrigins(t *testing.T) {
	f := setup(t)
	var work, next service.Source
	var role service.Character
	decode(t, f.json("POST", "/sources", "taxonomy-implicit-work", map[string]any{"name": "First work", "aliases": []string{}}, 201), &work)
	decode(t, f.json("POST", "/sources", "taxonomy-next-work", map[string]any{"name": "Next work", "aliases": []string{}}, 201), &next)
	decode(t, f.json("POST", "/characters", "taxonomy-parent-role", map[string]any{"name": "Hero", "sourceId": work.ID, "aliases": []string{}}, 201), &role)
	u, _ := f.upload("implicit-parent.png", 113, map[string]any{"characterIds": []string{role.ID}})
	a := *f.complete(u).Asset
	base := "/assets/" + a.ID
	if !slices.Equal(a.SourceIDs, []string{work.ID}) {
		t.Fatal("character did not supply its current parent", a)
	}
	decode(t, f.json("PUT", "/characters/"+role.ID, "taxonomy-reparent", map[string]any{"name": "Renamed hero", "aliases": []string{}, "sourceId": next.ID, "expectedRevision": role.Revision}, 200), &role)
	decode(t, f.json("GET", base, "", nil, 200), &a)
	if !slices.Equal(a.SourceIDs, []string{next.ID}) || a.Source != next.Name || !slices.Equal(a.Characters, []string{role.Name}) {
		t.Fatal("an implied parent became a permanent explicit source", a)
	}
	var page model.Page[model.Asset]
	decode(t, f.json("GET", "/assets?workKeys="+url.QueryEscape("source:"+work.ID), "", nil, 200), &page)
	if len(page.Items) != 0 {
		t.Fatal("old parent still matches a filter")
	}
	decode(t, f.json("PATCH", base, "taxonomy-remove-last-role", map[string]any{"expectedRevision": a.Revision, "characterIds": []string{}}, 200), &a)
	if len(a.SourceIDs) != 0 || len(a.CharacterIDs) != 0 {
		t.Fatal("unbinding the only role left its implied source", a)
	}
	decode(t, f.json("PATCH", base, "taxonomy-name-clue", map[string]any{"expectedRevision": a.Revision, "source": work.Name, "characters": []string{"unverified name"}}, 200), &a)
	decode(t, f.json("GET", base, "", nil, 200), &a)
	if a.Source != work.Name || !slices.Equal(a.Characters, []string{"unverified name"}) || len(a.SourceIDs)+len(a.CharacterIDs) != 0 {
		t.Fatal("text was lost or silently matched to an identity", a)
	}
	decode(t, f.json("PATCH", base, "taxonomy-edit-only-sources", map[string]any{"expectedRevision": a.Revision, "sourceIds": []string{work.ID}}, 200), &a)
	if !slices.Equal(a.Characters, []string{"unverified name"}) || len(a.CharacterIDs) != 0 {
		t.Fatal("other dimension's text clue changed", a)
	}
}

func TestIntelligentPlanFreezesTheSelectedRolesWithoutPinningTheirParents(t *testing.T) {
	f := setup(t)
	var source service.Source
	var role service.Character
	decode(t, f.json("POST", "/sources", "plan-taxonomy-source", map[string]any{"name": "Plan work", "aliases": []string{}}, 201), &source)
	decode(t, f.json("POST", "/characters", "plan-taxonomy-character", map[string]any{"name": "Plan role", "sourceId": source.ID, "aliases": []string{}}, 201), &role)
	var b model.Batch
	decode(t, f.json("POST", "/batches", "plan-taxonomy-batch", map[string]any{"libraryId": f.library, "mode": "intelligent", "label": "Plan taxonomy", "maxResearchCalls": 0}, 201), &b)
	inputs := f.declareInputs(b.ID, "plan-taxonomy.png")
	u, _ := f.upload("plan-taxonomy.png", 115, map[string]any{"mode": "intelligent", "batchId": b.ID, "inputId": inputs["plan-taxonomy.png"]})
	f.complete(u)
	var detail model.BatchDetail
	decode(t, f.json("GET", "/batches/"+b.ID, "", nil, 200), &detail)
	request := model.PlanCreate{ExpectedRevision: detail.Batch.Revision, Items: []model.PlanInput{{UploadID: u.ID, Family: "Plan",
		CharacterIDs: []string{role.ID}}}}
	var plan model.Plan
	decode(t, f.json("POST", "/batches/"+b.ID+"/plans", "plan-taxonomy-prepare", request, 201), &plan)
	if len(plan.Items) != 1 || !slices.Equal(plan.Items[0].SourceIDs, []string{source.ID}) ||
		!strings.Contains(plan.Items[0].FileName, source.Name) || !strings.Contains(plan.Items[0].FileName, role.Name) {
		t.Fatal("plan names and membership disagree", plan.Items)
	}
	f.json("POST", "/batches/"+b.ID+"/plans/"+plan.ID+"/commit", "plan-taxonomy-commit", map[string]string{"fingerprint": plan.Fingerprint}, 200)
	decode(t, f.json("GET", "/uploads/"+u.ID, "", nil, 200), &u)
	var a model.Asset
	base := "/assets/" + u.AssetID
	decode(t, f.json("GET", base, "", nil, 200), &a)
	if a.FileName != plan.Items[0].FileName || !slices.Equal(a.SourceIDs, []string{source.ID}) || !slices.Equal(a.CharacterIDs, []string{role.ID}) {
		t.Fatal("published data diverged from the frozen plan", a)
	}
	decode(t, f.json("PATCH", base, "plan-taxonomy-detach-role", map[string]any{"expectedRevision": a.Revision, "characterIds": []string{}}, 200), &a)
	if len(a.SourceIDs)+len(a.CharacterIDs) != 0 {
		t.Fatal("plan froze the implied source as a manual choice", a)
	}
}
