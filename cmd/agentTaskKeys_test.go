package cmd

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// Task keys (task 3d1f9dd7): a task argument may be a key, resolved to the uuid before the call.
func TestAKeyArgumentResolvesToItsUuid(t *testing.T) {
	const uuid = "0b6f0c3e-8a51-4c1e-9d2a-1f3c5e7a9b11"
	saved := taskKeyLookup
	defer func() { taskKeyLookup = saved }()
	var asked []string
	taskKeyLookup = func(key string) (string, error) {
		asked = append(asked, key)
		if key == "RD-42" {
			return uuid, nil
		}
		return "", nil
	}

	if got, err := resolveTaskRef(uuid); err != nil || got != uuid || len(asked) != 0 {
		t.Errorf("a uuid passes through without a read: %q %v %v", got, err, asked)
	}
	if got, err := resolveTaskRef("RD-42"); err != nil || got != uuid {
		t.Errorf("a key resolves to its task: %q %v", got, err)
	}
	if _, err := resolveTaskRef("RD-43"); err == nil || !strings.Contains(err.Error(), "no task has the key RD-43") {
		t.Errorf("an unknown key is refused by name: %v", err)
	}

	// Through a command: the positional argument and the flags it names, before its own run.
	var ranWith []string
	flag := "RD-42"
	deps := []string{"RD-42", uuid}
	c := &cobra.Command{Use: "x", Run: func(cmd *cobra.Command, args []string) { ranWith = args }}
	acceptTaskKeys(c, firstTaskArg, []*string{&flag}, []*[]string{&deps})
	args := []string{"RD-42", "note"}
	if err := c.PreRunE(c, args); err != nil {
		t.Fatal(err)
	}
	c.Run(c, args)
	if ranWith[0] != uuid || ranWith[1] != "note" || flag != uuid || deps[0] != uuid || deps[1] != uuid {
		t.Errorf("the run should see uuids only where tasks are named: %v %q %v", ranWith, flag, deps)
	}
}

// Every command that takes a task argument accepts a key.
func TestTaskCommandsAcceptKeys(t *testing.T) {
	for _, c := range []*cobra.Command{
		agentTaskAssignCmd, agentTaskSignoffCmd, agentTaskShowCmd, agentTaskMergeplanCmd, agentTaskAuthorizeCmd,
		boardsAuthorizeCmd, boardsSignoffCmd, boardsRegisterCmd, agentDocPublishCmd, agentSessionUsageCmd,
	} {
		if c.PreRunE == nil {
			t.Errorf("%s does not resolve task keys", c.CommandPath())
		}
	}
	if !strings.Contains(agentTaskCmd.Long, "RD-42") {
		t.Error("the task command's help does not say task arguments take a key")
	}
}
