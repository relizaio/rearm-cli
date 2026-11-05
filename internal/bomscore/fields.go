package bomscore

import (
	"sort"
	"strings"
	"time"
)

// The CycloneDX component properties the fda profile reads; ReARM writes both into exports with
// support metadata (cyclonedx-taxonomy.md, SupportBomInjector).
const (
	propSupportLevel = "reliza:support:levelOfSupport"
	propEndOfSupport = "cdx:lifecycle:milestone:endOfSupport"
)

// propSelfComponent marks a component as the subject's own entry: ReARM writes it, with the literal
// value "true", on the organization's own releases and artifacts listed in an export (SCORE-58).
// Only the two fda support checks read it, through perSupportableComponent.
const propSelfComponent = "reliza:support:selfComponent"

// The justified-unknown properties (D5 of FDAW-1): ReARM writes one on a component that lacks the
// fact when the manufacturer recorded why it cannot be given (FDA guidance V.A.4(b), "a
// justification for why the information cannot be included"), and strips uploaded copies. Only the
// two fda checks read them: fda.component.end-of-support and fda.baseline.component-hash.
const (
	propJustifiedEndOfSupport = "reliza:support:justifiedUnknown:endOfSupport"
	propJustifiedHash         = "reliza:support:justifiedUnknown:hash"
)

// justifiedReasons are the only values of the two justified-unknown properties that count as
// justified: the JustifiedUnknownReason enum names of ReARM (cyclonedx-taxonomy.md), exact (no
// trimming, no case folding, so a document cannot widen the list), one list for both properties.
var justifiedReasons = map[string]bool{
	"NO_COMMITTED_END_OF_SUPPORT":    true,
	"COMMUNITY_PROJECT":              true,
	"SUPPLIER_PUBLISHES_NO_DATE":     true,
	"DISTRIBUTION_PUBLISHES_NO_HASH": true,
	"VENDORED_SOURCE_NO_HASH":        true,
	"OTHER":                          true,
}

// supportLevels are the only values of propSupportLevel that pass fda.component.support-level.
var supportLevels = map[string]bool{
	"actively maintained":  true,
	"no longer maintained": true,
	"abandoned":            true,
}

// skippedSupportTypes are the component types the fda support checks do not judge: no supplier
// supports a file, key material, a data set, an ML model or a device. Exact, case-sensitive values
// of CycloneDX component.type and SPDX 2.3 primaryPackagePurpose; an absent or any other value is
// judged. Nothing else in the engine reads this set.
var skippedSupportTypes = map[Format]map[string]bool{
	FormatCycloneDX: {"file": true, "cryptographic-asset": true, "data": true, "machine-learning-model": true, "device": true},
	FormatSPDX:      {"FILE": true, "DEVICE": true},
}

// noteNoSupportable is the note of a support check that skipped every component.
const noteNoSupportable = "no supportable component"

// supportable says whether the fda support checks judge the component (false for the skipped types).
func (c *Comp) supportable(f Format) bool { return !skippedSupportTypes[f][c.Type] }

// selfComponent says whether the component carries propSelfComponent with the exact value "true":
// no trimming, no case folding, so a document cannot widen what the support checks skip.
func (c *Comp) selfComponent() bool {
	for _, p := range c.Properties {
		if p.Name == propSelfComponent && p.Value == "true" {
			return true
		}
	}
	return false
}

// justifiedBy says whether the component carries the property with a recognised reason code (any
// occurrence).
func (c *Comp) justifiedBy(prop string) bool {
	for _, p := range c.Properties {
		if p.Name == prop && justifiedReasons[p.Value] {
			return true
		}
	}
	return false
}

// neverJustified is the justified predicate of every check that does not read a justification.
func neverJustified(*Comp) bool { return false }

// isPresent says whether a value states something. Operator point O3: a declared-unknown value
// (SPDX NOASSERTION or NONE, an empty or whitespace-only string) counts as missing. To let declared
// unknowns pass, change this one function.
func isPresent(v string) bool {
	t := strings.TrimSpace(v)
	return t != "" && t != spdxNoAssertion && t != spdxNone
}

func anyPresent(vs []string) bool {
	for _, v := range vs {
		if isPresent(v) {
			return true
		}
	}
	return false
}

// The notes of a failing dependency check.
const (
	noteNoSubject     = "no metadata.component"
	noteNoSubjectRef  = "metadata.component has no bom-ref"
	noteNoDescribes   = "no DESCRIBES relationship"
	noteSubjectSilent = "described component declares no direct dependencies: "
)

func notRepresentableNote(d *Doc) string {
	return "not representable in " + string(d.Format) + " " + d.SpecVersion
}

