package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	rearm "github.com/relizaio/rearm-client-go"
)

// The orientation split by action (task RD3-10): `rearm agent orientation` prints the core or one
// section; the brief prints the core and the sections the task's actions need -- taking a task and
// publishing always, asking when the role reads documents, commit trailers when it pushes code, and
// waiting only on the session's first brief.

var orientationNames = []string{"core", "taking-a-task", "publishing", "asking", "waiting", "commit-trailers", "signing-key"}

// orientationWorld serves the orientation URLs and the GraphQL reads a brief makes, from one server.
func orientationWorld(t *testing.T, caps []any) {
	t.Helper()
	withStateDir(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/agents/orientation/") {
			name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/agents/orientation/"), ".md")
			for _, n := range orientationNames {
				if n == name {
					_, _ = w.Write([]byte("<<" + strings.ToUpper(name) + " TEXT>>\n"))
					return
				}
			}
			w.WriteHeader(404)
			_, _ = w.Write([]byte("No orientation section named '" + name + "'. The sections are: " + strings.Join(orientationNames, ", ") + ".\n"))
			return
		}
		var req struct {
			Query string `json:"query"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		q, data := req.Query, map[string]any{}
		switch {
		case strings.Contains(q, "AgentRoleBriefProgrammatic"):
			data["agentTaskRoleConfigsProgrammatic"] = []any{
				map[string]any{"name": "coder", "servedPrompt": "SERVED coder prompt", "promptVersion": "abc123", "requiredCapabilities": caps,
					"requiredInputs": []any{map[string]any{"kind": "DOCUMENT", "specification": "ARCHITECTURE"}}},
				map[string]any{"name": "tester", "servedPrompt": "SERVED tester prompt", "promptVersion": "def456"},
			}
		case strings.Contains(q, "AgentTasksByUuidProgrammatic"):
			data["agentTasksByUuidProgrammatic"] = []any{}
		case strings.Contains(q, "AgentTaskProgrammatic"):
			data["agentTaskProgrammatic"] = briefTaskFixture()
		case strings.Contains(q, "AgentBoardProgrammatic"):
			data["agentBoardProgrammatic"] = map[string]any{"uuid": "b-1", "documentsRoot": "boards/x/"}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	t.Cleanup(srv.Close)
	c, err := rearm.New(srv.URL, "id", "secret", rearm.WithoutTokenExchange())
	if err != nil {
		t.Fatal(err)
	}
	if err := writeAgentState(&agentSessionState{SessionUuid: "s-1", ClientSessionId: "c-1"}); err != nil {
		t.Fatal(err)
	}
	apiClient, rearmUri, briefSession = c, srv.URL, "s-1"
	t.Cleanup(func() {
		apiClient, rearmUri, briefSession, briefRole, briefInline, briefJson, orientationSection = nil, "", "", "", false, false, ""
	})
}

func TestTheOrientationCommandPrintsTheCoreOrASection(t *testing.T) {
	orientationWorld(t, nil)
	if out := stdoutOf(t, func() { agentOrientationCmd.Run(agentOrientationCmd, nil) }); out != "<<CORE TEXT>>\n" {
		t.Fatalf("the core by default, got %q", out)
	}
	orientationSection = "taking-a-task"
	if out := stdoutOf(t, func() { agentOrientationCmd.Run(agentOrientationCmd, nil) }); out != "<<TAKING-A-TASK TEXT>>\n" {
		t.Fatalf("the named section, got %q", out)
	}
	_, err := fetchOrientation("fishing")
	if err == nil || !strings.Contains(err.Error(), "No orientation section named 'fishing'") || !strings.Contains(err.Error(), "taking-a-task") {
		t.Fatalf("an unknown name lists the sections, got %v", err)
	}
}

func TestTheBriefPrintsTheCoreAndTheSectionsTheTaskNeeds(t *testing.T) {
	orientationWorld(t, []any{"CODE_PUSH"})
	out := runBrief(t)
	part := out[strings.Index(out, "## 7. Orientation"):]
	if !strings.HasPrefix(part, "## 7. Orientation: the core, then taking-a-task, publishing, asking, commit-trailers, waiting") {
		t.Fatalf("a coder that reads documents and pushes code, on its first brief, got %q", part[:120])
	}
	order := []string{"<<CORE TEXT>>", "<<TAKING-A-TASK TEXT>>", "<<PUBLISHING TEXT>>", "<<ASKING TEXT>>", "<<COMMIT-TRAILERS TEXT>>", "<<WAITING TEXT>>"}
	prev := -1
	for _, s := range order {
		i := strings.Index(part, s)
		if i <= prev {
			t.Fatalf("%s out of order in %q", s, part)
		}
		prev = i
	}
	if strings.Contains(part, "SIGNING-KEY") {
		t.Error("only the sections the task's actions need")
	}
	if !strings.Contains(out, ", orientation ") || !strings.Contains(out, "Orientation: "+rearmUri+"/api/agents/orientation.md") {
		t.Errorf("the size line and the whole document's URL stay, got %q", out[:200])
	}

	// the wait loop is the session's: its section once
	again := runBrief(t)
	if strings.Contains(again, "<<WAITING TEXT>>") || !strings.Contains(again, "<<TAKING-A-TASK TEXT>>") {
		t.Errorf("a second brief for the session leaves out waiting, got %q", again[strings.Index(again, "## 7."):])
	}

	// a role that neither reads documents nor pushes code gets the two every hop needs
	withStateDir(t)
	if err := writeAgentState(&agentSessionState{SessionUuid: "s-2", ClientSessionId: "c-2"}); err != nil {
		t.Fatal(err)
	}
	briefSession, briefRole = "s-2", "tester"
	tester := runBrief(t)
	if !strings.Contains(tester, "## 7. Orientation: the core, then taking-a-task, publishing, waiting") || strings.Contains(tester, "<<ASKING TEXT>>") ||
		strings.Contains(tester, "<<COMMIT-TRAILERS TEXT>>") {
		t.Errorf("the tester's sections, got %q", tester[strings.Index(tester, "## 7."):])
	}
}

func TestTheSectionsAHopNeeds(t *testing.T) {
	cases := []struct {
		asks, push, first bool
		want              string
	}{
		{false, false, false, "taking-a-task,publishing"},
		{true, false, false, "taking-a-task,publishing,asking"},
		{false, true, false, "taking-a-task,publishing,commit-trailers"},
		{true, true, true, "taking-a-task,publishing,asking,commit-trailers,waiting"},
	}
	for _, c := range cases {
		if got := strings.Join(briefSectionKeys(c.asks, c.push, c.first), ","); got != c.want {
			t.Errorf("asks=%v push=%v first=%v: got %s, want %s", c.asks, c.push, c.first, got, c.want)
		}
	}
}
