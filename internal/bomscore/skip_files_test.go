package bomscore

import (
	"bytes"
	"strings"
	"testing"
)

// SCORE-10: Options.SkipFiles (rearm bomutils score --skip-files) leaves file components out of
// the component checks.

// twoFilesCDX is full.cdx.json with two type file components and no version, one of them nesting a
// complete library (epsilon, a copy of alpha).
func twoFilesCDX(t *testing.T) []byte {
	t.Helper()
	m := jsonFixture(t, "full.cdx.json")
	components := m["components"].([]any)
	epsilon := clone(t, components[0].(map[string]any))
	epsilon["bom-ref"] = "epsilon"
	epsilon["name"] = "epsilon"
	epsilon["purl"] = "pkg:npm/epsilon@1.0.0"
	m["components"] = append(components,
		map[string]any{"type": "file", "bom-ref": "file-readme", "name": "README.md"},
		map[string]any{"type": "file", "bom-ref": "file-app-bin", "name": "bin/app", "components": []any{epsilon}},
	)
	return encode(t, m)
}

func TestSkipFiles(t *testing.T) {
	d := loadOK(t, twoFilesCDX(t))
	if n := len(d.Components); n != 7 {
		t.Fatalf("component set %d, want 7 before skipping", n)
	}
	if n := d.skipFiles(); n != 2 {
		t.Errorf("skipFiles() = %d, want 2", n)
	}
	var names []string
	for _, c := range d.Components {
		names = append(names, c.Name)
	}
	if len(names) != 5 || !contains(names, "epsilon") || contains(names, "README.md") || contains(names, "bin/app") {
		t.Errorf("component set after skipping %v, want the four fixture components and epsilon", names)
	}
	for _, ref := range []string{"file-readme", "file-app-bin", "epsilon"} {
		if !contains(d.Refs, ref) {
			t.Errorf("Refs %v lost %s", d.Refs, ref)
		}
	}

	m := jsonFixture(t, "full.spdx.json")
	gammaSPDX(m)["primaryPackagePurpose"] = "FILE"
	spdxPackage(m, "SPDXRef-alpha")["primaryPackagePurpose"] = "LIBRARY"
	d = loadOK(t, encode(t, m))
	if n := d.skipFiles(); n != 1 || len(d.Components) != 3 {
		t.Errorf("SPDX 2.3 primaryPackagePurpose FILE: skipped %d, %d left, want 1 and 3", n, len(d.Components))
	}

	// The described package is not in the set, so it is never counted as skipped.
	m = jsonFixture(t, "full.spdx.json")
	spdxPackage(m, "SPDXRef-app")["primaryPackagePurpose"] = "FILE"
	if n := loadOK(t, encode(t, m)).skipFiles(); n != 0 {
		t.Errorf("described package with purpose FILE: skipped %d, want 0", n)
	}

	for _, f := range []string{"full-2.2.spdx.json", "full.cdx.json"} {
		if n := loadOK(t, readFixture(t, f)).skipFiles(); n != 0 {
			t.Errorf("%s: skipped %d, want 0", f, n)
		}
	}
}

func TestSkipFilesReport(t *testing.T) {
	data := twoFilesCDX(t)
	without, err := Score(data, keyStrings(allProfiles), testEngineVersion, Options{})
	if err != nil {
		t.Fatal(err)
	}
	with, err := Score(data, keyStrings(allProfiles), testEngineVersion, Options{SkipFiles: true})
	if err != nil {
		t.Fatal(err)
	}
	if without.Options.SkipFiles || without.Input.Components != 7 || without.Input.ComponentsSkipped != 0 {
		t.Errorf("without the option: %+v %+v, want skipFiles false, 7 scored, 0 skipped", without.Options, without.Input)
	}
	if !with.Options.SkipFiles || with.Input.Components != 5 || with.Input.ComponentsSkipped != 2 {
		t.Errorf("with the option: %+v %+v, want skipFiles true, 5 scored, 2 skipped", with.Options, with.Input)
	}

	totals := func(r Report) map[string]int {
		out := map[string]int{}
		for _, p := range r.Profiles {
			for _, c := range p.Checks {
				out[c.ID] = c.Total
			}
		}
		for _, c := range r.Structure.Checks {
			out[c.ID] = c.Total
		}
		return out
	}
	before, after := totals(without), totals(with)
	component := 0
	for _, p := range profiles {
		for _, c := range p.Checks {
			if c.Scope != ScopeComponent {
				continue
			}
			component++
			if after[c.ID] != before[c.ID]-2 {
				t.Errorf("%s: total %d -> %d, want a drop of 2", c.ID, before[c.ID], after[c.ID])
			}
		}
	}
	for _, c := range structureChecks {
		if c.Scope == ScopeComponent && after[c.ID] != before[c.ID]-2 {
			t.Errorf("%s: total %d -> %d, want a drop of 2", c.ID, before[c.ID], after[c.ID])
		}
	}
	if component == 0 {
		t.Fatal("no component checks compared")
	}

	assertStatus(t, without, "cisa-2026.component-version", StatusFail)
	assertStatus(t, with, "cisa-2026.component-version", StatusPass)
	if b, a := profileOf(t, without, ProfileCISA2026).Score, profileOf(t, with, ProfileCISA2026).Score; *a <= *b {
		t.Errorf("cisa-2026 score %d with the option, %d without; want it higher", *a, *b)
	}

	j := reportJSON(t, with)
	for _, part := range []string{"\n  \"options\": {\n    \"skipFiles\": true\n  },\n", "\"componentsSkipped\": 2,"} {
		if !bytes.Contains(j, []byte(part)) {
			t.Errorf("report JSON has no %q", part)
		}
	}

	if got := lines(with.Text())[0]; got != "options: --skip-files, 2 file components left out" {
		t.Errorf("first text line %q", got)
	}
	if text := without.Text(); strings.Contains(text, "options:") {
		t.Errorf("text without the option prints an options line:\n%s", text)
	}
}
