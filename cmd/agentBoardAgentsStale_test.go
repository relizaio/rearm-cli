package cmd

import (
	"strings"
	"testing"
)

// RD3-5 T-2: the STALE list names each rule once, counting repeats, so the seat waiting on sixteen tasks
// reads seatSilent×16 rather than the name sixteen times.
func TestTheStaleListNamesEachRuleOnceWithACount(t *testing.T) {
	var stale []interface{}
	for i := 0; i < 16; i++ {
		stale = append(stale, map[string]interface{}{"rule": "seatSilent", "message": "waiting"})
	}
	stale = append(stale, map[string]interface{}{"rule": "hopNoProgress", "message": "stalled"})
	if got := staleRules(stale); got != "seatSilent×16,hopNoProgress" {
		t.Errorf("stale rules: %q", got)
	}
	line := agentLine(map[string]interface{}{"state": map[string]interface{}{"kind": "IDLE"}, "stale": stale})
	if !strings.Contains(line, "STALE seatSilent×16,hopNoProgress") || strings.Count(line, "seatSilent") != 1 {
		t.Errorf("the row line: %q", line)
	}
}
