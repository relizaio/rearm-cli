package bomscore

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// The behaviour tests of the published rule list, docs/fda-checker-rules.md: one function for each
// statement of the list that says how the engine decides. Every cell scores a small document
// through Score and compares status, passed and total as one value. A change that makes one of
// these fail changes what the list says: it needs a new sentence and a new rule set number.

type rl = map[string]any

const (
	rlAuthor   = "fda.baseline.author-name"
	rlTime     = "fda.baseline.timestamp"
	rlSupplier = "fda.baseline.supplier-name"
	rlName     = "fda.baseline.component-name"
	rlVersion  = "fda.baseline.version-string"
	rlHash     = "fda.baseline.component-hash"
	rlIdent    = "fda.baseline.unique-identifier"
	rlRel      = "fda.baseline.relationship"
	rlLevel    = "fda.component.support-level"
	rlEOS      = "fda.component.end-of-support"
	rlVuln     = "fda.document.vulnerabilities"

	rlSHA = "2c26b46b68ffc68ff99b453c1d30413413422d706483bfa0f98a5e886266e7ae"
)

func rlProp(name, value string) rl { return rl{"name": name, "value": value} }

// rlBare is a CycloneDX component that passes the seven baseline component checks and carries the
// given properties only.
func rlBare(ref string, props ...rl) rl {
	return rl{"type": "library", "bom-ref": ref, "name": ref, "version": "1.0", "purl": "pkg:npm/" + ref + "@1.0",
		"supplier": rl{"name": "S"}, "hashes": []any{rl{"alg": "SHA-256", "content": rlSHA}},
		"properties": append([]rl{}, props...)}
}

// rlGood is rlBare with a level of support and an end-of-support date: it passes every check.
func rlGood(ref string) rl {
	return rlBare(ref, rlProp(propSupportLevel, "abandoned"), rlProp(propEndOfSupport, "2030-01-01"))
}

// rlCDX is a CycloneDX document that is READY with the components given: the described component
// is "root", and a complete composition states that it has no dependencies.
func rlCDX(comps []rl, mut func(rl)) []byte {
	d := rl{"bomFormat": "CycloneDX", "specVersion": "1.6", "version": 1,
		"metadata": rl{"timestamp": "2026-10-01T12:00:00Z", "authors": []any{rl{"name": "A"}},
			"component": rl{"type": "application", "bom-ref": "root", "name": "root", "version": "1"}},
		"components":   comps,
		"compositions": []any{rl{"aggregate": "complete", "dependencies": []any{"root"}}}}
	if mut != nil {
		mut(d)
	}
	b, _ := json.Marshal(d)
	return b
}

func rlMeta(d rl) rl { return d["metadata"].(rl) }

func rlRelation(a, kind, b string) rl {
	return rl{"spdxElementId": a, "relationshipType": kind, "relatedSpdxElement": b}
}

// rlSPDX is an SPDX JSON document of the version given: it describes the package "app" and has two
// more, "a" and "b", which pass the seven baseline component checks and carry validUntilDate.
func rlSPDX(version string, rels []rl, mut func(d rl, pkgs []rl)) []byte {
	pkg := func(id string) rl {
		return rl{"SPDXID": "SPDXRef-" + id, "name": id, "versionInfo": "1.0", "supplier": "Organization: S", "downloadLocation": "NOASSERTION",
			"filesAnalyzed": false, "checksums": []any{rl{"algorithm": "SHA256", "checksumValue": rlSHA}},
			"externalRefs":   []any{rl{"referenceCategory": "PACKAGE-MANAGER", "referenceType": "purl", "referenceLocator": "pkg:npm/" + id + "@1.0"}},
			"validUntilDate": "2030-01-01T00:00:00Z"}
	}
	pkgs := []rl{pkg("app"), pkg("a"), pkg("b")}
	d := rl{"spdxVersion": "SPDX-" + version, "dataLicense": "CC0-1.0", "SPDXID": "SPDXRef-DOCUMENT", "name": "n",
		"documentNamespace": "https://example.com/n",
		"creationInfo":      rl{"created": "2026-10-01T12:00:00Z", "creators": []any{"Organization: O"}},
		"packages":          pkgs,
		"relationships":     append([]rl{rlRelation("SPDXRef-DOCUMENT", "DESCRIBES", "SPDXRef-app")}, rels...)}
	if mut != nil {
		mut(d, pkgs)
	}
	b, _ := json.Marshal(d)
	return b
}

var rlAppDependsOnA = []rl{rlRelation("SPDXRef-app", "DEPENDS_ON", "SPDXRef-a")}

type rlReport struct {
	verdict Verdict
	checks  map[string]CheckResult
	ids     []string
	options Options
}

func rlScore(t *testing.T, data []byte, opts Options) rlReport {
	t.Helper()
	r, err := Score(data, []string{string(ProfileFDA)}, testEngineVersion, opts)
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	out := rlReport{verdict: r.Profiles[0].Verdict, checks: map[string]CheckResult{}, options: r.Options}
	for _, c := range r.Profiles[0].Checks {
		out.checks[c.ID] = c
		out.ids = append(out.ids, c.ID)
	}
	return out
}

// is formats one check as "STATUS passed/total".
func (r rlReport) is(id string) string {
	c := r.checks[id]
	return fmt.Sprintf("%s %d/%d", c.Status, c.Passed, c.Total)
}

func rlWant(t *testing.T, cell string, r rlReport, id, want string) {
	t.Helper()
	if got := r.is(id); got != want {
		t.Errorf("%s: %s is %s (note %q), want %s", cell, id, got, r.checks[id].Note, want)
	}
}

func rlWantNote(t *testing.T, cell string, r rlReport, id, want, note string) {
	t.Helper()
	rlWant(t, cell, r, id, want)
	if got := r.checks[id].Note; got != note {
		t.Errorf("%s: %s note %q, want %q", cell, id, got, note)
	}
}

