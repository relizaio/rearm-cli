package cmd

import (
	"strings"
	"testing"

	rearm "github.com/relizaio/rearm-client-go"
)

// task list --changed-since (task 9540d3b6): the instant goes to the read as given, a typo is
// refused before the call, and without it the read is the plain list.
func TestTaskListVariables(t *testing.T) {
	v, err := taskListVariables("b1", "", "")
	if err != nil || v["boardUuid"] != "b1" || len(v) != 1 {
		t.Errorf("the plain list: %v %v", v, err)
	}
	v, err = taskListVariables("b1", "QUEUED", "2026-09-26T03:29:08.471Z")
	if err != nil || v["status"] != "QUEUED" || v["changedSince"] != "2026-09-26T03:29:08.471Z" {
		t.Errorf("status and cursor together: %v %v", v, err)
	}
	if _, err := taskListVariables("b1", "", "2026-09-26T03:29:08+02:00"); err != nil {
		t.Errorf("an offset is an instant too: %v", err)
	}
	for _, bad := range []string{"yesterday", "2026-09-26", "2026-09-26 03:29"} {
		if _, err := taskListVariables("b1", "", bad); err == nil || !strings.Contains(err.Error(), "RFC 3339") {
			t.Errorf("%q should be refused naming the format: %v", bad, err)
		}
	}
	if f := agentTaskListCmd.PersistentFlags().Lookup("changed-since"); f == nil {
		t.Error("task list has no --changed-since")
	}
}

// The read task list sends passes the cursor and returns each task's updatedAt.
func TestTaskListReadsUpdatedAtAndTakesTheCursor(t *testing.T) {
	op := strings.Join(strings.Fields(rearm.AgentTasksProgrammatic_Operation), " ")
	if !strings.Contains(op, "changedSince: $changedSince") || !strings.Contains(op, "updatedAt") {
		t.Error("task list's read does not take changedSince or return updatedAt")
	}
	show, _, _ := taskShowRequest([]string{"t1"})
	if !strings.Contains(show, "updatedAt") {
		t.Error("task show does not print updatedAt")
	}
}
