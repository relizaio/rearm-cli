package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	rearm "github.com/relizaio/rearm-client-go"
	"github.com/spf13/pflag"
)

// The role runWhen condition on the CLI (task TPF-156, backend TPF-155): roleconfig set's three flags, the
// condition beside roleconfig list, and the record of the roles routing passed over in the brief, beside task show
// and task list, and in the compact line. Rows T-104..T-107, T-117 and T-118 of the TPF-156 design.

// resetRoleFlags puts every roleconfig set flag back to its default and unchanged. A string slice keeps appending
// once set, so each slice flag gets a fresh value bound to the same variable.
func resetRoleFlags() {
	c := agentBoardRoleconfigSetCmd
	for name, def := range map[string]string{"order": "0", "wip-limit": "0", "require-distinct-agent": "false",
		"inactive": "false", "coordinator-checkpoint": "false", "run-when-clear": "false", "name": "", "prompt": "",
		"session": ""} {
		if f := c.Flags().Lookup(name); f != nil {
			_ = f.Value.Set(def)
			f.Changed = false
		}
	}
	for name, v := range map[string]*[]string{"run-when-tagged-any": &roleRunWhenAny, "run-when-tagged-none": &roleRunWhenNone} {
		fs := pflag.NewFlagSet("reset", pflag.ContinueOnError)
		fs.StringSliceVar(v, name, nil, "")
		if f := c.Flags().Lookup(name); f != nil {
			f.Value = fs.Lookup(name).Value
			f.Changed = false
		}
	}
}

// roleInput parses args as cobra does before Run and returns what roleconfig set would send.
func roleInput(t *testing.T, args ...string) map[string]interface{} {
	t.Helper()
	resetRoleFlags()
	c := agentBoardRoleconfigSetCmd
	if err := c.ParseFlags(args); err != nil {
		t.Fatal(err)
	}
	return roleconfigSetInput(c, "")
}

func marshalled(t *testing.T, v interface{}) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestRoleconfigSetSendsRunWhenOnlyWhenGiven(t *testing.T) {
	t.Cleanup(resetRoleFlags)
	runWhen := func(in map[string]interface{}) map[string]interface{} {
		rw, _ := in["runWhen"].(map[string]interface{})
		return rw
	}
	// (a) a plain set sends no runWhen.
	if in := roleInput(t, "--name", "coder"); func() bool { _, ok := in["runWhen"]; return ok }() {
		t.Errorf("(a) a plain set sends runWhen: %v", in)
	}
	for _, c := range []struct {
		name      string
		args      []string
		any, none []string
	}{
		{"(b) any alone", []string{"--run-when-tagged-any", "mutation"}, []string{"mutation"}, []string{}},
		{"(c) both, comma-separated", []string{"--run-when-tagged-any", "mutation,perf", "--run-when-tagged-none", "hotfix"},
			[]string{"mutation", "perf"}, []string{"hotfix"}},
		{"(d) repeated", []string{"--run-when-tagged-any", "mutation", "--run-when-tagged-any", "perf"},
			[]string{"mutation", "perf"}, []string{}},
		{"(e) none alone", []string{"--run-when-tagged-none", "hotfix"}, []string{}, []string{"hotfix"}},
		{"(f) trimmed, blanks dropped, case untouched", []string{"--run-when-tagged-any", " Mutation ,,"},
			[]string{"Mutation"}, []string{}},
	} {
		in := roleInput(t, append([]string{"--name", "coder"}, c.args...)...)
		rw := runWhen(in)
		if rw == nil {
			t.Errorf("%s: no runWhen object: %v", c.name, in)
			continue
		}
		if !reflect.DeepEqual(rw["taggedAny"], c.any) || !reflect.DeepEqual(rw["taggedNone"], c.none) {
			t.Errorf("%s: runWhen %v, want any %v none %v", c.name, rw, c.any, c.none)
		}
	}
	if got := marshalled(t, roleInput(t, "--name", "coder", "--run-when-tagged-any", "mutation")); !strings.Contains(got,
		`"runWhen":{"taggedAny":["mutation"],"taggedNone":[]}`) {
		t.Errorf("(b) a list left out is not sent empty: %s", got)
	}
	// (g) the clear sends null.
	in := roleInput(t, "--name", "coder", "--run-when-clear")
	if v, ok := in["runWhen"]; !ok || v != nil {
		t.Errorf("(g) --run-when-clear sends %v (present %v), want null", v, ok)
	}
	if got := marshalled(t, in); !strings.Contains(got, `"runWhen":null`) {
		t.Errorf("(g) marshals %s", got)
	}

	// (i) every field goes only when its flag was given (F-1).
	in = roleInput(t, "--name", "coder", "--run-when-tagged-any", "x")
	for _, k := range []string{"orderIndex", "requireDistinctAgent", "active", "wipLimit", "prompt", "coordinatorCheckpoint"} {
		if _, ok := in[k]; ok {
			t.Errorf("(i) a run-when set sends %s: %v", k, in)
		}
	}
	if len(in) != 2 || in["name"] != "coder" || runWhen(in) == nil {
		t.Errorf("(i) a run-when set sends %v, want name and runWhen only", in)
	}
	for _, c := range []struct {
		args   []string
		want   map[string]interface{}
		absent []string
	}{
		{[]string{"--order", "44"}, map[string]interface{}{"orderIndex": 44}, []string{"requireDistinctAgent", "active"}},
		{[]string{"--order", "0"}, map[string]interface{}{"orderIndex": 0}, []string{"requireDistinctAgent", "active"}},
		{[]string{"--inactive"}, map[string]interface{}{"active": false}, []string{"orderIndex", "requireDistinctAgent"}},
		{[]string{"--inactive=false"}, map[string]interface{}{"active": true}, []string{"orderIndex", "requireDistinctAgent"}},
		{[]string{"--require-distinct-agent"}, map[string]interface{}{"requireDistinctAgent": true}, []string{"orderIndex", "active"}},
		{[]string{"--wip-limit", "2"}, map[string]interface{}{"wipLimit": 2}, []string{"orderIndex", "active"}},
		{[]string{"--wip-limit", "0"}, map[string]interface{}{}, []string{"wipLimit", "orderIndex", "active", "requireDistinctAgent"}},
		{[]string{"--order", "44", "--require-distinct-agent", "--inactive", "--run-when-tagged-any", "x"},
			map[string]interface{}{"orderIndex": 44, "requireDistinctAgent": true, "active": false}, nil},
	} {
		in := roleInput(t, append([]string{"--name", "coder"}, c.args...)...)
		for k, v := range c.want {
			if got, ok := in[k]; !ok || got != v {
				t.Errorf("(i) %v sends %s=%v (present %v), want %v", c.args, k, got, ok, v)
			}
		}
		for _, k := range c.absent {
			if _, ok := in[k]; ok {
				t.Errorf("(i) %v sends %s: %v", c.args, k, in)
			}
		}
	}
	if in := roleInput(t, "--name", "coder", "--order", "44", "--require-distinct-agent", "--inactive",
		"--run-when-tagged-any", "x"); runWhen(in) == nil {
		t.Errorf("(i) the full set lost runWhen: %v", in)
	}
}

