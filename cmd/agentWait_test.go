package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 'rearm agent wait' (task RD2-32) against a fake board and a fake clock.

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time        { return c.t }
func (c *fakeClock) sleep(d time.Duration) { c.t = c.t.Add(d) }

type fakeBoard struct {
	offers     []interface{} // one per next call; the last repeats
	nextErrs   []error       // one per next call; nil when absent
	nextCalls  []string      // the session each next carried
	snapshots  [][]map[string]interface{}
	pages      []eventPage
	eventCalls []string
	touches    int
	merge      string
	mergeReads int
	deliver    []map[string]interface{}
	polls      int
}

func (f *fakeBoard) next(session, board string, roles []string) (interface{}, error) {
	i := len(f.nextCalls)
	f.nextCalls = append(f.nextCalls, session)
	if i < len(f.nextErrs) && f.nextErrs[i] != nil {
		return nil, f.nextErrs[i]
	}
	if i >= len(f.offers) {
		i = len(f.offers) - 1
	}
	if i < 0 {
		return nil, nil
	}
	return f.offers[i], nil
}

func (f *fakeBoard) events(board string, after *int64, since string) (eventPage, error) {
	if after != nil {
		f.eventCalls = append(f.eventCalls, "after="+itoa(*after))
	} else {
		f.eventCalls = append(f.eventCalls, "since="+since)
	}
	if len(f.pages) == 0 {
		return eventPage{NextAfter: after}, nil
	}
	p := f.pages[0]
	f.pages = f.pages[1:]
	return p, nil
}

func (f *fakeBoard) snapshot(board string) ([]map[string]interface{}, error) {
	i := f.polls
	f.polls++
	if i >= len(f.snapshots) {
		i = len(f.snapshots) - 1
	}
	if i < 0 {
		return nil, nil
	}
	return f.snapshots[i], nil
}

func (f *fakeBoard) touch(session string) error { f.touches++; return nil }

func (f *fakeBoard) mergeBy(board string) (string, error) { f.mergeReads++; return f.merge, nil }

func (f *fakeBoard) delivering(board string) ([]map[string]interface{}, error) { return f.deliver, nil }

func itoa(n int64) string { b, _ := json.Marshal(n); return string(b) }

