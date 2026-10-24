package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	rearm "github.com/relizaio/rearm-client-go"
)

// The served prompt by version in the CLI (task TPF-110, child 2 of TPF-97): --prompt-version and --full-prompt on
// task next, task assign, task brief, board coordinate and wait; heldVersions sent only from the flag; the text
// printed where it always was on SERVED and a short form on UNCHANGED; the older-server fallback; nothing recorded.

const (
	pvV1    = "1111111111111111111111111111111111111111111111111111111111111111"
	pvV2    = "2222222222222222222222222222222222222222222222222222222222222222"
	pvW1    = "3333333333333333333333333333333333333333333333333333333333333333"
	pvText  = "SERVED coder prompt, the full text"
	pvBoard = "bbbbbbbb-2222-4222-8222-000000000001"

	pvFieldUndefined = "Validation error (FieldUndefined@[agentTaskNextProgrammatic/promptByVersion]) : Field 'promptByVersion' in type 'AgentTaskAssignment' is undefined"
	pvBoundRefusal   = "heldVersions: at most 50 entries of at most 128 characters"
)

// byVersionRows turns role rows as the legacy role read answers them into the by-version read's rows: the
// served prompt moves into promptByVersion, SERVED, with the rig's version.
func byVersionRows(rows []any) []any {
	var out []any
	for _, r := range rows {
		row := map[string]any{}
		for k, v := range r.(map[string]any) {
			row[k] = v
		}
		text := row["servedPrompt"]
		delete(row, "servedPrompt")
		row["promptByVersion"] = map[string]any{"version": briefServedVersion, "status": "SERVED", "text": text}
		out = append(out, row)
	}
	return out
}

// servedPrompt is the nested {version, status, text} a by-version field answers.
func servedPrompt(version, status string, text *string) map[string]any {
	var t any
	if text != nil {
		t = *text
	}
	return map[string]any{"version": version, "status": status, "text": t}
}

func textOf(s string) *string { return &s }

type pvRequest struct {
	op   string
	vars map[string]any
}

// pvRig is a server that answers by operation name and records every request in order.
type pvRig struct {
	mu     sync.Mutex
	reqs   []pvRequest
	answer func(op string, vars map[string]any) any // a data object, or an error message string
	srv    *httptest.Server
}

func (g *pvRig) requests() []pvRequest {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]pvRequest(nil), g.reqs...)
}

func newPvRig(t *testing.T, answer func(op string, vars map[string]any) any) *pvRig {
	t.Helper()
	g := &pvRig{answer: answer}
	g.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		op := opNameOf(req.Query)
		g.mu.Lock()
		g.reqs = append(g.reqs, pvRequest{op, req.Variables})
		g.mu.Unlock()
		switch a := g.answer(op, req.Variables).(type) {
		case string:
			_ = json.NewEncoder(w).Encode(map[string]any{"errors": []any{map[string]any{"message": a,
				"extensions": map[string]any{"classification": "BAD_REQUEST"}}}})
		case nil:
			_, _ = w.Write(waitBoardAnswers(op, nil))
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"data": a})
		}
	}))
	t.Cleanup(g.srv.Close)
	return g
}

// pvWorld points the CLI at the rig with a state file for session s-1 (client id c-1), and resets every global
// the verbs read.
func pvWorld(t *testing.T, answer func(op string, vars map[string]any) any) *pvRig {
	t.Helper()
	withStateDir(t)
	if err := writeAgentState(&agentSessionState{SessionUuid: "s-1", ClientSessionId: "c-1"}); err != nil {
		t.Fatal(err)
	}
	g := newPvRig(t, answer)
	c, err := rearm.New(g.srv.URL, "id", "secret", rearm.WithoutTokenExchange())
	if err != nil {
		t.Fatal(err)
	}
	reset := func() {
		apiClient, taskSessionUuid, taskBoardUuid, taskRoles = nil, "", "", nil
		nextPromptFlags, assignPromptFlags, coordinatePromptFlags, waitPromptFlags = promptFlags{}, promptFlags{}, promptFlags{}, promptFlags{}
		promptByVersionUnsupported, assignSentHeldVersions, compactJson, debug = false, false, false, ""
	}
	reset()
	t.Cleanup(reset)
	apiClient, taskSessionUuid = c, "s-1"
	return g
}

