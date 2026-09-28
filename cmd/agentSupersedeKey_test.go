package cmd

import (
	"strings"
	"testing"
)

// supersedepr takes a task key as every other task verb does (task RD3-18): RD3-4 resolves to its uuid before
// the call, and a uuid passes through untouched.
func TestSupersedeprResolvesATaskKey(t *testing.T) {
	const uuid = "a73f6dc7-6f23-4e6a-a8bd-8a101ce3eafd"
	saved := taskKeyLookup
	defer func() { taskKeyLookup = saved }()
	var asked []string
	taskKeyLookup = func(key string) (string, error) {
		asked = append(asked, key)
		if key == "RD3-4" {
			return uuid, nil
		}
		return "", nil
	}
	if agentTaskSupersedeCmd.PreRunE == nil {
		t.Fatal("supersedepr does not resolve task keys")
	}
	args := []string{"RD3-4"}
	if err := agentTaskSupersedeCmd.PreRunE(agentTaskSupersedeCmd, args); err != nil || args[0] != uuid {
		t.Errorf("the key should resolve to %s: %v %v", uuid, args, err)
	}
	asked = nil
	args = []string{uuid}
	if err := agentTaskSupersedeCmd.PreRunE(agentTaskSupersedeCmd, args); err != nil || args[0] != uuid || len(asked) != 0 {
		t.Errorf("a uuid passes through without a read: %v %v %v", args, err, asked)
	}
	if !strings.Contains(agentTaskSupersedeCmd.Use, "<task-key-or-uuid>") {
		t.Errorf("the usage says a key is taken: %s", agentTaskSupersedeCmd.Use)
	}
}