// (h) --run-when-clear with a list is refused before any request, exit 1, the exact words on stderr.
func TestRoleconfigSetRefusesAClearWithAList(t *testing.T) {
	if url := os.Getenv("REARM_TEST_RUN_WHEN_CHILD"); url != "" {
		c, err := rearm.New(url, "id", "secret", rearm.WithoutTokenExchange())
		if err != nil {
			os.Exit(3)
		}
		apiClient = c
		cmd := agentBoardRoleconfigSetCmd
		if err := cmd.ParseFlags([]string{"--session", "seat", "--name", "coder", "--run-when-clear",
			"--run-when-tagged-any", "x"}); err != nil {
			os.Exit(4)
		}
		cmd.Run(cmd, []string{"0b1e8828-d724-4600-88f6-5a7ac12169a2"})
		os.Exit(0)
	}
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"agentTaskRoleConfigSetProgrammatic": map[string]any{"name": "coder"}}})
	}))
	defer srv.Close()
	child := exec.Command(os.Args[0], "-test.run=^TestRoleconfigSetRefusesAClearWithAList$")
	child.Env = append(os.Environ(), "REARM_TEST_RUN_WHEN_CHILD="+srv.URL)
	var stdout, stderr strings.Builder
	child.Stdout, child.Stderr = &stdout, &stderr
	err := child.Run()
	if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 1 {
		t.Fatalf("a clear with a list should exit 1, got %v\nstdout: %s\nstderr: %s", err, stdout.String(), stderr.String())
	}
	if want := "Error: " + runWhenClearAlone + "\n"; stderr.String() != want {
		t.Errorf("stderr %q, want %q", stderr.String(), want)
	}
	if runWhenClearAlone != "--run-when-clear clears the condition; give it alone, without --run-when-tagged-any or --run-when-tagged-none" {
		t.Errorf("the refusal's words changed: %q", runWhenClearAlone)
	}
	if n := requests.Load(); n != 0 {
		t.Errorf("%d requests sent before the refusal, want none", n)
	}
	if stdout.String() != "" {
		t.Errorf("stdout carries %q", stdout.String())
	}
}

