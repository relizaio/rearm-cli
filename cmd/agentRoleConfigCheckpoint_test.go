package cmd

import (
	"strings"
	"testing"

	rearm "github.com/relizaio/rearm-client-go"
)

// The design checkpoint (task SPLIT-7, parent SPLIT-5 T-17): roleconfig set sends coordinatorCheckpoint only when
// the flag is given, true or false, and its usage says the seat's set is refused and where a person sets it.
func TestRoleconfigSetSendsTheCheckpointOnlyWhenGiven(t *testing.T) {
	c := agentBoardRoleconfigSetCmd
	t.Cleanup(func() {
		if f := c.Flags().Lookup("coordinator-checkpoint"); f != nil {
			_ = f.Value.Set("false")
			f.Changed = false
		}
		roleName = ""
	})
	// ParseFlags merges the persistent flags and parses, as cobra does before Run.
	if err := c.ParseFlags([]string{"--name", "coder"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := roleconfigSetInput(c, "")["coordinatorCheckpoint"]; ok {
		t.Error("a set without the flag sends coordinatorCheckpoint")
	}
	for _, tc := range []struct {
		arg  string
		want bool
	}{{"--coordinator-checkpoint", true}, {"--coordinator-checkpoint=false", false}} {
		if err := c.ParseFlags([]string{"--name", "coder", tc.arg}); err != nil {
			t.Fatal(err)
		}
		input := roleconfigSetInput(c, "")
		if got, ok := input["coordinatorCheckpoint"]; !ok || got != tc.want {
			t.Errorf("%s sends %v (present %v), want %v", tc.arg, got, ok, tc.want)
		}
		if input["name"] != "coder" {
			t.Errorf("%s lost the role name: %v", tc.arg, input)
		}
	}
	f := c.Flags().Lookup("coordinator-checkpoint")
	for _, want := range []string{"Operator-only", "operator-only message", "board apply", "personal key"} {
		if !strings.Contains(f.Usage, want) {
			t.Errorf("the --coordinator-checkpoint usage does not say %q: %s", want, f.Usage)
		}
	}
}

// The role reads come from the pinned rearm-client-go, so this fails when the pin predates the SPLIT-7 client-go PR.
func TestRoleReadsSelectCoordinatorCheckpoint(t *testing.T) {
	for name, op := range map[string]string{
		"roleconfig list": rearm.AgentTaskRoleConfigsProgrammatic_Operation,
		"board export":    rearm.ExportBoard_Operation,
	} {
		if !strings.Contains(op, "coordinatorCheckpoint") {
			t.Errorf("%s does not select coordinatorCheckpoint; is rearm-client-go pinned at the SPLIT-7 PR or later?", name)
		}
	}
}

// The task reads the CLI prints from select the record, so brief, show and the compact lines can name it.
func TestTaskReadsSelectDesignCheckpoint(t *testing.T) {
	for name, op := range map[string]string{
		"AgentTaskProgrammatic":          rearm.AgentTaskProgrammatic_Operation,
		"AgentTasksByUuidProgrammatic":   rearm.AgentTasksByUuidProgrammatic_Operation,
		"AgentTasksProgrammatic":         rearm.AgentTasksProgrammatic_Operation,
		"AgentTaskNextProgrammatic":      rearm.AgentTaskNextProgrammatic_Operation,
		"AgentTaskAssignProgrammatic":    rearm.AgentTaskAssignProgrammatic_Operation,
		"AgentTaskAuthorizeProgrammatic": rearm.AgentTaskAuthorizeProgrammatic_Operation,
		"AgentTaskSplitProgrammatic":     rearm.AgentTaskSplitProgrammatic_Operation,
	} {
		if !strings.Contains(op, "designCheckpoint") {
			t.Errorf("%s does not select designCheckpoint; is rearm-client-go pinned at the SPLIT-7 PR or later?", name)
		}
	}
}

// The coordinator's help names the checkpoint: the wait wakes on its ALERT, authorize is how it is sent on.
func TestHelpNamesTheDesignCheckpoint(t *testing.T) {
	for name, c := range map[string]struct {
		text string
		want []string
	}{
		"agent wait":           {agentWaitCmd.Long, []string{"design checkpoint", "task authorize", "task split"}},
		"agent task authorize": {agentTaskAuthorizeCmd.Long, []string{"design checkpoint", "SENT_ON", "PENDING"}},
	} {
		text := strings.Join(strings.Fields(c.text), " ")
		for _, w := range c.want {
			if !strings.Contains(text, w) {
				t.Errorf("%s help does not say %q", name, w)
			}
		}
	}
}
