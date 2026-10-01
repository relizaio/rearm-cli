package cmd

import (
	"strings"
	"testing"

	rearm "github.com/relizaio/rearm-client-go"
)

// A coordinator's hold lift names a role only when one is given (task 4c566d0d).
func TestLiftHoldVars(t *testing.T) {
	plain := liftHoldVars("t1", "s1", "", "")
	if _, ok := plain["role"]; ok || plain["taskUuid"] != "t1" || plain["sessionUuid"] != "s1" {
		t.Errorf("without --role, routing picks: %v", plain)
	}
	if got := liftHoldVars("t1", "s1", " coder ", "")["role"]; got != "coder" {
		t.Errorf("with --role, the trimmed role is sent: %v", got)
	}
	if !strings.Contains(rearm.AgentTaskLiftHoldProgrammatic_Operation, "role: $role") {
		t.Error("the lift operation does not take a role")
	}
	if f := agentTaskLiftholdCmd.Flags().Lookup("role"); f == nil {
		t.Error("lifthold has no --role flag")
	}
}

// The coordinator's hold lift carries a note, and escalate a reason (task c0a2134c).
func TestLiftNoteAndEscalateVars(t *testing.T) {
	if _, ok := liftHoldVars("t1", "s1", "", "  ")["note"]; ok {
		t.Error("a blank --note sends no note")
	}
	if got := liftHoldVars("t1", "s1", "", " one more round ")["note"]; got != "one more round" {
		t.Errorf("--note is sent, trimmed: %v", got)
	}
	if agentTaskLiftholdCmd.Flags().Lookup("note") == nil {
		t.Error("lifthold has no --note flag")
	}
	if !strings.Contains(rearm.AgentTaskLiftHoldProgrammatic_Operation, "note: $note") {
		t.Error("the lift operation does not take a note")
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
