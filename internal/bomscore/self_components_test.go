package bomscore

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

// SCORE-59 (parent SCORE-58 ADR-6): fda.component.support-level and fda.component.end-of-support
// skip components ReARM marked as the subject's own (reliza:support:selfComponent, exact value
// "true"), count them as selfComponentsSkipped and say so in the text report. Every other check
// judges them as before.

// selfProp is the marker with the literal value the server writes, or another value.
func selfProp(value string) map[string]any {
	return map[string]any{"name": propSelfComponent, "value": value}
}

// selfLine is the text line under a failed support check that skipped n self components.
func selfLine(n string) string {
	return "        skipped " + n + " of the subject's own components (reliza:support:selfComponent)"
}

// selfCDX is full.cdx.json plus six top-level components (name and bom-ref, no version, purl or
// hash): self-plain (library, marker), self-supported (library, marker and both support facts),
// self-false, self-upper and self-padded (library, marker valued false, TRUE and " true"), and
// self-key (cryptographic-asset, marker). 4 + 6 = 10 components.
func selfCDX(t *testing.T) map[string]any {
	t.Helper()
	m := jsonFixture(t, "full.cdx.json")
	components := m["components"].([]any)
	add := func(name, typ string, props ...any) {
		components = append(components, map[string]any{"bom-ref": name, "type": typ, "name": name, "properties": props})
	}
	add("self-plain", "library", selfProp("true"))
	add("self-supported", "library", selfProp("true"),
		map[string]any{"name": propSupportLevel, "value": "actively maintained"},
		map[string]any{"name": propEndOfSupport, "value": "2030-01-01T00:00:00Z"})
	add("self-false", "library", selfProp("false"))
	add("self-upper", "library", selfProp("TRUE"))
	add("self-padded", "library", selfProp(" true"))
	add("self-key", "cryptographic-asset", selfProp("true"))
	m["components"] = components
	return m
}

func assertNotSelfSkipped(t *testing.T, c CheckResult) {
	t.Helper()
	if c.SelfComponentsSkipped != 0 {
		t.Errorf("%s: selfComponentsSkipped %d, want 0", c.ID, c.SelfComponentsSkipped)
	}
}

// T-1: only the exact literal is honoured, the self skip runs before the type skip, and a self
// component with support facts is neither passed nor failing.
func TestSupportChecksSkipSelfComponents(t *testing.T) {
	r := scoreOK(t, encode(t, selfCDX(t)), ProfileFDA)
	if r.Input.Components != 10 {
		t.Errorf("input.components %d, want 10", r.Input.Components)
	}
	for _, id := range []string{supportLevelID, endOfSupportID} {
		c := assertStatus(t, r, id, StatusFail)
		if c.SelfComponentsSkipped != 3 {
			t.Errorf("%s: selfComponentsSkipped %d, want 3", id, c.SelfComponentsSkipped)
		}
		assertNotSkipped(t, c)
		if c.Total != 7 || c.Passed != 4 {
			t.Errorf("%s: %d/%d, want 4/7 (the 4 fixture components pass, false/TRUE/padded fail)", id, c.Passed, c.Total)
		}
		if want := []string{"self-false", "self-padded", "self-upper"}; !reflect.DeepEqual(c.Failing, want) {
			t.Errorf("%s: failing %v, want %v", id, c.Failing, want)
		}
	}
}

