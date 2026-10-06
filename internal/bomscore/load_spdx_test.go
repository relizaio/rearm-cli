package bomscore

import (
	"bytes"
	"reflect"
	"testing"

	spdxjson "github.com/spdx/tools-golang/json"
	"github.com/spdx/tools-golang/spdx/v2/common"
)

// Test 18, SPDX part: the Tool creator is "toolidentifier-version"; the version is the text after
// the last "-" and both sides must be non-empty.
func TestSPDXToolVersion(t *testing.T) {
	cases := []struct {
		tool        string
		versionPass bool
	}{
		{"Tool: scanner", false},
		{"Tool: scanner-", false},
		{"Tool: -2.1", false},
		{"Tool: my-scanner-2.1", true},
		{"Tool: LicenseFind-1.0", true},
	}
	for _, c := range cases {
		t.Run(c.tool, func(t *testing.T) {
			m := jsonFixture(t, "full.spdx.json")
			spdxCreationInfo(m)["creators"] = []any{"Organization: Example Devices Inc.", c.tool}
			r := scoreOK(t, encode(t, m), ProfileCISA2026)
			want := StatusFail
			if c.versionPass {
				want = StatusPass
			}
			assertStatus(t, r, "cisa-2026.sbom-tool-version", want)
			assertStatus(t, r, "cisa-2026.sbom-tool-name", StatusPass)
		})
	}
}

var dependencyChecks = []string{
	"cisa-2026.component-dependency-relationship",
	"ntia-2021.dependency-relationship",
	"fda.baseline.relationship",
}

// Test 19: only a dependency relationship between packages (or to NONE) counts; the CONTAINS the
// reader adds for hasFiles, and an end of NOASSERTION, do not.
func TestSPDXDependencyRelationship(t *testing.T) {
	noDependsOn := func(t *testing.T) map[string]any {
		m := jsonFixture(t, "full.spdx.json")
		delete(m, "relationships")
		return m
	}

	m := noDependsOn(t)
	m["files"] = []any{map[string]any{
		"SPDXID": "SPDXRef-file-readme", "fileName": "./README.md",
		"checksums": []any{map[string]any{"algorithm": "SHA1", "checksumValue": "da39a3ee5e6b4b0d3255bfef95601890afd80709"}},
	}}
	gamma := gammaSPDX(m)
	gamma["filesAnalyzed"] = true
	gamma["hasFiles"] = []any{"SPDXRef-file-readme"}
	data := encode(t, m)
	doc, err := spdxjson.Read(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	folded := false
	for _, rel := range doc.Relationships {
		if rel.Relationship == common.TypeRelationshipContains && rel.RefB.ElementRefID == "file-readme" {
			folded = true
		}
	}
	if !folded {
		t.Fatal("the reader no longer folds hasFiles into a CONTAINS relationship; this test proves nothing")
	}
	r := scoreOK(t, data)
	for _, id := range dependencyChecks {
		assertStatus(t, r, id, StatusFail)
	}

	for _, c := range []struct {
		related string
		want    Status
	}{{"NONE", StatusPass}, {"NOASSERTION", StatusFail}} {
		m := noDependsOn(t)
		m["relationships"] = []any{map[string]any{
			"spdxElementId": "SPDXRef-delta", "relationshipType": "DEPENDS_ON", "relatedSpdxElement": c.related,
		}}
		r := scoreOK(t, encode(t, m))
		for _, id := range dependencyChecks {
			assertStatus(t, r, id, c.want)
		}
	}
}

// Test 20: the described package(s) are left out of the component set, whichever way the document
// names them.
func TestSPDXDescribedPackages(t *testing.T) {
	describes := func(a, rel, b string) map[string]any {
		return map[string]any{"spdxElementId": a, "relationshipType": rel, "relatedSpdxElement": b}
	}
	cases := []struct {
		name       string
		fixture    string
		mutate     func(m map[string]any)
		components int
		absent     []string
	}{
		{name: "documentDescribes", fixture: "full.spdx.json", mutate: func(map[string]any) {}, components: 4,
			absent: []string{"pkg:generic/example-device-app@5.0.0"}},
		{name: "documentDescribes, SPDX 2.2", fixture: "full-2.2.spdx.json", mutate: func(map[string]any) {}, components: 4,
			absent: []string{"pkg:generic/example-device-app@5.0.0"}},
		{name: "DESCRIBES relationship", fixture: "full.spdx.json", mutate: func(m map[string]any) {
			delete(m, "documentDescribes")
			m["relationships"] = append(m["relationships"].([]any), describes("SPDXRef-DOCUMENT", "DESCRIBES", "SPDXRef-app"))
		}, components: 4, absent: []string{"pkg:generic/example-device-app@5.0.0"}},
		{name: "DESCRIBED_BY relationship", fixture: "full.spdx.json", mutate: func(m map[string]any) {
			delete(m, "documentDescribes")
			m["relationships"] = append(m["relationships"].([]any), describes("SPDXRef-app", "DESCRIBED_BY", "SPDXRef-DOCUMENT"))
		}, components: 4, absent: []string{"pkg:generic/example-device-app@5.0.0"}},
		{name: "two described packages", fixture: "full.spdx.json", mutate: func(m map[string]any) {
			m["documentDescribes"] = []any{"SPDXRef-app", "SPDXRef-alpha"}
		}, components: 3, absent: []string{"pkg:generic/example-device-app@5.0.0", "pkg:npm/alpha@1.0.0"}},
		{name: "nothing described", fixture: "full.spdx.json", mutate: func(m map[string]any) {
			delete(m, "documentDescribes")
		}, components: 5},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := jsonFixture(t, c.fixture)
			c.mutate(m)
			d := loadOK(t, encode(t, m))
			if len(d.Components) != c.components {
				t.Errorf("components %d, want %d", len(d.Components), c.components)
			}
			for _, comp := range d.Components {
				if contains(c.absent, comp.DisplayID) {
					t.Errorf("described package %s is in the component set", comp.DisplayID)
				}
			}
		})
	}
}

func TestSPDXSerializationsScoreAlike(t *testing.T) {
	fromJSON := scoreOK(t, readFixture(t, "full.spdx.json"))
	for _, f := range []string{"full.spdx.yaml", "full.spdx"} {
		if r := scoreOK(t, readFixture(t, f)); !reflect.DeepEqual(r.Profiles, fromJSON.Profiles) {
			t.Errorf("%s scores differently from full.spdx.json:\n%+v\n%+v", f, r.Profiles, fromJSON.Profiles)
		}
	}
}
