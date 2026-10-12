package bomscore

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Shared helpers of the package's tests. Every red case is a mutation of one of the hand-written
// fixtures in testdata/, applied in the test, so it cannot drift from the green one.

const testEngineVersion = "test"

var allProfiles = []ProfileKey{ProfileCISA2026, ProfileNTIA2021, ProfileFDA}

// keyStrings is the form Score takes its profile keys in.
func keyStrings(keys []ProfileKey) []string {
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = string(k)
	}
	return out
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

// jsonFixture decodes a JSON fixture into a generic map for mutation.
func jsonFixture(t *testing.T, name string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(readFixture(t, name), &m); err != nil {
		t.Fatalf("decode fixture %s: %v", name, err)
	}
	return m
}

func encode(t *testing.T, m map[string]any) []byte {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return b
}

// clone deep-copies a decoded JSON object.
func clone(t *testing.T, m map[string]any) map[string]any {
	t.Helper()
	var c map[string]any
	if err := json.Unmarshal(encode(t, m), &c); err != nil {
		t.Fatal(err)
	}
	return c
}

func cdxMetadata(m map[string]any) map[string]any { return m["metadata"].(map[string]any) }

// removeProperty drops the properties with the name from a CycloneDX component.
func removeProperty(c map[string]any, name string) {
	var kept []any
	for _, p := range c["properties"].([]any) {
		if p.(map[string]any)["name"] != name {
			kept = append(kept, p)
		}
	}
	c["properties"] = kept
}

func setProperty(c map[string]any, name, value string) {
	removeProperty(c, name)
	c["properties"] = append(c["properties"].([]any), map[string]any{"name": name, "value": value})
}

func spdxCreationInfo(m map[string]any) map[string]any { return m["creationInfo"].(map[string]any) }

// scoreOK scores data and fails the test on an error.
func scoreOK(t *testing.T, data []byte, profiles ...ProfileKey) Report {
	t.Helper()
	if len(profiles) == 0 {
		profiles = allProfiles
	}
	r, err := Score(data, keyStrings(profiles), testEngineVersion, Options{})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	return r
}

func profileOf(t *testing.T, r Report, key ProfileKey) ProfileReport {
	t.Helper()
	for _, p := range r.Profiles {
		if p.Key == key {
			return p
		}
	}
	t.Fatalf("profile %s not in report", key)
	return ProfileReport{}
}

// checkOf finds a check by id in any profile or the structure block.
func checkOf(t *testing.T, r Report, id string) CheckResult {
	t.Helper()
	for _, p := range r.Profiles {
		for _, c := range p.Checks {
			if c.ID == id {
				return c
			}
		}
	}
	for _, c := range r.Structure.Checks {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("check %s not in report", id)
	return CheckResult{}
}

// compOf finds a component by ref in d.
func compOf(t *testing.T, d *Doc, ref string) Comp {
	t.Helper()
	for _, c := range d.Components {
		if c.Ref == ref {
			return c
		}
	}
	t.Fatalf("component %s not in document", ref)
	return Comp{}
}

// checkDeclaration finds a declared check and its profile by check id.
func checkDeclaration(t *testing.T, id string) (Profile, Check) {
	t.Helper()
	for _, p := range profiles {
		for _, c := range p.Checks {
			if c.ID == id {
				return p, c
			}
		}
	}
	t.Fatalf("check %s is not declared", id)
	return Profile{}, Check{}
}

func assertStatus(t *testing.T, r Report, id string, want Status) CheckResult {
	t.Helper()
	c := checkOf(t, r, id)
	if c.Status != want {
		t.Errorf("%s: status %s, want %s (passed %d/%d, failing %v, note %q)", id, c.Status, want, c.Passed, c.Total, c.Failing, c.Note)
	}
	return c
}

func contains(list []string, s string) bool {
	for _, e := range list {
		if e == s {
			return true
		}
	}
	return false
}

// manyComponentsCDX builds a CycloneDX 1.6 document with the metadata of full.cdx.json and n flat
// copies of its first component, each complete and in the dependency graph. mutate, when not nil,
// changes component i (0-based) after it is built.
func manyComponentsCDX(t *testing.T, n int, mutate func(i int, c map[string]any)) []byte {
	t.Helper()
	m := jsonFixture(t, "full.cdx.json")
	template := m["components"].([]any)[0].(map[string]any)
	var components, deps, refs []any
	for i := 0; i < n; i++ {
		c := clone(t, template)
		ref := fmt.Sprintf("c%04d", i)
		c["bom-ref"] = ref
		c["name"] = ref
		c["purl"] = fmt.Sprintf("pkg:npm/%s@1.0.0", ref)
		if mutate != nil {
			mutate(i, c)
		}
		components = append(components, c)
		deps = append(deps, map[string]any{"ref": ref, "dependsOn": []any{}})
		refs = append(refs, ref)
	}
	deps = append(deps, map[string]any{"ref": "app", "dependsOn": refs})
	m["components"] = components
	m["dependencies"] = deps
	return encode(t, m)
}

func lines(s string) []string { return strings.Split(strings.TrimRight(s, "\n"), "\n") }