func TestRoleconfigSetHasTheRunWhenFlags(t *testing.T) {
	fs := agentBoardRoleconfigSetCmd.PersistentFlags()
	for _, name := range []string{"run-when-tagged-any", "run-when-tagged-none"} {
		f := fs.Lookup(name)
		if f == nil {
			t.Fatalf("no --%s flag", name)
		}
		if f.Value.Type() != "stringSlice" || f.DefValue != "[]" {
			t.Errorf("--%s is %s with default %q, want an empty stringSlice", name, f.Value.Type(), f.DefValue)
		}
		for _, want := range []string{"OPTIONAL role", "a list left out is sent empty"} {
			if !strings.Contains(f.Usage, want) {
				t.Errorf("--%s usage does not say %q", name, want)
			}
		}
	}
	anyUsage := fs.Lookup("run-when-tagged-any").Usage
	for _, want := range []string{"the role carrying the coordinator checkpoint",
		"task hold then task lifthold --role while QUEUED, task authorize --role while AWAITING_COORDINATOR, task reopen --role --reason once COMPLETED"} {
		if !strings.Contains(anyUsage, want) {
			t.Errorf("--run-when-tagged-any usage does not say %q", want)
		}
	}
	clear := fs.Lookup("run-when-clear")
	if clear == nil || clear.Value.Type() != "bool" || clear.DefValue != "false" {
		t.Fatalf("--run-when-clear is %+v, want a bool defaulting false", clear)
	}
	if !strings.Contains(clear.Usage, "the role runs on every task again") {
		t.Errorf("--run-when-clear usage: %s", clear.Usage)
	}
	for _, name := range []string{"run-when-tagged-any", "run-when-tagged-none", "run-when-clear"} {
		u := fs.Lookup(name).Usage
		if strings.Contains(u, "authorize --role runs") || strings.Contains(u, "Pass --order") {
			t.Errorf("--%s usage carries a withdrawn sentence: %s", name, u)
		}
	}
	src, err := os.ReadFile("agentTask.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "[--run-when-tagged-any <keys>] [--run-when-tagged-none <keys>] [--run-when-clear]") {
		t.Error("the command list comment does not name the run-when flags")
	}
}

// T-105: the condition's text, and roleconfig list's stderr lines beside its JSON.
func TestRunWhenLine(t *testing.T) {
	rw := func(any, none []interface{}) map[string]interface{} {
		return map[string]interface{}{"taggedAny": any, "taggedNone": none}
	}
	for _, c := range []struct {
		in   interface{}
		want string
	}{
		{rw([]interface{}{"mutation"}, []interface{}{}), "runs when: tagged mutation"},
		{rw([]interface{}{}, []interface{}{"hotfix"}), "runs when: not tagged hotfix"},
		{rw([]interface{}{"nightly", "soak"}, []interface{}{"hotfix"}), "runs when: tagged nightly or soak and not tagged hotfix"},
		{rw([]interface{}{}, []interface{}{}), ""},
		{nil, ""},
	} {
		if got := runWhenLine(c.in); got != c.want {
			t.Errorf("runWhenLine(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// listRoles runs roleconfig list against a fake serving roles, and returns stdout and stderr apart.
func listRoles(t *testing.T, roles ...map[string]any) (string, string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		all := []any{}
		for _, rc := range roles {
			all = append(all, rc)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"agentTaskRoleConfigsProgrammatic": all}})
	}))
	defer srv.Close()
	c, err := rearm.New(srv.URL, "id", "secret", rearm.WithoutTokenExchange())
	if err != nil {
		t.Fatal(err)
	}
	apiClient = c
	t.Cleanup(func() { apiClient = nil })
	var stdout string
	stderr := captureStderr(t, func() {
		stdout = captureStdout(t, func() {
			agentBoardRoleconfigListCmd.Run(agentBoardRoleconfigListCmd, []string{"0b1e8828-d724-4600-88f6-5a7ac12169a2"})
		})
	})
	return stdout, stderr
}

func TestRoleconfigListPrintsTheConditionOnStderr(t *testing.T) {
	roles := []map[string]any{
		{"name": "architect", "necessity": "REQUIRED", "runWhen": nil},
		{"name": "mutant_coder", "necessity": "OPTIONAL", "runWhen": map[string]any{"taggedAny": []any{"mutation"}, "taggedNone": []any{}}},
		{"name": "nightly_only", "necessity": "OPTIONAL", "runWhen": map[string]any{"taggedAny": []any{"nightly", "soak"}, "taggedNone": []any{"hotfix"}}},
	}
	stdout, stderr := listRoles(t, roles...)
	var read []map[string]any
	if err := json.Unmarshal([]byte(stdout), &read); err != nil {
		t.Fatalf("stdout is not the role JSON: %v\n%s", err, stdout)
	}
	if len(read) != 3 || read[0]["runWhen"] != nil || read[1]["runWhen"] == nil {
		t.Errorf("stdout does not carry runWhen as served: %v", read)
	}
	if want := "mutant_coder: runs when: tagged mutation\nnightly_only: runs when: tagged nightly or soak and not tagged hotfix\n"; stderr != want {
		t.Errorf("stderr:\n%q\nwant:\n%q", stderr, want)
	}
	if _, stderr = listRoles(t, roles[0]); stderr != "" {
		t.Errorf("a board with no conditional role prints %q on stderr", stderr)
	}
}

