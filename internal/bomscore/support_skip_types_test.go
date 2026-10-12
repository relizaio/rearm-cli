package bomscore

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// SCORE-20 (parent SCORE-12 ADR-9): fda.component.support-level and fda.component.end-of-support
// skip components whose type is not a software package, and say per check how many and of which
// types. Every other check judges them as before.

const (
	supportLevelID = "fda.component.support-level"
	endOfSupportID = "fda.component.end-of-support"
)

// supportCheckIDs are the only checks that skip by type.
var supportCheckIDs = map[string]bool{supportLevelID: true, endOfSupportID: true}

// The CycloneDX types the support checks skip and keep; "" stands for a component without a type key.
var (
	skippedCDXTypes = []string{"file", "cryptographic-asset", "data", "machine-learning-model", "device"}
	keptCDXTypes    = []string{"application", "framework", "library", "container", "platform", "operating-system", "device-driver", "firmware", ""}
)

// typedName is the name (and display id: no version, no purl) of the added component of a type.
func typedName(kind, typ string) string {
	if typ == "" {
		return kind + "-untyped"
	}
	return kind + "-" + typ
}

// typesCDX is full.cdx.json plus one top-level component per skipped type (named skipped-<type>)
// and one per kept type (kept-<type>, kept-untyped without a type key): name and bom-ref only, so
// none has a version, a purl or a support property. 4 + 5 + 9 = 18 components.
func typesCDX(t *testing.T) map[string]any {
	t.Helper()
	m := jsonFixture(t, "full.cdx.json")
	components := m["components"].([]any)
	add := func(kind, typ string) {
		name := typedName(kind, typ)
		c := map[string]any{"bom-ref": name, "name": name}
		if typ != "" {
			c["type"] = typ
		}
		components = append(components, c)
	}
	for _, typ := range skippedCDXTypes {
		add("skipped", typ)
	}
	for _, typ := range keptCDXTypes {
		add("kept", typ)
	}
	m["components"] = components
	return m
}

// addedComponent finds a component typesCDX added.
func addedComponent(m map[string]any, name string) map[string]any {
	for _, e := range m["components"].([]any) {
		if c := e.(map[string]any); c["name"] == name {
			return c
		}
	}
	panic("no component " + name)
}

func assertSkipped(t *testing.T, c CheckResult, n int, types ...string) {
	t.Helper()
	if c.ComponentsSkipped != n || !reflect.DeepEqual(c.SkippedTypes, types) {
		t.Errorf("%s: componentsSkipped %d skippedTypes %#v, want %d %#v", c.ID, c.ComponentsSkipped, c.SkippedTypes, n, types)
	}
}

func assertNotSkipped(t *testing.T, c CheckResult) {
	t.Helper()
	if c.ComponentsSkipped != 0 || c.SkippedTypes != nil {
		t.Errorf("%s: componentsSkipped %d skippedTypes %#v, want 0 and nil", c.ID, c.ComponentsSkipped, c.SkippedTypes)
	}
}

// T-1: the CycloneDX skip and keep lists, by type only.
func TestSupportChecksSkipTypesCDX(t *testing.T) {
	r := scoreOK(t, encode(t, typesCDX(t)), ProfileFDA)
	if r.Input.Components != 18 {
		t.Errorf("input.components %d, want 18", r.Input.Components)
	}
	for _, id := range []string{supportLevelID, endOfSupportID} {
		c := assertStatus(t, r, id, StatusFail)
		if c.Total != 13 || c.Passed != 4 {
			t.Errorf("%s: %d/%d, want 4/13 (the 4 fixture components pass, the 9 kept ones fail)", id, c.Passed, c.Total)
		}
		assertSkipped(t, c, 5, "cryptographic-asset", "data", "device", "file", "machine-learning-model")
		for _, typ := range keptCDXTypes {
			if !contains(c.Failing, typedName("kept", typ)) {
				t.Errorf("%s: failing %v lacks %s", id, c.Failing, typedName("kept", typ))
			}
		}
		for _, typ := range skippedCDXTypes {
			if contains(c.Failing, typedName("skipped", typ)) {
				t.Errorf("%s: failing %v holds the skipped %s", id, c.Failing, typedName("skipped", typ))
			}
		}
	}
}

