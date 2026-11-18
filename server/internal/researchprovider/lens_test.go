package researchprovider

import (
	"fmt"
	"strings"
	"testing"
)

func TestLensSeparatesUnlinkedHintsAndOperationalFailures(t *testing.T) {
	sha := strings.Repeat("4", 64)
	raw := fmt.Sprintf(`{"ok":true,"status":"ok","frame_sha256":%q,"topDomains":["example.org","127.0.0.1","path.local","example.org"]}`, sha)
	state, items, hints := ParseLens(200, []byte(raw), sha)
	if state != "ok" || len(items) != 0 || len(hints) != 1 || hints[0] != (ImageHint{Domain: "example.org"}) {
		t.Fatal("unlinked or private domain became a candidate URL")
	}
	for _, c := range []struct {
		status     int
		code, want string
	}{{503, "BLOCKED_BOT", "blocked"}, {503, "UNAVAILABLE", "unavailable"}, {502, "NO_RESULT", "unavailable"}, {429, "BUSY", "rate_limited"}, {504, "TIMEOUT", "unavailable"}, {404, "NOT_FOUND", "unavailable"}, {401, "UNAUTHORIZED", "unavailable"}} {
		raw := fmt.Sprintf(`{"ok":false,"error":{"code":%q}}`, c.code)
		state, items, hints := ParseLens(c.status, []byte(raw), sha)
		if state != c.want || len(items) != 0 || len(hints) != 0 {
			t.Errorf("status %d/%s returned %s", c.status, c.code, state)
		}
	}
}
