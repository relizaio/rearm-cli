package bomscore

import (
	"strings"
	"time"
)

// The CycloneDX component properties the fda profile reads; ReARM writes both into exports with
// support metadata (cyclonedx-taxonomy.md, SupportBomInjector).
const (
	propSupportLevel = "reliza:support:levelOfSupport"
	propEndOfSupport = "cdx:lifecycle:milestone:endOfSupport"
)

// supportLevels are the only values of propSupportLevel that pass fda.component.support-level.
var supportLevels = map[string]bool{
	"actively maintained":  true,
	"no longer maintained": true,
	"abandoned":            true,
}

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
	o := Outcome{Total: len(d.Components)}
	for i := range d.Components {
		c := &d.Components[i]
		if has(c) {
			o.Passed++
		} else {
			o.Failing = append(o.Failing, c.DisplayID)
		}
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

func hasSignature(d *Doc) Outcome {
	if d.notRepresentable(FieldSignature) {
		return docNotRepresentable(d)
	}
	return docOutcome(d.SignaturePresent)
}

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

func hasDependencyRelationship(d *Doc) Outcome { return docOutcome(d.HasDependencies) }

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

// hasComponentHash: one hash entry with both algorithm and value.
func hasComponentHash(d *Doc) Outcome {
	return perComponent(d, func(c *Comp) bool {
		for _, h := range c.Hashes {
			if isPresent(h.Algorithm) && isPresent(h.Value) {
				return true
			}
		}
		return false
	})
}

func hasComponentLicense(d *Doc) Outcome {
	return perComponent(d, func(c *Comp) bool { return anyPresent(c.Licenses) })
}

func hasSupportLevel(d *Doc) Outcome {
	if d.notRepresentable(FieldSupportLevel) {
		return compNotRepresentable(d)
	}
	return perComponent(d, func(c *Comp) bool {
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
	return perComponent(d, func(c *Comp) bool {
		if d.Format == FormatSPDX {
			return parseISODate(c.ValidUntil)
		}
		for _, p := range c.Properties {
			if p.Name == propEndOfSupport && parseISODate(p.Value) {
				return true
			}
		}
		return false
	})
}

// notAssessed is the evaluator of an INFO check that one file cannot decide.
func notAssessed(note string) func(d *Doc) Outcome {
	return func(*Doc) Outcome { return Outcome{NotAssessed: true, Note: note} }
}
