package bomscore

import (
	"sort"
	"strings"

	"github.com/package-url/packageurl-go"
)

// The structure checks are INFO checks on CycloneDX input only. They never change a verdict or a
// score, and the report always carries them (empty for SPDX).
var structureChecks = []Check{
	{ID: "structure.purl-valid", Title: "Package URLs parse", Level: LevelInfo, Scope: ScopeComponent,
		Ref: "purl specification", Eval: purlsValid},
	{ID: "structure.refs-resolve", Title: "Dependency references resolve", Level: LevelInfo, Scope: ScopeDocument,
		Ref: "CycloneDX dependencies, bom-ref", Eval: refsResolve},
	{ID: "structure.no-orphans", Title: "Every component is in the dependency graph", Level: LevelInfo, Scope: ScopeComponent,
		Ref: "CycloneDX dependencies", Eval: noOrphans},
}

// purlsValid: every purl parses; a component without a purl has nothing to fail.
func purlsValid(d *Doc) Outcome {
	return perComponent(d, func(c *Comp) bool {
		for _, id := range c.Identifiers {
			if id.Kind == IdentifierPURL && strings.TrimSpace(id.Value) != "" {
				if _, err := packageurl.FromString(id.Value); err != nil {
					return false
				}
			}
		}
		return true
	})
}

// refsResolve: every ref and dependsOn of dependencies[] names a bom-ref of the document, and no
// bom-ref is declared twice. The note names the offending refs (at most maxFailing).
func refsResolve(d *Doc) Outcome {
	known := map[string]int{}
	for _, r := range d.Refs {
		known[r]++
	}
	var duplicates, unresolved []string
	for r, n := range known {
		if n > 1 {
			duplicates = append(duplicates, r)
		}
	}
	seen := map[string]bool{}
	check := func(r string) {
		if known[r] == 0 && !seen[r] {
			seen[r] = true
			unresolved = append(unresolved, r)
		}
	}
	for _, dep := range d.Dependencies {
		check(dep.Ref)
		for _, r := range dep.DependsOn {
			check(r)
		}
	}
	if len(duplicates) == 0 && len(unresolved) == 0 {
		return docOutcome(true)
	}
	var parts []string
	if len(unresolved) > 0 {
		parts = append(parts, "unresolved: "+capList(unresolved))
	}
	if len(duplicates) > 0 {
		parts = append(parts, "duplicate bom-ref: "+capList(duplicates))
	}
	o := docOutcome(false)
	o.Note = strings.Join(parts, "; ")
	return o
}

// noOrphans: every component's bom-ref appears in dependencies[], as a ref or in a dependsOn.
func noOrphans(d *Doc) Outcome {
	inGraph := map[string]bool{}
	for _, dep := range d.Dependencies {
		inGraph[dep.Ref] = true
		for _, r := range dep.DependsOn {
			inGraph[r] = true
		}
	}
	return perComponent(d, func(c *Comp) bool { return c.Ref != "" && inGraph[c.Ref] })
}

func capList(items []string) string {
	sort.Strings(items)
	if len(items) > maxFailing {
		return strings.Join(items[:maxFailing], ", ") + ", ..."
	}
	return strings.Join(items, ", ")
}