// The control of every cell below, and "in the order of the report": the base documents pass each
// check they can, and the report lists the checks in the order of the declarations.
func TestFDARuleControl(t *testing.T) {
	var declared []string
	for _, c := range profileFDA.Checks {
		declared = append(declared, c.ID)
	}
	r := rlScore(t, rlCDX([]rl{rlGood("a")}, nil), Options{})
	if got, want := strings.Join(r.ids, " "), strings.Join(declared, " "); got != want {
		t.Errorf("checks in the report: %s, want %s", got, want)
	}
	if r.verdict != VerdictReady {
		t.Errorf("CycloneDX control: verdict %s, want READY", r.verdict)
	}
	for _, id := range []string{rlAuthor, rlTime, rlRel} {
		rlWant(t, "CycloneDX control", r, id, "PASS 1/1")
	}
	for _, id := range []string{rlSupplier, rlName, rlVersion, rlHash, rlIdent, rlLevel, rlEOS} {
		rlWant(t, "CycloneDX control", r, id, "PASS 1/1")
	}
	s := rlScore(t, rlSPDX("2.3", rlAppDependsOnA, nil), Options{})
	for _, id := range []string{rlAuthor, rlTime, rlRel} {
		rlWant(t, "SPDX control", s, id, "PASS 1/1")
	}
	for _, id := range []string{rlSupplier, rlName, rlVersion, rlHash, rlIdent, rlEOS} {
		rlWant(t, "SPDX control", s, id, "PASS 2/2")
	}
}

// fda.baseline.author-name.
func TestFDARuleAuthorName(t *testing.T) {
	cdx := []struct {
		cell string
		mut  func(m rl)
		want string
	}{
		{"metadata.authors[].name alone", func(m rl) {}, "PASS 1/1"},
		{"metadata.manufacturer.name alone", func(m rl) { delete(m, "authors"); m["manufacturer"] = rl{"name": "M"} }, "PASS 1/1"},
		{"a tool, metadata.supplier and metadata.manufacture only", func(m rl) {
			delete(m, "authors")
			m["supplier"] = rl{"name": "S"}
			m["manufacture"] = rl{"name": "M"}
			m["tools"] = rl{"components": []any{rl{"type": "application", "name": "tool", "version": "1"}}}
		}, "FAIL 0/1"},
		// Any of the places, in any order: a reader of the first or the last place only fails one of these.
		{"a blank author before a named one", func(m rl) { m["authors"] = []any{rl{"name": ""}, rl{"name": "A"}} }, "PASS 1/1"},
		{"a named author before a blank one", func(m rl) { m["authors"] = []any{rl{"name": "A"}, rl{"name": " "}} }, "PASS 1/1"},
		{"a NOASSERTION author and metadata.manufacturer.name", func(m rl) {
			m["authors"] = []any{rl{"name": "NOASSERTION"}}
			m["manufacturer"] = rl{"name": "M"}
		}, "PASS 1/1"},
		{"an author and a blank metadata.manufacturer.name", func(m rl) { m["manufacturer"] = rl{"name": ""} }, "PASS 1/1"},
	}
	for _, c := range cdx {
		r := rlScore(t, rlCDX([]rl{rlGood("a")}, func(d rl) { c.mut(rlMeta(d)) }), Options{})
		rlWant(t, c.cell, r, rlAuthor, c.want)
	}
	for creator, want := range map[string]string{"Person: P": "PASS 1/1", "Organization: O": "PASS 1/1", "Tool: t-1": "FAIL 0/1"} {
		r := rlScore(t, rlSPDX("2.3", rlAppDependsOnA, func(d rl, _ []rl) { d["creationInfo"].(rl)["creators"] = []any{creator} }), Options{})
		rlWant(t, "SPDX creator "+creator, r, rlAuthor, want)
	}
	for _, creators := range [][]any{{"Tool: t-1", "Person: P"}, {"Person: P", "Tool: t-1"}} {
		r := rlScore(t, rlSPDX("2.3", rlAppDependsOnA, func(d rl, _ []rl) { d["creationInfo"].(rl)["creators"] = creators }), Options{})
		rlWant(t, fmt.Sprintf("SPDX creators %v", creators), r, rlAuthor, "PASS 1/1")
	}
}

// fda.baseline.timestamp and the date of fda.component.end-of-support: every example the list
// prints, and the blanks around a value.
func TestFDARuleDateForms(t *testing.T) {
	status := func(pass bool) string {
		if pass {
			return "PASS 1/1"
		}
		return "FAIL 0/1"
	}
	stamps := append([]form{{" 2026-10-01T12:00:00Z ", true}, {"", false}}, fdaTimestampForms...)
	for _, f := range stamps {
		r := rlScore(t, rlCDX([]rl{rlGood("a")}, func(d rl) { rlMeta(d)["timestamp"] = f.value }), Options{})
		rlWant(t, "CycloneDX timestamp "+f.value, r, rlTime, status(f.pass))
		s := rlScore(t, rlSPDX("2.3", rlAppDependsOnA, func(d rl, _ []rl) { d["creationInfo"].(rl)["created"] = f.value }), Options{})
		rlWant(t, "SPDX created "+f.value, s, rlTime, status(f.pass))
	}
	dates := append([]form{{" 2030-01-02 ", true}, {"", false}}, fdaEndOfSupportForms...)
	for _, f := range dates {
		r := rlScore(t, rlCDX([]rl{rlBare("a", rlProp(propSupportLevel, "abandoned"), rlProp(propEndOfSupport, f.value))}, nil), Options{})
		rlWant(t, "CycloneDX end of support "+f.value, r, rlEOS, status(f.pass))
		s := rlScore(t, rlSPDX("2.3", rlAppDependsOnA, func(_ rl, pkgs []rl) {
			pkgs[1]["validUntilDate"] = f.value
			pkgs[2]["primaryPackagePurpose"] = "FILE"
		}), Options{})
		rlWant(t, "SPDX validUntilDate "+f.value, s, rlEOS, status(f.pass))
	}
	// The tables hold both outcomes, and the forms that tell the layouts apart.
	for name, forms := range map[string][]form{"timestamp": fdaTimestampForms, "end of support": fdaEndOfSupportForms} {
		pass, fail := 0, 0
		for _, f := range forms {
			if f.pass {
				pass++
			} else {
				fail++
			}
		}
		if pass == 0 || fail == 0 {
			t.Errorf("%s forms: %d accepted, %d refused, want both", name, pass, fail)
		}
	}
}

