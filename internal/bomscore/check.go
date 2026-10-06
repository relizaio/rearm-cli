package bomscore

import (
	"fmt"
	"sort"
)

// Level says whether a check moves a profile's verdict and score.
type Level string

const (
	LevelRequired Level = "REQUIRED"
	LevelInfo     Level = "INFO"
)

// Scope says whether a check reads the document once or every component.
type Scope string

const (
	ScopeDocument  Scope = "DOCUMENT"
	ScopeComponent Scope = "COMPONENT"
)

// Status is derived by evaluate, never set by an evaluator.
type Status string

const (
	StatusPass        Status = "PASS"
	StatusFail        Status = "FAIL"
	StatusNotAssessed Status = "NOT_ASSESSED"
	StatusError       Status = "ERROR"
)

// Verdict is a profile's answer.
type Verdict string

const (
	VerdictReady    Verdict = "READY"
	VerdictNotReady Verdict = "NOT_READY"
	VerdictUnknown  Verdict = "UNKNOWN"
)

// notRepresentableLevel is the level a REQUIRED check takes when the input format has no place
// for its field (SPDX 2.x: signature, generation context, SBOM version, level of support, and the
// end-of-support date before 2.3). Operator point O4: REQUIRED, so every SPDX 2.x file is NOT_READY
// under cisa-2026 and fda. Set it to LevelInfo to leave those checks out of verdict and score for
// such input; nothing else changes.
const notRepresentableLevel = LevelRequired

// maxFailing caps the failing list of a check in the report; passed and total stay exact.
const maxFailing = 20

// Check is one declared check of a profile.
type Check struct {
	ID     string // "<profile>.<slug>", stable, part of the report contract
	Title  string // the field's name exactly as the source document prints it
	Level  Level
	Scope  Scope
	Ref    string // where in the source document
	Remedy string // one line, fixed per check; may be empty
	Eval   func(d *Doc) Outcome
}

// Outcome is what an evaluator returns. DOCUMENT scope: Total is 1.
type Outcome struct {
	Passed, Total    int
	Failing          []string // display ids of failing components, unsorted, complete
	NotAssessed      bool     // the file cannot decide this; INFO checks only
	NotRepresentable bool     // the input format has no place for the field
	Note             string
}

// CheckResult is one check in the report.
type CheckResult struct {
	ID               string   `json:"id"`
	Title            string   `json:"title"`
	Level            Level    `json:"level"`
	Scope            Scope    `json:"scope"`
	Status           Status   `json:"status"`
	Passed           int      `json:"passed"`
	Total            int      `json:"total"`
	Failing          []string `json:"failing"`
	FailingTruncated bool     `json:"failingTruncated"`
	Ref              string   `json:"ref"`
	Remedy           string   `json:"remedy"`
	Note             string   `json:"note"`
}

// CheckError is one evaluator that panicked.
type CheckError struct {
	Check   string `json:"check"`
	Message string `json:"message"`
}

// evaluate runs one check. A panic in Eval is recovered: the check is ERROR and the error is
// returned for the report's errors[].
func evaluate(c Check, d *Doc) (res CheckResult, cerr *CheckError) {
	res = CheckResult{
		ID:      c.ID,
		Title:   c.Title,
		Level:   c.Level,
		Scope:   c.Scope,
		Ref:     c.Ref,
		Remedy:  c.Remedy,
		Failing: []string{},
	}
	defer func() {
		if r := recover(); r != nil {
			res.Status = StatusError
			res.Passed, res.Total = 0, 0
			res.Failing = []string{}
			res.FailingTruncated = false
			res.Note = ""
			cerr = &CheckError{Check: c.ID, Message: fmt.Sprint(r)}
		}
	}()
	o := c.Eval(d)
	res.Passed, res.Total, res.Note = o.Passed, o.Total, o.Note
	if o.NotRepresentable && c.Level == LevelRequired {
		res.Level = notRepresentableLevel
	}
	switch {
	case o.NotAssessed:
		res.Status = StatusNotAssessed
	case o.Total > 0 && o.Passed == o.Total:
		res.Status = StatusPass
	default:
		res.Status = StatusFail
	}
	if c.Scope == ScopeComponent && len(o.Failing) > 0 {
		failing := append([]string(nil), o.Failing...)
		sort.Strings(failing)
		if len(failing) > maxFailing {
			failing = failing[:maxFailing]
			res.FailingTruncated = true
		}
		res.Failing = failing
	}
	return res, nil
}

// verdictOf: NOT_READY when any REQUIRED check fails; else UNKNOWN when any REQUIRED check is in
// ERROR; else READY. INFO checks never change a verdict.
func verdictOf(results []CheckResult) Verdict {
	anyError := false
	for _, r := range results {
		if r.Level != LevelRequired {
			continue
		}
		switch r.Status {
		case StatusFail:
			return VerdictNotReady
		case StatusError:
			anyError = true
		}
	}
	if anyError {
		return VerdictUnknown
	}
	return VerdictReady
}

// Source is the document a profile implements.
type Source struct {
	Title   string `json:"title"`
	Version string `json:"version"`
	Date    string `json:"date"`
	URL     string `json:"url"`
}

// Profile is one ordered list of checks, in the order of its source document. A check belongs to
// exactly one profile; two profiles that check one field declare two checks on one evaluator.
type Profile struct {
	Key    ProfileKey
	Title  string
	Source Source
	Checks []Check
}

// ProfileKey names a profile; the keys are the values of --profile.
type ProfileKey string

const (
	ProfileCISA2026 ProfileKey = "cisa-2026"
	ProfileNTIA2021 ProfileKey = "ntia-2021"
	ProfileFDA      ProfileKey = "fda"
)

// profiles is the registry, in the order the keys are listed to a user.
var profiles = []Profile{profileCISA2026, profileNTIA2021, profileFDA}

// ProfileKeys lists the valid profile keys.
func ProfileKeys() []string {
	keys := make([]string, len(profiles))
	for i, p := range profiles {
		keys[i] = string(p.Key)
	}
	return keys
}

func profileByKey(key ProfileKey) (Profile, bool) {
	for _, p := range profiles {
		if p.Key == key {
			return p, true
		}
	}
	return Profile{}, false
}

// Remedies shared by several checks.
const (
	remedyEnrich  = "run rearm bomutils enrich, or ask the supplier"
	remedySupport = "assess support for the component in ReARM and export with support metadata"
)
