package cmd

import (
	"testing"
	"time"
)

// board agents (task RD3-5): the window and the line a row prints as.
func TestAgentsWindow(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	for w, from := range map[string]string{"24h": "2026-09-28T12:00:00Z", "7d": "2026-09-22T12:00:00Z", "2h": "2026-09-29T10:00:00Z"} {
		v, err := agentsWindow(w, now)
		if err != nil || v["from"] != from || v["to"] != "2026-09-29T12:00:00Z" {
			t.Errorf("%s: %v %v", w, v, err)
		}
	}
	for _, all := range []string{"all", "", " ALL "} {
		if v, err := agentsWindow(all, now); err != nil || len(v) != 0 {
			t.Errorf("%q is the board's life: %v %v", all, v, err)
		}
	}
	for _, bad := range []string{"7w", "d", "-3d", "0h", "week"} {
		if _, err := agentsWindow(bad, now); err == nil {
			t.Errorf("%q should be refused", bad)
		}
	}
}

func TestAgentLine(t *testing.T) {
	working := map[string]interface{}{
		"session": "5e55a0b1-0000-4000-8000-00000000d014", "agentName": "scully-coder", "roles": []interface{}{"coder"},
		"state":      map[string]interface{}{"kind": "WORKING", "taskKey": "RD3-5", "since": "2026-09-29T11:00:00Z"},
		"lastPollAt": "2026-09-29T11:59:00Z", "tasksCompleted": float64(2), "costMicros": float64(1234567), "cacheShare": 0.6,
		"stale": []interface{}{map[string]interface{}{"rule": "hopNoProgress", "message": "RD3-5 stalled"}},
	}
	want := "WORKING RD3-5 since 2026-09-29T11:00:00Z · scully-coder (5e55a0b1) · coder · last poll 2026-09-29T11:59:00Z · 2 done · $1.23 · cache 60% · STALE hopNoProgress"
	if got := agentLine(working); got != want {
		t.Errorf("working:\n got %s\nwant %s", got, want)
	}
	closed := map[string]interface{}{"session": "abc", "agentName": "old", "state": map[string]interface{}{"kind": "CLOSED",
		"since": "2026-09-28T10:00:00Z", "closedBy": map[string]interface{}{"name": "ops"}}}
	if got := agentLine(closed); got != "CLOSED by ops since 2026-09-28T10:00:00Z · old (abc)" {
		t.Errorf("closed: %s", got)
	}
	found := false
	for _, c := range agentBoardCmd.Commands() {
		found = found || c.Name() == "agents"
	}
	if !found {
		t.Error("agents is not a board verb")
	}
}
