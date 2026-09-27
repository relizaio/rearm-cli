package cmd

import (
	"strings"
	"testing"

	rearm "github.com/relizaio/rearm-client-go"
)

// Advisory rounds (task e97fde56): doc publish --advisory sends the flag, and its release is not
// remembered as an output of the publisher's own sign-off.
func TestAdvisoryPublishSendsTheFlagAndIsNoOutput(t *testing.T) {
	defer func() { docAdvisory = false }()

	input := map[string]interface{}{"specification": "ARCHITECTURE"}
	applyAdvisory(input)
	if _, sent := input["advisory"]; sent || !remembersAsOutput() {
		t.Fatal("without --advisory nothing is sent and the release is an output as before")
	}

	if err := agentDocPublishCmd.Flags().Set("advisory", "true"); err != nil {
		t.Fatal(err)
	}
	applyAdvisory(input)
	if input["advisory"] != true {
		t.Errorf("--advisory sends advisory: true, got %v", input)
	}
	if remembersAsOutput() {
		t.Error("an advisory round must not be offered at the publisher's sign-off")
	}
	// An index-only publish sends it too (T-1): the server then refuses it as an index type.
	idx := indexOnlyInput("s-1", "REVIEW_FINDINGS", map[string]interface{}{"kind": "REVIEW_FINDINGS"})
	if idx["advisory"] != true || idx["specification"] != "REVIEW_FINDINGS" || idx["sessionUuid"] != "s-1" {
		t.Errorf("--advisory on --index-only sends advisory: true, got %v", idx)
	}
	docAdvisory = false
	if _, sent := indexOnlyInput("s-1", "QUESTIONS", map[string]interface{}{})["advisory"]; sent {
		t.Error("without --advisory an index-only publish sends nothing")
	}
	docAdvisory = true
	if !strings.Contains(agentDocPublishCmd.Long, "--advisory") {
		t.Error("the help should say what --advisory is for")
	}
}

// task show prints whether each round is advisory and the role it was published as.
func TestTaskShowReadsAdvisoryRounds(t *testing.T) {
	show := strings.Join(strings.Fields(rearm.AgentTaskProgrammatic_Operation), " ")
	if !strings.Contains(show, "advisory publishedByRole") {
		t.Error("task show does not read advisory rounds")
	}
}
