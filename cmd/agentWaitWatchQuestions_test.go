package cmd

import (
	"path/filepath"
	"testing"
)

// A watch's questions (task RD4-14): a question wakes only while it is still the session's to answer
// (its task open, no round of the answering role's specification by the session since it was asked),
// a first run with no state takes the session's last sign-off on the task as its baseline, and the
// roles print by name with their uuids beside them.

// producingRoles is the board's roles with what each produces, as the watch's scope read returns them.
var producingRoles = []map[string]interface{}{
	{"uuid": "r-arch", "name": "Architect", "producesOutputs": []interface{}{map[string]interface{}{"specification": "ARCHITECTURE"}}},
	{"uuid": "r-coder", "name": "coder", "producesOutputs": []interface{}{map[string]interface{}{"specification": "DETAILED_DESIGN"}}},
	{"uuid": "r-tester", "name": "tester", "producesOutputs": []interface{}{map[string]interface{}{"specification": "BOARD_TEST_REPORT"}}},
}

// askedOn is a snapshot entry for a task whose question the coder asked the architect at 11:00.
func askedOn(key, status string) map[string]interface{} {
	e := snapEntry(key, status, "architect")
	e["waitingOn"] = map[string]interface{}{"askingRole": "r-coder", "answeringRole": "r-arch",
		"questionsRelease": "rel-q-" + key, "askedAt": "2026-09-27T11:00:00Z"}
	return e
}

// round is a document round on a task as the watch reads it, with the session that published it.
func round(uuid, spec, session, at string) map[string]interface{} {
	d := doc(uuid, spec, 1, at, "")
	d["document"].(map[string]interface{})["session"] = session
	return d
}

func questionsOf(printed map[string]interface{}) []map[string]interface{} {
	return mapsOf(printed["questions"])
}

func TestAQuestionOnACompletedOrCancelledTaskDoesNotWake(t *testing.T) {
	for _, status := range []string{"COMPLETED", "CANCELLED"} {
		o := watcher(t, filepath.Join(t.TempDir(), "w.json"))
		f := &fakeWatch{fakeBoard: &fakeBoard{snapshots: snaps{{askedOn("RD-4", status)}}}, worked: worked(), roles: producingRoles}
		if code, printed := runWatch(t, o, f); code != waitExitTimeout {
			t.Errorf("%s: a question on a closed task woke it: exit %d, %v", status, code, printed)
		}
	}
	// Beside an open one, only the open one is listed.
	o := watcher(t, filepath.Join(t.TempDir(), "w.json"))
	f := &fakeWatch{fakeBoard: &fakeBoard{snapshots: snaps{{askedOn("RD-4", "COMPLETED"), askedOn("RD-5", "QUEUED")}}},
		worked: worked(), roles: producingRoles}
	code, printed := runWatch(t, o, f)
	if qs := questionsOf(printed); code != waitExitWork || len(qs) != 1 || qs[0]["task"] != "RD-5" {
		t.Errorf("exit %d, questions %v: only RD-5's", code, printed["questions"])
	}
}

func TestAQuestionTheSessionAnsweredDoesNotWake(t *testing.T) {
	// The session published an ARCHITECTURE round on RD-4 after the question was asked: answered.
	answered := taskDetail("RD-4", "QUEUED", "architect", nil,
		[]map[string]interface{}{round("rel-arch2", "ARCHITECTURE", me, "2026-09-27T11:20:00Z")}, nil)
	o := watcher(t, filepath.Join(t.TempDir(), "w.json"))
	f := &fakeWatch{fakeBoard: &fakeBoard{snapshots: snaps{{askedOn("RD-4", "QUEUED")}}}, worked: worked("RD-4"), roles: producingRoles,
		details: []map[string]map[string]interface{}{{"u-RD-4": answered}}}
	if code, printed := runWatch(t, o, f); code != waitExitTimeout {
		t.Errorf("an answered question woke it: exit %d, %v", code, printed)
	}
}

func TestAnUnansweredQuestionOnAnOpenTaskWakes(t *testing.T) {
	cases := map[string][]map[string]interface{}{
		// The session's own round predates the question.
		"a round before askedAt": {round("rel-arch1", "ARCHITECTURE", me, "2026-09-27T10:00:00Z")},
		// Another session's round of the answering type is not the session's answer.
		"another session's round": {round("rel-arch2", "ARCHITECTURE", "other-s", "2026-09-27T11:20:00Z")},
		// A round of a type the answering role does not produce answers nothing.
		"a round of another type": {round("rel-notes", "DETAILED_DESIGN", me, "2026-09-27T11:20:00Z")},
		"no round at all":         nil,
	}
	for name, docs := range cases {
		d := taskDetail("RD-4", "QUEUED", "architect", nil, docs, nil)
		o := watcher(t, filepath.Join(t.TempDir(), "w.json"))
		f := &fakeWatch{fakeBoard: &fakeBoard{snapshots: snaps{{askedOn("RD-4", "QUEUED")}}}, worked: worked("RD-4"), roles: producingRoles,
			details: []map[string]map[string]interface{}{{"u-RD-4": d}}}
		code, printed := runWatch(t, o, f)
		if qs := questionsOf(printed); code != waitExitWork || len(qs) != 1 || qs[0]["task"] != "RD-4" || qs[0]["new"] != true {
			t.Errorf("%s: exit %d, questions %v", name, code, printed["questions"])
		}
	}
}

