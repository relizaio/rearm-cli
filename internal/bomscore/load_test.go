package bomscore

import (
	"errors"
	"strings"
	"testing"
)

// Test 9: the eight accepted fixtures load, name their format, and give the ntia-2021 verdict of
// the JSON fixture they were derived from.
func TestLoadAcceptedFormats(t *testing.T) {
	cases := []struct {
		file          string
		format        Format
		specVersion   string
		serialization Serialization
		derivedFrom   string
	}{
		{"full-1.4.cdx.json", FormatCycloneDX, "1.4", SerializationJSON, "full.cdx.json"},
		{"full.cdx.json", FormatCycloneDX, "1.6", SerializationJSON, "full.cdx.json"},
		{"full.cdx.xml", FormatCycloneDX, "1.6", SerializationXML, "full.cdx.json"},
		{"full-1.7.cdx.json", FormatCycloneDX, "1.7", SerializationJSON, "full.cdx.json"},
		{"full-2.2.spdx.json", FormatSPDX, "2.2", SerializationJSON, "full.spdx.json"},
		{"full.spdx.json", FormatSPDX, "2.3", SerializationJSON, "full.spdx.json"},
		{"full.spdx.yaml", FormatSPDX, "2.3", SerializationYAML, "full.spdx.json"},
		{"full.spdx", FormatSPDX, "2.3", SerializationTagValue, "full.spdx.json"},
	}
	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			r := scoreOK(t, readFixture(t, c.file), ProfileNTIA2021)
			in := r.Input
			if in.Format != c.format || in.SpecVersion != c.specVersion || in.Serialization != c.serialization {
				t.Errorf("input %s %s %s, want %s %s %s", in.Format, in.SpecVersion, in.Serialization, c.format, c.specVersion, c.serialization)
			}
			if in.Components != 4 {
				t.Errorf("components %d, want 4", in.Components)
			}
			want := profileOf(t, scoreOK(t, readFixture(t, c.derivedFrom), ProfileNTIA2021), ProfileNTIA2021).Verdict
			if got := profileOf(t, r, ProfileNTIA2021).Verdict; got != want {
				t.Errorf("ntia-2021 %s, want %s as %s", got, want, c.derivedFrom)
			}
		})
	}
}