// Review item T-9 (ADR-1, the name half of design row T-1): only the exact property name is
// honoured (no case folding, no trimming, no prefix or suffix), and a second entry of the name
// valued otherwise does not cancel a "true" one, in either order. The server's sweep removes only
// names starting with "reliza:support:" as typed, so "Reliza:..." or " reliza:..." in an uploaded
// SBOM reaches the engine, which must not depend on the sweep.
func TestSupportChecksSelfNameExact(t *testing.T) {
	m := jsonFixture(t, "full.cdx.json")
	components := m["components"].([]any)
	add := func(name string, props ...any) {
		components = append(components, map[string]any{"bom-ref": name, "type": "library", "name": name, "properties": props})
	}
	named := func(name string) map[string]any { return map[string]any{"name": name, "value": "true"} }
	add("name-upper", named("Reliza:support:selfComponent"))
	add("name-lower", named("reliza:support:selfcomponent"))
	add("name-leading", named(" reliza:support:selfComponent"))
	add("name-trailing", named("reliza:support:selfComponent "))
	add("name-suffix", named("reliza:support:selfComponents"))
	add("name-prefix", named("reliza:support:self"))
	add("dup-false-true", selfProp("false"), selfProp("true"))
	add("dup-true-false", selfProp("true"), selfProp("false"))
	m["components"] = components
	r := scoreOK(t, encode(t, m), ProfileFDA)
	want := []string{"name-leading", "name-lower", "name-prefix", "name-suffix", "name-trailing", "name-upper"}
	for _, id := range []string{supportLevelID, endOfSupportID} {
		c := assertStatus(t, r, id, StatusFail)
		if c.SelfComponentsSkipped != 2 {
			t.Errorf("%s: selfComponentsSkipped %d, want 2 (the two duplicate-entry components)", id, c.SelfComponentsSkipped)
		}
		assertNotSkipped(t, c)
		if c.Total != 10 || c.Passed != 4 {
			t.Errorf("%s: %d/%d, want 4/10 (the 4 fixture components pass, the 6 near-miss names fail)", id, c.Passed, c.Total)
		}
		if !reflect.DeepEqual(c.Failing, want) {
			t.Errorf("%s: failing %v, want %v", id, c.Failing, want)
		}
	}
}

// Review item T-10 (SCORE-58 option A, "a marker, not a name match"): a component that shares the
// subject's name, version or purl, or whose name is the subject's bom-ref, but carries no marker
// is judged like any other.
func TestSupportChecksNoSubjectNameMatch(t *testing.T) {
	m := jsonFixture(t, "full.cdx.json")
	const purl = "pkg:generic/example-device-app@5.0.0"
	subject := m["metadata"].(map[string]any)["component"].(map[string]any)
	subject["purl"] = purl
	components := m["components"].([]any)
	components = append(components,
		// the subject listed again, as an export lists it: same name, version and purl
		map[string]any{"bom-ref": "app-listed", "type": "application", "name": "example-device-app", "version": "5.0.0", "purl": purl},
		// the subject's name and version, no purl
		map[string]any{"bom-ref": "app-copy", "type": "library", "name": "example-device-app", "version": "5.0.0"},
		// an entry whose name is the subject's bom-ref
		map[string]any{"bom-ref": "app-named", "type": "library", "name": "app"},
	)
	m["components"] = components
	r := scoreOK(t, encode(t, m), ProfileFDA)
	want := []string{"app", "example-device-app@5.0.0", purl} // shown by purl, else name@version, else name
	for _, id := range []string{supportLevelID, endOfSupportID} {
		c := assertStatus(t, r, id, StatusFail)
		assertNotSelfSkipped(t, c)
		assertNotSkipped(t, c)
		if c.Total != 7 || c.Passed != 4 {
			t.Errorf("%s: %d/%d, want 4/7 (the 4 fixture components pass, the 3 subject look-alikes fail)", id, c.Passed, c.Total)
		}
		if !reflect.DeepEqual(c.Failing, want) {
			t.Errorf("%s: failing %v, want %v", id, c.Failing, want)
		}
	}
}