// spdxTypesDoc is full.spdx.json (SPDX 2.3) plus one package per purpose (named pkg-<purpose>,
// pkg-untyped without one), each a copy of alpha with its own SPDXID, name and purl and no
// validUntilDate except the LIBRARY one; none is described. 4 + 13 = 17 packages in the set.
func spdxTypesDoc(t *testing.T, fixture string) map[string]any {
	t.Helper()
	m := jsonFixture(t, fixture)
	alpha := spdxPackage(m, "SPDXRef-alpha")
	packages := m["packages"].([]any)
	for _, purpose := range []string{"FILE", "DEVICE", "APPLICATION", "FRAMEWORK", "LIBRARY", "CONTAINER", "OPERATING-SYSTEM", "FIRMWARE", "SOURCE", "ARCHIVE", "INSTALL", "OTHER", ""} {
		name := "pkg-untyped"
		if purpose != "" {
			name = "pkg-" + strings.ToLower(purpose)
		}
		p := clone(t, alpha)
		p["SPDXID"] = "SPDXRef-" + name
		p["name"] = name
		p["externalRefs"] = []any{map[string]any{"referenceCategory": "PACKAGE-MANAGER", "referenceType": "purl", "referenceLocator": "pkg:generic/" + name + "@1.0.0"}}
		delete(p, "validUntilDate")
		if purpose != "" {
			p["primaryPackagePurpose"] = purpose
		}
		if purpose == "LIBRARY" {
			p["validUntilDate"] = "2030-01-01T00:00:00Z"
		}
		packages = append(packages, p)
	}
	m["packages"] = packages
	return m
}

// T-2: SPDX 2.3 purposes; the skip is observable on end-of-support only, since support-level is
// not representable in SPDX and end-of-support not in SPDX 2.2.
func TestSupportChecksSkipTypesSPDX(t *testing.T) {
	r := scoreOK(t, encode(t, spdxTypesDoc(t, "full.spdx.json")), ProfileFDA)
	if r.Input.Components != 17 {
		t.Fatalf("input.components %d, want 17", r.Input.Components)
	}
	eos := assertStatus(t, r, endOfSupportID, StatusFail)
	assertSkipped(t, eos, 2, "DEVICE", "FILE")
	if eos.Total != 15 || eos.Passed != 5 {
		t.Errorf("%s: %d/%d, want 5/15 (4 fixture packages and the LIBRARY one pass)", endOfSupportID, eos.Passed, eos.Total)
	}
	for _, name := range []string{"application", "framework", "container", "operating-system", "firmware", "source", "archive", "install", "other", "untyped"} {
		if !contains(eos.Failing, "pkg:generic/pkg-"+name+"@1.0.0") {
			t.Errorf("%s: failing %v lacks the kept pkg-%s", endOfSupportID, eos.Failing, name)
		}
	}
	for _, name := range []string{"library", "file", "device"} {
		if contains(eos.Failing, "pkg:generic/pkg-"+name+"@1.0.0") {
			t.Errorf("%s: failing %v holds pkg-%s", endOfSupportID, eos.Failing, name)
		}
	}

	level := assertStatus(t, r, supportLevelID, StatusFail)
	if level.Note != "not representable in SPDX 2.3" || level.Total != 17 || level.Passed != 0 {
		t.Errorf("%s: note %q %d/%d, want the not-representable note and 0/17", supportLevelID, level.Note, level.Passed, level.Total)
	}
	assertNotSkipped(t, level)

	// SPDX 2.2 has no primaryPackagePurpose: nothing is typed, nothing is skipped.
	m := jsonFixture(t, "full-2.2.spdx.json")
	spdxPackage(m, "SPDXRef-gamma")["primaryPackagePurpose"] = "FILE"
	r22 := scoreOK(t, encode(t, m), ProfileFDA)
	for _, id := range []string{supportLevelID, endOfSupportID} {
		c := assertStatus(t, r22, id, StatusFail)
		if c.Note != "not representable in SPDX 2.2" || c.Total != 4 {
			t.Errorf("%s on SPDX 2.2: note %q total %d, want the not-representable note and 4", id, c.Note, c.Total)
		}
		assertNotSkipped(t, c)
	}
}

