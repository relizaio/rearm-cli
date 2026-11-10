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

// The coordinator's verdict on an answer signed off --no-change and the lines task show prints for it (task
// TPF-173, TPF-171 design 6.2 and 7.2).

const confirmTaskUuid = "1ca09c3b-e6fe-4429-bf2c-86c5c7361e79"

// verdictServer records every operation it is sent with its variables, and answers each verdict with a task.
type verdictServer struct {
	mu   sync.Mutex
	ops  []string
	vars []map[string]any
}

func (v *verdictServer) serve(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			OperationName string         `json:"operationName"`
			Variables     map[string]any `json:"variables"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		v.mu.Lock()
		v.ops = append(v.ops, req.OperationName)
		v.vars = append(v.vars, req.Variables)
		v.mu.Unlock()
		task := map[string]any{"uuid": confirmTaskUuid, "key": "TPF-9", "status": "QUEUED", "role": "coder"}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"agentTaskConfirmAnswerProgrammatic": task, "agentTaskConfirmAnswer": task}})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func resetVerdictFlags(t *testing.T) {
	t.Cleanup(func() { answerVerdict, answerNote, taskSessionUuid = "", "", "" })
}

// T-23: the seat's verdict, lower case on the command line, goes out upper case with the task, the seat and the note.
func TestConfirmAnswerSendsTheSeatsVerdict(t *testing.T) {
	withStateDir(t)
	v := &verdictServer{}
	useFake(t, v.serve(t))
	resetVerdictFlags(t)
	taskSessionUuid, answerVerdict, answerNote = "S", "confirmed", "n"
	_ = captureStdout(t, func() { agentTaskConfirmAnswerCmd.Run(agentTaskConfirmAnswerCmd, []string{"K"}) })
	if len(v.ops) != 1 || v.ops[0] != "AgentTaskConfirmAnswerProgrammatic" {
		t.Fatalf("sent %v, want one AgentTaskConfirmAnswerProgrammatic", v.ops)
	}
	got := v.vars[0]
	if got["taskUuid"] != "K" || got["sessionUuid"] != "S" || got["verdict"] != "CONFIRMED" || got["note"] != "n" {
		t.Errorf("variables %v", got)
	}
}

// T-24: REFUSED without a note sends no note.
func TestConfirmAnswerRefusedWithoutANote(t *testing.T) {
	withStateDir(t)
	v := &verdictServer{}
	useFake(t, v.serve(t))
	resetVerdictFlags(t)
	taskSessionUuid, answerVerdict = "S", "refused"
	_ = captureStdout(t, func() { agentTaskConfirmAnswerCmd.Run(agentTaskConfirmAnswerCmd, []string{"K"}) })
	if len(v.vars) != 1 || v.vars[0]["verdict"] != "REFUSED" {
		t.Fatalf("sent %v %v", v.ops, v.vars)
	}
	if n, ok := v.vars[0]["note"]; ok && n != nil {
		t.Errorf("a verdict without --note sent note %v", n)
	}
}

// T-25: an unknown or missing verdict is a usage error naming both values, refused before any request, on both verbs.
func TestConfirmAnswerRefusesABadVerdictBeforeAnyRequest(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	withStateDir(t)
	v := &verdictServer{}
	useFake(t, v.serve(t))
	resetVerdictFlags(t)
	t.Cleanup(func() { rootCmd.SetArgs(nil) })
	for _, c := range []struct {
		name string
		argv []string
		want string
	}{
		{"seat, maybe", []string{"agent", "task", "confirm-answer", confirmTaskUuid, "--session", "S", "--verdict", "maybe"}, "CONFIRMED or REFUSED"},
		{"seat, missing", []string{"agent", "task", "confirm-answer", confirmTaskUuid, "--session", "S"}, `"verdict"`},
		{"person, maybe", []string{"boards", "confirm-answer", confirmTaskUuid, "--verdict", "maybe"}, "CONFIRMED or REFUSED"},
		{"person, missing", []string{"boards", "confirm-answer", confirmTaskUuid}, `"verdict"`},
	} {
		answerVerdict = ""
		rootCmd.SetArgs(c.argv)
		var err error
		_ = captureStderr(t, func() { _ = captureStdout(t, func() { err = rootCmd.Execute() }) })
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error %v, want one naming %s", c.name, err, c.want)
		}
		if c.want != `"verdict"` && !strings.Contains(err.Error(), "REFUSED") {
			t.Errorf("%s: the error does not name both values: %v", c.name, err)
		}
	}
	if len(v.ops) != 0 {
		t.Errorf("a refused verdict sent %v", v.ops)
	}
	for _, ok := range []string{"CONFIRMED", "confirmed", " Refused "} {
		if got, err := answerVerdictOf(ok); err != nil || got != strings.ToUpper(strings.TrimSpace(ok)) {
			t.Errorf("%q reads %q %v", ok, got, err)
		}
	}
}

// T-25: without --session and with no current session, the seat's verb refuses naming the flag, sending nothing.
func TestConfirmAnswerNeedsTheSeatSession(t *testing.T) {
	if url := os.Getenv("REARM_TEST_CONFIRM_ANSWER_NO_SESSION"); url != "" {
		c, err := rearm.New(url, "id", "secret", rearm.WithoutTokenExchange())
		if err != nil {
			os.Exit(3)
		}
		apiClient = c
		rootCmd.SetArgs([]string{"agent", "task", "confirm-answer", confirmTaskUuid, "--verdict", "CONFIRMED"})
		_ = rootCmd.Execute()
		os.Exit(0)
	}
	v := &verdictServer{}
	srv := v.serve(t)
	child := exec.Command(os.Args[0], "-test.run=^TestConfirmAnswerNeedsTheSeatSession$")
	child.Dir = t.TempDir()
	child.Env = append(os.Environ(), "REARM_TEST_CONFIRM_ANSWER_NO_SESSION="+srv.URL,
		"HOME="+t.TempDir(), "XDG_STATE_HOME="+t.TempDir(), "REARM_URL="+srv.URL)
	out, err := child.CombinedOutput()
	if err == nil {
		t.Fatalf("confirm-answer without --session exited 0: %s", out)
	}
	if !strings.Contains(string(out), "--session is required") {
		t.Errorf("the refusal does not name --session: %s", out)
	}
	if len(v.ops) != 0 {
		t.Errorf("sent %v without a session", v.ops)
	}
}

// T-26: the person's twin sends agentTaskConfirmAnswer with no session variable.
func TestBoardsConfirmAnswerSendsThePersonsVerdict(t *testing.T) {
	withStateDir(t)
	v := &verdictServer{}
	useFake(t, v.serve(t))
	resetVerdictFlags(t)
	answerVerdict = "CONFIRMED"
	_ = captureStdout(t, func() { boardsConfirmAnswerCmd.Run(boardsConfirmAnswerCmd, []string{"K"}) })
	if len(v.ops) != 1 || v.ops[0] != "AgentTaskConfirmAnswer" {
		t.Fatalf("sent %v, want one AgentTaskConfirmAnswer", v.ops)
	}
	if _, ok := v.vars[0]["sessionUuid"]; ok {
		t.Errorf("the person's verdict sent a session: %v", v.vars[0])
	}
	if v.vars[0]["taskUuid"] != "K" || v.vars[0]["verdict"] != "CONFIRMED" {
		t.Errorf("variables %v", v.vars[0])
	}
	if !strings.Contains(boardsConfirmAnswerCmd.Long, "personal key of the user whose key the answering agent runs on") {
		t.Error("the person's help does not say when CONFIRMED is refused")
	}
	if agentTaskConfirmAnswerCmd.PersistentFlags().Lookup("session") == nil || boardsConfirmAnswerCmd.Flags().Lookup("session") != nil {
		t.Error("the seat's verb takes --session, the person's does not")
	}
}

// confirmation is one answerConfirmations entry as a task read returns it.
func confirmation(state, answeredAt string, settledBy map[string]any, settledAt, note any, rounds ...map[string]any) map[string]any {
	rs := []any{}
	for _, r := range rounds {
		rs = append(rs, r)
	}
	e := map[string]any{"answeredAt": answeredAt, "role": "architect", "roleUuid": "ro-1", "session": "s-a",
		"agent": "a-1", "askingRole": "ro-4", "questionsRelease": "q-1", "rounds": rs, "state": state,
		"settledBy": nil, "settledAt": settledAt, "note": note}
	if settledBy != nil {
		e["settledBy"] = settledBy
	}
	return e
}

func answerRound(spec, release string, round any, previous string, previousRound any) map[string]any {
	return map[string]any{"release": release, "specification": spec, "round": round, "version": "26.11.1",
		"path": "design/x.md", "previous": previous, "previousRound": previousRound, "previousVersion": "26.11.0",
		"previousPath": "design/y.md"}
}

func confirmedTask(key, uuid string) map[string]any {
	return map[string]any{"key": key, "uuid": uuid, "status": "QUEUED", "role": "coder",
		"answerConfirmations": []any{confirmation("CONFIRMED", "2026-11-10T15:15:00Z",
			map[string]any{"kind": "SESSION", "uuid": "seat", "name": "seat"}, "2026-11-10T15:18:00Z", "wording only",
			answerRound("ARCHITECTURE", "r-3", float64(3), "r-2", float64(2)))}}
}

const (
	confirmedShowLine = "TPF-9: answer by architect at 2026-11-10T15:15:00Z: ARCHITECTURE round 3 vs round 2: confirmed by seat 2026-11-10T15:18:00Z: wording only"
	pendingShowLine   = "TPF-9: answer by architect at 2026-11-10T16:02:00Z: ARCHITECTURE round 4 vs round 3, THREAT_MODEL round 2 vs round 1: pending (rearm agent task confirm-answer TPF-9 --session <seat-session-uuid> --verdict CONFIRMED|REFUSED)"
)

// T-27: task show prints each confirmation on stderr, keyed by task, oldest first; stdout stays the task JSON whole.
func TestTaskShowPrintsTheAnswerConfirmations(t *testing.T) {
	// (a) one task, a CONFIRMED entry then a PENDING one with two specifications: the two lines of design 6.2.
	task := confirmedTask("TPF-9", "t-9")
	task["answerConfirmations"] = append(task["answerConfirmations"].([]any), confirmation("PENDING", "2026-11-10T16:02:00Z",
		nil, nil, nil, answerRound("ARCHITECTURE", "r-4", float64(4), "r-3", float64(3)),
		answerRound("THREAT_MODEL", "r-t2", float64(2), "r-t1", float64(1))))
	stdout, stderr := showTasks(t, task)
	if want := confirmedShowLine + "\n" + pendingShowLine + "\n"; stderr != want {
		t.Errorf("(a) stderr:\n%q\nwant:\n%q", stderr, want)
	}
	var read map[string]any
	if err := json.Unmarshal([]byte(stdout), &read); err != nil {
		t.Fatalf("(a) stdout is not the task JSON: %v\n%s", err, stdout)
	}
	if cs, _ := read["answerConfirmations"].([]any); len(cs) != 2 {
		t.Errorf("(a) stdout does not carry the confirmations whole: %v", read["answerConfirmations"])
	}
	if strings.Contains(stdout, "answer by") {
		t.Error("(a) a confirmation line reached stdout")
	}

	// (b) two tasks in one read: each line keyed by its own task.
	_, stderr = showTasks(t, confirmedTask("TPF-9", "t-9"), confirmedTask("TPF-10", "t-10"))
	want := confirmedShowLine + "\n" + strings.Replace(confirmedShowLine, "TPF-9: ", "TPF-10: ", 1) + "\n"
	if stderr != want {
		t.Errorf("(b) stderr:\n%q\nwant:\n%q", stderr, want)
	}

	// (c) an empty list, and a task without the field: no line.
	empty := map[string]any{"key": "TPF-11", "uuid": "t-11", "answerConfirmations": []any{}}
	absent := map[string]any{"key": "TPF-12", "uuid": "t-12"}
	if _, stderr = showTasks(t, empty, absent); strings.Contains(stderr, "answer by") {
		t.Errorf("(c) stderr %q", stderr)
	}

	// (d) a settler without a name reads by its kind; a null note leaves no trailing colon.
	nameless := map[string]any{"key": "TPF-9", "uuid": "t-9", "answerConfirmations": []any{confirmation("CONFIRMED",
		"2026-11-10T15:15:00Z", map[string]any{"kind": "SESSION", "uuid": "seat", "name": nil}, "2026-11-10T15:18:00Z", nil,
		answerRound("ARCHITECTURE", "r-3", float64(3), "r-2", float64(2)))}}
	if _, stderr = showTasks(t, nameless); !strings.HasSuffix(stderr, ": confirmed by session 2026-11-10T15:18:00Z\n") {
		t.Errorf("(d) stderr %q", stderr)
	}

	// (e) two rounds of one specification: one clause, from the last of them.
	twice := map[string]any{"key": "TPF-9", "uuid": "t-9", "answerConfirmations": []any{confirmation("REFUSED",
		"2026-11-10T15:15:00Z", map[string]any{"kind": "USER", "uuid": "u-1", "name": "Pat"}, "2026-11-10T15:18:00Z", "",
		answerRound("ARCHITECTURE", "r-3a", float64(3), "r-2", float64(2)),
		answerRound("ARCHITECTURE", "r-3b", float64(4), "r-2", float64(2)))}}
	want = "TPF-9: answer by architect at 2026-11-10T15:15:00Z: ARCHITECTURE round 4 vs round 2: refused by Pat 2026-11-10T15:18:00Z\n"
	if _, stderr = showTasks(t, twice); stderr != want {
		t.Errorf("(e) stderr:\n%q\nwant:\n%q", stderr, want)
	}
}

// Null round numbers read as the release, first 8 characters; a LAPSED entry says what lapsed it.
func TestAnswerConfirmationLineWithoutRoundNumbers(t *testing.T) {
	e := confirmation("LAPSED", "2026-11-10T15:15:00Z", map[string]any{"kind": "SYSTEM", "name": nil},
		"2026-11-10T15:20:00Z", "routed to coder",
		answerRound("ARCHITECTURE", "0123456789abcdef", nil, "fedcba9876543210", nil))
	want := "answer by architect at 2026-11-10T15:15:00Z: ARCHITECTURE release 01234567 vs release fedcba98: lapsed by system 2026-11-10T15:20:00Z: routed to coder"
	if got := answerConfirmationLine("TPF-9", e); got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

// T-28: the --no-change help says what the flag does on a question's answer.
func TestNoChangeHelpNamesTheCoordinatorStop(t *testing.T) {
	fl := agentTaskSignoffCmd.PersistentFlags().Lookup("no-change")
	if fl == nil {
		t.Fatal("signoff has no --no-change")
	}
	for _, want := range []string{"stops at the coordinator", "reviewed as a change", "unless another pass is stale"} {
		if !strings.Contains(fl.Usage, want) {
			t.Errorf("the --no-change help lacks %q: %q", want, fl.Usage)
		}
	}
}