// T-2: every other component check and every structure check judges the self components.
func TestOnlySupportChecksSkipSelf(t *testing.T) {
	r := scoreOK(t, encode(t, selfCDX(t)))
	compared := 0
	for _, p := range r.Profiles {
		for _, c := range p.Checks {
			if c.Scope != ScopeComponent || supportCheckIDs[c.ID] {
				continue
			}
			compared++
			if c.Total != 10 {
				t.Errorf("%s: total %d, want 10", c.ID, c.Total)
			}
			assertNotSelfSkipped(t, c)
			assertNotSkipped(t, c)
		}
	}
	if compared == 0 {
		t.Fatal("no component check compared")
	}
	for _, c := range r.Structure.Checks {
		if c.Scope == ScopeComponent && c.Total != 10 {
			t.Errorf("%s: total %d, want 10", c.ID, c.Total)
		}
		assertNotSelfSkipped(t, c)
	}
	if v := checkOf(t, r, "fda.baseline.version-string"); !contains(v.Failing, "self-plain") {
		t.Errorf("fda.baseline.version-string failing %v, want self-plain", v.Failing)
	}
}

// markSelf adds the marker to every component of cs at any depth.
func markSelf(cs []any) {
	for _, e := range cs {
		c := e.(map[string]any)
		props, _ := c["properties"].([]any)
		c["properties"] = append(props, selfProp("true"))
		if nested, ok := c["components"].([]any); ok {
			markSelf(nested)
		}
	}
}

// T-3: a document whose every component is self, or self and of a skipped type, fails both support
// checks at 0/0 with the note; a document without components carries neither the note nor the fields.
func TestSupportChecksAllSelf(t *testing.T) {
	const note = "no supportable component" // the literal text, not the production constant
	m := jsonFixture(t, "full.cdx.json")
	markSelf(m["components"].([]any))
	r := scoreOK(t, encode(t, m), ProfileFDA)
	for _, id := range []string{supportLevelID, endOfSupportID} {
		c := assertStatus(t, r, id, StatusFail)
		if c.Passed != 0 || c.Total != 0 || c.Note != note || len(c.Failing) != 0 {
			t.Errorf("%s: %d/%d note %q failing %v, want 0/0, note %q, none failing", id, c.Passed, c.Total, c.Note, c.Failing, note)
		}
		if c.SelfComponentsSkipped != 4 {
			t.Errorf("%s: selfComponentsSkipped %d, want 4", id, c.SelfComponentsSkipped)
		}
		assertNotSkipped(t, c)
	}
	if v := profileOf(t, r, ProfileFDA).Verdict; v != VerdictNotReady {
		t.Errorf("verdict %s, want NOT_READY", v)
	}

	// alpha and beta self, the nested gamma and delta typed data.
	m = jsonFixture(t, "full.cdx.json")
	top := m["components"].([]any)
	for _, e := range top {
		c := e.(map[string]any)
		c["properties"] = append(c["properties"].([]any), selfProp("true"))
	}
	gamma := gammaCDX(m)
	gamma["type"] = "data"
	gamma["components"].([]any)[0].(map[string]any)["type"] = "data"
	r = scoreOK(t, encode(t, m), ProfileFDA)
	for _, id := range []string{supportLevelID, endOfSupportID} {
		c := assertStatus(t, r, id, StatusFail)
		if c.SelfComponentsSkipped != 2 || c.Total != 0 || c.Note != note {
			t.Errorf("%s: selfComponentsSkipped %d total %d note %q, want 2, 0, %q", id, c.SelfComponentsSkipped, c.Total, c.Note, note)
		}
		assertSkipped(t, c, 2, "data")
	}
	if txt := r.Text(); strings.Count(txt, "note: "+note) != 2 {
		t.Errorf("want the note once per support check:\n%s", txt)
	}

	empty := jsonFixture(t, "full.cdx.json")
	empty["components"] = []any{}
	r = scoreOK(t, encode(t, empty), ProfileFDA)
	for _, id := range []string{supportLevelID, endOfSupportID} {
		c := assertStatus(t, r, id, StatusFail)
		if c.Total != 0 || c.Note != "" {
			t.Errorf("%s without components: total %d note %q, want 0 and no note", id, c.Total, c.Note)
		}
		assertNotSkipped(t, c)
		assertNotSelfSkipped(t, c)
	}
}

