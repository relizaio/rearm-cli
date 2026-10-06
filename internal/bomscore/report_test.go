package bomscore

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the golden reports in testdata/golden")

// Test 8: score and verdict by hand.
func TestScoreWorkedExample(t *testing.T) {
	// 123 components, three keep the hash algorithm but have an empty content.
	data := manyComponentsCDX(t, 123, func(i int, c map[string]any) {
		if i < 3 {
			c["hashes"].([]any)[0].(map[string]any)["content"] = ""
		}
	})
	p := profileOf(t, scoreOK(t, data, ProfileCISA2026), ProfileCISA2026)
	if p.Score == nil || *p.Score != 99 || p.Verdict != VerdictNotReady {
		t.Errorf("score %v verdict %s, want 99 NOT_READY", p.Score, p.Verdict)
	}
	if p.Summary != (Summary{Pass: 16, Fail: 1, NotAssessed: 6}) {
		t.Errorf("summary %+v, want 16 pass, 1 fail, 6 not assessed", p.Summary)
	}

	// Two failing component checks at 1/4 and 3/4: (15 + 0.25 + 0.75) / 17 = 0.9412, score 94.
	data = manyComponentsCDX(t, 4, func(i int, c map[string]any) {
		if i > 0 {
			delete(c, "licenses")
		}
		if i == 0 {
			delete(c, "version")
		}
	})
	p = profileOf(t, scoreOK(t, data, ProfileCISA2026), ProfileCISA2026)
	if p.Score == nil || *p.Score != 94 {
		t.Errorf("score %v, want 94", p.Score)
	}

	// An INFO check added to a profile leaves the score unchanged.
	d := loadOK(t, data)
	base, _ := scoreProfile(profileCISA2026, d)
	extra := profileCISA2026
	extra.Checks = append(append([]Check(nil), profileCISA2026.Checks...), Check{ID: "cisa-2026.extra", Title: "Extra",
		Level: LevelInfo, Scope: ScopeDocument, Ref: "test", Eval: func(*Doc) Outcome { return docOutcome(false) }})
	more, _ := scoreProfile(extra, d)
	if *more.Score != *base.Score || more.Verdict != base.Verdict {
		t.Errorf("an INFO check moved the score %d -> %d or verdict %s -> %s", *base.Score, *more.Score, base.Verdict, more.Verdict)
	}
}

// Test 22, O2: the score is rounded down; one field missing in one component of 1000 gives 99.
func TestOperatorPoint_O2(t *testing.T) {
	data := manyComponentsCDX(t, 1000, func(i int, c map[string]any) {
		if i == 0 {
			delete(c, "licenses")
		}
	})
	p := profileOf(t, scoreOK(t, data, ProfileCISA2026), ProfileCISA2026)
	if p.Score == nil || *p.Score != 99 {
		t.Errorf("score %v, want 99 (rounded down, not to 100)", p.Score)
	}
	if s := scoreOf([]CheckResult{{Level: LevelInfo, Status: StatusPass}, {Level: LevelRequired, Status: StatusError}}); s != nil {
		t.Errorf("score %d with no REQUIRED PASS or FAIL, want nil", *s)
	}
}

// Test 22, O4: SPDX 2.x is NOT_READY under cisa-2026 and fda; the not-representable checks keep
// level REQUIRED.
func TestOperatorPoint_O4(t *testing.T) {
	r := scoreOK(t, readFixture(t, "full.spdx.json"))
	for _, key := range []ProfileKey{ProfileCISA2026, ProfileFDA} {
		if v := profileOf(t, r, key).Verdict; v != VerdictNotReady {
			t.Errorf("%s: %s, want NOT_READY", key, v)
		}
	}
	for _, id := range []string{"cisa-2026.sbom-author-signature", "fda.component.support-level"} {
		if c := checkOf(t, r, id); c.Level != LevelRequired || c.Status != StatusFail {
			t.Errorf("%s: %s %s, want REQUIRED FAIL", id, c.Level, c.Status)
		}
	}
}

