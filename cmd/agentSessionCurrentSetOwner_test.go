package cmd

import (
	"strings"
	"testing"
)

// session current --set takes only a session these credentials' API key opened (task RD5-4, ARCHITECTURE round 3
// §1, tester run 1 T-1). The server answers the session read to more readers than the key that opened it (an admin
// key, a board's coordinator seat, a key with BOARD_READ on a board the session worked), so the read succeeding
// proves nothing: the key that opened the session, which the read answers, is compared with the key the
// credentials act as, the subject of their access token.

const (
	otherKey   = "9e8d7c6b-5a49-4382-a716-5f4e3d2c1b0a"
	otherAgent = "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d"
)

// A session another key opened, answered to these credentials as the server answers it to a board reader: refused,
// naming both keys and the session's agent, and nothing is recorded or kept.
func TestSessionCurrentSetRefusesAnotherKeysSessionItCanRead(t *testing.T) {
	w := newSessWorld(t)
	w.on(w.b)
	w.b.addSessionOf(otherKey, otherSession, "architect-7", otherAgent, "OPEN")
	_, errOut, code := w.run(agentSessionCurrentCmd, "--set", otherSession)
	for _, want := range []string{"session " + otherSession + " belongs to agent " + otherAgent, "opened by API key " + otherKey,
		"not by these credentials' key " + sKey, "nothing was recorded"} {
		if code != 1 || !strings.Contains(errOut, want) {
			t.Fatalf("exit %d, want %q in: %s", code, want, errOut)
		}
	}
	if entryOf(t, w.repoA, w.b.url) != nil {
		t.Fatalf("another key's session was recorded")
	}
	if lookupAgentState(otherSession) != nil || lookupAgentState("architect-7") != nil {
		t.Fatalf("another key's session was kept in local state")
	}
	if got := strings.Join(w.b.names(), ","); got != "AgentSessionCurrentProgrammatic" {
		t.Fatalf("one read, nothing else: %s", got)
	}
}

// The key decides, not the agent's name or the read: a session of the same agent uuid opened by another key is
// refused, and the same session is taken once the credentials are that key.
func TestSessionCurrentSetComparesTheKeyThatOpenedTheSession(t *testing.T) {
	w := newSessWorld(t)
	w.on(w.b)
	w.b.addSessionOf(otherKey, otherSession, "scully-coder-2", sAgent, "OPEN")
	if _, errOut, code := w.run(agentSessionCurrentCmd, "--set", otherSession); code != 1 || !strings.Contains(errOut, "opened by API key "+otherKey) {
		t.Fatalf("same agent, other key: exit %d %s", code, errOut)
	}
	if entryOf(t, w.repoA, w.b.url) != nil {
		t.Fatalf("nothing recorded")
	}
	w.onAs(w.b, otherKey)
	out, errOut, code := w.run(agentSessionCurrentCmd, "--set", otherSession)
	if code != 0 || !strings.Contains(out, otherSession) {
		t.Fatalf("the key's own session: exit %d %s %s", code, out, errOut)
	}
	if e := entryOf(t, w.repoA, w.b.url); e == nil || e.SessionUuid != otherSession || e.ClientSessionId != "scully-coder-2" {
		t.Fatalf("entry: %+v", e)
	}
}

// Credentials that hold no access token (a server without the token endpoint) cannot tell their key, so --set is
// refused, naming --session as the way to go on; nothing is recorded.
func TestSessionCurrentSetRefusesWhenTheCredentialsKeyIsUnknown(t *testing.T) {
	w := newSessWorld(t)
	w.onWithoutToken(w.b)
	w.b.addSession(boardSession, "scully-coder-1", sAgent, "OPEN")
	_, errOut, code := w.run(agentSessionCurrentCmd, "--set", boardSession)
	if code != 1 || !strings.Contains(errOut, "hold no access token") || !strings.Contains(errOut, "pass --session "+boardSession) {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if entryOf(t, w.repoA, w.b.url) != nil {
		t.Fatalf("nothing recorded")
	}
}

// A session the server answers without the key that opened it cannot be told from another key's: refused the same
// way as credentials whose key is unknown, naming --session <uuid> as the way to go on (tester run 2, T-4).
func TestSessionCurrentSetRefusesASessionAnsweredWithoutItsKey(t *testing.T) {
	w := newSessWorld(t)
	w.on(w.b)
	w.b.addSessionOf("", boardSession, "scully-coder-1", sAgent, "OPEN")
	_, errOut, code := w.run(agentSessionCurrentCmd, "--set", boardSession)
	for _, want := range []string{"answered no API key for session " + boardSession,
		"pass --session " + boardSession + " to each verb instead", "nothing was recorded"} {
		if code != 1 || !strings.Contains(errOut, want) {
			t.Fatalf("exit %d, want %q in: %s", code, want, errOut)
		}
	}
	if entryOf(t, w.repoA, w.b.url) != nil {
		t.Fatalf("nothing recorded")
	}
}

// The key the credentials act as is the subject of their access token; anything that is not a JWT with one names no
// key.
func TestTokenSubject(t *testing.T) {
	if got := tokenSubject(testToken(sKey)); got != sKey {
		t.Fatalf("subject: %q", got)
	}
	for _, tok := range []string{"", "opaque-token", "a.b", "a.!!!.c", testToken("")} {
		if got := tokenSubject(tok); got != "" {
			t.Fatalf("%q names %q", tok, got)
		}
	}
}
