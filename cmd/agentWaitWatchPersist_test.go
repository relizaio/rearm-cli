package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Task RD4-16: whatever ends a watch -- an offer, a change, a question, an event or the timeout -- the
// state holds what it reported before the process exits, and it prints one shape, the watch object,
// with the offer inside it or null.

// snapFails is a watch board whose snapshot read fails on the listed polls (0-based).
type snapFails struct {
	*fakeWatch
	failOn map[int]bool
	reads  int
}

func (f *snapFails) snapshot(board string) ([]map[string]interface{}, error) {
	i := f.reads
	f.reads++
	if f.failOn[i] {
		return nil, errors.New("snapshot unavailable")
	}
	return f.fakeWatch.snapshot(board)
}

// askedOf is a snapshot entry with an open question to the architect, asked after anything the session did.
func askedOf(key string) map[string]interface{} {
	e := snapEntry(key, "QUEUED", "architect")
	e["waitingOn"] = map[string]interface{}{"askingRole": "r-coder", "answeringRole": "r-arch",
		"questionsRelease": "rel-q-" + key, "askedAt": "2026-09-27T11:45:00Z"}
	return e
}

var anOffer = map[string]interface{}{"task": map[string]interface{}{"uuid": "t9", "key": "RD-9"}, "role": "architect"}

func TestAnOfferWakeKeepsWhatItSawAndTheNextRunStaysQuiet(t *testing.T) {
	state := filepath.Join(t.TempDir(), "w.json")
	o := watcher(t, state)
	board := snaps{{snapEntry("RD-1", "QUEUED", "coder", "rel-test"), askedOf("RD-5")}}
	details := []map[string]map[string]interface{}{{"u-RD-1": rejected()}}
	f := &fakeWatch{fakeBoard: &fakeBoard{offers: []interface{}{anOffer}, snapshots: board},
		worked: worked("RD-1"), roles: boardRoles, details: details}
	code, printed := runWatch(t, o, f)
	if code != waitExitWork || printed["offer"] == nil {
		t.Fatalf("exit %d, printed %v: the offer", code, printed)
	}
	ch, qs := changesOf(printed), mapsOf(printed["questions"])
	if len(ch) != 1 || ch[0]["task"] != "RD-1" || ch[0]["new"] != true || len(qs) != 1 || qs[0]["task"] != "RD-5" || qs[0]["new"] != true {
		t.Fatalf("the offer wake lists the change and the question it saw: changes %v, questions %v", printed["changes"], printed["questions"])
	}
	kept := readWatchState(state)
	if k := kept.Tasks["u-RD-1"]; k.Change == nil || k.Change.Trigger != "REJECTED" {
		t.Errorf("the state after the offer wake holds no change on RD-1: %+v", kept)
	}
	if len(kept.Questions) != 1 || kept.Questions[0] != "RD-5 rel-q-RD-5" {
		t.Errorf("the state after the offer wake holds questions %v", kept.Questions)
	}
	// The next run: no offer, nothing new on the board. It stays quiet and reads no task again.
	f = &fakeWatch{fakeBoard: &fakeBoard{offers: []interface{}{nil}, snapshots: board}, worked: worked("RD-1"), roles: boardRoles, details: details}
	code, printed = runWatch(t, o, f)
	if code != waitExitTimeout {
		t.Fatalf("the run after an offer wake woke again: exit %d, %v", code, printed)
	}
	if len(f.taskReads) != 0 {
		t.Errorf("the run after an offer wake read tasks %v: nothing moved", f.taskReads)
	}
	for _, c := range changesOf(printed) {
		if c["new"] == true {
			t.Errorf("a change reported with the offer is new again: %v", c)
		}
	}
	for _, q := range mapsOf(printed["questions"]) {
		if q["new"] == true {
			t.Errorf("a question reported with the offer is new again: %v", q)
		}
	}
}

// An offer on a first run takes the RD4-14 baseline, and the file it writes ends the first run: the
// next one does not take the baseline again.
func TestAnOfferWakeOnAFreshStateWritesTheFile(t *testing.T) {
	state := filepath.Join(t.TempDir(), "w.json")
	o := watcher(t, state)
	f := &fakeWatch{fakeBoard: &fakeBoard{offers: []interface{}{anOffer}, snapshots: snaps{{}}}, worked: worked(), roles: boardRoles}
	if code, _ := runWatch(t, o, f); code != waitExitWork {
		t.Fatalf("exit %d", code)
	}
	if !watchStateExists(state) {
		t.Fatal("an offer wake wrote no state file")
	}
	if kept := readWatchState(state); kept.Since != "2026-09-27T12:00:00Z" {
		t.Errorf("the cursor after the offer wake: %+v", kept)
	}
}

