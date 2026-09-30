package cmd

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"testing"

	rearm "github.com/relizaio/rearm-client-go"
)

// Registered PRs are consulted (task RD4-2): `task signoff --no-code` says a round changed no code, and the
// sign-off, task show and the compact lines say how far each PR's base moved since the round.

func squash(op string) string {
	return strings.Join(strings.Fields(op), " ")
}

func TestSignoffNoCodeSendsTheStatement(t *testing.T) {
	withStateDir(t)
	if err := writeAgentState(&agentSessionState{SessionUuid: "s-1", ClientSessionId: "c-1", CurrentTask: "t-1"}); err != nil {
		t.Fatal(err)
	}
	f := &seenBoard{}
	srv := f.serve(t, seenTask("t-1"))
	defer srv.Close()
	useFake(t, srv)
	t.Cleanup(func() { taskNoCode = false })

	taskSessionUuid, taskOutcome, taskNoCode = "s-1", "PASSED", true
	agentTaskSignoffCmd.Run(agentTaskSignoffCmd, []string{"t-1"})
	if got := f.signed["noCode"]; got != true {
		t.Errorf("--no-code sent noCode %v, want true", got)
	}

	taskSessionUuid, taskOutcome, taskNoCode = "s-1", "PASSED", false
	agentTaskSignoffCmd.Run(agentTaskSignoffCmd, []string{"t-1"})
	if got, ok := f.signed["noCode"]; ok && got != nil {
		t.Errorf("a sign-off without --no-code sent noCode %v", got)
	}
}

func TestSignoffHasTheNoCodeFlag(t *testing.T) {
	fl := agentTaskSignoffCmd.PersistentFlags().Lookup("no-code")
	if fl == nil || fl.Value.Type() != "bool" || fl.DefValue != "false" {
		t.Fatalf("signoff --no-code is missing or not a bool defaulting to false: %+v", fl)
	}
	if !strings.Contains(fl.Usage, "changed no code") || !strings.Contains(fl.Usage, "no linked PR moved since the assignment") {
		t.Errorf("the flag's help does not say what it means: %q", fl.Usage)
	}
}

// prSelections returns the body of every `pullRequests { ... }` selection in a GraphQL operation, braces matched.
func prSelections(op string) []string {
	var out []string
	for _, loc := range regexp.MustCompile(`pullRequests\s*\{`).FindAllStringIndex(op, -1) {
		depth, start := 1, loc[1]
		for i := start; i < len(op); i++ {
			switch op[i] {
			case '{':
				depth++
			case '}':
				depth--
			}
			if depth == 0 {
				out = append(out, op[start:i])
				break
			}
		}
	}
	return out
}

var (
	baseMovedField = regexp.MustCompile(`\bbaseMovedBy\b`)
	noCodeDeclared = regexp.MustCompile(`\$noCode\s*:\s*Boolean\b`)
	noCodePassed   = regexp.MustCompile(`\bnoCode\s*:\s*\$noCode\b`)
)

// readsBaseMovedOnce checks what a task read must carry, wherever the field sits: every PR selection reads
// baseMovedBy exactly once, and nothing outside them reads it.
func readsBaseMovedOnce(t *testing.T, what, op string) {
	t.Helper()
	sels := prSelections(op)
	if len(sels) == 0 {
		t.Errorf("%s selects no pullRequests", what)
	}
	for i, sel := range sels {
		if n := len(baseMovedField.FindAllString(sel, -1)); n != 1 {
			t.Errorf("%s: pullRequests selection %d reads baseMovedBy %d times, want 1: %s", what, i, n, squash(sel))
		}
	}
	if n := len(baseMovedField.FindAllString(op, -1)); n != len(sels) {
		t.Errorf("%s reads baseMovedBy %d times over %d PR selections", what, n, len(sels))
	}
}

func TestTheSignOffDeclaresNoCodeAndReadsBaseMovedBy(t *testing.T) {
	op := signOffOperation()
	if n := len(noCodeDeclared.FindAllString(op, -1)); n != 1 {
		t.Errorf("the sign-off declares $noCode %d times, want 1:\n%s", n, squash(op))
	}
	if n := len(noCodePassed.FindAllString(op, -1)); n != 1 {
		t.Errorf("the sign-off passes noCode %d times, want 1:\n%s", n, squash(op))
	}
	readsBaseMovedOnce(t, "the sign-off", op)
	// Completing is idempotent: the CLI's own operation, completed again, is unchanged.
	if again := completeSignOff(op); again != op {
		t.Errorf("completing a complete sign-off changed it:\n%s", squash(again))
	}
	// Everything else is the pinned operation's: noChange, seenInputs and the task's fields.
	if !strings.Contains(squash(rearm.AgentTaskSignOffProgrammatic_Operation), "noChange: $noChange") {
		t.Error("the pinned client-go sign-off does not send noChange")
	}
}

