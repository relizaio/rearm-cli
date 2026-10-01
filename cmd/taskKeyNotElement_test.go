package cmd

import (
	"testing"
)

// The task prefixes a publish reserves (task RD2-28): the board's current one and every one it held,
// read from the board the publish already reads; none without a board.
func TestReservedTaskPrefixesComeFromTheBoard(t *testing.T) {
	got := reservedOf(map[string]interface{}{"taskPrefix": "RD2", "taskPrefixHistory": []interface{}{"RD", "RD2"}})
	if len(got) != 2 || !got["RD2"] || !got["RD"] {
		t.Errorf("reserved = %v, want RD2 and RD", got)
	}
	if got := reservedOf(map[string]interface{}{}); len(got) != 0 {
		t.Errorf("a board without prefixes reserves %v", got)
	}
	if got := reservedOf(nil); len(got) != 0 {
		t.Errorf("no board reserves %v", got)
	}

	board := map[string]interface{}{"taskPrefix": "RD2", "taskPrefixHistory": []interface{}{"RD2"}}
	extra, ix, err := elementsInput("ARCHITECTURE", []byte("# RD2-1 — Title\n\nSee REQ-1.\n"), board)
	if err != nil {
		t.Fatal(err)
	}
	if extra != nil || ix == nil || len(ix.Elements) != 0 {
		t.Errorf("a design headed by its key publishes elements %v (extra %v), want none", ix, extra)
	}
}
