package cmd

import (
	"strings"
	"testing"
)

// rearm agent task push: rules the round 1 note states that no test pinned until the round 4 audit (task RD5-3, the
// rule table in notes-4): every closed PR is named when none is open; git runs with GIT_TERMINAL_PROMPT=0.

// Round 1 note, departure 2: when every linked PR on the repository is merged or replaced, the refusal names each.
func TestPushNamesEveryClosedPRWhenNoneIsOpen(t *testing.T) {
	w := newPushWorld(t)
	w.secondPR()
	w.pr(0)["mergedDate"] = "2026-10-01T00:00:00Z"
	w.pr(1)["declaration"] = map[string]any{"outcome": "ABANDONED"}
	out, code := w.run()
	if code != 1 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	wantIn(t, out, "refused: "+pPR+" is merged; "+pPR2+" is declared abandoned: no linked PR for github.com/acme/app is open to push to.",
		"Remedy: a merged or replaced PR takes no more commits")
	if strings.Contains(out, "ran: ") {
		t.Fatalf("ran git:\n%s", out)
	}
}

// Round 1 note: git commands run with GIT_TERMINAL_PROMPT=0, so a remote asking for credentials fails rather than
// waiting on a prompt nobody answers. A git alias prints the variable as git's child sees it.
func TestPushGitNeverPrompts(t *testing.T) {
	w := newPushWorld(t)
	t.Setenv("GIT_TERMINAL_PROMPT", "1")
	out, _, err := pushGit(w.repo, "-c", "alias.prompt=!printenv GIT_TERMINAL_PROMPT", "prompt")
	if err != nil || out != "0" {
		t.Fatalf("GIT_TERMINAL_PROMPT as git sees it: %q (%v)", out, err)
	}
}
