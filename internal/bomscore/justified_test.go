package bomscore

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

// FDAW-13 (D5 of FDAW-1): the justified outcome of the two fda checks that read
// reliza:support:justifiedUnknown:*. The T-n names are the rows of the FDAW-13 architecture.

const (
	componentHashID = "fda.baseline.component-hash"
	deltaPurl       = "pkg:golang/example.com/delta@v0.4.2"
)

// justifiedCheckIDs are the only checks that may carry a justified count.
var justifiedCheckIDs = map[string]bool{componentHashID: true, endOfSupportID: true}

var allJustifiedReasons = []string{
	"NO_COMMITTED_END_OF_SUPPORT", "COMMUNITY_PROJECT", "SUPPLIER_PUBLISHES_NO_DATE",
	"DISTRIBUTION_PUBLISHES_NO_HASH", "VENDORED_SOURCE_NO_HASH", "OTHER",
}

// gammaWithout returns full.cdx.json with gamma's hash and end-of-support date removed (as asked).
func gammaWithout(t *testing.T, hash, endOfSupport bool) (map[string]any, map[string]any) {
	t.Helper()
	m := jsonFixture(t, "full.cdx.json")
	g := gammaCDX(m)
	if hash {
		delete(g, "hashes")
	}
	if endOfSupport {
		removeProperty(g, propEndOfSupport)
	}
	return m, g
}

func assertJustified(t *testing.T, c CheckResult, status Status, passed, justified, total int) {
	t.Helper()
	if c.Status != status || c.Passed != passed || c.Justified != justified || c.Total != total {
		t.Errorf("%s: %s passed %d justified %d total %d, want %s %d %d %d (failing %v)",
			c.ID, c.Status, c.Passed, c.Justified, c.Total, status, passed, justified, total, c.Failing)
	}
}

// T-1: the new fixture under fda.
func TestJustifiedFixture(t *testing.T) {
	r := scoreOK(t, readFixture(t, "full.justified.cdx.json"), ProfileFDA)
	hash := checkOf(t, r, componentHashID)
	assertJustified(t, hash, StatusPass, 4, 1, 5)
	if len(hash.Failing) != 0 {
		t.Errorf("component-hash failing %v, want none", hash.Failing)
	}
	eos := checkOf(t, r, endOfSupportID)
	assertJustified(t, eos, StatusFail, 3, 1, 5)
	if !reflect.DeepEqual(eos.Failing, []string{deltaPurl}) {
		t.Errorf("end-of-support failing %v, want [%s]", eos.Failing, deltaPurl)
	}
	p := profileOf(t, r, ProfileFDA)
	if p.Verdict != VerdictNotReady || p.Score == nil || *p.Score != 98 {
		t.Errorf("verdict %s score %v, want NOT_READY 98", p.Verdict, p.Score)
	}
}

