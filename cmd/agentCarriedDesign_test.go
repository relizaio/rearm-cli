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

// The carried design on a split child as the CLI sends and prints it (task TPF-163, design 6.2 and 7.2).

const (
	carriedArchPath = "boards/x/design/TPF-12/architecture-1.md"
	carriedTmPath   = "boards/x/design/TPF-12/threat_model-1.md"
	carriedSession  = "5e55-1011-abcd-architect-session"
)

// carriedRecord is a carried design as a task read returns it: the architect's agent pass, the functional
// reviewer's REJECTED pass a person decided over, the security reviewer's pass signed by a person.
func carriedRecord(standing bool) map[string]any {
	return map[string]any{"parentTask": "p-12", "parentKey": "TPF-12",
		"rounds": []any{
			map[string]any{"specification": "ARCHITECTURE", "release": "r-arch", "round": float64(1), "path": carriedArchPath},
			map[string]any{"specification": "THREAT_MODEL", "release": "r-tm", "round": float64(1), "path": carriedTmPath},
		},
		"roles": []any{
			map[string]any{"role": "architect", "roleUuid": "ro-1", "signedOffAt": "2026-11-06T17:38:12.345Z",
				"session": carriedSession, "reviewedBy": nil, "decidedOver": false, "live": true},
			map[string]any{"role": "functional_reviewer", "roleUuid": "ro-2", "signedOffAt": "2026-11-06T17:47:00Z",
				"session": carriedSession, "reviewedBy": nil, "decidedOver": true, "live": true},
			map[string]any{"role": "security_reviewer", "roleUuid": "ro-3", "signedOffAt": "2026-11-06T17:50:00Z",
				"session": nil, "reviewedBy": map[string]any{"kind": "USER", "uuid": "u-1", "name": "Pat"},
				"decidedOver": false, "live": true},
		},
		"at": "2026-11-06T18:00:00Z", "by": map[string]any{"kind": "SESSION", "uuid": "seat", "name": "coord"},
		"standing": standing}
}

const carriedLine = "design carried from TPF-12: ARCHITECTURE round 1 (" + carriedArchPath + "), THREAT_MODEL round 1 (" +
	carriedTmPath + "); passed by architect (2026-11-06 17:38 UTC), functional_reviewer (2026-11-06 17:47 UTC, decided over" +
	" by a person), security_reviewer (2026-11-06 17:50 UTC, by Pat); standing"

