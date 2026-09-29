package cmd

import (
	"strings"
	"testing"
)

// Task RD3-6: the coordinator sets a task's level at authorize on a ladder board, as the ladder section
// asks; --level 0 is a level and is sent, a flag not given is not, and the help names the ladder.
func TestAgentAuthorizeSendsTheLevelWhenGiven(t *testing.T) {
	vars := map[string]interface{}{"taskUuid": "t"}
	agentAuthorizeLevel(vars, 0, false)
	if _, ok := vars["level"]; ok {
		t.Error("authorize sent a level it was not given")
	}
	agentAuthorizeLevel(vars, 0, true)
	if vars["level"] != 0 {
		t.Errorf("authorize --level 0 sent %v", vars["level"])
	}
	f := agentTaskAuthorizeCmd.Flag("level")
	if f == nil {
		t.Fatal("agent task authorize has no --level")
	}
	for name, usage := range map[string]string{
		"authorize --level":       f.Usage,
		"register --level":        agentTaskRegisterCmd.Flag("level").Usage,
		"level --level":           agentTaskLevelCmd.Flag("level").Usage,
		"boards task level":       boardsTaskLevelCmd.Short,
		"group set default-level": agentBoardGroupSetCmd.Flag("default-level").Usage,
	} {
		if !strings.Contains(usage, "the board's ladder") {
			t.Errorf("%s does not name the ladder: %q", name, usage)
		}
	}
}