// T-2: mutations of gamma in full.cdx.json.
func TestJustifiedMutations(t *testing.T) {
	t.Run("value and property: the value wins", func(t *testing.T) {
		m := jsonFixture(t, "full.cdx.json")
		g := gammaCDX(m)
		setProperty(g, propJustifiedHash, "OTHER")
		setProperty(g, propJustifiedEndOfSupport, "OTHER")
		r := scoreOK(t, encode(t, m), ProfileFDA)
		assertJustified(t, checkOf(t, r, componentHashID), StatusPass, 4, 0, 4)
		assertJustified(t, checkOf(t, r, endOfSupportID), StatusPass, 4, 0, 4)
	})

	for _, code := range allJustifiedReasons {
		for _, row := range []struct{ check, prop string }{
			{componentHashID, propJustifiedHash}, {endOfSupportID, propJustifiedEndOfSupport},
		} {
			t.Run("recognised "+code+" on "+row.check, func(t *testing.T) {
				m, g := gammaWithout(t, row.check == componentHashID, row.check == endOfSupportID)
				setProperty(g, row.prop, code)
				c := checkOf(t, scoreOK(t, encode(t, m), ProfileFDA), row.check)
				assertJustified(t, c, StatusPass, 3, 1, 4)
				if len(c.Failing) != 0 {
					t.Errorf("failing %v, want none", c.Failing)
				}
			})
		}
	}

	for _, value := range []string{"OTHER ", "other", "Other", "", "NONE", " OTHER"} {
		for _, row := range []struct{ check, prop string }{
			{componentHashID, propJustifiedHash}, {endOfSupportID, propJustifiedEndOfSupport},
		} {
			t.Run("unrecognised "+strings.ReplaceAll(value, " ", "_")+" on "+row.check, func(t *testing.T) {
				m, g := gammaWithout(t, row.check == componentHashID, row.check == endOfSupportID)
				setProperty(g, row.prop, value)
				c := checkOf(t, scoreOK(t, encode(t, m), ProfileFDA), row.check)
				assertJustified(t, c, StatusFail, 3, 0, 4)
				if !contains(c.Failing, gammaPurl) {
					t.Errorf("failing %v, want %s", c.Failing, gammaPurl)
				}
			})
		}
	}

	t.Run("the other field's property does not justify", func(t *testing.T) {
		m, g := gammaWithout(t, true, true)
		setProperty(g, propJustifiedHash, "NO_COMMITTED_END_OF_SUPPORT")
		setProperty(g, "reliza:support:justification", "the supplier publishes no support policy")
		r := scoreOK(t, encode(t, m), ProfileFDA)
		assertJustified(t, checkOf(t, r, componentHashID), StatusPass, 3, 1, 4)
		assertJustified(t, checkOf(t, r, endOfSupportID), StatusFail, 3, 0, 4)
	})

	t.Run("both missing, both justified: READY 100", func(t *testing.T) {
		m, g := gammaWithout(t, true, true)
		setProperty(g, propJustifiedHash, "DISTRIBUTION_PUBLISHES_NO_HASH")
		setProperty(g, propJustifiedEndOfSupport, "SUPPLIER_PUBLISHES_NO_DATE")
		r := scoreOK(t, encode(t, m), ProfileFDA)
		assertJustified(t, checkOf(t, r, componentHashID), StatusPass, 3, 1, 4)
		assertJustified(t, checkOf(t, r, endOfSupportID), StatusPass, 3, 1, 4)
		p := profileOf(t, r, ProfileFDA)
		if p.Verdict != VerdictReady || p.Score == nil || *p.Score != 100 {
			t.Errorf("verdict %s score %v, want READY 100", p.Verdict, p.Score)
		}
	})

	t.Run("a skipped type is skipped, not justified, under end-of-support only", func(t *testing.T) {
		m, g := gammaWithout(t, true, true)
		g["type"] = "cryptographic-asset"
		setProperty(g, propJustifiedEndOfSupport, "NO_COMMITTED_END_OF_SUPPORT")
		setProperty(g, propJustifiedHash, "VENDORED_SOURCE_NO_HASH")
		r := scoreOK(t, encode(t, m), ProfileFDA)
		eos := checkOf(t, r, endOfSupportID)
		assertJustified(t, eos, StatusPass, 3, 0, 3)
		if eos.ComponentsSkipped != 1 || !reflect.DeepEqual(eos.SkippedTypes, []string{"cryptographic-asset"}) {
			t.Errorf("end-of-support skipped %d %v, want 1 [cryptographic-asset]", eos.ComponentsSkipped, eos.SkippedTypes)
		}
		assertJustified(t, checkOf(t, r, componentHashID), StatusPass, 3, 1, 4)
	})

	t.Run("a self component is self-skipped, not justified", func(t *testing.T) {
		m, g := gammaWithout(t, false, true)
		setProperty(g, propSelfComponent, "true")
		setProperty(g, propJustifiedEndOfSupport, "NO_COMMITTED_END_OF_SUPPORT")
		eos := checkOf(t, scoreOK(t, encode(t, m), ProfileFDA), endOfSupportID)
		assertJustified(t, eos, StatusPass, 3, 0, 3)
		if eos.SelfComponentsSkipped != 1 {
			t.Errorf("selfComponentsSkipped %d, want 1", eos.SelfComponentsSkipped)
		}
	})
}

// justifiedKeyChecks returns the ids of the checks whose JSON carries "justified", and fails when
// the key is not right after "total".
func justifiedKeyChecks(t *testing.T, j []byte) []string {
	t.Helper()
	key := []byte(`"justified"`)
	var ids []string
	for off := 0; ; {
		i := bytes.Index(j[off:], key)
		if i < 0 {
			return ids
		}
		at := off + i
		before := j[:at]
		idAt := bytes.LastIndex(before, []byte(`"id": "`))
		id := string(before[idAt+len(`"id": "`):])
		id = id[:strings.IndexByte(id, '"')]
		ids = append(ids, id)
		prev := bytes.TrimRight(before, " \n")
		if line := prev[bytes.LastIndexByte(prev, '\n')+1:]; !bytes.HasPrefix(bytes.TrimSpace(line), []byte(`"total": `)) {
			t.Errorf("%s: justified follows %q, want the total", id, bytes.TrimSpace(line))
		}
		if !bytes.HasPrefix(bytes.TrimLeft(j[at+len(key):], ": 0123456789"), []byte(",\n")) {
			t.Errorf("%s: justified is not an integer member", id)
		}
		if f := bytes.Index(j[at:], []byte(`"failing"`)); f < 0 || bytes.Contains(j[at:at+f], []byte(`"id"`)) {
			t.Errorf("%s: justified is not before the check's failing", id)
		}
		off = at + len(key)
	}
}

