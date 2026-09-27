package cmd

import (
	"encoding/json"
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

// task show and task list print the title and the description right after the key
// (tests/fceb1e57/run-1.md T-1); the rest keeps the sorted order.
func TestTaskReadsLeadWithKeyTitleAndDescription(t *testing.T) {
	task := map[string]interface{}{"uuid": "u1", "board": "b1", "dependsOn": []interface{}{}, "key": "RD-42",
		"title": "Cap titles", "description": "Why: briefs in titles.\nWhat: a description.", "documents": nil}
	one := string(keyFirstJSON(task))
	want := `{"key":"RD-42","title":"Cap titles","description":"Why: briefs in titles.\nWhat: a description.","board":"b1",`
	if !strings.HasPrefix(one, want) {
		t.Errorf("key, title, description lead, then the rest sorted:\n%s", one)
	}
	var back map[string]interface{}
	if err := json.Unmarshal([]byte(one), &back); err != nil || len(back) != len(task) {
		t.Errorf("and it is the same object: %s %v", one, err)
	}
	noDescription := string(keyFirstJSON(map[string]interface{}{"uuid": "u2", "key": "RD-43", "title": "Short"}))
	if noDescription != `{"key":"RD-43","title":"Short","uuid":"u2"}` {
		t.Errorf("a task without a description: %s", noDescription)
	}
	if got := string(keyFirstJSON(map[string]interface{}{"title": "not a task", "a": 1})); got != `{"a":1,"title":"not a task"}` {
		t.Errorf("an object without a key keeps the sorted order: %s", got)
	}
}
