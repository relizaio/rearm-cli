package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	rearm "github.com/relizaio/rearm-client-go"
)

// rearm agent task brief (task RD3-9): the six parts in order, the role's served prompt, the documents newest
// round first, --inline from the checkout, --json, the notes tail, and what it printed recorded as read.

// stdoutOf runs f and returns what it printed.
func stdoutOf(t *testing.T, f func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	f()
	w.Close()
	os.Stdout = old
	return <-done
}

// briefBoard answers the reads a brief makes.
type briefBoard struct {
	noServedPrompt bool
}

func (b *briefBoard) serve() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Query string `json:"query"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		q := req.Query
		data := map[string]any{}
		switch {
		case strings.Contains(q, "AgentRoleBriefProgrammatic"):
			if b.noServedPrompt {
				_ = json.NewEncoder(w).Encode(map[string]any{"errors": []any{map[string]any{"message": "Field 'servedPrompt' undefined"}}})
				return
			}
			data["agentTaskRoleConfigsProgrammatic"] = []any{
				map[string]any{"name": "coder", "servedPrompt": "SERVED coder prompt", "promptVersion": "abc123",
					"requiredInputs": []any{map[string]any{"kind": "DOCUMENT", "specification": "ARCHITECTURE"}}},
				map[string]any{"name": "tester", "servedPrompt": "SERVED tester prompt", "promptVersion": "def456"},
			}
		case strings.Contains(q, "AgentTaskRoleConfigsProgrammatic"):
			data["agentTaskRoleConfigsProgrammatic"] = []any{map[string]any{"name": "coder", "prompt": "RAW coder prompt"}}
		case strings.Contains(q, "AgentTasksByUuidProgrammatic"):
			data["agentTasksByUuidProgrammatic"] = []any{map[string]any{"uuid": "t-0", "key": "RD-0", "title": "the base", "status": "COMPLETED"}}
		case strings.Contains(q, "AgentTaskProgrammatic"):
			data["agentTaskProgrammatic"] = briefTaskFixture()
		case strings.Contains(q, "AgentBoardProgrammatic"):
			data["agentBoardProgrammatic"] = map[string]any{"uuid": "b-1", "documentsRoot": "boards/x/",
				"documentsRepo": map[string]any{"uri": "github.com/acme/docs"},
				"documentPaths": map[string]any{"ARCHITECTURE": "design/{key}/architecture-{round}.md"}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
}

func briefTaskFixture() map[string]any {
	doc := func(uuid, spec string, round int, path string, advisory bool, findings map[string]any) map[string]any {
		d := map[string]any{"specification": spec, "round": round, "path": path, "advisory": advisory}
		if findings != nil {
			d["findings"] = findings
		}
		return map[string]any{"uuid": uuid, "version": "7", "lifecycle": "ASSEMBLED", "document": d}
	}
	return map[string]any{"uuid": "t-1", "key": "RD-1", "board": "b-1", "title": "build it", "description": "Build the thing.",
		"status": "ASSIGNED", "role": "coder", "effectiveLevel": 1, "dependsOn": []any{"t-0"},
		"tags": []any{map[string]any{"key": "urgent"}}, "spentMicros": 1_500_000,
		"openQuestions": []any{map[string]any{"id": "Q-1", "title": "which branch?"}},
		"documents": []any{
			doc("r-a1", "ARCHITECTURE", 1, "boards/x/design/RD-1/architecture-1.md", false, nil),
			doc("r-t1", "TEST_REPORT", 1, "boards/x/tests/RD-1/run-1.md", false,
				map[string]any{"verdict": "REJECTED", "counts": map[string]any{"passed": 3, "failed": 1, "skipped": 0}}),
			doc("r-a2", "ARCHITECTURE", 2, "boards/x/design/RD-1/architecture-2.md", true, nil),
		}}
}

// briefWorld: a fake server, the session's state with a checkout holding round 2 and the coder's notes.
func briefWorld(t *testing.T, b *briefBoard, withNotes bool) string {
	t.Helper()
	withStateDir(t)
	checkout := t.TempDir()
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(checkout, "boards/x/design/RD-1"), 0o755))
	must(os.WriteFile(filepath.Join(checkout, "boards/x/design/RD-1/architecture-2.md"), []byte("ROUND TWO BODY\n"), 0o644))
	if withNotes {
		must(os.MkdirAll(filepath.Join(checkout, "boards/x/notes"), 0o755))
		var lines []string
		for i := 1; i <= 25; i++ {
			lines = append(lines, "- note "+string(rune('a'+i-1)))
		}
		must(os.WriteFile(filepath.Join(checkout, "boards/x/notes/coder.md"), []byte(strings.Join(lines, "\n")+"\n"), 0o644))
	}
	must(writeAgentState(&agentSessionState{SessionUuid: "s-1", ClientSessionId: "c-1", DocumentsRepoPath: checkout}))
	srv := b.serve()
	t.Cleanup(srv.Close)
	c, err := rearm.New(srv.URL, "id", "secret", rearm.WithoutTokenExchange())
	must(err)
	apiClient = c
	rearmUri = "https://rearm.example"
	briefSession = "s-1"
	t.Cleanup(func() {
		apiClient, rearmUri, briefSession, briefRole, briefInline, briefJson = nil, "", "", "", false, false
	})
	return checkout
}

func runBrief(t *testing.T) string {
	return stdoutOf(t, func() { agentTaskBriefCmd.Run(agentTaskBriefCmd, []string{"t-1"}) })
}

func TestTheBriefPrintsItsSixPartsInOrder(t *testing.T) {
	briefWorld(t, &briefBoard{}, true)
	out := runBrief(t)
	prev := -1
	for _, h := range []string{"Brief for RD-1 as coder: prompt", "## 1. Your prompt (coder, version abc123)", "SERVED coder prompt",
		"Orientation: https://rearm.example/api/agents/orientation.md", "## 2. The task", "**RD-1** build it",
		"depends on RD-0 the base (completed)", "open question Q-1: which branch?", "## 3. Its documents",
		"ARCHITECTURE round 2 v7, assembled, advisory", "ARCHITECTURE round 1 v7", "TEST_REPORT round 1 v7, assembled, rejected (3 passed, 1 failed, 0 skipped)",
		"## 4. The repository", "github.com/acme/docs", "the board's root: `boards/x/`", "## 5. The rules", "Never force-push", "## 6. Notes (notes/coder.md)",
		"- note y"} {
		i := strings.Index(out, h)
		if i < 0 {
			t.Fatalf("missing %q in\n%s", h, out)
		}
		if i < prev {
			t.Errorf("%q is out of order", h)
		}
		prev = i
	}
	if strings.Contains(out, "- note e\n") {
		t.Error("the notes are the last 20 lines only")
	}
	if !strings.Contains(out, "- note f\n") {
		t.Error("the 20th line from the end is kept")
	}
}

func TestTheBriefTakesAnotherRolesPrompt(t *testing.T) {
	briefWorld(t, &briefBoard{}, false)
	briefRole = "tester"
	out := runBrief(t)
	if !strings.Contains(out, "SERVED tester prompt") || strings.Contains(out, "SERVED coder prompt") {
		t.Errorf("--role tester:\n%s", out)
	}
	if !strings.Contains(out, "## 6. Notes (notes/tester.md)\n\nNone yet.") {
		t.Errorf("no notes file: %s", out[strings.Index(out, "## 6."):])
	}
}

func TestInlinePrintsTheNewestRoundOfEachInput(t *testing.T) {
	briefWorld(t, &briefBoard{}, false)
	briefInline = true
	out := runBrief(t)
	if !strings.Contains(out, "### ARCHITECTURE round 2 (`boards/x/design/RD-1/architecture-2.md`)\n\nROUND TWO BODY") {
		t.Errorf("round 2 not inlined:\n%s", out)
	}
	if !strings.Contains(out, "inline 15 bytes") {
		t.Errorf("the first line gives the size: %s", strings.SplitN(out, "\n", 2)[0])
	}
}

func TestTheBriefAsJson(t *testing.T) {
	briefWorld(t, &briefBoard{}, true)
	briefJson = true
	var b taskBrief
	if err := json.Unmarshal([]byte(runBrief(t)), &b); err != nil {
		t.Fatal(err)
	}
	if b.ServedPrompt != "SERVED coder prompt" || b.PromptVersion != "abc123" || b.Role != "coder" {
		t.Errorf("prompt: %+v", b)
	}
	if len(b.Documents) != 3 || b.Documents[0].Round != 2 || len(b.Notes) != 20 || len(b.Dependencies) != 1 {
		t.Errorf("documents %d (first round %d), notes %d, deps %d", len(b.Documents), b.Documents[0].Round, len(b.Notes), len(b.Dependencies))
	}
}

func TestTheBriefRecordsWhatItShowedAsRead(t *testing.T) {
	briefWorld(t, &briefBoard{}, false)
	runBrief(t)
	if got := lookupAgentState("s-1").SeenInputs["t-1"]; !reflect.DeepEqual(got, []string{"r-a1", "r-t1", "r-a2"}) {
		t.Errorf("recorded %v", got)
	}
}

func TestAnOlderServerStillBriefsWithTheRolesOwnPrompt(t *testing.T) {
	briefWorld(t, &briefBoard{noServedPrompt: true}, false)
	out := runBrief(t)
	if !strings.Contains(out, "RAW coder prompt") || !strings.Contains(out, servedPromptMissing) {
		t.Errorf("fallback:\n%s", out)
	}
}