// A client-go that already carries noCode and baseMovedBy, as rearm-client-go#78 selects them (baseMovedBy after
// mergedDate): the CLI sends its operations as they are.
const completeSignOffOp = `mutation agentTaskSignOffProgrammatic ($taskUuid: ID!, $sessionUuid: ID!, $outcome: AgentSignOffOutcome!, $note: String, $outputs: [ID!], $seenInputs: [ID!], $noChange: Boolean, $noCode: Boolean) {
	agentTaskSignOffProgrammatic(taskUuid: $taskUuid, sessionUuid: $sessionUuid, outcome: $outcome, note: $note, outputs: $outputs, seenInputs: $seenInputs, noChange: $noChange, noCode: $noCode) {
		uuid
		pullRequests {
			url
			state
			targetBranch
			mergedDate
			baseMovedBy
			registered
			head
		}
	}
}`

func TestAnOperationThatCarriesTheFieldsIsSentAsItIs(t *testing.T) {
	if got := completeSignOff(completeSignOffOp); got != completeSignOffOp {
		t.Errorf("completing a sign-off that carries noCode and baseMovedBy changed it:\n%s", squash(got))
	}
	if got := withBaseMovedBy(completeSignOffOp); got != completeSignOffOp {
		t.Errorf("a read that selects baseMovedBy was changed:\n%s", squash(got))
	}
	readsBaseMovedOnce(t, "the complete sign-off", completeSignOffOp)
}

func TestTaskShowReadsBaseMovedBy(t *testing.T) {
	for _, uuids := range [][]string{{"t-1"}, {"t-1", "t-2"}} {
		op, _, _ := taskShowRequest(uuids)
		readsBaseMovedOnce(t, fmt.Sprintf("task show of %d task(s)", len(uuids)), op)
	}
}

func TestBaseMovedLines(t *testing.T) {
	task := map[string]interface{}{"key": "RD4-2", "status": "AWAITING_COORDINATOR", "role": "coder", "pullRequests": []interface{}{
		map[string]interface{}{"url": "https://github.com/o/r/pull/1", "baseMovedBy": float64(3)},
		map[string]interface{}{"url": "https://github.com/o/r/pull/2", "baseMovedBy": float64(1)},
		map[string]interface{}{"url": "https://github.com/o/r/pull/3", "baseMovedBy": float64(0)},
		map[string]interface{}{"url": "https://github.com/o/r/pull/4", "baseMovedBy": nil},
		map[string]interface{}{"url": "https://github.com/o/r/pull/5"},
	}}
	want := []string{
		"base moved: 3 commits since your round: https://github.com/o/r/pull/1",
		"base moved: 1 commit since your round: https://github.com/o/r/pull/2",
	}
	got := baseMovedLines(task)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("lines:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	compact := compactTask(task, nil)
	if !strings.HasSuffix(compact, strings.Join(want, "\n")) {
		t.Errorf("the compact lines do not end with the base-moved lines:\n%s", compact)
	}
}

func TestTaskShowPrintsBaseMovedOnStderr(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stderr
	os.Stderr = w
	printBaseMoved([]interface{}{
		map[string]interface{}{"key": "RD4-2", "pullRequests": []interface{}{
			map[string]interface{}{"url": "https://github.com/o/r/pull/1", "baseMovedBy": float64(2)}}},
		map[string]interface{}{"key": "RD4-3", "pullRequests": []interface{}{
			map[string]interface{}{"url": "https://github.com/o/r/pull/9", "baseMovedBy": nil}}},
	})
	printBaseMoved(map[string]interface{}{"key": "RD4-4", "pullRequests": []interface{}{
		map[string]interface{}{"url": "https://github.com/o/r/pull/4", "baseMovedBy": float64(5)}}})
	_ = w.Close()
	os.Stderr = saved
	out, _ := io.ReadAll(r)
	want := "RD4-2: base moved: 2 commits since your round: https://github.com/o/r/pull/1\n" +
		"RD4-4: base moved: 5 commits since your round: https://github.com/o/r/pull/4\n"
	if string(out) != want {
		t.Errorf("stderr:\n%q\nwant:\n%q", out, want)
	}
}