// T-1: --children-json reaches the server with designFromParent as given, through the split verb end to end.
func TestSplitPassesDesignFromParent(t *testing.T) {
	if url := os.Getenv("REARM_TEST_CARRIED_SPLIT_CHILD"); url != "" {
		c, err := rearm.New(url, "id", "secret", rearm.WithoutTokenExchange())
		if err != nil {
			os.Exit(3)
		}
		apiClient = c
		taskSessionUuid = "seat"
		if err := agentTaskSplitCmd.ParseFlags([]string{"--children-json", os.Getenv("REARM_TEST_CARRIED_SPLIT_JSON")}); err != nil {
			os.Exit(4)
		}
		agentTaskSplitCmd.Run(agentTaskSplitCmd, []string{"t-1"})
		os.Exit(0)
	}
	var mu sync.Mutex
	var sent map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			OperationName string         `json:"operationName"`
			Variables     map[string]any `json:"variables"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		if body.OperationName == "AgentTaskSplitProgrammatic" {
			sent = body.Variables
		}
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"agentTaskSplitProgrammatic": map[string]any{"key": "TPF-12", "status": "COMPLETED"}}})
	}))
	defer srv.Close()
	child := exec.Command(os.Args[0], "-test.run=^TestSplitPassesDesignFromParent$")
	child.Env = append(os.Environ(), "REARM_TEST_CARRIED_SPLIT_CHILD="+srv.URL,
		`REARM_TEST_CARRIED_SPLIT_JSON=[{"title":"a","designFromParent":true},{"title":"b"}]`)
	if out, err := child.CombinedOutput(); err != nil {
		t.Fatalf("split exited %v: %s", err, out)
	}
	want := []any{map[string]any{"title": "a", "designFromParent": true}, map[string]any{"title": "b"}}
	if !reflect.DeepEqual(sent["children"], want) {
		t.Errorf("children sent as %#v, want %#v", sent["children"], want)
	}
	if !strings.Contains(agentTaskSplitCmd.Long, "designFromParent (boolean, default false): build the child from the"+
		" parent's\nreviewed design; it skips its design stage and goes to the build role") {
		t.Errorf("split's help does not describe designFromParent:\n%s", agentTaskSplitCmd.Long)
	}
	if !strings.Contains(agentTaskReturnCmd.Long, "designFromParent: true as a suggestion") ||
		!strings.Contains(agentTaskReturnCmd.Long, "the seat decides") {
		t.Errorf("return's help does not call designFromParent a suggestion:\n%s", agentTaskReturnCmd.Long)
	}
}

// T-2 (TM T-14): --from-request copies designFromParent through the explicit field list when the proposal set it;
// an unflagged child is sent without the key.
func TestFromRequestKeepsDesignFromParent(t *testing.T) {
	flagged := readChild("one")
	flagged["designFromParent"] = true
	plain := readChild("two")
	plain["designFromParent"] = nil
	got, err := proposalChildren(map[string]any{"key": "TPF-12", "splitRequest": requestedSplit(flagged, plain)}, "t-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0]["designFromParent"] != true {
		t.Errorf("the flagged child is sent as %#v", got)
	}
	if _, ok := got[1]["designFromParent"]; ok {
		t.Errorf("the unflagged child carries designFromParent: %#v", got[1])
	}
}

// T-3 (functional F-1): task show prints the 6.2 line on stderr, keyed by task, after the waiver lines; a role whose
// carried pass no longer stands says so; the JSON on stdout keeps the record.
func TestShowPrintsTheCarriedDesign(t *testing.T) {
	task := map[string]any{"key": "TPF-13", "uuid": "t-13", "status": "ASSIGNED", "role": "coder",
		"carriedDesign": carriedRecord(true)}
	stdout, stderr := showTasks(t, task)
	if want := "TPF-13: " + carriedLine + "\n"; stderr != want {
		t.Errorf("stderr:\n%q\nwant:\n%q", stderr, want)
	}
	for _, part := range []string{"; passed by ", "; standing\n"} {
		if !strings.Contains(stderr, part) {
			t.Errorf("stderr has no %q", part)
		}
	}
	var read map[string]any
	if err := json.Unmarshal([]byte(stdout), &read); err != nil {
		t.Fatalf("stdout is not the task JSON: %v\n%s", err, stdout)
	}
	if c, _ := read["carriedDesign"].(map[string]any); c == nil || c["parentKey"] != "TPF-12" {
		t.Errorf("stdout does not carry the record: %v", read["carriedDesign"])
	}

	stale := carriedRecord(false)
	stale["roles"].([]any)[1].(map[string]any)["live"] = false
	task["carriedDesign"] = stale
	_, stderr = showTasks(t, task)
	want := "TPF-13: " + strings.TrimSuffix(carriedLine, "; standing") + "; functional_reviewer no longer carried; not standing\n"
	if stderr != want {
		t.Errorf("stale stderr:\n%q\nwant:\n%q", stderr, want)
	}

	// A waived task's lines come first; a task without the record prints nothing for it.
	waived := waivedTask("TPF-153", "t-1", true)
	waived["carriedDesign"] = carriedRecord(true)
	_, stderr = showTasks(t, waived, map[string]any{"key": "TPF-2", "uuid": "t-2"})
	if want := waivedShowLine + waiverShowLine + "\nTPF-153: " + carriedLine + "\n"; stderr != want {
		t.Errorf("with a waiver:\n%q\nwant:\n%q", stderr, want)
	}
}

// carriedChildFixture is a carried child assigned to the coder, whose ARCHITECTURE input resolved to the carried
// round; it has no document of its own.
func carriedChildFixture() map[string]any {
	return map[string]any{"uuid": "t-1", "key": "TPF-13", "board": "b-1", "title": "child", "status": "ASSIGNED",
		"role": "coder", "carriedDesign": carriedRecord(true),
		"requiredInputs": []any{
			map[string]any{"kind": "DOCUMENT", "specification": "ARCHITECTURE", "release": "r-arch"},
			map[string]any{"kind": "DOCUMENT", "specification": "THREAT_MODEL", "release": "r-tm"},
		},
		"assignment": map[string]any{"session": "s-1", "role": "coder", "resolvedInputs": []any{
			map[string]any{"kind": "DOCUMENT", "specification": "ARCHITECTURE", "release": "r-arch", "version": "3", "lifecycle": "READY_TO_SHIP"},
		}},
		"documents": []any{}}
}

// T-4: the brief prints the line after the pinned inputs, lists the carried rounds under its documents, and
// --inline prints the carried round the role's input resolved to.
func TestBriefPrintsTheCarriedDesign(t *testing.T) {
	checkout := briefWorld(t, &briefBoard{task: carriedChildFixture()}, false)
	if err := os.MkdirAll(filepath.Join(checkout, "boards/x/design/TPF-12"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(checkout, carriedArchPath), []byte("PARENT ROUND ONE\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	briefInline = true
	out := runBrief(t)
	if !strings.Contains(out, "- pinned inputs: ARCHITECTURE r-arch, THREAT_MODEL r-tm\n- "+carriedLine+"\n") {
		t.Errorf("no carried line after the pinned inputs:\n%s", out)
	}
	docs := "## 3. Its documents\n\nCarried from TPF-12:\n- ARCHITECTURE round 1 (ready to ship) `" + carriedArchPath +
		"`\n- THREAT_MODEL round 1 `" + carriedTmPath + "`\n"
	if !strings.Contains(out, docs) {
		t.Errorf("the documents section does not list the carried rounds:\n%s", out)
	}
	if strings.Contains(out, "None yet.\n\n## 4.") {
		t.Errorf("the documents section says none beside the carried rounds:\n%s", out)
	}
	if !strings.Contains(out, "### ARCHITECTURE round 1 (`"+carriedArchPath+"`)\n\nPARENT ROUND ONE\n") {
		t.Errorf("the carried round is not inlined:\n%s", out)
	}
}

