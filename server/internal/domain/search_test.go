package domain

import "testing"

func TestSearchKeepsWorkNumbersSeparateFromPages(t *testing.T) {
	cases := []struct {
		text, token string
		want        bool
	}{
		{"朱诺 11 (2).png", "11", true}, {"朱诺 110.png", "11", false}, {"朱诺 2011.png", "11", false}, {"11 (2).png", "2", false}, {"11 （2）.png", "(2)", true}, {"11 [2].png", "【2】", true}, {"11 【20】.png", "(2)", false},
		{"DVA Sunset", "dva", true}, {"50%_\\real", "%_\\", true}, {"汉字角色", "字", true}, {"1080p.mp4", "1080", true}, {"1080p.mp4", "4", true}, {"Family 0011.png", "11", false}, {"Family 0011.png", "0011", true},
	}
	for _, c := range cases {
		if got := SearchToken(c.text, c.token); got != c.want {
			t.Errorf("SearchToken(%q,%q)=%v", c.text, c.token, got)
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
