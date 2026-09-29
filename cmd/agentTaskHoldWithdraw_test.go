package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// Task RD4-5: task hold --operator --question parks the caller's own hop for a person, and task withdraw sends
// a registrant's withdrawal as a cancel with the reason as its note.

func TestHoldVariables(t *testing.T) {
	for _, c := range []struct {
		name             string
		reason, question string
		operator         bool
		want             map[string]interface{}
		refused          string
	}{
		{name: "the seat's hold", reason: "waiting on legal",
			want: map[string]interface{}{"taskUuid": "t-1", "sessionUuid": "s-1", "reason": "waiting on legal"}},
		{name: "the holder parks its hop", question: " per-org or per-board? ", operator: true,
			want: map[string]interface{}{"taskUuid": "t-1", "sessionUuid": "s-1", "reason": "per-org or per-board?", "level": "OPERATOR"}},
		{name: "the seat without a reason", refused: "give --reason"},
		{name: "a question without --operator", reason: "x", question: "q", refused: "--question goes with --operator"},
		{name: "--operator without a question", operator: true, refused: "give --question"},
		{name: "--operator with a reason", operator: true, reason: "x", question: "q", refused: "give --question, not --reason"},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, err := holdVariables("t-1", "s-1", c.reason, c.question, c.operator)
			if c.refused != "" {
				if err == nil || !strings.Contains(err.Error(), c.refused) {
					t.Fatalf("want a refusal with %q, got %v, %v", c.refused, got, err)
				}
				return
			}
			if err != nil || !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got %v, %v; want %v", got, err, c.want)
			}
		})
	}
}

func TestWithdrawVariables(t *testing.T) {
	got, err := withdrawVariables("t-1", "s-1", " duplicate of RD4-3 ")
	want := map[string]interface{}{"taskUuid": "t-1", "sessionUuid": "s-1", "note": "duplicate of RD4-3"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, %v; want %v", got, err, want)
	}
	if _, err := withdrawVariables("t-1", "s-1", "  "); err == nil || !strings.Contains(err.Error(), "give --reason") {
		t.Fatalf("a withdrawal without a reason is refused before sending, got %v", err)
	}
}

func TestTheHoldOperationTakesTheLevel(t *testing.T) {
	op := normalisedOp(holdOperation())
	for _, want := range []string{"$level: AgentTaskHoldLevel)", "reason: $reason, level: $level)", "hold { level kind"} {
		if !strings.Contains(op, want) {
			t.Errorf("the hold operation lacks %q: %s", want, op[:200])
		}
	}
}

func normalisedOp(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

type holdBoard struct {
	mu    sync.Mutex
	query map[string]string
	sent  map[string]map[string]any
}

func (f *holdBoard) serve() *httptest.Server {
	f.query, f.sent = map[string]string{}, map[string]map[string]any{}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		data := map[string]any{}
		for op, task := range map[string]map[string]any{
			"agentTaskHoldProgrammatic": {"uuid": "t-1", "key": "RD-5", "status": "ON_HOLD", "role": "architect",
				"hold": map[string]any{"level": "OPERATOR", "kind": "MANUAL", "reason": "awaiting the operator: per-org?"}},
			"agentTaskCancelProgrammatic": {"uuid": "t-1", "key": "RD-5", "status": "CANCELLED", "role": nil},
		} {
			if strings.Contains(req.Query, op+"(") {
				f.mu.Lock()
				f.query[op], f.sent[op] = req.Query, req.Variables
				f.mu.Unlock()
				data[op] = task
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
}

func TestTheHolderParksItsHopAndTheRegistrantWithdraws(t *testing.T) {
	f := &holdBoard{}
	srv := f.serve()
	defer srv.Close()
	useFake(t, srv)
	t.Cleanup(func() { taskSessionUuid, taskNote, holdQuestion, holdOperator, withdrawReason = "", "", "", false, "" })

	taskSessionUuid, holdOperator, holdQuestion = "s-arch", true, "per-org?"
	out := captureStdout(t, func() { agentTaskHoldCmd.Run(agentTaskHoldCmd, []string{"t-1"}) })
	got := f.sent["agentTaskHoldProgrammatic"]
	if got["level"] != "OPERATOR" || got["reason"] != "per-org?" || got["sessionUuid"] != "s-arch" || got["taskUuid"] != "t-1" {
		t.Fatalf("the holder's hold sends the level and the question, got %v", got)
	}
	if !strings.Contains(f.query["agentTaskHoldProgrammatic"], "level: $level") {
		t.Error("the query does not pass the level")
	}
	if !strings.Contains(out, "RD-5") || !strings.Contains(out, "awaiting the operator: per-org?") {
		t.Errorf("prints the parked task compactly, got %q", out)
	}

	taskSessionUuid, withdrawReason = "s-reg", "duplicate of RD-4"
	captureStdout(t, func() { agentTaskWithdrawCmd.Run(agentTaskWithdrawCmd, []string{"t-1"}) })
	w := f.sent["agentTaskCancelProgrammatic"]
	if w["note"] != "duplicate of RD-4" || w["sessionUuid"] != "s-reg" || w["taskUuid"] != "t-1" {
		t.Fatalf("the withdrawal sends the reason as the cancel's note, got %v", w)
	}
}

func TestTheWithdrawVerbTakesATaskKeyAndItsFlags(t *testing.T) {
	for _, flag := range []string{"session", "reason", "json"} {
		if agentTaskWithdrawCmd.Flags().Lookup(flag) == nil && agentTaskWithdrawCmd.PersistentFlags().Lookup(flag) == nil {
			t.Errorf("task withdraw has no --%s", flag)
		}
	}
	for _, flag := range []string{"operator", "question", "reason", "session"} {
		if agentTaskHoldCmd.Flags().Lookup(flag) == nil && agentTaskHoldCmd.PersistentFlags().Lookup(flag) == nil {
			t.Errorf("task hold has no --%s", flag)
		}
	}
}
