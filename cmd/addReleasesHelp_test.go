package cmd

import (
	"os"
	"strings"
	"testing"
)

// FDAW-87 (ARCHITECTURE round 3, 3.9 and section 4 row 13): the help of addreleases and its README
// paragraph say that what rules sent for an earlier release of a refused batch is not recalled. The
// sentence is written out here as the design gives it; each check keeps a neighbour on its left.
const batchNote = "Actions that rules fire when a release is saved (integration triggers, external validations," +
	" emails, pull request comments, VDR snapshots) and the pull request check run are sent as each release is" +
	" saved and are not recalled when a later release of the batch is refused."

func TestAddReleasesHelpAndReadmeSayWhatARefusedBatchDoesNotRecall(t *testing.T) {
	help := strings.Join(strings.Fields(addReleasesCmd.Long), " ")
	if !strings.Contains(help, "Creates several releases on ReARM in one all-or-nothing call. "+batchNote) {
		t.Errorf("the addreleases help does not carry the batch note after its first sentence: %s", help)
	}
	raw, err := os.ReadFile("../README.md")
	if err != nil {
		t.Fatal(err)
	}
	readme := strings.Join(strings.Fields(string(raw)), " ")
	if !strings.Contains(readme, "none are persisted. "+batchNote+" This is the batch counterpart to `addrelease`") {
		t.Error("the README's addreleases paragraph does not carry the batch note between its two kept sentences")
	}
}
