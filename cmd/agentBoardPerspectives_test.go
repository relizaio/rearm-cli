package cmd

import (
	"strings"
	"testing"

	rearm "github.com/relizaio/rearm-client-go"
)

// board show prints the board's perspectives, by uuid and as the file names them, and the export
// carries them so a board file round-trips them (task b9115d09).
func TestBoardShowPrintsThePerspectives(t *testing.T) {
	if !strings.Contains(strings.Join(strings.Fields(rearm.AgentBoardProgrammatic_Operation), " "), "perspectives perspectiveNames") {
		t.Error("board show does not print the perspectives")
	}
	if !strings.Contains(strings.Join(strings.Fields(rearm.ExportBoard_Operation), " "), "perspectives") {
		t.Error("board export does not carry the perspectives")
	}
}