// fda.baseline.supplier-name: each carrier alone, and one named carrier beside unusable ones.
func TestFDARuleSupplierName(t *testing.T) {
	carriers := map[string]any{"manufacturer": rl{"name": "N"}, "supplier": rl{"name": "N"}, "authors": []any{rl{"name": "N"}}, "author": "N", "publisher": "N"}
	for k, v := range carriers {
		c := rlGood("a")
		delete(c, "supplier")
		c[k] = v
		want := "PASS 1/1"
		if k == "publisher" {
			want = "FAIL 0/1"
		}
		rlWant(t, "CycloneDX "+k+" alone", rlScore(t, rlCDX([]rl{c}, nil), Options{}), rlSupplier, want)
	}
	for k, want := range map[string]string{"supplier": "PASS 2/2", "originator": "PASS 2/2", "": "FAIL 1/2"} {
		s := rlScore(t, rlSPDX("2.3", rlAppDependsOnA, func(_ rl, pkgs []rl) {
			delete(pkgs[1], "supplier")
			if k != "" {
				pkgs[1][k] = "Organization: N"
			}
		}), Options{})
		rlWant(t, "SPDX "+k+" alone", s, rlSupplier, want)
	}
	// Any of the places, in any order: a reader of the first or the last place only fails one of these.
	beside := []struct {
		cell string
		set  map[string]any
	}{
		{"supplier.name NOASSERTION, author named", map[string]any{"supplier": rl{"name": "NOASSERTION"}, "author": "Jane"}},
		{"manufacturer.name blank, supplier.name named", map[string]any{"manufacturer": rl{"name": ""}, "supplier": rl{"name": "N"}}},
		{"a blank authors[].name before a named one", map[string]any{"authors": []any{rl{"name": ""}, rl{"name": "N"}}}},
		{"a named authors[].name before a blank one", map[string]any{"authors": []any{rl{"name": "N"}, rl{"name": ""}}}},
	}
	for _, b := range beside {
		c := rlGood("a")
		delete(c, "supplier")
		for k, v := range b.set {
			c[k] = v
		}
		rlWant(t, "CycloneDX "+b.cell, rlScore(t, rlCDX([]rl{c}, nil), Options{}), rlSupplier, "PASS 1/1")
	}
	for cell, set := range map[string][2]string{
		"supplier NOASSERTION, originator named": {"NOASSERTION", "Organization: O"},
		"supplier named, originator NOASSERTION": {"Organization: N", "NOASSERTION"},
	} {
		s := rlScore(t, rlSPDX("2.3", rlAppDependsOnA, func(_ rl, pkgs []rl) {
			pkgs[1]["supplier"] = set[0]
			pkgs[1]["originator"] = set[1]
		}), Options{})
		rlWant(t, "SPDX "+cell, s, rlSupplier, "PASS 2/2")
	}
}

// fda.baseline.component-name and fda.baseline.version-string.
func TestFDARuleNameAndVersion(t *testing.T) {
	for key, id := range map[string]string{"name": rlName, "version": rlVersion} {
		c := rlGood("a")
		delete(c, key)
		r := rlScore(t, rlCDX([]rl{c, rlGood("b")}, nil), Options{})
		rlWant(t, "CycloneDX without "+key, r, id, "FAIL 1/2")
	}
	for key, id := range map[string]string{"name": rlName, "versionInfo": rlVersion} {
		s := rlScore(t, rlSPDX("2.3", rlAppDependsOnA, func(_ rl, pkgs []rl) { pkgs[1][key] = "" }), Options{})
		rlWant(t, "SPDX without "+key, s, id, "FAIL 1/2")
	}
}

// fda.baseline.component-hash, and "Reason codes, each valid on either property".
func TestFDARuleComponentHash(t *testing.T) {
	cells := []struct {
		cell   string
		hashes any
		props  []rl
		want   string
		just   int
	}{
		{"algorithm and value", []any{rl{"alg": "SHA-256", "content": rlSHA}}, nil, "PASS 1/1", 0},
		{"no hash", nil, nil, "FAIL 0/1", 0},
		{"a value that is blank", []any{rl{"alg": "SHA-256", "content": " "}}, nil, "FAIL 0/1", 0},
		{"an unusable entry before a whole one", []any{rl{"alg": "SHA-256", "content": "NOASSERTION"}, rl{"alg": "MD5", "content": rlSHA}}, nil, "PASS 1/1", 0},
		{"a whole entry before an unusable one", []any{rl{"alg": "MD5", "content": rlSHA}, rl{"alg": "SHA-256", "content": ""}}, nil, "PASS 1/1", 0},
		{"the algorithm in one entry, the value in another", []any{rl{"alg": "SHA-256", "content": ""}, rl{"content": rlSHA}}, nil, "FAIL 0/1", 0},
		{"no hash, justified", nil, []rl{rlProp(propJustifiedHash, "DISTRIBUTION_PUBLISHES_NO_HASH")}, "PASS 0/1", 1},
		{"no hash, justified with a code named for the date", nil, []rl{rlProp(propJustifiedHash, "NO_COMMITTED_END_OF_SUPPORT")}, "PASS 0/1", 1},
		{"no hash, a bad code beside a good one", nil, []rl{rlProp(propJustifiedHash, "BECAUSE"), rlProp(propJustifiedHash, "OTHER")}, "PASS 0/1", 1},
		{"no hash, a good code before a bad one", nil, []rl{rlProp(propJustifiedHash, "OTHER"), rlProp(propJustifiedHash, "BECAUSE")}, "PASS 0/1", 1},
		{"no hash, an unlisted code", nil, []rl{rlProp(propJustifiedHash, "BECAUSE")}, "FAIL 0/1", 0},
		{"no hash, the other property's justification", nil, []rl{rlProp(propJustifiedEndOfSupport, "OTHER")}, "FAIL 0/1", 0},
	}
	for _, c := range cells {
		comp := rlGood("a")
		delete(comp, "hashes")
		if c.hashes != nil {
			comp["hashes"] = c.hashes
		}
		comp["properties"] = append(comp["properties"].([]rl), c.props...)
		r := rlScore(t, rlCDX([]rl{comp}, nil), Options{})
		rlWant(t, c.cell, r, rlHash, c.want)
		if got := r.checks[rlHash].Justified; got != c.just {
			t.Errorf("%s: justified %d, want %d", c.cell, got, c.just)
		}
	}
	s := rlScore(t, rlSPDX("2.3", rlAppDependsOnA, func(_ rl, pkgs []rl) { delete(pkgs[1], "checksums") }), Options{})
	rlWant(t, "SPDX package without checksums", s, rlHash, "FAIL 1/2")
	unusable := rl{"algorithm": "SHA256", "checksumValue": "NOASSERTION"}
	whole := rl{"algorithm": "SHA1", "checksumValue": "a94a8fe5ccb19ba61c4c0873d391e987982fbbd3"}
	for cell, sums := range map[string][]any{"an unusable checksum before a whole one": {unusable, whole}, "a whole checksum before an unusable one": {whole, unusable}} {
		s := rlScore(t, rlSPDX("2.3", rlAppDependsOnA, func(_ rl, pkgs []rl) { pkgs[1]["checksums"] = sums }), Options{})
		rlWant(t, "SPDX "+cell, s, rlHash, "PASS 2/2")
	}
}