// T-3: only the two support checks skip; every other check and the loader count all components.
func TestOnlySupportChecksSkip(t *testing.T) {
	r := scoreOK(t, encode(t, typesCDX(t)))
	compared := 0
	for _, p := range r.Profiles {
		for _, c := range p.Checks {
			if c.Scope != ScopeComponent || supportCheckIDs[c.ID] {
				continue
			}
			compared++
			if c.Total != 18 {
				t.Errorf("%s: total %d, want 18", c.ID, c.Total)
			}
			assertNotSkipped(t, c)
		}
	}
	if compared == 0 {
		t.Fatal("no component check compared")
	}
	for _, c := range r.Structure.Checks {
		if c.Scope == ScopeComponent && c.Total != 18 {
			t.Errorf("%s: total %d, want 18", c.ID, c.Total)
		}
		assertNotSkipped(t, c)
	}
	if uid := checkOf(t, r, "fda.baseline.unique-identifier"); !contains(uid.Failing, "skipped-cryptographic-asset") {
		t.Errorf("fda.baseline.unique-identifier failing %v, want the cryptographic-asset without purl", uid.Failing)
	}
}

// T-4: a document whose every component is skipped fails both support checks at 0/0.
func TestSupportChecksAllSkipped(t *testing.T) {
	m := jsonFixture(t, "full.cdx.json")
	var retype func(cs []any)
	retype = func(cs []any) {
		for _, e := range cs {
			c := e.(map[string]any)
			c["type"] = "cryptographic-asset"
			if nested, ok := c["components"].([]any); ok {
				retype(nested)
			}
		}
	}
	retype(m["components"].([]any))
	r := scoreOK(t, encode(t, m), ProfileFDA)
	const note = "no supportable component" // the literal text of design 3.1, not the production constant
	for _, id := range []string{supportLevelID, endOfSupportID} {
		c := assertStatus(t, r, id, StatusFail)
		if c.Passed != 0 || c.Total != 0 || c.Note != note || len(c.Failing) != 0 {
			t.Errorf("%s: %d/%d note %q failing %v, want 0/0, note %q, none failing", id, c.Passed, c.Total, c.Note, c.Failing, note)
		}
		assertSkipped(t, c, 4, "cryptographic-asset")
	}
	if v := profileOf(t, r, ProfileFDA).Verdict; v != VerdictNotReady {
		t.Errorf("verdict %s, want NOT_READY", v)
	}
	for _, id := range []string{"fda.baseline.supplier-name", "fda.baseline.component-name", "fda.baseline.version-string",
		"fda.baseline.component-hash", "fda.baseline.unique-identifier"} {
		if c := checkOf(t, r, id); c.Total != 4 {
			t.Errorf("%s: total %d, want 4", id, c.Total)
		}
	}

	// Nothing skipped and nothing to judge: 0/0 as before SCORE-20, without the note or the fields.
	empty := jsonFixture(t, "full.cdx.json")
	empty["components"] = []any{}
	r = scoreOK(t, encode(t, empty), ProfileFDA)
	for _, id := range []string{supportLevelID, endOfSupportID} {
		c := assertStatus(t, r, id, StatusFail)
		if c.Total != 0 || c.Note != "" {
			t.Errorf("%s without components: total %d note %q, want 0 and no note", id, c.Total, c.Note)
		}
		assertNotSkipped(t, c)
	}
}