// T-3: the JSON key, after total and only where a check justified a component.
func TestJustifiedJSONShape(t *testing.T) {
	for _, f := range []string{"full.cdx.json", "full.self.cdx.json", "full.types.cdx.json", "full.spdx.json"} {
		for _, key := range allProfiles {
			if ids := justifiedKeyChecks(t, reportJSON(t, scoreOK(t, readFixture(t, f), key))); len(ids) != 0 {
				t.Errorf("%s %s: justified on %v, want none", f, key, ids)
			}
		}
	}
	ids := justifiedKeyChecks(t, reportJSON(t, scoreOK(t, readFixture(t, "full.justified.cdx.json"))))
	if len(ids) != 2 {
		t.Fatalf("full.justified.cdx.json: justified on %v, want 2 checks", ids)
	}
	for _, id := range ids {
		if !justifiedCheckIDs[id] {
			t.Errorf("justified on %s", id)
		}
	}
}

// T-4: the text output.
func TestJustifiedText(t *testing.T) {
	text := scoreOK(t, readFixture(t, "full.justified.cdx.json"), ProfileFDA).Text()
	want := []string{
		"fda  FDA Premarket SBOM (2026)  NOT_READY  score 98",
		"  PASS (1 justified)  Component Hash  4/5  (V.A.4(b); NTIA Framing 2nd ed. 2.2, Component Hash)",
		"  FAIL (1 justified)  End-of-support date  3/5  (V.A.4(b))",
		"        missing in: " + deltaPurl,
		"        remedy: " + remedySupport,
		"  10 required: 9 pass, 1 fail, 0 error; 1 not assessed",
	}
	if got := lines(text); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("text:\n%s\nwant:\n%s", text, strings.Join(want, "\n"))
	}

	plain := scoreOK(t, readFixture(t, "full.cdx.json"), ProfileFDA).Text()
	if strings.Contains(plain, "justified") || strings.Contains(plain, "  PASS") {
		t.Errorf("text of full.cdx.json prints a justified or passing check:\n%s", plain)
	}

	// A justified PASS keeps the skip lines and prints no remedy.
	m, g := gammaWithout(t, false, true)
	setProperty(g, propJustifiedEndOfSupport, "COMMUNITY_PROJECT")
	setProperty(gammaCDX(m)["components"].([]any)[0].(map[string]any), propSelfComponent, "true")
	got := lines(scoreOK(t, encode(t, m), ProfileFDA).Text())
	wantPass := []string{
		"  PASS (1 justified)  End-of-support date  2/3  (V.A.4(b))",
		"        skipped 1 of the subject's own components (" + propSelfComponent + ")",
		"  10 required: 10 pass, 0 fail, 0 error; 1 not assessed",
	}
	if strings.Join(got[1:], "\n") != strings.Join(wantPass, "\n") {
		t.Errorf("text:\n%s\nwant after the head:\n%s", strings.Join(got, "\n"), strings.Join(wantPass, "\n"))
	}
}