// fda.baseline.unique-identifier: each carrier alone.
func TestFDARuleUniqueIdentifier(t *testing.T) {
	carriers := map[string]any{"purl": "x", "cpe": "x", "swid": rl{"tagId": "t", "name": "n"}, "swhid": []any{"x"}, "omniborId": []any{"x"}, "": nil}
	for k, v := range carriers {
		c := rlGood("a")
		delete(c, "purl")
		want := "FAIL 0/1"
		if k != "" {
			c[k] = v
			want = "PASS 1/1"
		}
		rlWant(t, "CycloneDX "+k+" alone", rlScore(t, rlCDX([]rl{c}, nil), Options{}), rlIdent, want)
	}
	types := map[string]string{"purl": "PASS 2/2", "cpe22Type": "PASS 2/2", "cpe23Type": "PASS 2/2", "swh": "PASS 2/2", "gitoid": "PASS 2/2", "maven-central": "FAIL 1/2"}
	for ty, want := range types {
		s := rlScore(t, rlSPDX("2.3", rlAppDependsOnA, func(_ rl, pkgs []rl) {
			pkgs[1]["externalRefs"] = []any{rl{"referenceCategory": "OTHER", "referenceType": ty, "referenceLocator": "x"}}
		}), Options{})
		rlWant(t, "SPDX external reference "+ty+" alone", s, rlIdent, want)
	}
	// One identifier beside unusable entries of the same array, in either order.
	for _, k := range []string{"swhid", "omniborId"} {
		for _, list := range [][]any{{"", "x"}, {"x", " "}} {
			c := rlGood("a")
			delete(c, "purl")
			c[k] = list
			rlWant(t, fmt.Sprintf("CycloneDX %s %q", k, list), rlScore(t, rlCDX([]rl{c}, nil), Options{}), rlIdent, "PASS 1/1")
		}
	}
	other := rl{"referenceCategory": "OTHER", "referenceType": "maven-central", "referenceLocator": "x"}
	purl := rl{"referenceCategory": "PACKAGE-MANAGER", "referenceType": "purl", "referenceLocator": "pkg:npm/a@1.0"}
	for cell, refs := range map[string][]any{"another type before purl": {other, purl}, "purl before another type": {purl, other}} {
		s := rlScore(t, rlSPDX("2.3", rlAppDependsOnA, func(_ rl, pkgs []rl) { pkgs[1]["externalRefs"] = refs }), Options{})
		rlWant(t, "SPDX "+cell, s, rlIdent, "PASS 2/2")
	}
}

// fda.baseline.relationship, CycloneDX.
func TestFDARuleRelationshipCycloneDX(t *testing.T) {
	silent := "described component declares no direct dependencies: root"
	cells := []struct {
		cell string
		mut  func(d rl)
		want string
		note string
	}{
		{"a complete composition lists the bom-ref in dependencies[]", nil, "PASS 1/1", ""},
		{"a dependencies[] entry of the bom-ref with a dependsOn", func(d rl) {
			delete(d, "compositions")
			d["dependencies"] = []any{rl{"ref": "root", "dependsOn": []any{"a"}}}
		}, "PASS 1/1", ""},
		{"neither", func(d rl) { delete(d, "compositions") }, "FAIL 0/1", silent},
		{"an empty dependsOn", func(d rl) {
			delete(d, "compositions")
			d["dependencies"] = []any{rl{"ref": "root", "dependsOn": []any{}}}
		}, "FAIL 0/1", silent},
		{"only another component declares", func(d rl) {
			delete(d, "compositions")
			d["dependencies"] = []any{rl{"ref": "a", "dependsOn": []any{"root"}}}
		}, "FAIL 0/1", silent},
		{"a composition that is not complete", func(d rl) {
			d["compositions"] = []any{rl{"aggregate": "incomplete", "dependencies": []any{"root"}}}
		}, "FAIL 0/1", silent},
		{"a complete composition lists it in assemblies[] only", func(d rl) {
			d["compositions"] = []any{rl{"aggregate": "complete", "assemblies": []any{"root"}}}
		}, "FAIL 0/1", silent},
		{"a complete composition lists another component", func(d rl) {
			d["compositions"] = []any{rl{"aggregate": "complete", "dependencies": []any{"a"}}}
		}, "FAIL 0/1", silent},
		{"no metadata.component", func(d rl) { delete(rlMeta(d), "component") }, "FAIL 0/1", "no metadata.component"},
		{"metadata.component without a bom-ref", func(d rl) { delete(rlMeta(d)["component"].(rl), "bom-ref") }, "FAIL 0/1", "metadata.component has no bom-ref"},
		// Any entry, in any order: a reader of the first or the last entry only fails one of these.
		{"another component's entry before the bom-ref's", func(d rl) {
			delete(d, "compositions")
			d["dependencies"] = []any{rl{"ref": "a", "dependsOn": []any{"root"}}, rl{"ref": "root", "dependsOn": []any{"a"}}}
		}, "PASS 1/1", ""},
		{"the bom-ref's entry before another component's", func(d rl) {
			delete(d, "compositions")
			d["dependencies"] = []any{rl{"ref": "root", "dependsOn": []any{"a"}}, rl{"ref": "a"}}
		}, "PASS 1/1", ""},
		{"a composition of another component before the bom-ref's", func(d rl) {
			d["compositions"] = []any{rl{"aggregate": "complete", "dependencies": []any{"a"}}, rl{"aggregate": "complete", "dependencies": []any{"root"}}}
		}, "PASS 1/1", ""},
		{"the bom-ref's composition before another", func(d rl) {
			d["compositions"] = []any{rl{"aggregate": "complete", "dependencies": []any{"root"}}, rl{"aggregate": "incomplete", "dependencies": []any{"a"}}}
		}, "PASS 1/1", ""},
		{"a complete composition lists another component, then the bom-ref", func(d rl) {
			d["compositions"] = []any{rl{"aggregate": "complete", "dependencies": []any{"a", "root"}}}
		}, "PASS 1/1", ""},
		{"a complete composition lists the bom-ref, then another component", func(d rl) {
			d["compositions"] = []any{rl{"aggregate": "complete", "dependencies": []any{"root", "a"}}}
		}, "PASS 1/1", ""},
	}
	for _, c := range cells {
		r := rlScore(t, rlCDX([]rl{rlGood("a")}, c.mut), Options{})
		rlWantNote(t, c.cell, r, rlRel, c.want, c.note)
	}
}

