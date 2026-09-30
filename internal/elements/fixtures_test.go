package elements

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The grammar 1.2 fixtures (task RD4-6) are shared with rearm-saas, which keeps the same files under
// backend/src/test/resources/elements/grammar-1.2: each fixture is a markdown file, the type it is
// published as, an optional board override of elementFamilies, and the index this parser must emit,
// byte for byte. The server's test reads the same index and checks that its validator sorts the ids
// the same way and that the named checks come out as the manifest says. Set UPDATE_FIXTURES=1 to
// rewrite the index files after a deliberate grammar change, then copy the directory to rearm-saas.

const fixtureDir = "testdata/grammar-1.2"

type fixture struct {
	Name            string                 `json:"name"`
	File            string                 `json:"file"`
	Specification   string                 `json:"specification"`
	Index           string                 `json:"index"`
	ElementFamilies map[string]interface{} `json:"elementFamilies"`
}

func fixtures(t *testing.T) []fixture {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(fixtureDir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var out []fixture
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// declaredFamilies reads a board's declared elementFamilies the way the server does: an entry is a
// family name, or {family, definedIn}; a default prefix may leave the family out.
func declaredFamilies(declared map[string]interface{}) Families {
	names := map[string]string{}
	for p, n := range DefaultFamilies {
		names[p] = n
	}
	lists := map[string][]string{}
	for prefix, v := range declared {
		switch e := v.(type) {
		case string:
			names[prefix] = e
		case map[string]interface{}:
			if f, ok := e["family"].(string); ok && f != "" {
				names[prefix] = f
			}
			if raw, ok := e["definedIn"].([]interface{}); ok {
				list := []string{}
				for _, t := range raw {
					list = append(list, t.(string))
				}
				lists[prefix] = list
			}
		}
	}
	return FamiliesFrom(names, lists)
}

func TestEveryGrammarFixtureParsesToItsIndex(t *testing.T) {
	update := os.Getenv("UPDATE_FIXTURES") == "1"
	for _, f := range fixtures(t) {
		src, err := os.ReadFile(filepath.Join(fixtureDir, f.File))
		if err != nil {
			t.Fatal(err)
		}
		got, _, err := Canonical(Parse(src, f.Specification, declaredFamilies(f.ElementFamilies), nil))
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(fixtureDir, f.Index)
		if update {
			if err := os.WriteFile(path, append(got, '\n'), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: %v", f.Name, err)
		}
		if !bytes.Equal(got, bytes.TrimRight(want, "\n")) {
			t.Errorf("%s: %s as %s\n got: %s\nwant: %s", f.Name, f.File, f.Specification, got, want)
		}
	}
}
