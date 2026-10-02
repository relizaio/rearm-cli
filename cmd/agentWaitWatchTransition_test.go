package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A watch change prints the transition it observed, not the task's current status (task RD5-7): from is
// the from of the hop end's own status-history row, to is where that row's transaction left the task
// (the row followed through the routing rows the system wrote with it, ARCHITECTURE round 2), the
// current status prints as status, and new is always printed, so a standing change reads as standing
// after the task moved on. The rows are shaped as on Dogfood 5 (RD5-1's tester rejection: signedOffAt
// 00:30:00.035, the SIGNOFF row 30 ms later, the SYSTEM routing row 7 ms after that).

// rejectionHistory is RD-1's status history through the tester's rejection, plus the rows given.
func rejectionHistory(more ...map[string]interface{}) []interface{} {
	h := []interface{}{
		histRow("QUEUED", "ASSIGNED", "2026-09-27T10:00:00Z", "ASSIGN", "SESSION"),
		histRow("ASSIGNED", "AWAITING_COORDINATOR", "2026-09-27T11:30:00.030Z", "SIGNOFF", "SESSION"),
		histRow("AWAITING_COORDINATOR", "QUEUED", "2026-09-27T11:30:00.037Z", "AUTHORIZE", "SYSTEM"),
	}
	for _, m := range more {
		h = append(h, m)
	}
	return h
}

// keptChange is the change the state file holds for a task after an exit.
func keptChange(t *testing.T, state, uuid string) *watchChange {
	t.Helper()
	b, err := os.ReadFile(state)
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	var st watchKept
	if err := json.Unmarshal(b, &st); err != nil {
		t.Fatalf("state %s: %v", b, err)
	}
	return st.Tasks[uuid].Change
}

// transition checks a printed change's from, to, status and new; new must be present, true or false.
func transition(t *testing.T, label string, c map[string]interface{}, from, to, status string, isNew bool) {
	t.Helper()
	n, has := c["new"]
	if !has {
		t.Errorf("%s: no new key in %v; new is always printed", label, c)
	}
	if c["from"] != from || c["to"] != to || c["status"] != status || n != isNew {
		t.Errorf("%s: from %v to %v status %v new %v, want %s %s %s %v", label, c["from"], c["to"], c["status"], n, from, to, status, isNew)
	}
	if c["from"] == c["to"] {
		t.Errorf("%s: from equals to (%v): not a transition", label, c["from"])
	}
}

