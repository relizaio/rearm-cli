package cmd

import (
	"strings"
	"testing"

	rearm "github.com/relizaio/rearm-client-go"
	"github.com/spf13/cobra"
)

// An unlink's variables, its required flags, and its place among the task verbs (task t20261010-033523-18839).
func TestUnlinkVarsAndFlags(t *testing.T) {
	v, err := unlinkVars("t1", "s1", " https://github.com/a/b/pull/1 ", " linked by mistake ")
	if err != nil || v["taskUuid"] != "t1" || v["sessionUuid"] != "s1" || v["prUrl"] != "https://github.com/a/b/pull/1" ||
		v["note"] != "linked by mistake" {
		t.Errorf("an unlink: %v %v", v, err)
	}
	for k := range v {
		if !strings.Contains(rearm.AgentTaskUnlinkPrProgrammatic_Operation, "$"+k+":") {
			t.Errorf("the operation declares no $%s", k)
		}
	}
	if v, _ := unlinkVars("t1", "s1", "p", "  "); v["note"] != nil {
		t.Errorf("a blank note is not sent: %v", v)
	}
	for _, bad := range []string{"", "  "} {
		if _, err := unlinkVars("t1", "s1", bad, ""); err == nil || !strings.Contains(err.Error(), "--pr") {
			t.Errorf("%q should be refused: %v", bad, err)
		}
	}
	for f, required := range map[string]bool{"session": true, "pr": true, "note": false} {
		flag := agentTaskUnlinkCmd.Flags().Lookup(f)
		if flag == nil {
			t.Errorf("unlinkpr takes --%s", f)
			continue
		}
		if _, got := flag.Annotations[cobra.BashCompOneRequiredFlag]; got != required {
			t.Errorf("--%s required: %v, want %v", f, got, required)
		}
	}
	found := false
	for _, c := range agentTaskCmd.Commands() {
		found = found || c.Name() == "unlinkpr"
	}
	if !found {
		t.Error("unlinkpr is not a task verb")
	}
	// A session's unlink is refused on a task parked for the operator, like the other verbs; linkpr names its reverse.
	if !strings.Contains(strings.Join(strings.Fields(agentTaskHoldCmd.Long), " "), "supersedepr, unlinkpr, complete") {
		t.Error("the parked-task verb list does not name unlinkpr")
	}
	if !strings.Contains(strings.Join(strings.Fields(agentTaskLinkprCmd.Long), " "), "The reverse is task unlinkpr") {
		t.Error("linkpr does not name its reverse")
	}
}

// unlinkpr takes a task key as every other task verb does: RD3-4 resolves to its uuid before the call, and a
// uuid passes through untouched.
func TestUnlinkprResolvesATaskKey(t *testing.T) {
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
	if agentTaskUnlinkCmd.PreRunE == nil {
		t.Fatal("unlinkpr does not resolve task keys")
	}
	args := []string{"RD3-4"}
	if err := agentTaskUnlinkCmd.PreRunE(agentTaskUnlinkCmd, args); err != nil || args[0] != uuid {
		t.Errorf("the key should resolve to %s: %v %v", uuid, args, err)
	}
	asked = nil
	args = []string{uuid}
	if err := agentTaskUnlinkCmd.PreRunE(agentTaskUnlinkCmd, args); err != nil || args[0] != uuid || len(asked) != 0 {
		t.Errorf("a uuid passes through without a read: %v %v %v", args, err, asked)
	}
}
