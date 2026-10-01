package cmd

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	rearm "github.com/relizaio/rearm-client-go"
)

// Registered PRs are consulted (task RD4-2). On a ReARM where CI registers a task's PRs, a PASSED sign-off by a
// role that pushes code is refused while no linked PR moved since the assignment; `task signoff --no-code` says
// the round changed no code (a note-only round). The task's PR rows carry baseMovedBy, the commits CI reported on
// the PR's target branch since the round began, which the sign-off, task show and the compact lines print as
// "base moved: N commits since your round".
//
// The pinned rearm-client-go predates noCode and baseMovedBy, and a pin moves only to a client-go merge commit, so
// the operations are completed here while the pin lacks them and used as they are once it carries them.

var taskNoCode bool

var (
	signOffVars   = regexp.MustCompile(`\$noChange: Boolean\)`)
	signOffArgs   = regexp.MustCompile(`noChange: \$noChange\)`)
	taskPrsSelect = regexp.MustCompile(`pullRequests\s*\{`)
)

// signOffOperation is the sign-off the CLI sends: it declares noCode and reads each PR's baseMovedBy.
func signOffOperation() string {
	return completeSignOff(rearm.AgentTaskSignOffProgrammatic_Operation)
}

// completeSignOff adds what a sign-off operation lacks of noCode and baseMovedBy, and leaves the rest as it is.
func completeSignOff(op string) string {
	if !strings.Contains(op, "$noCode") {
		op = signOffVars.ReplaceAllString(op, "$$noChange: Boolean, $$noCode: Boolean)")
		op = signOffArgs.ReplaceAllString(op, "noChange: $$noChange, noCode: $$noCode)")
	}
	return withBaseMovedBy(op)
}

// withBaseMovedBy adds baseMovedBy to a task read's PR selection when the operation does not read it already.
func withBaseMovedBy(op string) string {
	if strings.Contains(op, "baseMovedBy") {
		return op
	}
	return taskPrsSelect.ReplaceAllString(op, "pullRequests { baseMovedBy")
}

// baseMovedLines are "base moved: N commits since your round: <url>", one per linked PR whose base moved since the
// task's newest round. A PR with no count (not registered here, or nothing known of its base) or a zero count
// prints nothing: the line says the base moved, never whether the PR still merges.
func baseMovedLines(t map[string]interface{}) []string {
	prs, _ := t["pullRequests"].([]interface{})
	var out []string
	for _, p := range prs {
		pr, _ := p.(map[string]interface{})
		n, ok := pr["baseMovedBy"].(float64)
		if !ok || n < 1 {
			continue
		}
		unit := "commits"
		if n == 1 {
			unit = "commit"
		}
		out = append(out, fmt.Sprintf("base moved: %d %s since your round: %s", int(n), unit, str(pr["url"])))
	}
	return out
}

// printBaseMoved writes the base-moved lines of what task show read to stderr, keyed by task, so the JSON on
// stdout stays whole for a script reading it.
func printBaseMoved(read interface{}) {
	var tasks []interface{}
	switch x := read.(type) {
	case []interface{}:
		tasks = x
	case map[string]interface{}:
		tasks = []interface{}{x}
	}
	for _, e := range tasks {
		t, _ := e.(map[string]interface{})
		if t == nil {
			continue
		}
		for _, line := range baseMovedLines(t) {
			fmt.Fprintln(os.Stderr, orElse(str(t["key"]), str(t["uuid"]))+": "+line)
		}
	}
}
