package bomscore

import (
	"reflect"
	"testing"
)

// Test 1: catalogue, green.
func TestCatalogueGreenCycloneDX(t *testing.T) {
	r := scoreOK(t, readFixture(t, "full.cdx.json"))
	for _, p := range r.Profiles {
		if p.Verdict != VerdictReady || p.Score == nil || *p.Score != 100 {
			t.Errorf("%s: verdict %s score %v, want READY 100", p.Key, p.Verdict, p.Score)
		}
		for _, c := range p.Checks {
			if c.Level == LevelRequired && c.Status != StatusPass {
				t.Errorf("%s: %s, want PASS", c.ID, c.Status)
			}
		}
	}
}

func TestCatalogueGreenSPDX(t *testing.T) {
	r := scoreOK(t, readFixture(t, "full.spdx.json"))
	if p := profileOf(t, r, ProfileNTIA2021); p.Verdict != VerdictReady {
		t.Errorf("ntia-2021: %s, want READY", p.Verdict)
	}
	wantFail := map[string]bool{
		"cisa-2026.sbom-author-signature":   true,
		"cisa-2026.sbom-generation-context": true,
		"cisa-2026.sbom-version":            true,
		"fda.component.support-level":       true,
	}
	for _, key := range []ProfileKey{ProfileCISA2026, ProfileFDA} {
		p := profileOf(t, r, key)
		if p.Verdict != VerdictNotReady {
			t.Errorf("%s: %s, want NOT_READY", key, p.Verdict)
		}
		for _, c := range p.Checks {
			if c.Level != LevelRequired {
				continue
			}
			if wantFail[c.ID] {
				if c.Status != StatusFail || c.Note != "not representable in SPDX 2.3" {
					t.Errorf("%s: %s %q, want FAIL with the not-representable note", c.ID, c.Status, c.Note)
				}
			} else if c.Status != StatusPass {
				t.Errorf("%s: %s, want PASS", c.ID, c.Status)
			}
		}
	}
}

// catalogueRow removes one field for one REQUIRED check. Component mutations hit gamma, which is
// nested one level under beta, so the red cases also go through nested components.
type catalogueRow struct {
	cdx  func(m map[string]any)
	spdx func(m map[string]any) // nil when SPDX 2.x cannot carry the field
	doc  func(d *Doc)           // instead of cdx/spdx, for the fields a loaded file always has
	// failing is the display id of the mutated component (COMPONENT scope), per format.
	failingCDX, failingSPDX string
	// spdxAlsoFails: checks of the same profile with another evaluator that the SPDX mutation
	// necessarily fails too.
	spdxAlsoFails []string
}

const (
	gammaPurl   = "pkg:pypi/gamma@3.1.0"
	gammaNoPurl = "gamma@3.1.0"
)

// The mutations, one per field.
var (
	mutAuthorCDX = func(m map[string]any) {
		md := cdxMetadata(m)
		delete(md, "authors")
		delete(md, "manufacturer")
	}
	mutAuthorSPDX = func(m map[string]any) {
		spdxCreationInfo(m)["creators"] = []any{"Tool: example-scanner-4.2.0"}
	}
	mutTimestampCDX   = func(m map[string]any) { delete(cdxMetadata(m), "timestamp") }
	mutTimestampSPDX  = func(m map[string]any) { delete(spdxCreationInfo(m), "created") }
	mutDependencyCDX  = func(m map[string]any) { delete(m, "dependencies") }
	mutDependencySPDX = func(m map[string]any) {
		delete(m, "relationships")
	}
	mutGammaCDX = func(field string) func(m map[string]any) {
		return func(m map[string]any) { delete(gammaCDX(m), field) }
	}
	mutGammaSPDX = func(field string) func(m map[string]any) {
		return func(m map[string]any) { delete(gammaSPDX(m), field) }
	}
)

// gammaCDX and gammaSPDX find the component the red cases mutate; they run outside a test's
// helpers, so a fixture that no longer has it panics, which fails the test applying the mutation.
func gammaCDX(m map[string]any) map[string]any {
	beta := m["components"].([]any)[1].(map[string]any)
	gamma := beta["components"].([]any)[0].(map[string]any)
	if gamma["bom-ref"] != "gamma" {
		panic("fixture changed: gamma is expected under beta")
	}
	return gamma
}

func gammaSPDX(m map[string]any) map[string]any {
	for _, e := range m["packages"].([]any) {
		if p := e.(map[string]any); p["SPDXID"] == "SPDXRef-gamma" {
			return p
		}
	}
	panic("fixture changed: no package SPDXRef-gamma")
}

