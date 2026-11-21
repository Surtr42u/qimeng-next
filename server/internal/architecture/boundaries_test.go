package architecture_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"
	"testing"
)

const internalPrefix = "qimeng.local/next/server/internal/"

// Only production imports are constrained. Integration tests may compose real
// modules, and generated transport files have their own OpenAPI source of truth.
var dependencies = map[string][]string{
	"analytics":        {"model"},
	"assetmerge":       {"backup", "domain", "model", "service", "storage", "store"},
	"authorlist":       {"model"},
	"backup":           {},
	"domain":           {"model"},
	"engagement":       {"model", "store"},
	"events":           {},
	"mediafile":        {},
	"httpapi":          {"events", "service", "model", "migration", "transport/generated"},
	"legacycold":       {"analytics", "authorlist", "backup", "domain", "model", "service", "sourcerules", "storage", "store"},
	"migration":        {"model"},
	"model":            {},
	"researchprovider": {},
	"service":          {"analytics", "authorlist", "backup", "domain", "engagement", "events", "mediafile", "migration", "model", "researchprovider", "sourcerules", "storage", "store", "sysmon"},
	"sourcerules":      {"model"},
	"storage":          {},
	"store":            {"domain", "model"},
	"sysmon":           {},
}

var rulesAndRecords = []string{"analytics", "authorlist", "domain", "migration", "model", "sourcerules"}

func importProblem(owner, imported string) string {
	allowed, known := dependencies[owner]
	if !known {
		return "assign this module a responsibility and an import boundary"
	}
	if local, ok := strings.CutPrefix(imported, internalPrefix); ok {
		for _, candidate := range append(slices.Clone(allowed), owner) {
			if local == candidate || strings.HasPrefix(local, candidate+"/") {
				return ""
			}
		}
		return "dependency points outside the module's responsibility"
	}
	if owner == "httpapi" || slices.Contains(rulesAndRecords, owner) {
		for _, root := range []string{"database/sql", "os", "syscall", "io/ioutil", "golang.org/x/sys", "modernc.org/sqlite", "github.com/mattn/go-sqlite3"} {
			if imported == root || strings.HasPrefix(imported, root+"/") {
				return "filesystem, database and process access belongs in an adapter"
			}
		}
	}
	if slices.Contains(rulesAndRecords, owner) && (imported == "net" || strings.HasPrefix(imported, "net/http") || imported == "crypto/rand") {
		return "external inputs must be supplied to the rule by its caller"
	}
	return ""
}

func TestProductionImportBoundaries(t *testing.T) {
	manifest, err := os.ReadFile("../../go.mod")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(strings.Split(string(manifest), "\n"), "module "+strings.TrimSuffix(internalPrefix, "/internal/")) {
		t.Fatal("module identity changed; update the internal import boundary")
	}
	source := os.DirFS("..")
	files := token.NewFileSet()
	err = fs.WalkDir(source, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if name == "transport/generated" && entry.IsDir() {
			return fs.SkipDir
		}
		if entry.IsDir() || path.Ext(name) != ".go" || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		owner, _, _ := strings.Cut(name, "/")
		if _, known := dependencies[owner]; !known {
			t.Errorf("internal/%s: unclassified production module %q", name, owner)
			return nil
		}
		body, err := fs.ReadFile(source, name)
		if err != nil {
			return err
		}
		file, err := parser.ParseFile(files, name, body, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, ref := range file.Imports {
			imported, err := strconv.Unquote(ref.Path.Value)
			if err != nil {
				return err
			}
			if problem := importProblem(owner, imported); problem != "" {
				t.Errorf("internal/%s:%d imports %q: %s", name, files.Position(ref.Pos()).Line, imported, problem)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestBoundaryExamples(t *testing.T) {
	for _, example := range []struct {
		owner   string
		imports string
		allowed bool
	}{
		{"httpapi", internalPrefix + "service", true},
		{"httpapi", internalPrefix + "transport/generated", true},
		{"httpapi", internalPrefix + "store", false},
		{"httpapi", internalPrefix + "engagement", false},
		{"engagement", internalPrefix + "service", false},
		{"engagement", internalPrefix + "store", true},
		{"service", internalPrefix + "mediafile", true},
		{"mediafile", internalPrefix + "service", false},
		{"store", internalPrefix + "service", false},
		{"service", internalPrefix + "httpapi", false},
		{"legacycold", internalPrefix + "store", true},
		{"assetmerge", internalPrefix + "backup", true},
		{"assetmerge", internalPrefix + "store", true},
		{"service", internalPrefix + "assetmerge", false},
		{"httpapi", internalPrefix + "assetmerge", false},
		{"service", internalPrefix + "legacycold", false},
		{"httpapi", internalPrefix + "legacycold", false},
		{"domain", "os/exec", false},
		{"sourcerules", "net/http", false},
		{"model", "net/url", true},
		{"migration", "database/sql", false},
		{"researchprovider", "net/http", true},
	} {
		if got := importProblem(example.owner, example.imports) == ""; got != example.allowed {
			t.Errorf("%s -> %s allowed = %t, want %t", example.owner, example.imports, got, example.allowed)
		}
	}
}
