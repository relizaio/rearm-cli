package cmd

import (
	"reflect"
	"testing"
)

func intp(n int) *int { return &n }

// Task work level from the CLI (RD2-1): work level 0 (requirements) is a level, so a flag given as 0 is sent
// and a flag not given is not; work-level takes 0 to 9 or --clear.
func TestWorkLevelIsSentWhenGivenZeroIncluded(t *testing.T) {
	if _, ok := registerVariables("b", "t", "", "", "", "", nil)["input"].(map[string]interface{})["workLevel"]; ok {
		t.Error("boards register sent a work level it was not given")
	}
	got := registerVariables("b", "t", "", "", "", "", intp(0))["input"].(map[string]interface{})["workLevel"]
	if got != 0 {
		t.Errorf("boards register --work-level 0 sent %v", got)
	}
	if authorizeVariables("t", "coder", 0, false, nil, intp(0))["workLevel"] != 0 {
		t.Error("boards authorize --work-level 0 not sent")
	}

	taskBoardUuid, taskTitle, taskLevel, taskLevelSet = "b", "t", 0, false
	defer func() { taskBoardUuid, taskTitle, taskLevel, taskLevelSet = "", "", 0, false }()
	if _, ok := agentRegisterInput()["workLevel"]; ok {
		t.Error("agent task register sent a work level it was not given")
	}
	taskLevelSet = true
	if agentRegisterInput()["workLevel"] != 0 {
		t.Error("agent task register --work-level 0 not sent")
	}
	for _, flag := range []string{"work-level"} {
		if agentTaskRegisterCmd.Flag(flag) == nil || boardsRegisterCmd.Flag(flag) == nil {
			t.Errorf("register has no --%s", flag)
		}
	}
}

func TestSetWorkLevelTakesZeroToNineOrClear(t *testing.T) {
	got, err := workLevelVariables("t", 2, true, false)
	if err != nil || !reflect.DeepEqual(got, map[string]interface{}{"taskUuid": "t", "workLevel": 2}) {
		t.Errorf("--work-level 2: %v %v", got, err)
	}
	got, err = workLevelVariables("t", 0, false, true)
	if err != nil || got["workLevel"] != nil {
		t.Errorf("--clear sends null: %v %v", got, err)
	}
	if _, has := got["workLevel"]; !has {
		t.Error("--clear must send the key, as null")
	}
	for _, bad := range []struct {
		level      int
		set, clear bool
	}{{10, true, false}, {-1, true, false}, {2, true, true}, {0, false, false}} {
		if _, err := workLevelVariables("t", bad.level, bad.set, bad.clear); err == nil {
			t.Errorf("%+v accepted", bad)
		}
	}
	for _, c := range []interface{ Name() string }{boardsTaskWorkLevelCmd, agentTaskWorkLevelCmd} {
		if c.Name() != "work-level" {
			t.Errorf("verb named %q", c.Name())
		}
	}
	if agentTaskWorkLevelCmd.Flag("session") == nil || agentTaskWorkLevelCmd.Flag("clear") == nil || boardsTaskWorkLevelCmd.Flag("clear") == nil {
		t.Error("the work-level verbs take --session (seat) and --clear")
	}
}
