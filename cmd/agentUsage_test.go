package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestChunkSequencesAreDistinctAndEndAtTheRealOffset(t *testing.T) {
	// The chunk sequences are derived from one end offset because chunking splits lines, not byte
	// ranges. They must stay distinct and monotonic, and the LAST one must be the real end of the
	// parsed range -- that value is what LastSeq becomes once every chunk is confirmed.
	lines := []usageLine{{Model: "a", Requests: 300}, {Model: "b", Requests: 300}, {Model: "c", Requests: 300}}
	chunks := chunkLines(lines, 500)
	end := int64(70_000)
	seen := map[int64]bool{}
	var last int64
	for i := range chunks {
		seq := end - int64(len(chunks)-1-i)
		if seen[seq] {
			t.Fatalf("chunk %d reused sequence %d", i, seq)
		}
		if i > 0 && seq <= last {
			t.Fatalf("sequences are not monotonic: %d after %d", seq, last)
		}
		seen[seq] = true
		last = seq
	}
	if last != end {
		t.Errorf("the final chunk must carry the true end offset, got %d want %d", last, end)
	}
}

func TestAdoptionBindsTheOnlyUnboundSession(t *testing.T) {
	// Without this the hook finds no state, exits 0 by design, and the session reports nothing for
	// its whole life with no signal anywhere -- the worst failure shape available.
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if err := writeAgentState(&agentSessionState{SessionUuid: "u-1", ClientSessionId: "c-1"}); err != nil {
		t.Fatal(err)
	}
	got := adoptClaudeSession("claude-xyz")
	if got == nil || got.SessionUuid != "u-1" {
		t.Fatalf("expected adoption of the single unbound session, got %+v", got)
	}
	// And the binding is persisted, so the next turn resolves directly.
	reread, err := readAgentState("claude-xyz")
	if err != nil || reread == nil || reread.SessionUuid != "u-1" {
		t.Errorf("binding was not written: %+v (%v)", reread, err)
	}
}

func TestAdoptionRefusesToGuessBetweenTwoUnboundSessions(t *testing.T) {
	// Binding usage to the wrong session is worse than not binding it: the number is then wrong
	// somewhere it looks right, and nothing later corrects it.
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	writeAgentState(&agentSessionState{SessionUuid: "u-1", ClientSessionId: "c-1"})
	writeAgentState(&agentSessionState{SessionUuid: "u-2", ClientSessionId: "c-2"})
	if got := adoptClaudeSession("claude-xyz"); got != nil {
		t.Errorf("expected no adoption when two sessions are unbound, got %+v", got)
	}
}

func TestAdoptionIgnoresSessionsAlreadyBound(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	writeAgentState(&agentSessionState{SessionUuid: "u-1", ClientSessionId: "c-1", ExternalSessionId: "claude-old"})
	if got := adoptClaudeSession("claude-new"); got != nil {
		t.Errorf("a session already bound to another Claude id must not be re-bound, got %+v", got)
	}
}

func TestTruncatedTranscriptIsFlaggedSoTheCallerCanRefuse(t *testing.T) {
	// A re-read from zero produces a sequence below the server's high-water mark, which it
	// refuses. Left unflagged, that repeats every turn for the life of the session.
	dir := t.TempDir()
	path := filepath.Join(dir, "t.jsonl")
	os.WriteFile(path, []byte(assistantRow("m1", "claude-opus-5", "text", 0, 10, 5, 0, 0)+"\n"), 0o600)

	d, err := parseClaudeTranscript(path, 999_999)
	if err != nil {
		t.Fatal(err)
	}
	if !d.Truncated {
		t.Error("an offset past the end must be flagged as a truncation/reuse")
	}
	fresh, _ := parseClaudeTranscript(path, 0)
	if fresh.Truncated {
		t.Error("an ordinary read must not be flagged")
	}
}

func TestEffortIsReadFromTheVariableClaudeCodeActuallySets(t *testing.T) {
	// Verified against a running Claude Code: the variable is CLAUDE_EFFORT. The first guess,
	// CLAUDE_CODE_EFFORT_LEVEL, is unset in practice, so relying on it silently dropped the field.
	t.Setenv("CLAUDE_CODE_EFFORT_LEVEL", "")
	t.Setenv("CLAUDE_EFFORT", "high")
	if got := claudeReasoningFromEnv(); got != "high" {
		t.Errorf("expected effort from CLAUDE_EFFORT, got %q", got)
	}
}

func TestTheStaleTranscriptWarningIsPrintedOnlyOnce(t *testing.T) {
	// The condition holds for the whole life of the session, so without the recorded flag this
	// paragraph lands on the agent's stderr on every single turn.
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	st := &agentSessionState{SessionUuid: "u-1", ClientSessionId: "c-1", LastSeq: 999}
	if err := writeAgentState(st); err != nil {
		t.Fatal(err)
	}
	reportDelta(st, &usageDelta{Truncated: true}, "TRANSCRIPT", false)
	if !st.TruncationWarned {
		t.Fatal("the first encounter should record that it warned")
	}
	// Persisted, so a later process does not warn again.
	reread, err := readAgentState("c-1")
	if err != nil || reread == nil || !reread.TruncationWarned {
		t.Errorf("the warning flag was not persisted: %+v (%v)", reread, err)
	}
}

func TestAllowanceWarningOnlyWhenTheHopIsOver(t *testing.T) {
	over := map[string]interface{}{"task": "t-1", "hopAllowanceMicros": float64(1000), "hopSpentMicros": float64(5000)}
	want := "hop allowance exceeded: spent 5000 of 1000 micros on task t-1 — consider returning the task"
	if got := allowanceWarning(over); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	// A server that quotes longs, and one that decodes with UseNumber, read the same.
	quoted := map[string]interface{}{"task": "t-1", "hopAllowanceMicros": "1000", "hopSpentMicros": json.Number("5000")}
	if got := allowanceWarning(quoted); got != want {
		t.Errorf("quoted longs: got %q", got)
	}
	for name, ack := range map[string]map[string]interface{}{
		"under":        {"hopAllowanceMicros": float64(1000), "hopSpentMicros": float64(400)},
		"exactly":      {"hopAllowanceMicros": float64(1000), "hopSpentMicros": float64(1000)},
		"no allowance": {"hopAllowanceMicros": nil, "hopSpentMicros": nil},
		"older server": {"accepted": float64(1)},
		"no ack":       nil,
	} {
		if got := allowanceWarning(ack); got != "" {
			t.Errorf("%s: want no warning, got %q", name, got)
		}
	}
}
