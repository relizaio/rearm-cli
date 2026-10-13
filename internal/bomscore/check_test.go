package bomscore

import (
	"sort"
	"strings"
	"testing"
)

// Test 7: the declarations.
func TestDeclarations(t *testing.T) {
	ids := map[string]bool{}
	docs := []*Doc{
		loadOK(t, readFixture(t, "full.cdx.json")),
		loadOK(t, readFixture(t, "full.spdx.json")),
		loadOK(t, readFixture(t, "full-2.2.spdx.json")),
		{NotRepresentable: map[Field]bool{}},
	}
	all := append([]Check(nil), structureChecks...)
	for _, p := range profiles {
		for _, c := range p.Checks {
			if !strings.HasPrefix(c.ID, string(p.Key)+".") {
				t.Errorf("%s is not prefixed with its profile %s", c.ID, p.Key)
			}
			all = append(all, c)
		}
	}
	for _, c := range all {
		if ids[c.ID] {
			t.Errorf("duplicate check id %s", c.ID)
		}
		ids[c.ID] = true
		if c.Ref == "" || c.Title == "" || c.Eval == nil {
			t.Errorf("%s: empty Ref, Title or Eval", c.ID)
		}
		if c.Level == LevelRequired {
			for _, d := range docs {
				if c.Eval(d).NotAssessed {
					t.Errorf("REQUIRED check %s returns NotAssessed", c.ID)
				}
			}
		}
	}

	r := scoreOK(t, readFixture(t, "full.cdx.json"))
	info := map[string]string{
		"cisa-2026.practice.updates":             notePractice,
		"cisa-2026.practice.coverage":            notePractice,
		"cisa-2026.practice.distribution":        notePractice,
		"cisa-2026.practice.unknowns":            notePractice,
		"cisa-2026.practice.frequency":           notePractice,
		"cisa-2026.practice.machine-processable": notePractice,
		"fda.document.vulnerabilities":           noteVulnerabilities,
	}
	for id, note := range info {
		c := assertStatus(t, r, id, StatusNotAssessed)
		if c.Level != LevelInfo || c.Note != note {
			t.Errorf("%s: level %s note %q, want INFO %q", id, c.Level, c.Note, note)
		}
	}
	counts := map[ProfileKey][2]int{ProfileCISA2026: {17, 6}, ProfileNTIA2021: {7, 0}, ProfileFDA: {10, 1}}
	for _, p := range profiles {
		var req, inf int
		for _, c := range p.Checks {
			if c.Level == LevelRequired {
				req++
			} else {
				inf++
			}
		}
		if want := counts[p.Key]; req != want[0] || inf != want[1] {
			t.Errorf("%s: %d REQUIRED + %d INFO, want %d + %d", p.Key, req, inf, want[0], want[1])
		}
	}
}

// Test 11: more than 20 failing components.
func TestFailingIsCappedAndSorted(t *testing.T) {
	data := manyComponentsCDX(t, 30, func(i int, c map[string]any) {
		if i >= 5 {
			delete(c, "hashes")
		}
	})
	c := assertStatus(t, scoreOK(t, data, ProfileCISA2026), "cisa-2026.component-hash-value", StatusFail)
	if c.Passed != 5 || c.Total != 30 || len(c.Failing) != 20 || !c.FailingTruncated {
		t.Errorf("%d/%d, %d failing, truncated %v; want 5/30, 20, true", c.Passed, c.Total, len(c.Failing), c.FailingTruncated)
	}
	if !sort.StringsAreSorted(c.Failing) || c.Failing[0] != "pkg:npm/c0005@1.0.0" {
		t.Errorf("failing %v, want sorted from pkg:npm/c0005@1.0.0", c.Failing)
	}
}

// Test 12: a panicking evaluator is ERROR, left out of the score, and does not stop the others.
func TestFaultIsolation(t *testing.T) {
	d := loadOK(t, readFixture(t, "full.cdx.json"))
	boom := Check{ID: "test.boom", Title: "Boom", Level: LevelRequired, Scope: ScopeDocument, Ref: "test",
		Eval: func(*Doc) Outcome { panic("evaluator exploded") }}
	p := Profile{Key: "test", Checks: append([]Check{boom}, profileCISA2026.Checks...)}

	pr, errs := scoreProfile(p, d)
	if pr.Checks[0].Status != StatusError || len(errs) != 1 || errs[0].Check != "test.boom" || errs[0].Message != "evaluator exploded" {
		t.Fatalf("status %s errors %+v, want ERROR and one entry", pr.Checks[0].Status, errs)
	}
	if pr.Verdict != VerdictUnknown || pr.Score == nil || *pr.Score != 100 || pr.Summary.Errors != 1 || pr.Summary.Pass != 17 {
		t.Errorf("verdict %s score %v summary %+v; want UNKNOWN, 100, 17 pass, 1 error", pr.Verdict, pr.Score, pr.Summary)
	}

	d.Timestamp = ""
	pr, _ = scoreProfile(p, d)
	if pr.Verdict != VerdictNotReady {
		t.Errorf("verdict %s with a failing REQUIRED check beside the ERROR, want NOT_READY", pr.Verdict)
	}
}
