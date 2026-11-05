package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"

	rearm "github.com/relizaio/rearm-client-go"
)

// The architect's design checkpoint waiver (task TPF-153, backend TPF-152): `task signoff --no-split` sends
// noSplit true; without the flag nothing is sent. A rejection or a missing note is refused locally, in the
// server's words, before any link or request; every other refusal is the server's.

func TestSignoffNoSplitSendsTheStatement(t *testing.T) {
	withStateDir(t)
	if err := writeAgentState(&agentSessionState{SessionUuid: "s-1", ClientSessionId: "c-1", CurrentTask: "t-1"}); err != nil {
		t.Fatal(err)
	}
	f := &seenBoard{}
	srv := f.serve(t, seenTask("t-1"))
	defer srv.Close()
	useFake(t, srv)
	t.Cleanup(func() { taskNoSplit, taskNote = false, "" })

	taskSessionUuid, taskOutcome, taskNote, taskNoSplit = "s-1", "PASSED", "fits one coder pass", true
	agentTaskSignoffCmd.Run(agentTaskSignoffCmd, []string{"t-1"})
	if got := f.signed["noSplit"]; got != true {
		t.Errorf("--no-split sent noSplit %v, want true", got)
	}
	if got := f.signed["note"]; got != "fits one coder pass" {
		t.Errorf("--no-split sent the note %v", got)
	}

	taskSessionUuid, taskOutcome, taskNote, taskNoSplit = "s-1", "PASSED", "fits one coder pass", false
	agentTaskSignoffCmd.Run(agentTaskSignoffCmd, []string{"t-1"})
	if got, ok := f.signed["noSplit"]; ok && got != nil {
		t.Errorf("a sign-off without --no-split sent noSplit %v", got)
	}
}

// The two local refusals: run in a child process (they exit 1), against a fake in this one that counts requests.
func TestSignoffNoSplitRefusesLocally(t *testing.T) {
	if url := os.Getenv("REARM_TEST_NO_SPLIT_CHILD"); url != "" {
		withStateDir(t)
		c, err := rearm.New(url, "id", "secret", rearm.WithoutTokenExchange())
		if err != nil {
			os.Exit(3)
		}
		apiClient = c
		taskSessionUuid, taskNoSplit = "s-1", true
		taskOutcome = os.Getenv("REARM_TEST_NO_SPLIT_OUTCOME")
		taskNote = os.Getenv("REARM_TEST_NO_SPLIT_NOTE")
		taskSignoffPrs = []string{"https://github.com/acme/x/pull/1"}
		agentTaskSignoffCmd.Run(agentTaskSignoffCmd, []string{"t-1"})
		os.Exit(0)
	}
	if noSplitNeedsPassed != "--no-split says a passed design fits one coder pass; a rejection hands nothing over, so sign off REJECTED without it" {
		t.Errorf("the outcome refusal is not the server's: %q", noSplitNeedsPassed)
	}
	if noSplitNeedsNote != "--no-split needs a note: say in one line why the design fits one coder pass (--note '...')" {
		t.Errorf("the note refusal is not the server's: %q", noSplitNeedsNote)
	}
	var mu sync.Mutex
	var requests []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Query string `json:"query"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		mu.Lock()
		requests = append(requests, req.Query)
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{}})
	}))
	defer srv.Close()
	for _, c := range []struct {
		name, outcome, note, want string
	}{
		{"a rejection", "REJECTED", "x", noSplitNeedsPassed},
		{"a rejection without a note", "REJECTED", "", noSplitNeedsPassed},
		{"no note", "PASSED", "", noSplitNeedsNote},
		{"a blank note", "PASSED", "  ", noSplitNeedsNote},
	} {
		child := exec.Command(os.Args[0], "-test.run=^TestSignoffNoSplitRefusesLocally$")
		child.Env = append(os.Environ(), "REARM_TEST_NO_SPLIT_CHILD="+srv.URL,
			"REARM_TEST_NO_SPLIT_OUTCOME="+c.outcome, "REARM_TEST_NO_SPLIT_NOTE="+c.note)
		var stdout, stderr strings.Builder
		child.Stdout, child.Stderr = &stdout, &stderr
		err := child.Run()
		if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 1 {
			t.Fatalf("%s: should exit 1, got %v\nstdout: %s\nstderr: %s", c.name, err, stdout.String(), stderr.String())
		}
		if !strings.Contains(stderr.String(), "Error: "+c.want+"\n") {
			t.Errorf("%s: stderr does not carry the refusal: %q", c.name, stderr.String())
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 0 {
		t.Errorf("a locally refused --no-split sent %d requests (a link or the sign-off): %v", len(requests), requests)
	}
}

func TestSignoffHasTheNoSplitFlagAndTheOperationCarriesIt(t *testing.T) {
	fl := agentTaskSignoffCmd.PersistentFlags().Lookup("no-split")
	if fl == nil || fl.Value.Type() != "bool" || fl.DefValue != "false" {
		t.Fatalf("signoff --no-split is missing or not a bool defaulting to false: %+v", fl)
	}
	for _, want := range []string{
		"the design fits one coder pass; recorded on the task as a checkpoint waiver by this role, per design round",
		"--note", "architectCheckpointWaiver",
	} {
		if !strings.Contains(fl.Usage, want) {
			t.Errorf("the flag's help lacks %q: %q", want, fl.Usage)
		}
	}
	if !strings.Contains(rearm.AgentTaskSignOffProgrammatic_Operation, "noSplit: $noSplit") {
		t.Error("the pinned client-go sign-off does not send noSplit; is rearm-client-go pinned at the TPF-153 PR or later?")
	}
	src, err := os.ReadFile("agentTask.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "[--no-change] [--no-split]") {
		t.Error("the command list comment does not carry [--no-split]")
	}
}

// TPF-153 T-8: the coordinator's wait says a waived checkpoint posts an INFO and wakes nothing.
func TestWaitHelpNamesTheWaivedCheckpoint(t *testing.T) {
	const want = "A checkpoint the architect waived with --no-split posts an INFO instead of the ALERT, printed with" +
		" the events and waking nothing: the task is queued for that role, and task list --changed-since shows it."
	long := strings.Join(strings.Fields(agentWaitCmd.Long), " ")
	if !strings.Contains(long, want) {
		t.Errorf("agent wait's help does not carry the waived checkpoint sentence:\n%s", agentWaitCmd.Long)
	}
	if i, j := strings.Index(long, "or split it with 'task split'."), strings.Index(long, want); !(i >= 0 && i < j && j < strings.Index(long, "A split request is another")) {
		t.Error("the sentence is not between the checkpoint ALERT and the split request")
	}
}