func TestWatchChangePrintsItsOwnTransitionAcrossAPickUpAndASecondRejection(t *testing.T) {
	state := filepath.Join(t.TempDir(), "w.json")
	o := watcher(t, state)

	// The tester rejects: the SIGNOFF row and the routing row, ASSIGNED to QUEUED, queued for the coder.
	// It wakes, marked new.
	first := rejected()
	first["statusHistory"] = rejectionHistory()
	f := &fakeWatch{fakeBoard: &fakeBoard{snapshots: snaps{{snapEntry("RD-1", "QUEUED", "coder", "rel-test")}}},
		worked: worked("RD-1"), roles: boardRoles, details: []map[string]map[string]interface{}{{"u-RD-1": first}}}
	code, printed := runWatch(t, o, f)
	ch := changesOf(printed)
	if code != waitExitWork || len(ch) != 1 {
		t.Fatalf("rejection: exit %d, changes %v", code, printed["changes"])
	}
	transition(t, "rejection", ch[0], "ASSIGNED", "QUEUED", "QUEUED", true)
	if k := keptChange(t, state, "u-RD-1"); k == nil || k.From != "ASSIGNED" || k.To != "QUEUED" || k.Status != "QUEUED" || k.At != "2026-09-27T11:30:00Z" {
		t.Errorf("state after the rejection: %+v", k)
	}

	// The coder picks it up: QUEUED to ASSIGNED, no new hop end. The task moved and is read again; the
	// change still prints the rejection's own transition, with the task's status now ASSIGNED, as
	// standing, and the watch does not wake.
	taken := rejected()
	taken["status"] = "ASSIGNED"
	taken["statusHistory"] = rejectionHistory(histRow("QUEUED", "ASSIGNED", "2026-09-27T11:45:00Z", "ASSIGN", "SESSION"))
	f = &fakeWatch{fakeBoard: &fakeBoard{snapshots: snaps{{snapEntry("RD-1", "ASSIGNED", "coder", "rel-test")}}},
		worked: worked("RD-1"), roles: boardRoles, details: []map[string]map[string]interface{}{{"u-RD-1": taken}}}
	code, printed = runWatch(t, o, f)
	ch = changesOf(printed)
	if code != waitExitTimeout || len(f.taskReads) != 1 || len(ch) != 1 {
		t.Fatalf("pick-up: exit %d, task reads %v, changes %v", code, f.taskReads, printed["changes"])
	}
	transition(t, "pick-up", ch[0], "ASSIGNED", "QUEUED", "ASSIGNED", false)
	if k := keptChange(t, state, "u-RD-1"); k == nil || k.From != "ASSIGNED" || k.To != "QUEUED" || k.Status != "ASSIGNED" || k.At != "2026-09-27T11:30:00Z" {
		t.Errorf("state after the pick-up: %+v", k)
	}

	// The coder's fix passes to the tester, who takes it and rejects again: the second rejection prints
	// its own transition, marked new, and wakes.
	again := rejected()
	again["signOffs"] = append(again["signOffs"].([]interface{}),
		signOff("coder-s", "coder", "PASSED", "2026-09-27T12:00:00Z"), signOff("tester-s", "tester", "REJECTED", "2026-09-27T12:30:00Z"))
	again["documents"] = append([]interface{}{doc("rel-test2", "BOARD_TEST_REPORT", 2, "2026-09-27T12:29:00Z", "REJECTED")}, again["documents"].([]interface{})...)
	again["statusHistory"] = rejectionHistory(
		histRow("QUEUED", "ASSIGNED", "2026-09-27T11:45:00Z", "ASSIGN", "SESSION"),
		histRow("ASSIGNED", "AWAITING_COORDINATOR", "2026-09-27T12:00:00.030Z", "SIGNOFF", "SESSION"),
		histRow("AWAITING_COORDINATOR", "QUEUED", "2026-09-27T12:00:00.037Z", "AUTHORIZE", "SYSTEM"),
		histRow("QUEUED", "ASSIGNED", "2026-09-27T12:10:00Z", "ASSIGN", "SESSION"),
		histRow("ASSIGNED", "AWAITING_COORDINATOR", "2026-09-27T12:30:00.030Z", "SIGNOFF", "SESSION"),
		histRow("AWAITING_COORDINATOR", "QUEUED", "2026-09-27T12:30:00.037Z", "AUTHORIZE", "SYSTEM"))
	f = &fakeWatch{fakeBoard: &fakeBoard{snapshots: snaps{{snapEntry("RD-1", "QUEUED", "coder", "rel-test2")}}},
		worked: worked("RD-1"), roles: boardRoles, details: []map[string]map[string]interface{}{{"u-RD-1": again}}}
	code, printed = runWatch(t, o, f)
	ch = changesOf(printed)
	if code != waitExitWork || len(ch) != 1 || ch[0]["at"] != "2026-09-27T12:30:00Z" {
		t.Fatalf("second rejection: exit %d, changes %v", code, printed["changes"])
	}
	transition(t, "second rejection", ch[0], "ASSIGNED", "QUEUED", "QUEUED", true)
	if k := keptChange(t, state, "u-RD-1"); k == nil || k.From != "ASSIGNED" || k.To != "QUEUED" || k.Status != "QUEUED" || k.At != "2026-09-27T12:30:00Z" {
		t.Errorf("state after the second rejection: %+v", k)
	}
}

// rejectedWith is RD-1 rejected by the tester, with the given status and history.
func rejectedWith(status string, rows ...map[string]interface{}) map[string]interface{} {
	r := rejected()
	r["status"] = status
	h := []interface{}{histRow("QUEUED", "ASSIGNED", "2026-09-27T10:00:00Z", "ASSIGN", "SESSION")}
	for _, row := range rows {
		h = append(h, row)
	}
	r["statusHistory"] = h
	return r
}

