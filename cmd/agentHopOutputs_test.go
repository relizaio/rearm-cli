package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// Outputs scoped to the hop (task RD4-7): `task assign` starts the task's entry empty, `doc publish` appends to
// the current hop's entry (never an advisory round), `task signoff` sends that entry and forgets it once
// accepted, and --outputs still overrides. A state written before RD4-7 migrates its per-task list once.

// hopBoard answers assign, publish and sign-off for any task, and records each sign-off's outputs.
type hopBoard struct {
	mu       sync.Mutex
	assigns  int
	signoffs []map[string]any
	refuse   bool
}

func (f *hopBoard) serve() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		f.mu.Lock()
		defer f.mu.Unlock()
		data := map[string]any{}
		switch {
		case strings.Contains(req.Query, "agentTaskSignOffProgrammatic("):
			if f.refuse {
				_ = json.NewEncoder(w).Encode(map[string]any{"errors": []any{map[string]any{"message": "refused for the test"}}})
				return
			}
			f.signoffs = append(f.signoffs, req.Variables)
			data["agentTaskSignOffProgrammatic"] = map[string]any{"uuid": req.Variables["taskUuid"]}
		case strings.Contains(req.Query, "agentTaskAssignProgrammatic("):
			f.assigns++
			task := req.Variables["taskUuid"]
			data["agentTaskAssignProgrammatic"] = map[string]any{"role": "coder", "task": map[string]any{
				"uuid": task, "key": "RD-1", "documents": []any{},
				"assignment": map[string]any{"session": "s-1", "role": "coder",
					"assignedAt": fmt.Sprintf("2026-09-29T10:0%d:00Z", f.assigns)}}}
		case strings.Contains(req.Query, "agentDocumentPublishProgrammatic("):
			in, _ := req.Variables["input"].(map[string]any)
			data["agentDocumentPublishProgrammatic"] = map[string]any{"uuid": in["digest"], "version": "1",
				"lifecycle": "DRAFT", "document": map[string]any{"specification": "DETAILED_DESIGN", "round": 1}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
}

func (f *hopBoard) lastOutputs(t *testing.T) []any {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.signoffs) == 0 {
		t.Fatal("no sign-off reached the server")
	}
	out, _ := f.signoffs[len(f.signoffs)-1]["outputs"].([]any)
	return out
}

func hopWorld(t *testing.T) *hopBoard {
	t.Helper()
	if os.Getenv("REARM_HOP_REFUSAL_CHILD") != "1" {
		// The refusal child works in its parent's state directory, which the parent reads afterwards.
		withStateDir(t)
	}
	if err := writeAgentState(&agentSessionState{SessionUuid: "s-1", ClientSessionId: "c-1"}); err != nil {
		t.Fatal(err)
	}
	f := &hopBoard{}
	srv := f.serve()
	t.Cleanup(srv.Close)
	useFake(t, srv)
	t.Cleanup(func() { docTask, docAdvisory = "", false })
	return f
}

func hopAssign(t *testing.T, task string) {
	t.Helper()
	taskSessionUuid = "s-1"
	stdoutOf(t, func() { agentTaskAssignCmd.Run(agentTaskAssignCmd, []string{task}) })
}

// hopPublish sends a publish as `doc publish` does, the release uuid being the digest sent.
func hopPublish(t *testing.T, task, release string, advisory bool) {
	t.Helper()
	docTask, docAdvisory = task, advisory
	st := lookupAgentState("s-1")
	stdoutOf(t, func() {
		if err := sendDocPublish(st, map[string]any{"taskUuid": task, "digest": release}, ""); err != nil {
			t.Fatal(err)
		}
	})
}

func hopSignoff(t *testing.T, task string) {
	t.Helper()
	taskSessionUuid, taskOutcome = "s-1", "REJECTED"
	stdoutOf(t, func() { agentTaskSignoffCmd.Run(agentTaskSignoffCmd, []string{task}) })
}

