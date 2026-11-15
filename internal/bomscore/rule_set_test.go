package bomscore

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// fdaRuleSetPins: one line per published rule set, oldest first; sha256 of the rule list text. A
// line is added, never edited: an edited line means a published version changed its text.
var fdaRuleSetPins = []struct{ version, sha256 string }{
	{"1", "2284da03c650f772c431be567aa657796b1d47b86eeedecda1be1118efc3cc89"},
}

const fdaRuleListPath = "../../docs/fda-checker-rules.md"

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func codeList(vs []string) string { return "`" + strings.Join(vs, "`, `") + "`" }

// form is one example value of a date field and whether the engine accepts it. The two tables are
// printed in the rule list and scored one by one in TestFDARuleDateForms, so the list cannot name
// an example the engine decides otherwise.
type form struct {
	value string
	pass  bool
}

var fdaTimestampForms = []form{
	{"2026-10-01T12:00:00Z", true}, {"2026-10-01T12:00:00+02:00", true}, {"2026-10-01T12:00:00.25Z", true}, {"2026-10-01T12:00:00,25Z", true},
	{"2026-10-01", false}, {"2026-10-01T12:00:00", false}, {"2026-10-01T12:00Z", false}, {"2026-10-01 12:00:00Z", false},
	{"2026-10-01t12:00:00Z", false}, {"2026-10-01T12:00:00z", false}, {"2026-10-01T12:00:00+0200", false}, {"2026-10-01T23:59:60Z", false},
}

var fdaEndOfSupportForms = []form{
	{"2030-01-02", true}, {"2030-01-02T03:04:05Z", true}, {"2030-01-02T03:04:05+02:00", true}, {"2030-01-02T03:04:05", true},
	{"2030-01-02T03:04:05.5", true}, {"2030-01-02T03:04:05,5Z", true},
	{"20300102", false}, {"2030-01", false}, {"2030-02-30", false}, {"2030-01-02T03:04", false}, {"2030-01-02 03:04:05", false},
	{"2030-01-02t03:04:05", false}, {"2030-01-02T03:04:05z", false},
}

// formExamples prints a table of forms, in its order.
func formExamples(forms []form) string {
	var yes, no []string
	for _, f := range forms {
		if f.pass {
			yes = append(yes, f.value)
		} else {
			no = append(no, f.value)
		}
	}
	return "Accepted, for example: " + codeList(yes) + ". Refused, for example: " + codeList(no) + "."
}

// ruleListMarkdown renders the published rule list of a profile from its declarations and from the
// three value lists the fda evaluators read.
func ruleListMarkdown(p Profile) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# `%s` checker rules, rule set %s\n\n", p.Key, p.RuleSetVersion)
	b.WriteString("<!-- Generated: go test ./internal/bomscore -run TestFDARuleListDocument -update. Do not edit. -->\n\n")
	fmt.Fprintf(&b, "The checks of the profile `%s` of `rearm bomutils score`, in the order of the report. A JSON report names this list in the `ruleSetVersion` of its `%s` profile and the engine that ran in `engine`; a report whose `%s` profile has no `ruleSetVersion` was written by an engine older than rule set 1.\n\n", p.Key, p.Key, p.Key)
	fmt.Fprintf(&b, "The number is the engine's and numbers this list only: a `ruleSetVersion` anywhere else than in the `%s` profile of an engine report is another number.\n\n", p.Key)
	b.WriteString("This list says what each check reads and when it passes, so that the checker can be compared with what you expect of it. It is not a statement that a document which passes meets the expectations of the source document.\n\n")
	b.WriteString("The rule set number changes whenever this list changes in any way, and whenever a check is changed to decide differently on some document. The same number means the same list.\n\n")
	fmt.Fprintf(&b, "Source: %s, %s, %s\n\n", p.Source.Title, p.Source.Date, p.Source.URL)
	b.WriteString("| Id | Title | Level | Scope | What it checks | Reference |\n|---|---|---|---|---|---|\n")
	for _, c := range p.Checks {
		fmt.Fprintf(&b, "| `%s` | %s | %s | %s | %s | %s |\n", c.ID, c.Title, c.Level, c.Scope, c.What, c.Ref)
	}
	b.WriteString("\n- A `DOCUMENT` check passes or fails on the document. A `COMPONENT` check passes when it judges at least one component and every component it judges has what the check asks for or, where the check takes one, a justification. With no component to judge it fails, at `0/0`: every `COMPONENT` check of a document without components, and the two support checks, with the note `no supportable component`, when they judge none of the components there are.\n")
	b.WriteString("- Level `REQUIRED`: a failing check makes the verdict `NOT_READY`, whatever the other checks say; with none failing, a check that could not be evaluated (`ERROR`) makes it `UNKNOWN`; with neither, the verdict is `READY`. Level `INFO`: reported, never changes the verdict.\n")
	b.WriteString("- A component is every component of the document except the described one: CycloneDX every entry of `components[]`, nested ones included, but not `metadata.component` nor the components nested in it; SPDX every package the document does not describe. With `--skip-files`, file components (CycloneDX type `file`; SPDX 2.3 `primaryPackagePurpose` `FILE`) are left out of every `COMPONENT` check as well, and the report says so in `options.skipFiles`.\n")
	b.WriteString("- A name, version, author, producer, identifier, hash algorithm or hash value counts as missing when, blanks around it ignored, it is empty or is exactly `NOASSERTION` or `NONE`, in upper case.\n")
	b.WriteString("- The level-of-support values, the reason codes, the value `true` of `reliza:support:selfComponent`, the component types and the property names are matched exactly: no trimming, no case folding.\n")
	fmt.Fprintf(&b, "- Timestamp forms. The value, blanks around it ignored, is read by Go's `time.Parse` with the layout `%s`; that is close to RFC 3339 and not the same. %s\n", time.RFC3339, formExamples(fdaTimestampForms))
	fmt.Fprintf(&b, "- End-of-support date forms. The value, blanks around it ignored, is read by Go's `time.Parse` with the layout `%s`, `%s` or `%s`. %s\n", time.DateOnly, time.RFC3339, "2006-01-02T15:04:05", formExamples(fdaEndOfSupportForms))
	fmt.Fprintf(&b, "- Level-of-support values: %s.\n", codeList(sortedKeys(supportLevels)))
	fmt.Fprintf(&b, "- A component the two support checks do not judge neither passes nor fails. In CycloneDX they do not judge a component whose `type` is one of %s (a component without a type is judged), nor a component with a property `reliza:support:selfComponent` of value `true`. In SPDX 2.3, `fda.component.end-of-support` does not judge a package whose `primaryPackagePurpose` is one of %s.\n",
		codeList(sortedKeys(skippedSupportTypes[FormatCycloneDX])), codeList(sortedKeys(skippedSupportTypes[FormatSPDX])))
	fmt.Fprintf(&b, "- Reason codes, each valid on either property: %s.\n", codeList(sortedKeys(justifiedReasons)))
	b.WriteString("- An SPDX 2.x document is always `NOT_READY` under this profile: `fda.component.support-level` fails.\n")
	return b.String()
}