// fda.baseline.relationship, SPDX.
func TestFDARuleRelationshipSPDX(t *testing.T) {
	describes := rlRelation("SPDXRef-DOCUMENT", "DESCRIBES", "SPDXRef-app")
	file := rl{"SPDXID": "SPDXRef-file1", "fileName": "./f", "checksums": []any{rl{"algorithm": "SHA1", "checksumValue": "a94a8fe5ccb19ba61c4c0873d391e987982fbbd3"}}}
	cells := []struct {
		cell string
		rels []rl // every relationship of the document
		want string
	}{
		{"DEPENDS_ON a package", []rl{describes, rlRelation("SPDXRef-app", "DEPENDS_ON", "SPDXRef-a")}, "PASS 1/1"},
		{"CONTAINS a package", []rl{describes, rlRelation("SPDXRef-app", "CONTAINS", "SPDXRef-a")}, "PASS 1/1"},
		{"DEPENDS_ON NONE", []rl{describes, rlRelation("SPDXRef-app", "DEPENDS_ON", "NONE")}, "PASS 1/1"},
		{"CONTAINS NONE", []rl{describes, rlRelation("SPDXRef-app", "CONTAINS", "NONE")}, "PASS 1/1"},
		{"a package DEPENDENCY_OF it", []rl{describes, rlRelation("SPDXRef-a", "DEPENDENCY_OF", "SPDXRef-app")}, "PASS 1/1"},
		{"a package CONTAINED_BY it", []rl{describes, rlRelation("SPDXRef-a", "CONTAINED_BY", "SPDXRef-app")}, "PASS 1/1"},
		{"NONE DEPENDENCY_OF it", []rl{describes, rlRelation("NONE", "DEPENDENCY_OF", "SPDXRef-app")}, "PASS 1/1"},
		{"NONE CONTAINED_BY it", []rl{describes, rlRelation("NONE", "CONTAINED_BY", "SPDXRef-app")}, "PASS 1/1"},
		{"DESCRIBED_BY the other way describes", []rl{rlRelation("SPDXRef-app", "DESCRIBED_BY", "SPDXRef-DOCUMENT"), rlRelation("SPDXRef-app", "DEPENDS_ON", "SPDXRef-a")}, "PASS 1/1"},
		{"nothing declared", []rl{describes}, "FAIL 0/1"},
		{"the mirror the wrong way round", []rl{describes, rlRelation("SPDXRef-app", "DEPENDENCY_OF", "SPDXRef-a")}, "FAIL 0/1"},
		{"an end of NOASSERTION", []rl{describes, rlRelation("SPDXRef-app", "DEPENDS_ON", "NOASSERTION")}, "FAIL 0/1"},
		{"an end in another document", []rl{describes, rlRelation("SPDXRef-app", "DEPENDS_ON", "DocumentRef-x:SPDXRef-y")}, "FAIL 0/1"},
		{"an end of an unknown id", []rl{describes, rlRelation("SPDXRef-app", "DEPENDS_ON", "SPDXRef-nobody")}, "FAIL 0/1"},
		{"an end that is a file", []rl{describes, rlRelation("SPDXRef-app", "CONTAINS", "SPDXRef-file1")}, "FAIL 0/1"},
		{"only another package declares", []rl{describes, rlRelation("SPDXRef-a", "DEPENDS_ON", "SPDXRef-b")}, "FAIL 0/1"},
		{"another relationship type", []rl{describes, rlRelation("SPDXRef-app", "DYNAMIC_LINK", "SPDXRef-a")}, "FAIL 0/1"},
		{"two described packages, one silent", []rl{describes, rlRelation("SPDXRef-DOCUMENT", "DESCRIBES", "SPDXRef-a"), rlRelation("SPDXRef-app", "DEPENDS_ON", "SPDXRef-b")}, "FAIL 0/1"},
		{"two described packages, the first silent", []rl{describes, rlRelation("SPDXRef-DOCUMENT", "DESCRIBES", "SPDXRef-a"), rlRelation("SPDXRef-a", "DEPENDS_ON", "SPDXRef-b")}, "FAIL 0/1"},
		{"two described packages, both declare", []rl{describes, rlRelation("SPDXRef-DOCUMENT", "DESCRIBES", "SPDXRef-a"), rlRelation("SPDXRef-app", "DEPENDS_ON", "SPDXRef-b"), rlRelation("SPDXRef-a", "DEPENDS_ON", "NONE")}, "PASS 1/1"},
	}
	for _, c := range cells {
		s := rlScore(t, rlSPDX("2.3", nil, func(d rl, _ []rl) {
			d["relationships"] = c.rels
			d["files"] = []any{file}
		}), Options{})
		rlWant(t, c.cell, s, rlRel, c.want)
	}
	for cell, rels := range map[string][]rl{
		"no DESCRIBES":             {rlRelation("SPDXRef-app", "DEPENDS_ON", "SPDXRef-a")},
		"DESCRIBES a file":         {rlRelation("SPDXRef-DOCUMENT", "DESCRIBES", "SPDXRef-file1"), rlRelation("SPDXRef-app", "DEPENDS_ON", "SPDXRef-a")},
		"DESCRIBES from a package": {rlRelation("SPDXRef-a", "DESCRIBES", "SPDXRef-app"), rlRelation("SPDXRef-app", "DEPENDS_ON", "SPDXRef-a")},
	} {
		s := rlScore(t, rlSPDX("2.3", nil, func(d rl, _ []rl) {
			d["relationships"] = rels
			d["files"] = []any{file}
		}), Options{})
		rlWantNote(t, cell, s, rlRel, "FAIL 0/1", "no DESCRIBES relationship")
	}
}