// docOutcome is a DOCUMENT check's outcome.
func docOutcome(ok bool) Outcome {
	if ok {
		return Outcome{Passed: 1, Total: 1}
	}
	return Outcome{Passed: 0, Total: 1}
}

// docNotRepresentable fails a DOCUMENT check whose field the format cannot carry.
func docNotRepresentable(d *Doc) Outcome {
	return Outcome{Passed: 0, Total: 1, NotRepresentable: true, Note: notRepresentableNote(d)}
}

// perComponent evaluates has on every component of the set.
func perComponent(d *Doc, has func(c *Comp) bool) Outcome {
	return perComponentJ(d, has, neverJustified)
}

// perComponentJ is perComponent with a justification: a component without the field that is
// justified counts in Justified, not in Failing. The value wins: a component with both is Passed.
func perComponentJ(d *Doc, has, justified func(c *Comp) bool) Outcome {
	o := Outcome{Total: len(d.Components)}
	for i := range d.Components {
		c := &d.Components[i]
		judge(&o, c, has, justified)
	}
	return o
}

// judge counts one judged component: Passed when it has the field, else Justified when it carries
// a recognised justification, else Failing.
func judge(o *Outcome, c *Comp, has, justified func(c *Comp) bool) {
	switch {
	case has(c):
		o.Passed++
	case justified(c):
		o.Justified++
	default:
		o.Failing = append(o.Failing, c.DisplayID)
	}
}

// perSupportableComponent evaluates has on the supportable components only and reports the rest:
// the subject's own components (selfComponent, counted first and only as self), then how many were
// skipped by type and their distinct types, sorted. When it skipped every component the check has
// total 0 (FAIL) and the note noteNoSupportable.
func perSupportableComponent(d *Doc, has func(c *Comp) bool) Outcome {
	return perSupportableComponentJ(d, has, neverJustified)
}

// perSupportableComponentJ is perSupportableComponent with a justification (perComponentJ); the
// skips come first, so a skipped component is skipped whether or not it carries one.
func perSupportableComponentJ(d *Doc, has, justified func(c *Comp) bool) Outcome {
	var o Outcome
	types := map[string]bool{}
	for i := range d.Components {
		c := &d.Components[i]
		if c.selfComponent() {
			o.SelfSkipped++
			continue
		}
		if !c.supportable(d.Format) {
			o.Skipped++
			types[c.Type] = true
			continue
		}
		o.Total++
		judge(&o, c, has, justified)
	}
	for t := range types {
		o.SkippedTypes = append(o.SkippedTypes, t)
	}
	sort.Strings(o.SkippedTypes)
	if (o.Skipped > 0 || o.SelfSkipped > 0) && o.Total == 0 {
		o.Note = noteNoSupportable
	}
	return o
}

// compNotRepresentable fails a COMPONENT check whose field the format cannot carry: every
// component fails.
func compNotRepresentable(d *Doc) Outcome {
	o := perComponent(d, func(*Comp) bool { return false })
	o.NotRepresentable = true
	o.Note = notRepresentableNote(d)
	return o
}

// parseRFC3339 accepts an RFC 3339 date-time (fractional seconds allowed).
func parseRFC3339(v string) bool {
	_, err := time.Parse(time.RFC3339, strings.TrimSpace(v))
	return err == nil
}

// parseISODate accepts an ISO 8601 calendar date or an RFC 3339 / local ISO 8601 date-time.
func parseISODate(v string) bool {
	t := strings.TrimSpace(v)
	for _, layout := range []string{time.DateOnly, time.RFC3339, "2006-01-02T15:04:05"} {
		if _, err := time.Parse(layout, t); err == nil {
			return true
		}
	}
	return false
}

// Document evaluators.

func hasSBOMAuthor(d *Doc) Outcome { return docOutcome(anyPresent(d.Authors)) }

func hasFormatName(d *Doc) Outcome { return docOutcome(isPresent(string(d.Format))) }

func hasFormatVersion(d *Doc) Outcome { return docOutcome(isPresent(d.SpecVersion)) }

func hasGenerationContext(d *Doc) Outcome {
	if d.notRepresentable(FieldGenerationContext) {
		return docNotRepresentable(d)
	}
	return docOutcome(anyPresent(d.Lifecycles))
}

func hasTimestamp(d *Doc) Outcome { return docOutcome(parseRFC3339(d.Timestamp)) }

func hasToolName(d *Doc) Outcome {
	for _, t := range d.Tools {
		if isPresent(t.Name) {
			return docOutcome(true)
		}
	}
	return docOutcome(false)
}

