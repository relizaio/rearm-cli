package cmd

import (
	"reflect"
	"testing"
)

func intp(n int) *int { return &n }

// Task level from the CLI (RD2-1): level 0 (requirements) is a level, so a flag given as 0 is sent
// and a flag not given is not; set-level takes 0 to 9 or --clear.
func TestLevelIsSentWhenGivenZeroIncluded(t *testing.T) {
	if _, ok := registerVariables("b", "t", "", "", "", "", nil)["input"].(map[string]interface{})["level"]; ok {
		t.Error("boards register sent a level it was not given")
	}
	got := registerVariables("b", "t", "", "", "", "", intp(0))["input"].(map[string]interface{})["level"]
	if got != 0 {
		t.Errorf("boards register --level 0 sent %v", got)
	}
	if authorizeVariables("t", "coder", 0, false, nil, intp(0))["level"] != 0 {
		t.Error("boards authorize --level 0 not sent")
	}

	taskBoardUuid, taskTitle, taskLevel, taskLevelSet = "b", "t", 0, false
	defer func() { taskBoardUuid, taskTitle, taskLevel, taskLevelSet = "", "", 0, false }()
	if _, ok := agentRegisterInput()["level"]; ok {
		t.Error("agent task register sent a level it was not given")
	}
	taskLevelSet = true
	if agentRegisterInput()["level"] != 0 {
		t.Error("agent task register --level 0 not sent")
	}
	for _, flag := range []string{"level"} {
		if agentTaskRegisterCmd.Flag(flag) == nil || boardsRegisterCmd.Flag(flag) == nil {
			t.Errorf("register has no --%s", flag)
		}
	}
}

func TestSetLevelTakesZeroToNineOrClear(t *testing.T) {
	got, err := levelVariables("t", 2, true, false)
	if err != nil || !reflect.DeepEqual(got, map[string]interface{}{"taskUuid": "t", "level": 2}) {
		t.Errorf("--level 2: %v %v", got, err)
	}
	got, err = levelVariables("t", 0, false, true)
	if err != nil || got["level"] != nil {
		t.Errorf("--clear sends null: %v %v", got, err)
	}
	if _, has := got["level"]; !has {
		t.Error("--clear must send the key, as null")
	}
	for _, bad := range []struct {
		level      int
		set, clear bool
	}{{10, true, false}, {-1, true, false}, {2, true, true}, {0, false, false}} {
		if _, err := levelVariables("t", bad.level, bad.set, bad.clear); err == nil {
			t.Errorf("%+v accepted", bad)
		}
	}
	for _, c := range []interface{ Name() string }{boardsTaskLevelCmd, agentTaskLevelCmd} {
		if c.Name() != "level" {
			t.Errorf("verb named %q", c.Name())
		}
	}
	if agentTaskLevelCmd.Flag("session") == nil || agentTaskLevelCmd.Flag("clear") == nil || boardsTaskLevelCmd.Flag("clear") == nil {
		t.Error("the level verbs take --session (seat) and --clear")
	}
}
