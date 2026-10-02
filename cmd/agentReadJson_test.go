package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	rearm "github.com/relizaio/rearm-client-go"
	"github.com/spf13/cobra"
)

// --json on every verb (task RD4-9): a read takes the flag and prints the same JSON with it as without
// it, never nothing; a mutation prints compact lines without it and the full response with it.

const readJsonBoard = "5a8d6b0e-0000-4000-8000-000000000001"

// readJsonWorld answers any operation with one task-shaped payload under the operation's root field; the
// board read also carries the groups that board group list prints.
func readJsonWorld(t *testing.T) {
	t.Helper()
	withStateDir(t)
	if err := writeAgentState(&agentSessionState{SessionUuid: "s-1", ClientSessionId: "c-1"}); err != nil {
		t.Fatal(err)
	}
	task := map[string]any{"uuid": "t-1", "key": "RD-1", "status": "QUEUED", "role": "tester", "title": "build it",
		"groups": []any{map[string]any{"key": "g1", "status": "OPEN"}}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Query string `json:"query"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		root := compactRoot.FindStringSubmatch(req.Query[strings.Index(req.Query, "{"):])[1]
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{root: task}})
	}))
	t.Cleanup(srv.Close)
	c, err := rearm.New(srv.URL, "id", "secret", rearm.WithoutTokenExchange())
	if err != nil {
		t.Fatal(err)
	}
	apiClient = c
	t.Cleanup(func() {
		apiClient, compactJson, readJson = nil, false, false
		taskSessionUuid, taskOutcome, taskReturnReason, taskPrUrl, taskTitle, taskBoardUuid, taskNote = "", "", "", "", "", "", ""
		taskRole, taskReopenReason, taskChildrenJson, taskExternalRef, taskEscalateReason = "", "", "", "", ""
		taskOrder, taskLevel, taskLevelClear = 0, 0, false
		taskShowSession, taskStatusFilter, taskChangedSince = "", "", ""
		releaseShowSessionUuid, releaseShowClientSessionId = "", ""
		supersedeSession, supersedeOld, supersedeBy = "", "", ""
		unassignReason = ""
		taskTagsClear, taskGroupKey = false, ""
	})
}

// runParsed parses the words as the command line would (so an unknown flag fails the test, as it failed the
// verb), then runs the verb and returns what it printed.
func runParsed(t *testing.T, c *cobra.Command, words ...string) string {
	t.Helper()
	compactJson, readJson = false, false
	if err := c.ParseFlags(words); err != nil {
		t.Fatalf("%s %s: %v", c.CommandPath(), strings.Join(words, " "), err)
	}
	args := c.Flags().Args()
	return strings.TrimSpace(stdoutOf(t, func() {
		if c.RunE != nil {
			if err := c.RunE(c, args); err != nil {
				t.Errorf("%s: %v", c.CommandPath(), err)
			}
			return
		}
		c.Run(c, args)
	}))
}

func sameJson(a, b string) bool {
	var x, y any
	if json.Unmarshal([]byte(a), &x) != nil || json.Unmarshal([]byte(b), &y) != nil {
		return false
	}
	return reflect.DeepEqual(x, y)
}

func TestReadVerbsPrintTheSameJsonWithAndWithoutTheFlag(t *testing.T) {
	readJsonWorld(t)
	doc := filepath.Join(t.TempDir(), "notes.md")
	if err := os.WriteFile(doc, []byte("# Notes\n\nplain prose\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		cmd   *cobra.Command
		words []string
	}{
		{agentTaskShowCmd, []string{"t-1"}},
		{agentTaskShowCmd, []string{"t-1", "t-2"}},
		{agentTaskShowCmd, []string{"t-1", "--session", "s-1"}},
		{agentTaskListCmd, []string{"--board", readJsonBoard}},
		{agentTaskNextCmd, []string{"--session", "s-1"}},
		{agentBoardListCmd, nil},
		{agentBoardShowCmd, []string{readJsonBoard}},
		{agentBoardSnapshotCmd, []string{readJsonBoard}},
		{agentBoardRoleconfigListCmd, []string{readJsonBoard}},
		{agentBoardGroupListCmd, []string{readJsonBoard}},
		{agentSessionShowCmd, []string{"s-1"}},
		{agentSessionInboxCmd, []string{"s-1"}},
		{agentReleaseShowCmd, []string{"r-1", "--session", "s-1"}},
		{agentDocElementsCmd, []string{doc}},
	} {
		name := tc.cmd.CommandPath() + " " + strings.Join(tc.words, " ")
		plain := runParsed(t, tc.cmd, tc.words...)
		flagged := runParsed(t, tc.cmd, append(append([]string{}, tc.words...), "--json")...)
		if plain == "" || flagged == "" {
			t.Errorf("%s: printed nothing (without --json %d bytes, with it %d)", name, len(plain), len(flagged))
			continue
		}
		if !sameJson(plain, flagged) {
			t.Errorf("%s: --json changed the output\nwithout: %s\nwith:    %s", name, plain, flagged)
		}
	}
}

func TestMutationsPrintCompactWithoutTheFlagAndJsonWithIt(t *testing.T) {
	readJsonWorld(t)
	session := []string{"--session", "s-1"}
	for _, tc := range []struct {
		cmd   *cobra.Command
		words []string
	}{
		{agentTaskRegisterCmd, []string{"--board", readJsonBoard, "--title", "build it"}},
		{agentTaskAssignCmd, append([]string{"t-1"}, session...)},
		{agentTaskSignoffCmd, append([]string{"t-1", "--outcome", "PASSED", "--outputs", "r-1"}, session...)},
		{agentTaskReturnCmd, append([]string{"t-1", "--reason", "OTHER", "--description", "why", "--outputs", "r-1"}, session...)},
		{agentTaskAuthorizeCmd, append([]string{"t-1", "--role", "coder"}, session...)},
		{agentTaskHoldCmd, append([]string{"t-1", "--reason", "legal"}, session...)},
		{agentTaskLiftholdCmd, append([]string{"t-1"}, session...)},
		{agentTaskEscalateCmd, append([]string{"t-1", "--reason", "decide"}, session...)},
		{agentTaskRequireReviewCmd, append([]string{"t-1"}, session...)},
		{agentTaskOrderCmd, append([]string{"t-1", "--order", "3"}, session...)},
		{agentTaskWorkLevelCmd, append([]string{"t-1", "--work-level", "2"}, session...)},
		{agentTaskSplitCmd, append([]string{"t-1", "--children-json", `[{"title":"part 1"}]`}, session...)},
		{agentTaskCompleteCmd, append([]string{"t-1"}, session...)},
		{agentTaskCancelCmd, append([]string{"t-1", "--note", "dup"}, session...)},
		{agentTaskReopenCmd, append([]string{"t-1", "--role", "coder", "--reason", "conflict"}, session...)},
		{agentTaskBindrefCmd, []string{"t-1", "--external-ref", "github:acme/x#1"}},
		{agentTaskLinkprCmd, []string{"t-1", "--pr-url", "https://github.com/acme/x/pull/1"}},
		{agentTaskSetGroupCmd, append([]string{"t-1", "--group", "g1"}, session...)},
		{agentTaskTagCmd, append([]string{"t-1", "--clear"}, session...)},
		{agentTaskSupersedeCmd, append([]string{"t-1", "--old", "https://github.com/acme/x/pull/1",
			"--by", "https://github.com/acme/x/pull/2"}, session...)},
		{agentTaskUnassignCmd, append([]string{"t-1", "--reason", "stale"}, session...)},
	} {
		name := tc.cmd.CommandPath()
		plain := runParsed(t, tc.cmd, tc.words...)
		flagged := runParsed(t, tc.cmd, append(append([]string{}, tc.words...), "--json")...)
		if plain == "" || flagged == "" {
			t.Errorf("%s: printed nothing (without --json %d bytes, with it %d)", name, len(plain), len(flagged))
			continue
		}
		if !strings.HasPrefix(plain, "RD-1 ") {
			t.Errorf("%s: without --json, want the compact lines, got %s", name, plain)
		}
		var full map[string]any
		if err := json.Unmarshal([]byte(flagged), &full); err != nil || full["title"] != "build it" {
			t.Errorf("%s: with --json, want the full response, got %s", name, flagged)
		}
	}
}

// Every verb under 'rearm agent' is classified, so a new verb cannot join without deciding what --json
// does for it. Only the verbs that print neither JSON nor compact lines, and the mutations that always print
// their JSON, are left without the flag.
func TestEveryAgentVerbIsClassifiedForJson(t *testing.T) {
	takesJson := map[*cobra.Command]string{}
	for _, c := range compactCommands() {
		takesJson[c] = "compact"
	}
	takesJson[agentTaskUnassignCmd] = "compact"
	for _, c := range jsonReadCommands() {
		takesJson[c] = "read"
	}
	// Text by default, JSON with the flag: their own --json.
	for _, c := range []*cobra.Command{agentTaskBriefCmd, agentBoardEventsCmd, agentBoardAgentsCmd, agentTaskMergeplanCmd,
		agentDocElementCheckCmd, agentTaskVerifyCmd, agentTaskPushCmd, agentGitCommitCmd, agentGitMergeCmd} {
		takesJson[c] = "own"
	}
	without := map[string]bool{
		// mutations that print the full response already
		"rearm agent board coordinate": true, "rearm agent board pause": true, "rearm agent board resume": true,
		"rearm agent board postevent": true, "rearm agent board roleconfig set": true, "rearm agent board group set": true,
		"rearm agent board group close": true, "rearm agent board group reopen": true, "rearm agent task declare-delivery": true,
		"rearm agent session init": true, "rearm agent session touch": true, "rearm agent session close": true,
		"rearm agent session add-artifact": true, "rearm agent session update-meta": true, "rearm agent session usage": true,
		"rearm agent enrollkey": true,
		// text, markdown or YAML, with no JSON form
		"rearm agent orientation": true, "rearm agent notes append": true, "rearm agent notes tail": true,
		"rearm agent claude hooks install": true, "rearm agent claude hooks uninstall": true, "rearm agent claude usage": true,
		"rearm agent board apply": true, "rearm agent board export": true,
		"rearm agent presets apply": true, "rearm agent presets export": true,
	}
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		if c.Runnable() {
			kind, classified := takesJson[c]
			switch {
			case classified && without[c.CommandPath()]:
				t.Errorf("%s is classified twice (%s and without --json)", c.CommandPath(), kind)
			case classified && c.Flags().Lookup("json") == nil:
				t.Errorf("%s (%s) does not take --json", c.CommandPath(), kind)
			case !classified && !without[c.CommandPath()]:
				t.Errorf("%s is not classified: add it to the reads, the compact mutations, or the verbs without --json", c.CommandPath())
			}
		}
		for _, s := range c.Commands() {
			walk(s)
		}
	}
	walk(agentCmd)
}