// assertSkipLines checks that each failing support check line of text is followed by want, in
// order, and then by its missing list.
func assertSkipLines(t *testing.T, text string, want ...string) {
	t.Helper()
	ls := lines(text)
	found := 0
	for i, l := range ls {
		if !strings.HasPrefix(l, "  FAIL  Software level of support  ") && !strings.HasPrefix(l, "  FAIL  End-of-support date  ") {
			continue
		}
		found++
		n := len(want)
		if i+n+1 >= len(ls) || !reflect.DeepEqual(ls[i+1:i+1+n], want) || !strings.HasPrefix(ls[i+1+n], "        missing in: ") {
			t.Errorf("after %q want %q then the missing list, got:\n%s", l, want, strings.Join(ls[i:], "\n"))
		}
	}
	if found != 2 {
		t.Errorf("%d failing support check lines, want 2:\n%s", found, text)
	}
}

// T-4 (a, b): the text line and the JSON field.
func TestSelfSkipReportOutput(t *testing.T) {
	m := selfCDX(t)
	delete(gammaCDX(m), "properties")
	assertSkipLines(t, scoreOK(t, encode(t, m), ProfileFDA).Text(), selfLine("3"))

	m = jsonFixture(t, "full.self.cdx.json")
	delete(gammaCDX(m), "properties")
	assertSkipLines(t, scoreOK(t, encode(t, m), ProfileFDA).Text(),
		"        skipped 2 components of type cryptographic-asset, file (not software packages)", selfLine("1"))

	m = jsonFixture(t, "full.cdx.json")
	delete(gammaCDX(m), "properties")
	if txt := scoreOK(t, encode(t, m), ProfileFDA).Text(); strings.Contains(txt, "subject's own") {
		t.Errorf("text of full.cdx.json mentions the subject's own components:\n%s", txt)
	}

	if j := reportJSON(t, scoreOK(t, readFixture(t, "full.cdx.json"), ProfileFDA)); bytes.Contains(j, []byte(`"selfComponentsSkipped"`)) {
		t.Error("fda report of full.cdx.json carries selfComponentsSkipped")
	}
	j := reportJSON(t, scoreOK(t, readFixture(t, "full.self.cdx.json"), ProfileFDA))
	key := []byte(`"selfComponentsSkipped"`)
	if n := bytes.Count(j, key); n != 2 {
		t.Fatalf("fda report of full.self.cdx.json: %d selfComponentsSkipped keys, want 2", n)
	}
	for rest, off := j, 0; ; {
		i := bytes.Index(rest, key)
		if i < 0 {
			break
		}
		at := off + i
		before := j[:at]
		idAt := bytes.LastIndex(before, []byte(`"id": "`))
		id := string(before[idAt+len(`"id": "`):])
		id = id[:strings.IndexByte(id, '"')]
		if !supportCheckIDs[id] {
			t.Errorf("selfComponentsSkipped on %s", id)
		}
		if typesAt := bytes.LastIndex(before, []byte(`"skippedTypes"`)); typesAt < idAt {
			t.Errorf("%s: selfComponentsSkipped not after skippedTypes", id)
		}
		off = at + len(key)
		rest = j[off:]
	}
}

