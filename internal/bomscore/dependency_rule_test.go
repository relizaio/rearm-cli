package bomscore

import (
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// SCORE-10: the dependency checks pass only when the described component declares its direct
// dependencies, or states it has none.

const silentApp = noteSubjectSilent + "app"

// cdxRootDependency is the dependencies[] entry of the fixture's metadata.component (bom-ref app).
func cdxRootDependency(m map[string]any) map[string]any {
	for _, e := range m["dependencies"].([]any) {
		if dep := e.(map[string]any); dep["ref"] == "app" {
			return dep
		}
	}
	panic("fixture changed: no dependencies[] entry for app")
}

func removeCDXRootDependency(m map[string]any) {
	var kept []any
	for _, e := range m["dependencies"].([]any) {
		if e.(map[string]any)["ref"] != "app" {
			kept = append(kept, e)
		}
	}
	m["dependencies"] = kept
}

func cdxComposition(aggregate string, refs ...string) map[string]any {
	deps := make([]any, len(refs))
	for i, r := range refs {
		deps[i] = r
	}
	return map[string]any{"aggregate": aggregate, "dependencies": deps}
}

func spdxPackage(m map[string]any, id string) map[string]any {
	for _, e := range m["packages"].([]any) {
		if p := e.(map[string]any); p["SPDXID"] == id {
			return p
		}
	}
	panic("fixture changed: no package " + id)
}

func spdxRelationship(a, rel, b string) map[string]any {
	return map[string]any{"spdxElementId": a, "relationshipType": rel, "relatedSpdxElement": b}
}

// spdxWithoutRootRelationships keeps the fixture's relationships that do not start at the
// described package SPDXRef-app, then appends extra.
func spdxWithoutRootRelationships(m map[string]any, extra ...map[string]any) {
	var kept []any
	for _, e := range m["relationships"].([]any) {
		if e.(map[string]any)["spdxElementId"] != "SPDXRef-app" {
			kept = append(kept, e)
		}
	}
	for _, r := range extra {
		kept = append(kept, r)
	}
	m["relationships"] = kept
}

// assertDependencyChecks asserts status and note on the three dependency checks.
func assertDependencyChecks(t *testing.T, r Report, want Status, note string) {
	t.Helper()
	for _, id := range dependencyChecks {
		c := assertStatus(t, r, id, want)
		if c.Note != note {
			t.Errorf("%s: note %q, want %q", id, c.Note, note)
		}
	}
}

var xmlRootDependency = regexp.MustCompile(`(?s)<dependency ref="app">.*?</dependency>`)

func TestDeclaresDirectDependencies(t *testing.T) {
	cdxCases := []struct {
		name   string
		mutate func(m map[string]any)
		want   Status
		note   string
	}{
		{"1 unchanged", func(map[string]any) {}, StatusPass, ""},
		{"2 root entry removed, other entries kept", removeCDXRootDependency, StatusFail, silentApp},
		{"3 root entry with an empty dependsOn", func(m map[string]any) {
			cdxRootDependency(m)["dependsOn"] = []any{}
		}, StatusFail, silentApp},
		{"4 empty root entry and a complete composition", func(m map[string]any) {
			cdxRootDependency(m)["dependsOn"] = []any{}
			m["compositions"] = []any{cdxComposition("complete", "app")}
		}, StatusPass, ""},
		{"5 no root entry and a complete composition", func(m map[string]any) {
			removeCDXRootDependency(m)
			m["compositions"] = []any{cdxComposition("complete", "app")}
		}, StatusPass, ""},
		{"6 incomplete composition and an empty root entry", func(m map[string]any) {
			cdxRootDependency(m)["dependsOn"] = []any{}
			m["compositions"] = []any{cdxComposition("incomplete", "app")}
		}, StatusFail, silentApp},
		{"6 unknown composition and an empty root entry", func(m map[string]any) {
			cdxRootDependency(m)["dependsOn"] = []any{}
			m["compositions"] = []any{cdxComposition("unknown", "app")}
		}, StatusFail, silentApp},
		{"7 complete composition naming another ref", func(m map[string]any) {
			cdxRootDependency(m)["dependsOn"] = []any{}
			m["compositions"] = []any{cdxComposition("complete", "alpha")}
		}, StatusFail, silentApp},
		{"8 incomplete composition and a non-empty root entry", func(m map[string]any) {
			m["compositions"] = []any{cdxComposition("incomplete", "app")}
		}, StatusPass, ""},
		{"9 metadata.component without a bom-ref", func(m map[string]any) {
			delete(cdxMetadata(m)["component"].(map[string]any), "bom-ref")
		}, StatusFail, noteNoSubjectRef},
		{"9 metadata.component with a blank bom-ref", func(m map[string]any) {
			cdxMetadata(m)["component"].(map[string]any)["bom-ref"] = " "
		}, StatusFail, noteNoSubjectRef},
		{"10 no metadata.component", func(m map[string]any) {
			delete(cdxMetadata(m), "component")
		}, StatusFail, noteNoSubject},
	}
	for _, c := range cdxCases {
		t.Run("cdx "+c.name, func(t *testing.T) {
			m := jsonFixture(t, "full.cdx.json")
			c.mutate(m)
			assertDependencyChecks(t, scoreOK(t, encode(t, m)), c.want, c.note)
		})
	}

	for _, f := range []string{"full.cdx.xml", "full-1.4.cdx.json", "full.spdx", "full.spdx.yaml"} {
		t.Run("11 and 13 "+f, func(t *testing.T) {
			assertDependencyChecks(t, scoreOK(t, readFixture(t, f)), StatusPass, "")
		})
	}

	t.Run("cdx xml empty root entry, with and without a complete composition", func(t *testing.T) {
		full := string(readFixture(t, "full.cdx.xml"))
		emptied := xmlRootDependency.ReplaceAllString(full, `<dependency ref="app"/>`)
		if emptied == full || strings.Count(emptied, `<dependency ref="alpha"/>`) != 1 {
			t.Fatal("could not empty the root dependency of full.cdx.xml")
		}
		assertDependencyChecks(t, scoreOK(t, []byte(emptied)), StatusFail, silentApp)
		complete := strings.Replace(emptied, "</dependencies>",
			"</dependencies>\n  <compositions><composition><aggregate>complete</aggregate><dependencies><dependency ref=\"app\"/></dependencies></composition></compositions>", 1)
		assertDependencyChecks(t, scoreOK(t, []byte(complete)), StatusPass, "")
	})

	spdxCases := []struct {
		name   string
		mutate func(m map[string]any)
		want   Status
		note   string
	}{
		{"unchanged", func(map[string]any) {}, StatusPass, ""},
		{"DEPENDS_ON from the described package removed", func(m map[string]any) {
			spdxWithoutRootRelationships(m)
		}, StatusFail, noteSubjectSilent + "SPDXRef-app"},
		{"DEPENDENCY_OF a package to the described package", func(m map[string]any) {
			spdxWithoutRootRelationships(m, spdxRelationship("SPDXRef-alpha", "DEPENDENCY_OF", "SPDXRef-app"))
		}, StatusPass, ""},
		{"CONTAINS", func(m map[string]any) {
			spdxWithoutRootRelationships(m, spdxRelationship("SPDXRef-app", "CONTAINS", "SPDXRef-alpha"))
		}, StatusPass, ""},
		{"CONTAINED_BY", func(m map[string]any) {
			spdxWithoutRootRelationships(m, spdxRelationship("SPDXRef-alpha", "CONTAINED_BY", "SPDXRef-app"))
		}, StatusPass, ""},
		{"DEPENDS_ON NONE", func(m map[string]any) {
			spdxWithoutRootRelationships(m, spdxRelationship("SPDXRef-app", "DEPENDS_ON", "NONE"))
		}, StatusPass, ""},
		{"CONTAINS NONE", func(m map[string]any) {
			spdxWithoutRootRelationships(m, spdxRelationship("SPDXRef-app", "CONTAINS", "NONE"))
		}, StatusPass, ""},
		{"DEPENDS_ON NOASSERTION", func(m map[string]any) {
			spdxWithoutRootRelationships(m, spdxRelationship("SPDXRef-app", "DEPENDS_ON", "NOASSERTION"))
		}, StatusFail, noteSubjectSilent + "SPDXRef-app"},
		{"CONTAINS a file", func(m map[string]any) {
			m["files"] = []any{map[string]any{
				"SPDXID": "SPDXRef-file-readme", "fileName": "./README.md",
				"checksums": []any{map[string]any{"algorithm": "SHA1", "checksumValue": "da39a3ee5e6b4b0d3255bfef95601890afd80709"}},
			}}
			spdxWithoutRootRelationships(m, spdxRelationship("SPDXRef-app", "CONTAINS", "SPDXRef-file-readme"))
		}, StatusFail, noteSubjectSilent + "SPDXRef-app"},
		{"DESCRIBES removed", func(m map[string]any) {
			delete(m, "documentDescribes")
		}, StatusFail, noteNoDescribes},
		{"two described packages, one declaring", func(m map[string]any) {
			m["documentDescribes"] = []any{"SPDXRef-app", "SPDXRef-alpha"}
		}, StatusFail, noteSubjectSilent + "SPDXRef-alpha"},
		{"two described packages, both declaring", func(m map[string]any) {
			m["documentDescribes"] = []any{"SPDXRef-app", "SPDXRef-alpha"}
			m["relationships"] = append(m["relationships"].([]any), spdxRelationship("SPDXRef-alpha", "DEPENDS_ON", "NONE"))
		}, StatusPass, ""},
	}
	for _, c := range spdxCases {
		t.Run("12 spdx "+c.name, func(t *testing.T) {
			m := jsonFixture(t, "full.spdx.json")
			c.mutate(m)
			assertDependencyChecks(t, scoreOK(t, encode(t, m)), c.want, c.note)
		})
	}
}

// The three checks keep their declaration and carry the dependency remedy.
func TestDependencyCheckDeclarations(t *testing.T) {
	want := map[string]Check{
		"cisa-2026.component-dependency-relationship": {Title: "Component Dependency Relationship", Ref: "Table 1, Component Dependency Relationship"},
		"ntia-2021.dependency-relationship":           {Title: "Dependency Relationship", Ref: "Data Fields, Dependency Relationship"},
		"fda.baseline.relationship":                   {Title: "Relationship", Ref: "V.A.4(b); NTIA Framing 2nd ed. 2.2, Relationship"},
	}
	for id, w := range want {
		_, c := checkDeclaration(t, id)
		if c.Title != w.Title || c.Ref != w.Ref || c.Level != LevelRequired || c.Scope != ScopeDocument {
			t.Errorf("%s: %q %q %s %s, want %q %q REQUIRED DOCUMENT", id, c.Title, c.Ref, c.Level, c.Scope, w.Title, w.Ref)
		}
		if c.Remedy != remedyDependencies {
			t.Errorf("%s: remedy %q, want %q", id, c.Remedy, remedyDependencies)
		}
	}
	if !strings.Contains(remedyDependencies, "compositions") || !strings.Contains(remedyDependencies, "DEPENDS_ON") {
		t.Errorf("remedy %q names neither compositions nor DEPENDS_ON", remedyDependencies)
	}
}

// What the loaders read for the rule.
func TestDependencyRuleLoad(t *testing.T) {
	d := loadOK(t, readFixture(t, "full.cdx.json"))
	if !reflect.DeepEqual(d.SubjectRefs, []string{"app"}) || !d.SubjectNamed || len(d.DeclaredNoDependencies) != 0 {
		t.Errorf("cdx fixture: refs %v named %v none %v, want [app] true {}", d.SubjectRefs, d.SubjectNamed, d.DeclaredNoDependencies)
	}

	m := jsonFixture(t, "full.cdx.json")
	cdxRootDependency(m)["dependsOn"] = []any{}
	m["compositions"] = []any{cdxComposition("complete", "app", "alpha"), cdxComposition("incomplete", "beta")}
	d = loadOK(t, encode(t, m))
	if want := map[string]bool{"app": true, "alpha": true}; !reflect.DeepEqual(d.DeclaredNoDependencies, want) {
		t.Errorf("complete and incomplete compositions: %v, want %v", d.DeclaredNoDependencies, want)
	}

	m = jsonFixture(t, "full.cdx.json")
	delete(cdxMetadata(m)["component"].(map[string]any), "bom-ref")
	if d = loadOK(t, encode(t, m)); len(d.SubjectRefs) != 0 || !d.SubjectNamed {
		t.Errorf("no bom-ref: refs %v named %v, want none and true", d.SubjectRefs, d.SubjectNamed)
	}
	delete(cdxMetadata(m), "component")
	if d = loadOK(t, encode(t, m)); len(d.SubjectRefs) != 0 || d.SubjectNamed {
		t.Errorf("no metadata.component: refs %v named %v, want none and false", d.SubjectRefs, d.SubjectNamed)
	}

	d = loadOK(t, readFixture(t, "full.spdx.json"))
	wantDeps := []Dependency{
		{Ref: "SPDXRef-app", DependsOn: []string{"SPDXRef-alpha", "SPDXRef-beta"}},
		{Ref: "SPDXRef-beta", DependsOn: []string{"SPDXRef-gamma"}},
		{Ref: "SPDXRef-gamma", DependsOn: []string{"SPDXRef-delta"}},
	}
	if !reflect.DeepEqual(d.SubjectRefs, []string{"SPDXRef-app"}) || !d.SubjectNamed || !reflect.DeepEqual(d.Dependencies, wantDeps) {
		t.Errorf("spdx fixture: refs %v named %v dependencies %+v", d.SubjectRefs, d.SubjectNamed, d.Dependencies)
	}

	m = jsonFixture(t, "full.spdx.json")
	m["documentDescribes"] = []any{"SPDXRef-app", "SPDXRef-alpha"}
	m["relationships"] = []any{
		spdxRelationship("SPDXRef-alpha", "DEPENDENCY_OF", "SPDXRef-app"),
		spdxRelationship("SPDXRef-gamma", "CONTAINED_BY", "SPDXRef-beta"),
		spdxRelationship("SPDXRef-alpha", "DEPENDS_ON", "NONE"),
		spdxRelationship("SPDXRef-delta", "DEPENDS_ON", "NOASSERTION"),
	}
	d = loadOK(t, encode(t, m))
	wantDeps = []Dependency{
		{Ref: "SPDXRef-app", DependsOn: []string{"SPDXRef-alpha"}},
		{Ref: "SPDXRef-beta", DependsOn: []string{"SPDXRef-gamma"}},
	}
	if !reflect.DeepEqual(d.SubjectRefs, []string{"SPDXRef-app", "SPDXRef-alpha"}) || !reflect.DeepEqual(d.Dependencies, wantDeps) ||
		!reflect.DeepEqual(d.DeclaredNoDependencies, map[string]bool{"SPDXRef-alpha": true}) {
		t.Errorf("spdx mirrors and NONE: refs %v dependencies %+v none %v", d.SubjectRefs, d.Dependencies, d.DeclaredNoDependencies)
	}

	m = jsonFixture(t, "full.spdx.json")
	delete(m, "documentDescribes")
	if d = loadOK(t, encode(t, m)); len(d.SubjectRefs) != 0 || d.SubjectNamed {
		t.Errorf("no DESCRIBES: refs %v named %v, want none and false", d.SubjectRefs, d.SubjectNamed)
	}
}