// fda.component.support-level.
func TestFDARuleSupportLevel(t *testing.T) {
	cells := []struct {
		cell  string
		props []rl
		want  string
	}{
		{"abandoned", []rl{rlProp(propSupportLevel, "abandoned")}, "PASS 1/1"},
		{"actively maintained", []rl{rlProp(propSupportLevel, "actively maintained")}, "PASS 1/1"},
		{"no longer maintained", []rl{rlProp(propSupportLevel, "no longer maintained")}, "PASS 1/1"},
		{"no property", nil, "FAIL 0/1"},
		{"an unlisted value", []rl{rlProp(propSupportLevel, "unknown")}, "FAIL 0/1"},
		{"an unlisted value before a listed one", []rl{rlProp(propSupportLevel, "bogus"), rlProp(propSupportLevel, "abandoned")}, "PASS 1/1"},
		{"a listed value before an unlisted one", []rl{rlProp(propSupportLevel, "abandoned"), rlProp(propSupportLevel, "bogus")}, "PASS 1/1"},
	}
	for _, c := range cells {
		props := append([]rl{rlProp(propEndOfSupport, "2030-01-01")}, c.props...)
		r := rlScore(t, rlCDX([]rl{rlBare("a", props...)}, nil), Options{})
		rlWant(t, c.cell, r, rlLevel, c.want)
	}
	for _, v := range []string{"2.1", "2.2", "2.3"} {
		s := rlScore(t, rlSPDX(v, rlAppDependsOnA, func(_ rl, pkgs []rl) {
			pkgs[1]["primaryPackagePurpose"] = "FILE"
		}), Options{})
		rlWantNote(t, "SPDX "+v+" with a FILE package", s, rlLevel, "FAIL 0/2", "not representable in SPDX "+v)
		if n := s.checks[rlLevel].ComponentsSkipped; n != 0 {
			t.Errorf("SPDX %s: %d components skipped, want 0", v, n)
		}
	}
}

// fda.component.end-of-support; the forms of the date are in TestFDARuleDateForms.
func TestFDARuleEndOfSupport(t *testing.T) {
	cells := []struct {
		cell  string
		props []rl
		want  string
		just  int
	}{
		{"a date", []rl{rlProp(propEndOfSupport, "2030-01-02")}, "PASS 1/1", 0},
		{"no property", nil, "FAIL 0/1", 0},
		{"not a date", []rl{rlProp(propEndOfSupport, "soon")}, "FAIL 0/1", 0},
		{"not a date before a date", []rl{rlProp(propEndOfSupport, "soon"), rlProp(propEndOfSupport, "2030-01-02")}, "PASS 1/1", 0},
		{"a date before not a date", []rl{rlProp(propEndOfSupport, "2030-01-02"), rlProp(propEndOfSupport, "soon")}, "PASS 1/1", 0},
		{"justified", []rl{rlProp(propJustifiedEndOfSupport, "NO_COMMITTED_END_OF_SUPPORT")}, "PASS 0/1", 1},
		{"justified with a code named for the hash", []rl{rlProp(propJustifiedEndOfSupport, "VENDORED_SOURCE_NO_HASH")}, "PASS 0/1", 1},
		{"a bad code before a good one", []rl{rlProp(propJustifiedEndOfSupport, "BECAUSE"), rlProp(propJustifiedEndOfSupport, "OTHER")}, "PASS 0/1", 1},
		{"a good code before a bad one", []rl{rlProp(propJustifiedEndOfSupport, "OTHER"), rlProp(propJustifiedEndOfSupport, "BECAUSE")}, "PASS 0/1", 1},
		{"an unlisted code", []rl{rlProp(propJustifiedEndOfSupport, "BECAUSE")}, "FAIL 0/1", 0},
		{"the other property's justification", []rl{rlProp(propJustifiedHash, "OTHER")}, "FAIL 0/1", 0},
	}
	for _, c := range cells {
		props := append([]rl{rlProp(propSupportLevel, "abandoned")}, c.props...)
		r := rlScore(t, rlCDX([]rl{rlBare("a", props...)}, nil), Options{})
		rlWant(t, c.cell, r, rlEOS, c.want)
		if got := r.checks[rlEOS].Justified; got != c.just {
			t.Errorf("%s: justified %d, want %d", c.cell, got, c.just)
		}
	}
	s := rlScore(t, rlSPDX("2.3", rlAppDependsOnA, func(_ rl, pkgs []rl) { delete(pkgs[1], "validUntilDate") }), Options{})
	rlWant(t, "SPDX 2.3, one package without validUntilDate", s, rlEOS, "FAIL 1/2")
	for _, v := range []string{"2.1", "2.2"} {
		s := rlScore(t, rlSPDX(v, rlAppDependsOnA, func(_ rl, pkgs []rl) { pkgs[1]["primaryPackagePurpose"] = "FILE" }), Options{})
		rlWantNote(t, "SPDX "+v+" with a FILE package", s, rlEOS, "FAIL 0/2", "not representable in SPDX "+v)
		if n := s.checks[rlEOS].ComponentsSkipped; n != 0 {
			t.Errorf("SPDX %s: %d components skipped, want 0", v, n)
		}
	}
}

// fda.document.vulnerabilities.
func TestFDARuleVulnerabilities(t *testing.T) {
	for cell, data := range map[string][]byte{
		"a document that is READY":        rlCDX([]rl{rlGood("a")}, nil),
		"a document with vulnerabilities": rlCDX([]rl{rlGood("a")}, func(d rl) { d["vulnerabilities"] = []any{rl{"id": "CVE-2026-0001"}} }),
		"an SPDX document":                rlSPDX("2.3", rlAppDependsOnA, nil),
	} {
		rlWant(t, cell, rlScore(t, data, Options{}), rlVuln, "NOT_ASSESSED 0/0")
	}
}

// "With no component to judge it fails, at 0/0".
func TestFDARuleNothingToJudge(t *testing.T) {
	component := []string{rlSupplier, rlName, rlVersion, rlHash, rlIdent, rlLevel, rlEOS}
	r := rlScore(t, rlCDX([]rl{}, nil), Options{})
	for _, id := range component {
		rlWant(t, "CycloneDX without components", r, id, "FAIL 0/0")
	}
	rlWant(t, "CycloneDX without components", r, rlRel, "PASS 1/1")
	if r.verdict != VerdictNotReady {
		t.Errorf("CycloneDX without components: verdict %s, want NOT_READY", r.verdict)
	}
	s := rlScore(t, rlSPDX("2.3", nil, func(d rl, pkgs []rl) { d["packages"] = pkgs[:1] }), Options{})
	for _, id := range component {
		rlWant(t, "SPDX with the described package only", s, id, "FAIL 0/0")
	}

	f := rlBare("f")
	f["type"] = "file"
	self := rlBare("s", rlProp(propSelfComponent, "true"))
	r = rlScore(t, rlCDX([]rl{f, self}, nil), Options{})
	for _, id := range []string{rlLevel, rlEOS} {
		rlWantNote(t, "only components the support checks do not judge", r, id, "FAIL 0/0", "no supportable component")
	}
	rlWant(t, "only components the support checks do not judge", r, rlHash, "PASS 2/2")
}