// The record's fixtures: the two entries of the TPF scenario, as a JSON read yields them.
func skipEntry(role string, live bool, waives ...interface{}) map[string]interface{} {
	if waives == nil {
		waives = []interface{}{}
	}
	return map[string]interface{}{"role": role, "roleUuid": "r-" + role, "reason": "runWhen not met: no mutation tag",
		"at": "2026-11-05T13:00:00Z", "by": map[string]interface{}{"kind": "SYSTEM", "uuid": nil, "name": "routing"},
		"waives": waives, "live": live}
}

func signOffOf(role, at string) map[string]interface{} {
	return map[string]interface{}{"role": role, "outcome": "PASSED", "signedOffAt": at}
}

const (
	coderSkipLine  = "- passed over mutant_coder at 2026-11-05T13:00:00Z: runWhen not met: no mutation tag\n"
	testerSkipLine = "- passed over mutant_tester at 2026-11-05T13:00:00Z: runWhen not met: no mutation tag; waives BOARD_TEST_REPORT\n"
	hintText       = "to run a passed-over role now, name it with the verb the task's state admits: task hold then task lifthold --role <role> while QUEUED; task authorize --role <role> while AWAITING_COORDINATOR; task reopen --role <role> --reason '<why>' once COMPLETED"
)

// T-106: the brief's passed-over lines after the split request line, the (since ran) marker read off live, and no
// run-it-now hint for any role (T-1: task show prints it). The fixture leaves the waiver out, so the substring is the
// checkpoint line, the split request line and the two skip lines.
func TestBriefPrintsThePassedOverLines(t *testing.T) {
	checkpoint := map[string]interface{}{"state": "SENT_ON", "beforeRole": "coder", "afterRole": "reviewer",
		"settledBy": map[string]interface{}{"kind": "SESSION", "uuid": "s-1", "name": "seat"},
		"settledAt": "2026-11-05T03:00:00Z", "note": "authorized for coder"}
	split := requestedSplit()
	task := func(status, role string, entries ...interface{}) map[string]interface{} {
		return map[string]interface{}{"key": "TPF-1", "title": "t", "status": status, "role": role,
			"hold":             map[string]interface{}{"kind": "MANUAL", "level": "TASK", "reason": "why"},
			"designCheckpoint": checkpoint, "splitRequest": split, "rolesSkippedByCondition": entries}
	}
	render := func(role string, t map[string]interface{}) string {
		return renderTaskBrief(&taskBrief{Key: "TPF-1", Role: role, Task: briefTaskFields(t)})
	}
	both := task("QUEUED", "tester", skipEntry("mutant_coder", true), skipEntry("mutant_tester", true, "BOARD_TEST_REPORT"))
	out := render("coder", both)
	want := "- " + designCheckpointLine(checkpoint) + "\n- " + splitRequestLine(split) + "\n" + coderSkipLine + testerSkipLine
	if !strings.Contains(out, want) {
		t.Errorf("the brief does not print, in order:\n%s\ngot:\n%s", want, out)
	}
	if strings.Contains(out, "r-mutant_coder") || strings.Contains(out, "routing") {
		t.Errorf("the brief prints roleUuid or by:\n%s", out)
	}
	fields := briefTaskFields(both)
	if got := asList(fields["rolesSkippedByCondition"]); len(got) != 2 || !reflect.DeepEqual(got[1], skipEntry("mutant_tester", true, "BOARD_TEST_REPORT")) {
		t.Errorf("the brief's JSON carries %v", fields["rolesSkippedByCondition"])
	}

	// An empty list, or none: no line, no key.
	for _, bare := range []map[string]interface{}{task("QUEUED", "tester"), {"key": "TPF-1", "title": "t", "status": "QUEUED", "role": "tester"}} {
		if out := render("coordinator", bare); strings.Contains(out, "passed over") {
			t.Errorf("a task without entries prints a passed-over line:\n%s", out)
		}
		if _, ok := briefTaskFields(bare)["rolesSkippedByCondition"]; ok {
			t.Error("a task without entries has the key in the brief's JSON")
		}
	}

	// A blind reader's entry (by and roleUuid null) prints the same line; two waives print joined.
	blind := skipEntry("mutant_coder", true)
	blind["by"], blind["roleUuid"] = nil, nil
	out = render("coder", task("QUEUED", "tester", blind, skipEntry("mutant_tester", true, "BOARD_TEST_REPORT", "ARCHITECTURE")))
	if !strings.Contains(out, coderSkipLine) || strings.Contains(out, "null") || strings.Contains(out, "<nil>") {
		t.Errorf("a null actor changes the line:\n%s", out)
	}
	if !strings.Contains(out, "; waives BOARD_TEST_REPORT, ARCHITECTURE\n") {
		t.Errorf("two waives are not joined:\n%s", out)
	}

	// (f) the marker reads live, whatever the sign-offs say.
	dead := task("QUEUED", "tester", skipEntry("mutant_coder", false), skipEntry("mutant_tester", true, "BOARD_TEST_REPORT"))
	dead["signOffs"] = []interface{}{signOffOf("mutant_coder", "2026-11-05T12:00:00Z")}
	out = render("coder", dead)
	if !strings.Contains(out, strings.TrimSuffix(coderSkipLine, "\n")+" (since ran)\n"+testerSkipLine) {
		t.Errorf("(f) the live false entry is not marked, or the live one is:\n%s", out)
	}
	liveWithLaterSignOff := task("QUEUED", "tester", skipEntry("mutant_coder", true))
	liveWithLaterSignOff["signOffs"] = []interface{}{signOffOf("mutant_coder", "2026-11-05T14:00:00Z")}
	if out = render("coder", liveWithLaterSignOff); !strings.Contains(out, coderSkipLine) || strings.Contains(out, "since ran") {
		t.Errorf("(f) a live entry is marked from the sign-offs:\n%s", out)
	}
	if out = render("coder", task("QUEUED", "tester", skipEntry("mutant_tester", false, "BOARD_TEST_REPORT"))); !strings.Contains(out,
		"; waives BOARD_TEST_REPORT (since ran)\n") {
		t.Errorf("(f) the marker is not last:\n%s", out)
	}
	noLive := skipEntry("mutant_coder", true)
	delete(noLive, "live")
	if out = render("coder", task("QUEUED", "tester", noLive)); !strings.Contains(out, coderSkipLine) {
		t.Errorf("(f) an entry without live is marked:\n%s", out)
	}

	// (g) no hint in the brief (T-1), for the seat's name, a worker's role or the task's own role: the brief is the
	// same brief with the two lines added after the split request line, and nothing else.
	bare := task("QUEUED", "tester")
	for _, role := range []string{"coordinator", "coder", "tester"} {
		out = render(role, both)
		if strings.Contains(out, "to run a passed-over role") {
			t.Errorf("(g) the %s brief prints the hint:\n%s", role, out)
		}
		splitLine := "- " + splitRequestLine(split) + "\n"
		if want := strings.Replace(render(role, bare), splitLine, splitLine+coderSkipLine+testerSkipLine, 1); out != want {
			t.Errorf("(g) the %s brief is not the bare brief plus the two lines:\n%s\nwant:\n%s", role, out, want)
		}
	}
	// The window (F-4): the coordinator ran the mutant coder; the entries are still live, print as served, no hint.
	running := task("ASSIGNED", "mutant_coder", skipEntry("mutant_coder", true), skipEntry("mutant_tester", true, "BOARD_TEST_REPORT"))
	running["assignment"] = map[string]interface{}{"session": "s9", "role": "mutant_coder", "assignedAt": "2026-11-05T13:30:00Z"}
	for _, role := range []string{"coordinator", "mutant_coder"} {
		out = render(role, running)
		if !strings.Contains(out, coderSkipLine+testerSkipLine) || strings.Contains(out, "since ran") || strings.Contains(out, "to run a passed-over role") {
			t.Errorf("(g) the window: the %s brief does not print the live entries as served, or prints the hint:\n%s", role, out)
		}
	}
}