// Test 14: determinism, golden reports, profile order and duplicates.
func TestDeterminismAndGolden(t *testing.T) {
	for _, fixture := range []string{"full.cdx.json", "full.spdx.json"} {
		data := readFixture(t, fixture)
		for _, key := range allProfiles {
			first := reportJSON(t, scoreOK(t, data, key))
			second := reportJSON(t, scoreOK(t, data, key))
			if !bytes.Equal(first, second) {
				t.Fatalf("%s %s: two runs differ", fixture, key)
			}
			golden := filepath.Join("testdata", "golden", string(key)+"."+strings.TrimPrefix(fixture, "full."))
			if *update {
				if err := os.WriteFile(golden, first, 0644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("%v (run go test ./internal/bomscore -run TestDeterminismAndGolden -update)", err)
			}
			if !bytes.Equal(first, want) {
				t.Errorf("%s differs from the report:\n%s", golden, first)
			}
		}
	}

	r := scoreOK(t, readFixture(t, "full.cdx.json"), ProfileFDA, ProfileCISA2026, ProfileFDA)
	if len(r.Profiles) != 2 || r.Profiles[0].Key != ProfileFDA || r.Profiles[1].Key != ProfileCISA2026 {
		t.Errorf("profiles %v, want fda then cisa-2026, once each", r.Profiles)
	}

	_, err := Score(readFixture(t, "full.cdx.json"), []string{"bsi"}, testEngineVersion)
	var unknown *UnknownProfileError
	if !errors.As(err, &unknown) || unknown.Key != "bsi" {
		t.Errorf("error %v, want an unknown profile error for bsi", err)
	}
}

func reportJSON(t *testing.T, r Report) []byte {
	t.Helper()
	b, err := r.JSON()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// The report's JSON shape: key order, empty arrays never null, score null, no HTML escaping, one
// trailing newline.
func TestReportJSONShape(t *testing.T) {
	r := scoreOK(t, readFixture(t, "full.spdx.json"), ProfileCISA2026)
	b := reportJSON(t, r)
	if !bytes.HasSuffix(b, []byte("}\n")) || bytes.HasSuffix(b, []byte("\n\n")) {
		t.Error("want exactly one trailing newline")
	}
	if !bytes.HasPrefix(b, []byte("{\n  \"reportVersion\": 1,\n  \"engine\": {\n    \"name\": \"rearm-cli\",\n    \"version\": \"test\"\n  },\n  \"input\": {")) {
		t.Errorf("unexpected head:\n%s", b[:200])
	}
	if bytes.Contains(b, []byte("null")) || !bytes.Contains(b, []byte(`"failing": []`)) || !bytes.Contains(b, []byte(`"checks": []`)) || !bytes.Contains(b, []byte(`"errors": []`)) {
		t.Error("want empty arrays as [] and no null")
	}
	if !bytes.Contains(b, []byte(`CISA et al., \"2026 Minimum Elements`)) {
		t.Error("source title missing")
	}
	var generic map[string]any
	if err := json.Unmarshal(b, &generic); err != nil {
		t.Fatal(err)
	}
	keys := []string{"reportVersion", "engine", "input", "profiles", "structure", "errors"}
	pos := -1
	for _, k := range keys {
		i := bytes.Index(b, []byte("\n  \""+k+"\":"))
		if i <= pos {
			t.Errorf("key %s out of order", k)
		}
		pos = i
	}

	none := Report{Profiles: []ProfileReport{{Key: "x", Checks: []CheckResult{}}}, Structure: StructureReport{Checks: []CheckResult{}}, Errors: []CheckError{}}
	if !bytes.Contains(reportJSON(t, none), []byte(`"score": null`)) {
		t.Error("want score null when nothing is counted")
	}
}

// HTML escaping is off (design 3.4): the report carries the characters of the input raw. Purls
// with more than one qualifier carry '&'; a name with '<' and '>' covers the other two.
func TestReportJSONNoHTMLEscaping(t *testing.T) {
	m := jsonFixture(t, "full.cdx.json")
	g := gammaCDX(m)
	g["purl"] = "pkg:apk/alpine/gamma@3.1.0?arch=x86_64&distro=alpine-3.20.5"
	delete(g, "licenses")
	alpha := m["components"].([]any)[0].(map[string]any)
	delete(alpha, "purl")
	delete(alpha, "licenses")
	alpha["name"] = "alpha<arm64>"
	r := scoreOK(t, encode(t, m), ProfileCISA2026)
	failing := checkOf(t, r, "cisa-2026.component-license").Failing
	wantFailing := []string{"alpha<arm64>@1.0.0", "pkg:apk/alpine/gamma@3.1.0?arch=x86_64&distro=alpine-3.20.5"}
	if !reflect.DeepEqual(failing, wantFailing) {
		t.Fatalf("failing %v, want %v", failing, wantFailing)
	}
	b := reportJSON(t, r)
	for _, raw := range []string{`"alpha<arm64>@1.0.0"`, `"pkg:apk/alpine/gamma@3.1.0?arch=x86_64&distro=alpine-3.20.5"`} {
		if !bytes.Contains(b, []byte(raw)) {
			t.Errorf("report does not carry %s raw", raw)
		}
	}
	for _, escaped := range []string{`\u0026`, `\u003c`, `\u003e`} {
		if bytes.Contains(b, []byte(escaped)) {
			t.Errorf("report carries %s: HTML escaping is on", escaped)
		}
	}
}

// The text shape of the command (design 3.5).
func TestReportText(t *testing.T) {
	data := manyComponentsCDX(t, 25, func(i int, c map[string]any) {
		if i < 22 {
			c["hashes"].([]any)[0].(map[string]any)["content"] = ""
		}
		if i == 0 {
			c["purl"] = "not a purl"
		}
	})
	text := scoreOK(t, data, ProfileCISA2026).Text()
	want := []string{
		"cisa-2026  CISA Minimum Elements for an SBOM (2026)  NOT_READY  score 94",
		"  FAIL  Component Hash Value  3/25  (Table 1, Component Hash Value)",
		"        missing in: not a purl, pkg:npm/c0001@1.0.0, pkg:npm/c0002@1.0.0, pkg:npm/c0003@1.0.0, pkg:npm/c0004@1.0.0, pkg:npm/c0005@1.0.0, pkg:npm/c0006@1.0.0, pkg:npm/c0007@1.0.0, pkg:npm/c0008@1.0.0, pkg:npm/c0009@1.0.0, pkg:npm/c0010@1.0.0, pkg:npm/c0011@1.0.0, pkg:npm/c0012@1.0.0, pkg:npm/c0013@1.0.0, pkg:npm/c0014@1.0.0, pkg:npm/c0015@1.0.0, pkg:npm/c0016@1.0.0, pkg:npm/c0017@1.0.0, pkg:npm/c0018@1.0.0, pkg:npm/c0019@1.0.0, ...",
		"  17 required: 16 pass, 1 fail, 0 error; 6 not assessed",
		"",
		"structure  BOM structure (informational, never changes a verdict)",
		"  FAIL  Package URLs parse  24/25  (purl specification)",
		"        missing in: not a purl",
	}
	if got := lines(text); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("text:\n%s\nwant:\n%s", text, strings.Join(want, "\n"))
	}

	spdx := scoreOK(t, readFixture(t, "full.spdx.json"), ProfileFDA).Text()
	for _, line := range []string{
		"fda  FDA Premarket SBOM (2026)  NOT_READY  score 90",
		"  FAIL  Software level of support  0/4  (V.A.4(b))",
		"        remedy: assess support for the component in ReARM and export with support metadata",
		"        note: not representable in SPDX 2.3",
		"  10 required: 9 pass, 1 fail, 0 error; 1 not assessed",
	} {
		if !strings.Contains(spdx, line+"\n") {
			t.Errorf("text has no line %q:\n%s", line, spdx)
		}
	}
	doc := scoreOK(t, readFixture(t, "full.spdx.json"), ProfileCISA2026).Text()
	if !strings.Contains(doc, "  FAIL  SBOM Version  (Table 1, SBOM Version)\n        note: not representable in SPDX 2.3\n") {
		t.Errorf("a DOCUMENT check prints no counts and its note:\n%s", doc)
	}
}

// Test 16: no new module; the engine reads with the two libraries go.mod already has.
func TestNoNewDependency(t *testing.T) {
	gomod, err := os.ReadFile(filepath.Join("..", "..", "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"github.com/CycloneDX/cyclonedx-go v0.11.0", "github.com/spdx/tools-golang v0.5.7", "github.com/package-url/packageurl-go v0.1.6"} {
		if !bytes.Contains(gomod, []byte(want)) {
			t.Errorf("go.mod does not require %s", want)
		}
	}
	if bytes.Contains(gomod, []byte("github.com/spdx/gordf")) {
		t.Error("go.mod requires gordf: SPDX RDF/XML is out of scope (design 3.2)")
	}
}
