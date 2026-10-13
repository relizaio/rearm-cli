package bomscore

import (
	"strings"
	"testing"
)

// Test 5: the two FDA component checks.
func TestFDAComponentChecks(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(g map[string]any)
		check  string
	}{
		{"support level in other case", func(g map[string]any) { setProperty(g, propSupportLevel, "Actively Maintained") }, "fda.component.support-level"},
		{"support level unknown", func(g map[string]any) { setProperty(g, propSupportLevel, "unknown") }, "fda.component.support-level"},
		{"unparsable end-of-support date", func(g map[string]any) { setProperty(g, propEndOfSupport, "31/12/2028") }, "fda.component.end-of-support"},
		{"end-of-support not a date", func(g map[string]any) { setProperty(g, propEndOfSupport, "soon") }, "fda.component.end-of-support"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := jsonFixture(t, "full.cdx.json")
			c.mutate(gammaCDX(m))
			r := scoreOK(t, encode(t, m), ProfileFDA)
			got := assertStatus(t, r, c.check, StatusFail)
			if !contains(got.Failing, gammaPurl) {
				t.Errorf("failing %v, want %s", got.Failing, gammaPurl)
			}
		})
	}

	t.Run("justification alone passes neither", func(t *testing.T) {
		m := jsonFixture(t, "full.cdx.json")
		g := gammaCDX(m)
		removeProperty(g, propSupportLevel)
		removeProperty(g, propEndOfSupport)
		setProperty(g, "reliza:support:justification", "the supplier publishes no support policy")
		r := scoreOK(t, encode(t, m), ProfileFDA)
		assertStatus(t, r, "fda.component.support-level", StatusFail)
		assertStatus(t, r, "fda.component.end-of-support", StatusFail)
	})

	t.Run("all three support levels pass", func(t *testing.T) {
		r := scoreOK(t, readFixture(t, "full.cdx.json"), ProfileFDA)
		assertStatus(t, r, "fda.component.support-level", StatusPass)
	})

	t.Run("end-of-support as a date-time", func(t *testing.T) {
		m := jsonFixture(t, "full.cdx.json")
		setProperty(gammaCDX(m), propEndOfSupport, "2027-05-01T10:00:00+02:00")
		assertStatus(t, scoreOK(t, encode(t, m), ProfileFDA), "fda.component.end-of-support", StatusPass)
	})

	t.Run("SPDX 2.3 validUntilDate passes end-of-support", func(t *testing.T) {
		r := scoreOK(t, readFixture(t, "full.spdx.json"), ProfileFDA)
		assertStatus(t, r, "fda.component.end-of-support", StatusPass)
	})

	t.Run("SPDX 2.2 cannot carry end-of-support", func(t *testing.T) {
		r := scoreOK(t, readFixture(t, "full-2.2.spdx.json"), ProfileFDA)
		got := assertStatus(t, r, "fda.component.end-of-support", StatusFail)
		if got.Note != "not representable in SPDX 2.2" || got.Passed != 0 || got.Total != 4 {
			t.Errorf("note %q %d/%d, want the not-representable note and 0/4", got.Note, got.Passed, got.Total)
		}
	})
}

// Test 22, O1: fda is built on the eight Framing baseline attributes, Component Hash included.
func TestOperatorPoint_O1(t *testing.T) {
	var hash *Check
	baseline := 0
	for i, c := range profileFDA.Checks {
		if c.ID == "fda.baseline.component-hash" {
			hash = &profileFDA.Checks[i]
		}
		if strings.HasPrefix(c.ID, "fda.baseline.") {
			baseline++
		}
	}
	if hash == nil || hash.Level != LevelRequired || baseline != 8 {
		t.Fatalf("fda.baseline.component-hash %+v, %d baseline checks; want it REQUIRED among 8", hash, baseline)
	}
	m := jsonFixture(t, "full.cdx.json")
	gammaCDX(m)["hashes"] = []any{}
	if v := profileOf(t, scoreOK(t, encode(t, m), ProfileFDA), ProfileFDA).Verdict; v != VerdictNotReady {
		t.Errorf("fda without a component hash: %s, want NOT_READY", v)
	}
}
