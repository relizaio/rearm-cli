package cmd

import (
	"strings"
	"testing"

	rearm "github.com/relizaio/rearm-client-go"
)

// A coordinator's release names a role only when one is given (task 4c566d0d).
func TestReleaseHoldVars(t *testing.T) {
	plain := releaseHoldVars("t1", "s1", "", "")
	if _, ok := plain["role"]; ok || plain["taskUuid"] != "t1" || plain["sessionUuid"] != "s1" {
		t.Errorf("without --role, routing picks: %v", plain)
	}
	if got := releaseHoldVars("t1", "s1", " coder ", "")["role"]; got != "coder" {
		t.Errorf("with --role, the trimmed role is sent: %v", got)
	}
	if !strings.Contains(rearm.AgentTaskReleaseHoldProgrammatic_Operation, "role: $role") {
		t.Error("the release operation does not take a role; is rearm-client-go pinned at #38 or later?")
	}
	if f := agentTaskReleaseholdCmd.Flags().Lookup("role"); f == nil {
		t.Error("releasehold has no --role flag")
	}
}

// The coordinator's release carries a note, and escalate a reason (task c0a2134c).
func TestReleaseNoteAndEscalateVars(t *testing.T) {
	if _, ok := releaseHoldVars("t1", "s1", "", "  ")["note"]; ok {
		t.Error("a blank --note sends no note")
	}
	if got := releaseHoldVars("t1", "s1", "", " one more round ")["note"]; got != "one more round" {
		t.Errorf("--note is sent, trimmed: %v", got)
	}
	if agentTaskReleaseholdCmd.Flags().Lookup("note") == nil {
		t.Error("releasehold has no --note flag")
	}
	if !strings.Contains(rearm.AgentTaskReleaseHoldProgrammatic_Operation, "note: $note") {
		t.Error("the release operation does not take a note")
	}
	vars, err := escalateHoldVars("t1", "s1", " recommend accepting T-1 ")
	if err != nil || vars["reason"] != "recommend accepting T-1" || vars["sessionUuid"] != "s1" || vars["taskUuid"] != "t1" {
		t.Errorf("escalate sends the task, the seat and the trimmed reason: %v %v", vars, err)
	}
	if _, err := escalateHoldVars("t1", "s1", " "); err == nil {
		t.Error("an escalation without a reason is refused")
	}
	if agentTaskEscalateCmd.Flags().Lookup("reason") == nil || agentTaskEscalateCmd.PersistentFlags().Lookup("session") == nil {
		t.Error("escalate takes --session and --reason")
	}
	if !strings.Contains(rearm.AgentTaskEscalateHoldProgrammatic_Operation, "agentTaskEscalateHoldProgrammatic(") {
		t.Error("escalate calls the seat's mutation")
	}
}
