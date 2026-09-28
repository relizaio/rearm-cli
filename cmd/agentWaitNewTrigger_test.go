package cmd

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

// The coordinator's wait wakes only on a trigger it has not reported (task RD3-15): the set kept is the last
// poll's, and a wake needs an id that set did not hold, or an ALERT. A set that only shrinks stays quiet.

type snaps = [][]map[string]interface{}

func shortCoordinator(t *testing.T) waitOpts {
	o := coordinator(t)
	o.timeout = 3 * time.Minute
	return o
}

// marked prints the triggers as "id" or "id*" when marked new, in the order printed.
func marked(printed map[string]interface{}) string {
	list, _ := printed["triggers"].([]interface{})
	ids := kinds(printed)
	for i, tr := range list {
		if n, _ := tr.(map[string]interface{})["new"].(bool); n {
			ids[i] += "*"
		}
	}
	return strings.Join(ids, ",")
}

func delivering(key string) map[string]interface{} {
	return map[string]interface{}{"key": key, "pullRequests": []interface{}{map[string]interface{}{"url": "u", "state": "OPEN"}}}
}

func TestAShrinkingSetStaysQuietAndIsKept(t *testing.T) {
	// The incident: woken on {RD3-13, RD3-14 intake, RD3-4 DELIVERING}; the intakes handled, RD3-4 alone must not wake it.
	o := shortCoordinator(t)
	three := snaps{{entry("RD-13", "PENDING_INTAKE", nil), entry("RD-14", "PENDING_INTAKE", nil), entry("RD-4", "DELIVERING", nil)}}
	f := &fakeBoard{snapshots: three, merge: "COORDINATOR", deliver: []map[string]interface{}{delivering("RD-4")}}
	if code, printed := runCoordinator(t, o, f); code != waitExitWork || marked(printed) != "RD-13 PENDING_INTAKE*,RD-14 PENDING_INTAKE*,RD-4 DELIVERING*" {
		t.Fatalf("first wake: exit %d, %s", code, marked(printed))
	}
	one := snaps{{entry("RD-13", "QUEUED", nil), entry("RD-14", "QUEUED", nil), entry("RD-4", "DELIVERING", nil)}}
	f = &fakeBoard{snapshots: one, merge: "COORDINATOR", deliver: []map[string]interface{}{delivering("RD-4")}}
	if code, printed := runCoordinator(t, o, f); code != waitExitTimeout {
		t.Errorf("a shrunk set woke it: exit %d, %s", code, marked(printed))
	}
	if got := readWaitState(o.statePath).Triggers; !reflect.DeepEqual(got, []string{"RD-4 DELIVERING"}) {
		t.Errorf("kept %v after a quiet poll, want the current set", got)
	}
}

func TestAnUnchangedSetStaysQuiet(t *testing.T) {
	o := shortCoordinator(t)
	set := snaps{{entry("RD-1", "PENDING_INTAKE", nil), entry("RD-2", "AWAITING_COORDINATOR", nil)}}
	runCoordinator(t, o, &fakeBoard{snapshots: set})
	if code, _ := runCoordinator(t, o, &fakeBoard{snapshots: set}); code != waitExitTimeout {
		t.Errorf("the same set woke it again: exit %d", code)
	}
}

func TestANewTriggerWakesAndIsListedFirstMarkedNew(t *testing.T) {
	o := shortCoordinator(t)
	runCoordinator(t, o, &fakeBoard{snapshots: snaps{{entry("RD-1", "PENDING_INTAKE", nil)}}})
	code, printed := runCoordinator(t, o, &fakeBoard{snapshots: snaps{{entry("RD-1", "PENDING_INTAKE", nil), entry("RD-2", "AWAITING_COORDINATOR", nil)}}})
	if code != waitExitWork || marked(printed) != "RD-2 AWAITING_COORDINATOR*,RD-1 PENDING_INTAKE" {
		t.Errorf("exit %d, triggers %s: the new one first and marked, then what still waits", code, marked(printed))
	}
}

func TestATriggerThatLeavesAndReturnsWakes(t *testing.T) {
	o := shortCoordinator(t)
	both := []map[string]interface{}{entry("RD-1", "PENDING_INTAKE", nil), entry("RD-2", "AWAITING_COORDINATOR", nil)}
	runCoordinator(t, o, &fakeBoard{snapshots: snaps{both}})
	// RD-1 leaves (a quiet poll that keeps {RD-2}), then comes back.
	code, printed := runCoordinator(t, o, &fakeBoard{snapshots: snaps{{entry("RD-1", "QUEUED", nil), entry("RD-2", "AWAITING_COORDINATOR", nil)}, both}})
	if code != waitExitWork || marked(printed) != "RD-1 PENDING_INTAKE*,RD-2 AWAITING_COORDINATOR" {
		t.Errorf("a returning trigger: exit %d, %s", code, marked(printed))
	}
}

func TestATaskChangingKindWakes(t *testing.T) {
	o := shortCoordinator(t)
	f := &fakeBoard{snapshots: snaps{{entry("RD-4", "DELIVERING", nil)}}, merge: "COORDINATOR", deliver: []map[string]interface{}{delivering("RD-4")}}
	runCoordinator(t, o, f)
	code, printed := runCoordinator(t, o, &fakeBoard{snapshots: snaps{{entry("RD-4", "AWAITING_COORDINATOR", nil)}}})
	if code != waitExitWork || marked(printed) != "RD-4 AWAITING_COORDINATOR*" {
		t.Errorf("DELIVERING to AWAITING_COORDINATOR: exit %d, %s", code, marked(printed))
	}
}

func TestAnAlertWakesWithNoTaskChangeAndIsNotKept(t *testing.T) {
	o := shortCoordinator(t)
	set := snaps{{entry("RD-1", "PENDING_INTAKE", nil)}}
	runCoordinator(t, o, &fakeBoard{snapshots: set})
	seq := int64(7)
	f := &fakeBoard{snapshots: set, pages: []eventPage{{Events: []map[string]interface{}{{"seq": float64(7), "kind": "ALERT", "message": "stop"}}, NextAfter: &seq}}}
	code, printed := runCoordinator(t, o, f)
	if code != waitExitWork || marked(printed) != "ALERT 7*,RD-1 PENDING_INTAKE" {
		t.Errorf("an ALERT: exit %d, %s", code, marked(printed))
	}
	if got := readWaitState(o.statePath).Triggers; !reflect.DeepEqual(got, []string{"RD-1 PENDING_INTAKE"}) {
		t.Errorf("kept %v: the ALERT is never kept", got)
	}
}

func TestAnEmptiedSetKeepsNothing(t *testing.T) {
	o := shortCoordinator(t)
	runCoordinator(t, o, &fakeBoard{snapshots: snaps{{entry("RD-1", "PENDING_INTAKE", nil)}}})
	if code, _ := runCoordinator(t, o, &fakeBoard{snapshots: snaps{{entry("RD-1", "QUEUED", nil)}}}); code != waitExitTimeout {
		t.Errorf("an empty set woke it: exit %d", code)
	}
	if got := readWaitState(o.statePath).Triggers; got != nil {
		t.Errorf("kept %v for an empty set", got)
	}
}
