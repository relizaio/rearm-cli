package cmd

import (
	"strings"
	"testing"

	rearm "github.com/relizaio/rearm-client-go"
)

// A pass that answers a review item about the signing role's own document can say its round changes nothing to build
// (task RD4-13): `task signoff --no-change` sends noChange true; without the flag nothing is sent, so the server
// records nothing and routes the round to the role that builds from it.

func TestSignoffNoChangeSendsTheStatement(t *testing.T) {
	withStateDir(t)
	if err := writeAgentState(&agentSessionState{SessionUuid: "s-1", ClientSessionId: "c-1", CurrentTask: "t-1"}); err != nil {
		t.Fatal(err)
	}
	f := &seenBoard{}
	srv := f.serve(t, seenTask("t-1"))
	defer srv.Close()
	useFake(t, srv)
	t.Cleanup(func() { taskNoChange = false })

	taskSessionUuid, taskOutcome, taskNoChange = "s-1", "PASSED", true
	agentTaskSignoffCmd.Run(agentTaskSignoffCmd, []string{"t-1"})
	if got := f.signed["noChange"]; got != true {
		t.Errorf("--no-change sent noChange %v, want true", got)
	}

	taskSessionUuid, taskOutcome, taskNoChange = "s-1", "PASSED", false
	agentTaskSignoffCmd.Run(agentTaskSignoffCmd, []string{"t-1"})
	if got, ok := f.signed["noChange"]; ok && got != nil {
		t.Errorf("a sign-off without --no-change sent noChange %v", got)
	}
}

func TestSignoffHasTheNoChangeFlagAndTheOperationCarriesIt(t *testing.T) {
	fl := agentTaskSignoffCmd.PersistentFlags().Lookup("no-change")
	if fl == nil || fl.Value.Type() != "bool" || fl.DefValue != "false" {
		t.Fatalf("signoff --no-change is missing or not a bool defaulting to false: %+v", fl)
	}
	if !strings.Contains(fl.Usage, "changes nothing to build") {
		t.Errorf("the flag's help does not say what it means: %q", fl.Usage)
	}
	if !strings.Contains(rearm.AgentTaskSignOffProgrammatic_Operation, "noChange: $noChange") {
		t.Error("the pinned client-go sign-off does not send noChange")
	}
}