// filesAndKeyCDX is twoFilesCDX with other types plus one cryptographic-asset without a version.
func filesAndKeyCDX(t *testing.T) []byte {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(twoFilesCDX(t, true), &m); err != nil {
		t.Fatal(err)
	}
	m["components"] = append(m["components"].([]any),
		map[string]any{"type": "cryptographic-asset", "bom-ref": "key-1", "name": "signing-key"})
	return encode(t, m)
}

// T-5: --skip-files runs first, so the support checks never count a file twice.
func TestSkipFilesAndSupportSkip(t *testing.T) {
	data := filesAndKeyCDX(t)
	with, err := Score(data, keyStrings(allProfiles), testEngineVersion, Options{SkipFiles: true})
	if err != nil {
		t.Fatal(err)
	}
	if with.Input.ComponentsSkipped != 2 {
		t.Errorf("with the option: input.componentsSkipped %d, want 2", with.Input.ComponentsSkipped)
	}
	for _, id := range []string{supportLevelID, endOfSupportID} {
		assertSkipped(t, checkOf(t, with, id), 1, "cryptographic-asset")
	}

	without := scoreOK(t, data)
	if without.Input.ComponentsSkipped != 0 {
		t.Errorf("without the option: input.componentsSkipped %d, want 0", without.Input.ComponentsSkipped)
	}
	for _, id := range []string{supportLevelID, endOfSupportID} {
		assertSkipped(t, checkOf(t, without, id), 3, "cryptographic-asset", "file")
	}
	version := assertStatus(t, without, "fda.baseline.version-string", StatusFail)
	for _, name := range []string{"README.md", "bin/app"} {
		if !contains(version.Failing, name) {
			t.Errorf("fda.baseline.version-string failing %v, want the file %s", version.Failing, name)
		}
	}
}

// assertSkippedLine checks that each failing support check line of text is followed by want and
// then by its missing list.
func assertSkippedLine(t *testing.T, text, want string) {
	t.Helper()
	ls := lines(text)
	found := 0
	for i, l := range ls {
		if !strings.HasPrefix(l, "  FAIL  Software level of support  ") && !strings.HasPrefix(l, "  FAIL  End-of-support date  ") {
			continue
		}
		found++
		if i+2 >= len(ls) || ls[i+1] != want || !strings.HasPrefix(ls[i+2], "        missing in: ") {
			t.Errorf("after %q want %q then the missing list, got:\n%s", l, want, strings.Join(ls[i:], "\n"))
		}
	}
	if found != 2 {
		t.Errorf("%d failing support check lines, want 2:\n%s", found, text)
	}
}

// T-6 (a, b): the text line sits between the check line and its missing list, for one skipped
// component as for several; a report without skipped components has neither the line nor the
// JSON fields.
func TestSupportSkipReportOutput(t *testing.T) {
	data := filesAndKeyCDX(t)
	assertSkippedLine(t, scoreOK(t, data, ProfileFDA).Text(),
		"        skipped 3 components of type cryptographic-asset, file (not software packages)")
	one, err := Score(data, keyStrings([]ProfileKey{ProfileFDA}), testEngineVersion, Options{SkipFiles: true})
	if err != nil {
		t.Fatal(err)
	}
	assertSkippedLine(t, one.Text(),
		"        skipped 1 components of type cryptographic-asset (not software packages)")

	m := jsonFixture(t, "full.cdx.json")
	delete(gammaCDX(m), "properties")
	plain := scoreOK(t, encode(t, m), ProfileFDA)
	assertStatus(t, plain, supportLevelID, StatusFail)
	if txt := plain.Text(); strings.Contains(txt, "skipped ") {
		t.Errorf("text without skipped components mentions skipped:\n%s", txt)
	}
	// input.componentsSkipped (SCORE-10, --skip-files) is always in the report; no check carries
	// the per-check fields.
	j := reportJSON(t, scoreOK(t, readFixture(t, "full.cdx.json"), ProfileFDA))
	if n := bytes.Count(j, []byte(`"componentsSkipped"`)); n != 1 || !bytes.Contains(j, []byte(`"componentsSkipped": 0,`)) {
		t.Errorf("fda report of full.cdx.json: %d componentsSkipped keys, want only input.componentsSkipped 0", n)
	}
	if bytes.Contains(j, []byte(`"skippedTypes"`)) {
		t.Error("fda report of full.cdx.json carries skippedTypes")
	}
}

