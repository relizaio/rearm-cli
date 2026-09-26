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

// task show prints each finding's correction flag (task cac71351): an item a person filed while
// approving at a gate, which never blocks.
func TestTaskShowReadsTheCorrectionFlag(t *testing.T) {
	for _, uuids := range [][]string{{"t1"}, {"t1", "t2"}} {
		op, _, _ := taskShowRequest(uuids)
		if !strings.Contains(strings.Join(strings.Fields(op), " "), "resolvedBy resolution correction }") {
			t.Errorf("task show of %d task(s) does not read correction", len(uuids))
		}
	}
}

// task show prints what each sign-off reviewed and what its review promoted, and what a guard kept
// back with the reason (task fda2c9f1).
func TestTaskShowReadsWhatEachSignOffReviewed(t *testing.T) {
	for _, uuids := range [][]string{{"t1"}, {"t1", "t2"}} {
		op, _, _ := taskShowRequest(uuids)
		n := strings.Join(strings.Fields(op), " ")
		if !strings.Contains(n, "reviewedInputs { release specification round promotedTo }") ||
			!strings.Contains(n, "refusedPromotions { release specification round reason }") {
			t.Errorf("task show of %d task(s) does not read what a sign-off reviewed", len(uuids))
		}
	}
}

// task show prints the task's budget, its coordinator share and spentMicros, what the board
// charges it (task 02bfab7c).
func TestTaskShowReadsTheTaskSpend(t *testing.T) {
	for _, uuids := range [][]string{{"t1"}, {"t1", "t2"}} {
		op, _, _ := taskShowRequest(uuids)
		if !strings.Contains(strings.Join(strings.Fields(op), " "), "budgetMicros coordinatorEstimateMicros spentMicros") {
			t.Errorf("task show of %d task(s) does not read the task's spend", len(uuids))
		}
	}
}