func TestAFreshStateTakesTheSessionsLastSignOffAsItsBaseline(t *testing.T) {
	state := filepath.Join(t.TempDir(), "w.json")
	o := watcher(t, state)
	// The question was asked at 11:00; the session signed off on RD-4 at 11:30 without an answering
	// round (it went on to other work): with no state, the question is not new.
	acted := taskDetail("RD-4", "QUEUED", "architect",
		[]map[string]interface{}{signOff(me, "architect", "PASSED", "2026-09-27T11:30:00Z")}, nil, nil)
	f := &fakeWatch{fakeBoard: &fakeBoard{snapshots: snaps{{askedOn("RD-4", "QUEUED")}}}, worked: worked("RD-4"), roles: producingRoles,
		details: []map[string]map[string]interface{}{{"u-RD-4": acted}}}
	if code, printed := runWatch(t, o, f); code != waitExitTimeout {
		t.Fatalf("an old question woke a fresh state: exit %d, %v", code, printed)
	}
	// It was seen, so the next run with the same board stays quiet too.
	if st := readWatchState(state); len(st.Questions) != 1 || st.Questions[0] != "RD-4 rel-q-RD-4" {
		t.Errorf("kept questions %v", st.Questions)
	}
	f = &fakeWatch{fakeBoard: &fakeBoard{snapshots: snaps{{askedOn("RD-4", "QUEUED")}}}, worked: worked("RD-4"), roles: producingRoles,
		details: []map[string]map[string]interface{}{{"u-RD-4": acted}}}
	if code, _ := runWatch(t, o, f); code != waitExitTimeout {
		t.Errorf("second run: exit %d", code)
	}

	// Asked after the session's last sign-off: a fresh state wakes on it.
	o = watcher(t, filepath.Join(t.TempDir(), "w.json"))
	earlier := taskDetail("RD-4", "QUEUED", "architect",
		[]map[string]interface{}{signOff(me, "architect", "PASSED", "2026-09-27T10:30:00Z")}, nil, nil)
	f = &fakeWatch{fakeBoard: &fakeBoard{snapshots: snaps{{askedOn("RD-4", "QUEUED")}}}, worked: worked("RD-4"), roles: producingRoles,
		details: []map[string]map[string]interface{}{{"u-RD-4": earlier}}}
	code, printed := runWatch(t, o, f)
	if qs := questionsOf(printed); code != waitExitWork || len(qs) != 1 || qs[0]["new"] != true {
		t.Errorf("a question after the last sign-off: exit %d, questions %v", code, printed["questions"])
	}
	// A task the session never worked has no baseline: its question is new.
	o = watcher(t, filepath.Join(t.TempDir(), "w.json"))
	f = &fakeWatch{fakeBoard: &fakeBoard{snapshots: snaps{{askedOn("RD-6", "QUEUED")}}}, worked: worked(), roles: producingRoles}
	if code, printed := runWatch(t, o, f); code != waitExitWork || len(questionsOf(printed)) != 1 {
		t.Errorf("a task never worked: exit %d, %v", code, printed["questions"])
	}
}

func TestQuestionsPrintRoleNamesWithTheUuidsBeside(t *testing.T) {
	o := watcher(t, filepath.Join(t.TempDir(), "w.json"))
	asked := askedOn("RD-5", "QUEUED")
	unknown := snapEntry("RD-6", "QUEUED", "architect")
	unknown["waitingOn"] = map[string]interface{}{"askingRole": "r-gone", "answeringRole": "r-arch", "questionsRelease": "rel-q6"}
	f := &fakeWatch{fakeBoard: &fakeBoard{snapshots: snaps{{asked, unknown}}}, worked: worked(), roles: producingRoles}
	code, printed := runWatch(t, o, f)
	qs := questionsOf(printed)
	if code != waitExitWork || len(qs) != 2 {
		t.Fatalf("exit %d, questions %v", code, printed["questions"])
	}
	q := qs[0]
	if q["askingRole"] != "coder" || q["askingRoleUuid"] != "r-coder" || q["answeringRole"] != "Architect" || q["answeringRoleUuid"] != "r-arch" {
		t.Errorf("question %v: names, with the uuids beside them", q)
	}
	// A role the board's list does not have prints its uuid rather than nothing.
	if qs[1]["askingRole"] != "r-gone" || qs[1]["askingRoleUuid"] != "r-gone" {
		t.Errorf("unknown role %v", qs[1])
	}
}

// A state file written before the session's own record existed reads each task once more, then not
// again; the change it kept is not reported twice.
func TestAStateWithoutTheSessionsRecordReadsEachTaskOnce(t *testing.T) {
	state := filepath.Join(t.TempDir(), "w.json")
	o := watcher(t, state)
	queued := snaps{{snapEntry("RD-1", "QUEUED", "coder", "rel-test")}}
	one := []map[string]map[string]interface{}{{"u-RD-1": rejected()}}
	if code, _ := runWatch(t, o, &fakeWatch{fakeBoard: &fakeBoard{snapshots: queued}, worked: worked("RD-1"), roles: producingRoles, details: one}); code != waitExitWork {
		t.Fatalf("first run: exit %d", code)
	}
	st := readWatchState(state)
	k := st.Tasks["u-RD-1"]
	k.Mine = nil
	st.Tasks["u-RD-1"] = k
	writeWatchState(state, st)
	f := &fakeWatch{fakeBoard: &fakeBoard{snapshots: queued}, worked: worked("RD-1"), roles: producingRoles, details: one}
	if code, _ := runWatch(t, o, f); code != waitExitTimeout || len(f.taskReads) != 1 {
		t.Errorf("upgraded state: exit %d, task reads %v: read once, the change not reported again", code, f.taskReads)
	}
	f = &fakeWatch{fakeBoard: &fakeBoard{snapshots: queued}, worked: worked("RD-1"), roles: producingRoles, details: one}
	if code, _ := runWatch(t, o, f); code != waitExitTimeout || len(f.taskReads) != 0 {
		t.Errorf("after: exit %d, task reads %v", code, f.taskReads)
	}
}