// The RD3-5 shape: an earlier hop of the same session on the same task left a BOARD_QUESTIONS round behind (its
// sign-off named its outputs), and the next hop's sign-off without --outputs must not carry it.
func TestASignOffCarriesThisHopsPublishesAndNothingFromAnEarlierHop(t *testing.T) {
	f := hopWorld(t)
	hopAssign(t, "t-1")
	hopPublish(t, "t-1", "questions-1", false)
	taskOutputs = []string{"questions-1"}
	hopSignoff(t, "t-1")
	taskOutputs = nil
	// A sign-off with --outputs closes the hop as well.
	if got := hopOutputsFor("s-1", "t-1"); len(got) != 0 {
		t.Fatalf("the closed hop's record survived: %v", got)
	}

	// Simulate a record the old CLI left behind, then the next hop on the same task.
	rememberPendingOutput(lookupAgentState("s-1"), "t-1", "questions-1")
	hopAssign(t, "t-1")
	if got := hopOutputsFor("s-1", "t-1"); len(got) != 0 {
		t.Fatalf("assign must start the hop empty, got %v", got)
	}
	if h := lookupAgentState("s-1").HopOutputs["t-1"]; h == nil || h.AssignedAt != "2026-09-29T10:02:00Z" {
		t.Errorf("the hop is named by its assignedAt, got %+v", h)
	}
	hopPublish(t, "t-1", "notes-2", false)
	hopSignoff(t, "t-1")
	if got := f.lastOutputs(t); !reflect.DeepEqual(got, []any{"notes-2"}) {
		t.Errorf("the second hop's sign-off sent %v, want only notes-2", got)
	}
}

// Two tasks worked by one session, their hops interleaved: each sign-off sends its own task's documents.
func TestTwoHopsOfOneSessionOnTwoTasksKeepTheirOwnOutputs(t *testing.T) {
	f := hopWorld(t)
	hopAssign(t, "t-a")
	hopPublish(t, "t-a", "rel-a", false)
	hopAssign(t, "t-b")
	hopPublish(t, "t-b", "rel-b", false)
	hopPublish(t, "t-b", "rel-b", false) // a retry returns the same release: recorded once
	hopSignoff(t, "t-a")
	if got := f.lastOutputs(t); !reflect.DeepEqual(got, []any{"rel-a"}) {
		t.Errorf("task a's sign-off sent %v", got)
	}
	hopSignoff(t, "t-b")
	if got := f.lastOutputs(t); !reflect.DeepEqual(got, []any{"rel-b"}) {
		t.Errorf("task b's sign-off sent %v", got)
	}
}

func TestAnAdvisoryPublishIsNotRecordedAsAnOutput(t *testing.T) {
	hopWorld(t)
	hopAssign(t, "t-1")
	hopPublish(t, "t-2", "advisory-1", true)
	hopPublish(t, "t-1", "notes-1", false)
	if got := hopOutputsFor("s-1", "t-2"); len(got) != 0 {
		t.Errorf("an advisory round is no hop's output, recorded %v", got)
	}
	if got := hopOutputsFor("s-1", "t-1"); !reflect.DeepEqual(got, []string{"notes-1"}) {
		t.Errorf("the held task's publish was recorded as %v", got)
	}
}

func TestExplicitOutputsOverrideTheHopsRecord(t *testing.T) {
	f := hopWorld(t)
	hopAssign(t, "t-1")
	hopPublish(t, "t-1", "notes-1", false)
	taskOutputs = []string{"chosen"}
	hopSignoff(t, "t-1")
	if got := f.lastOutputs(t); !reflect.DeepEqual(got, []any{"chosen"}) {
		t.Errorf("--outputs was not what the sign-off sent: %v", got)
	}
}

// Read, not taken: before RD4-7 the outputs were taken before the call, so a refused sign-off lost them.
func TestARefusedSignOffKeepsTheOutputsForTheRetry(t *testing.T) {
	if os.Getenv("REARM_HOP_REFUSAL_CHILD") == "1" {
		f := hopWorld(t)
		hopAssign(t, "t-1")
		hopPublish(t, "t-1", "notes-1", false)
		f.refuse = true
		hopSignoff(t, "t-1") // exits 1
		return
	}
	// The refusal exits the process, so the refused call runs in a child; its state directory is ours.
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	cmd := testChild(t, "TestARefusedSignOffKeepsTheOutputsForTheRetry", "REARM_HOP_REFUSAL_CHILD=1", "XDG_STATE_HOME="+dir)
	if err := cmd.Run(); err == nil {
		t.Fatal("the refused sign-off should exit non-zero")
	}
	if got := hopOutputsFor("s-1", "t-1"); !reflect.DeepEqual(got, []string{"notes-1"}) {
		t.Errorf("after a refusal the hop's outputs are %v, want notes-1 kept", got)
	}
}

