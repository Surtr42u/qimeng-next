package sourcerules

import (
	"slices"
	"testing"
)

func TestReviewedVocabularyAndFallback(t *testing.T) {
	count := 0
	for _, g := range frozen {
		count += len(g.Characters)
	}
	if len(frozen) != 133 || count != 1394 {
		t.Fatalf("incomplete frozen vocabulary: %d/%d", len(frozen), count)
	}
	index := Build(Layer{Groups: []Group{{Canonical: "合成系列", Variants: []string{"Fixture Series"}, Characters: []Character{{Canonical: "标准角色", Aliases: []string{"Original Hero"}}}}}, StopWords: []string{"合成备注"}})
	for _, c := range []struct {
		name, source string
		roles        []string
	}{
		{"Fixture Series Original Hero & 合成路人 5.png", "合成系列", []string{"标准角色"}},
		{"Fixture Series 1 Original Hero 私人备注.png", "合成系列", nil},
		{"Fixture Series 未知甲 3 Original Hero.png", "合成系列", []string{"未知甲"}},
		{"合成系列 白丝 合成甲&合成乙 8K 合成备注 (2) 尾部.png", "合成系列", []string{"合成乙", "合成甲"}},
		{"守望先锋 Dmon 1.mp4", "守望先锋", []string{"D.Mon"}},
		{"NieR Automata 2B 1.png", "尼尔 机械纪元", []string{"2B"}},
		{"Monster Hunter Gemma 1.jpg", "怪物猎人", []string{"杰玛"}},
	} {
		got := index.Match(c.name)
		if len(got.Matches) != 1 || got.Matches[0].Source != c.source || !slices.Equal(got.Matches[0].Characters, c.roles) {
			t.Fatalf("%s: %+v", c.name, got)
		}
	}
	multi := index.Match("合成系列+怪物猎人 Gemma 1.png")
	if len(multi.Matches) != 2 {
		t.Fatal(multi)
	}
}
func TestAmbiguityCannotAcquireAnAutomaticOwner(t *testing.T) {
	base := Layer{Groups: []Group{{Canonical: "合成甲", Variants: []string{"Common"}, Characters: []Character{}}, {Canonical: "合成乙", Variants: []string{"common"}, Characters: []Character{}}, {Canonical: "合成乙分部", Variants: []string{"Common Extra"}, Characters: []Character{{Canonical: "合成人A", Aliases: []string{"both"}}, {Canonical: "合成人B", Aliases: []string{"Both"}}}}}, StopWords: []string{}}
	index := Build(base)
	for _, name := range []string{"Common thing.png", "Common Extra Both.png"} {
		got := index.Match(name)
		if len(got.Matches) > 0 || len(got.Candidates) != 2 {
			t.Fatalf("%s: %+v", name, got)
		}
	}
	exact := index.Match("Common Extra 独一 2.png")
	if len(exact.Matches) != 1 || exact.Matches[0].Source != "合成乙分部" {
		t.Fatal(exact)
	}
}
