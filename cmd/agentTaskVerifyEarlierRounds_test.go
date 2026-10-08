package cmd

import (
	"os"
	"strings"
	"testing"
)

// task verify's trailers check on a returning task: a commit committed before the assignment is an earlier round's,
// checked at that round's sign-off under a session that may since have closed (the 24h idle sweep), so any
// ReARM-Agentic-Session is accepted on it; it still needs the three trailers and the same ReARM-Agent. A commit
// committed after the assignment carries this session's client id or a --code-session id, and with no assignment time
// every commit does, as before.

// newVerifyWorld's hop was assigned on 2020-01-01.
const (
	vBeforeAssigned = "2019-06-01T00:00:00+00:00"
	vAfterAssigned  = "2020-06-01T00:00:00+00:00"
)

// commitAt commits with the given committer date.
func (w *verifyWorld) commitAt(date, file, message string) string {
	w.t.Helper()
	w.t.Setenv("GIT_COMMITTER_DATE", date)
	defer os.Unsetenv("GIT_COMMITTER_DATE")
	return w.commit(file, message)
}

// earlierRoundWorld: the feature branch starts from main again.
func earlierRoundWorld(t *testing.T) *verifyWorld {
	t.Helper()
	w := newVerifyWorld(t)
	vGit(t, w.repo, "", "reset", "-q", "--hard", "main")
	return w
}

func TestVerifyTrailersAcceptAnEarlierRoundsSession(t *testing.T) {
	w := earlierRoundWorld(t)
	w.commitAt(vBeforeAssigned, "r1.txt", trailered("feat: round 1", "tea-coder-1790972008"))
	w.commit("r2.txt", trailered("fix: round 2", "c-1"))
	w.publishPR()
	out, code := w.run()
	if code != 0 {
		t.Fatalf("exit %d, want 0:\n%s", code, out)
	}
	wantLine(t, out, checkTrailers, "PASS", "2 commit(s)", "session c-1;",
		"1 earlier-round commit(s), committed before your assignment, accepted with session tea-coder-1790972008.")
}

func TestVerifyTrailersFailAnotherSessionCommittedAfterTheAssignment(t *testing.T) {
	w := earlierRoundWorld(t)
	sha := w.commitAt(vAfterAssigned, "r2.txt", trailered("fix: round 2", "tea-coder-1790972008"))
	w.publishPR()
	out, code := w.run()
	if code != 1 {
		t.Fatalf("exit %d, want 1:\n%s", code, out)
	}
	wantLine(t, out, checkTrailers, "FAIL", shortSha(sha)+" carries ReARM-Agentic-Session tea-coder-1790972008, not c-1",
		"commits from before your assignment are accepted under any session; when this round's commits carry a code session")

	// With --code-session given, the same commit fails, and the remedy says the same.
	verifyCodeSession = []string{"code-9"}
	out, _ = w.run()
	wantLine(t, out, checkTrailers, "FAIL", shortSha(sha)+" carries ReARM-Agentic-Session tea-coder-1790972008, not code-9",
		"commits from before your assignment are accepted under any session, so only this round's commits", "repeat --code-session for each")
}

func TestVerifyTrailersStillNeedTheThreeOnAnEarlierRound(t *testing.T) {
	w := earlierRoundWorld(t)
	sha := w.commitAt(vBeforeAssigned, "r1.txt", "feat: round 1\n\nReARM-Agentic-Session: old-1\nReARM-Agent: "+vAgent+"\n")
	w.publishPR()
	out, code := w.run()
	if code != 1 {
		t.Fatalf("exit %d, want 1:\n%s", code, out)
	}
	wantLine(t, out, checkTrailers, "FAIL", shortSha(sha)+" lacks Co-Authored-By in its final paragraph")
}

func TestVerifyTrailersStillNeedTheSameAgentOnAnEarlierRound(t *testing.T) {
	w := earlierRoundWorld(t)
	otherAgent := "00000000-0000-4000-8000-000000000000"
	sha := w.commitAt(vBeforeAssigned, "r1.txt", "feat: round 1\n\nReARM-Agentic-Session: old-1\nReARM-Agent: "+otherAgent+
		"\nCo-Authored-By: Claude <noreply@anthropic.com>\n")
	head := w.commit("r2.txt", trailered("fix: round 2", "c-1"))
	w.publishPR()
	out, code := w.run()
	if code != 1 {
		t.Fatalf("exit %d, want 1:\n%s", code, out)
	}
	wantLine(t, out, checkTrailers, "FAIL", "ReARM-Agent differs between commits: ", shortSha(sha)+" has "+otherAgent,
		shortSha(head)+" has "+vAgent)
	if l := line(t, out, checkTrailers); strings.Contains(l, "carries") {
		t.Fatalf("the earlier round's session is not a problem: %s", l)
	}
}

func TestVerifyTrailersWithoutAnAssignmentTimeCompareEveryCommit(t *testing.T) {
	w := earlierRoundWorld(t)
	w.assignment()["assignedAt"] = ""
	sha := w.commitAt(vBeforeAssigned, "r1.txt", trailered("feat: round 1", "tea-coder-1790972008"))
	w.publishPR()
	out, code := w.run()
	if code != 1 {
		t.Fatalf("exit %d, want 1:\n%s", code, out)
	}
	wantLine(t, out, checkTrailers, "FAIL", shortSha(sha)+" carries ReARM-Agentic-Session tea-coder-1790972008, not c-1",
		"when your code commits carry a code session on another instance, pass --code-session <its client id>")
	if l := line(t, out, checkTrailers); strings.Contains(l, "before your assignment") {
		t.Fatalf("no assignment time, no earlier rounds: %s", l)
	}
}
