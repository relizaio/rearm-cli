package cmd

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"sync"
	"testing"

	rearm "github.com/relizaio/rearm-client-go"
	"github.com/spf13/cobra"
)

// The split request as the CLI sends and prints it (task SPLIT-12, parent SPLIT-9 T-17).

func requestedSplit(children ...interface{}) map[string]interface{} {
	return map[string]interface{}{"state": "REQUESTED", "role": "coder", "requestedAt": "2026-11-03T06:00:00Z",
		"description": "two parts", "proposedChildren": children}
}

func readChild(title string) map[string]interface{} {
	return map[string]interface{}{"title": title, "description": nil, "externalRef": nil, "sourceUrl": nil,
		"dependsOnSiblingIndexes": []interface{}{}, "producesComponent": nil, "workLevel": nil, "group": nil, "tags": nil}
}

// (i) return: --children-json is sent as proposedChildren only when given; bad JSON is refused before any request.
func TestReturnProposesChildrenOnlyWhenGiven(t *testing.T) {
	vars := map[string]interface{}{"reason": "SPLIT"}
	if err := addProposedChildren(vars, `[{"title":"a"},{"title":"b","dependsOnSiblingIndexes":[0]}]`, true); err != nil {
		t.Fatal(err)
	}
	want := []map[string]interface{}{{"title": "a"}, {"title": "b", "dependsOnSiblingIndexes": []interface{}{float64(0)}}}
	if !reflect.DeepEqual(vars["proposedChildren"], want) {
		t.Errorf("proposedChildren sent as %#v", vars["proposedChildren"])
	}
	none := map[string]interface{}{"reason": "OTHER"}
	if err := addProposedChildren(none, "", false); err != nil {
		t.Fatal(err)
	}
	if _, ok := none["proposedChildren"]; ok {
		t.Error("a return without --children-json sent proposedChildren")
	}
	bad := map[string]interface{}{}
	if err := addProposedChildren(bad, `[{`, true); err == nil {
		t.Error("bad JSON was not refused")
	}
	if _, ok := bad["proposedChildren"]; ok {
		t.Error("bad JSON was sent")
	}
	if l := agentTaskReturnCmd.Long; !strings.Contains(l, "SPLIT") || !strings.Contains(l, "--children-json") {
		t.Errorf("return's help does not name SPLIT and --children-json: %q", l)
	}
	if u := agentTaskReturnCmd.PersistentFlags().Lookup("description").Usage; !strings.Contains(u, "SPLIT") {
		t.Errorf("--description does not say SPLIT needs it: %q", u)
	}
}

// (ii) authorize: --note is sent when given, absent otherwise; the help names the split request.
func TestAuthorizeSendsTheNoteWhenGiven(t *testing.T) {
	vars := map[string]interface{}{}
	addAuthorizeNote(vars, "")
	if _, ok := vars["note"]; ok {
		t.Error("authorize sent a note it was not given")
	}
	addAuthorizeNote(vars, "why")
	if vars["note"] != "why" {
		t.Errorf("authorize --note why sent %v", vars["note"])
	}
	if agentTaskAuthorizeCmd.PersistentFlags().Lookup("note") == nil {
		t.Fatal("agent task authorize has no --note")
	}
	if l := agentTaskAuthorizeCmd.Long; !strings.Contains(l, "split request") || !strings.Contains(l, "--note") {
		t.Errorf("authorize's help does not name the split request and --note: %q", l)
	}
}

