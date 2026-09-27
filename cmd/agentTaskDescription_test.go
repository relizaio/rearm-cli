package cmd

import (
	"reflect"
	"strings"
	"testing"
)

// A task's description goes with its title when one is given and not otherwise (task fceb1e57).
func TestRegisterSendsTheDescriptionWhenGiven(t *testing.T) {
	got := registerVariables("b-1", "fix it", "the long story", "", "", "", 0)
	want := map[string]interface{}{"boardUuid": "b-1", "input": map[string]interface{}{"title": "fix it", "description": "the long story"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("boards register: got %v, want %v", got, want)
	}

	taskBoardUuid, taskTitle, taskDescription, taskExternalRef, taskSourceUrl, taskSessionUuid = "b-1", "fix it", "", "", "", ""
	if _, ok := agentRegisterInput()["description"]; ok {
		t.Error("agent task register sent a description without --description")
	}
	taskDescription = "the long story"
	defer func() { taskBoardUuid, taskTitle, taskDescription = "", "", "" }()
	if got := agentRegisterInput()["description"]; got != "the long story" {
		t.Errorf("agent task register: description %v", got)
	}
	for _, c := range []string{"register", "boards-register"} {
		cmd := agentTaskRegisterCmd
		if c == "boards-register" {
			cmd = boardsRegisterCmd
		}
		if cmd.Flag("description") == nil {
			t.Errorf("%s has no --description", c)
		}
	}
}

// task show reads the description (through the client's selection).
func TestTaskShowReadsTheDescription(t *testing.T) {
	for _, uuids := range [][]string{{"t1"}, {"t1", "t2"}} {
		op, _, _ := taskShowRequest(uuids)
		if !strings.Contains(strings.Join(strings.Fields(op), " "), "externalRef title description sourceUrl") {
			t.Errorf("task show of %d task(s) does not read the description", len(uuids))
		}
	}
}
