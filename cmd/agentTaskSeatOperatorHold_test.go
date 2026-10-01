package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// Task RD4-17: the coordinator seat parks a task nobody is working for an operator decision with the same flags a
// hop's holder uses, task hold --operator --question. The server tells the seat from the holder; the CLI sends the
// question as the reason at OPERATOR level, and refuses a question at COORDINATOR level before anything is sent.

const seatQuestion = "CI is red on #401. Options: re-run, reopen to the coder. Recommend: re-run."

func TestTheSeatsHoldFlags(t *testing.T) {
	for _, c := range []struct {
		name             string
		reason, question string
		operator         bool
		want             map[string]interface{}
		refused          string
	}{
		{name: "the seat parks a task for the operator", question: " " + seatQuestion + " ", operator: true,
			want: map[string]interface{}{"taskUuid": "RD4-16", "sessionUuid": "s-seat", "reason": seatQuestion, "level": "OPERATOR"}},
		{name: "the seat's own hold stays COORDINATOR with a reason", reason: "waiting on the tracker",
			want: map[string]interface{}{"taskUuid": "RD4-16", "sessionUuid": "s-seat", "reason": "waiting on the tracker"}},
		{name: "a question at COORDINATOR level", question: seatQuestion,
			refused: "--question goes with --operator: it is what the session holding the task, or the coordinator seat, asks the operator"},
		{name: "a reason at OPERATOR level", reason: "red CI", question: seatQuestion, operator: true,
			refused: "with --operator the reason is the question"},
		{name: "--operator without the question", operator: true, refused: "give --question"},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, err := holdVariables("RD4-16", "s-seat", c.reason, c.question, c.operator)
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

func TestTheSeatParksADeliveringTaskForTheOperator(t *testing.T) {
	var sent map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if !strings.Contains(req.Query, "agentTaskHoldProgrammatic(") {
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{}})
			return
		}
		sent = req.Variables
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"agentTaskHoldProgrammatic": map[string]any{
			"uuid": "t-16", "key": "RD4-16", "status": "ON_HOLD", "role": "coder",
			"hold": map[string]any{"level": "OPERATOR", "kind": "MANUAL", "reason": "awaiting the operator: " + seatQuestion}}}})
	}))
	defer srv.Close()
	useFake(t, srv)
	t.Cleanup(func() { taskSessionUuid, taskNote, holdQuestion, holdOperator = "", "", "", false })

	taskSessionUuid, holdOperator, holdQuestion = "s-seat", true, seatQuestion
	out := captureStdout(t, func() { agentTaskHoldCmd.Run(agentTaskHoldCmd, []string{"t-16"}) })
	if sent["level"] != "OPERATOR" || sent["reason"] != seatQuestion || sent["sessionUuid"] != "s-seat" {
		t.Fatalf("the seat's hold sends the level and the question, got %v", sent)
	}
	if !strings.Contains(out, "RD4-16") || !strings.Contains(out, "awaiting the operator: "+seatQuestion) {
		t.Errorf("prints the parked task compactly, got %q", out)
	}
}

func TestTheHoldHelpNamesTheSeatsOperatorHold(t *testing.T) {
	long := strings.Join(strings.Fields(agentTaskHoldCmd.Long), " ")
	for _, want := range []string{
		"The coordinator seat parks a task nobody is working for a person's decision the same way, with --operator --question",
		"in PENDING_INTAKE, QUEUED, AWAITING_COORDINATOR or DELIVERING",
		"the task returns to the state it was parked from",
		"A DELIVERING task is parked only this way.",
		// RD4-17 architecture round 2: any person's action is the answer, and every session verb is refused
		"or by anything else they do on the task (answering its questions,",
		`reading "<action> by <person>: <note>"`,
		"Until then every task verb a session runs on it is refused",
		// RD4-19: the link is the exception, preparation for the decision rather than its answer
		"except task linkpr, which is accepted, recorded on the decision and posted as an INFO; it does not answer the question",
	} {
		if !strings.Contains(long, want) {
			t.Errorf("task hold --help lacks %q", want)
		}
	}
	if f := agentTaskHoldCmd.Flags().Lookup("operator"); f == nil || !strings.Contains(f.Usage, "from the coordinator seat") {
		t.Error("--operator does not say the seat uses it")
	}
}