// T-118: the brief's copy keeps the read's shape, so a JSON-decoded task renders the lines (F-2), and no hint (T-1).
func TestBriefCarriesThePassedOverListInTheReadsShape(t *testing.T) {
	var task map[string]interface{}
	if err := json.Unmarshal([]byte(`{"key":"TPF-1","title":"t","status":"QUEUED","role":"tester","rolesSkippedByCondition":`+
		`[{"role":"mutant_coder","roleUuid":"r1","reason":"runWhen not met: no mutation tag","at":"2026-11-05T13:00:00Z",`+
		`"by":{"kind":"SYSTEM","uuid":null,"name":"routing"},"waives":["BOARD_TEST_REPORT"],"live":true}]}`), &task); err != nil {
		t.Fatal(err)
	}
	out := briefTaskFields(task)
	if _, ok := out["rolesSkippedByCondition"].([]interface{}); !ok {
		t.Fatalf("the brief stores %T, want the read's []interface{}", out["rolesSkippedByCondition"])
	}
	if n := len(asList(out["rolesSkippedByCondition"])); n != 1 {
		t.Errorf("asList reads %d entries from the copy, want 1", n)
	}
	if got := liveSkipRoles(out); !reflect.DeepEqual(got, []string{"mutant_coder"}) {
		t.Errorf("liveSkipRoles reads %v from the copy", got)
	}
	brief := renderTaskBrief(&taskBrief{Key: "TPF-1", Role: "coordinator", Task: out})
	if want := "- passed over mutant_coder at 2026-11-05T13:00:00Z: runWhen not met: no mutation tag; waives BOARD_TEST_REPORT\n"; !strings.Contains(brief, want) {
		t.Errorf("the brief does not print\n%s\ngot:\n%s", want, brief)
	}
	if strings.Contains(brief, "to run a passed-over role") {
		t.Errorf("the brief prints the hint:\n%s", brief)
	}
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]interface{}
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back["rolesSkippedByCondition"], task["rolesSkippedByCondition"]) {
		t.Errorf("the brief's JSON does not round-trip the entry: %v", back["rolesSkippedByCondition"])
	}
}

