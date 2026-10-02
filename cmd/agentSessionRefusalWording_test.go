package cmd

import (
	"os"
	"strings"
	"testing"
)

// The words of session open, close --final and current [--set] when they refuse or stop (task RD5-4, tester run 2
// T-4 and the coder's wording scan): every refusal whose wording notes-1 or notes-2 states is asserted here, so a
// change that drops the remedy or the reason fails a test by name. The scan's mutants are W1 to W16 in
// impl/RD5-4/mutate-3.py; the ones that survived before this file (W1, W2, W5, W7, W8, W10 to W13) are pinned here.

// wantAll fails unless the exit is want and every phrase is in the output.
func wantAll(t *testing.T, what string, code, want int, out string, phrases ...string) {
	t.Helper()
	for _, p := range phrases {
		if code != want || !strings.Contains(out, p) {
			t.Fatalf("%s: exit %d (want %d), want %q in: %s", what, code, want, p, out)
		}
	}
}

// --set refusals say why (W1, W2): another key's session, only a session this key opened can be current; a session
// that is not open, only an open session can be current.
func TestSessionCurrentSetRefusalsSayWhichSessionCanBeCurrent(t *testing.T) {
	w := newSessWorld(t)
	w.on(w.b)
	w.b.addSessionOf(otherKey, otherSession, "architect-7", otherAgent, "OPEN")
	_, errOut, code := w.run(agentSessionCurrentCmd, "--set", otherSession)
	wantAll(t, "another key's session", code, 1, errOut,
		"not by these credentials' key "+sKey+"; only a session this key opened can be current; nothing was recorded")
	w.b.addSession(boardSession, "scully-coder-1", sAgent, "CLOSED")
	_, errOut, code = w.run(agentSessionCurrentCmd, "--set", boardSession)
	wantAll(t, "a closed session", code, 1, errOut,
		"session "+boardSession+" is CLOSED on "+instanceKey(w.b.url)+"; only an open session can be current; nothing was recorded")
	if entryOf(t, w.repoA, w.b.url) != nil {
		t.Fatalf("nothing recorded")
	}
}

// --set takes a client id this host's state knows and records the session it names (W5, notes-1 departure 8).
func TestSessionCurrentSetTakesAClientIdThisHostKnows(t *testing.T) {
	w := newSessWorld(t)
	w.on(w.b)
	w.b.addSession(boardSession, "scully-coder-1", sAgent, "OPEN")
	if err := writeAgentState(&agentSessionState{SessionUuid: boardSession, ClientSessionId: "scully-coder-1", AgentUuid: sAgent}); err != nil {
		t.Fatal(err)
	}
	out, errOut, code := w.run(agentSessionCurrentCmd, "--set", "scully-coder-1")
	wantAll(t, "--set <client id>", code, 0, out, "session "+boardSession+" (client id scully-coder-1) is the current session")
	if e := entryOf(t, w.repoA, w.b.url); e == nil || e.SessionUuid != boardSession {
		t.Fatalf("the client id's session is recorded: %+v (%s)", e, errOut)
	}
}

// current with no entry names both ways to get one (W7): session open and session current --set.
func TestSessionCurrentWithoutAnEntryNamesOpenAndSet(t *testing.T) {
	w := newSessWorld(t)
	w.on(w.a)
	_, errOut, code := w.run(agentSessionCurrentCmd)
	wantAll(t, "current, no entry", code, 1, errOut, "rearm: no current session for "+w.repoA+" on "+instanceKey(w.a.url)+
		"; open one with rearm agent session open, or record one with rearm agent session current --set <session-uuid>")
}

// open names the step that stopped it (W12, W13): step 0 is a local check, step 2 recording the session, which
// names the --set that records it by hand; a failed ORIENTATION upload says the session stays open and current (W8).
func TestSessionOpenNamesEveryStepThatStopsIt(t *testing.T) {
	w := newSessWorld(t)
	w.on(w.a)
	_, errOut, code := w.open("--orientation", writeFile(t, t.TempDir(), "empty.md", "\n"))
	wantAll(t, "step 0", code, 1, errOut, "rearm: session open: step 0 (read the ORIENTATION report) failed: ", "; nothing was opened")
	rearmUri = ""
	_, errOut, code = w.open()
	wantAll(t, "step 0, no instance", code, 1, errOut, "rearm: session open: step 0 (find the instance) failed: the credentials name no instance")
	w.on(w.a)

	w.a.failUpload = true
	out, errOut, code := w.open("--orientation-text", "plan")
	uuid := envOf(t, out)["REARM_SESSION"]
	wantAll(t, "step 3", code, 1, errOut, "rearm: session open: step 3 (ORIENTATION report) failed: ",
		"; session "+uuid+" stays open and current; file the report with: rearm agent session add-artifact "+uuid)
	w.a.failUpload = false

	// Recording fails when the repository's current-session file cannot be written: a directory where its temporary
	// file goes.
	path, err := currentSessionsPath(w.repoB)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(path+".tmp", 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(w.repoB)
	out, errOut, code = w.open()
	uuid = envOf(t, out)["REARM_SESSION"]
	wantAll(t, "step 2", code, 1, errOut, "rearm: session open: step 2 (record the current session) failed: ",
		"; session "+uuid+" is open: record it with rearm agent session current --set "+uuid)
	if _, err := os.Stat(path); err == nil {
		t.Fatalf("nothing was recorded for %s", w.repoB)
	}
}

// close --final says how to finish by hand (W10, W11): a failed FINAL upload prints the add-artifact line that
// files it, and a refused close after the report is filed prints the close to run.
func TestSessionCloseFinalSaysHowToFinishByHand(t *testing.T) {
	w := newSessWorld(t)
	w.on(w.a)
	out, _, _ := w.open()
	uuid := envOf(t, out)["REARM_SESSION"]
	final := writeFile(t, t.TempDir(), "final.md", "done\n")
	w.a.failUpload = true
	_, errOut, code := w.run(agentSessionCloseCmd, "--final", final)
	wantAll(t, "a failed FINAL upload", code, 1, errOut, "session "+uuid+" was not closed; file the report with: rearm agent session add-artifact "+
		uuid+" --file "+final+" --type AGENTIC_REPORT --display-id final --tag agenticPhase=FINAL")
	w.a.failUpload, w.a.failClose = false, true
	_, errOut, code = w.run(agentSessionCloseCmd, "--final", final)
	wantAll(t, "a refused close", code, 1, errOut, "the FINAL report is filed; close with: rearm agent session close "+uuid)
}