// Test 10: refused inputs, each with its exact message and no report.
func TestLoadRefusals(t *testing.T) {
	cdxJSON := string(readFixture(t, "full.cdx.json"))
	cdxXML := string(readFixture(t, "full.cdx.xml"))
	spdxJSON := string(readFixture(t, "full.spdx.json"))
	cases := []struct {
		name, input, want string
		prefixOnly        bool
	}{
		{name: "empty input", input: "", want: "unsupported SBOM: empty input"},
		{name: "whitespace only", input: " \n\t ", want: "unsupported SBOM: empty input"},
		{name: "byte-order mark only", input: "\xEF\xBB\xBF", want: "unsupported SBOM: empty input"},
		{name: "plain text", input: "this is not an SBOM\n", want: "unsupported SBOM: not a CycloneDX or SPDX document"},
		{name: "JSON of neither format", input: `{"runs": [], "version": "2.1.0"}`, want: "unsupported SBOM: not a CycloneDX or SPDX document"},
		{name: "broken JSON of neither format", input: `{"runs": [`, want: "unsupported SBOM: not a CycloneDX or SPDX document"},
		{name: "CycloneDX JSON without specVersion", input: strings.Replace(cdxJSON, `"specVersion": "1.6",`, "", 1), want: "unsupported SBOM: CycloneDX specVersion missing or not 1.0 to 1.7"},
		{name: "CycloneDX JSON specVersion 2.0", input: strings.Replace(cdxJSON, `"specVersion": "1.6"`, `"specVersion": "2.0"`, 1), want: "unsupported SBOM: CycloneDX specVersion missing or not 1.0 to 1.7"},
		{name: "CycloneDX XML unknown namespace", input: strings.Replace(cdxXML, "http://cyclonedx.org/schema/bom/1.6", "http://cyclonedx.org/schema/bom/2.0", 1), want: "unsupported SBOM: CycloneDX specVersion missing or not 1.0 to 1.7"},
		{name: "SPDX 3 JSON-LD", input: `{"@context": "https://spdx.org/rdf/3.0.1/spdx-context.jsonld", "@graph": []}`, want: "unsupported SBOM: SPDX 3 is not supported"},
		{name: "SPDX RDF/XML", input: `<?xml version="1.0"?><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#" xmlns:spdx="http://spdx.org/rdf/terms#"><spdx:SpdxDocument rdf:about="#SPDXRef-DOCUMENT"><spdx:specVersion>SPDX-2.1</spdx:specVersion></spdx:SpdxDocument></rdf:RDF>`, want: "unsupported SBOM: SPDX RDF/XML is not supported"},
		{name: "XML of neither format", input: `<?xml version="1.0"?><project><name>x</name></project>`, want: "unsupported SBOM: not a CycloneDX or SPDX document"},
		{name: "SPDX JSON 2.0", input: strings.Replace(spdxJSON, `"SPDX-2.3"`, `"SPDX-2.0"`, 1), want: "unsupported SBOM: SPDX 2.0 is not supported in json"},
		{name: "SPDX tag-value 2.0", input: "SPDXVersion: SPDX-2.0\nDataLicense: CC0-1.0\n", want: "unsupported SBOM: SPDX 2.0 is not supported in tag-value"},
		{name: "SPDX YAML 2.0", input: "spdxVersion: SPDX-2.0\nSPDXID: SPDXRef-DOCUMENT\n", want: "unsupported SBOM: SPDX 2.0 is not supported in yaml"},
		{name: "SPDX JSON version without the prefix", input: strings.Replace(spdxJSON, `"SPDX-2.3"`, `"2.3"`, 1), want: `unsupported SBOM: SPDX version "2.3" is not supported in json`},
		{name: "SPDX JSON version not a string", input: strings.Replace(spdxJSON, `"SPDX-2.3"`, `null`, 1), want: "unsupported SBOM: SPDX version null is not supported in json"},
		{name: "SPDX YAML version a number", input: "spdxVersion: 2.3\nSPDXID: SPDXRef-DOCUMENT\n", want: "unsupported SBOM: SPDX version 2.3 is not supported in yaml"},
		{name: "YAML of neither format", input: "name: x\nversion: 1\n", want: "unsupported SBOM: not a CycloneDX or SPDX document"},
		{name: "truncated CycloneDX JSON", input: cdxJSON[:len(cdxJSON)/2], want: "unparsable SBOM: ", prefixOnly: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, err := Score([]byte(c.input), keyStrings(allProfiles), testEngineVersion, Options{})
			if err == nil {
				t.Fatalf("scored, want refusal %q", c.want)
			}
			var refusal *RefusalError
			if !errors.As(err, &refusal) {
				t.Fatalf("error %T %v, want *RefusalError", err, err)
			}
			if c.prefixOnly {
				if !strings.HasPrefix(err.Error(), c.want) || len(err.Error()) == len(c.want) {
					t.Errorf("error %q, want prefix %q and a library message", err, c.want)
				}
			} else if err.Error() != c.want {
				t.Errorf("error %q, want %q", err, c.want)
			}
			if r.ReportVersion != 0 || r.Profiles != nil {
				t.Errorf("a report came back with the refusal: %+v", r)
			}
		})
	}
}

// Test 10, last row: a CycloneDX document without components (VEX-only) is scored, and every
// COMPONENT check fails with 0/0.
func TestLoadVEXOnlyIsScored(t *testing.T) {
	vex := `{"bomFormat": "CycloneDX", "specVersion": "1.6", "version": 1,
		"vulnerabilities": [{"id": "CVE-2026-0001", "analysis": {"state": "not_affected", "justification": "code_not_reachable"}}]}`
	r := scoreOK(t, []byte(vex))
	if r.Input.Components != 0 {
		t.Fatalf("components %d, want 0", r.Input.Components)
	}
	for _, p := range r.Profiles {
		for _, c := range p.Checks {
			if c.Scope == ScopeComponent && (c.Status != StatusFail || c.Passed != 0 || c.Total != 0) {
				t.Errorf("%s: %s %d/%d, want FAIL 0/0", c.ID, c.Status, c.Passed, c.Total)
			}
		}
	}
}

func TestLoadByteOrderMarkAndLeadingWhitespace(t *testing.T) {
	data := append([]byte("\xEF\xBB\xBF\n  "), readFixture(t, "full.cdx.json")...)
	if r := scoreOK(t, data, ProfileCISA2026); profileOf(t, r, ProfileCISA2026).Verdict != VerdictReady {
		t.Errorf("BOM-marked input did not score READY")
	}
}