// T-107: task show prints the entries on stderr keyed by task, after the base-moved and waiver lines, then the
// run-it-now hint once while an entry is live (T-1).
func TestTaskShowPrintsThePassedOverLinesOnStderr(t *testing.T) {
	skipped := func(key, uuid string) map[string]any {
		return map[string]any{"key": key, "uuid": uuid, "status": "QUEUED", "role": "tester",
			"rolesSkippedByCondition": []any{skipEntry("mutant_coder", false), skipEntry("mutant_tester", true, "BOARD_TEST_REPORT")}}
	}
	const (
		coderShow  = "TPF-156: passed over mutant_coder at 2026-11-05T13:00:00Z: runWhen not met: no mutation tag (since ran)\n"
		testerShow = "TPF-156: passed over mutant_tester at 2026-11-05T13:00:00Z: runWhen not met: no mutation tag; waives BOARD_TEST_REPORT\n"
		hintShow   = "TPF-156: " + hintText + "\n"
	)
	// (a) the two lines and the hint on stderr (one entry is live), the JSON on stdout with live as served.
	stdout, stderr := showTasks(t, skipped("TPF-156", "t-1"))
	if want := coderShow + testerShow + hintShow; stderr != want {
		t.Errorf("(a) stderr:\n%q\nwant:\n%q", stderr, want)
	}
	var read map[string]any
	if err := json.Unmarshal([]byte(stdout), &read); err != nil {
		t.Fatalf("(a) stdout is not the task JSON: %v\n%s", err, stdout)
	}
	if l, _ := read["rolesSkippedByCondition"].([]any); len(l) != 2 || l[0].(map[string]any)["live"] != false {
		t.Errorf("(a) stdout carries %v", read["rolesSkippedByCondition"])
	}
	if strings.Contains(stdout, "passed over mutant") || strings.Contains(stdout, "to run a passed-over role") {
		t.Error("(a) a passed-over line or the hint reached stdout")
	}
	// (b) an empty list: nothing.
	if _, stderr = showTasks(t, map[string]any{"key": "TPF-2", "uuid": "t-2", "rolesSkippedByCondition": []any{}}); stderr != "" {
		t.Errorf("(b) stderr carries %q", stderr)
	}
	// (c) two tasks: each keyed by its own task, the uuid when it has no key.
	_, stderr = showTasks(t, skipped("TPF-156", "t-1"), skipped("", "t-9"))
	if want := coderShow + testerShow + hintShow + strings.ReplaceAll(coderShow+testerShow+hintShow, "TPF-156", "t-9"); stderr != want {
		t.Errorf("(c) stderr:\n%q\nwant:\n%q", stderr, want)
	}
	// (d) base-moved first, the two waiver lines, then the skip lines.
	all := waivedTask("TPF-156", "t-1", true)
	all["pullRequests"] = []any{map[string]any{"url": "https://github.com/o/r/pull/1", "baseMovedBy": float64(2)}}
	all["rolesSkippedByCondition"] = skipped("TPF-156", "t-1")["rolesSkippedByCondition"]
	_, stderr = showTasks(t, all)
	if want := "TPF-156: base moved: 2 commits since your round: https://github.com/o/r/pull/1\n" +
		strings.ReplaceAll(waivedShowLine+waiverShowLine+"\n", "TPF-153", "TPF-156") + coderShow + testerShow + hintShow; stderr != want {
		t.Errorf("(d) stderr:\n%q\nwant:\n%q", stderr, want)
	}
	// (e) the hint (T-1): right after the task's last entry line and nothing after it; none without a live entry.
	if _, stderr = showTasks(t, skipped("TPF-156", "t-1")); !strings.HasSuffix(stderr, testerShow+hintShow) || strings.Count(stderr, hintText) != 1 {
		t.Errorf("(e) the hint is not once, right after the entry lines, and last:\n%q", stderr)
	}
	dead := skipped("TPF-156", "t-1")
	dead["rolesSkippedByCondition"] = []any{skipEntry("mutant_coder", false), skipEntry("mutant_tester", false, "BOARD_TEST_REPORT")}
	dead["signOffs"] = []any{signOffOf("mutant_coder", "2026-11-05T14:00:00Z")}
	if _, stderr = showTasks(t, dead); stderr != coderShow+strings.TrimSuffix(testerShow, "\n")+" (since ran)\n" {
		t.Errorf("(e) a task with no live entry:\n%q", stderr)
	}
	if _, stderr = showTasks(t, map[string]any{"key": "TPF-2", "uuid": "t-2", "rolesSkippedByCondition": []any{}}); stderr != "" {
		t.Errorf("(e) an empty list prints %q", stderr)
	}
	deadOther := map[string]any{"key": "TPF-7", "uuid": "t-7", "status": "QUEUED", "role": "tester",
		"rolesSkippedByCondition": []any{skipEntry("mutant_coder", false)}}
	_, stderr = showTasks(t, skipped("TPF-156", "t-1"), deadOther, skipped("", "t-9"))
	if want := coderShow + testerShow + hintShow + strings.ReplaceAll(coderShow, "TPF-156", "TPF-7") +
		strings.ReplaceAll(coderShow+testerShow+hintShow, "TPF-156", "t-9"); stderr != want {
		t.Errorf("(e) the array read: each task's hint after its own lines, none without a live entry:\n%q\nwant:\n%q", stderr, want)
	}
	// (f) the window (F-4): the passed-over role runs on an explicit authorize; both entries live, the hint the same.
	running := skipped("TPF-156", "t-1")
	running["status"], running["role"] = "ASSIGNED", "mutant_tester"
	running["assignment"] = map[string]any{"session": "s9", "role": "mutant_tester", "assignedAt": "2026-11-05T13:30:00Z"}
	running["rolesSkippedByCondition"] = []any{skipEntry("mutant_coder", true), skipEntry("mutant_tester", true, "BOARD_TEST_REPORT")}
	if _, stderr = showTasks(t, running); stderr != strings.Replace(coderShow, " (since ran)", "", 1)+testerShow+hintShow {
		t.Errorf("(f) the window:\n%q", stderr)
	}
}

