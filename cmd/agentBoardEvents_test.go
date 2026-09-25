package cmd

import (
	"errors"
	"strings"
	"testing"
)

// The read's variables (task 1c5442d2): one starting point, a valid time, the limit when set.
func TestBoardEventsVars(t *testing.T) {
	v, err := boardEventsVars("b1", 0, "", 0)
	if err != nil || v["boardUuid"] != "b1" || len(v) != 1 {
		t.Errorf("a bare read sends only the board: %v %v", v, err)
	}
	if v, _ := boardEventsVars("b1", 42, "", 25); v["after"] != int64(42) || v["limit"] != 25 {
		t.Errorf("--after and --limit: %v", v)
	}
	if v, _ := boardEventsVars("b1", 0, "2026-09-25T14:00:00Z", 0); v["since"] != "2026-09-25T14:00:00Z" {
		t.Errorf("--since: %v", v)
	}
	if _, err := boardEventsVars("b1", 3, "2026-09-25T14:00:00Z", 0); err == nil {
		t.Error("--after and --since together are refused")
	}
	if _, err := boardEventsVars("b1", 0, "yesterday", 0); err == nil {
		t.Error("a --since that is not a timestamp is refused")
	}
	for _, f := range []string{"after", "since", "limit", "json", "follow"} {
		if agentBoardEventsCmd.Flags().Lookup(f) == nil {
			t.Errorf("board events has no --%s", f)
		}
	}
}

func TestFormatEvent(t *testing.T) {
	line := formatEvent(map[string]interface{}{"seq": float64(61), "eventAt": "2026-09-25T14:00:00Z", "kind": "ALERT",
		"message": "Task x stopped", "actor": map[string]interface{}{"kind": "SYSTEM", "name": "routing"}})
	for _, want := range []string{"61", "2026-09-25T14:00:00Z", "ALERT", "routing", "Task x stopped"} {
		if !strings.Contains(line, want) {
			t.Errorf("%q lacks %q", line, want)
		}
	}
	if l := formatEvent(map[string]interface{}{"actor": map[string]interface{}{"kind": "SESSION", "uuid": "83922fa1-307e"}}); !strings.Contains(l, "session 83922fa1") {
		t.Errorf("an unnamed actor reads as its kind and a short id: %q", l)
	}
}

func i64(v int64) *int64 { return &v }

// --follow reads on from each page's nextAfter, drops --since after the first read, waits only
// once it has caught up, and ends on a failed read.
func TestFollowEvents(t *testing.T) {
	pages := []eventPage{
		{Events: []map[string]interface{}{{"message": "a"}, {"message": "b"}}, NextAfter: i64(2), HasMore: true},
		{Events: []map[string]interface{}{{"message": "c"}}, NextAfter: i64(3), HasMore: false},
		{Events: nil, NextAfter: i64(3), HasMore: false},
	}
	var asked []map[string]interface{}
	var shown []string
	waits := 0
	i := 0
	vars := map[string]interface{}{"boardUuid": "b1", "since": "2026-09-25T14:00:00Z"}
	err := followEvents(vars,
		func(v map[string]interface{}) (eventPage, error) {
			copyOf := map[string]interface{}{}
			for k, x := range v {
				copyOf[k] = x
			}
			asked = append(asked, copyOf)
			if i >= len(pages) {
				return eventPage{}, errors.New("gone")
			}
			p := pages[i]
			i++
			return p, nil
		},
		func(events []map[string]interface{}) {
			for _, e := range events {
				shown = append(shown, e["message"].(string))
			}
		},
		func() { waits++ },
		func() bool { return false })
	if err == nil || err.Error() != "gone" {
		t.Errorf("a failed read ends the follow with its error: %v", err)
	}
	if strings.Join(shown, "") != "abc" {
		t.Errorf("every event once, in order: %v", shown)
	}
	if asked[0]["since"] == nil || asked[1]["since"] != nil || asked[1]["after"] != int64(2) || asked[2]["after"] != int64(3) {
		t.Errorf("the first read starts from --since, the next ones after nextAfter: %v", asked)
	}
	if waits != 2 {
		t.Errorf("it waits only once caught up (after pages 2 and 3): %d", waits)
	}
}
