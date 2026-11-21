package domain

import "testing"

func TestSearchKeepsWorkNumbersSeparateFromPages(t *testing.T) {
	cases := []struct {
		text, token string
		want        bool
	}{
		{"朱诺 11 (2).png", "11", true}, {"朱诺 110.png", "11", false}, {"朱诺 2011.png", "11", false}, {"11 (2).png", "2", false}, {"11 （2）.png", "(2)", true}, {"11 [2].png", "【2】", true}, {"11 【20】.png", "(2)", false},
		{"DVA Sunset", "dva", true}, {"50%_\\real", "%_\\", true}, {"汉字角色", "字", true}, {"1080p.mp4", "1080", true}, {"1080p.mp4", "4", true}, {"Family 0011.png", "11", false}, {"Family 0011.png", "0011", true},
		// Literal metacharacters are never treated as pattern syntax.
		{"a*b-c:d(e)f", "*b-c", true}, {"a*b-c:d(e)f", "%", false}, {"a_b%c\\d", "b%c\\", true}, {`quote "x"`, `"x"`, true}, {"a'b", "'", true}, {"a[b]c", "[b]", true}, {"a?b", "?", true}, {"a*b", "*", true},
		// Bracketed page variants and CJK without separators.
		{"（2）[3]【4】", "（3）", true}, {"（2）[3]【4】", "【4】", true}, {"(2) [3] 【4】", "(4)", true}, {"(2) [3] 【4】", "(3)", true}, {"汉字角色", "字角", true}, {"作品110", "作品1", true},
		// Standalone digit runs: start, middle, end, and both rejected sides.
		{"11.png", "11", true}, {"x/11/y", "11", true}, {"x/11", "11", true}, {"x11", "11", true}, {"中11文", "11", true},
		{"(11).png", "11", false}, {"（11）.png", "11", false}, {"[11].png", "11", false}, {"【11】.png", "11", false},
		{"111", "11", false}, {"119", "11", false}, {"011", "11", false},
	}
	for _, c := range cases {
		if got := SearchToken(c.text, c.token); got != c.want {
			t.Errorf("SearchToken(%q,%q)=%v", c.text, c.token, got)
		}
	}
}

func TestSearchTokenPatternClassifiesForSQL(t *testing.T) {
	cases := []struct {
		token        string
		kind         SearchTokenKind
		folded, page string
	}{
		{"DVA", SearchTokenContains, "dva", ""},
		{"50%_\\", SearchTokenContains, "50%_\\", ""},
		{"【2】", SearchTokenPage, "【2】", "(2)"},
		{"(2)", SearchTokenPage, "(2)", "(2)"},
		{"12", SearchTokenNumber, "12", ""},
	}
	for _, c := range cases {
		kind, folded, page := SearchTokenPattern(c.token)
		if kind != c.kind || folded != c.folded || page != c.page {
			t.Errorf("SearchTokenPattern(%q)=(%v,%q,%q), want (%v,%q,%q)",
				c.token, kind, folded, page, c.kind, c.folded, c.page)
		}
	}
}

func TestFilterPresetBoundariesBelongToGo(t *testing.T) {
	for _, name := range []string{"low", "mid"} {
		low, high, ok := CountInterval(name)
		if !ok || *low > 5 || *high < 5 {
			t.Fatal("visible inclusive count boundary changed")
		}
	}
	low, high, ok := SizeInterval("m1to10")
	if !ok || *low != 1<<20 || *high != 10<<20 {
		t.Fatal("file-size units or boundaries changed")
	}
	_, less, _ := SizeInterval("lt1m")
	greater, _, _ := SizeInterval("gt50m")
	if *less != (1<<20)-1 || *greater != (50<<20)+1 {
		t.Fatal("exclusive file-size bounds became inclusive")
	}
}
