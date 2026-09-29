package cmd

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A worker's wait with --watch (task RD4-3), against a scripted board: each wake reason, the documents
// since the session's last sign-off, the dedupe across polls and runs, the event cursor, an offer
// winning, and a wait without --watch waking on offers alone.

// fakeWatch is the worker's fake board plus what a watch reads: the session's tasks and the board's
// roles, and each task's hops and documents, scripted per read (the last read's repeats).
type fakeWatch struct {
	*fakeBoard
	worked     []map[string]interface{}
	roles      []map[string]interface{}
	details    []map[string]map[string]interface{}
	scopeReads int
	taskReads  [][]string
}

func (f *fakeWatch) scope(session, board string) ([]map[string]interface{}, []map[string]interface{}, error) {
	f.scopeReads++
	return f.worked, f.roles, nil
}

func (f *fakeWatch) tasks(uuids []string) ([]map[string]interface{}, error) {
	i := len(f.taskReads)
	f.taskReads = append(f.taskReads, uuids)
	if i >= len(f.details) {
		i = len(f.details) - 1
	}
	var out []map[string]interface{}
	for _, u := range uuids {
		if d, ok := f.details[i][u]; ok {
			out = append(out, d)
		}
	}
	return out, nil
}

const me = "s1"

func watcher(t *testing.T, state string) waitOpts {
	t.Helper()
	o, err := waitOptsOf(me, "b1", []string{"architect"}, false, 0, 60, 3*time.Minute, state)
	if err != nil {
		t.Fatal(err)
	}
	o, err = o.watching()
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func worked(keys ...string) []map[string]interface{} {
	var out []map[string]interface{}
	for _, k := range keys {
		out = append(out, map[string]interface{}{"uuid": "u-" + k, "key": k, "role": "architect", "board": "b1"})
	}
	// A task of another board the session worked is not watched here.
	return append(out, map[string]interface{}{"uuid": "u-OTHER", "key": "OT-1", "role": "architect", "board": "b2"})
}

var boardRoles = []map[string]interface{}{
	{"uuid": "r-arch", "name": "Architect"}, {"uuid": "r-coder", "name": "coder"}, {"uuid": "r-tester", "name": "tester"},
}

// snapEntry is a snapshot entry: the task's status and role and its latest document releases.
func snapEntry(key, status, role string, docs ...string) map[string]interface{} {
	e := entry(key, status, nil)
	e["task"].(map[string]interface{})["role"] = role
	var latest []interface{}
	for _, d := range docs {
		latest = append(latest, map[string]interface{}{"uuid": d, "lifecycle": "ASSEMBLED"})
	}
	e["latestDocuments"] = latest
	return e
}

func signOff(session, role, outcome, at string) map[string]interface{} {
	return map[string]interface{}{"session": session, "role": role, "outcome": outcome, "signedOffAt": at}
}

func doc(uuid, spec string, round int, at, verdict string) map[string]interface{} {
	ref := map[string]interface{}{"specification": spec, "round": float64(round), "path": "p/" + uuid + ".md", "publishedByRole": "tester"}
	if verdict != "" {
		ref["findings"] = map[string]interface{}{"verdict": verdict}
	}
	return map[string]interface{}{"uuid": uuid, "createdDate": at, "document": ref}
}

// taskDetail is a task as the watch reads it when it moved.
func taskDetail(key, status, role string, signOffs, docs []map[string]interface{}, extra map[string]interface{}) map[string]interface{} {
	d := map[string]interface{}{"key": key, "uuid": "u-" + key, "status": status, "role": role,
		"signOffs": toList(signOffs), "documents": toList(docs),
		"statusHistory": []interface{}{
			map[string]interface{}{"from": "QUEUED", "to": "ASSIGNED", "at": "2026-09-27T10:00:00Z", "trigger": "ASSIGN"},
			map[string]interface{}{"from": "ASSIGNED", "to": "AWAITING_COORDINATOR", "at": "2026-09-27T11:30:00.100Z", "trigger": "SIGNOFF"},
		}}
	for k, v := range extra {
		d[k] = v
	}
	return d
}

func toList(in []map[string]interface{}) []interface{} {
	out := []interface{}{}
	for _, m := range in {
		out = append(out, m)
	}
	return out
}

// rejected is RD-1 as it stands once the tester rejected it after the architect's pass: the architect's
// design round is older than its sign-off, the coder's notes and the tester's report are newer.
func rejected() map[string]interface{} {
	return taskDetail("RD-1", "QUEUED", "coder",
		[]map[string]interface{}{
			signOff(me, "architect", "PASSED", "2026-09-27T09:00:00Z"),
			signOff("coder-s", "coder", "PASSED", "2026-09-27T10:00:00Z"),
			signOff("tester-s", "tester", "REJECTED", "2026-09-27T11:30:00Z"),
		},
		[]map[string]interface{}{
			doc("rel-test", "TEST_REPORT", 1, "2026-09-27T11:29:00Z", "REJECTED"),
			doc("rel-notes", "DETAILED_DESIGN", 1, "2026-09-27T09:55:00Z", ""),
			doc("rel-arch", "ARCHITECTURE", 1, "2026-09-27T08:59:00Z", ""),
		}, nil)
}

func runWatch(t *testing.T, o waitOpts, f *fakeWatch) (int, map[string]interface{}) {
	t.Helper()
	var out bytes.Buffer
	code := runWait(o, f, start(), &out)
	var printed map[string]interface{}
	if err := json.Unmarshal(out.Bytes(), &printed); err != nil {
		t.Fatalf("printed %q: %v", out.String(), err)
	}
	return code, printed
}

func changesOf(printed map[string]interface{}) []map[string]interface{} {
	return mapsOf(printed["changes"])
}

func TestWatchWakesOnARejectionWithTheRoundsSinceTheSessionsSignOff(t *testing.T) {
	o := watcher(t, filepath.Join(t.TempDir(), "w.json"))
	f := &fakeWatch{fakeBoard: &fakeBoard{snapshots: snaps{{snapEntry("RD-1", "QUEUED", "coder", "rel-test")}}},
		worked: worked("RD-1"), roles: boardRoles, details: []map[string]map[string]interface{}{{"u-RD-1": rejected()}}}
	code, printed := runWatch(t, o, f)
	if code != waitExitWork {
		t.Fatalf("exit %d, want 0: %v", code, printed)
	}
	if _, ok := printed["offer"]; !ok || printed["offer"] != nil {
		t.Errorf("offer %v: a watch wake prints offer null", printed["offer"])
	}
	ch := changesOf(printed)
	if len(ch) != 1 {
		t.Fatalf("changes %v", printed["changes"])
	}
	c := ch[0]
	if c["task"] != "RD-1" || c["trigger"] != "REJECTED" || c["by"] != "tester" || c["from"] != "ASSIGNED" ||
		c["to"] != "QUEUED" || c["role"] != "coder" || c["at"] != "2026-09-27T11:30:00Z" || c["new"] != true {
		t.Errorf("change %v", c)
	}
	var docs []string
	for _, d := range mapsOf(c["documents"]) {
		docs = append(docs, d["release"].(string)+" "+d["specification"].(string))
	}
	if strings.Join(docs, ",") != "rel-notes DETAILED_DESIGN,rel-test TEST_REPORT" {
		t.Errorf("documents %v: the rounds since the session's sign-off, oldest first, not its own design", docs)
	}
	if v := mapsOf(c["documents"])[1]["verdict"]; v != "REJECTED" {
		t.Errorf("the test report's verdict %v", v)
	}
	if len(f.taskReads) != 1 || strings.Join(f.taskReads[0], ",") != "u-RD-1" {
		t.Errorf("task reads %v: only the session's tasks on this board", f.taskReads)
	}
}

func TestWatchWakesOnAReturnAndOnAReopen(t *testing.T) {
	returned := taskDetail("RD-2", "AWAITING_COORDINATOR", "coder",
		[]map[string]interface{}{signOff(me, "architect", "PASSED", "2026-09-27T09:00:00Z")}, nil,
		map[string]interface{}{"returns": []interface{}{map[string]interface{}{"session": "coder-s", "role": "coder",
			"reason": "ROLE_MISMATCH", "returnedAt": "2026-09-27T10:00:00Z"}}})
	reopened := taskDetail("RD-3", "QUEUED", "architect",
		[]map[string]interface{}{signOff(me, "architect", "PASSED", "2026-09-27T09:00:00Z"),
			signOff("tester-s", "tester", "PASSED", "2026-09-27T10:00:00Z")}, nil,
		map[string]interface{}{"reopens": []interface{}{map[string]interface{}{"role": "architect", "at": "2026-09-27T11:00:00Z",
			"reason": "the design missed a case", "by": map[string]interface{}{"kind": "USER", "name": "operator"}}},
			"statusHistory": []interface{}{map[string]interface{}{"from": "COMPLETED", "to": "QUEUED", "at": "2026-09-27T11:00:00Z", "trigger": "REOPEN"}}})
	o := watcher(t, filepath.Join(t.TempDir(), "w.json"))
	f := &fakeWatch{fakeBoard: &fakeBoard{snapshots: snaps{{snapEntry("RD-2", "AWAITING_COORDINATOR", "coder"), snapEntry("RD-3", "QUEUED", "architect")}}},
		worked: worked("RD-2", "RD-3"), roles: boardRoles,
		details: []map[string]map[string]interface{}{{"u-RD-2": returned, "u-RD-3": reopened}}}
	code, printed := runWatch(t, o, f)
	ch := changesOf(printed)
	if code != waitExitWork || len(ch) != 2 {
		t.Fatalf("exit %d, changes %v", code, printed["changes"])
	}
	if ch[0]["trigger"] != "RETURNED" || ch[0]["by"] != "coder" || ch[0]["reason"] != "ROLE_MISMATCH" {
		t.Errorf("the return %v", ch[0])
	}
	if ch[1]["trigger"] != "REOPENED" || ch[1]["by"] != "operator" || ch[1]["from"] != "COMPLETED" || ch[1]["to"] != "QUEUED" {
		t.Errorf("the reopen %v", ch[1])
	}
}

func TestWatchIsQuietOnAPassAndOnATaskTheSessionNeverSignedOff(t *testing.T) {
	passed := taskDetail("RD-1", "QUEUED", "tester", []map[string]interface{}{
		signOff(me, "architect", "PASSED", "2026-09-27T09:00:00Z"),
		signOff("tester-s", "tester", "REJECTED", "2026-09-27T10:00:00Z"),
		// The coder's fix passed after the rejection: the newest hop end is a pass.
		signOff("coder-s", "coder", "PASSED", "2026-09-27T11:00:00Z")}, nil, nil)
	// The session only returned this one; it never signed off on it.
	notMine := taskDetail("RD-2", "QUEUED", "architect", []map[string]interface{}{
		signOff("tester-s", "tester", "REJECTED", "2026-09-27T10:00:00Z")}, nil,
		map[string]interface{}{"returns": []interface{}{map[string]interface{}{"session": me, "role": "architect", "returnedAt": "2026-09-27T09:00:00Z"}}})
	// Rejected before the session's latest sign-off on it: already answered.
	answered := taskDetail("RD-3", "QUEUED", "coder", []map[string]interface{}{
		signOff("tester-s", "tester", "REJECTED", "2026-09-27T10:00:00Z"),
		signOff(me, "architect", "PASSED", "2026-09-27T10:30:00Z")}, nil, nil)
	o := watcher(t, filepath.Join(t.TempDir(), "w.json"))
	f := &fakeWatch{fakeBoard: &fakeBoard{snapshots: snaps{{snapEntry("RD-1", "QUEUED", "tester"), snapEntry("RD-2", "QUEUED", "architect"), snapEntry("RD-3", "QUEUED", "coder")}}},
		worked: worked("RD-1", "RD-2", "RD-3"), roles: boardRoles,
		details: []map[string]map[string]interface{}{{"u-RD-1": passed, "u-RD-2": notMine, "u-RD-3": answered}}}
	if code, printed := runWatch(t, o, f); code != waitExitTimeout {
		t.Errorf("exit %d, want the timeout: %v", code, printed)
	}
}

func TestWatchWakesOnAQuestionToItsRoleOnly(t *testing.T) {
	asked := snapEntry("RD-5", "QUEUED", "architect")
	asked["waitingOn"] = map[string]interface{}{"askingRole": "r-coder", "answeringRole": "r-arch", "questionsRelease": "rel-q", "askedAt": "2026-09-27T11:00:00Z"}
	other := snapEntry("RD-6", "QUEUED", "tester")
	other["waitingOn"] = map[string]interface{}{"askingRole": "r-tester", "answeringRole": "r-coder", "questionsRelease": "rel-q2"}
	// --role architect matches the role named Architect, in any case.
	o := watcher(t, filepath.Join(t.TempDir(), "w.json"))
	f := &fakeWatch{fakeBoard: &fakeBoard{snapshots: snaps{{asked, other}}}, worked: worked(), roles: boardRoles}
	code, printed := runWatch(t, o, f)
	qs := mapsOf(printed["questions"])
	if code != waitExitWork || len(qs) != 1 || qs[0]["task"] != "RD-5" || qs[0]["questionsRelease"] != "rel-q" || qs[0]["new"] != true {
		t.Fatalf("exit %d, questions %v", code, printed["questions"])
	}
	// Without --role, the roles the session worked on this board: a coder session sees RD-6's.
	o2, _ := waitOptsOf(me, "b1", nil, false, 0, 60, 3*time.Minute, filepath.Join(t.TempDir(), "w.json"))
	o2, _ = o2.watching()
	coderWorked := []map[string]interface{}{{"uuid": "u-RD-9", "key": "RD-9", "role": "coder", "board": "b1"}}
	f = &fakeWatch{fakeBoard: &fakeBoard{snapshots: snaps{{asked, other}}}, worked: coderWorked, roles: boardRoles,
		details: []map[string]map[string]interface{}{{}}}
	code, printed = runWatch(t, o2, f)
	if qs := mapsOf(printed["questions"]); code != waitExitWork || len(qs) != 1 || qs[0]["task"] != "RD-6" {
		t.Errorf("roles worked: exit %d, questions %v", code, printed["questions"])
	}
}

func TestWatchWakesOnAlertLockedAndUnlockedButNotInfo(t *testing.T) {
	seq := func(n int64) *int64 { return &n }
	o := watcher(t, filepath.Join(t.TempDir(), "w.json"))
	f := &fakeWatch{fakeBoard: &fakeBoard{snapshots: snaps{{}}, pages: []eventPage{
		{Events: []map[string]interface{}{{"seq": float64(10), "kind": "INFO"}}, NextAfter: seq(10)}}}, worked: worked(), roles: boardRoles}
	if code, _ := runWatch(t, o, f); code != waitExitTimeout {
		t.Errorf("an INFO woke it: exit %d", code)
	}
	for _, kind := range []string{"ALERT", "LOCKED", "UNLOCKED"} {
		f := &fakeWatch{fakeBoard: &fakeBoard{snapshots: snaps{{}}, pages: []eventPage{
			{Events: []map[string]interface{}{{"seq": float64(11), "kind": "INFO"}, {"seq": float64(12), "kind": kind}}, NextAfter: seq(12)}}},
			worked: worked(), roles: boardRoles}
		o := watcher(t, filepath.Join(t.TempDir(), "w.json"))
		code, printed := runWatch(t, o, f)
		events := mapsOf(printed["events"])
		if code != waitExitWork || len(events) != 1 || events[0]["kind"] != kind || printed["nextAfter"].(float64) != 12 {
			t.Errorf("%s: exit %d, events %v, nextAfter %v", kind, code, printed["events"], printed["nextAfter"])
		}
	}
}

func TestWatchKeepsTheEventCursorBetweenRuns(t *testing.T) {
	seq := func(n int64) *int64 { return &n }
	state := filepath.Join(t.TempDir(), "w.json")
	o := watcher(t, state)
	f := &fakeWatch{fakeBoard: &fakeBoard{snapshots: snaps{{}}, pages: []eventPage{
		{Events: []map[string]interface{}{{"seq": float64(20), "kind": "ALERT"}}, NextAfter: seq(20)}}}, worked: worked(), roles: boardRoles}
	runWatch(t, o, f)
	if !strings.HasPrefix(f.eventCalls[0], "since=2026-09-27T12:00:00Z") {
		t.Errorf("first run read %v: from the moment it started", f.eventCalls)
	}
	// The next run, without --after, reads on after 20, and its timeout prints the cursor.
	f = &fakeWatch{fakeBoard: &fakeBoard{snapshots: snaps{{}}}, worked: worked(), roles: boardRoles}
	code, printed := runWatch(t, o, f)
	if code != waitExitTimeout || f.eventCalls[0] != "after=20" || printed["nextAfter"].(float64) != 20 {
		t.Errorf("second run: exit %d, read %v, printed %v", code, f.eventCalls, printed)
	}
	// --after wins over what was kept.
	o.after = 7
	f = &fakeWatch{fakeBoard: &fakeBoard{snapshots: snaps{{}}}, worked: worked(), roles: boardRoles}
	runWatch(t, o, f)
	if f.eventCalls[0] != "after=7" {
		t.Errorf("--after 7 read %v", f.eventCalls)
	}
}

func TestWatchReportsAChangeOnceAndASecondRejectionAgain(t *testing.T) {
	state := filepath.Join(t.TempDir(), "w.json")
	o := watcher(t, state)
	queued := snaps{{snapEntry("RD-1", "QUEUED", "coder", "rel-test")}}
	one := []map[string]map[string]interface{}{{"u-RD-1": rejected()}}
	if code, _ := runWatch(t, o, &fakeWatch{fakeBoard: &fakeBoard{snapshots: queued}, worked: worked("RD-1"), roles: boardRoles, details: one}); code != waitExitWork {
		t.Fatalf("first run: exit %d", code)
	}
	// The same snapshot: nothing moved, nothing is read again, and it does not wake twice.
	f := &fakeWatch{fakeBoard: &fakeBoard{snapshots: queued}, worked: worked("RD-1"), roles: boardRoles, details: one}
	if code, printed := runWatch(t, o, f); code != waitExitTimeout || len(f.taskReads) != 0 {
		t.Errorf("unchanged: exit %d, task reads %v, %v", code, f.taskReads, printed)
	}
	// The coder takes it: the task moved and is read, but the change is the one reported.
	taken := rejected()
	taken["status"] = "ASSIGNED"
	f = &fakeWatch{fakeBoard: &fakeBoard{snapshots: snaps{{snapEntry("RD-1", "ASSIGNED", "coder", "rel-test")}}}, worked: worked("RD-1"), roles: boardRoles,
		details: []map[string]map[string]interface{}{{"u-RD-1": taken}}}
	if code, _ := runWatch(t, o, f); code != waitExitTimeout || len(f.taskReads) != 1 {
		t.Errorf("picked up: exit %d, task reads %v", code, f.taskReads)
	}
	// The coder's fix passes, the tester rejects again: a new rejection wakes it, marked new.
	again := rejected()
	again["signOffs"] = append(again["signOffs"].([]interface{}),
		signOff("coder-s", "coder", "PASSED", "2026-09-27T12:00:00Z"), signOff("tester-s", "tester", "REJECTED", "2026-09-27T12:30:00Z"))
	again["documents"] = append([]interface{}{doc("rel-test2", "TEST_REPORT", 2, "2026-09-27T12:29:00Z", "REJECTED")}, again["documents"].([]interface{})...)
	f = &fakeWatch{fakeBoard: &fakeBoard{snapshots: snaps{{snapEntry("RD-1", "QUEUED", "coder", "rel-test2")}}}, worked: worked("RD-1"), roles: boardRoles,
		details: []map[string]map[string]interface{}{{"u-RD-1": again}}}
	code, printed := runWatch(t, o, f)
	ch := changesOf(printed)
	if code != waitExitWork || len(ch) != 1 || ch[0]["at"] != "2026-09-27T12:30:00Z" || ch[0]["new"] != true {
		t.Fatalf("second rejection: exit %d, %v", code, printed["changes"])
	}
	if n := len(mapsOf(ch[0]["documents"])); n != 3 {
		t.Errorf("documents since the session's sign-off: %d, want the notes and both reports", n)
	}
}

func TestWatchQuestionsDedupeAndAnEmptiedSetComesBack(t *testing.T) {
	state := filepath.Join(t.TempDir(), "w.json")
	o := watcher(t, state)
	asked := snapEntry("RD-5", "QUEUED", "architect")
	asked["waitingOn"] = map[string]interface{}{"answeringRole": "r-arch", "questionsRelease": "rel-q"}
	withQ := snaps{{asked}}
	runWatch(t, o, &fakeWatch{fakeBoard: &fakeBoard{snapshots: withQ}, worked: worked(), roles: boardRoles})
	if code, _ := runWatch(t, o, &fakeWatch{fakeBoard: &fakeBoard{snapshots: withQ}, worked: worked(), roles: boardRoles}); code != waitExitTimeout {
		t.Errorf("the same question woke it twice: exit %d", code)
	}
	// Answered (gone), then asked again with the same release: wakes again.
	back := snaps{{snapEntry("RD-5", "QUEUED", "architect")}, {asked}}
	if code, printed := runWatch(t, o, &fakeWatch{fakeBoard: &fakeBoard{snapshots: back}, worked: worked(), roles: boardRoles}); code != waitExitWork {
		t.Errorf("an emptied set coming back: exit %d, %v", code, printed)
	}
}

func TestAnOfferWinsOverAWatchChange(t *testing.T) {
	offer := map[string]interface{}{"task": map[string]interface{}{"uuid": "t9"}, "role": "architect"}
	o := watcher(t, filepath.Join(t.TempDir(), "w.json"))
	f := &fakeWatch{fakeBoard: &fakeBoard{offers: []interface{}{offer}, snapshots: snaps{{snapEntry("RD-1", "QUEUED", "coder", "rel-test")}}},
		worked: worked("RD-1"), roles: boardRoles, details: []map[string]map[string]interface{}{{"u-RD-1": rejected()}}}
	code, printed := runWatch(t, o, f)
	if code != waitExitWork || printed["role"] != "architect" || printed["changes"] != nil {
		t.Errorf("exit %d, printed %v: the offer, as without --watch", code, printed)
	}
	if f.polls != 0 || f.scopeReads != 0 || len(f.eventCalls) != 0 {
		t.Errorf("an offer read the snapshot %d, the scope %d, the events %v times", f.polls, f.scopeReads, f.eventCalls)
	}
	// The change was not reported, so the next run reports it.
	f.offers = []interface{}{nil}
	f.nextCalls = nil
	if code, printed := runWatch(t, o, f); code != waitExitWork || len(changesOf(printed)) != 1 {
		t.Errorf("after the offer: exit %d, %v", code, printed)
	}
}

func TestWithoutWatchOnlyAnOfferWakesAWorker(t *testing.T) {
	seq := int64(5)
	f := &fakeWatch{fakeBoard: &fakeBoard{offers: []interface{}{nil}, snapshots: snaps{{snapEntry("RD-1", "QUEUED", "coder", "rel-test")}},
		pages: []eventPage{{Events: []map[string]interface{}{{"seq": float64(5), "kind": "ALERT"}}, NextAfter: &seq}}},
		worked: worked("RD-1"), roles: boardRoles, details: []map[string]map[string]interface{}{{"u-RD-1": rejected()}}}
	o, _ := waitOptsOf(me, "b1", []string{"architect"}, false, 0, 60, 3*time.Minute, "")
	var out bytes.Buffer
	if code := runWait(o, f, start(), &out); code != waitExitTimeout {
		t.Errorf("exit %d, want the timeout: %s", code, out.String())
	}
	if f.polls != 0 || f.scopeReads != 0 || len(f.eventCalls) != 0 || len(f.taskReads) != 0 {
		t.Errorf("a plain worker read more than next: snapshot %d, scope %d, events %v, tasks %v", f.polls, f.scopeReads, f.eventCalls, f.taskReads)
	}
}

func TestTheWatchFlagIsCheckedAndKeepsItsOwnState(t *testing.T) {
	o, _ := waitOptsOf(me, "", nil, false, 0, 60, time.Hour, "")
	if _, err := o.watching(); err == nil || !strings.Contains(err.Error(), "--board") {
		t.Errorf("--watch without --board: %v", err)
	}
	o, _ = waitOptsOf(me, "b1", nil, true, 0, 60, time.Hour, "x")
	if _, err := o.watching(); err == nil {
		t.Error("--watch with --coordinator accepted")
	}
	o, _ = waitOptsOf(me, "b1", nil, false, 0, 60, time.Hour, "")
	w, err := o.watching()
	if err != nil || !w.watch || !strings.HasSuffix(w.statePath, filepath.Join(".rearm", "wait-b1-watch-s1.json")) {
		t.Errorf("default state %q (%v): its own file, not the coordinator's", w.statePath, err)
	}
	if agentWaitCmd.Flags().Lookup("watch") == nil || !strings.Contains(agentWaitCmd.Long, "--watch") {
		t.Error("no --watch flag, or the help does not describe it")
	}
}