func TestATimeoutWritesTheState(t *testing.T) {
	seq := func(n int64) *int64 { return &n }
	state := filepath.Join(t.TempDir(), "w.json")
	o := watcher(t, state)
	// A quiet run on a fresh state: the timeout leaves the file with the sets and the cursor.
	f := &fakeWatch{fakeBoard: &fakeBoard{offers: []interface{}{nil}, snapshots: snaps{{snapEntry("RD-2", "QUEUED", "tester")}}},
		worked: worked("RD-2"), roles: boardRoles, details: []map[string]map[string]interface{}{{"u-RD-2": taskDetail("RD-2", "QUEUED", "tester",
			[]map[string]interface{}{signOff(me, "architect", "PASSED", "2026-09-27T09:00:00Z")}, nil, nil)}}}
	if code, printed := runWatch(t, o, f); code != waitExitTimeout {
		t.Fatalf("exit %d, %v", code, printed)
	}
	kept := readWatchState(state)
	if _, ok := kept.Tasks["u-RD-2"]; !ok || kept.Since == "" {
		t.Fatalf("the timeout left the state %+v", kept)
	}
	// A run whose only poll reads an event and then fails on the snapshot times out: the event prints
	// with the timeout, and the cursor past it is kept, so the next run reads on after it.
	o.timeout = time.Minute
	sf := &snapFails{fakeWatch: &fakeWatch{fakeBoard: &fakeBoard{offers: []interface{}{nil}, snapshots: snaps{{snapEntry("RD-2", "QUEUED", "tester")}},
		pages: []eventPage{{Events: []map[string]interface{}{{"seq": float64(30), "kind": "ALERT"}}, NextAfter: seq(30)}}},
		worked: worked("RD-2"), roles: boardRoles}, failOn: map[int]bool{0: true}}
	code, printed := runWatchOn(t, o, sf)
	if code != waitExitTimeout || printed["timeout"] != true {
		t.Fatalf("exit %d, %v: the timeout", code, printed)
	}
	if ev := mapsOf(printed["events"]); len(ev) != 1 || ev[0]["kind"] != "ALERT" || printed["nextAfter"].(float64) != 30 {
		t.Errorf("the timeout prints the event read before it: events %v, nextAfter %v", printed["events"], printed["nextAfter"])
	}
	if kept := readWatchState(state); kept.After == nil || *kept.After != 30 {
		t.Errorf("the timeout did not keep the cursor: %+v", kept)
	}
}

// With no state file and no poll that read the whole board, the exit writes nothing, so the next run
// still takes the first-run baseline.
func TestAFailedFirstPollLeavesNoStateFile(t *testing.T) {
	state := filepath.Join(t.TempDir(), "w.json")
	o := watcher(t, state)
	o.timeout = time.Minute
	sf := &snapFails{fakeWatch: &fakeWatch{fakeBoard: &fakeBoard{offers: []interface{}{nil}, snapshots: snaps{{}}}, worked: worked(), roles: boardRoles},
		failOn: map[int]bool{0: true}}
	if code, _ := runWatchOn(t, o, sf); code != waitExitTimeout {
		t.Fatalf("exit %d", code)
	}
	if _, err := os.Stat(state); err == nil {
		t.Error("a run that never read the board wrote a state file: the next run would lose its baseline")
	}
}

