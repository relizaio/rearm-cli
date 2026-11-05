package cmd

import (
	"strings"
	"testing"
)

// The design checkpoint record as the CLI prints it (task SPLIT-7, parent SPLIT-5 T-17).

func pendingCheckpoint() map[string]interface{} {
	return map[string]interface{}{"state": "PENDING", "beforeRole": "coder", "afterRole": "reviewer",
		"reachedAt": "2026-10-30T06:00:00Z", "design": []interface{}{"r1"}}
}

// compactTask names the checkpoint only for a task waiting at the coordinator on a PENDING record.
func TestCompactTaskNamesTheDesignCheckpoint(t *testing.T) {
	for _, c := range []struct {
		name   string
		status string
		record map[string]interface{}
		want   string
	}{
		{"pending at the coordinator", "AWAITING_COORDINATOR", pendingCheckpoint(),
			"SPLIT-7 awaiting coordinator, role reviewer\nwaits for the coordinator: design checkpoint before coder"},
		{"sent on, at the coordinator again", "AWAITING_COORDINATOR",
			map[string]interface{}{"state": "SENT_ON", "beforeRole": "coder"},
			"SPLIT-7 awaiting coordinator, role reviewer\nwaits for the coordinator"},
		{"no record", "AWAITING_COORDINATOR", nil, "SPLIT-7 awaiting coordinator, role reviewer\nwaits for the coordinator"},
		{"queued with a pending record", "QUEUED", pendingCheckpoint(), "SPLIT-7 queued, role reviewer\nqueued for reviewer"},
	} {
		task := map[string]interface{}{"key": "SPLIT-7", "status": c.status, "role": "reviewer"}
		if c.record != nil {
			task["designCheckpoint"] = c.record
		}
		if got := compactTask(task, nil); got != c.want {
			t.Errorf("%s: printed\n%s\nwant\n%s", c.name, got, c.want)
		}
	}
}

// The brief prints one line for the record, by state, after the held line; none without a record.
func TestBriefPrintsTheDesignCheckpoint(t *testing.T) {
	seat := map[string]interface{}{"kind": "SESSION", "uuid": "s-1", "name": "rg-coord1"}
	for _, c := range []struct {
		name   string
		record map[string]interface{}
		want   string
	}{
		{"pending", pendingCheckpoint(),
			"- design checkpoint before coder: pending since 2026-10-30T06:00:00Z, reached after reviewer (1 design round)\n"},
		{"pending, no after role, two rounds", map[string]interface{}{"state": "PENDING", "beforeRole": "coder",
			"reachedAt": "2026-10-30T06:00:00Z", "design": []interface{}{"r1", "r2"}},
			"- design checkpoint before coder: pending since 2026-10-30T06:00:00Z (2 design rounds)\n"},
		{"sent on", map[string]interface{}{"state": "SENT_ON", "beforeRole": "coder", "settledBy": seat,
			"settledAt": "2026-10-30T06:05:00Z", "note": "authorized for coder"},
			"- design checkpoint before coder: sent on by rg-coord1 2026-10-30T06:05:00Z: authorized for coder\n"},
		{"split by an unnamed actor", map[string]interface{}{"state": "SPLIT", "beforeRole": "coder",
			"settledBy": map[string]interface{}{"kind": "SESSION", "uuid": "s-1"}, "settledAt": "2026-10-30T06:05:00Z",
			"note": "2 children: SPLIT-8, SPLIT-9"},
			"- design checkpoint before coder: split by session 2026-10-30T06:05:00Z: 2 children: SPLIT-8, SPLIT-9\n"},
		{"sent on, no note", map[string]interface{}{"state": "SENT_ON", "beforeRole": "coder", "settledBy": seat,
			"settledAt": "2026-10-30T06:05:00Z"},
			"- design checkpoint before coder: sent on by rg-coord1 2026-10-30T06:05:00Z\n"},
		// TPF-153 T-6 (a): the architect waived the checkpoint with --no-split; the server wrote the note.
		{"waived", map[string]interface{}{"state": "SENT_ON", "beforeRole": "coder",
			"settledBy": map[string]interface{}{"kind": "SESSION", "uuid": "s-arch", "name": "architect-seat"},
			"settledAt": "2026-11-05T03:00:00Z", "note": "waived by architect: fits one coder pass"},
			"- design checkpoint waived by architect: \"fits one coder pass\" (before coder, 2026-11-05T03:00:00Z)\n"},
		// T-6 (b): a blind reader's copy, settledBy null and the note cut to "waived by <role>".
		{"waived, blind reader", map[string]interface{}{"state": "SENT_ON", "beforeRole": "coder", "settledBy": nil,
			"settledAt": "2026-11-05T03:00:00Z", "note": "waived by architect"},
			"- design checkpoint waived by architect (before coder, 2026-11-05T03:00:00Z)\n"},
	} {
		task := map[string]interface{}{"key": "SPLIT-7", "title": "t", "status": "AWAITING_COORDINATOR", "role": "reviewer",
			"hold":             map[string]interface{}{"kind": "MANUAL", "level": "COORDINATOR", "reason": "r"},
			"designCheckpoint": c.record}
		out := renderTaskBrief(&taskBrief{Key: "SPLIT-7", Role: "coordinator", Task: briefTaskFields(task)})
		if !strings.Contains(out, "- held (MANUAL, COORDINATOR): r\n"+c.want) {
			t.Errorf("%s: the brief does not print %q after the held line:\n%s", c.name, c.want, out)
		}
		if strings.Contains(out, "waits") {
			t.Errorf("%s: the brief says waits", c.name)
		}
		// T-6 (c): only a note the server wrote with the waived prefix reads as waived.
		if !strings.HasPrefix(c.name, "waived") && strings.Contains(out, "waived") {
			t.Errorf("%s: the brief says waived", c.name)
		}
		if strings.Contains(out, "null") || strings.Contains(out, "<nil>") {
			t.Errorf("%s: the brief prints a placeholder:\n%s", c.name, out)
		}
	}
	task := map[string]interface{}{"key": "SPLIT-7", "title": "t", "status": "QUEUED", "role": "coder"}
	if out := renderTaskBrief(&taskBrief{Key: "SPLIT-7", Role: "coder", Task: briefTaskFields(task)}); strings.Contains(out, "design checkpoint") {
		t.Errorf("a task without a record prints a checkpoint line:\n%s", out)
	}
}

