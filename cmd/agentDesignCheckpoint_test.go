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
	}
	task := map[string]interface{}{"key": "SPLIT-7", "title": "t", "status": "QUEUED", "role": "coder"}
	if out := renderTaskBrief(&taskBrief{Key: "SPLIT-7", Role: "coder", Task: briefTaskFields(task)}); strings.Contains(out, "design checkpoint") {
		t.Errorf("a task without a record prints a checkpoint line:\n%s", out)
	}
}
