package cmd

import (
	"strings"
	"testing"

	rearm "github.com/relizaio/rearm-client-go"
)

// A coordinator's release names a role only when one is given (task 4c566d0d).
func TestReleaseHoldVars(t *testing.T) {
	plain := releaseHoldVars("t1", "s1", "")
	if _, ok := plain["role"]; ok || plain["taskUuid"] != "t1" || plain["sessionUuid"] != "s1" {
		t.Errorf("without --role, routing picks: %v", plain)
	}
	if got := releaseHoldVars("t1", "s1", " coder ")["role"]; got != "coder" {
		t.Errorf("with --role, the trimmed role is sent: %v", got)
	}
	if !strings.Contains(rearm.AgentTaskReleaseHoldProgrammatic_Operation, "role: $role") {
		t.Error("the release operation does not take a role; is rearm-client-go pinned at #38 or later?")
	}
	if f := agentTaskReleaseholdCmd.Flags().Lookup("role"); f == nil {
		t.Error("releasehold has no --role flag")
	}
}