// "Level REQUIRED ... Level INFO": the verdict.
func TestFDARuleVerdict(t *testing.T) {
	req := func(s Status) CheckResult { return CheckResult{Level: LevelRequired, Status: s} }
	info := func(s Status) CheckResult { return CheckResult{Level: LevelInfo, Status: s} }
	cells := []struct {
		cell    string
		results []CheckResult
		want    Verdict
	}{
		{"all pass", []CheckResult{req(StatusPass), req(StatusPass)}, VerdictReady},
		{"one fails", []CheckResult{req(StatusPass), req(StatusFail)}, VerdictNotReady},
		{"one in error", []CheckResult{req(StatusPass), req(StatusError)}, VerdictUnknown},
		{"a failing check after one in error", []CheckResult{req(StatusError), req(StatusFail)}, VerdictNotReady},
		{"a failing check before one in error", []CheckResult{req(StatusFail), req(StatusError)}, VerdictNotReady},
		{"INFO failing", []CheckResult{req(StatusPass), info(StatusFail)}, VerdictReady},
		{"INFO in error", []CheckResult{req(StatusPass), info(StatusError)}, VerdictReady},
		{"INFO not assessed", []CheckResult{req(StatusPass), info(StatusNotAssessed)}, VerdictReady},
	}
	for _, c := range cells {
		if got := verdictOf(c.results); got != c.want {
			t.Errorf("%s: verdict %s, want %s", c.cell, got, c.want)
		}
	}
}

// "A component is every component of the document except the described one", and --skip-files.
func TestFDARuleComponentSet(t *testing.T) {
	inner := rlGood("inner")
	outer := rlGood("outer")
	outer["components"] = []any{inner}
	nested := func(d rl) {
		rlMeta(d)["component"].(rl)["components"] = []any{rl{"type": "library", "bom-ref": "part"}}
	}
	r := rlScore(t, rlCDX([]rl{outer}, nested), Options{})
	rlWant(t, "CycloneDX: a nested component counts, metadata.component and its parts do not", r, rlName, "PASS 2/2")

	s := rlScore(t, rlSPDX("2.3", rlAppDependsOnA, nil), Options{})
	rlWant(t, "SPDX: three packages, one described", s, rlName, "PASS 2/2")

	file := rlGood("f")
	file["type"] = "file"
	delete(file, "version")
	for skip, want := range map[bool]string{false: "FAIL 1/2", true: "PASS 1/1"} {
		r := rlScore(t, rlCDX([]rl{file, rlGood("a")}, nil), Options{SkipFiles: skip})
		rlWant(t, fmt.Sprintf("CycloneDX file component, skip files %v", skip), r, rlVersion, want)
		if r.options.SkipFiles != skip {
			t.Errorf("options.skipFiles %v, want %v", r.options.SkipFiles, skip)
		}
		s := rlScore(t, rlSPDX("2.3", rlAppDependsOnA, func(_ rl, pkgs []rl) {
			pkgs[1]["primaryPackagePurpose"] = "FILE"
			pkgs[1]["versionInfo"] = ""
		}), Options{SkipFiles: skip})
		rlWant(t, fmt.Sprintf("SPDX FILE package, skip files %v", skip), s, rlVersion, want)
	}
}

// "counts as missing when, blanks around it ignored, it is empty or is exactly NOASSERTION or NONE".
func TestFDARuleUnknownValues(t *testing.T) {
	values := map[string]bool{"x": true, "noassertion": true, "None": true, "NOASSERTION": false, "NONE": false, " NOASSERTION ": false, " NONE ": false, "": false, "  ": false}
	fields := []struct {
		field string
		id    string
		set   func(c rl, v string)
	}{
		{"name", rlName, func(c rl, v string) { c["name"] = v }},
		{"version", rlVersion, func(c rl, v string) { c["version"] = v }},
		{"producer", rlSupplier, func(c rl, v string) { c["supplier"] = rl{"name": v} }},
		{"identifier", rlIdent, func(c rl, v string) { c["purl"] = v }},
		{"hash algorithm", rlHash, func(c rl, v string) { c["hashes"] = []any{rl{"alg": v, "content": rlSHA}} }},
		{"hash value", rlHash, func(c rl, v string) { c["hashes"] = []any{rl{"alg": "SHA-256", "content": v}} }},
	}
	for _, f := range fields {
		for v, present := range values {
			c := rlGood("a")
			f.set(c, v)
			want := "FAIL 0/1"
			if present {
				want = "PASS 1/1"
			}
			rlWant(t, fmt.Sprintf("%s %q", f.field, v), rlScore(t, rlCDX([]rl{c}, nil), Options{}), f.id, want)
		}
	}
	for v, present := range values {
		want := "FAIL 0/1"
		if present {
			want = "PASS 1/1"
		}
		r := rlScore(t, rlCDX([]rl{rlGood("a")}, func(d rl) { rlMeta(d)["authors"] = []any{rl{"name": v}} }), Options{})
		rlWant(t, fmt.Sprintf("author %q", v), r, rlAuthor, want)
	}
}

