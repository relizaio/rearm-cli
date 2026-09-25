package cmd

import (
	"reflect"
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