func TestAPreRD47StateMigratesItsPerTaskListOnce(t *testing.T) {
	withStateDir(t)
	path, err := agentStatePath("c-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(strings.TrimSuffix(path, "/c-1.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	old := `{"sessionUuid":"s-1","clientSessionId":"c-1","lastSeq":0,"pendingOutputs":{"t-1":["rel-old"]}}`
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	st := lookupAgentState("c-1")
	if st == nil || st.HopOutputs["t-1"] == nil || !reflect.DeepEqual(st.HopOutputs["t-1"].Outputs, []string{"rel-old"}) {
		t.Fatalf("the per-task list did not become the hop's entry: %+v", st)
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "pendingOutputs") {
		t.Errorf("the old list was written back: %s", raw)
	}
	if !strings.Contains(string(raw), `"hopOutputs"`) {
		t.Errorf("the migration was not written: %s", raw)
	}
	// Once: the next read finds nothing to migrate, and an assign clears the migrated entry as any other.
	if migratePendingOutputs(lookupAgentState("c-1")) {
		t.Error("a second read migrated again")
	}
	startHopOutputs("s-1", "t-1", "2026-09-29T11:00:00Z")
	if got := hopOutputsFor("s-1", "t-1"); len(got) != 0 {
		t.Errorf("assign kept the migrated outputs: %v", got)
	}
}

func TestTheBriefMarksAReplacedVersionAndInlinesTheNewest(t *testing.T) {
	doc := func(uuid, version string, round int, path string) map[string]any {
		return map[string]any{"uuid": uuid, "version": version, "lifecycle": "DRAFT",
			"document": map[string]any{"specification": "DETAILED_DESIGN", "round": round, "path": path}}
	}
	// The server lists a task's documents newest first.
	task := map[string]any{"documents": []any{
		doc("r-v6", "6", 2, "impl/RD-1/notes-2.md"),
		doc("r-v5", "5", 2, "impl/RD-1/notes-2.md"),
		doc("r-v3", "3", 1, "impl/RD-1/notes-1.md"),
	}}
	docs := briefDocuments(task)
	if len(docs) != 3 || docs[0].Release != "r-v6" || docs[0].ReplacedBy != "" || docs[1].ReplacedBy != "6" || docs[2].ReplacedBy != "" {
		t.Fatalf("got %+v", docs)
	}
	local := t.TempDir()
	if err := os.MkdirAll(local+"/impl/RD-1", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(local+"/impl/RD-1/notes-2.md", []byte("corrected\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	in := briefInlineInputs(docs, []string{"DETAILED_DESIGN"}, local)
	if len(in) != 1 || in[0].Round != 2 || in[0].Content != "corrected\n" {
		t.Errorf("inlined %+v", in)
	}
	var sb strings.Builder
	for _, d := range docs {
		if d.ReplacedBy != "" {
			fmt.Fprintf(&sb, "round %d v%s replaced by v%s", d.Round, d.Version, d.ReplacedBy)
		}
	}
	if sb.String() != "round 2 v5 replaced by v6" {
		t.Errorf("got %q", sb.String())
	}
}

func TestAssignedAtIsReadFromTheAssignResponse(t *testing.T) {
	got := assignedAtOf(map[string]interface{}{"task": map[string]interface{}{
		"assignment": map[string]interface{}{"assignedAt": "2026-09-29T10:00:00Z"}}})
	if got != "2026-09-29T10:00:00Z" {
		t.Errorf("got %q", got)
	}
	if assignedAtOf(nil) != "" {
		t.Error("no response, no assignedAt")
	}
}

// testChild runs one test of this binary in a child process with extra environment.
func testChild(t *testing.T, name string, env ...string) *exec.Cmd {
	t.Helper()
	child := exec.Command(os.Args[0], "-test.run=^"+name+"$")
	child.Env = append(os.Environ(), env...)
	return child
}
