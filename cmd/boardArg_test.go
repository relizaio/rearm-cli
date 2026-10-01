package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"

	rearm "github.com/relizaio/rearm-client-go"
	"github.com/spf13/cobra"
)

// A board named by its uuid or its name (task RD3-3): the verbs that take a board resolve a name
// through the boards list and send the server the uuid.

const namedUuid = "7b0e6a52-5d1e-4c7b-9f3e-2a41c0d9e111"

func fakeLister(boards ...namedBoard) (boardLister, *int) {
	calls := 0
	return func() ([]namedBoard, error) {
		calls++
		return boards, nil
	}, &calls
}

func TestAUuidPassesThroughWithoutAListCall(t *testing.T) {
	list, calls := fakeLister(namedBoard{uuid: namedUuid, name: "ReARM Dogfood 3"})
	got, err := resolveBoardArgWith(" "+namedUuid+" ", list)
	if err != nil || got != namedUuid {
		t.Fatalf("got %q, %v", got, err)
	}
	if *calls != 0 {
		t.Errorf("a uuid needs no lookup; the list was read %d times", *calls)
	}
}

func TestANameResolvesToItsBoard(t *testing.T) {
	list, _ := fakeLister(namedBoard{uuid: "other", name: "ReARM Dogfood 2"}, namedBoard{uuid: namedUuid, name: "ReARM Dogfood 3"})
	got, err := resolveBoardArgWith("ReARM Dogfood 3", list)
	if err != nil || got != namedUuid {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestAnUnknownNameOrAnAmbiguousOneSaysSo(t *testing.T) {
	list, _ := fakeLister(namedBoard{uuid: "a", name: "Twin"}, namedBoard{uuid: "b", name: "Twin"})
	_, err := resolveBoardArgWith("Nowhere", list)
	if err == nil || err.Error() != `no board named "Nowhere" in this organization; run rearm agent board list` {
		t.Errorf("unknown: %v", err)
	}
	_, err = resolveBoardArgWith("Twin", list)
	if err == nil || err.Error() != `2 boards named "Twin"; use the uuid` {
		t.Errorf("ambiguous: %v", err)
	}
	if _, err = resolveBoardArgWith("  ", list); err == nil {
		t.Error("an empty board is refused")
	}
	named, _ := fakeLister(namedBoard{uuid: namedUuid, name: "ReARM Dogfood 3"})
	if _, err = resolveBoardArgWith("rearm dogfood 3", named); err == nil {
		t.Error("the match is exact, case included")
	}
}

// nameBoard answers the boards lists with one board and records each other operation's variables by its
// operation name (two operations can share a root field: the groups list reads agentBoardProgrammatic).
type nameBoard struct {
	mu   sync.Mutex
	vars map[string]map[string]any
}

var (
	opName    = regexp.MustCompile(`^\s*(?:query|mutation)\s+([A-Za-z]+)`)
	rootField = regexp.MustCompile(`\{\s*([A-Za-z]+)`)
)

func (f *nameBoard) serve() *httptest.Server {
	f.vars = map[string]map[string]any{}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		data := map[string]any{}
		board := []any{map[string]any{"uuid": namedUuid, "name": "ReARM Dogfood 3"}}
		switch {
		case strings.Contains(req.Query, "agentBoardsProgrammatic"):
			data["agentBoardsProgrammatic"] = board
		case strings.Contains(req.Query, "agentBoardsOfOrg"):
			data["agentBoardsOfOrg"] = board
		default:
			op := opName.FindStringSubmatch(req.Query)
			root := rootField.FindStringSubmatch(req.Query[strings.Index(req.Query, "{"):])
			if len(op) > 1 && len(root) > 1 {
				f.mu.Lock()
				f.vars[op[1]] = req.Variables
				f.mu.Unlock()
				data[root[1]] = map[string]any{"uuid": namedUuid, "events": []any{}, "groups": []any{}}
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
}

func TestEveryBoardVerbSendsTheResolvedUuid(t *testing.T) {
	f := &nameBoard{}
	srv := f.serve()
	defer srv.Close()
	c, err := rearm.New(srv.URL, "id", "secret", rearm.WithoutTokenExchange())
	if err != nil {
		t.Fatal(err)
	}
	apiClient = c
	orgFlag = "org-1"
	taskSessionUuid, taskEventKind, taskNote, taskTitle = "s-1", "INFO", "hello", "a task"
	t.Cleanup(func() {
		apiClient, orgFlag, taskSessionUuid, taskEventKind, taskNote, taskTitle, taskBoardUuid = nil, "", "", "", "", "", ""
	})
	cases := []struct {
		verb  string
		cmd   *cobra.Command
		args  []string
		field string
		flag  bool
	}{
		{"agent board show", agentBoardShowCmd, []string{"ReARM Dogfood 3"}, "AgentBoardProgrammatic", false},
		{"agent board snapshot", agentBoardSnapshotCmd, []string{"ReARM Dogfood 3"}, "AgentBoardSnapshotProgrammatic", false},
		{"agent board pause", agentBoardPauseCmd, []string{"ReARM Dogfood 3"}, "AgentBoardCoordinatorPauseProgrammatic", false},
		{"agent board postevent", agentBoardPosteventCmd, []string{"ReARM Dogfood 3"}, "AgentBoardPostEventProgrammatic", false},
		{"agent board roleconfig list", agentBoardRoleconfigListCmd, []string{"ReARM Dogfood 3"}, "AgentTaskRoleConfigsProgrammatic", false},
		{"agent board events", agentBoardEventsCmd, []string{"ReARM Dogfood 3"}, "AgentBoardEventsProgrammatic", false},
		{"agent board group list", agentBoardGroupListCmd, []string{"ReARM Dogfood 3"}, "AgentBoardGroupsProgrammatic", false},
		{"agent task next --board", agentTaskNextCmd, nil, "AgentTaskNextProgrammatic", true},
		{"agent task register --board", agentTaskRegisterCmd, nil, "AgentTaskRegisterProgrammatic", true},
		{"boards tasks", boardsTasksCmd, []string{"ReARM Dogfood 3"}, "AgentTasksOfBoard", false},
		{"boards pause", boardsPauseCmd, []string{"ReARM Dogfood 3"}, "AgentBoardOperatorPause", false},
	}
	for _, tc := range cases {
		taskBoardUuid = ""
		if tc.flag {
			taskBoardUuid = "ReARM Dogfood 3"
		}
		tc.cmd.Run(tc.cmd, tc.args)
		v, ok := f.vars[tc.field]
		if !ok {
			t.Errorf("%s: %s was not called (called: %v)", tc.verb, tc.field, keysOf(f.vars))
			continue
		}
		got := v["boardUuid"]
		if in, isInput := v["input"].(map[string]any); isInput && got == nil {
			got = in["boardUuid"]
		}
		if got != namedUuid {
			t.Errorf("%s sent boardUuid %v, want %s", tc.verb, got, namedUuid)
		}
	}
}

func keysOf(m map[string]map[string]any) []string {
	out := []string{}
	for k := range m {
		out = append(out, k)
	}
	return out
}
