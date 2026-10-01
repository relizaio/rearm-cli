package cmd

import (
	"strings"
	"testing"
)

// A supersede declaration's variables and flags (task RD3-13).
func TestSupersedeVarsAndFlags(t *testing.T) {
	v, err := supersedeVars("t1", "s1", " https://github.com/a/b/pull/1 ", " https://github.com/a/b/pull/2 ", " no force push ")
	if err != nil || v["taskUuid"] != "t1" || v["sessionUuid"] != "s1" || v["oldUrl"] != "https://github.com/a/b/pull/1" ||
		v["byUrl"] != "https://github.com/a/b/pull/2" || v["note"] != "no force push" {
		t.Errorf("a declaration: %v %v", v, err)
	}
	if v, _ := supersedeVars("t1", "s1", "o", "b", "  "); v["note"] != nil {
		t.Errorf("a blank note is not sent: %v", v)
	}
	for _, bad := range [][2]string{{"", "b"}, {"o", ""}, {" ", " "}} {
		if _, err := supersedeVars("t1", "s1", bad[0], bad[1], ""); err == nil || !strings.Contains(err.Error(), "--old") {
			t.Errorf("%v should be refused: %v", bad, err)
		}
	}
	for _, f := range []string{"session", "old", "by", "note"} {
		if agentTaskSupersedeCmd.Flags().Lookup(f) == nil {
			t.Errorf("supersedepr takes --%s", f)
		}
	}
	found := false
	for _, c := range agentTaskCmd.Commands() {
		found = found || c.Name() == "supersedepr"
	}
	if !found {
		t.Error("supersedepr is not a task verb")
	}
}
