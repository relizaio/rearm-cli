package cmd

import (
	"strings"
	"testing"

	rearm "github.com/relizaio/rearm-client-go"
)

// board show prints the board's document components and documents block (task e2ae9cfa), and the
// board export carries the documents block, so a file round-trips documents.prefix, shared and
// root; board show also prints the root resolved (task 0cc38817).
func TestBoardShowPrintsTheDocumentComponents(t *testing.T) {
	show := strings.Join(strings.Fields(rearm.AgentBoardProgrammatic_Operation), " ")
	if !strings.Contains(show, "documentComponents { specification component }") {
		t.Error("board show does not print the document components")
	}
	if !strings.Contains(show, "documents { prefix shared root }") {
		t.Error("board show does not print the documents block")
	}
	if !strings.Contains(show, "documentsRoot") {
		t.Error("board show does not print the resolved documents root")
	}
	export := strings.Join(strings.Fields(rearm.ExportBoard_Operation), " ")
	if !strings.Contains(export, "documents { prefix shared root }") {
		t.Error("board export does not carry the documents block")
	}
	if !strings.Contains(agentBoardShowCmd.Short, "document components") {
		t.Error("board show's help does not say it shows the document components")
	}
	if !strings.Contains(agentBoardShowCmd.Short, "documents root") {
		t.Error("board show's help does not say it shows the documents root")
	}
}
