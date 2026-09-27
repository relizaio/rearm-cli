package cmd

import (
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// Task groups and tags from the CLI (task RD2-29).

func TestRegisterCarriesTheGroupAndTags(t *testing.T) {
	in := withGroupAndTags(map[string]interface{}{"title": "t"}, " basic ", []string{"sandbox-only", " ", "req-7"})
	if in["group"] != "basic" {
		t.Errorf("group %v", in["group"])
	}
	if !reflect.DeepEqual(in["tags"], []map[string]interface{}{{"key": "sandbox-only"}, {"key": "req-7"}}) {
		t.Errorf("tags %v", in["tags"])
	}
	bare := withGroupAndTags(map[string]interface{}{"title": "t"}, "", nil)
	if _, ok := bare["group"]; ok {
		t.Error("a group was sent that was not given")
	}
	if _, ok := bare["tags"]; ok {
		t.Error("tags were sent that were not given")
	}

	taskBoardUuid, taskTitle, taskGroupKey, taskTagKeys = "b", "t", "basic", []string{"x"}
	defer func() { taskBoardUuid, taskTitle, taskGroupKey, taskTagKeys = "", "", "", nil }()
	if got := agentRegisterInput(); got["group"] != "basic" || got["tags"] == nil {
		t.Errorf("agent task register input %v", got)
	}
	for _, c := range []*cobra.Command{agentTaskRegisterCmd, boardsRegisterCmd} {
		if c.Flags().Lookup("group") == nil || c.Flags().Lookup("tag") == nil {
			t.Errorf("%s has no --group or --tag", c.CommandPath())
		}
	}
}

func TestListFiltersByGroupAndAnyTag(t *testing.T) {
	vars := withListFilters(map[string]interface{}{"boardUuid": "b"}, "basic", []string{"a", " ", "b"})
	if vars["group"] != "basic" || !reflect.DeepEqual(vars["tag"], []string{"a", "b"}) {
		t.Errorf("filters %v", vars)
	}
	if v := withListFilters(map[string]interface{}{}, "", nil); len(v) != 0 {
		t.Errorf("unfiltered sent %v", v)
	}
}

func TestSetGroupTakesAGroupOrNone(t *testing.T) {
	v, err := setGroupVariables("t1", "basic", false)
	if err != nil || v["group"] != "basic" {
		t.Errorf("--group basic: %v %v", v, err)
	}
	v, err = setGroupVariables("t1", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if g, has := v["group"]; !has || g != nil {
		t.Errorf("--none must send group as null: %v", v)
	}
	for _, bad := range []struct {
		g    string
		none bool
	}{{"", false}, {"basic", true}} {
		if _, err := setGroupVariables("t1", bad.g, bad.none); err == nil {
			t.Errorf("%+v accepted", bad)
		}
	}
}

func TestTagsAfterAddingAndRemoving(t *testing.T) {
	current := []interface{}{
		map[string]interface{}{"key": "keep", "value": "v1"},
		map[string]interface{}{"key": "Drop"},
	}
	got := tagsAfter(current, []string{"new", "keep", " "}, []string{"drop"})
	want := []map[string]interface{}{{"key": "keep", "value": "v1"}, {"key": "new"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("tags after %v, want %v (values kept, removal any case, no duplicates)", got, want)
	}
	if got := tagsAfter(nil, nil, nil); got == nil || len(got) != 0 {
		t.Errorf("an empty list, not null: %v", got)
	}
}

func TestGroupInputSendsOnlyWhatWasGiven(t *testing.T) {
	c := &cobra.Command{Use: "set"}
	addGroupSetFlags(c)
	defer func() {
		groupSetKey, groupSetName, groupSetDesc, groupSetStatus = "", "", "", ""
		groupSetOrder, groupSetLevel, groupSetDeps, groupSetNoDeps, groupSetNoLvl = 0, 0, nil, false, false
	}()
	if err := c.ParseFlags([]string{"--key", "perms", "--depends-on", "basic,infra", "--default-level", "0"}); err != nil {
		t.Fatal(err)
	}
	in, err := groupInput(c, groupSetKey)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]interface{}{"key": "perms", "dependsOn": []string{"basic", "infra"}, "defaultLevel": 0}
	if !reflect.DeepEqual(in, want) {
		t.Errorf("input %v, want %v (level 0 is a level; name not given is not sent)", in, want)
	}

	c2 := &cobra.Command{Use: "set"}
	addGroupSetFlags(c2)
	if err := c2.ParseFlags([]string{"--key", "perms", "--no-depends-on", "--no-default-level", "--status", "closed"}); err != nil {
		t.Fatal(err)
	}
	in, err = groupInput(c2, groupSetKey)
	if err != nil {
		t.Fatal(err)
	}
	if lv, has := in["defaultLevel"]; !has || lv != nil {
		t.Errorf("--no-default-level sends null: %v", in)
	}
	if !reflect.DeepEqual(in["dependsOn"], []string{}) || in["status"] != "CLOSED" {
		t.Errorf("clears and status: %v", in)
	}

	c3 := &cobra.Command{Use: "set"}
	addGroupSetFlags(c3)
	_ = c3.ParseFlags([]string{"--key", "p", "--status", "gone"})
	if _, err := groupInput(c3, groupSetKey); err == nil || !strings.Contains(err.Error(), "OPEN or CLOSED") {
		t.Errorf("--status gone: %v", err)
	}
	if _, err := groupInput(c3, " "); err == nil {
		t.Error("no --key accepted")
	}
}

func TestTheGroupVerbsAreReachableFromTheRoot(t *testing.T) {
	for _, path := range [][]string{
		{"agent", "task", "setgroup"}, {"agent", "task", "tag"},
		{"agent", "board", "group", "set"}, {"agent", "board", "group", "list"},
		{"agent", "board", "group", "close"}, {"agent", "board", "group", "reopen"},
		{"boards", "setgroup"}, {"boards", "tag"},
		{"boards", "group", "set"}, {"boards", "group", "close"}, {"boards", "group", "reopen"}, {"boards", "group", "delete"},
	} {
		cmd, _, err := rootCmd.Find(path)
		if err != nil || cmd.Name() != path[len(path)-1] || cmd.Parent().Name() != path[len(path)-2] {
			t.Errorf("%s resolves to %v (%v)", strings.Join(path, " "), cmd.CommandPath(), err)
		}
	}
}