// T-5: scoreOf and evaluate with justified components.
func TestJustifiedScoreAndEvaluate(t *testing.T) {
	req := func(status Status, passed, justified, total int) CheckResult {
		return CheckResult{Level: LevelRequired, Status: status, Passed: passed, Justified: justified, Total: total}
	}
	full := req(StatusPass, 1, 0, 1)
	for _, c := range []struct {
		name string
		rows []CheckResult
		want int
	}{
		{"3 passed 1 justified of 5 is 4/5", []CheckResult{req(StatusFail, 3, 1, 5)}, 80},
		{"0 passed 5 justified of 5 is 1", []CheckResult{req(StatusPass, 0, 5, 5)}, 100},
		{"0/0 counts as 0", []CheckResult{full, req(StatusFail, 0, 0, 0)}, 50},
		{"a justified INFO or NOT_ASSESSED check is not counted", []CheckResult{
			full, {Level: LevelInfo, Status: StatusFail, Justified: 5, Total: 5},
			{Level: LevelRequired, Status: StatusNotAssessed, Justified: 5, Total: 5}}, 100},
	} {
		if s := scoreOf(c.rows); s == nil || *s != c.want {
			t.Errorf("%s: score %v, want %d", c.name, s, c.want)
		}
	}

	eval := func(o Outcome) CheckResult {
		res, _ := evaluate(Check{ID: "x.justified", Scope: ScopeComponent, Level: LevelRequired, Eval: func(*Doc) Outcome { return o }}, &Doc{})
		return res
	}
	if r := eval(Outcome{Passed: 3, Justified: 2, Total: 5}); r.Status != StatusPass || r.Justified != 2 {
		t.Errorf("3+2 of 5: %s justified %d, want PASS 2", r.Status, r.Justified)
	}
	if r := eval(Outcome{Passed: 2, Justified: 2, Total: 5, Failing: []string{"x"}}); r.Status != StatusFail || r.Justified != 2 {
		t.Errorf("2+2 of 5: %s justified %d, want FAIL 2", r.Status, r.Justified)
	}
	if r := eval(Outcome{}); r.Status != StatusFail || r.Justified != 0 {
		t.Errorf("0/0: %s justified %d, want FAIL 0", r.Status, r.Justified)
	}
	res, cerr := evaluate(Check{ID: "x.justified.panic", Scope: ScopeComponent, Level: LevelRequired, Eval: func(*Doc) Outcome {
		panic("boom")
	}}, &Doc{})
	if cerr == nil || res.Status != StatusError || res.Justified != 0 {
		t.Errorf("panicking check: %+v %v, want ERROR with justified 0", res, cerr)
	}

	// The per-component helpers: value first, then the justification, else failing.
	has := func(c *Comp) bool { return c.Ref == "has" }
	just := func(c *Comp) bool { return c.Ref != "none" }
	d := &Doc{Format: FormatCycloneDX, Components: []Comp{{Ref: "has", DisplayID: "has"}, {Ref: "just", DisplayID: "just"}, {Ref: "none", DisplayID: "none"}}}
	for name, o := range map[string]Outcome{"perComponentJ": perComponentJ(d, has, just), "perSupportableComponentJ": perSupportableComponentJ(d, has, just)} {
		if o.Passed != 1 || o.Justified != 1 || o.Total != 3 || !reflect.DeepEqual(o.Failing, []string{"none"}) {
			t.Errorf("%s: %+v, want 1 passed, 1 justified, 3 total, failing [none]", name, o)
		}
	}
	if o := perComponent(d, has); o.Justified != 0 || len(o.Failing) != 2 {
		t.Errorf("perComponent: %+v, want no justified and 2 failing", o)
	}
}

// T-6: cisa-2026 and ntia-2021 do not read the properties.
func TestJustifiedFDAOnly(t *testing.T) {
	for _, row := range []struct {
		name string
		hash bool
		prop string
		code string
	}{
		{"hash", true, propJustifiedHash, "VENDORED_SOURCE_NO_HASH"},
		{"end of support", false, propJustifiedEndOfSupport, "NO_COMMITTED_END_OF_SUPPORT"},
	} {
		t.Run(row.name, func(t *testing.T) {
			m, g := gammaWithout(t, row.hash, !row.hash)
			plain := encode(t, m)
			setProperty(g, row.prop, row.code)
			justified := encode(t, m)
			for _, key := range []ProfileKey{ProfileCISA2026, ProfileNTIA2021} {
				a, b := scoreOK(t, plain, key), scoreOK(t, justified, key)
				if !reflect.DeepEqual(a.Profiles, b.Profiles) {
					t.Errorf("%s: the profile block differs with %s", key, row.prop)
				}
				if ids := justifiedKeyChecks(t, reportJSON(t, b)); len(ids) != 0 {
					t.Errorf("%s: justified on %v", key, ids)
				}
				if row.hash && key == ProfileCISA2026 {
					c := assertStatus(t, b, "cisa-2026.component-hash-value", StatusFail)
					if !contains(c.Failing, gammaPurl) {
						t.Errorf("cisa-2026.component-hash-value failing %v, want %s", c.Failing, gammaPurl)
					}
				}
			}
		})
	}
}

// T-7: SPDX has no carrier for the properties, so the fda checks on SPDX never justify.
func TestJustifiedSPDX(t *testing.T) {
	for _, f := range []string{"full.spdx.json", "full-2.2.spdx.json", "full.spdx", "full.spdx.yaml"} {
		r := scoreOK(t, readFixture(t, f), ProfileFDA)
		for _, id := range []string{componentHashID, endOfSupportID} {
			if c := checkOf(t, r, id); c.Justified != 0 {
				t.Errorf("%s %s: justified %d, want 0", f, id, c.Justified)
			}
		}
	}
}