// T-6 (c): the fda golden of full.types.cdx.json carries the fields on the two support checks
// only; the other goldens carry them nowhere; reportVersion stays 1.
func TestSupportSkipGoldens(t *testing.T) {
	type golden struct {
		ReportVersion int `json:"reportVersion"`
		Profiles      []struct {
			Checks []map[string]any `json:"checks"`
		} `json:"profiles"`
	}
	files, err := filepath.Glob(filepath.Join("testdata", "golden", "*.json"))
	if err != nil || len(files) != 9 {
		t.Fatalf("goldens %v (%v), want 9", files, err)
	}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var g golden
		if err := json.Unmarshal(raw, &g); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if g.ReportVersion != 1 {
			t.Errorf("%s: reportVersion %d, want 1", f, g.ReportVersion)
		}
		isTypesFDA := filepath.Base(f) == "fda.types.cdx.json"
		for _, p := range g.Profiles {
			for _, c := range p.Checks {
				id := c["id"].(string)
				_, hasCount := c["componentsSkipped"]
				_, hasTypes := c["skippedTypes"]
				if !isTypesFDA || !supportCheckIDs[id] {
					if hasCount || hasTypes {
						t.Errorf("%s %s carries the skip fields", f, id)
					}
					if isTypesFDA && strings.HasPrefix(id, "fda.baseline.") && c["scope"] == string(ScopeComponent) && c["total"] != float64(6) {
						t.Errorf("%s %s: total %v, want 6", f, id, c["total"])
					}
					continue
				}
				if c["componentsSkipped"] != float64(2) || !reflect.DeepEqual(c["skippedTypes"], []any{"cryptographic-asset", "file"}) || c["total"] != float64(4) {
					t.Errorf("%s %s: componentsSkipped %v skippedTypes %v total %v, want 2 [cryptographic-asset file] 4",
						f, id, c["componentsSkipped"], c["skippedTypes"], c["total"])
				}
			}
		}
	}
}

// T-7: the skip is by type, not by result: a skipped component with valid support facts is neither
// passed nor failing.
func TestSupportSkipIsByType(t *testing.T) {
	m := typesCDX(t)
	for _, name := range []string{"kept-library", "kept-firmware", "skipped-cryptographic-asset"} {
		addedComponent(m, name)["properties"] = []any{
			map[string]any{"name": propSupportLevel, "value": "actively maintained"},
			map[string]any{"name": propEndOfSupport, "value": "2030-01-01T00:00:00Z"},
		}
	}
	r := scoreOK(t, encode(t, m), ProfileFDA)
	for _, id := range []string{supportLevelID, endOfSupportID} {
		c := checkOf(t, r, id)
		if c.Passed != 4+2 || c.Total != 13 {
			t.Errorf("%s: %d/%d, want 6/13 (the 4 fixture components, library and firmware)", id, c.Passed, c.Total)
		}
		assertSkipped(t, c, 5, "cryptographic-asset", "data", "device", "file", "machine-learning-model")
		if contains(c.Failing, "skipped-cryptographic-asset") || contains(c.Failing, "kept-library") || contains(c.Failing, "kept-firmware") {
			t.Errorf("%s: failing %v holds a component with support facts", id, c.Failing)
		}
	}
}