func worker(t *testing.T) waitOpts {
	o, err := waitOptsOf("s1", "b1", []string{"coder"}, false, 0, 60, 4*time.Hour, "")
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func coordinator(t *testing.T) waitOpts {
	o, err := waitOptsOf("seat", "b1", nil, true, 0, 60, 4*time.Hour, filepath.Join(t.TempDir(), "wait.json"))
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func entry(key, status string, hold map[string]interface{}) map[string]interface{} {
	t := map[string]interface{}{"key": key, "uuid": "u-" + key, "status": status}
	if hold != nil {
		t["hold"] = hold
	}
	return map[string]interface{}{"task": t}
}

func start() *fakeClock { return &fakeClock{t: time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)} }

func TestAWorkerExitsWithTheOfferAndCarriesItsSession(t *testing.T) {
	offer := map[string]interface{}{"task": map[string]interface{}{"uuid": "t1"}, "role": "coder"}
	f := &fakeBoard{offers: []interface{}{nil, nil, offer}}
	var out bytes.Buffer
	clk := start()
	if code := runWait(worker(t), f, clk, &out); code != waitExitWork {
		t.Fatalf("exit %d, want 0", code)
	}
	if len(f.nextCalls) != 3 || f.nextCalls[0] != "s1" || f.nextCalls[2] != "s1" {
		t.Errorf("next calls %v: quiet on null, and each carries the session", f.nextCalls)
	}
	if got := clk.t.Sub(start().t); got != 2*time.Minute {
		t.Errorf("waited %v between three polls at 60 s, want 2m", got)
	}
	var printed map[string]interface{}
	if err := json.Unmarshal(out.Bytes(), &printed); err != nil || printed["role"] != "coder" {
		t.Errorf("printed %q, want the offer JSON", out.String())
	}
}

func TestTheIntervalHasAFloorAndTheFlagsAreChecked(t *testing.T) {
	if _, err := waitOptsOf("s1", "b1", nil, false, 0, 10, time.Hour, ""); err == nil || err.Error() != "interval is 30 s or more" {
		t.Errorf("--interval 10: %v", err)
	}
	if _, err := waitOptsOf("s1", "", nil, true, 0, 60, time.Hour, "x"); err == nil {
		t.Error("--coordinator without --board accepted")
	}
	if _, err := waitOptsOf("", "b1", nil, false, 0, 60, time.Hour, ""); err == nil {
		t.Error("no --session accepted")
	}
	if _, err := waitOptsOf("s1", "b1", []string{"coder"}, true, 0, 60, time.Hour, "x"); err == nil {
		t.Error("--role with --coordinator accepted")
	}
	if !traced("braceexpand:hashall:xtrace") || traced("braceexpand:hashall") {
		t.Error("xtrace detection")
	}
}

func TestTheTimeoutExitsTwoToReArm(t *testing.T) {
	o := worker(t)
	o.timeout = 5 * time.Minute
	var out bytes.Buffer
	f := &fakeBoard{offers: []interface{}{nil}}
	if code := runWait(o, f, start(), &out); code != waitExitTimeout {
		t.Fatalf("exit %d, want 2", code)
	}
	if strings.TrimSpace(out.String()) != "{\n  \"timeout\": true\n}" {
		t.Errorf("printed %q", out.String())
	}
	if len(f.nextCalls) != 5 {
		t.Errorf("polled %d times in 5 minutes at 60 s, want 5", len(f.nextCalls))
	}
}

func TestOneErrorIsRetriedAndTwoInARowExitOne(t *testing.T) {
	boom := errors.New("502 from the ingress")
	offer := map[string]interface{}{"role": "coder"}
	var out bytes.Buffer
	f := &fakeBoard{offers: []interface{}{nil, nil, nil, offer}, nextErrs: []error{boom, nil, boom, nil}}
	if code := runWait(worker(t), f, start(), &out); code != waitExitWork {
		t.Errorf("errors apart: exit %d, want 0 once the offer comes", code)
	}
	f = &fakeBoard{offers: []interface{}{nil}, nextErrs: []error{nil, boom, boom}}
	if code := runWait(worker(t), f, start(), &bytes.Buffer{}); code != waitExitError {
		t.Errorf("two in a row: exit %d, want 1", code)
	}
}

func runCoordinator(t *testing.T, o waitOpts, f *fakeBoard) (int, map[string]interface{}) {
	t.Helper()
	var out bytes.Buffer
	code := runWait(o, f, start(), &out)
	var printed map[string]interface{}
	_ = json.Unmarshal(out.Bytes(), &printed)
	return code, printed
}

func kinds(printed map[string]interface{}) []string {
	var out []string
	list, _ := printed["triggers"].([]interface{})
	for _, t := range list {
		m := t.(map[string]interface{})
		if a, ok := m["alert"]; ok {
			out = append(out, "ALERT "+itoa(int64(a.(float64))))
			continue
		}
		out = append(out, m["task"].(string)+" "+m["kind"].(string))
	}
	return out
}

func TestTheCoordinatorWakesOnWhatWaitsOnIt(t *testing.T) {
	snap := []map[string]interface{}{
		entry("RD-1", "PENDING_INTAKE", nil),
		entry("RD-2", "AWAITING_COORDINATOR", nil),
		entry("RD-3", "ON_HOLD", map[string]interface{}{"level": "COORDINATOR", "kind": "MANUAL"}),
		entry("RD-4", "ON_HOLD", map[string]interface{}{"level": "OPERATOR", "kind": "MANUAL"}),
		entry("RD-5", "ON_HOLD", map[string]interface{}{"level": "OPERATOR", "kind": "HUMAN_GATE"}),
		entry("RD-6", "QUEUED", nil),
	}
	f := &fakeBoard{snapshots: [][]map[string]interface{}{snap}}
	code, printed := runCoordinator(t, coordinator(t), f)
	if code != waitExitWork {
		t.Fatalf("exit %d", code)
	}
	if events, ok := printed["events"].([]interface{}); !ok || len(events) != 0 {
		t.Errorf("events %v: an empty list when nothing was posted, never null", printed["events"])
	}
	want := "RD-1 PENDING_INTAKE,RD-2 AWAITING_COORDINATOR,RD-3 COORDINATOR_HOLD"
	if got := strings.Join(kinds(printed), ","); got != want {
		t.Errorf("triggers %s, want %s (an operator hold and a human gate wait on a person)", got, want)
	}
	if f.touches != 1 {
		t.Errorf("seat touched %d times in one poll", f.touches)
	}
}

func TestDeliveringWakesOnlyWhenTheMergeIsTheCoordinatorsAndAPrIsOpen(t *testing.T) {
	snap := []map[string]interface{}{entry("RD-7", "DELIVERING", nil)}
	open := []map[string]interface{}{{"key": "RD-7", "pullRequests": []interface{}{
		map[string]interface{}{"url": "u1", "state": "MERGED"}, map[string]interface{}{"url": "u2", "state": "OPEN"}}}}
	merged := []map[string]interface{}{{"key": "RD-7", "pullRequests": []interface{}{
		map[string]interface{}{"url": "u1", "state": "MERGED"}}}}

	o := coordinator(t)
	o.timeout = 3 * time.Minute
	f := &fakeBoard{snapshots: [][]map[string]interface{}{snap}, merge: "COORDINATOR", deliver: open}
	if code, printed := runCoordinator(t, o, f); code != waitExitWork || strings.Join(kinds(printed), ",") != "RD-7 DELIVERING" {
		t.Errorf("coordinator merges, PR open: exit %d, %v", code, kinds(printed))
	}
	for name, f := range map[string]*fakeBoard{
		"a person merges": {snapshots: [][]map[string]interface{}{snap}, merge: "PERSON", deliver: open},
		"every PR merged": {snapshots: [][]map[string]interface{}{snap}, merge: "COORDINATOR", deliver: merged},
	} {
		o := coordinator(t)
		o.timeout = 3 * time.Minute
		if code, _ := runCoordinator(t, o, f); code != waitExitTimeout {
			t.Errorf("%s: exit %d, want the timeout", name, code)
		}
		if f.mergeReads != 1 {
			t.Errorf("%s: who merges read %d times, want once per run", name, f.mergeReads)
		}
	}
}

func TestTheSameTriggersDoNotWakeTwiceAndAnEmptiedSetComesBack(t *testing.T) {
	o := coordinator(t)
	o.timeout = 3 * time.Minute
	intake := [][]map[string]interface{}{{entry("RD-1", "PENDING_INTAKE", nil)}}
	if code, _ := runCoordinator(t, o, &fakeBoard{snapshots: intake}); code != waitExitWork {
		t.Fatalf("first run: exit %d", code)
	}
	if code, _ := runCoordinator(t, o, &fakeBoard{snapshots: intake}); code != waitExitTimeout {
		t.Errorf("the same set woke it twice: exit %d", code)
	}
	// Handled (empty), then back: wakes again.
	back := [][]map[string]interface{}{{entry("RD-1", "QUEUED", nil)}, {entry("RD-1", "PENDING_INTAKE", nil)}}
	if code, printed := runCoordinator(t, o, &fakeBoard{snapshots: back}); code != waitExitWork || kinds(printed)[0] != "RD-1 PENDING_INTAKE" {
		t.Errorf("an emptied set coming back: exit %d, %v", code, kinds(printed))
	}
	if readWaitState(o.statePath) == nil {
		t.Error("the wake's set is not kept")
	}
}

func TestTheCursorStartsNowAndCarriesInfoAndAlerts(t *testing.T) {
	seq := func(n int64) *int64 { return &n }
	f := &fakeBoard{
		snapshots: [][]map[string]interface{}{{}},
		pages: []eventPage{
			{Events: []map[string]interface{}{{"seq": float64(41), "kind": "INFO", "message": "hello"}}, NextAfter: seq(41), HasMore: true},
			{Events: []map[string]interface{}{{"seq": float64(42), "kind": "LOCKED"}, {"seq": float64(43), "kind": "ALERT", "message": "stop"}}, NextAfter: seq(43)},
		},
	}
	code, printed := runCoordinator(t, coordinator(t), f)
	if code != waitExitWork {
		t.Fatalf("exit %d", code)
	}
	if !strings.HasPrefix(f.eventCalls[0], "since=2026-09-27T12:00:00Z") || f.eventCalls[1] != "after=41" {
		t.Errorf("event reads %v: from the moment the loop started, then on from the cursor", f.eventCalls)
	}
	if got := strings.Join(kinds(printed), ","); got != "ALERT 43" {
		t.Errorf("triggers %s", got)
	}
	events, _ := printed["events"].([]interface{})
	if len(events) != 2 || printed["nextAfter"].(float64) != 43 {
		t.Errorf("printed events %v nextAfter %v: INFO and ALERT, not LOCKED", events, printed["nextAfter"])
	}

	// --after starts the cursor there instead.
	o := coordinator(t)
	o.after = 40
	f = &fakeBoard{snapshots: [][]map[string]interface{}{{entry("RD-1", "PENDING_INTAKE", nil)}}}
	runCoordinator(t, o, f)
	if f.eventCalls[0] != "after=40" {
		t.Errorf("--after 40 read %v", f.eventCalls)
	}
}

func TestWaitIsAttachedUnderAgent(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"agent", "wait"})
	if err != nil || cmd != agentWaitCmd {
		t.Fatalf("agent wait resolves to %v (%v)", cmd, err)
	}
	for _, f := range []string{"session", "board", "role", "coordinator", "after", "interval", "timeout", "state"} {
		if agentWaitCmd.Flags().Lookup(f) == nil {
			t.Errorf("no --%s", f)
		}
	}
}
