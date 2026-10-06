package bomscore

import "testing"

// Test 3: each "else" and "any of" of the profile tables passes on the alternative alone.
func TestAlternativesCount(t *testing.T) {
	cdx := []struct {
		name   string
		mutate func(m map[string]any)
		checks []string
	}{
		{"producer from supplier only", func(m map[string]any) {
			g := gammaCDX(m)
			delete(g, "authors")
			g["supplier"] = map[string]any{"name": "Gamma Supplier"}
		}, []string{"cisa-2026.component-producer", "ntia-2021.supplier-name", "fda.baseline.supplier-name"}},
		{"producer from manufacturer only", func(m map[string]any) {
			g := gammaCDX(m)
			delete(g, "authors")
			g["manufacturer"] = map[string]any{"name": "Gamma Manufacturer"}
		}, []string{"cisa-2026.component-producer"}},
		{"producer from the legacy author only", func(m map[string]any) {
			g := gammaCDX(m)
			delete(g, "authors")
			g["author"] = "Gamma Author"
		}, []string{"cisa-2026.component-producer"}},
		{"identifier from cpe only", func(m map[string]any) {
			g := gammaCDX(m)
			delete(g, "purl")
			delete(g, "swhid")
			g["cpe"] = "cpe:2.3:a:gamma:gamma:3.1.0:*:*:*:*:*:*:*"
		}, []string{"cisa-2026.component-identifiers", "ntia-2021.other-unique-identifiers", "fda.baseline.unique-identifier"}},
		{"identifier from swid only", func(m map[string]any) {
			g := gammaCDX(m)
			delete(g, "purl")
			delete(g, "swhid")
			g["swid"] = map[string]any{"tagId": "swidgen-gamma-3.1.0", "name": "gamma"}
		}, []string{"cisa-2026.component-identifiers"}},
		{"identifier from swhid only", func(m map[string]any) {
			delete(gammaCDX(m), "purl")
		}, []string{"cisa-2026.component-identifiers"}},
		{"identifier from omniborId only", func(m map[string]any) {
			g := gammaCDX(m)
			delete(g, "purl")
			delete(g, "swhid")
			g["omniborId"] = []any{"gitoid:blob:sha1:261eeb9e9f8b2b4b0d119366dda99c6fd7d35c64"}
		}, []string{"cisa-2026.component-identifiers"}},
		{"license from expression only", func(m map[string]any) {
			gammaCDX(m)["licenses"] = []any{map[string]any{"expression": "MIT OR Apache-2.0"}}
		}, []string{"cisa-2026.component-license"}},
		{"license from name only", func(m map[string]any) {
			gammaCDX(m)["licenses"] = []any{map[string]any{"license": map[string]any{"name": "Custom"}}}
		}, []string{"cisa-2026.component-license"}},
		{"sbom-author from metadata.manufacturer alone", func(m map[string]any) {
			delete(cdxMetadata(m), "authors")
		}, []string{"cisa-2026.sbom-author", "ntia-2021.author-of-sbom-data", "fda.baseline.author-name"}},
		{"sbom-author from metadata.authors alone", func(m map[string]any) {
			delete(cdxMetadata(m), "manufacturer")
		}, []string{"cisa-2026.sbom-author"}},
		{"tools from tools.services alone", func(m map[string]any) {
			cdxMetadata(m)["tools"] = map[string]any{"services": []any{map[string]any{"name": "scan-service", "version": "7.1"}}}
		}, []string{"cisa-2026.sbom-tool-name", "cisa-2026.sbom-tool-version"}},
		{"tools from tools.components alone", func(map[string]any) {}, []string{"cisa-2026.sbom-tool-name", "cisa-2026.sbom-tool-version"}},
		{"generation context from a lifecycle name", func(m map[string]any) {
			cdxMetadata(m)["lifecycles"] = []any{map[string]any{"name": "nightly-build", "description": "custom phase"}}
		}, []string{"cisa-2026.sbom-generation-context"}},
	}
	for _, c := range cdx {
		t.Run("cdx "+c.name, func(t *testing.T) {
			m := jsonFixture(t, "full.cdx.json")
			c.mutate(m)
			r := scoreOK(t, encode(t, m))
			for _, id := range c.checks {
				assertStatus(t, r, id, StatusPass)
			}
		})
	}

	t.Run("cdx tools in the 1.4 array form", func(t *testing.T) {
		r := scoreOK(t, readFixture(t, "full-1.4.cdx.json"))
		assertStatus(t, r, "cisa-2026.sbom-tool-name", StatusPass)
		assertStatus(t, r, "cisa-2026.sbom-tool-version", StatusPass)
	})

	spdx := []struct {
		name   string
		mutate func(m map[string]any)
		checks []string
	}{
		{"originator only", func(m map[string]any) {
			g := gammaSPDX(m)
			delete(g, "supplier")
			g["originator"] = "Organization: Gamma Origin"
		}, []string{"cisa-2026.component-producer", "ntia-2021.supplier-name", "fda.baseline.supplier-name"}},
		{"licenseConcluded only", func(m map[string]any) {
			g := gammaSPDX(m)
			delete(g, "licenseDeclared")
			g["licenseConcluded"] = "MIT"
		}, []string{"cisa-2026.component-license"}},
		{"licenseConcluded behind a NOASSERTION licenseDeclared", func(m map[string]any) {
			g := gammaSPDX(m)
			g["licenseDeclared"] = "NOASSERTION"
			g["licenseConcluded"] = "MIT"
		}, []string{"cisa-2026.component-license"}},
		{"cpe22Type only", func(m map[string]any) {
			gammaSPDX(m)["externalRefs"] = []any{map[string]any{"referenceCategory": "SECURITY", "referenceType": "cpe22Type", "referenceLocator": "cpe:/a:gamma:gamma:3.1.0"}}
		}, []string{"cisa-2026.component-identifiers"}},
		{"Person creator only", func(m map[string]any) {
			spdxCreationInfo(m)["creators"] = []any{"Person: Jane Builder", "Tool: example-scanner-4.2.0"}
		}, []string{"cisa-2026.sbom-author", "ntia-2021.author-of-sbom-data", "fda.baseline.author-name"}},
		{"DEPENDENCY_OF only", func(m map[string]any) {
			m["relationships"] = []any{map[string]any{"spdxElementId": "SPDXRef-delta", "relationshipType": "DEPENDENCY_OF", "relatedSpdxElement": "SPDXRef-gamma"}}
		}, dependencyChecks},
		{"CONTAINS between packages only", func(m map[string]any) {
			m["relationships"] = []any{map[string]any{"spdxElementId": "SPDXRef-app", "relationshipType": "CONTAINS", "relatedSpdxElement": "SPDXRef-alpha"}}
		}, dependencyChecks},
	}
	for _, c := range spdx {
		t.Run("spdx "+c.name, func(t *testing.T) {
			m := jsonFixture(t, "full.spdx.json")
			c.mutate(m)
			r := scoreOK(t, encode(t, m))
			for _, id := range c.checks {
				assertStatus(t, r, id, StatusPass)
			}
		})
	}
}