func hasToolVersion(d *Doc) Outcome {
	if len(d.Tools) == 0 {
		return docOutcome(false)
	}
	for _, t := range d.Tools {
		if !isPresent(t.Version) {
			return docOutcome(false)
		}
	}
	return docOutcome(true)
}

func hasSBOMVersion(d *Doc) Outcome {
	if d.notRepresentable(FieldSBOMVersion) {
		return docNotRepresentable(d)
	}
	return docOutcome(d.SBOMVersion >= 1)
}

// declaresDirectDependencies: the document names its described component(s) and every one of
// them declares its direct dependencies, by a dependency entry with a non-empty dependsOn or by a
// statement that it has none (Doc.DeclaredNoDependencies). Entries of any other component count
// for nothing. A dependsOn naming only unresolvable refs still counts; structure.refs-resolve
// reports it.
func declaresDirectDependencies(d *Doc) Outcome {
	if len(d.SubjectRefs) == 0 {
		o := docOutcome(false)
		switch {
		case d.Format == FormatSPDX:
			o.Note = noteNoDescribes
		case d.SubjectNamed:
			o.Note = noteNoSubjectRef
		default:
			o.Note = noteNoSubject
		}
		return o
	}
	declares := map[string]bool{}
	for _, dep := range d.Dependencies {
		if len(dep.DependsOn) > 0 {
			declares[dep.Ref] = true
		}
	}
	var silent []string
	for _, ref := range d.SubjectRefs {
		if !declares[ref] && !d.DeclaredNoDependencies[ref] {
			silent = append(silent, ref)
		}
	}
	if len(silent) == 0 {
		return docOutcome(true)
	}
	o := docOutcome(false)
	o.Note = noteSubjectSilent + capList(silent)
	return o
}

// Component evaluators.

func hasComponentName(d *Doc) Outcome {
	return perComponent(d, func(c *Comp) bool { return isPresent(c.Name) })
}

func hasComponentVersion(d *Doc) Outcome {
	return perComponent(d, func(c *Comp) bool { return isPresent(c.Version) })
}

func hasComponentProducer(d *Doc) Outcome {
	return perComponent(d, func(c *Comp) bool { return anyPresent(c.Producers) })
}

func hasComponentIdentifier(d *Doc) Outcome {
	return perComponent(d, func(c *Comp) bool {
		for _, id := range c.Identifiers {
			if isPresent(id.Value) {
				return true
			}
		}
		return false
	})
}

func hasComponentHashAlgorithm(d *Doc) Outcome {
	return perComponent(d, func(c *Comp) bool {
		for _, h := range c.Hashes {
			if isPresent(h.Algorithm) {
				return true
			}
		}
		return false
	})
}

func hasComponentHashValue(d *Doc) Outcome {
	return perComponent(d, func(c *Comp) bool {
		for _, h := range c.Hashes {
			if isPresent(h.Value) {
				return true
			}
		}
		return false
	})
}

// hasComponentHash: one hash entry with both algorithm and value; else justified by
// propJustifiedHash (fda only: cisa-2026 uses hasComponentHashAlgorithm and hasComponentHashValue).
func hasComponentHash(d *Doc) Outcome {
	return perComponentJ(d, func(c *Comp) bool {
		for _, h := range c.Hashes {
			if isPresent(h.Algorithm) && isPresent(h.Value) {
				return true
			}
		}
		return false
	}, func(c *Comp) bool { return c.justifiedBy(propJustifiedHash) })
}

func hasComponentLicense(d *Doc) Outcome {
	return perComponent(d, func(c *Comp) bool { return anyPresent(c.Licenses) })
}

func hasSupportLevel(d *Doc) Outcome {
	if d.notRepresentable(FieldSupportLevel) {
		return compNotRepresentable(d)
	}
	return perSupportableComponent(d, func(c *Comp) bool {
		for _, p := range c.Properties {
			if p.Name == propSupportLevel && supportLevels[p.Value] {
				return true
			}
		}
		return false
	})
}

func hasEndOfSupport(d *Doc) Outcome {
	if d.notRepresentable(FieldEndOfSupport) {
		return compNotRepresentable(d)
	}
	return perSupportableComponentJ(d, func(c *Comp) bool {
		if d.Format == FormatSPDX {
			return parseISODate(c.ValidUntil)
		}
		for _, p := range c.Properties {
			if p.Name == propEndOfSupport && parseISODate(p.Value) {
				return true
			}
		}
		return false
	}, func(c *Comp) bool { return c.justifiedBy(propJustifiedEndOfSupport) })
}

// notAssessed is the evaluator of an INFO check that one file cannot decide.
func notAssessed(note string) func(d *Doc) Outcome {
	return func(*Doc) Outcome { return Outcome{NotAssessed: true, Note: note} }
}
