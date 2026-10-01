package cmd

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// Unassigning a stalled task (task RD3-4): the coordinator's and the person's unassign verbs send
// the task, the reason and, for the seat, the session; a sign-off or return refused because the hop
// was unassigned forgets the task locally, and no other refusal does.

type unassignBoard struct {
	mu   sync.Mutex
	sent map[string]map[string]any
}

func (f *unassignBoard) serve() *httptest.Server {
	f.sent = map[string]map[string]any{}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		data := map[string]any{}
		task := map[string]any{"uuid": "t-1", "key": "RD-7", "status": "QUEUED", "role": "coder"}
		for _, op := range []string{"agentTaskUnassignProgrammatic", "agentTaskUnassign"} {
			if strings.Contains(req.Query, op+"(") {
				f.mu.Lock()
				f.sent[op] = req.Variables
				f.mu.Unlock()
				data[op] = task
				break
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
}

func TestTheUnassignVerbsSendTheTaskTheReasonAndTheSeat(t *testing.T) {
	f := &unassignBoard{}
	srv := f.serve()
	defer srv.Close()
	useFake(t, srv)
	t.Cleanup(func() { unassignReason = "" })

	taskSessionUuid, unassignReason = "seat-1", "the agent is gone"
	out := captureStdout(t, func() { agentTaskUnassignCmd.Run(agentTaskUnassignCmd, []string{"t-1"}) })
	got := f.sent["agentTaskUnassignProgrammatic"]
	if got["taskUuid"] != "t-1" || got["sessionUuid"] != "seat-1" || got["reason"] != "the agent is gone" {
		t.Fatalf("the seat's unassign sends task, session and reason, got %v", got)
	}
	if !strings.Contains(out, "RD-7") {
		t.Errorf("prints the task compactly, got %q", out)
	}

	unassignReason = "stuck for two hours"
	boardsUnassignCmd.Run(boardsUnassignCmd, []string{"t-1"})
	got = f.sent["agentTaskUnassign"]
	if got["taskUuid"] != "t-1" || got["reason"] != "stuck for two hours" || got["sessionUuid"] != nil {
		t.Fatalf("a person's unassign sends task and reason, no session, got %v", got)
	}

	for _, c := range []string{"agent task unassign", "boards unassign"} {
		found, _, err := rootCmd.Find(append(strings.Fields(c), "t-1"))
		if err != nil || found.Name() != "unassign" || found.Flag("reason") == nil {
			t.Errorf("%s is attached with --reason", c)
		}
	}
	if agentTaskUnassignCmd.Flag("session") == nil || agentTaskUnassignCmd.Flag("json") == nil {
		t.Error("the seat's verb takes --session and --json")
	}
}

func TestARefusalForAnUnassignedHopForgetsTheTaskLocally(t *testing.T) {
	withStateDir(t)
	if err := writeAgentState(&agentSessionState{SessionUuid: "s-1", ClientSessionId: "c-1", CurrentTask: "t-1"}); err != nil {
		t.Fatal(err)
	}
	other := errors.New("Task t-1 is assigned to a different session")
	if forgetUnassignedHop(other, "s-1", "t-1") {
		t.Fatal("any other refusal leaves the task")
	}
	if st := findStateBySessionUuid("s-1"); st == nil || st.CurrentTask != "t-1" {
		t.Fatalf("still the session's task, got %+v", st)
	}
	unassigned := errors.New("you were unassigned from RD-7 by ops@acme.example at 2026-09-28T17:00:00Z; the task is queued again")
	if !forgetUnassignedHop(unassigned, "s-1", "t-1") {
		t.Fatal("the unassigned refusal is recognised")
	}
	if st := findStateBySessionUuid("s-1"); st == nil || st.CurrentTask != "" {
		t.Fatalf("the unassigned task is forgotten, got %+v", st)
	}
}