// T-4: which round --inline takes: the bound release, else a carried round only when the task has none of its own.
func TestInlineTakesTheCarriedRoundOnlyWhenItIsTheInput(t *testing.T) {
	own := []briefDocument{{Release: "r-own", Specification: "ARCHITECTURE", Round: 2, Path: "own.md"}}
	carried := []briefCarriedRound{{Release: "r-arch", Specification: "ARCHITECTURE", Round: 1, Path: carriedArchPath}}
	for _, c := range []struct {
		name  string
		docs  []briefDocument
		bound map[string]string
		want  string
	}{
		{"bound to the carried round", own, map[string]string{"ARCHITECTURE": "r-arch"}, carriedArchPath},
		{"bound to its own round", own, map[string]string{"ARCHITECTURE": "r-own"}, "own.md"},
		{"unassigned, no own round", nil, map[string]string{}, carriedArchPath},
		{"unassigned, an own round", own, map[string]string{}, "own.md"},
	} {
		in := briefInlineInputs(c.docs, []string{"ARCHITECTURE"}, "", carried, c.bound)
		if len(in) != 1 || in[0].Path != c.want {
			t.Errorf("%s: inlined %+v, want %s", c.name, in, c.want)
		}
	}
	task := carriedChildFixture()
	if got := boundReleases(task, "coder"); got["ARCHITECTURE"] != "r-arch" {
		t.Errorf("the coder's bound releases: %v", got)
	}
	if got := boundReleases(task, "tester"); len(got) != 0 {
		t.Errorf("another role's brief reads the coder's bindings: %v", got)
	}
}

// T-5: every task read the CLI sends comes from the pinned rearm-client-go and selects the record and the flag;
// this fails when the pin predates the TPF-163 client-go PR.
func TestTaskReadsSelectCarriedDesign(t *testing.T) {
	for name, op := range map[string]string{
		"AgentTaskProgrammatic":          rearm.AgentTaskProgrammatic_Operation,
		"AgentTasksByUuidProgrammatic":   rearm.AgentTasksByUuidProgrammatic_Operation,
		"AgentTasksProgrammatic":         rearm.AgentTasksProgrammatic_Operation,
		"AgentTaskReturnProgrammatic":    rearm.AgentTaskReturnProgrammatic_Operation,
		"AgentTaskAuthorizeProgrammatic": rearm.AgentTaskAuthorizeProgrammatic_Operation,
		"AgentTaskSplitProgrammatic":     rearm.AgentTaskSplitProgrammatic_Operation,
		"AgentTaskNextProgrammatic":      rearm.AgentTaskNextProgrammatic_Operation,
		"AgentTaskAssignProgrammatic":    rearm.AgentTaskAssignProgrammatic_Operation,
	} {
		n := strings.Join(strings.Fields(op), " ")
		for _, want := range []string{
			"carriedDesign { parentTask parentKey rounds { specification release round path } roles { role roleUuid" +
				" signedOffAt session reviewedBy {",
			"decidedOver live } at by {",
			"standing }",
			"tags { key value } designFromParent }",
		} {
			if !strings.Contains(n, want) {
				t.Errorf("%s does not select %q; is rearm-client-go pinned at the TPF-163 PR (#107) or later?", name, want)
			}
		}
	}
}

