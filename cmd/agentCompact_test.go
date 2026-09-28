package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	rearm "github.com/relizaio/rearm-client-go"
	"github.com/spf13/cobra"
)

// Compact output for the mutations (task RD3-9): one to three lines by default, the full response with
// --json, and task next unchanged.

var compactRoot = regexp.MustCompile(`\{\s*([A-Za-z]+)`)

func compactWorld(t *testing.T) {
	t.Helper()
	withStateDir(t)
	if err := writeAgentState(&agentSessionState{SessionUuid: "s-1", ClientSessionId: "c-1"}); err != nil {
		t.Fatal(err)
	}
	queued := map[string]any{"uuid": "t-1", "key": "RD-1", "status": "QUEUED", "role": "tester", "title": "build it"}
	assigned := map[string]any{"uuid": "t-1", "key": "RD-1", "status": "ASSIGNED", "role": "coder",
		"assignment": map[string]any{"session": "s-1-full-session-uuid"}, "documents": []any{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Query string `json:"query"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		root := compactRoot.FindStringSubmatch(req.Query[strings.Index(req.Query, "{"):])[1]
		var v any = queued
		switch root {
		case "agentTaskAssignProgrammatic", "agentTaskNextProgrammatic":
			v = map[string]any{"task": assigned, "role": "coder", "promptVersion": "abc123", "rolePrompt": "a prompt"}
		case "agentTaskHoldProgrammatic":
			v = map[string]any{"uuid": "t-1", "key": "RD-1", "status": "ON_HOLD", "role": "coder",
				"hold": map[string]any{"kind": "MANUAL", "reason": "waiting on legal"}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{root: v}})
	}))
	t.Cleanup(srv.Close)
	c, err := rearm.New(srv.URL, "id", "secret", rearm.WithoutTokenExchange())
	if err != nil {
		t.Fatal(err)
	}
	apiClient = c
	taskSessionUuid, taskOutcome, taskReturnReason, taskPrUrl, taskTitle, taskBoardUuid, taskNote = "s-1", "PASSED", "OTHER",
		"https://github.com/acme/x/pull/1", "build it", "b-1", "why"
	t.Cleanup(func() {
		apiClient, compactJson = nil, false
		taskSessionUuid, taskOutcome, taskReturnReason, taskPrUrl, taskTitle, taskBoardUuid, taskNote = "", "", "", "", "", "", ""
	})
}

func runCmd(t *testing.T, c *cobra.Command, args ...string) string {
	return strings.TrimSpace(stdoutOf(t, func() { c.Run(c, args) }))
}

func TestMutationsPrintCompactLines(t *testing.T) {
	compactWorld(t)
	for _, tc := range []struct {
		name string
		cmd  *cobra.Command
		args []string
		want string
	}{
		{"signoff", agentTaskSignoffCmd, []string{"t-1"}, "RD-1 queued, role tester\nqueued for tester"},
		{"return", agentTaskReturnCmd, []string{"t-1"}, "RD-1 queued, role tester\nqueued for tester"},
		{"linkpr", agentTaskLinkprCmd, []string{"t-1"}, "RD-1 queued, role tester\nqueued for tester"},
		{"register", agentTaskRegisterCmd, nil, "RD-1 queued, role tester\nqueued for tester"},
		{"assign", agentTaskAssignCmd, []string{"t-1"}, "RD-1 assigned, role coder, prompt abc123\n" +
			"held by session s-1-full; read it with: rearm agent task brief RD-1 --session s-1-full-session-uuid"},
		{"hold", agentTaskHoldCmd, []string{"t-1"}, "RD-1 on hold, role coder\non hold (manual): waiting on legal"},
	} {
		if got := runCmd(t, tc.cmd, tc.args...); got != tc.want {
			t.Errorf("%s printed\n%s\nwant\n%s", tc.name, got, tc.want)
		}
	}
}

func TestJsonPrintsTheFullResponse(t *testing.T) {
	compactWorld(t)
	compactJson = true
	var got map[string]any
	if err := json.Unmarshal([]byte(runCmd(t, agentTaskSignoffCmd, "t-1")), &got); err != nil {
		t.Fatal(err)
	}
	if got["status"] != "QUEUED" || got["title"] != "build it" {
		t.Errorf("--json: %v", got)
	}
}

func TestTaskNextKeepsItsShape(t *testing.T) {
	compactWorld(t)
	var got map[string]any
	if err := json.Unmarshal([]byte(runCmd(t, agentTaskNextCmd)), &got); err != nil {
		t.Fatalf("task next is JSON, as wait prints and scripts parse: %v", err)
	}
	if got["rolePrompt"] != "a prompt" {
		t.Errorf("task next: %v", got)
	}
}

func TestEveryCompactVerbTakesJson(t *testing.T) {
	for _, c := range compactCommands() {
		if c.Flags().Lookup("json") == nil {
			t.Errorf("%s has no --json", c.CommandPath())
		}
	}
}

func TestAPublishIsOneLine(t *testing.T) {
	got := compactDocument(map[string]interface{}{"uuid": "r-9", "version": "113", "lifecycle": "ASSEMBLED"}, "ARCHITECTURE", true)
	if got != "published ARCHITECTURE v113 (assembled), advisory; release r-9" {
		t.Errorf("publish: %s", got)
	}
}
