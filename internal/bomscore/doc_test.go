package bomscore

import "testing"

// Test 6: the component set.
func TestComponentSet(t *testing.T) {
	d := loadOK(t, readFixture(t, "full.cdx.json"))
	var ids []string
	for _, c := range d.Components {
		ids = append(ids, c.DisplayID)
	}
	want := []string{"pkg:npm/alpha@1.0.0", "pkg:maven/org.beta/beta@2.0.0", gammaPurl, "pkg:golang/example.com/delta@v0.4.2"}
	if len(ids) != len(want) {
		t.Fatalf("component set %v, want %v (nested components counted, metadata.component left out)", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Errorf("component %d is %s, want %s", i, ids[i], want[i])
		}
	}

	// The component three levels deep is scored like the others.
	m := jsonFixture(t, "full.cdx.json")
	delete(gammaCDX(m)["components"].([]any)[0].(map[string]any), "version")
	got := assertStatus(t, scoreOK(t, encode(t, m), ProfileCISA2026), "cisa-2026.component-version", StatusFail)
	if got.Passed != 3 || got.Total != 4 || !contains(got.Failing, "pkg:golang/example.com/delta@v0.4.2") {
		t.Errorf("%d/%d failing %v, want 3/4 failing delta", got.Passed, got.Total, got.Failing)
	}

	// The SPDX described package is left out.
	if n := len(loadOK(t, readFixture(t, "full.spdx.json")).Components); n != 4 {
		t.Errorf("SPDX component set %d, want 4", n)
	}

	// No components: every COMPONENT check fails with 0/0.
	m = jsonFixture(t, "full.cdx.json")
	delete(m, "components")
	r := scoreOK(t, encode(t, m))
	for _, p := range r.Profiles {
		for _, c := range p.Checks {
			if c.Scope == ScopeComponent && (c.Status != StatusFail || c.Total != 0) {
				t.Errorf("%s: %s %d/%d, want FAIL 0/0", c.ID, c.Status, c.Passed, c.Total)
			}
		}
	}
}

func TestDisplayID(t *testing.T) {
	cases := []struct{ purl, name, version, ref, want string }{
		{"pkg:npm/a@1", "a", "1", "r", "pkg:npm/a@1"},
		{"", "a", "1", "r", "a@1"},
		{"", "a", "", "r", "a"},
		{"", "", "1", "r", "r"},
		{"NOASSERTION", "", "", "", "#7"},
	}
	for _, c := range cases {
		if got := displayID(c.purl, c.name, c.version, c.ref, 7); got != c.want {
			t.Errorf("displayID(%q, %q, %q, %q) = %q, want %q", c.purl, c.name, c.version, c.ref, got, c.want)
		}
	}
}