// T-117: the live roles, the compact clause and task list's summary read live and nothing else.
func TestLiveSkipRoles(t *testing.T) {
	task := func(entries ...interface{}) map[string]interface{} {
		return map[string]interface{}{"key": "TPF-1", "status": "QUEUED", "role": "tester", "rolesSkippedByCondition": entries}
	}
	older := skipEntry("mutant_coder", false)
	older["at"] = "2026-11-05T11:00:00Z"
	derived := task(skipEntry("mutant_coder", true))
	derived["signOffs"] = []interface{}{signOffOf("mutant_coder", "2026-11-05T14:00:00Z")}
	derived["reopens"] = []interface{}{map[string]interface{}{"toRole": "mutant_coder", "at": "2026-11-05T15:00:00Z"}}
	noLive := skipEntry("mutant_coder", true)
	delete(noLive, "live")
	window := task(skipEntry("mutant_coder", true), skipEntry("mutant_tester", true))
	window["status"], window["role"] = "ASSIGNED", "mutant_coder"
	for _, c := range []struct {
		name string
		task map[string]interface{}
		want []string
	}{
		{"(a) both live", task(skipEntry("mutant_coder", true), skipEntry("mutant_tester", true)), []string{"mutant_coder", "mutant_tester"}},
		{"(b) one since ran", task(skipEntry("mutant_coder", false), skipEntry("mutant_tester", true)), []string{"mutant_tester"}},
		{"(c) none live", task(skipEntry("mutant_coder", false), skipEntry("mutant_tester", false)), nil},
		{"(d) an old and a current entry", task(older, skipEntry("mutant_coder", true)), []string{"mutant_coder"}},
		{"(e) live beside a later sign-off and reopen", derived, []string{"mutant_coder"}},
		{"(f) not live, nothing after it", task(skipEntry("mutant_coder", false)), nil},
		{"(g) no live key", task(noLive), nil},
		{"(h) the window", window, []string{"mutant_coder", "mutant_tester"}},
	} {
		if got := liveSkipRoles(c.task); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
	if got := conditionSkipLine(noLive); strings.Contains(got, "since ran") {
		t.Errorf("(g) an entry without live is marked: %q", got)
	}
}

func TestCompactTaskNamesThePassedOverRoles(t *testing.T) {
	task := func(status, role string, entries ...interface{}) map[string]interface{} {
		return map[string]interface{}{"key": "TPF-1", "status": status, "role": role, "rolesSkippedByCondition": entries}
	}
	a := []interface{}{skipEntry("mutant_coder", true), skipEntry("mutant_tester", true)}
	running := task("ASSIGNED", "mutant_coder", a...)
	running["assignment"] = map[string]interface{}{"session": "s9-session"}
	for _, c := range []struct {
		name       string
		task       map[string]interface{}
		assignment map[string]interface{}
		want       string
	}{
		{"(a)", task("QUEUED", "tester", a...), nil, "TPF-1 queued, role tester, passed over: mutant_coder, mutant_tester\nqueued for tester"},
		{"(b)", task("QUEUED", "tester", skipEntry("mutant_coder", false), skipEntry("mutant_tester", true)), nil,
			"TPF-1 queued, role tester, passed over: mutant_tester\nqueued for tester"},
		{"(c)", task("QUEUED", "tester", skipEntry("mutant_coder", false), skipEntry("mutant_tester", false)), nil,
			"TPF-1 queued, role tester\nqueued for tester"},
		{"before the prompt clause", task("QUEUED", "coder", skipEntry("mutant_coder", true)),
			map[string]interface{}{"promptVersion": "abc123"}, "TPF-1 queued, role coder, passed over: mutant_coder, prompt abc123\nqueued for coder"},
		{"the window", running, nil, "TPF-1 assigned, role mutant_coder, passed over: mutant_coder, mutant_tester\n" +
			"held by session " + short("s9-session") + "; read it with: rearm agent task brief TPF-1 --session s9-session"},
	} {
		if got := compactTask(c.task, c.assignment); got != c.want {
			t.Errorf("%s: printed\n%s\nwant\n%s", c.name, got, c.want)
		}
	}
}

// listTasks runs task list against a fake serving tasks, and returns stdout and stderr apart.
func listTasks(t *testing.T, tasks ...map[string]any) (string, string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		all := []any{}
		for _, task := range tasks {
			all = append(all, task)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"agentTasksProgrammatic": all}})
	}))
	defer srv.Close()
	c, err := rearm.New(srv.URL, "id", "secret", rearm.WithoutTokenExchange())
	if err != nil {
		t.Fatal(err)
	}
	apiClient = c
	taskBoardUuid = "0b1e8828-d724-4600-88f6-5a7ac12169a2"
	t.Cleanup(func() { apiClient, taskBoardUuid = nil, "" })
	var stdout string
	stderr := captureStderr(t, func() {
		stdout = captureStdout(t, func() { agentTaskListCmd.Run(agentTaskListCmd, nil) })
	})
	return stdout, stderr
}