// T-8: the loaders keep the type as written.
func TestComponentTypeLoaded(t *testing.T) {
	for _, f := range []string{"full.cdx.json", "full.cdx.xml"} {
		if got := compOf(t, loadOK(t, readFixture(t, f)), "alpha").Type; got != "library" {
			t.Errorf("%s alpha: type %q, want library", f, got)
		}
	}

	m := jsonFixture(t, "full.cdx.json")
	delete(gammaCDX(m), "type")
	m["components"].([]any)[0].(map[string]any)["type"] = "Library"
	d := loadOK(t, encode(t, m))
	if got := compOf(t, d, "gamma").Type; got != "" {
		t.Errorf("untyped gamma: type %q, want empty", got)
	}
	if got := compOf(t, d, "alpha").Type; got != "Library" {
		t.Errorf("alpha typed Library: type %q, want it as written", got)
	}
	// Wrong case is not a skipped type: the component is judged.
	m = jsonFixture(t, "full.cdx.json")
	m["components"].([]any)[0].(map[string]any)["type"] = "File"
	r := scoreOK(t, encode(t, m), ProfileFDA)
	for _, id := range []string{supportLevelID, endOfSupportID} {
		c := checkOf(t, r, id)
		if c.Total != 4 {
			t.Errorf("%s with a component typed File: total %d, want 4", id, c.Total)
		}
		assertNotSkipped(t, c)
	}

	// Kept as written, neither trimmed nor case-folded: a padded or wrong-case skipped type is
	// judged, and it is not a file for --skip-files either.
	for _, typ := range []string{" file", "file ", " file ", "\tfile", "file\n", "\tfile\n", "File"} {
		m = jsonFixture(t, "full.cdx.json")
		m["components"].([]any)[0].(map[string]any)["type"] = typ
		data := encode(t, m)
		if c := compOf(t, loadOK(t, data), "alpha"); c.Type != typ || c.IsFile {
			t.Errorf("alpha typed %q: type %q, file %v, want it as written and not a file", typ, c.Type, c.IsFile)
		}
		r = scoreOK(t, data, ProfileFDA)
		for _, id := range []string{supportLevelID, endOfSupportID} {
			c := checkOf(t, r, id)
			if c.Total != 4 {
				t.Errorf("%s with a component typed %q: total %d, want 4", id, typ, c.Total)
			}
			assertNotSkipped(t, c)
		}
	}
	m = jsonFixture(t, "full.cdx.json")
	m["components"].([]any)[0].(map[string]any)["type"] = "file"
	if c := compOf(t, loadOK(t, encode(t, m)), "alpha"); c.Type != "file" || !c.IsFile {
		t.Errorf("alpha typed file: type %q, file %v, want file and a file", c.Type, c.IsFile)
	}

	m = jsonFixture(t, "full.spdx.json")
	spdxPackage(m, "SPDXRef-alpha")["primaryPackagePurpose"] = "LIBRARY"
	spdxPackage(m, "SPDXRef-beta")["primaryPackagePurpose"] = "FILE"
	d = loadOK(t, encode(t, m))
	for ref, want := range map[string]string{"SPDXRef-alpha": "LIBRARY", "SPDXRef-beta": "FILE", "SPDXRef-gamma": ""} {
		if got := compOf(t, d, ref).Type; got != want {
			t.Errorf("SPDX %s: type %q, want %q", ref, got, want)
		}
	}
	if c := compOf(t, d, "SPDXRef-beta"); !c.IsFile {
		t.Errorf("SPDX beta with purpose FILE: not a file")
	}
	spdxPackage(m, "SPDXRef-beta")["primaryPackagePurpose"] = "DEVICE"
	if got := compOf(t, loadOK(t, encode(t, m)), "SPDXRef-beta").Type; got != "DEVICE" {
		t.Errorf("SPDX beta with purpose DEVICE: type %q, want DEVICE", got)
	}
	// Kept as written, neither trimmed nor upper-cased: a padded or lower-case skipped purpose is
	// judged, and it is not a file for --skip-files either.
	for _, purpose := range []string{" FILE", "FILE ", " FILE ", "\tFILE", "FILE\n", "\tFILE\n", "file", "File"} {
		spdxPackage(m, "SPDXRef-beta")["primaryPackagePurpose"] = purpose
		data := encode(t, m)
		if c := compOf(t, loadOK(t, data), "SPDXRef-beta"); c.Type != purpose || c.IsFile {
			t.Errorf("SPDX beta with purpose %q: type %q, file %v, want it as written and not a file", purpose, c.Type, c.IsFile)
		}
		eos := checkOf(t, scoreOK(t, data, ProfileFDA), endOfSupportID)
		if eos.Total != 4 {
			t.Errorf("%s with a package of purpose %q: total %d, want 4", endOfSupportID, purpose, eos.Total)
		}
		assertNotSkipped(t, eos)
	}
}