// The three cases of ARCHITECTURE round 2 §2, and what is not chained: to follows the SYSTEM rows the
// rejection's transaction wrote, from stays the SIGNOFF row's, and status is always the current status.
func TestWatchChangeToIsWhereTheTransactionLeftTheTask(t *testing.T) {
	signoffRow := histRow("ASSIGNED", "AWAITING_COORDINATOR", "2026-09-27T11:30:00.030Z", "SIGNOFF", "SESSION")
	cases := []struct {
		name, status, to string
		rows             []map[string]interface{}
	}{
		// The SIGNOFF row and the routing row in one second: queued for the coder.
		{"two rows", "QUEUED", "QUEUED", []map[string]interface{}{signoffRow,
			histRow("AWAITING_COORDINATOR", "QUEUED", "2026-09-27T11:30:00.037Z", "AUTHORIZE", "SYSTEM")}},
		// The SIGNOFF row alone: the board waits on the coordinator, and that is where the task stands.
		{"lone row", "AWAITING_COORDINATOR", "AWAITING_COORDINATOR", []map[string]interface{}{signoffRow}},
		// The board parks the rejection at a stop: the routing HOLD row follows.
		{"hold", "ON_HOLD", "ON_HOLD", []map[string]interface{}{signoffRow,
			histRow("AWAITING_COORDINATOR", "ON_HOLD", "2026-09-27T11:30:00.041Z", "HOLD", "SYSTEM")}},
		// The routing row 900 ms after the sign-off is still within the second's slack.
		{"within the slack", "QUEUED", "QUEUED", []map[string]interface{}{signoffRow,
			histRow("AWAITING_COORDINATOR", "QUEUED", "2026-09-27T11:30:00.900Z", "AUTHORIZE", "SYSTEM")}},
		// A SYSTEM row past the second is another transaction: not chained.
		{"past the slack", "QUEUED", "AWAITING_COORDINATOR", []map[string]interface{}{signoffRow,
			histRow("AWAITING_COORDINATOR", "QUEUED", "2026-09-27T11:30:01.200Z", "AUTHORIZE", "SYSTEM")}},
		// The coordinator's own authorize in the same second is a session's row, not the transaction's.
		{"session row", "QUEUED", "AWAITING_COORDINATOR", []map[string]interface{}{signoffRow,
			histRow("AWAITING_COORDINATOR", "QUEUED", "2026-09-27T11:30:00.500Z", "AUTHORIZE", "SESSION")}},
		// A person's row is not the system's either.
		{"user row", "QUEUED", "AWAITING_COORDINATOR", []map[string]interface{}{signoffRow,
			histRow("AWAITING_COORDINATOR", "QUEUED", "2026-09-27T11:30:00.500Z", "AUTHORIZE", "USER")}},
		// A SYSTEM row that does not start where the chain stands is not part of it.
		{"other from", "DELIVERING", "AWAITING_COORDINATOR", []map[string]interface{}{signoffRow,
			histRow("QUEUED", "DELIVERING", "2026-09-27T11:30:00.040Z", "DELIVER_WAIT", "SYSTEM")}},
		// The chain is followed through more than one routing row.
		{"two routing rows", "ON_HOLD", "ON_HOLD", []map[string]interface{}{signoffRow,
			histRow("AWAITING_COORDINATOR", "QUEUED", "2026-09-27T11:30:00.037Z", "AUTHORIZE", "SYSTEM"),
			histRow("QUEUED", "ON_HOLD", "2026-09-27T11:30:00.041Z", "HOLD", "SYSTEM")}},
	}
	for _, c := range cases {
		ch := changeOf(rejectedWith(c.status, c.rows...), me)
		if ch == nil || ch.Trigger != "REJECTED" || ch.From != "ASSIGNED" || ch.To != c.to || ch.Status != c.status {
			t.Errorf("%s: %+v, want from ASSIGNED to %s status %s", c.name, ch, c.to, c.status)
		}
	}

	// A pick-up after the hold was lifted does not move from or to; status is ASSIGNED.
	lifted := changeOf(rejectedWith("ASSIGNED", signoffRow,
		histRow("AWAITING_COORDINATOR", "ON_HOLD", "2026-09-27T11:30:00.041Z", "HOLD", "SYSTEM"),
		histRow("ON_HOLD", "QUEUED", "2026-09-27T11:40:00Z", "LIFT_HOLD", "USER"),
		histRow("QUEUED", "ASSIGNED", "2026-09-27T11:45:00Z", "ASSIGN", "SESSION")), me)
	if lifted == nil || lifted.From != "ASSIGNED" || lifted.To != "ON_HOLD" || lifted.Status != "ASSIGNED" {
		t.Errorf("hold lifted and picked up: %+v", lifted)
	}
}

// chainedTo on its own: the slack is counted from the change, as hopEndRow counts it.
func TestChainedToCountsTheSlackFromTheChange(t *testing.T) {
	rows := []map[string]interface{}{
		histRow("ASSIGNED", "AWAITING_COORDINATOR", "2026-09-27T11:30:00.030Z", "SIGNOFF", "SESSION"),
		histRow("AWAITING_COORDINATOR", "QUEUED", "2026-09-27T11:30:00.990Z", "AUTHORIZE", "SYSTEM"),
	}
	at := timeOf("2026-09-27T11:30:00Z")
	if got := chainedTo(rows, 0, at); got != "QUEUED" {
		t.Errorf("990 ms after the change: %s, want QUEUED", got)
	}
	if got := chainedTo(rows, 0, at.Add(-100*time.Millisecond)); got != "AWAITING_COORDINATOR" {
		t.Errorf("1090 ms after the change: %s, want AWAITING_COORDINATOR", got)
	}
}