func pvStateBytes(t *testing.T) []byte {
	t.Helper()
	path, err := agentStatePath("c-1")
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func pvOffer(pbv map[string]any) map[string]any {
	o := map[string]any{"role": "coder", "promptVersion": "abc123",
		"task": map[string]any{"key": "RD-1", "uuid": "t-1", "status": "ASSIGNED", "role": "coder",
			"assignment": map[string]any{"session": "s-1", "role": "coder", "assignedAt": "2026-10-24T01:00:00Z"}}}
	if pbv != nil {
		o["promptByVersion"] = pbv
	}
	return o
}

// nextAnswer answers task next by version with pbv, everything else with the wait board's answers.
func nextAnswer(pbv map[string]any) func(op string, vars map[string]any) any {
	return func(op string, vars map[string]any) any {
		if op == "AgentTaskNextByPromptVersion" {
			return map[string]any{"agentTaskNextProgrammatic": pvOffer(pbv)}
		}
		return nil
	}
}

func runNext(t *testing.T) (map[string]any, string) {
	t.Helper()
	out, errOut := both(t, func() { agentTaskNextCmd.Run(agentTaskNextCmd, nil) })
	var obj map[string]any
	if err := json.Unmarshal([]byte(out), &obj); err != nil || strings.Count(strings.TrimSpace(out), "\n") != 0 {
		t.Fatalf("task next prints one JSON line, got %q (%v)", out, err)
	}
	return obj, errOut
}

func heldOf(t *testing.T, r pvRequest) (any, bool) {
	t.Helper()
	v, ok := r.vars["heldVersions"]
	return v, ok
}

func TestNextWithoutTheFlagPrintsTheTextAndSendsNoHeldVersions(t *testing.T) {
	g := pvWorld(t, nextAnswer(servedPrompt(pvV1, "SERVED", textOf(pvText))))
	before := pvStateBytes(t)
	obj, errOut := runNext(t)
	reqs := g.requests()
	if len(reqs) != 1 || reqs[0].op != "AgentTaskNextByPromptVersion" {
		t.Fatalf("one by-version request, got %v", reqs)
	}
	if _, sent := heldOf(t, reqs[0]); sent {
		t.Fatalf("no heldVersions key without the flag, got %v", reqs[0].vars)
	}
	if obj["rolePrompt"] != pvText || obj["promptVersion"] != "abc123" {
		t.Fatalf("the text where it always was and the pin as sent, got %v", obj)
	}
	if !reflect.DeepEqual(obj["promptByVersion"], map[string]any{"version": pvV1, "status": "SERVED"}) {
		t.Fatalf("promptByVersion is exactly {version, status}, got %v", obj["promptByVersion"])
	}
	if errOut != "" {
		t.Fatalf("nothing on stderr, got %q", errOut)
	}
	if !bytes.Equal(before, pvStateBytes(t)) {
		t.Fatal("the state file changed on account of the prompt")
	}
}

func TestNextWithTheHeldVersionPrintsTheShortFormAndTheHint(t *testing.T) {
	g := pvWorld(t, nextAnswer(servedPrompt(pvV1, "UNCHANGED", nil)))
	nextPromptFlags = promptFlags{versions: []string{pvV1}}
	before := pvStateBytes(t)
	obj, errOut := runNext(t)
	if held, _ := heldOf(t, g.requests()[0]); !reflect.DeepEqual(held, []any{pvV1}) {
		t.Fatalf("heldVersions [v1], got %v", held)
	}
	if _, has := obj["rolePrompt"]; has {
		t.Fatalf("no rolePrompt on UNCHANGED, got %v", obj)
	}
	if !reflect.DeepEqual(obj["promptByVersion"], map[string]any{"version": pvV1, "status": "UNCHANGED"}) {
		t.Fatalf("promptByVersion {v1, UNCHANGED}, got %v", obj["promptByVersion"])
	}
	want := "rearm: prompt unchanged (served version " + pvV1 + "): keep the text you hold; a context that does not hold it " +
		"re-runs without --prompt-version or with --full-prompt\n"
	if errOut != want {
		t.Fatalf("the hint, exactly:\nwant %q\ngot  %q", want, errOut)
	}
	if !bytes.Equal(before, pvStateBytes(t)) {
		t.Fatal("the state file changed on account of the prompt")
	}
}

func TestNextWithAStaleVersionPrintsTheNewText(t *testing.T) {
	pvWorld(t, nextAnswer(servedPrompt(pvV2, "SERVED", textOf(pvText))))
	nextPromptFlags = promptFlags{versions: []string{pvV1}}
	obj, errOut := runNext(t)
	if obj["rolePrompt"] != pvText || !reflect.DeepEqual(obj["promptByVersion"], map[string]any{"version": pvV2, "status": "SERVED"}) {
		t.Fatalf("the new text and {v2, SERVED}, got %v", obj)
	}
	if errOut != "" {
		t.Fatalf("nothing on stderr, got %q", errOut)
	}
}

func TestFullPromptSendsNoHeldVersions(t *testing.T) {
	for _, f := range []promptFlags{{full: true, versions: []string{pvV1}}, {full: true}} {
		g := pvWorld(t, nextAnswer(servedPrompt(pvV1, "SERVED", textOf(pvText))))
		nextPromptFlags = f
		obj, _ := runNext(t)
		if _, sent := heldOf(t, g.requests()[0]); sent {
			t.Fatalf("--full-prompt sends no heldVersions, got %v", g.requests()[0].vars)
		}
		if obj["rolePrompt"] != pvText {
			t.Fatalf("--full-prompt prints the text, got %v", obj)
		}
	}
}

func TestSeveralVersionsAreSentDeduplicatedInOrder(t *testing.T) {
	g := pvWorld(t, nextAnswer(servedPrompt(pvV1, "SERVED", textOf(pvText))))
	nextPromptFlags = promptFlags{versions: []string{"x", "y", "x"}}
	runNext(t)
	if held, _ := heldOf(t, g.requests()[0]); !reflect.DeepEqual(held, []any{"x", "y"}) {
		t.Fatalf("exactly [x y], got %v", held)
	}
}

func TestTheFlagBoundIsCheckedLocally(t *testing.T) {
	many := make([]string, 51)
	for i := range many {
		many[i] = pvV1
	}
	for name, vs := range map[string][]string{"51 values": many, "an empty value": {pvV1, ""}, "129 characters": {strings.Repeat("a", 129)}} {
		if err := checkPromptFlags(promptFlags{versions: vs}); err == nil ||
			err.Error() != "rearm: --prompt-version: at most 50 values of at most 128 characters, none empty" {
			t.Errorf("%s: refused with the bound message, got %v", name, err)
		}
	}
	fifty := make([]string, 50)
	for i := range fifty {
		fifty[i] = fmt.Sprintf("%03d", i) + strings.Repeat("a", 125)
	}
	g := pvWorld(t, nextAnswer(servedPrompt(pvV1, "SERVED", textOf(pvText))))
	nextPromptFlags = promptFlags{versions: fifty}
	if err := checkPromptFlags(nextPromptFlags); err != nil {
		t.Fatalf("50 values of 128 characters pass, got %v", err)
	}
	runNext(t)
	if held, _ := heldOf(t, g.requests()[0]); len(held.([]any)) != 50 {
		t.Fatalf("all 50 are sent, got %d", len(held.([]any)))
	}
}

func TestATwelveCharacterVersionIsSentWithAHint(t *testing.T) {
	g := pvWorld(t, nextAnswer(servedPrompt(pvV1, "SERVED", textOf(pvText))))
	nextPromptFlags = promptFlags{versions: []string{"abc123abc123"}}
	var errOut string
	_, errOut = both(t, func() { agentTaskNextCmd.Run(agentTaskNextCmd, nil) })
	if held, _ := heldOf(t, g.requests()[0]); !reflect.DeepEqual(held, []any{"abc123abc123"}) {
		t.Fatalf("sent as given, got %v", held)
	}
	if strings.Count(errOut, "looks like the prompt pin (12 characters)") != 1 {
		t.Fatalf("the pin hint once, got %q", errOut)
	}
}

// TestPromptVersionChild runs one verb in a child process, so its exit status can be read.
func TestPromptVersionChild(t *testing.T) {
	verb := os.Getenv("REARM_TEST_PV_VERB")
	if verb == "" {
		t.Skip("run by the exit tests")
	}
	c, err := rearm.New(os.Getenv("REARM_TEST_PV_URL"), "id", "secret", rearm.WithoutTokenExchange())
	if err != nil {
		t.Fatal(err)
	}
	apiClient, taskSessionUuid = c, "s-1"
	var vs []string
	_ = json.Unmarshal([]byte(os.Getenv("REARM_TEST_PV_VALUES")), &vs)
	switch verb {
	case "next":
		nextPromptFlags = promptFlags{versions: vs}
		agentTaskNextCmd.Run(agentTaskNextCmd, nil)
	case "assign":
		assignPromptFlags = promptFlags{versions: vs}
		agentTaskAssignCmd.Run(agentTaskAssignCmd, []string{"t-1"})
	}
}

func runPvChild(t *testing.T, g *pvRig, verb string, values []string) (int, string) {
	t.Helper()
	vs, _ := json.Marshal(values)
	child := testChild(t, "TestPromptVersionChild", "REARM_TEST_PV_VERB="+verb, "REARM_TEST_PV_URL="+g.srv.URL,
		"REARM_TEST_PV_VALUES="+string(vs), "XDG_STATE_HOME="+os.Getenv("XDG_STATE_HOME"))
	var errb bytes.Buffer
	child.Stderr = &errb
	err := child.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return code, errb.String()
}

func TestAVerbRefusesTheBoundBeforeAnyRequest(t *testing.T) {
	withStateDir(t)
	g := newPvRig(t, func(string, map[string]any) any { return nil })
	many := make([]string, 51)
	for i := range many {
		many[i] = pvV1
	}
	for _, verb := range []string{"next", "assign"} {
		for _, vs := range [][]string{many, {""}, {strings.Repeat("a", 129)}} {
			code, errOut := runPvChild(t, g, verb, vs)
			if code != 1 || !strings.Contains(errOut, "rearm: --prompt-version: at most 50 values of at most 128 characters, none empty") {
				t.Errorf("%s: exit 1 with the bound message, got %d %q", verb, code, errOut)
			}
		}
	}
	if n := len(g.requests()); n != 0 {
		t.Fatalf("nothing is sent, got %d requests", n)
	}
}

func TestAnUnchangedTheCLIDidNotAskForIsNeverMasked(t *testing.T) {
	pvWorld(t, nextAnswer(servedPrompt(pvV1, "UNCHANGED", nil)))
	obj, errOut := runNext(t)
	if _, has := obj["rolePrompt"]; has || !reflect.DeepEqual(obj["promptByVersion"], map[string]any{"version": pvV1, "status": "UNCHANGED"}) {
		t.Fatalf("the answer as received, got %v", obj)
	}
	want := "rearm: the server reports the prompt unchanged against a version this command did not send; --full-prompt prints it\n"
	if errOut != want {
		t.Fatalf("the fail-safe line, not the hint:\nwant %q\ngot  %q", want, errOut)
	}
}

func assignAnswer(pbv map[string]any) func(op string, vars map[string]any) any {
	return func(op string, vars map[string]any) any {
		switch op {
		case "AgentTaskAssignByPromptVersion", "AgentTaskAssignProgrammatic":
			return map[string]any{"agentTaskAssignProgrammatic": pvOffer(pbv)}
		}
		return nil
	}
}

func runAssign(t *testing.T) (string, string) {
	t.Helper()
	return both(t, func() { agentTaskAssignCmd.Run(agentTaskAssignCmd, []string{"t-1"}) })
}

func currentTaskOf(t *testing.T) string {
	t.Helper()
	st, err := readAgentState("c-1")
	if err != nil || st == nil {
		t.Fatal(err)
	}
	return st.CurrentTask
}

func TestAssignPrintsTheUnchangedOrChangedSuffix(t *testing.T) {
	held := "held by session s-1; read it with: rearm agent task brief RD-1 --session s-1"

	g := pvWorld(t, assignAnswer(servedPrompt(pvV1, "UNCHANGED", nil)))
	assignPromptFlags = promptFlags{versions: []string{pvV1}}
	out, errOut := runAssign(t)
	if want := "RD-1 assigned, role coder, prompt abc123, prompt unchanged (served version " + pvV1 + ")\n" + held + "\n"; out != want || errOut != "" {
		t.Fatalf("compact UNCHANGED:\nwant %q\ngot  %q (stderr %q)", want, out, errOut)
	}
	if h, _ := heldOf(t, g.requests()[0]); !reflect.DeepEqual(h, []any{pvV1}) || currentTaskOf(t) != "t-1" {
		t.Fatalf("heldVersions [v1] and the current task set, got %v %q", h, currentTaskOf(t))
	}

	pvWorld(t, assignAnswer(servedPrompt(pvV2, "SERVED", textOf(pvText))))
	assignPromptFlags = promptFlags{versions: []string{pvV1}}
	out, _ = runAssign(t)
	if want := "RD-1 assigned, role coder, prompt abc123, served prompt changed: re-read it with task brief\n" + held + "\n"; out != want {
		t.Fatalf("compact SERVED after a held version:\nwant %q\ngot  %q", want, out)
	}
	if strings.Contains(out, pvText) || currentTaskOf(t) != "t-1" {
		t.Fatalf("no text printed and the current task set, got %q", out)
	}

	g = pvWorld(t, assignAnswer(servedPrompt(pvV1, "SERVED", textOf(pvText))))
	out, errOut = runAssign(t)
	if want := "RD-1 assigned, role coder, prompt abc123\n" + held + "\n"; out != want || errOut != "" {
		t.Fatalf("compact without the flag, today's lines:\nwant %q\ngot  %q (stderr %q)", want, out, errOut)
	}
	if _, sent := heldOf(t, g.requests()[0]); sent || currentTaskOf(t) != "t-1" {
		t.Fatalf("no heldVersions without the flag and the current task set, got %v", g.requests()[0].vars)
	}
}

func TestAssignJsonIsShapedAsNext(t *testing.T) {
	for _, tc := range []struct {
		flags  promptFlags
		pbv    map[string]any
		text   bool
		status string
	}{
		{promptFlags{}, servedPrompt(pvV1, "SERVED", textOf(pvText)), true, "SERVED"},
		{promptFlags{versions: []string{pvV1}}, servedPrompt(pvV1, "UNCHANGED", nil), false, "UNCHANGED"},
	} {
		pvWorld(t, assignAnswer(tc.pbv))
		assignPromptFlags, compactJson = tc.flags, true
		out, _ := runAssign(t)
		var obj map[string]any
		if err := json.Unmarshal([]byte(out), &obj); err != nil {
			t.Fatalf("--json prints JSON, got %q", out)
		}
		if _, has := obj["rolePrompt"]; has != tc.text {
			t.Fatalf("%s: rolePrompt present %v, got %v", tc.status, tc.text, obj)
		}
		if !reflect.DeepEqual(obj["promptByVersion"], map[string]any{"version": pvV1, "status": tc.status}) {
			t.Fatalf("promptByVersion {v1, %s}, got %v", tc.status, obj["promptByVersion"])
		}
		if currentTaskOf(t) != "t-1" {
			t.Fatal("the current task is set as today")
		}
	}
}

func TestTheBriefSendsTheFlagAndPrintsTheShortForm(t *testing.T) {
	b := &briefBoard{}
	briefWorld(t, b, false)
	out := runBrief(t)
	var brief []briefRequest
	for _, r := range b.requests {
		if strings.HasPrefix(r.op, "AgentRoleBrief") {
			brief = append(brief, r)
		}
	}
	if len(brief) != 1 || brief[0].op != "AgentRoleBriefByPromptVersion" {
		t.Fatalf("one by-version role read, got %v", brief)
	}
	if _, sent := brief[0].vars["heldVersions"]; sent {
		t.Fatalf("no heldVersions without the flag, got %v", brief[0].vars)
	}
	if !strings.Contains(out, "## 1. Your prompt (coder, version abc123, served version "+briefServedVersion+")\n\nSERVED coder prompt") {
		t.Fatalf("the heading names the served version and the text follows, got:\n%s", out)
	}

	b = &briefBoard{byVersionStatus: "UNCHANGED"}
	briefWorld(t, b, false)
	briefPromptFlags = promptFlags{versions: []string{briefServedVersion}}
	out = runBrief(t)
	for _, r := range b.requests {
		if r.op == "AgentRoleBriefByPromptVersion" && !reflect.DeepEqual(r.vars["heldVersions"], []any{briefServedVersion}) {
			t.Fatalf("heldVersions [v], got %v", r.vars)
		}
	}
	if !strings.HasPrefix(out, "Brief for RD-1 as coder: prompt unchanged, 3 documents") {
		t.Fatalf("the header says prompt unchanged, got:\n%s", out)
	}
	want := "## 1. Your prompt (coder, version abc123, served version " + briefServedVersion + ")\n\nprompt unchanged (served version " +
		briefServedVersion + "): keep the prompt you hold; --full-prompt prints it\n\nOrientation:"
	if !strings.Contains(out, want) || strings.Contains(out, "SERVED coder prompt") {
		t.Fatalf("section 1 is the heading and the one line, no text; got:\n%s", out)
	}

	b = &briefBoard{byVersionStatus: "UNCHANGED"}
	briefWorld(t, b, false)
	briefPromptFlags, briefJson = promptFlags{versions: []string{briefServedVersion}}, true
	var obj map[string]any
	if err := json.Unmarshal([]byte(runBrief(t)), &obj); err != nil {
		t.Fatal(err)
	}
	if obj["servedPromptVersion"] != briefServedVersion || obj["promptStatus"] != "UNCHANGED" || obj["servedPrompt"] != "" {
		t.Fatalf("--json carries servedPromptVersion and promptStatus, no text, got %v %v %q",
			obj["servedPromptVersion"], obj["promptStatus"], obj["servedPrompt"])
	}
}

func coordinateAnswer(pbv map[string]any) func(op string, vars map[string]any) any {
	return func(op string, vars map[string]any) any {
		switch op {
		case "AgentBoardCoordinateByPromptVersion":
			return map[string]any{"agentBoardCoordinateProgrammatic": map[string]any{"uuid": pvBoard, "name": "b",
				"coordinatorPromptByVersion": pbv, "missingCapabilities": []any{}}}
		case "AgentBoardCoordinateProgrammatic":
			return map[string]any{"agentBoardCoordinateProgrammatic": map[string]any{"uuid": pvBoard, "name": "b",
				"coordinatorPrompt": "STORED", "servedCoordinatorPrompt": pvText}}
		}
		return nil
	}
}

func runCoordinate(t *testing.T) (map[string]any, string) {
	t.Helper()
	out, errOut := both(t, func() { agentBoardCoordinateCmd.Run(agentBoardCoordinateCmd, []string{pvBoard}) })
	var obj map[string]any
	if err := json.Unmarshal([]byte(out), &obj); err != nil {
		t.Fatalf("coordinate prints JSON, got %q", out)
	}
	return obj, errOut
}

func boardOfState(t *testing.T) string {
	t.Helper()
	st, err := readAgentState("c-1")
	if err != nil || st == nil {
		t.Fatal(err)
	}
	return st.Board
}

func TestCoordinatePrintsTheServedTextOrTheShortForm(t *testing.T) {
	g := pvWorld(t, coordinateAnswer(servedPrompt(pvW1, "SERVED", textOf(pvText))))
	obj, errOut := runCoordinate(t)
	if _, sent := heldOf(t, g.requests()[0]); sent || g.requests()[0].op != "AgentBoardCoordinateByPromptVersion" {
		t.Fatalf("the by-version claim without heldVersions, got %v", g.requests())
	}
	if obj["servedCoordinatorPrompt"] != pvText || !reflect.DeepEqual(obj["coordinatorPromptByVersion"], map[string]any{"version": pvW1, "status": "SERVED"}) {
		t.Fatalf("the served text and {w1, SERVED}, got %v", obj)
	}
	if _, has := obj["coordinatorPrompt"]; has || errOut != "" {
		t.Fatalf("no coordinatorPrompt key and nothing on stderr, got %v %q", obj, errOut)
	}
	if boardOfState(t) != pvBoard {
		t.Fatal("rememberBoard still runs")
	}

	g = pvWorld(t, coordinateAnswer(servedPrompt(pvW1, "UNCHANGED", nil)))
	coordinatePromptFlags = promptFlags{versions: []string{pvW1}}
	obj, errOut = runCoordinate(t)
	if h, _ := heldOf(t, g.requests()[0]); !reflect.DeepEqual(h, []any{pvW1}) {
		t.Fatalf("heldVersions [w1], got %v", h)
	}
	if _, has := obj["servedCoordinatorPrompt"]; has {
		t.Fatalf("no servedCoordinatorPrompt on UNCHANGED, got %v", obj)
	}
	if !strings.Contains(errOut, "rearm: prompt unchanged (served version "+pvW1+")") {
		t.Fatalf("the hint on stderr, got %q", errOut)
	}
	if boardOfState(t) != pvBoard {
		t.Fatal("rememberBoard still runs")
	}
}

// olderServer answers every by-version operation with the FieldUndefined error of a server from before TPF-109.
func olderServer(legacy func(op string, vars map[string]any) any) func(op string, vars map[string]any) any {
	return func(op string, vars map[string]any) any {
		if strings.HasSuffix(op, "ByPromptVersion") {
			return pvFieldUndefined
		}
		return legacy(op, vars)
	}
}

func TestAnOlderServerGetsTheLegacyOperationRemembered(t *testing.T) {
	g := pvWorld(t, olderServer(func(op string, vars map[string]any) any {
		if op == "AgentTaskNextProgrammatic" {
			o := pvOffer(nil)
			o["rolePrompt"] = pvText
			return map[string]any{"agentTaskNextProgrammatic": o}
		}
		return nil
	}))
	nextPromptFlags, debug = promptFlags{versions: []string{pvV1}}, "true"
	obj, errOut := runNext(t)
	reqs := g.requests()
	if len(reqs) != 2 || reqs[0].op != "AgentTaskNextByPromptVersion" || reqs[1].op != "AgentTaskNextProgrammatic" {
		t.Fatalf("by-version, then legacy, got %v", reqs)
	}
	if _, sent := heldOf(t, reqs[1]); sent {
		t.Fatalf("the legacy request carries no heldVersions, got %v", reqs[1].vars)
	}
	if obj["rolePrompt"] != pvText {
		t.Fatalf("today's offer with its text, got %v", obj)
	}
	if strings.Count(errOut, "server predates promptByVersion, using the legacy selection") != 1 {
		t.Fatalf("the debug line once, got %q", errOut)
	}
	_, errOut = runNext(t)
	if reqs = g.requests(); len(reqs) != 3 || reqs[2].op != "AgentTaskNextProgrammatic" || errOut != "" {
		t.Fatalf("a second call goes straight to the legacy operation, got %v (stderr %q)", reqs, errOut)
	}
}

func TestAnOlderServerForCoordinateAndAssign(t *testing.T) {
	g := pvWorld(t, olderServer(coordinateAnswer(nil)))
	obj, _ := runCoordinate(t)
	if reqs := g.requests(); len(reqs) != 2 || reqs[1].op != "AgentBoardCoordinateProgrammatic" {
		t.Fatalf("coordinate: by-version then legacy, got %v", reqs)
	}
	if obj["coordinatorPrompt"] != "STORED" || obj["servedCoordinatorPrompt"] != pvText {
		t.Fatalf("coordinate: the legacy board as before, got %v", obj)
	}

	g = pvWorld(t, olderServer(assignAnswer(nil)))
	assignPromptFlags = promptFlags{versions: []string{pvV1}}
	out, _ := runAssign(t)
	if reqs := g.requests(); len(reqs) != 2 || reqs[1].op != "AgentTaskAssignProgrammatic" {
		t.Fatalf("assign: by-version then legacy, got %v", reqs)
	}
	if !strings.HasPrefix(out, "RD-1 assigned, role coder, prompt abc123\nheld by session s-1") {
		t.Fatalf("assign: today's compact lines, got %q", out)
	}
}

func TestTheBoundRefusalIsNotRetriedAsAnOlderServer(t *testing.T) {
	withStateDir(t)
	g := newPvRig(t, func(op string, _ map[string]any) any {
		if strings.HasSuffix(op, "ByPromptVersion") {
			return pvBoundRefusal
		}
		return nil
	})
	code, errOut := runPvChild(t, g, "next", []string{pvV1})
	if code != 1 || !strings.Contains(errOut, "Error: ") || !strings.Contains(errOut, pvBoundRefusal) {
		t.Fatalf("exit 1 with the refusal, got %d %q", code, errOut)
	}
	if reqs := g.requests(); len(reqs) != 1 || reqs[0].op != "AgentTaskNextByPromptVersion" {
		t.Fatalf("one request, never the legacy retry, got %v", reqs)
	}
}

func TestWaitSendsTheFlagAndShapesTheOffer(t *testing.T) {
	for _, tc := range []struct {
		pbv   map[string]any
		watch bool
		text  bool
		hint  bool
	}{
		{servedPrompt(pvV1, "UNCHANGED", nil), false, false, true},
		{servedPrompt(pvV2, "SERVED", textOf(pvText)), false, true, false},
		{servedPrompt(pvV1, "UNCHANGED", nil), true, false, true},
	} {
		g := pvWorld(t, nextAnswer(tc.pbv))
		o := waitOpts{session: "s-1", interval: 30 * time.Second, timeout: 90 * time.Second,
			statePath: filepath.Join(t.TempDir(), "wait-state.json")}
		if tc.watch {
			o.watch, o.board = true, "b-1"
		}
		var out bytes.Buffer
		var code int
		errOut := captureStderr(t, func() {
			code = runWait(o, cliWaitClient{prompt: promptFlags{versions: []string{pvV1}}}, &fakeClock{t: time.Now()}, &out)
		})
		if code != waitExitWork {
			t.Fatalf("an offer is work, got %d (%s)", code, out.String())
		}
		var obj map[string]any
		if err := json.Unmarshal(out.Bytes(), &obj); err != nil {
			t.Fatal(err)
		}
		offer := obj
		if tc.watch {
			offer, _ = obj["offer"].(map[string]any)
		}
		if _, has := offer["rolePrompt"]; has != tc.text {
			t.Fatalf("rolePrompt present %v, got %v", tc.text, obj)
		}
		if strings.Contains(errOut, "rearm: prompt unchanged (served version "+pvV1+")") != tc.hint {
			t.Fatalf("the hint %v, got %q", tc.hint, errOut)
		}
		for _, r := range g.requests() {
			if r.op == "AgentTaskNextByPromptVersion" && !reflect.DeepEqual(r.vars["heldVersions"], []any{pvV1}) {
				t.Fatalf("the poll carries [v1], got %v", r.vars)
			}
		}
	}
}

func TestTheFiveVerbsListThePromptFlags(t *testing.T) {
	for _, path := range [][]string{{"agent", "task", "next"}, {"agent", "task", "assign"}, {"agent", "task", "brief"},
		{"agent", "board", "coordinate"}, {"agent", "wait"}} {
		c, _, err := rootCmd.Find(path)
		if err != nil || c == nil || c.Name() != path[len(path)-1] {
			t.Fatalf("%v: not found (%v)", path, err)
		}
		for _, f := range []string{"prompt-version", "full-prompt"} {
			if c.Flags().Lookup(f) == nil {
				t.Errorf("%v lacks --%s", path, f)
			}
		}
		if !strings.Contains(c.Long, "--prompt-version") {
			t.Errorf("%v: the help text does not explain --prompt-version", path)
		}
	}
}
