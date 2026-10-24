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

// Task TPF-96: tag, setgroup and work-level, in both the rearm agent task and the rearm boards tree,
// take a task key as every other task verb does.

const tpf96Uuid = "5d2c8a41-7b3e-4f19-a6c0-2e9b7d14f8a3"

// tpf96Verbs are the six verbs this task registers.
func tpf96Verbs() []*cobra.Command {
	return []*cobra.Command{
		agentTaskTagCmd, agentTaskSetGroupCmd, agentTaskWorkLevelCmd,
		boardsTagCmd, boardsSetGroupCmd, boardsTaskWorkLevelCmd,
	}
}

// T-1: for each of the six, a key resolves to the uuid in PreRunE, a uuid passes through with no read,
// and an unknown key fails with the existing message.
func TestTagSetgroupAndWorkLevelResolveATaskKey(t *testing.T) {
	saved := taskKeyLookup
	t.Cleanup(func() { taskKeyLookup = saved })
	var asked []string
	taskKeyLookup = func(key string) (string, error) {
		asked = append(asked, key)
		if key == "TPF-68" {
			return tpf96Uuid, nil
		}
		return "", nil
	}
	for _, c := range tpf96Verbs() {
		path := c.CommandPath()
		if c.PreRunE == nil {
			t.Errorf("%s does not resolve task keys (no PreRunE)", path)
			continue
		}
		args := []string{"TPF-68"}
		if err := c.PreRunE(c, args); err != nil || args[0] != tpf96Uuid {
			t.Errorf("%s: the key should resolve to %s: %v %v", path, tpf96Uuid, args, err)
		}
		asked = nil
		args = []string{tpf96Uuid}
		if err := c.PreRunE(c, args); err != nil || args[0] != tpf96Uuid || len(asked) != 0 {
			t.Errorf("%s: a uuid passes through without a read: %v %v %v", path, args, err, asked)
		}
		args = []string{"TPF-999"}
		if err := c.PreRunE(c, args); err == nil || !strings.Contains(err.Error(), "no task has the key TPF-999") {
			t.Errorf("%s: an unknown key is refused by name: %v", path, err)
		}
	}
}

// T-6: the six say they take a key, in Use and in Long.
func TestTagSetgroupAndWorkLevelHelpSaysAKeyIsTaken(t *testing.T) {
	for _, c := range tpf96Verbs() {
		if !strings.Contains(c.Use, "<task>") || strings.Contains(c.Use, "<task-uuid>") {
			t.Errorf("%s: Use should name <task>: %q", c.CommandPath(), c.Use)
		}
		if !strings.Contains(c.Long, "<task> is a task key such as TPF-42, or its uuid.") {
			t.Errorf("%s: Long should say a key is taken: %q", c.CommandPath(), c.Long)
		}
	}
}

// tagServer stands in for the GraphQL endpoint, recording each operation by name with its variables.
type tagServer struct {
	mu   sync.Mutex
	ops  []string
	vars []map[string]any
}

var operationName = regexp.MustCompile(`^\s*(?:query|mutation)\s+(\w+)`)

func (s *tagServer) serve() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		op := ""
		if m := operationName.FindStringSubmatch(req.Query); m != nil {
			op = m[1]
		}
		s.mu.Lock()
		s.ops = append(s.ops, op)
		s.vars = append(s.vars, req.Variables)
		s.mu.Unlock()
		data := map[string]any{}
		task := map[string]any{"uuid": tpf96Uuid, "key": "TPF-68", "board": "b-1", "tags": []any{}}
		switch op {
		case "AgentTaskByKeyProgrammatic":
			if req.Variables["key"] == "TPF-68" {
				data["agentTaskByKeyProgrammatic"] = task
			} else {
				data["agentTaskByKeyProgrammatic"] = nil
			}
		case "AgentTaskProgrammatic":
			data["agentTaskProgrammatic"] = task
		case "AgentTaskSetTagsProgrammatic":
			data["agentTaskSetTagsProgrammatic"] = map[string]any{"uuid": tpf96Uuid, "key": "TPF-68",
				"tags": []any{map[string]any{"key": "mutation", "value": nil}}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
}

// T-2: through the root, agent task tag TPF-68 --add mutation resolves the key with the real lookup,
// then reads the current tags and sends the mutation, both by the uuid; the key never reaches either.
func TestAgentTaskTagSendsTheResolvedUuid(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := &tagServer{}
	srv := s.serve()
	t.Cleanup(srv.Close)
	c, err := rearm.New(srv.URL, "id", "secret", rearm.WithoutTokenExchange())
	if err != nil {
		t.Fatal(err)
	}
	apiClient = c
	t.Cleanup(func() {
		apiClient = nil
		taskTagAdd, taskTagRemove, taskTagsClear, taskSessionUuid = nil, nil, false, ""
		rootCmd.SetArgs(nil)
	})
	rootCmd.SetArgs([]string{"agent", "task", "tag", "TPF-68", "--add", "mutation", "--session", "s-1"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatal(err)
	}

	want := []string{"AgentTaskByKeyProgrammatic", "AgentTaskProgrammatic", "AgentTaskSetTagsProgrammatic"}
	if strings.Join(s.ops, ",") != strings.Join(want, ",") {
		t.Fatalf("the key is resolved before the read, and the read before the mutation: %v", s.ops)
	}
	if s.vars[0]["key"] != "TPF-68" {
		t.Errorf("the resolve asks for the key: %v", s.vars[0])
	}
	if s.vars[1]["taskUuid"] != tpf96Uuid {
		t.Errorf("the read of the current tags goes by the uuid: %v", s.vars[1])
	}
	m := s.vars[2]
	if m["taskUuid"] != tpf96Uuid || m["sessionUuid"] != "s-1" {
		t.Errorf("the mutation carries the uuid, never the key: %v", m)
	}
	tags, _ := m["tags"].([]any)
	if len(tags) != 1 || tags[0].(map[string]any)["key"] != "mutation" {
		t.Errorf("the mutation adds the tag: %v", m["tags"])
	}
}