func catalogueRows() map[string]catalogueRow {
	producer := catalogueRow{
		cdx: mutGammaCDX("authors"), spdx: mutGammaSPDX("supplier"),
		failingCDX: gammaPurl, failingSPDX: gammaPurl,
	}
	name := catalogueRow{
		cdx: mutGammaCDX("name"), spdx: func(m map[string]any) { gammaSPDX(m)["name"] = "" },
		failingCDX: gammaPurl, failingSPDX: gammaPurl,
	}
	version := catalogueRow{
		cdx: mutGammaCDX("version"), spdx: mutGammaSPDX("versionInfo"),
		failingCDX: gammaPurl, failingSPDX: gammaPurl,
	}
	identifiers := catalogueRow{
		cdx: func(m map[string]any) {
			g := gammaCDX(m)
			delete(g, "purl")
			delete(g, "swhid")
		},
		spdx:       mutGammaSPDX("externalRefs"),
		failingCDX: gammaNoPurl, failingSPDX: gammaNoPurl,
	}
	hashAlg := catalogueRow{
		cdx: func(m map[string]any) {
			delete(gammaCDX(m)["hashes"].([]any)[0].(map[string]any), "alg")
		},
		spdx: func(m map[string]any) {
			gammaSPDX(m)["checksums"].([]any)[0].(map[string]any)["algorithm"] = ""
		},
		failingCDX: gammaPurl, failingSPDX: gammaPurl,
	}
	hashValue := catalogueRow{
		cdx: func(m map[string]any) {
			delete(gammaCDX(m)["hashes"].([]any)[0].(map[string]any), "content")
		},
		spdx: func(m map[string]any) {
			gammaSPDX(m)["checksums"].([]any)[0].(map[string]any)["checksumValue"] = ""
		},
		failingCDX: gammaPurl, failingSPDX: gammaPurl,
	}
	author := catalogueRow{cdx: mutAuthorCDX, spdx: mutAuthorSPDX}
	timestamp := catalogueRow{cdx: mutTimestampCDX, spdx: mutTimestampSPDX}
	dependency := catalogueRow{cdx: mutDependencyCDX, spdx: mutDependencySPDX}

	return map[string]catalogueRow{
		"cisa-2026.sbom-author": author,
		"cisa-2026.sbom-author-signature": {
			cdx: func(m map[string]any) { delete(m, "signature") },
		},
		"cisa-2026.sbom-data-format-name":    {doc: func(d *Doc) { d.Format = "" }},
		"cisa-2026.sbom-data-format-version": {doc: func(d *Doc) { d.SpecVersion = "" }},
		"cisa-2026.sbom-generation-context": {
			cdx: func(m map[string]any) { delete(cdxMetadata(m), "lifecycles") },
		},
		"cisa-2026.sbom-timestamp": timestamp,
		"cisa-2026.sbom-tool-name": {
			cdx: func(m map[string]any) {
				tool := cdxMetadata(m)["tools"].(map[string]any)["components"].([]any)[0].(map[string]any)
				tool["name"] = ""
			},
			// SPDX: a Tool creator always has a name, so only removing every Tool creator fails
			// the name, and with no tool at all the version fails too.
			spdx: func(m map[string]any) {
				spdxCreationInfo(m)["creators"] = []any{"Organization: Example Devices Inc."}
			},
			spdxAlsoFails: []string{"cisa-2026.sbom-tool-version"},
		},
		"cisa-2026.sbom-tool-version": {
			cdx: func(m map[string]any) {
				tool := cdxMetadata(m)["tools"].(map[string]any)["components"].([]any)[0].(map[string]any)
				delete(tool, "version")
			},
			spdx: func(m map[string]any) {
				spdxCreationInfo(m)["creators"] = []any{"Organization: Example Devices Inc.", "Tool: scanner"}
			},
		},
		"cisa-2026.sbom-version": {
			cdx: func(m map[string]any) { m["version"] = 0 },
		},
		"cisa-2026.component-name":                    name,
		"cisa-2026.component-version":                 version,
		"cisa-2026.component-producer":                producer,
		"cisa-2026.component-identifiers":             identifiers,
		"cisa-2026.component-hash-algorithm":          hashAlg,
		"cisa-2026.component-hash-value":              hashValue,
		"cisa-2026.component-license":                 {cdx: mutGammaCDX("licenses"), spdx: mutGammaSPDX("licenseDeclared"), failingCDX: gammaPurl, failingSPDX: gammaPurl},
		"cisa-2026.component-dependency-relationship": dependency,

		"ntia-2021.supplier-name":            producer,
		"ntia-2021.component-name":           name,
		"ntia-2021.component-version":        version,
		"ntia-2021.other-unique-identifiers": identifiers,
		"ntia-2021.dependency-relationship":  dependency,
		"ntia-2021.author-of-sbom-data":      author,
		"ntia-2021.timestamp":                timestamp,

		"fda.baseline.author-name":       author,
		"fda.baseline.timestamp":         timestamp,
		"fda.baseline.supplier-name":     producer,
		"fda.baseline.component-name":    name,
		"fda.baseline.version-string":    version,
		"fda.baseline.component-hash":    hashValue,
		"fda.baseline.unique-identifier": identifiers,
		"fda.baseline.relationship":      dependency,
		"fda.component.support-level": {
			cdx:        func(m map[string]any) { removeProperty(gammaCDX(m), propSupportLevel) },
			failingCDX: gammaPurl,
		},
		"fda.component.end-of-support": {
			cdx:        func(m map[string]any) { removeProperty(gammaCDX(m), propEndOfSupport) },
			spdx:       mutGammaSPDX("validUntilDate"),
			failingCDX: gammaPurl, failingSPDX: gammaPurl,
		},
	}
}

