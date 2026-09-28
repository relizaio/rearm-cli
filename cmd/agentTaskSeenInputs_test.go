package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	rearm "github.com/relizaio/rearm-client-go"
)

// What a hop read of its task (task RD2-34): `task show --session` and `task assign` print the task's documents and
// record them for the session working it; `task signoff` sends them as seenInputs, with --seen added, and
// forgets them once the sign-off is accepted.

// seenBoard answers every operation with the task below and captures the sign-off's variables.
type seenBoard struct {
	mu     sync.Mutex
	signed map[string]any
}

func (f *seenBoard) serve(t *testing.T, task map[string]any) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		data := map[string]any{}
		switch {
		case strings.Contains(req.Query, "agentTaskSignOffProgrammatic("):
			f.mu.Lock()
			f.signed = req.Variables
			f.mu.Unlock()
			data["agentTaskSignOffProgrammatic"] = map[string]any{"uuid": task["uuid"]}
		case strings.Contains(req.Query, "agentTaskAssignProgrammatic("):
			data["agentTaskAssignProgrammatic"] = map[string]any{"task": task, "role": "coder"}
		default:
			data["agentTaskProgrammatic"] = task
			data["agentTasksByUuidProgrammatic"] = []any{task}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
}

func useFake(t *testing.T, srv *httptest.Server) {
	t.Helper()
	c, err := rearm.New(srv.URL, "id", "secret", rearm.WithoutTokenExchange())
	if err != nil {
		t.Fatal(err)
	}
	apiClient = c
	t.Cleanup(func() {
		apiClient, taskSessionUuid, taskOutcome, taskSeen, taskShowSession, taskOutputs = nil, "", "", nil, "", nil
	})
}

func seenTask(uuid string, releases ...string) map[string]any {
	docs := []any{}
	for _, r := range releases {
		docs = append(docs, map[string]any{"uuid": r, "document": map[string]any{"specification": "ARCHITECTURE"}})
	}
	return map[string]any{"uuid": uuid, "key": "RD-1", "documents": docs}
}

// Tester run 1 T-1: a show with no session recorded for every local state holding the task, so any read on the
// host acknowledged for the holder. Only a read the session made counts (architecture-3 §1).
func TestShowWithoutASessionRecordsNothingEvenForTheHolder(t *testing.T) {
	withStateDir(t)
	if err := writeAgentState(&agentSessionState{SessionUuid: "s-1", ClientSessionId: "c-1", CurrentTask: "t-1"}); err != nil {
		t.Fatal(err)
	}
	f := &seenBoard{}
	srv := f.serve(t, seenTask("t-1", "r-1", "r-2"))
	defer srv.Close()
	useFake(t, srv)
	agentTaskShowCmd.Run(agentTaskShowCmd, []string{"t-1"})
	if got := lookupAgentState("s-1").SeenInputs; len(got) != 0 {
		t.Errorf("a sessionless show acknowledged for the holder: %v", got)
	}
	taskSessionUuid, taskOutcome = "s-1", "PASSED"
	agentTaskSignoffCmd.Run(agentTaskSignoffCmd, []string{"t-1"})
	if got := f.signed["seenInputs"]; !reflect.DeepEqual(got, []any{}) {
		t.Errorf("the holder's sign-off sent %v after a bystander's show, want []", got)
	}
}

func TestShowWithASessionRecordsIntoThatStateOnly(t *testing.T) {
	withStateDir(t)
	for _, st := range []*agentSessionState{
		{SessionUuid: "s-1", ClientSessionId: "c-1", CurrentTask: "t-1"},
		{SessionUuid: "s-2", ClientSessionId: "c-2", CurrentTask: "t-1"},
	} {
		if err := writeAgentState(st); err != nil {
			t.Fatal(err)
		}
	}
	f := &seenBoard{}
	srv := f.serve(t, seenTask("t-1", "r-1", "r-2"))
	defer srv.Close()
	useFake(t, srv)
	taskShowSession = "s-1"
	agentTaskShowCmd.Run(agentTaskShowCmd, []string{"t-1"})
	if got := lookupAgentState("s-1").SeenInputs["t-1"]; !reflect.DeepEqual(got, []string{"r-1", "r-2"}) {
		t.Errorf("the named session recorded %v", got)
	}
	if got := lookupAgentState("s-2").SeenInputs; len(got) != 0 {
		t.Errorf("another state holding the same task recorded %v", got)
	}
}

func TestAssignRecordsAndSignOffSendsWhatWasReadThenForgetsIt(t *testing.T) {
	withStateDir(t)
	if err := writeAgentState(&agentSessionState{SessionUuid: "s-1", ClientSessionId: "c-1"}); err != nil {
		t.Fatal(err)
	}
	f := &seenBoard{}
	srv := f.serve(t, seenTask("t-1", "r-1"))
	defer srv.Close()
	useFake(t, srv)
	taskSessionUuid = "s-1"
	agentTaskAssignCmd.Run(agentTaskAssignCmd, []string{"t-1"})
	if got := lookupAgentState("s-1").SeenInputs["t-1"]; !reflect.DeepEqual(got, []string{"r-1"}) {
		t.Fatalf("the assign recorded %v", got)
	}
	taskOutcome, taskSeen = "PASSED", []string{"r-9"}
	agentTaskSignoffCmd.Run(agentTaskSignoffCmd, []string{"t-1"})
	if got := f.signed["seenInputs"]; !reflect.DeepEqual(got, []any{"r-1", "r-9"}) {
		t.Errorf("the sign-off sent seenInputs %v", got)
	}
	if got := lookupAgentState("s-1").SeenInputs["t-1"]; got != nil {
		t.Errorf("still recorded after the sign-off: %v", got)
	}
}

func TestASessionThatReadNothingSendsAnEmptyList(t *testing.T) {
	withStateDir(t)
	if err := writeAgentState(&agentSessionState{SessionUuid: "s-1", ClientSessionId: "c-1"}); err != nil {
		t.Fatal(err)
	}
	f := &seenBoard{}
	srv := f.serve(t, seenTask("t-1"))
	defer srv.Close()
	useFake(t, srv)
	taskSessionUuid, taskOutcome = "s-1", "PASSED"
	agentTaskSignoffCmd.Run(agentTaskSignoffCmd, []string{"t-1"})
	if got, ok := f.signed["seenInputs"]; !ok || !reflect.DeepEqual(got, []any{}) {
		t.Errorf("tracked but read nothing: seenInputs %v (sent %v), want []", got, ok)
	}
}

func TestASessionWithNoLocalStateSendsNoSeenInputs(t *testing.T) {
	withStateDir(t)
	f := &seenBoard{}
	srv := f.serve(t, seenTask("t-1"))
	defer srv.Close()
	useFake(t, srv)
	taskSessionUuid, taskOutcome = "s-elsewhere", "PASSED"
	agentTaskSignoffCmd.Run(agentTaskSignoffCmd, []string{"t-1"})
	if got, ok := f.signed["seenInputs"]; ok {
		t.Errorf("a session this machine does not track sent seenInputs %v; the server check is for callers that track", got)
	}
}