// T-5: perSupportableComponent and evaluate.
func TestPerSupportableComponentSelf(t *testing.T) {
	self := []Property{{Name: propSelfComponent, Value: "true"}}
	comp := func(id, typ string, isSelf bool) Comp {
		c := Comp{DisplayID: id, Type: typ}
		if isSelf {
			c.Properties = self
		}
		return c
	}
	yes := func(*Comp) bool { return true }
	o := perSupportableComponent(&Doc{Format: FormatCycloneDX, Components: []Comp{
		comp("a", "library", true), comp("b", "data", false), comp("c", "data", true), comp("d", "library", false)}}, yes)
	if o.SelfSkipped != 2 || o.Skipped != 1 || !reflect.DeepEqual(o.SkippedTypes, []string{"data"}) || o.Total != 1 || o.Passed != 1 || o.Note != "" {
		t.Errorf("outcome %+v, want 2 self, 1 skipped [data], 1/1, no note", o)
	}
	o = perSupportableComponent(&Doc{Format: FormatCycloneDX, Components: []Comp{comp("a", "library", true), comp("b", "data", false)}}, yes)
	if o.Total != 0 || o.Note != noteNoSupportable || o.SelfSkipped != 1 || o.Skipped != 1 {
		t.Errorf("outcome %+v, want 1 self, 1 skipped, total 0 with the note", o)
	}

	d := &Doc{Format: FormatCycloneDX, Components: []Comp{comp("a", "library", true), comp("b", "library", false)}}
	res, cerr := evaluate(Check{ID: "x.self.panic", Scope: ScopeComponent, Level: LevelRequired, Eval: func(d *Doc) Outcome {
		perSupportableComponent(d, yes)
		panic("boom")
	}}, d)
	if cerr == nil || res.Status != StatusError || res.SelfComponentsSkipped != 0 {
		t.Errorf("panicking check: %+v %v, want ERROR with selfComponentsSkipped 0", res, cerr)
	}
	res, _ = evaluate(Check{ID: "x.self.ok", Scope: ScopeComponent, Level: LevelRequired, Eval: func(d *Doc) Outcome {
		return perSupportableComponent(d, yes)
	}}, d)
	if res.Status != StatusPass || res.Passed != 1 || res.Total != 1 || res.SelfComponentsSkipped != 1 || res.ComponentsSkipped != 0 {
		t.Errorf("check result %+v, want PASS 1/1 with selfComponentsSkipped 1 and componentsSkipped 0", res)
	}
}

// T-6: SPDX carries no marker; the CycloneDX XML loader reads it as the JSON one does.
func TestSelfComponentFormats(t *testing.T) {
	for _, f := range []string{"full.spdx.json", "full-2.2.spdx.json"} {
		r := scoreOK(t, readFixture(t, f))
		for _, p := range r.Profiles {
			for _, c := range p.Checks {
				assertNotSelfSkipped(t, c)
			}
		}
	}

	xml := string(readFixture(t, "full.cdx.xml"))
	const anchor = `        <property name="reliza:support:levelOfSupport">actively maintained</property>
        <property name="cdx:lifecycle:milestone:endOfSupport">2028-12-31</property>`
	if strings.Count(xml, anchor) != 1 {
		t.Fatal("fixture changed: alpha's support properties not found")
	}
	// alpha keeps no support facts, so it would fail both checks if it were judged.
	data := []byte(strings.Replace(xml, anchor, `        <property name="reliza:support:selfComponent">true</property>`, 1))
	if props := compOf(t, loadOK(t, data), "alpha").Properties; !contains(propNames(props, "true"), propSelfComponent) {
		t.Fatalf("alpha properties %v lack the marker", props)
	}
	r := scoreOK(t, data, ProfileFDA)
	for _, id := range []string{supportLevelID, endOfSupportID} {
		c := checkOf(t, r, id)
		if c.SelfComponentsSkipped != 1 || c.Total != 3 || contains(c.Failing, "pkg:npm/alpha@1.0.0") {
			t.Errorf("%s: selfComponentsSkipped %d total %d failing %v, want 1, 3, alpha not failing", id, c.SelfComponentsSkipped, c.Total, c.Failing)
		}
	}
}

// propNames lists the names of the properties with the value.
func propNames(props []Property, value string) []string {
	var out []string
	for _, p := range props {
		if p.Value == value {
			out = append(out, p.Name)
		}
	}
	return out
}
