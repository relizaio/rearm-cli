package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	rearm "github.com/relizaio/rearm-client-go"
)

// The sign-off guard covers a CLI without local state (task RD3-14, withdrawing RD2-34 architecture-2 §1): the
// sign-off always sends seenInputs, an empty list when this host has no state for the session, and `task show
// --session` and `task assign` create the state, filed under the session uuid, so the refusal's remedy works on
// any host.

const statelessSession = "5e55a0b1-0000-4000-8000-00000000d014"

// guardBoard answers like the server's RD2-34 rule: a sign-off whose seenInputs leave out a document published since
// the assignment is refused, naming it; a sign-off with no seenInputs at all skips the check (callers that are
// not the CLI may still send null).
type guardBoard struct {
	mu       sync.Mutex
	task     map[string]any
	late     []string
	signed   map[string]any
	signOffs int
	refusals int
}

func (g *guardBoard) serve() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		g.mu.Lock()
		defer g.mu.Unlock()
		data := map[string]any{}
		switch {
		case strings.Contains(req.Query, "agentTaskSignOffProgrammatic("):
			g.signOffs++
			g.signed = req.Variables
			if seen, sent := req.Variables["seenInputs"].([]any); sent {
				for _, l := range g.late {
					if !containsAny(seen, l) {
						g.refusals++
						_ = json.NewEncoder(w).Encode(map[string]any{"errors": []any{map[string]any{"message": "Task RD-1 has documents " +
							"published since your assignment that this sign-off does not acknowledge: ARCHITECTURE round 2 (advisory). " +
							"Run task show --session " + statelessSession + ", read it, then sign off again."}}})
						return
					}
				}
			}
			data["agentTaskSignOffProgrammatic"] = map[string]any{"uuid": g.task["uuid"]}
		case strings.Contains(req.Query, "agentTaskAssignProgrammatic("):
			data["agentTaskAssignProgrammatic"] = map[string]any{"task": g.task, "role": "coder"}
		default:
			data["agentTaskProgrammatic"] = g.task
			data["agentTasksByUuidProgrammatic"] = []any{g.task}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
}