// Test 4: declared-unknown values count as missing.
func TestUnknownMarkersCountAsMissing(t *testing.T) {
	spdx := []struct {
		name   string
		mutate func(g map[string]any)
		check  string
	}{
		{"NOASSERTION supplier", func(g map[string]any) { g["supplier"] = "NOASSERTION" }, "cisa-2026.component-producer"},
		{"NOASSERTION version", func(g map[string]any) { g["versionInfo"] = "NOASSERTION" }, "cisa-2026.component-version"},
		{"NOASSERTION and NONE license", func(g map[string]any) {
			g["licenseDeclared"] = "NOASSERTION"
			g["licenseConcluded"] = "NONE"
		}, "cisa-2026.component-license"},
	}
	for _, c := range spdx {
		t.Run("spdx "+c.name, func(t *testing.T) {
			m := jsonFixture(t, "full.spdx.json")
			c.mutate(gammaSPDX(m))
			got := assertStatus(t, scoreOK(t, encode(t, m)), c.check, StatusFail)
			if !contains(got.Failing, gammaPurl) {
				t.Errorf("failing %v, want %s", got.Failing, gammaPurl)
			}
		})
	}
	t.Run("cdx whitespace-only version", func(t *testing.T) {
		m := jsonFixture(t, "full.cdx.json")
		gammaCDX(m)["version"] = "   "
		assertStatus(t, scoreOK(t, encode(t, m)), "cisa-2026.component-version", StatusFail)
	})
}

// The timestamp checks parse the value as RFC 3339 (design P-3.3); a present value that does not
// parse fails them like a missing one, in both formats, and an RFC 3339 value with an offset and
// fractional seconds passes.
func TestTimestampMustParseRFC3339(t *testing.T) {
	timestampChecks := []string{"cisa-2026.sbom-timestamp", "ntia-2021.timestamp", "fda.baseline.timestamp"}
	formats := []struct {
		name    string
		fixture string
		set     func(m map[string]any, v string)
	}{
		{"cdx", "full.cdx.json", func(m map[string]any, v string) { cdxMetadata(m)["timestamp"] = v }},
		{"spdx", "full.spdx.json", func(m map[string]any, v string) { spdxCreationInfo(m)["created"] = v }},
	}
	for _, f := range formats {
		baseline := scoreOK(t, readFixture(t, f.fixture))
		for _, v := range []string{"yesterday", "2026-10-06", "2026-10-06T10:00:00"} {
			t.Run(f.name+"/"+v, func(t *testing.T) {
				m := jsonFixture(t, f.fixture)
				f.set(m, v)
				r := scoreOK(t, encode(t, m))
				for _, id := range timestampChecks {
					p, c := checkDeclaration(t, id)
					assertOnlyFails(t, p, c, r, baseline, "", nil)
				}
			})
		}
		t.Run(f.name+"/offset and fraction", func(t *testing.T) {
			m := jsonFixture(t, f.fixture)
			f.set(m, "2026-10-06T10:00:00.123+02:00")
			r := scoreOK(t, encode(t, m))
			for _, id := range timestampChecks {
				assertStatus(t, r, id, StatusPass)
			}
		})
	}
}

// Test 22, O3: a declared-unknown value counts as missing (isPresent).
func TestOperatorPoint_O3(t *testing.T) {
	for _, v := range []string{"", "  ", "\t", "NOASSERTION", "NONE", " NOASSERTION "} {
		if isPresent(v) {
			t.Errorf("isPresent(%q) = true, want false", v)
		}
	}
	for _, v := range []string{"MIT", "0", "none", "x"} {
		if !isPresent(v) {
			t.Errorf("isPresent(%q) = false, want true", v)
		}
	}
}