// TPF-153 T-6 (k): compactTask is unchanged by a waiver: a task queued after a waived pass says where it went.
func TestCompactTaskIgnoresAWaivedRecord(t *testing.T) {
	task := map[string]interface{}{"key": "SPLIT-7", "status": "QUEUED", "role": "coder",
		"designCheckpoint": map[string]interface{}{"state": "SENT_ON", "beforeRole": "coder",
			"settledAt": "2026-11-05T03:00:00Z", "note": "waived by architect: fits one coder pass"},
		"designCheckpointWaiver": architectWaiver("a1", true)}
	if got, want := compactTask(task, nil), "SPLIT-7 queued, role coder\nqueued for coder"; got != want {
		t.Errorf("printed\n%s\nwant\n%s", got, want)
	}
}

func architectWaiver(round string, standing bool) map[string]interface{} {
	return map[string]interface{}{"bySession": "s-arch", "byAgent": "ag-1", "role": "architect", "roleUuid": "r-arch",
		"at": "2026-11-05T02:59:00Z", "designRound": round, "note": "fits one coder pass", "standing": standing}
}

func architectureDocs(rounds ...string) []interface{} {
	var docs []interface{}
	for i, r := range rounds {
		docs = append(docs, map[string]interface{}{"uuid": r,
			"document": map[string]interface{}{"specification": "ARCHITECTURE", "round": float64(i + 1)}})
	}
	docs = append(docs, map[string]interface{}{"uuid": "t1",
		"document": map[string]interface{}{"specification": "THREAT_MODEL", "round": float64(7)}})
	return docs
}