// T-6 (TM T-1): the split request line marks a flagged proposed title, in the brief too.
func TestSplitRequestLineMarksFlaggedChildren(t *testing.T) {
	flagged := readChild("part one")
	flagged["designFromParent"] = true
	plain := readChild("part two")
	plain["designFromParent"] = false
	const want = "split request by coder 2026-11-03T06:00:00Z: two parts; proposes: part one [design from parent]; part two"
	r := requestedSplit(flagged, plain)
	if got := splitRequestLine(r); got != want {
		t.Errorf("%q, want %q", got, want)
	}
	task := map[string]any{"key": "TPF-12", "title": "t", "status": "AWAITING_COORDINATOR", "role": "coder", "splitRequest": r}
	if out := renderTaskBrief(&taskBrief{Key: "TPF-12", Role: "coder", Task: briefTaskFields(task)}); !strings.Contains(out, "- "+want+"\n") {
		t.Errorf("the brief does not print the marked line:\n%s", out)
	}
}

// T-7 (TM T-12, parent F-14/F-16): a person is named only from reviewedBy; a decided-over agent pass says so without
// a name; a blind read (session and reviewedBy null) reads as an agent's pass; the session is printed nowhere and no
// null placeholder appears.
func TestCarriedDesignLineNamesNoSession(t *testing.T) {
	c := carriedRecord(true)
	blind := carriedRecord(true)
	for _, r := range blind["roles"].([]any) {
		m := r.(map[string]any)
		if m["reviewedBy"] == nil {
			m["session"] = nil
		}
	}
	for name, rec := range map[string]map[string]any{"full read": c, "blind read": blind} {
		line := carriedDesignLine(rec)
		if line != carriedLine {
			t.Errorf("%s: %q\nwant %q", name, line, carriedLine)
		}
		if strings.Contains(line, carriedSession) || strings.Contains(line, "seat") {
			t.Errorf("%s: a session in %q", name, line)
		}
		for _, p := range []string{"null", "<nil>", "()", "by ,"} {
			if strings.Contains(line, p) {
				t.Errorf("%s: placeholder %q in %q", name, p, line)
			}
		}
	}
	if !strings.Contains(carriedLine, "architect (2026-11-06 17:38 UTC), ") {
		t.Error("an agent pass names someone")
	}
	none := map[string]any{"parentKey": "TPF-12", "rounds": []any{map[string]any{"specification": "ARCHITECTURE",
		"release": "r", "round": nil, "path": nil}}, "roles": []any{map[string]any{"role": "architect",
		"signedOffAt": nil, "session": nil, "reviewedBy": nil, "decidedOver": false, "live": true}}, "standing": true}
	if got, want := carriedDesignLine(none), "design carried from TPF-12: ARCHITECTURE; passed by architect; standing"; got != want {
		t.Errorf("nulls: %q, want %q", got, want)
	}
	if carriedDesignLine(nil) != "" {
		t.Error("no record prints a line")
	}
}

// compact names the carried design under the status line.
func TestCompactTaskNamesTheCarriedDesign(t *testing.T) {
	task := map[string]any{"key": "TPF-13", "status": "QUEUED", "role": "coder", "carriedDesign": carriedRecord(true)}
	if got, want := compactTask(task, nil), "TPF-13 queued, role coder\ncarried from TPF-12 (2 rounds, standing)\nqueued for coder"; got != want {
		t.Errorf("printed\n%s\nwant\n%s", got, want)
	}
	task["carriedDesign"] = carriedRecord(false)
	if got := compactTask(task, nil); !strings.Contains(got, "carried from TPF-12 (2 rounds, stale)") {
		t.Errorf("a stale record printed\n%s", got)
	}
	delete(task, "carriedDesign")
	if got := compactTask(task, nil); strings.Contains(got, "carried") {
		t.Errorf("a task without the record printed\n%s", got)
	}
}
