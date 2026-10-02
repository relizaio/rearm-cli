package cmd

import (
	"strings"
	"testing"
)

// task verify's code-session fallback (task RD5-4, ARCHITECTURE round 2 §2): with no --code-session, the client ids of
// the current sessions recorded for this repository on every other instance than the board's are compared, from local
// state, with no read of those instances; --code-session replaces the fallback, and its repeated values add up (the
// RD5-1 test TestVerifyTrailersAcceptEveryCodeSessionOfTheTask); nothing recorded elsewhere compares the board
// session's client id, as before.

const (
	vBoardInstance = "https://agent-scully.example"
	vCodeInstance  = "https://reliza.example"
	vCodeInstance2 = "https://ci.example"
)

func verifyFallbackWorld(t *testing.T) *verifyWorld {
	t.Helper()
	w := newVerifyWorld(t)
	prev := rearmUri
	rearmUri = vBoardInstance
	t.Cleanup(func() { rearmUri = prev })
	vGit(t, w.repo, "", "reset", "-q", "--hard", "main")
	return w
}

func recordFor(t *testing.T, repo, instance, uuid, clientId string) {
	t.Helper()
	if err := recordCurrentSession(repo, currentSessionEntry{SessionUuid: uuid, ClientSessionId: clientId, AgentUuid: vAgent,
		Instance: instance}); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyFallsBackToTheOtherInstancesCurrentSession(t *testing.T) {
	w := verifyFallbackWorld(t)
	w.commit("code.txt", trailered("feat: code session", "code-9"))
	w.publishPR()
	recordFor(t, w.repo, vCodeInstance, "aaaaaaaa-1111-4111-8111-000000000009", "code-9")
	// The board instance's own entry is the board session: never a code session.
	recordFor(t, w.repo, vBoardInstance, vSession, "c-1")
	out, code := w.run()
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	wantLine(t, out, checkTrailers, "PASS", "session code-9 (the current session recorded here on "+vCodeInstance+")")
	for _, op := range w.srv.operation {
		if strings.Contains(op, "sessionProgrammatic") {
			t.Fatalf("verify read a session: %s", op)
		}
	}
}

// Every other instance's entry counts, and a commit carrying none of them fails naming the fallback's source.
func TestVerifyFallbackTakesEveryOtherInstance(t *testing.T) {
	w := verifyFallbackWorld(t)
	w.commit("a.txt", trailered("feat: a", "code-9"))
	w.commit("b.txt", trailered("feat: b", "ci-3"))
	w.publishPR()
	recordFor(t, w.repo, vCodeInstance, "aaaaaaaa-1111-4111-8111-000000000009", "code-9")
	out, code := w.run()
	if code != 1 {
		t.Fatalf("exit %d, want 1:\n%s", code, out)
	}
	wantLine(t, out, checkTrailers, "FAIL", "carries ReARM-Agentic-Session ci-3, not code-9",
		"the current session recorded for this repository on "+vCodeInstance)
	recordFor(t, w.repo, vCodeInstance2, "cccccccc-1111-4111-8111-000000000003", "ci-3")
	out, code = w.run()
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	wantLine(t, out, checkTrailers, "PASS", "sessions ci-3, code-9")
}

// --code-session replaces the fallback: the recorded code session is not added to the ids given.
func TestVerifyCodeSessionFlagReplacesTheFallback(t *testing.T) {
	w := verifyFallbackWorld(t)
	sha := w.commit("code.txt", trailered("feat: code session", "code-9"))
	w.publishPR()
	recordFor(t, w.repo, vCodeInstance, "aaaaaaaa-1111-4111-8111-000000000009", "code-9")
	verifyCodeSession = []string{"code-1"}
	out, code := w.run()
	if code != 1 {
		t.Fatalf("exit %d, want 1:\n%s", code, out)
	}
	wantLine(t, out, checkTrailers, "FAIL", shortSha(sha)+" carries ReARM-Agentic-Session code-9, not code-1.")
	verifyCodeSession = []string{"code-1", "code-9"}
	out, code = w.run()
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	if l := line(t, out, checkTrailers); strings.Contains(l, "recorded here") {
		t.Fatalf("the flag's ids are not the fallback's: %s", l)
	}
}

// Nothing recorded on another instance: the board session's client id, as RD5-1 has it.
func TestVerifyWithNoOtherEntryComparesTheBoardSession(t *testing.T) {
	w := verifyFallbackWorld(t)
	w.commit("code.txt", trailered("feat: board session", "c-1"))
	w.publishPR()
	recordFor(t, w.repo, vBoardInstance, vSession, "board-entry")
	out, code := w.run()
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	wantLine(t, out, checkTrailers, "PASS", "session c-1.")
}