func containsAny(list []any, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// stateFileFor is the file a state created for a session with no client id is filed under.
func stateFileFor(t *testing.T, session string) string {
	t.Helper()
	p, err := agentStatePath(session)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// The refused sign-off exits the process, so it runs in a child of the test binary.
func TestMain(m *testing.M) {
	if os.Getenv("REARM_TEST_SIGNOFF_CHILD") != "" {
		c, err := rearm.New(os.Getenv("REARM_TEST_SIGNOFF_CHILD"), "id", "secret", rearm.WithoutTokenExchange())
		if err != nil {
			os.Exit(3)
		}
		apiClient = c
		taskSessionUuid, taskOutcome = statelessSession, "PASSED"
		agentTaskSignoffCmd.Run(agentTaskSignoffCmd, []string{"t-1"})
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestAStatelessSignOffIsRefusedWhenADocumentWasPublishedSinceTheAssignment(t *testing.T) {
	withStateDir(t)
	g := &guardBoard{task: seenTask("t-1", "r-1", "r-late"), late: []string{"r-late"}}
	srv := g.serve()
	defer srv.Close()
	child := exec.Command(os.Args[0], "-test.run=^$")
	child.Env = append(os.Environ(), "REARM_TEST_SIGNOFF_CHILD="+srv.URL)
	out, err := child.CombinedOutput()
	if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 1 {
		t.Fatalf("a stateless sign-off with a late document should exit 1, got %v:\n%s", err, out)
	}
	if !strings.Contains(string(out), "does not acknowledge: ARCHITECTURE round 2 (advisory)") ||
		!strings.Contains(string(out), "task show --session "+statelessSession) {
		t.Errorf("the refusal should name the document and the remedy, got:\n%s", out)
	}
	if got := g.signed["seenInputs"]; !reflect.DeepEqual(got, []any{}) {
		t.Errorf("the stateless sign-off sent seenInputs %#v, want []", got)
	}
	if g.refusals != 1 {
		t.Errorf("refusals %d, want 1", g.refusals)
	}
}

func TestAStatelessSignOffPassesWhenNothingWasPublishedSinceTheAssignment(t *testing.T) {
	withStateDir(t)
	g := &guardBoard{task: seenTask("t-1", "r-1")}
	srv := g.serve()
	defer srv.Close()
	useFake(t, srv)
	taskSessionUuid, taskOutcome = statelessSession, "PASSED"
	agentTaskSignoffCmd.Run(agentTaskSignoffCmd, []string{"t-1"})
	if got, ok := g.signed["seenInputs"]; !ok || !reflect.DeepEqual(got, []any{}) {
		t.Errorf("seenInputs %#v (sent %v), want []", got, ok)
	}
	if g.signOffs != 1 || g.refusals != 0 {
		t.Errorf("sign-offs %d refusals %d, want 1 and 0", g.signOffs, g.refusals)
	}
}

func TestShowOnAHostWithoutStateCreatesItAndTheNextSignOffPasses(t *testing.T) {
	withStateDir(t)
	g := &guardBoard{task: seenTask("t-1", "r-1", "r-late"), late: []string{"r-late"}}
	srv := g.serve()
	defer srv.Close()
	useFake(t, srv)
	taskShowSession = statelessSession
	stdoutOf(t, func() { agentTaskShowCmd.Run(agentTaskShowCmd, []string{"t-1"}) })
	raw, err := os.ReadFile(stateFileFor(t, statelessSession))
	if err != nil {
		t.Fatalf("the show created no state file under the session uuid: %v", err)
	}
	var st agentSessionState
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatal(err)
	}
	if st.SessionUuid != statelessSession || st.ClientSessionId != "" || !reflect.DeepEqual(st.SeenInputs["t-1"], []string{"r-1", "r-late"}) {
		t.Errorf("the created state is %+v", st)
	}
	taskSessionUuid, taskOutcome = statelessSession, "PASSED"
	stdoutOf(t, func() { agentTaskSignoffCmd.Run(agentTaskSignoffCmd, []string{"t-1"}) })
	if got := g.signed["seenInputs"]; !reflect.DeepEqual(got, []any{"r-1", "r-late"}) {
		t.Errorf("the sign-off after the show sent %#v", got)
	}
	if g.refusals != 0 {
		t.Errorf("refused after the show")
	}
}

func TestAssignOnAHostWithoutStateCreatesItWithTheTaskAndWhatItShowed(t *testing.T) {
	withStateDir(t)
	g := &guardBoard{task: seenTask("t-1", "r-1")}
	srv := g.serve()
	defer srv.Close()
	useFake(t, srv)
	taskSessionUuid = statelessSession
	stdoutOf(t, func() { agentTaskAssignCmd.Run(agentTaskAssignCmd, []string{"t-1"}) })
	st := lookupAgentState(statelessSession)
	if st == nil {
		t.Fatal("the assign created no state")
	}
	if st.CurrentTask != "t-1" || !reflect.DeepEqual(st.SeenInputs["t-1"], []string{"r-1"}) {
		t.Errorf("the created state is %+v", st)
	}
	if _, err := os.Stat(stateFileFor(t, statelessSession)); err != nil {
		t.Errorf("not filed under the session uuid: %v", err)
	}
}

func TestSeenIsSentWithoutState(t *testing.T) {
	withStateDir(t)
	g := &guardBoard{task: seenTask("t-1", "r-late"), late: []string{"r-late"}}
	srv := g.serve()
	defer srv.Close()
	useFake(t, srv)
	taskSessionUuid, taskOutcome, taskSeen = statelessSession, "PASSED", []string{"r-late"}
	stdoutOf(t, func() { agentTaskSignoffCmd.Run(agentTaskSignoffCmd, []string{"t-1"}) })
	if got := g.signed["seenInputs"]; !reflect.DeepEqual(got, []any{"r-late"}) {
		t.Errorf("seenInputs %#v, want [r-late]", got)
	}
}

func TestASessionReferenceThatIsNotAUuidCreatesNoState(t *testing.T) {
	withStateDir(t)
	if st := ensureAgentState("../../.ssh/config"); st != nil {
		t.Errorf("created state for a non-uuid reference: %+v", st)
	}
	dir, _ := agentStateDir()
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("files written: %v", entries)
	}
}

func TestAStateCreatedByAReadIsRemovedWithItsSession(t *testing.T) {
	withStateDir(t)
	st := ensureAgentState(statelessSession)
	if st == nil {
		t.Fatal("no state created")
	}
	path := stateFileFor(t, statelessSession)
	if filepath.Base(path) != statelessSession+".json" {
		t.Fatalf("filed as %s", path)
	}
	removeAgentState(findStateBySessionUuid(statelessSession))
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("the state survived its session's close: %v", err)
	}
}
