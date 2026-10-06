package bomscore

import (
	"reflect"
	"testing"
)

// Test 13: the structure checks.
func TestStructureChecks(t *testing.T) {
	green := scoreOK(t, readFixture(t, "full.cdx.json"))
	for _, c := range green.Structure.Checks {
		if c.Status != StatusPass || c.Level != LevelInfo {
			t.Errorf("%s: %s %s, want INFO PASS on the full fixture", c.ID, c.Level, c.Status)
		}
	}
	if len(green.Structure.Checks) != 3 {
		t.Fatalf("%d structure checks, want 3", len(green.Structure.Checks))
	}

	cases := []struct {
		name   string
		mutate func(m map[string]any)
		check  string
	}{
		{"invalid purl", func(m map[string]any) { gammaCDX(m)["purl"] = "not-a-purl" }, "structure.purl-valid"},
		{"dependency ref to a missing bom-ref", func(m map[string]any) {
			deps := m["dependencies"].([]any)
			deps[len(deps)-1].(map[string]any)["dependsOn"] = []any{"no-such-ref"}
		}, "structure.refs-resolve"},
		{"duplicate bom-ref", func(m map[string]any) { gammaCDX(m)["bom-ref"] = "alpha" }, "structure.refs-resolve"},
		{"orphan component", func(m map[string]any) {
			orphan := clone(t, m["components"].([]any)[0].(map[string]any))
			orphan["bom-ref"] = "orphan"
			orphan["purl"] = "pkg:npm/orphan@1.0.0"
			m["components"] = append(m["components"].([]any), orphan)
		}, "structure.no-orphans"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := jsonFixture(t, "full.cdx.json")
			c.mutate(m)
			r := scoreOK(t, encode(t, m))
			assertStatus(t, r, c.check, StatusFail)
			for _, s := range r.Structure.Checks {
				if s.ID != c.check && s.Status != StatusPass {
					t.Errorf("%s: %s, want PASS", s.ID, s.Status)
				}
			}
			for i, p := range r.Profiles {
				if p.Verdict != green.Profiles[i].Verdict || !reflect.DeepEqual(p.Score, green.Profiles[i].Score) {
					t.Errorf("%s: %s %v, want %s %v as without the defect", p.Key, p.Verdict, *p.Score, green.Profiles[i].Verdict, *green.Profiles[i].Score)
				}
			}
		})
	}

	// A dependency may name a service or a part of metadata.component: both resolve.
	m := jsonFixture(t, "full.cdx.json")
	m["services"] = []any{map[string]any{"bom-ref": "svc", "name": "api", "services": []any{map[string]any{"bom-ref": "svc-inner", "name": "inner"}}}}
	cdxMetadata(m)["component"].(map[string]any)["components"] = []any{map[string]any{"bom-ref": "app-part", "type": "library", "name": "part"}}
	m["dependencies"] = append(m["dependencies"].([]any), map[string]any{"ref": "svc", "dependsOn": []any{"svc-inner", "app-part"}})
	r := scoreOK(t, encode(t, m))
	assertStatus(t, r, "structure.refs-resolve", StatusPass)
	if r.Input.Components != 4 {
		t.Errorf("components %d, want 4: services and parts of metadata.component are not in the set", r.Input.Components)
	}

	if s := scoreOK(t, readFixture(t, "full.spdx.json")).Structure.Checks; len(s) != 0 {
		t.Errorf("SPDX structure checks %+v, want none", s)
	}
}
