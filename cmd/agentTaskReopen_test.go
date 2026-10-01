package cmd

import (
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestReopenSendsTheRoleAndTheReason(t *testing.T) {
	got := reopenVariables("t-1", "s-1", "coder", "PR conflicts")
	want := map[string]interface{}{"taskUuid": "t-1", "sessionUuid": "s-1", "role": "coder", "reason": "PR conflicts"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("variables: got %v, want %v", got, want)
	}
}

func TestReopenRequiresTheSessionRoleAndReason(t *testing.T) {
	for _, name := range []string{"session", "role", "reason"} {
		f := agentTaskReopenCmd.PersistentFlags().Lookup(name)
		if f == nil {
			t.Fatalf("reopen has no --%s", name)
		}
		if _, ok := f.Annotations[cobra.BashCompOneRequiredFlag]; !ok {
			t.Errorf("--%s is not required", name)
		}
	}
	if agentTaskReopenCmd.Parent() != agentTaskCmd {
		t.Error("reopen is not under `agent task`")
	}
}

func TestTheStatusFilterNamesDelivering(t *testing.T) {
	f := agentTaskListCmd.PersistentFlags().Lookup("status")
	if f == nil {
		t.Fatal("task list has no --status")
	}
	for _, s := range []string{"DELIVERING", "ON_HOLD", "COMPLETED"} {
		if !strings.Contains(f.Usage, s) {
			t.Errorf("--status help does not name %s: %q", s, f.Usage)
		}
	}
}