// Test 2: catalogue, red. One row per REQUIRED check id (17 + 7 + 10 = 34); a declared check id
// without a row fails the test.
func TestCatalogueRed(t *testing.T) {
	rows := catalogueRows()
	declared := 0
	for _, p := range profiles {
		for _, c := range p.Checks {
			if c.Level != LevelRequired {
				continue
			}
			declared++
			if _, ok := rows[c.ID]; !ok {
				t.Errorf("REQUIRED check %s has no row", c.ID)
			}
		}
	}
	if declared != 34 || len(rows) != 34 {
		t.Fatalf("declared %d REQUIRED checks and %d rows, want 34 and 34", declared, len(rows))
	}

	baseline := map[Format]Report{
		FormatCycloneDX: scoreOK(t, readFixture(t, "full.cdx.json")),
		FormatSPDX:      scoreOK(t, readFixture(t, "full.spdx.json")),
	}
	for _, p := range profiles {
		for _, c := range p.Checks {
			row, ok := rows[c.ID]
			if !ok {
				continue
			}
			if row.doc != nil {
				t.Run(c.ID+"/doc", func(t *testing.T) {
					d, err := Load(readFixture(t, "full.cdx.json"))
					if err != nil {
						t.Fatal(err)
					}
					row.doc(d)
					res, cerr := evaluate(c, d)
					if cerr != nil || res.Status != StatusFail {
						t.Errorf("status %s, error %v, want FAIL", res.Status, cerr)
					}
					pr, _ := scoreProfile(p, d)
					if pr.Verdict != VerdictNotReady {
						t.Errorf("verdict %s, want NOT_READY", pr.Verdict)
					}
					for _, other := range pr.Checks {
						if other.ID == c.ID {
							continue
						}
						if want := checkOf(t, baseline[FormatCycloneDX], other.ID).Status; other.Status != want {
							t.Errorf("%s: %s, want %s as on the full fixture", other.ID, other.Status, want)
						}
					}
				})
				continue
			}
			t.Run(c.ID+"/cdx", func(t *testing.T) {
				m := jsonFixture(t, "full.cdx.json")
				row.cdx(m)
				assertOnlyFails(t, p, c, scoreOK(t, encode(t, m)), baseline[FormatCycloneDX], row.failingCDX, nil)
			})
			if row.spdx != nil {
				t.Run(c.ID+"/spdx", func(t *testing.T) {
					m := jsonFixture(t, "full.spdx.json")
					row.spdx(m)
					assertOnlyFails(t, p, c, scoreOK(t, encode(t, m)), baseline[FormatSPDX], row.failingSPDX, row.spdxAlsoFails)
				})
			}
		}
	}
}

// assertOnlyFails: the check fails (COMPONENT: passed == total - 1 and the component is in
// failing); every other check of the profile that does not share its evaluator keeps its baseline
// status; the verdict is NOT_READY.
func assertOnlyFails(t *testing.T, p Profile, target Check, r, baseline Report, failing string, alsoFails []string) {
	t.Helper()
	got := assertStatus(t, r, target.ID, StatusFail)
	if target.Scope == ScopeComponent {
		if got.Passed != got.Total-1 {
			t.Errorf("%s: passed %d of %d, want total - 1", target.ID, got.Passed, got.Total)
		}
		if !contains(got.Failing, failing) {
			t.Errorf("%s: failing %v, want it to name %s", target.ID, got.Failing, failing)
		}
	}
	evaluator := reflect.ValueOf(target.Eval).Pointer()
	for _, c := range p.Checks {
		if c.ID == target.ID || reflect.ValueOf(c.Eval).Pointer() == evaluator {
			continue
		}
		want := checkOf(t, baseline, c.ID).Status
		if contains(alsoFails, c.ID) {
			want = StatusFail
		}
		assertStatus(t, r, c.ID, want)
	}
	if v := profileOf(t, r, p.Key).Verdict; v != VerdictNotReady {
		t.Errorf("%s: verdict %s, want NOT_READY", p.Key, v)
	}
}
