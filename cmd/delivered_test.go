package cmd

import "testing"

const sha40 = "abcdef0123456789abcdef0123456789abcdef01"

// An attestation's variables (task 18c5c293).
func TestDeliveredVars(t *testing.T) {
	v, err := deliveredVars("t1", "s1", " https://github.com/a/b/pull/1 ", sha40, false, " merged there ")
	if err != nil || v["unit"] != "https://github.com/a/b/pull/1" || v["commit"] != sha40 || v["outcome"] != "DELIVERED" ||
		v["sessionUuid"] != "s1" || v["note"] != "merged there" {
		t.Errorf("a delivery: %v %v", v, err)
	}
	if v, err := deliveredVars("t1", "", "u", "", true, ""); err != nil || v["outcome"] != "ABANDONED" || v["commit"] != nil {
		t.Errorf("abandoned needs no commit: %v %v", v, err)
	}
	if v, _ := deliveredVars("t1", "", "u", sha40, false, ""); v["sessionUuid"] != nil {
		t.Errorf("a person's attestation carries no session: %v", v)
	}
	for _, bad := range []struct{ unit, commit string }{{"", sha40}, {"u", ""}, {"u", "not-a-sha"}} {
		if _, err := deliveredVars("t1", "s1", bad.unit, bad.commit, false, ""); err == nil {
			t.Errorf("%+v should be refused", bad)
		}
	}
	for _, c := range []string{"unit", "commit", "abandoned", "note"} {
		if agentTaskDeliveredCmd.Flags().Lookup(c) == nil || boardsDeliveredCmd.Flags().Lookup(c) == nil {
			t.Errorf("both verbs take --%s", c)
		}
	}
	if agentTaskDeliveredCmd.Flags().Lookup("session") == nil || boardsDeliveredCmd.Flags().Lookup("session") != nil {
		t.Error("the agent's verb takes --session; the person's does not")
	}
}