func TestTaskListPrintsThePassedOverSummaryOnStderr(t *testing.T) {
	live := map[string]any{"key": "TPF-1", "uuid": "t-1", "status": "QUEUED", "role": "tester",
		"rolesSkippedByCondition": []any{skipEntry("mutant_coder", true), skipEntry("mutant_tester", true)}}
	dead := map[string]any{"key": "TPF-2", "uuid": "t-2", "status": "QUEUED", "role": "tester",
		"rolesSkippedByCondition": []any{skipEntry("mutant_coder", false)}}
	bare := map[string]any{"key": "TPF-3", "uuid": "t-3", "status": "QUEUED", "role": "tester", "rolesSkippedByCondition": []any{}}
	stdout, stderr := listTasks(t, live, dead)
	var read []map[string]any
	if err := json.Unmarshal([]byte(stdout), &read); err != nil {
		t.Fatalf("stdout is not the task JSON: %v\n%s", err, stdout)
	}
	if len(read) != 2 {
		t.Fatalf("stdout carries %d tasks", len(read))
	}
	if l, _ := read[0]["rolesSkippedByCondition"].([]any); len(l) != 2 || l[0].(map[string]any)["live"] != true {
		t.Errorf("stdout does not carry the entries as served: %v", read[0]["rolesSkippedByCondition"])
	}
	if want := "TPF-1: passed over: mutant_coder, mutant_tester\n"; stderr != want {
		t.Errorf("stderr:\n%q\nwant:\n%q", stderr, want)
	}
	if _, stderr = listTasks(t, dead, bare); stderr != "" {
		t.Errorf("no live entry, yet stderr carries %q", stderr)
	}
	window := map[string]any{"key": "TPF-1", "uuid": "t-1", "status": "ASSIGNED", "role": "mutant_coder",
		"rolesSkippedByCondition": []any{skipEntry("mutant_coder", true)}}
	if _, stderr = listTasks(t, window); stderr != "TPF-1: passed over: mutant_coder\n" {
		t.Errorf("the window: stderr %q", stderr)
	}
}
