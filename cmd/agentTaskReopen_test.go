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

// A delivery whose PR no longer merges is reopened, not parked: the server reopens DELIVERING as well as
// COMPLETED, and a park's answer returns the task to DELIVERING, which leaves the work undone.
func TestTheReopenHelpNamesADeliveryThatCannotLand(t *testing.T) {
	long := strings.Join(strings.Fields(agentTaskReopenCmd.Long), " ")
	for _, want := range []string{
		"Sends a COMPLETED or DELIVERING task back to an active role.",
		"A DELIVERING task whose PR no longer merges, typically because the base moved, is reopened",
		"a park for the operator returns the task to DELIVERING",
		"A task the seat parked for the operator is not reopened by the seat; a person's reopen answers the park.",
	} {
		if !strings.Contains(long, want) {
			t.Errorf("reopen help is missing %q", want)
		}
	}
	if !strings.Contains(agentTaskReopenCmd.Short, "COMPLETED or DELIVERING") {
		t.Errorf("reopen short help: %q", agentTaskReopenCmd.Short)
	}
	hold := strings.Join(strings.Fields(agentTaskHoldCmd.Long), " ")
	if !strings.Contains(hold, "A delivery that needs a role's work again (its PR no longer merges) is not parked: it is reopened to that role with task reopen --role --reason.") {
		t.Errorf("hold help does not point a delivery that cannot land at reopen")
	}
}
