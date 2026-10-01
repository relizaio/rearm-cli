package cmd

import (
	"strings"
	"testing"
)

// Task RD3-6: the coordinator sets a task's work level at authorize on a ladder board, as the ladder section
// asks; --work-level 0 is a level and is sent, a flag not given is not, and the help names the ladder.
func TestAgentAuthorizeSendsTheWorkLevelWhenGiven(t *testing.T) {
	vars := map[string]interface{}{"taskUuid": "t"}
	agentAuthorizeWorkLevel(vars, 0, false)
	if _, ok := vars["workLevel"]; ok {
		t.Error("authorize sent a work level it was not given")
	}
	agentAuthorizeWorkLevel(vars, 0, true)
	if vars["workLevel"] != 0 {
		t.Errorf("authorize --work-level 0 sent %v", vars["workLevel"])
	}
	f := agentTaskAuthorizeCmd.Flag("work-level")
	if f == nil {
		t.Fatal("agent task authorize has no --work-level")
	}
	for name, usage := range map[string]string{
		"authorize --work-level":       f.Usage,
		"register --work-level":        agentTaskRegisterCmd.Flag("work-level").Usage,
		"work-level --work-level":      agentTaskWorkLevelCmd.Flag("work-level").Usage,
		"boards task work-level":       boardsTaskWorkLevelCmd.Short,
		"group set default-work-level": agentBoardGroupSetCmd.Flag("default-work-level").Usage,
	} {
		if !strings.Contains(usage, "the board's ladder") {
			t.Errorf("%s does not name the ladder: %q", name, usage)
		}
	}
}