// (iii) split --from-request maps the proposal as D10 says.
func TestFromRequestMapsTheProposedChildren(t *testing.T) {
	full := map[string]interface{}{"title": "one", "description": "d", "externalRef": "X-1", "sourceUrl": "https://example.test/1",
		"dependsOnSiblingIndexes": []interface{}{}, "producesComponent": "c-1", "workLevel": float64(2), "group": "g-1",
		"tags": []interface{}{map[string]interface{}{"key": "k", "value": "v"}}}
	bare := readChild("two")
	bare["dependsOnSiblingIndexes"] = []interface{}{float64(0)}
	got, err := proposalChildren(map[string]interface{}{"key": "SPLIT-1", "splitRequest": requestedSplit(full, bare)}, "t-1")
	if err != nil {
		t.Fatal(err)
	}
	want := []map[string]interface{}{
		{"title": "one", "description": "d", "externalRef": "X-1", "sourceUrl": "https://example.test/1",
			"dependsOnSiblingIndexes": []interface{}{}, "producesComponent": "c-1", "workLevel": float64(2), "group": "g-1",
			"tags": []map[string]interface{}{{"key": "k", "value": "v"}}},
		{"title": "two", "dependsOnSiblingIndexes": []interface{}{float64(0)}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("sent\n%#v\nwant\n%#v", got, want)
	}
	for _, field := range []string{"tags", "group"} {
		if _, ok := got[1][field]; ok {
			t.Errorf("a null %s was sent; the child must inherit the parent's", field)
		}
	}
}

// (iii) no record, a settled record or an empty proposal is refused, naming the task, before any mutation.
func TestFromRequestRefusesWithoutARequestedProposal(t *testing.T) {
	const want = "Task SPLIT-1 has no split request with proposed children; pass --children-json"
	for name, record := range map[string]interface{}{
		"no record": nil,
		"declined":  map[string]interface{}{"state": "DECLINED", "role": "coder", "proposedChildren": []interface{}{readChild("a")}},
		"split":     map[string]interface{}{"state": "SPLIT", "role": "coder", "proposedChildren": []interface{}{readChild("a")}},
		"lapsed":    map[string]interface{}{"state": "LAPSED", "role": "coder", "proposedChildren": []interface{}{readChild("a")}},
		"empty":     requestedSplit(),
	} {
		task := map[string]interface{}{"key": "SPLIT-1"}
		if record != nil {
			task["splitRequest"] = record
		}
		if _, err := proposalChildren(task, "t-1"); err == nil || err.Error() != want {
			t.Errorf("%s: refused with %v, want %q", name, err, want)
		}
	}
}

// (iii) --children-json wins over --from-request without a read; neither flag is refused without a read.
func TestSplitChildrenPrecedence(t *testing.T) {
	reads := 0
	read := func(string) (map[string]interface{}, error) {
		reads++
		return map[string]interface{}{"key": "SPLIT-1", "splitRequest": requestedSplit(readChild("proposed"))}, nil
	}
	got, err := splitChildren("t-1", `[{"title":"mine"}]`, true, true, read)
	if err != nil || len(got) != 1 || got[0]["title"] != "mine" || reads != 0 {
		t.Errorf("both flags: sent %v (%v) after %d reads, want the --children-json list and no read", got, err, reads)
	}
	if _, err := splitChildren("t-1", "", false, false, read); !errors.Is(err, errSplitNeedsChildren) || reads != 0 {
		t.Errorf("neither flag: %v after %d reads", err, reads)
	}
	got, err = splitChildren("t-1", "", false, true, read)
	if err != nil || len(got) != 1 || got[0]["title"] != "proposed" || reads != 1 {
		t.Errorf("--from-request: sent %v (%v) after %d reads", got, err, reads)
	}
	f := agentTaskSplitCmd.PersistentFlags().Lookup("children-json")
	if _, required := f.Annotations[cobra.BashCompOneRequiredFlag]; required {
		t.Error("split still marks --children-json required")
	}
	if strings.Contains(f.Usage, "required") {
		t.Errorf("--children-json usage still says required: %q", f.Usage)
	}
	if !strings.Contains(agentTaskSplitCmd.Long, "--from-request") {
		t.Errorf("split's help does not name --from-request: %q", agentTaskSplitCmd.Long)
	}
}

// The split and return verbs end to end against a stub server: what reaches the server, the exit and stderr.
func TestSplitAndReturnRefuseBeforeAnyMutation(t *testing.T) {
	if url := os.Getenv("REARM_TEST_SPLIT_REQUEST_CHILD"); url != "" {
		c, err := rearm.New(url, "id", "secret", rearm.WithoutTokenExchange())
		if err != nil {
			os.Exit(3)
		}
		apiClient = c
		taskSessionUuid = "seat"
		verb := agentTaskSplitCmd
		if os.Getenv("REARM_TEST_SPLIT_REQUEST_VERB") == "return" {
			verb = agentTaskReturnCmd
		}
		if err := verb.ParseFlags(strings.Fields(os.Getenv("REARM_TEST_SPLIT_REQUEST_ARGS"))); err != nil {
			os.Exit(4)
		}
		verb.Run(verb, []string{"t-1"})
		os.Exit(0)
	}
	for _, c := range []struct {
		name, verb, args string
		record           interface{}
		exit             int
		stderr           string
		ops              []string
	}{
		{"split, neither flag", "split", "", nil, 1, "rearm: task split needs --children-json or --from-request", nil},
		{"split, bad JSON", "split", "--children-json [{", nil, 1, "Error:", nil},
		{"split, from a declined request", "split", "--from-request",
			map[string]interface{}{"state": "DECLINED", "role": "coder", "proposedChildren": []interface{}{readChild("a")}}, 1,
			"Task SPLIT-1 has no split request with proposed children; pass --children-json", []string{"AgentTaskProgrammatic"}},
		{"split, from a requested proposal", "split", "--from-request", requestedSplit(readChild("a"), readChild("b")), 0, "",
			[]string{"AgentTaskProgrammatic", "AgentTaskSplitProgrammatic"}},
		{"return, bad JSON", "return", "--reason SPLIT --description why --children-json [{", nil, 1, "Error:", nil},
	} {
		var mu sync.Mutex
		var ops []string
		var splitVars map[string]interface{}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				OperationName string                 `json:"operationName"`
				Variables     map[string]interface{} `json:"variables"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			ops = append(ops, body.OperationName)
			if body.OperationName == "AgentTaskSplitProgrammatic" {
				splitVars = body.Variables
			}
			mu.Unlock()
			task := map[string]interface{}{"key": "SPLIT-1", "status": "COMPLETED", "splitRequest": c.record}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
				"agentTaskProgrammatic": task, "agentTaskSplitProgrammatic": task}})
		}))
		child := exec.Command(os.Args[0], "-test.run=^TestSplitAndReturnRefuseBeforeAnyMutation$")
		child.Env = append(os.Environ(), "REARM_TEST_SPLIT_REQUEST_CHILD="+srv.URL,
			"REARM_TEST_SPLIT_REQUEST_VERB="+c.verb, "REARM_TEST_SPLIT_REQUEST_ARGS="+c.args)
		var stdout, stderr strings.Builder
		child.Stdout, child.Stderr = &stdout, &stderr
		err := child.Run()
		srv.Close()
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if code != c.exit || !strings.Contains(stderr.String(), c.stderr) {
			t.Errorf("%s: exit %d, stderr %q; want exit %d and %q", c.name, code, stderr.String(), c.exit, c.stderr)
		}
		if !reflect.DeepEqual(ops, c.ops) {
			t.Errorf("%s: the server saw %v, want %v", c.name, ops, c.ops)
		}
		if c.exit == 0 {
			children, _ := splitVars["children"].([]interface{})
			if len(children) != 2 {
				t.Errorf("%s: the split sent %v", c.name, splitVars["children"])
			}
		}
	}
}

// (iv) compactTask names a REQUESTED split request only for a task waiting at the coordinator.
func TestCompactTaskNamesTheSplitRequest(t *testing.T) {
	two := requestedSplit(readChild("a"), readChild("b"))
	for _, c := range []struct {
		name       string
		status     string
		checkpoint map[string]interface{}
		request    map[string]interface{}
		want       string
	}{
		{"requested at the coordinator", "AWAITING_COORDINATOR", nil, two,
			"SPLIT-1 awaiting coordinator, role coder\nwaits for the coordinator: split requested by coder (2 children proposed)"},
		{"requested and a pending checkpoint", "AWAITING_COORDINATOR", pendingCheckpoint(), two,
			"SPLIT-1 awaiting coordinator, role coder\nwaits for the coordinator: design checkpoint before coder\n" +
				"waits for the coordinator: split requested by coder (2 children proposed)"},
		{"a blind read of a request", "AWAITING_COORDINATOR", nil,
			map[string]interface{}{"state": "REQUESTED", "role": "coder", "description": nil, "proposedChildren": []interface{}{}},
			"SPLIT-1 awaiting coordinator, role coder\nwaits for the coordinator: split requested by coder (0 children proposed)"},
		{"lapsed, back at the coordinator", "AWAITING_COORDINATOR", nil,
			map[string]interface{}{"state": "LAPSED", "role": "coder", "proposedChildren": []interface{}{readChild("a")}},
			"SPLIT-1 awaiting coordinator, role coder\nwaits for the coordinator"},
		{"declined, back at the coordinator", "AWAITING_COORDINATOR", nil,
			map[string]interface{}{"state": "DECLINED", "role": "coder", "proposedChildren": []interface{}{}},
			"SPLIT-1 awaiting coordinator, role coder\nwaits for the coordinator"},
		{"queued with a requested record", "QUEUED", nil, two, "SPLIT-1 queued, role coder\nqueued for coder"},
	} {
		task := map[string]interface{}{"key": "SPLIT-1", "status": c.status, "role": "coder"}
		if c.checkpoint != nil {
			task["designCheckpoint"] = c.checkpoint
		}
		task["splitRequest"] = c.request
		if got := compactTask(task, nil); got != c.want {
			t.Errorf("%s: printed\n%s\nwant\n%s", c.name, got, c.want)
		}
	}
}

// (v) the brief prints one line for the record, by state, after the checkpoint line; none without a record.
func TestBriefPrintsTheSplitRequest(t *testing.T) {
	seat := map[string]interface{}{"kind": "SESSION", "uuid": "s-1", "name": "rg-coord1"}
	settled := func(state, note string) map[string]interface{} {
		return map[string]interface{}{"state": state, "role": "coder", "requestedAt": "2026-11-03T06:00:00Z",
			"settledBy": seat, "settledAt": "2026-11-03T06:05:00Z", "note": note}
	}
	for _, c := range []struct {
		name   string
		record map[string]interface{}
		want   string
	}{
		{"requested", requestedSplit(readChild("part one"), readChild("part two")),
			"split request by coder 2026-11-03T06:00:00Z: two parts; proposes: part one; part two"},
		{"requested, no children", requestedSplit(),
			"split request by coder 2026-11-03T06:00:00Z: two parts; proposes no children"},
		{"requested, blind read", map[string]interface{}{"state": "REQUESTED", "role": "coder",
			"requestedAt": "2026-11-03T06:00:00Z", "description": nil, "proposedChildren": []interface{}{}},
			"split request by coder 2026-11-03T06:00:00Z"},
		{"declined", settled("DECLINED", "harness: one pass"),
			"split request by coder: declined by rg-coord1 2026-11-03T06:05:00Z: harness: one pass"},
		{"split", settled("SPLIT", "2 children: SPLIT-2, SPLIT-3"),
			"split request by coder: split by rg-coord1 2026-11-03T06:05:00Z: 2 children: SPLIT-2, SPLIT-3"},
		{"lapsed", settled("LAPSED", "routed to architect: answered"),
			"split request by coder: lapsed 2026-11-03T06:05:00Z: routed to architect: answered"},
	} {
		if got := splitRequestLine(c.record); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
		task := map[string]interface{}{"key": "SPLIT-1", "title": "t", "status": "AWAITING_COORDINATOR", "role": "coder",
			"designCheckpoint": pendingCheckpoint(), "splitRequest": c.record}
		out := renderTaskBrief(&taskBrief{Key: "SPLIT-1", Role: "coordinator", Task: briefTaskFields(task)})
		checkpoint := "- " + designCheckpointLine(pendingCheckpoint()) + "\n"
		if !strings.Contains(out, checkpoint+"- "+c.want+"\n") {
			t.Errorf("%s: the brief does not print %q after the checkpoint line:\n%s", c.name, c.want, out)
		}
		if strings.Contains(out, "waits") {
			t.Errorf("%s: the brief says waits", c.name)
		}
	}
	if got := splitRequestLine(nil); got != "" {
		t.Errorf("no record prints %q", got)
	}
	task := map[string]interface{}{"key": "SPLIT-1", "title": "t", "status": "QUEUED", "role": "coder"}
	if out := renderTaskBrief(&taskBrief{Key: "SPLIT-1", Role: "coder", Task: briefTaskFields(task)}); strings.Contains(out, "split request") {
		t.Errorf("a task without a record prints a split request line:\n%s", out)
	}
}

// (vi) the coordinator's wait names the split request and how to answer it.
func TestWaitHelpNamesTheSplitRequest(t *testing.T) {
	const want = "A split request is another: a role returned the task with --reason SPLIT and the ALERT names its" +
		" proposed children; split it with 'task split <key> --from-request' or send it on with" +
		" 'task authorize <key> --role <role> --note <why>'."
	if !strings.Contains(strings.Join(strings.Fields(agentWaitCmd.Long), " "), want) {
		t.Errorf("agent wait's help does not carry the split request sentence:\n%s", agentWaitCmd.Long)
	}
}

// (vii) the task reads and the two mutations come from the pinned rearm-client-go, so this fails when the pin
// predates the SPLIT-12 client-go PR (rearm-client-go#101).
func TestTaskReadsSelectSplitRequest(t *testing.T) {
	for name, op := range map[string]string{
		"AgentTaskProgrammatic":          rearm.AgentTaskProgrammatic_Operation,
		"AgentTaskReturnProgrammatic":    rearm.AgentTaskReturnProgrammatic_Operation,
		"AgentTaskAuthorizeProgrammatic": rearm.AgentTaskAuthorizeProgrammatic_Operation,
		"AgentTaskSplitProgrammatic":     rearm.AgentTaskSplitProgrammatic_Operation,
		"AgentTaskNextProgrammatic":      rearm.AgentTaskNextProgrammatic_Operation,
		"AgentTaskAssignProgrammatic":    rearm.AgentTaskAssignProgrammatic_Operation,
	} {
		if !strings.Contains(op, "splitRequest") {
			t.Errorf("%s does not select splitRequest; is rearm-client-go pinned at the SPLIT-12 PR (#101) or later?", name)
		}
	}
	if !strings.Contains(rearm.AgentTaskReturnProgrammatic_Operation, "$proposedChildren") {
		t.Error("AgentTaskReturnProgrammatic has no $proposedChildren; is rearm-client-go pinned at the SPLIT-12 PR (#101) or later?")
	}
	if !strings.Contains(rearm.AgentTaskAuthorizeProgrammatic_Operation, "$note") {
		t.Error("AgentTaskAuthorizeProgrammatic has no $note; is rearm-client-go pinned at the SPLIT-12 PR (#101) or later?")
	}
}