// An offer ends the wait even when the watch's read fails: it prints inside the watch object, with the
// events read before the failure, and the cursor past them is kept.
func TestAnOfferPrintsWhenTheWatchReadFails(t *testing.T) {
	seq := func(n int64) *int64 { return &n }
	state := filepath.Join(t.TempDir(), "w.json")
	o := watcher(t, state)
	// An earlier quiet run left the state file.
	if code, _ := runWatch(t, o, &fakeWatch{fakeBoard: &fakeBoard{snapshots: snaps{{}}}, worked: worked(), roles: boardRoles}); code != waitExitTimeout {
		t.Fatalf("the earlier run: exit %d", code)
	}
	sf := &snapFails{fakeWatch: &fakeWatch{fakeBoard: &fakeBoard{offers: []interface{}{anOffer}, snapshots: snaps{{}},
		pages: []eventPage{{Events: []map[string]interface{}{{"seq": float64(50), "kind": "PAUSED"}}, NextAfter: seq(50)}}},
		worked: worked(), roles: boardRoles}, failOn: map[int]bool{0: true}}
	code, printed := runWatchOn(t, o, sf)
	if code != waitExitWork || printed["offer"] == nil {
		t.Fatalf("exit %d, %v: the offer", code, printed)
	}
	if ch, ok := printed["changes"].([]interface{}); !ok || len(ch) != 0 {
		t.Errorf("changes %v: an empty list when the board was not read", printed["changes"])
	}
	if ev := mapsOf(printed["events"]); len(ev) != 1 || ev[0]["kind"] != "PAUSED" {
		t.Errorf("events %v: the event read before the failure prints with the offer", printed["events"])
	}
	if kept := readWatchState(state); kept.After == nil || *kept.After != 50 {
		t.Errorf("the offer exit did not keep the cursor past the event it printed: %+v", kept)
	}
}

// The watch object on every wake reason: the same keys, the offer inside it or null.
func TestTheWatchObjectOnEveryWakeReason(t *testing.T) {
	seq := func(n int64) *int64 { return &n }
	alert := []eventPage{{Events: []map[string]interface{}{{"seq": float64(40), "kind": "ALERT"}}, NextAfter: seq(40)}}
	cases := []struct {
		name  string
		board func() *fakeWatch
		code  int
		offer bool
	}{
		{"offer", func() *fakeWatch {
			return &fakeWatch{fakeBoard: &fakeBoard{offers: []interface{}{anOffer}, snapshots: snaps{{}}}, worked: worked(), roles: boardRoles}
		}, waitExitWork, true},
		{"change", func() *fakeWatch {
			return &fakeWatch{fakeBoard: &fakeBoard{snapshots: snaps{{snapEntry("RD-1", "QUEUED", "coder", "rel-test")}}},
				worked: worked("RD-1"), roles: boardRoles, details: []map[string]map[string]interface{}{{"u-RD-1": rejected()}}}
		}, waitExitWork, false},
		{"question", func() *fakeWatch {
			return &fakeWatch{fakeBoard: &fakeBoard{snapshots: snaps{{askedOf("RD-5")}}}, worked: worked(), roles: boardRoles}
		}, waitExitWork, false},
		{"event", func() *fakeWatch {
			return &fakeWatch{fakeBoard: &fakeBoard{snapshots: snaps{{}}, pages: alert}, worked: worked(), roles: boardRoles}
		}, waitExitWork, false},
		{"timeout", func() *fakeWatch {
			return &fakeWatch{fakeBoard: &fakeBoard{snapshots: snaps{{}}}, worked: worked(), roles: boardRoles}
		}, waitExitTimeout, false},
	}
	for _, c := range cases {
		o := watcher(t, filepath.Join(t.TempDir(), "w.json"))
		code, printed := runWatch(t, o, c.board())
		if code != c.code {
			t.Errorf("%s: exit %d, want %d: %v", c.name, code, c.code, printed)
			continue
		}
		for _, k := range []string{"offer", "changes", "questions", "events", "nextAfter"} {
			if _, ok := printed[k]; !ok {
				t.Errorf("%s: no %q in %v", c.name, k, printed)
			}
		}
		for _, k := range []string{"changes", "questions", "events"} {
			if _, ok := printed[k].([]interface{}); !ok {
				t.Errorf("%s: %q is %v, want a list", c.name, k, printed[k])
			}
		}
		if got := printed["offer"] != nil; got != c.offer {
			t.Errorf("%s: offer %v", c.name, printed["offer"])
		}
		if (printed["timeout"] == true) != (c.name == "timeout") {
			t.Errorf("%s: timeout %v", c.name, printed["timeout"])
		}
	}
}

// runWatchOn is runWatch for any client, such as a board whose reads fail.
func runWatchOn(t *testing.T, o waitOpts, c waitClient) (int, map[string]interface{}) {
	t.Helper()
	var out bytes.Buffer
	code := runWait(o, c, start(), &out)
	var printed map[string]interface{}
	if err := json.Unmarshal(out.Bytes(), &printed); err != nil {
		t.Fatalf("printed %q: %v", out.String(), err)
	}
	return code, printed
}