// T-9: perSupportableComponent and evaluate.
func TestPerSupportableComponent(t *testing.T) {
	comps := func(types ...string) []Comp {
		out := make([]Comp, len(types))
		for i, typ := range types {
			out[i] = Comp{DisplayID: typ + "-" + string(rune('a'+i)), Type: typ}
		}
		return out
	}
	yes := func(*Comp) bool { return true }
	o := perSupportableComponent(&Doc{Format: FormatCycloneDX, Components: comps("data", "cryptographic-asset", "data", "library")}, yes)
	if o.Skipped != 3 || !reflect.DeepEqual(o.SkippedTypes, []string{"cryptographic-asset", "data"}) || o.Total != 1 || o.Passed != 1 || o.Note != "" {
		t.Errorf("outcome %+v, want 3 skipped [cryptographic-asset data], 1/1, no note", o)
	}
	o = perSupportableComponent(&Doc{Format: FormatCycloneDX, Components: comps("library", "", "FILE")}, func(*Comp) bool { return false })
	if o.Skipped != 0 || o.SkippedTypes != nil || o.Total != 3 || len(o.Failing) != 3 || o.Note != "" {
		t.Errorf("outcome %+v, want nothing skipped, nil types, 0/3, no note", o)
	}
	// The SPDX set is its own: a lowercase CycloneDX value is judged in SPDX.
	o = perSupportableComponent(&Doc{Format: FormatSPDX, Components: comps("file", "FILE", "DEVICE")}, yes)
	if o.Skipped != 2 || !reflect.DeepEqual(o.SkippedTypes, []string{"DEVICE", "FILE"}) || o.Total != 1 {
		t.Errorf("SPDX outcome %+v, want 2 skipped [DEVICE FILE], total 1", o)
	}

	d := &Doc{Format: FormatCycloneDX, Components: comps("data", "library")}
	res, cerr := evaluate(Check{ID: "x.skip", Scope: ScopeComponent, Level: LevelRequired, Eval: func(d *Doc) Outcome {
		perSupportableComponent(d, yes)
		panic("boom")
	}}, d)
	if cerr == nil || res.Status != StatusError || res.ComponentsSkipped != 0 || res.SkippedTypes != nil {
		t.Errorf("panicking check: %+v %v, want ERROR with no skip fields", res, cerr)
	}
	res, _ = evaluate(Check{ID: "x.ok", Scope: ScopeComponent, Level: LevelRequired, Eval: func(d *Doc) Outcome {
		return perSupportableComponent(d, yes)
	}}, d)
	if res.Status != StatusPass || res.ComponentsSkipped != 1 || !reflect.DeepEqual(res.SkippedTypes, []string{"data"}) {
		t.Errorf("check result %+v, want PASS 1/1 with 1 skipped [data]", res)
	}
}
