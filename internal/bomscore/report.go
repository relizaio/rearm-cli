package bomscore

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"strconv"
	"strings"
)

// ReportVersion is the version of the report's shape. A new check or profile does not change it;
// removing or renaming a field or a check id does.
const ReportVersion = 1

// EngineName names the engine in the report.
const EngineName = "rearm-cli"

// Report is the versioned result of one Score call. Field order is the JSON key order.
type Report struct {
	ReportVersion int             `json:"reportVersion"`
	Engine        Engine          `json:"engine"`
	Input         Input           `json:"input"`
	Options       Options         `json:"options"`
	Profiles      []ProfileReport `json:"profiles"`
	Structure     StructureReport `json:"structure"`
	Errors        []CheckError    `json:"errors"`
}

type Engine struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Input describes the scored file. Components counts the components scored (after
// Options.SkipFiles), ComponentsSkipped the ones it left out (0 without it).
type Input struct {
	Format            Format        `json:"format"`
	SpecVersion       string        `json:"specVersion"`
	Serialization     Serialization `json:"serialization"`
	Components        int           `json:"components"`
	ComponentsSkipped int           `json:"componentsSkipped"`
	SHA256            string        `json:"sha256"`
}

// Options are the scoring options of one Score call; the report echoes them.
type Options struct {
	// SkipFiles leaves file components (CycloneDX type file, SPDX 2.3 primaryPackagePurpose FILE)
	// out of the component checks.
	SkipFiles bool `json:"skipFiles"`
}

type ProfileReport struct {
	Key     ProfileKey    `json:"key"`
	Title   string        `json:"title"`
	Source  Source        `json:"source"`
	Verdict Verdict       `json:"verdict"`
	Score   *int          `json:"score"`
	Summary Summary       `json:"summary"`
	Checks  []CheckResult `json:"checks"`
}

// Summary counts the checks of both levels by status.
type Summary struct {
	Pass        int `json:"pass"`
	Fail        int `json:"fail"`
	NotAssessed int `json:"notAssessed"`
	Errors      int `json:"errors"`
}

type StructureReport struct {
	Checks []CheckResult `json:"checks"`
}

// UnknownProfileError is returned by Score for a profile key that is not one of ProfileKeys.
type UnknownProfileError struct{ Key string }

func (e *UnknownProfileError) Error() string {
	return fmt.Sprintf("unknown profile %q; valid profiles: %s", e.Key, strings.Join(ProfileKeys(), ", "))
}

// Score scores input under the given profiles, in the order given (a key given twice is scored
// once, at its first position). engineVersion is the CLI version the report names; opts are echoed
// in the report. A refused input returns a *RefusalError and no report; an unknown profile key an
// *UnknownProfileError.
func Score(input []byte, profileKeys []string, engineVersion string, opts Options) (Report, error) {
	var selected []Profile
	seen := map[ProfileKey]bool{}
	for _, raw := range profileKeys {
		k := ProfileKey(raw)
		if seen[k] {
			continue
		}
		p, ok := profileByKey(k)
		if !ok {
			return Report{}, &UnknownProfileError{Key: raw}
		}
		seen[k] = true
		selected = append(selected, p)
	}
	d, err := Load(input)
	if err != nil {
		return Report{}, err
	}
	skipped := 0
	if opts.SkipFiles {
		skipped = d.skipFiles()
	}
	sum := sha256.Sum256(input)
	r := Report{
		ReportVersion: ReportVersion,
		Engine:        Engine{Name: EngineName, Version: engineVersion},
		Input: Input{
			Format:            d.Format,
			SpecVersion:       d.SpecVersion,
			Serialization:     d.Serialization,
			Components:        len(d.Components),
			ComponentsSkipped: skipped,
			SHA256:            hex.EncodeToString(sum[:]),
		},
		Options:   opts,
		Profiles:  []ProfileReport{},
		Structure: StructureReport{Checks: []CheckResult{}},
		Errors:    []CheckError{},
	}
	for _, p := range selected {
		pr, errs := scoreProfile(p, d)
		r.Profiles = append(r.Profiles, pr)
		r.Errors = append(r.Errors, errs...)
	}
	if d.Format == FormatCycloneDX {
		for _, c := range structureChecks {
			res, cerr := evaluate(c, d)
			r.Structure.Checks = append(r.Structure.Checks, res)
			if cerr != nil {
				r.Errors = append(r.Errors, *cerr)
			}
		}
	}
	return r, nil
}

func scoreProfile(p Profile, d *Doc) (ProfileReport, []CheckError) {
	pr := ProfileReport{Key: p.Key, Title: p.Title, Source: p.Source, Checks: []CheckResult{}}
	var errs []CheckError
	for _, c := range p.Checks {
		res, cerr := evaluate(c, d)
		if cerr != nil {
			errs = append(errs, *cerr)
		}
		pr.Checks = append(pr.Checks, res)
		switch res.Status {
		case StatusPass:
			pr.Summary.Pass++
		case StatusFail:
			pr.Summary.Fail++
		case StatusNotAssessed:
			pr.Summary.NotAssessed++
		case StatusError:
			pr.Summary.Errors++
		}
	}
	pr.Verdict = verdictOf(pr.Checks)
	pr.Score = scoreOf(pr.Checks)
	return pr, errs
}

// scoreOf: over the REQUIRED checks with status PASS or FAIL, the mean of passed/total (0 when
// total is 0), times 100, rounded down; nil when there is no such check. Computed exactly.
func scoreOf(results []CheckResult) *int {
	sum := new(big.Rat)
	count := 0
	for _, r := range results {
		if r.Level != LevelRequired || (r.Status != StatusPass && r.Status != StatusFail) {
			continue
		}
		count++
		if r.Total > 0 {
			sum.Add(sum, big.NewRat(int64(r.Passed), int64(r.Total)))
		}
	}
	if count == 0 {
		return nil
	}
	scaled := new(big.Rat).Mul(sum, big.NewRat(100, int64(count)))
	// Operator point O2: rounded down, so 100 means every counted check passed in full. Both
	// operands are non-negative, so the integer quotient is the floor.
	score := int(new(big.Int).Quo(scaled.Num(), scaled.Denom()).Int64())
	return &score
}

// JSON renders the report: two-space indent, HTML escaping off, one trailing newline.
func (r Report) JSON() ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(r); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Text renders one block per profile, then the structure block when it has a failing check. With
// --skip-files an options line comes first; without it nothing is added.
func (r Report) Text() string {
	var b strings.Builder
	if r.Options.SkipFiles {
		fmt.Fprintf(&b, "options: --skip-files, %d file components left out\n", r.Input.ComponentsSkipped)
	}
	for i, p := range r.Profiles {
		if i > 0 {
			b.WriteString("\n")
		}
		score := "n/a"
		if p.Score != nil {
			score = strconv.Itoa(*p.Score)
		}
		fmt.Fprintf(&b, "%s  %s  %s  score %s\n", p.Key, p.Title, p.Verdict, score)
		writeFailedChecks(&b, p.Checks)
		var required, pass, fail, errs int
		for _, c := range p.Checks {
			if c.Level != LevelRequired {
				continue
			}
			required++
			switch c.Status {
			case StatusPass:
				pass++
			case StatusFail:
				fail++
			case StatusError:
				errs++
			}
		}
		fmt.Fprintf(&b, "  %d required: %d pass, %d fail, %d error; %d not assessed\n",
			required, pass, fail, errs, p.Summary.NotAssessed)
	}
	if hasFailed(r.Structure.Checks) {
		if len(r.Profiles) > 0 {
			b.WriteString("\n")
		}
		b.WriteString("structure  BOM structure (informational, never changes a verdict)\n")
		writeFailedChecks(&b, r.Structure.Checks)
	}
	return b.String()
}

func hasFailed(checks []CheckResult) bool {
	for _, c := range checks {
		if c.Status == StatusFail || c.Status == StatusError {
			return true
		}
	}
	return false
}

func writeFailedChecks(b *strings.Builder, checks []CheckResult) {
	for _, c := range checks {
		if c.Status != StatusFail && c.Status != StatusError {
			continue
		}
		if c.Scope == ScopeComponent {
			fmt.Fprintf(b, "  %-4s  %s  %d/%d  (%s)\n", c.Status, c.Title, c.Passed, c.Total, c.Ref)
		} else {
			fmt.Fprintf(b, "  %-4s  %s  (%s)\n", c.Status, c.Title, c.Ref)
		}
		if c.ComponentsSkipped > 0 {
			fmt.Fprintf(b, "        skipped %d components of type %s (not software packages)\n",
				c.ComponentsSkipped, strings.Join(c.SkippedTypes, ", "))
		}
		if len(c.Failing) > 0 {
			missing := strings.Join(c.Failing, ", ")
			if c.FailingTruncated {
				missing += ", ..."
			}
			fmt.Fprintf(b, "        missing in: %s\n", missing)
		}
		if c.Remedy != "" {
			fmt.Fprintf(b, "        remedy: %s\n", c.Remedy)
		}
		if c.Note != "" {
			fmt.Fprintf(b, "        note: %s\n", c.Note)
		}
	}
}