// The published file is the generated text, byte for byte.
func TestFDARuleListDocument(t *testing.T) {
	want := ruleListMarkdown(profileFDA)
	if *update {
		if err := os.WriteFile(filepath.FromSlash(fdaRuleListPath), []byte(want), 0644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(filepath.FromSlash(fdaRuleListPath))
	if err != nil {
		t.Fatalf("%v (run go test ./internal/bomscore -run TestFDARuleListDocument -update)", err)
	}
	if string(got) != want {
		t.Errorf("%s differs from the rules the engine runs (run go test ./internal/bomscore -run TestFDARuleListDocument -update):\n%s", fdaRuleListPath, want)
	}
	for _, c := range profileFDA.Checks {
		if strings.TrimSpace(c.What) == "" || strings.ContainsAny(c.What, "|\n") {
			t.Errorf("%s: What %q, want one non-empty line without |", c.ID, c.What)
		}
	}
}

// A changed rule list needs a new rule-set version.
func TestFDARuleSetPinned(t *testing.T) {
	last := fdaRuleSetPins[len(fdaRuleSetPins)-1]
	sum := sha256.Sum256([]byte(ruleListMarkdown(profileFDA)))
	if got := hex.EncodeToString(sum[:]); last.version != FDARuleSetVersion || last.sha256 != got {
		t.Errorf("rule set %s has the list %s; the last pin is %s %s: raise FDARuleSetVersion and add a pin line, never edit one",
			FDARuleSetVersion, got, last.version, last.sha256)
	}
	seen := map[string]bool{}
	for _, p := range fdaRuleSetPins {
		if seen[p.version] {
			t.Errorf("rule set %s is pinned twice", p.version)
		}
		seen[p.version] = true
	}
}

// Only a profile with a published rule list names one in the report.
func TestRuleSetVersionInReport(t *testing.T) {
	r := scoreOK(t, readFixture(t, "full.cdx.json"))
	got := map[ProfileKey]string{}
	for _, p := range r.Profiles {
		got[p.Key] = p.RuleSetVersion
	}
	want := map[ProfileKey]string{ProfileCISA2026: "", ProfileNTIA2021: "", ProfileFDA: FDARuleSetVersion}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("rule-set versions %v, want %v", got, want)
	}
	js := string(reportJSON(t, r))
	if n := strings.Count(js, `"ruleSetVersion"`); n != 1 || !strings.Contains(js, `"ruleSetVersion": "`+FDARuleSetVersion+`",`) {
		t.Errorf("%d ruleSetVersion keys in the report of three profiles, want one, of value %s", n, FDARuleSetVersion)
	}
}
