package cmd

import (
	"strings"
	"testing"

	rearm "github.com/relizaio/rearm-client-go"
)

// task show with one uuid reads as before; with several, one request for all (task cc14f4cb).
func TestTaskShowRequest(t *testing.T) {
	op, vars, key := taskShowRequest([]string{"t1"})
	if op != rearm.AgentTaskProgrammatic_Operation || vars["taskUuid"] != "t1" || key != "agentTaskProgrammatic" {
		t.Errorf("one uuid is the single read, unchanged: %v %v", vars, key)
	}
	op, vars, key = taskShowRequest([]string{"t2", "t1"})
	if op != rearm.AgentTasksByUuidProgrammatic_Operation || key != "agentTasksByUuidProgrammatic" {
		t.Errorf("several uuids are one read of the set: %v", key)
	}
	if got, ok := vars["taskUuids"].([]string); !ok || strings.Join(got, ",") != "t2,t1" {
		t.Errorf("the uuids go in the order given: %v", vars)
	}
	if err := agentTaskShowCmd.Args(agentTaskShowCmd, []string{}); err == nil {
		t.Error("task show needs a uuid")
	}
	many := make([]string, 101)
	if err := agentTaskShowCmd.Args(agentTaskShowCmd, many); err == nil {
		t.Error("more than 100 is refused before the call")
	}
}
