package httpapi_test

import (
	"context"
	"encoding/json"
	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/service"
	"qimeng.local/next/server/internal/sourcerules"
	"slices"
	"testing"
	"time"
)

func waitSourceRule(t *testing.T, f *fixture, revision int64) service.SourceRuleSettings {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		state, err := f.s.SourceRules(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if state.Revision == revision && state.RebuildState == "completed" {
			return state
		}
		if state.RebuildState == "failed" {
			t.Fatal(state.ErrorCode)
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("source rules did not finish")
	return service.SourceRuleSettings{}
}
func TestSourceRulesApplyAtIngestAndReconcileOnlyEligibleAssets(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	oldInput, _ := f.upload("合成词组 未知甲 11.png", 51, nil)
	old := *f.complete(oldInput).Asset
	keptInput, _ := f.upload("合成词组 明确空 12.png", 52, map[string]any{"sourceIds": []string{}})
	kept := *f.complete(keptInput).Asset
	editedInput, _ := f.upload("合成词组 人工空 13.png", 53, nil)
	edited := *f.complete(editedInput).Asset
	f.json("PATCH", "/assets/"+edited.ID, "explicit-clear-before-rules", map[string]any{"expectedRevision": edited.Revision, "characters": []string{}}, 200)
	layer := sourcerules.Layer{Groups: []sourcerules.Group{{Canonical: "合成词组", Variants: []string{"FixtureScope"}, Characters: []sourcerules.Character{}}}, StopWords: []string{"忽略"}}
	input := service.SourceRuleUpdate{Layer: layer, ExpectedRevision: 1}
	response := f.json("PUT", "/sources/custom-groups", "source-rule-configure", input, 200)
	var saved service.SourceRuleSettings
	decode(t, response, &saved)
	if saved.Revision != 2 {
		t.Fatal(saved)
	}
	duplicate := f.json("PUT", "/sources/custom-groups", "source-rule-configure", input, 200)
	if duplicate.Body.String() != response.Body.String() {
		t.Fatal("retry created another settings revision")
	}
	f.json("PUT", "/sources/custom-groups", "source-rule-stale", input, 409)
	waitSourceRule(t, f, 2)
	get := func(id string) model.Asset {
		a, err := f.s.Asset(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	old = get(old.ID)
	if !old.TaxonomyDerived || old.Source != "合成词组" || !slices.Equal(old.Characters, []string{"未知甲"}) {
		t.Fatalf("existing unmodified file did not reconcile: %+v", old)
	}
	for _, a := range []model.Asset{get(edited.ID), get(kept.ID)} {
		if a.TaxonomyDerived || a.Source != "" || len(a.CharacterIDs) > 0 {
			t.Fatal("a deliberate empty selection was overwritten")
		}
	}
	newInput, _ := f.upload("FixtureScope 忽略 新甲 2.png", 54, nil)
	fresh := *f.complete(newInput).Asset
	if fresh.Source != "合成词组" || !slices.Equal(fresh.Characters, []string{"新甲"}) || !slices.Equal(old.SourceIDs, fresh.SourceIDs) {
		t.Fatalf("ingest receipt did not use the active vocabulary: %+v", fresh)
	}
	f.json("PATCH", "/assets/"+fresh.ID, "source-rule-clear-result", map[string]any{"expectedRevision": fresh.Revision, "sourceIds": []string{}, "characterIds": []string{}}, 200)
	layer.Groups = append(layer.Groups, sourcerules.Group{Canonical: "合成同名组", Variants: []string{"合成词组"}, Characters: []sourcerules.Character{}})
	f.json("PUT", "/sources/custom-groups", "source-rule-conflicting-spelling", service.SourceRuleUpdate{Layer: layer, ExpectedRevision: 2}, 200)
	waitSourceRule(t, f, 3)
	old = get(old.ID)
	if old.Source != "" || len(old.TaxonomyCandidates) != 2 || !old.TaxonomyDerived {
		t.Fatal("ambiguous filename acquired an unconfirmed source")
	}
	fresh = get(fresh.ID)
	if fresh.Source != "" || fresh.TaxonomyDerived || len(fresh.TaxonomyCandidates) > 0 {
		t.Fatal("rebuild replaced an owner's explicit clear")
	}
	clear := service.SourceRuleUpdate{ExpectedRevision: 3, Layer: sourcerules.Layer{Groups: []sourcerules.Group{}, StopWords: []string{}}}
	f.json("PUT", "/sources/custom-groups", "source-rule-reset", clear, 200)
	waitSourceRule(t, f, 4)
	old = get(old.ID)
	if old.Source != "" || old.TaxonomyDerived || len(old.TaxonomyCandidates) > 0 {
		t.Fatal("removed custom matches remained active")
	}
	raw, _ := json.Marshal(clear)
	if result := f.request("PUT", "/sources/custom-groups", "untrusted-source-change", "", raw, nil); result.Code != 401 {
		t.Fatal("unauthenticated settings write succeeded")
	}
}