// A reopen prints the REOPEN row's transition while the task already moved on, followed through the
// budget hold the routing writes with it; a pass after the session's rejection prints the producer's
// sign-off, not the status the task holds now.
func TestWatchChangeToIsTheHistoryRowNotTheCurrentStatus(t *testing.T) {
	reopen := func(status string, rows ...interface{}) map[string]interface{} {
		return taskDetail("RD-3", status, "architect",
			[]map[string]interface{}{signOff(me, "architect", "PASSED", "2026-09-27T09:00:00Z"),
				signOff("tester-s", "tester", "PASSED", "2026-09-27T10:00:00Z")}, nil,
			map[string]interface{}{"reopens": []interface{}{map[string]interface{}{"role": "architect", "at": "2026-09-27T11:00:00Z",
				"reason": "the design missed a case", "by": map[string]interface{}{"kind": "USER", "name": "operator"}}},
				"statusHistory": rows})
	}
	c := changeOf(reopen("ASSIGNED",
		histRow("COMPLETED", "QUEUED", "2026-09-27T11:00:00.010Z", "REOPEN", "USER"),
		histRow("QUEUED", "ASSIGNED", "2026-09-27T11:10:00Z", "ASSIGN", "SESSION")), me)
	if c == nil || c.Trigger != "REOPENED" || c.From != "COMPLETED" || c.To != "QUEUED" || c.Status != "ASSIGNED" {
		t.Errorf("reopen picked up: %+v", c)
	}
	c = changeOf(reopen("ON_HOLD",
		histRow("COMPLETED", "QUEUED", "2026-09-27T11:00:00.010Z", "REOPEN", "USER"),
		histRow("QUEUED", "ON_HOLD", "2026-09-27T11:00:00.012Z", "HOLD", "SYSTEM")), me)
	if c == nil || c.Trigger != "REOPENED" || c.From != "COMPLETED" || c.To != "ON_HOLD" || c.Status != "ON_HOLD" {
		t.Errorf("reopen over budget: %+v", c)
	}

	resubmitted := taskDetail("RD-7", "ASSIGNED", "tester", []map[string]interface{}{
		signOff(me, "reviewer", "REJECTED", "2026-09-27T10:00:00Z"),
		signOff("coder-s", "coder", "PASSED", "2026-09-27T11:30:00Z")}, nil,
		map[string]interface{}{"statusHistory": []interface{}{
			histRow("ASSIGNED", "AWAITING_COORDINATOR", "2026-09-27T11:30:00.030Z", "SIGNOFF", "SESSION"),
			histRow("AWAITING_COORDINATOR", "QUEUED", "2026-09-27T11:30:00.037Z", "AUTHORIZE", "SYSTEM"),
			histRow("QUEUED", "ASSIGNED", "2026-09-27T11:40:00Z", "ASSIGN", "SESSION")}})
	c = changeOf(resubmitted, me)
	if c == nil || c.Trigger != "PASSED" || c.From != "ASSIGNED" || c.To != "QUEUED" || c.Status != "ASSIGNED" {
		t.Errorf("producer's pass picked up: %+v", c)
	}

	// With no row of the hop end's kind in the history, from and to are both empty: never the status.
	bare := taskDetail("RD-8", "ASSIGNED", "coder", []map[string]interface{}{
		signOff(me, "architect", "PASSED", "2026-09-27T09:00:00Z"),
		signOff("tester-s", "tester", "REJECTED", "2026-09-27T10:00:00Z")}, nil,
		map[string]interface{}{"statusHistory": []interface{}{}})
	c = changeOf(bare, me)
	if c == nil || c.From != "" || c.To != "" || c.Status != "ASSIGNED" {
		t.Errorf("no history row: %+v", c)
	}
	b, _ := json.Marshal(c)
	if !strings.Contains(string(b), `"to":""`) || !strings.Contains(string(b), `"new":false`) || !strings.Contains(string(b), `"status":"ASSIGNED"`) {
		t.Errorf("printed %s: to, status and new are always keys", b)
	}
}

// The watch asks for who wrote each status-history row, which the chain reads.
func TestWatchTasksReadSelectsTheHistoryActor(t *testing.T) {
	if !strings.Contains(watchTasksOp, "statusHistory { from to at trigger actor { kind } }") {
		t.Errorf("the watch's task read lacks statusHistory actor { kind }:\n%s", watchTasksOp)
	}
}

func TestWaitHelpNamesStatusAndAlwaysPrintedNew(t *testing.T) {
	help := strings.Join(strings.Fields(agentWaitCmd.Long), " ")
	for _, want := range []string{
		`never from the task's current status, which prints as "status"`,
		`to is where that transaction left the task, the row's to followed through the routing rows the system wrote with it`,
		`A rejection prints from ASSIGNED, to QUEUED; one the board parks, to ON_HOLD; a sign-off that waits on the coordinator, to AWAITING_COORDINATOR`,
		`prints from ASSIGNED, to QUEUED, status ASSIGNED`,
		`Every change prints "new": true or false; false marks a standing change`,
	} {
		if !strings.Contains(help, want) {
			t.Errorf("wait --help lacks %q", want)
		}
	}
}
