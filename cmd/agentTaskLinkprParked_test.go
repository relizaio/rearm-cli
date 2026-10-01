package cmd

import (
	"strings"
	"testing"
)

// Task RD4-19: a link on a task the coordinator seat parked for the operator is accepted, recorded on the decision
// and posted as an INFO, and does not answer the question; task linkpr --help says so from its side.
func TestTheLinkprHelpSaysALinkOnAParkedTaskAnswersNothing(t *testing.T) {
	long := strings.Join(strings.Fields(agentTaskLinkprCmd.Long), " ")
	for _, want := range []string{
		"On a task the coordinator seat parked for the operator (task hold --operator --question) the link is accepted too",
		"recorded on the decision as the PR, your key's agent and the time, and posted as an INFO",
		"it does not answer the question or release the hold (task RD4-19)",
		"A person who will supersede a PR links its replacement first.",
	} {
		if !strings.Contains(long, want) {
			t.Errorf("task linkpr --help lacks %q", want)
		}
	}
}
