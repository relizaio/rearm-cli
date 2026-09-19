package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func withStateDir(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
}

func TestStateIsFiledUnderBothSessionIds(t *testing.T) {
	// The agent knows its client id; a hook payload carries only Claude's. Filing under both is
	// what lets the hook resolve the ReARM session without a server call on every turn.
	withStateDir(t)
	st := &agentSessionState{SessionUuid: "u-1", ClientSessionId: "client-1", ExternalSessionId: "claude-1"}
	if err := writeAgentState(st); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"client-1", "claude-1"} {
		got, err := readAgentState(id)
		if err != nil || got == nil {
			t.Fatalf("no state under %q: %v", id, err)
		}
		if got.SessionUuid != "u-1" {
			t.Errorf("wrong session under %q: %+v", id, got)
		}
	}
}

func TestAMissingStateFileIsNotAnError(t *testing.T) {
	// A developer running Claude Code outside any ReARM session hits this on every single turn.
	withStateDir(t)
	st, err := readAgentState("never-seen")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if st != nil {
		t.Errorf("expected nil state, got %+v", st)
	}
}

func TestASessionIdCannotEscapeTheStateDirectory(t *testing.T) {
	// Client session ids are agent-chosen strings, and the CLI writes a file named after one.
	withStateDir(t)
	st := &agentSessionState{SessionUuid: "u-1", ClientSessionId: "../../../tmp/escaped"}
	if err := writeAgentState(st); err != nil {
		t.Fatal(err)
	}
	dir, _ := agentStateDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one file in the state dir, got %d", len(entries))
	}
	// What matters is that the name is a single directory entry that resolves INSIDE the state
	// directory -- not that it avoids the characters ".." as a substring. ".._.._tmp_escaped.json"
	// looks alarming and is perfectly safe: it has no separator, so it cannot traverse anywhere.
	name := entries[0].Name()
	if strings.ContainsAny(name, "/\\") {
		t.Errorf("sanitised name still carries a path separator: %q", name)
	}
	resolved := filepath.Clean(filepath.Join(dir, name))
	if !strings.HasPrefix(resolved, filepath.Clean(dir)+string(os.PathSeparator)) {
		t.Errorf("state file resolves outside its directory: %q", resolved)
	}
	if _, err := os.Stat(filepath.Join(dir, "..", "..", "..", "tmp", "escaped.json")); err == nil {
		t.Error("a file was written outside the state directory")
	}
}

func TestClearingTheTaskOnlyAppliesToTheTaskBeingClosed(t *testing.T) {
	// Signing off some other task must not detach the one this session still holds -- the rest of
	// the session's usage would go unattributed and nothing would show it was wrong.
	withStateDir(t)
	st := &agentSessionState{SessionUuid: "u-1", ClientSessionId: "client-1", CurrentTask: "task-A"}
	if err := writeAgentState(st); err != nil {
		t.Fatal(err)
	}
	clearCurrentTask("u-1", "task-B")
	got, _ := readAgentState("client-1")
	if got.CurrentTask != "task-A" {
		t.Errorf("signing off task-B cleared task-A: %+v", got)
	}
	clearCurrentTask("u-1", "task-A")
	got, _ = readAgentState("client-1")
	if got.CurrentTask != "" {
		t.Errorf("signing off task-A should have cleared it: %+v", got)
	}
}

func TestStateIsFoundByItsSessionUuid(t *testing.T) {
	withStateDir(t)
	writeAgentState(&agentSessionState{SessionUuid: "u-1", ClientSessionId: "a"})
	writeAgentState(&agentSessionState{SessionUuid: "u-2", ClientSessionId: "b"})
	got := findStateBySessionUuid("u-2")
	if got == nil || got.ClientSessionId != "b" {
		t.Errorf("reverse lookup failed: %+v", got)
	}
	if findStateBySessionUuid("u-3") != nil {
		t.Error("expected nil for an unknown uuid")
	}
}

func TestRemovingStateDropsBothNames(t *testing.T) {
	withStateDir(t)
	st := &agentSessionState{SessionUuid: "u-1", ClientSessionId: "client-1", ExternalSessionId: "claude-1"}
	writeAgentState(st)
	removeAgentState(st)
	for _, id := range []string{"client-1", "claude-1"} {
		if got, _ := readAgentState(id); got != nil {
			t.Errorf("state under %q survived removal", id)
		}
	}
}

func TestUpdatingAbsentStateIsANoOpNotACreation(t *testing.T) {
	// `task assign` runs this. An agent in CI with no local state must not start failing, and must
	// not have a state file conjured for a session it never initialised here.
	withStateDir(t)
	updateAgentState("nothing-here", func(s *agentSessionState) { s.CurrentTask = "t" })
	dir, _ := agentStateDir()
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
		t.Errorf("a state file was created for an unknown session: %+v", entries)
	}
}
