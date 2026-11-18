package httpapi_test

import "testing"

func TestInputDirectoryRoutingUsesSharedLibraryIdentityRules(t *testing.T) {
	f := setup(t)
	f.json("POST", "/libraries", "another-synthetic-root", map[string]string{"name": "Synthetic", "kind": "regular"}, 201)
	f.json("POST", "/libraries", "sanitized-library-root", map[string]string{"name": "A:B", "kind": "regular"}, 201)
	f.json("POST", "/libraries", "unsupported-cos-root", map[string]string{"name": "Other", "kind": "cos"}, 201)
	var page struct {
		Items []struct{ State, LibraryID string }
	}
	decode(t, f.json("POST", "/libraries/resolve-input-directories", "", map[string]any{"names": []string{"Synthetic", "A_B", "missing", "Other"}}, 200), &page)
	want := []string{"ambiguous", "matched", "missing", "unsupported"}
	for i, item := range page.Items {
		if item.State != want[i] {
			t.Fatal("directory resolved inconsistently", item)
		}
	}
	if len(page.Items) != 4 || page.Items[1].LibraryID == "" {
		t.Fatal("matched identity absent")
	}
	f.json("POST", "/libraries/resolve-input-directories", "", map[string]any{"names": []string{"../arbitrary"}}, 400)
}