// TPF-153 T-6 (d)-(l): the waiver line, right after the checkpoint line, with one clause read off the board's
// switch and the server's standing, never off the documents.
func TestBriefPrintsTheWaiver(t *testing.T) {
	waived := map[string]interface{}{"state": "SENT_ON", "beforeRole": "coder",
		"settledBy": map[string]interface{}{"kind": "SESSION", "uuid": "s-arch", "name": "architect-seat"},
		"settledAt": "2026-11-05T03:00:00Z", "note": "waived by architect: fits one coder pass"}
	checkpointLine := "- design checkpoint waived by architect: \"fits one coder pass\" (before coder, 2026-11-05T03:00:00Z)\n"
	head := "- waiver: --no-split by architect on ARCHITECTURE round 1 at 2026-11-05T02:59:00Z"
	const ignored = " (ignored: this board's architectCheckpointWaiver is off)"
	const notStanding = " (not standing: a newer design round, an advisory round or a reopen since)"
	blind := architectWaiver("a1", true)
	blind["bySession"], blind["byAgent"], blind["note"] = nil, nil, nil
	for _, c := range []struct {
		name      string
		record    map[string]interface{}
		waiver    map[string]interface{}
		docs      []interface{}
		switchOff bool
		want      string
	}{
		{"(d) standing, switch on", waived, architectWaiver("a1", true), architectureDocs("a1"), false,
			checkpointLine + head + "\n"},
		{"(e) switch off wins over not standing", pendingCheckpoint(), architectWaiver("a1", false), architectureDocs("a1"), true,
			head + ignored + "\n"},
		{"(f) not standing", pendingCheckpoint(), architectWaiver("a1", false), architectureDocs("a1", "a2"), false,
			head + notStanding + "\n"},
		{"(g) standing with a newer round in the documents", waived, architectWaiver("a1", true), architectureDocs("a1", "a2"), false,
			head + "\n"},
		{"(h) a round the read did not return", waived, architectWaiver("0123abcd-ffff-4000-8000-000000000000", true),
			architectureDocs("a1"), false, "- waiver: --no-split by architect on release 0123abcd at 2026-11-05T02:59:00Z\n"},
		{"(i) a waiver with no record", nil, architectWaiver("a1", true), architectureDocs("a1"), false,
			head + "\n"},
		{"(l) a blind reader's waiver", waived, blind, architectureDocs("a1"), false, checkpointLine + head + "\n"},
	} {
		task := map[string]interface{}{"key": "TPF-153", "title": "t", "status": "QUEUED", "role": "coder",
			"spentMicros": float64(0), "documents": c.docs, "designCheckpointWaiver": c.waiver}
		if c.record != nil {
			task["designCheckpoint"] = c.record
		}
		out := renderTaskBrief(&taskBrief{Key: "TPF-153", Role: "coder", Task: briefTaskFields(task),
			ArchitectCheckpointWaiverOff: c.switchOff})
		if !strings.Contains(out, c.want) {
			t.Errorf("%s: the brief does not print %q:\n%s", c.name, c.want, out)
		}
		if c.record == nil && !strings.Contains(out, "role coder\n"+head+"\n") {
			t.Errorf("%s: the waiver line does not stand alone after the status line:\n%s", c.name, out)
		}
		if c.switchOff && strings.Contains(out, "not standing") {
			t.Errorf("%s: both clauses printed:\n%s", c.name, out)
		}
		if !c.switchOff && strings.Contains(out, "ignored") {
			t.Errorf("%s: the ignored clause with the switch on:\n%s", c.name, out)
		}
		if c.waiver["standing"] == true && strings.Contains(out, "not standing") {
			t.Errorf("%s: a standing waiver reads not standing:\n%s", c.name, out)
		}
		if strings.Contains(out, "null") || strings.Contains(out, "<nil>") || strings.Contains(out, "s-arch") ||
			strings.Contains(out, "ag-1") {
			t.Errorf("%s: the waiver line prints a session, an agent or a placeholder:\n%s", c.name, out)
		}
	}
	// (d) the order: checkpoint, waiver, split request.
	task := map[string]interface{}{"key": "TPF-153", "title": "t", "status": "QUEUED", "role": "coder",
		"documents": architectureDocs("a1"), "designCheckpoint": waived, "designCheckpointWaiver": architectWaiver("a1", true),
		"splitRequest": map[string]interface{}{"state": "PENDING", "role": "coder", "requestedAt": "2026-11-05T04:00:00Z"}}
	out := renderTaskBrief(&taskBrief{Key: "TPF-153", Role: "coder", Task: briefTaskFields(task)})
	if i, j, k := strings.Index(out, "- design checkpoint waived"), strings.Index(out, "- waiver:"), strings.Index(out, "- split request"); !(i >= 0 && i < j && j < k) {
		t.Errorf("the lines are not checkpoint, waiver, split request (%d %d %d):\n%s", i, j, k, out)
	}
	// (j) no waiver, no line.
	delete(task, "designCheckpointWaiver")
	if out := renderTaskBrief(&taskBrief{Key: "TPF-153", Role: "coder", Task: briefTaskFields(task)}); strings.Contains(out, "waiver:") {
		t.Errorf("a task without a waiver prints a waiver line:\n%s", out)
	}
}