// "matched exactly: no trimming, no case folding".
func TestFDARuleExactMatching(t *testing.T) {
	judged := func(cell string, props ...rl) rlReport {
		return rlScore(t, rlCDX([]rl{rlBare("a", props...)}, nil), Options{})
	}
	level, date := rlProp(propSupportLevel, "abandoned"), rlProp(propEndOfSupport, "2030-01-01")
	for _, v := range []string{"Abandoned", " abandoned", "abandoned "} {
		rlWant(t, "level of support "+v, judged(v, rlProp(propSupportLevel, v), date), rlLevel, "FAIL 0/1")
	}
	for _, n := range []string{"Reliza:support:levelOfSupport", " reliza:support:levelOfSupport", "reliza:support:levelofsupport"} {
		rlWant(t, "property name "+n, judged(n, rlProp(n, "abandoned"), date), rlLevel, "FAIL 0/1")
	}
	for _, n := range []string{"CDX:lifecycle:milestone:endOfSupport", "cdx:lifecycle:milestone:endOfSupport ", "cdx:lifecycle:milestone:endofsupport"} {
		rlWant(t, "property name "+n, judged(n, level, rlProp(n, "2030-01-01")), rlEOS, "FAIL 0/1")
	}
	for _, v := range []string{"other", " OTHER", "OTHER "} {
		rlWant(t, "reason code "+v, judged(v, level, rlProp(propJustifiedEndOfSupport, v)), rlEOS, "FAIL 0/1")
	}
	for _, n := range []string{"Reliza:support:justifiedUnknown:endOfSupport", "reliza:support:justifiedUnknown:endOfSupport "} {
		rlWant(t, "property name "+n, judged(n, level, rlProp(n, "OTHER")), rlEOS, "FAIL 0/1")
	}
	for _, n := range []string{"Reliza:support:justifiedUnknown:hash", "reliza:support:justifiedUnknown:hash "} {
		c := rlGood("a")
		delete(c, "hashes")
		c["properties"] = append(c["properties"].([]rl), rlProp(n, "OTHER"))
		rlWant(t, "property name "+n, rlScore(t, rlCDX([]rl{c}, nil), Options{}), rlHash, "FAIL 0/1")
	}
	// The self marker and the types: a component that is not skipped is judged, and fails.
	for _, p := range []rl{rlProp(propSelfComponent, "TRUE"), rlProp(propSelfComponent, " true"), rlProp(propSelfComponent, "yes"),
		rlProp("Reliza:support:selfComponent", "true"), rlProp("reliza:support:selfComponent ", "true")} {
		r := rlScore(t, rlCDX([]rl{rlBare("a", p), rlGood("b")}, nil), Options{})
		rlWant(t, fmt.Sprintf("self marker %v", p), r, rlLevel, "FAIL 1/2")
	}
	for _, ty := range []string{"File", "DATA", "device "} {
		c := rlBare("a")
		c["type"] = ty
		r := rlScore(t, rlCDX([]rl{c, rlGood("b")}, nil), Options{})
		rlWant(t, "component type "+ty, r, rlLevel, "FAIL 1/2")
	}
	s := rlScore(t, rlSPDX("2.3", rlAppDependsOnA, func(_ rl, pkgs []rl) {
		pkgs[1]["primaryPackagePurpose"] = "File"
		delete(pkgs[1], "validUntilDate")
	}), Options{})
	rlWant(t, "SPDX primaryPackagePurpose File", s, rlEOS, "FAIL 1/2")
}

// "A component the two support checks do not judge neither passes nor fails".
func TestFDARuleSupportChecksJudge(t *testing.T) {
	for _, ty := range sortedKeys(skippedSupportTypes[FormatCycloneDX]) {
		c := rlBare("a")
		c["type"] = ty
		r := rlScore(t, rlCDX([]rl{c, rlGood("b")}, nil), Options{})
		for _, id := range []string{rlLevel, rlEOS} {
			rlWant(t, "CycloneDX type "+ty, r, id, "PASS 1/1")
		}
	}
	for _, ty := range []string{"library", "application", "firmware", "container", "operating-system", ""} {
		c := rlBare("a")
		c["type"] = ty
		if ty == "" {
			delete(c, "type")
		}
		r := rlScore(t, rlCDX([]rl{c, rlGood("b")}, nil), Options{})
		for _, id := range []string{rlLevel, rlEOS} {
			rlWant(t, "CycloneDX type "+ty+" is judged", r, id, "FAIL 1/2")
		}
	}
	r := rlScore(t, rlCDX([]rl{rlBare("a", rlProp(propSelfComponent, "true")), rlGood("b")}, nil), Options{})
	for _, id := range []string{rlLevel, rlEOS} {
		rlWant(t, "self component", r, id, "PASS 1/1")
	}
	rlWant(t, "self component, another check", r, rlHash, "PASS 2/2")
	for _, vs := range [][2]string{{"false", "true"}, {"true", "false"}} {
		c := rlBare("a", rlProp(propSelfComponent, vs[0]), rlProp(propSelfComponent, vs[1]))
		r := rlScore(t, rlCDX([]rl{c, rlGood("b")}, nil), Options{})
		for _, id := range []string{rlLevel, rlEOS} {
			rlWant(t, "self marker "+vs[0]+" then "+vs[1], r, id, "PASS 1/1")
		}
	}

	for _, ty := range sortedKeys(skippedSupportTypes[FormatSPDX]) {
		s := rlScore(t, rlSPDX("2.3", rlAppDependsOnA, func(_ rl, pkgs []rl) {
			pkgs[1]["primaryPackagePurpose"] = ty
			delete(pkgs[1], "validUntilDate")
		}), Options{})
		rlWant(t, "SPDX purpose "+ty, s, rlEOS, "PASS 1/1")
		rlWant(t, "SPDX purpose "+ty+", the level of support", s, rlLevel, "FAIL 0/2")
	}
	for _, ty := range []string{"LIBRARY", "APPLICATION", "FIRMWARE", ""} {
		s := rlScore(t, rlSPDX("2.3", rlAppDependsOnA, func(_ rl, pkgs []rl) {
			pkgs[1]["primaryPackagePurpose"] = ty
			if ty == "" {
				delete(pkgs[1], "primaryPackagePurpose")
			}
			delete(pkgs[1], "validUntilDate")
		}), Options{})
		rlWant(t, "SPDX purpose "+ty+" is judged", s, rlEOS, "FAIL 1/2")
	}
}

// "An SPDX 2.x document is always NOT_READY under this profile".
func TestFDARuleSPDXNotReady(t *testing.T) {
	for _, v := range []string{"2.1", "2.2", "2.3"} {
		s := rlScore(t, rlSPDX(v, rlAppDependsOnA, nil), Options{})
		if s.verdict != VerdictNotReady || s.checks[rlLevel].Status != StatusFail {
			t.Errorf("SPDX %s: verdict %s, %s %s, want NOT_READY and FAIL", v, s.verdict, rlLevel, s.checks[rlLevel].Status)
		}
		failing := 0
		for _, c := range s.checks {
			if c.Status == StatusFail {
				failing++
			}
		}
		if want := map[string]int{"2.1": 2, "2.2": 2, "2.3": 1}[v]; failing != want {
			t.Errorf("SPDX %s: %d failing checks, want %d (the support checks only)", v, failing, want)
		}
	}
}
