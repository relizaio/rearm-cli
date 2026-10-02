package cmd

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	rearm "github.com/relizaio/rearm-client-go"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// rearm agent session open / close --final / current (task RD5-4), against two scripted instances and real temporary
// repositories: open runs init, records the current session and files the ORIENTATION report, printing the three env
// lines; a failed upload leaves the session open; close --final files the FINAL report, closes and clears; current
// prints the entry or exits 1 naming --set; --set reads once, refuses a closed session and one these credentials
// cannot read, keeps the client id, and never rewrites a state this host already has; task assign records nothing.

const sAgent = "62df357e-a3a4-4df5-82d4-049e629d1c6b"

// sKey is the API key the world's credentials act as: the subject of every access token the scripted instances
// issue to them, and the key every session opened or added on them is recorded with unless a test says otherwise.
const sKey = "4b1d7a2e-5c3f-4e8a-9d61-0f2b3c4d5e6f"

// testToken is an unsigned JWT whose subject is the key, as the token endpoint issues one.
func testToken(key string) string {
	enc := base64.RawURLEncoding.EncodeToString
	return enc([]byte(`{"alg":"HS256","typ":"JWT"}`)) + "." + enc([]byte(`{"sub":"`+key+`","aud":"programmatic"}`)) + ".c2ln"
}

// sessOp is one request a scripted instance received.
type sessOp struct {
	name     string
	vars     map[string]any
	file     string
	filename string
}

// sessServer is one scripted instance: it opens, reads and closes sessions, takes reports, and answers task assign.
type sessServer struct {
	mu         sync.Mutex
	t          *testing.T
	prefix     string
	url        string
	ops        []sessOp
	sessions   map[string]map[string]any
	tokens     int
	n          int
	failInit   bool
	failUpload bool
	failClose  bool
}

func newSessServer(t *testing.T, prefix string) *sessServer {
	s := &sessServer{t: t, prefix: prefix, sessions: map[string]map[string]any{}}
	srv := httptest.NewServer(http.HandlerFunc(s.handle))
	t.Cleanup(srv.Close)
	s.url = srv.URL
	return s
}

// addSession puts a session on the instance, as one these credentials' key opened elsewhere.
func (s *sessServer) addSession(uuid, clientId, agent, status string) {
	s.addSessionOf(sKey, uuid, clientId, agent, status)
}

// addSessionOf puts a session the key opened on the instance; the read answers it to every caller, as the server
// answers it to an admin key, a board's seat and a reader of a board the session worked.
func (s *sessServer) addSessionOf(key, uuid, clientId, agent, status string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[uuid] = map[string]any{"uuid": uuid, "clientSessionId": clientId, "agent": agent, "apiKey": key, "status": status}
}

func (s *sessServer) names() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, o := range s.ops {
		out = append(out, o.name)
	}
	return out
}

func (s *sessServer) last(name string) sessOp {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.ops) - 1; i >= 0; i-- {
		if s.ops[i].name == name {
			return s.ops[i]
		}
	}
	s.t.Fatalf("no %s request; got %v", name, s.ops)
	return sessOp{}
}

func gqlFail(w http.ResponseWriter, msg string) {
	_ = json.NewEncoder(w).Encode(map[string]any{"errors": []any{map[string]any{"message": msg}}})
}

func (s *sessServer) handle(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == rearm.TokenPath {
		// The client-credentials exchange: the key id is the key's uuid here, and the token names it as its subject.
		key, _, _ := r.BasicAuth()
		s.mu.Lock()
		s.tokens++
		s.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": testToken(key), "token_type": "Bearer", "expires_in": 3600})
		return
	}
	var req struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables"`
	}
	op := sessOp{}
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			s.t.Errorf("multipart: %v", err)
		}
		_ = json.Unmarshal([]byte(r.FormValue("operations")), &req)
		for _, files := range r.MultipartForm.File {
			for _, fh := range files {
				f, _ := fh.Open()
				b, _ := io.ReadAll(f)
				f.Close()
				op.file, op.filename = string(b), fh.Filename
			}
		}
	} else {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	op.name, op.vars = opNameOf(req.Query), req.Variables
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ops = append(s.ops, op)
	data := map[string]any{}
	switch op.name {
	case "SessionInitializeProgrammatic":
		if s.failInit {
			gqlFail(w, "Agent registration refused")
			return
		}
		s.n++
		uuid := fmt.Sprintf("%s-1111-4111-8111-%012d", s.prefix, s.n)
		in, _ := req.Variables["sessionInit"].(map[string]any)
		clientId := str(in["clientSessionId"])
		if clientId == "" {
			clientId = uuid
		}
		sess := map[string]any{"uuid": uuid, "clientSessionId": clientId, "agent": sAgent, "apiKey": sKey, "status": "OPEN"}
		s.sessions[uuid] = sess
		data["sessionInitializeProgrammatic"] = map[string]any{"uuid": uuid, "clientSessionId": clientId, "agent": sAgent,
			"status": "OPEN", "policyEvents": []any{}}
	case "SessionAddArtifact":
		in, _ := req.Variables["addArtifact"].(map[string]any)
		uuid := str(in["sessionUuid"])
		if s.failUpload {
			gqlFail(w, "artifact storage unavailable")
			return
		}
		if sess := s.sessions[uuid]; sess != nil && sess["status"] == "CLOSED" {
			gqlFail(w, "Session "+uuid+" is CLOSED — cannot add artifacts.")
			return
		}
		data["sessionAddArtifactProgrammatic"] = map[string]any{"uuid": uuid, "status": "OPEN", "artifacts": []any{"art-1"}}
	case "SessionCloseProgrammatic":
		if s.failClose {
			gqlFail(w, "close refused")
			return
		}
		uuid := str(req.Variables["sessionUuid"])
		if sess := s.sessions[uuid]; sess != nil {
			sess["status"] = "CLOSED"
		}
		data["sessionCloseProgrammatic"] = map[string]any{"uuid": uuid, "status": "CLOSED", "closedAt": "2026-10-02T00:00:00Z"}
	case "AgentSessionCurrentProgrammatic", "AgentGitSessionProgrammatic":
		sess := s.sessions[str(req.Variables["sessionUuid"])]
		if sess == nil {
			gqlFail(w, "Session not found")
			return
		}
		data["sessionProgrammatic"] = sess
	case "AgentTaskAssignProgrammatic":
		uuid := str(req.Variables["sessionUuid"])
		if str(req.Variables["taskUuid"]) == "t-held" {
			gqlFail(w, "Session "+uuid+" does not hold task t-held")
			return
		}
		if sess := s.sessions[uuid]; sess != nil && sess["status"] == "CLOSED" {
			gqlFail(w, "Session "+uuid+" is CLOSED, not OPEN")
			return
		}
		data["agentTaskAssignProgrammatic"] = map[string]any{"assignedAt": "2026-10-02T00:00:00Z",
			"task": map[string]any{"uuid": "t-1", "key": "RD-1", "title": "build it", "status": "ASSIGNED", "role": "coder"}}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
}

// sessWorld: two instances (A, the controlling instance where code sessions live; B, the board's), two repositories
// and a directory outside any repository, a fresh state directory, and the session flags reset.
type sessWorld struct {
	t            *testing.T
	a, b         *sessServer
	repoA, repoB string
	plain        string
}

func newSessWorld(t *testing.T) *sessWorld {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, v := range []string{"REARM_URI", "REARM_URL", "REARM_APIKEYID", "REARM_API_ID", "REARM_APIKEY", "REARM_API_KEY",
		"REARM_SESSION", "REARM_CLIENT_SESSION_ID", "CLAUDE_CODE_SESSION_ID", "CLAUDE_SESSION_ID"} {
		t.Setenv(v, "")
		os.Unsetenv(v)
	}
	w := &sessWorld{t: t, a: newSessServer(t, "aaaaaaaa"), b: newSessServer(t, "bbbbbbbb")}
	tmp := t.TempDir()
	w.repoA, w.repoB, w.plain = filepath.Join(tmp, "app"), filepath.Join(tmp, "docs"), filepath.Join(tmp, "plain")
	for _, d := range []string{w.repoA, w.repoB, w.plain} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, d := range []string{w.repoA, w.repoB} {
		if out, err := exec.Command("git", "-C", d, "init", "-q", "-b", "main").CombinedOutput(); err != nil {
			t.Fatalf("git init: %v\n%s", err, out)
		}
	}
	prevUri, prevCfg := rearmUri, cfgFile
	cfgFile = ""
	resetSessionFlags()
	t.Cleanup(func() {
		resetSessionFlags()
		apiClient, rearmUri, cfgFile, fallbackInUse = nil, prevUri, prevCfg, nil
	})
	t.Chdir(w.repoA)
	return w
}

// on points the credentials at an instance, as the key sKey.
func (w *sessWorld) on(s *sessServer) { w.onAs(s, sKey) }

// onAs points the credentials at an instance as another key: the instance issues them tokens naming that key.
func (w *sessWorld) onAs(s *sessServer, key string) {
	w.t.Helper()
	c, err := rearm.New(s.url, key, "secret")
	if err != nil {
		w.t.Fatal(err)
	}
	apiClient, rearmUri = c, s.url
}

// onWithoutToken points the credentials at an instance that has no token endpoint: the key is sent itself.
func (w *sessWorld) onWithoutToken(s *sessServer) {
	w.t.Helper()
	c, err := rearm.New(s.url, sKey, "secret", rearm.WithoutTokenExchange())
	if err != nil {
		w.t.Fatal(err)
	}
	apiClient, rearmUri = c, s.url
}

// resetFlags puts every flag of the commands back to its default, unchanged: cobra keeps flag state between tests.
func resetFlags(cmds ...*cobra.Command) {
	for _, c := range cmds {
		reset := func(f *pflag.Flag) {
			if rootCmd.PersistentFlags().Lookup(f.Name) == f {
				return // the credentials (--uri and the rest) are the world's, not the verb's
			}
			if sv, ok := f.Value.(pflag.SliceValue); ok {
				_ = sv.Replace(nil)
			} else {
				_ = f.Value.Set(f.DefValue)
			}
			f.Changed = false
		}
		c.Flags().VisitAll(reset)
		c.PersistentFlags().VisitAll(reset)
	}
}

func resetSessionFlags() {
	resetFlags(agentSessionOpenCmd, agentSessionCloseCmd, agentSessionCurrentCmd, agentSessionInitCmd)
	openOrientation, openOrientationText, openBoard, openRoles, closeFinal, currentSet, currentJson = "", "", "", nil, "", "", false
	agentName, agentModel, agentVendor, agentModelVersion, clientSessionId, sessionTitle = "", "", "", "", "", ""
	noDeviceInfo = true
}

// run parses the words on the command and runs it the way cobra does, through the agent hook, returning stdout,
// stderr and the exit code. The composites' Run functions exit, so their run functions are called directly.
func (w *sessWorld) run(c *cobra.Command, words ...string) (string, string, int) {
	w.t.Helper()
	resetFlags(c)
	if err := c.ParseFlags(words); err != nil {
		w.t.Fatalf("%s %v: %v", c.CommandPath(), words, err)
	}
	args := c.Flags().Args()
	if c.Args != nil {
		if err := c.Args(c, args); err != nil {
			return "", err.Error(), 1
		}
	}
	code := 0
	var out string
	errOut := stderrOf(w.t, func() {
		out = stdoutOf(w.t, func() {
			if fc := applySessionFallback(c); fc != 0 {
				code = fc
				return
			}
			switch c {
			case agentSessionOpenCmd:
				noDeviceInfo = true
				code = runSessionOpen()
			case agentSessionCloseCmd:
				code = runSessionClose(args)
			case agentSessionCurrentCmd:
				code = runSessionCurrent()
			default:
				c.Run(c, args)
			}
		})
	})
	return out, errOut, code
}

func (w *sessWorld) open(words ...string) (string, string, int) {
	w.t.Helper()
	base := []string{"--agent-name", "Claude Code", "--agent-model", "claude-opus-5-5", "--title", "RD-1 coder"}
	return w.run(agentSessionOpenCmd, append(base, words...)...)
}

// envOf reads the three env lines open prints.
func envOf(t *testing.T, out string) map[string]string {
	t.Helper()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("want exactly three env lines, got %d:\n%s", len(lines), out)
	}
	env := map[string]string{}
	for i, want := range []string{"REARM_SESSION", "REARM_CLIENT_SESSION_ID", "REARM_AGENT"} {
		k, v, ok := strings.Cut(lines[i], "=")
		if !ok || k != want || v == "" {
			t.Fatalf("line %d: want %s=<value>, got %q", i+1, want, lines[i])
		}
		env[k] = v
	}
	return env
}

func writeFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func entryOf(t *testing.T, repo, instance string) *currentSessionEntry {
	t.Helper()
	return currentSessionFor(repo, instanceKey(instance))
}

func tagOf(op sessOp) (string, string, string) {
	in, _ := op.vars["addArtifact"].(map[string]any)
	arts, _ := in["artifacts"].([]any)
	if len(arts) != 1 {
		return "", "", ""
	}
	a, _ := arts[0].(map[string]any)
	tags, _ := a["tags"].([]any)
	tag := ""
	if len(tags) == 1 {
		m, _ := tags[0].(map[string]any)
		tag = str(m["key"]) + "=" + str(m["value"])
	}
	return str(a["type"]), str(a["displayIdentifier"]), tag
}

func TestSessionOpenThenCurrentThenClose(t *testing.T) {
	w := newSessWorld(t)
	w.on(w.a)
	orient := writeFile(t, t.TempDir(), "orient.md", "# Orientation\n\nplan\n")
	out, errOut, code := w.open("--client-session-id", "code-r2-1", "--orientation", orient)
	if code != 0 {
		t.Fatalf("open: exit %d\n%s", code, errOut)
	}
	env := envOf(t, out)
	uuid := env["REARM_SESSION"]
	if env["REARM_CLIENT_SESSION_ID"] != "code-r2-1" || env["REARM_AGENT"] != sAgent || !strings.HasPrefix(uuid, "aaaaaaaa-") {
		t.Fatalf("env lines: %v", env)
	}
	if got := strings.Join(w.a.names(), ","); got != "SessionInitializeProgrammatic,SessionAddArtifact" {
		t.Fatalf("open sends init then the report, got %s", got)
	}
	up := w.a.last("SessionAddArtifact")
	if typ, disp, tag := tagOf(up); typ != "AGENTIC_REPORT" || disp != "orient" || tag != "agenticPhase=ORIENTATION" {
		t.Fatalf("the ORIENTATION report: type %s display %s tag %s", typ, disp, tag)
	}
	if up.file != "# Orientation\n\nplan\n" || str(up.vars["addArtifact"].(map[string]any)["sessionUuid"]) != uuid {
		t.Fatalf("the report body or session: %q %v", up.file, up.vars)
	}
	e := entryOf(t, w.repoA, w.a.url)
	if e == nil || e.SessionUuid != uuid || e.ClientSessionId != "code-r2-1" || e.AgentUuid != sAgent || e.RecordedBy != "session open" {
		t.Fatalf("the current session: %+v", e)
	}

	out, _, code = w.run(agentSessionCurrentCmd)
	if code != 0 || strings.TrimSpace(out) != uuid {
		t.Fatalf("current: exit %d, %q", code, out)
	}

	final := writeFile(t, t.TempDir(), "final.md", "# Final\n")
	_, errOut, code = w.run(agentSessionCloseCmd, "--final", final)
	if code != 0 {
		t.Fatalf("close --final: exit %d\n%s", code, errOut)
	}
	if got := strings.Join(w.a.names()[2:], ","); got != "SessionAddArtifact,SessionCloseProgrammatic" {
		t.Fatalf("close --final files the report then closes, got %s", got)
	}
	fin := w.a.last("SessionAddArtifact")
	if typ, disp, tag := tagOf(fin); typ != "AGENTIC_REPORT" || disp != "final" || tag != "agenticPhase=FINAL" || fin.file != "# Final\n" {
		t.Fatalf("the FINAL report: %s %s %s %q", typ, disp, tag, fin.file)
	}
	if str(w.a.last("SessionCloseProgrammatic").vars["sessionUuid"]) != uuid {
		t.Fatalf("close names the current session")
	}
	if e := entryOf(t, w.repoA, w.a.url); e != nil {
		t.Fatalf("close clears the current session: %+v", e)
	}
	if findStateBySessionUuid(uuid) != nil {
		t.Fatalf("close removes the session's local state, as before")
	}
	out, errOut, code = w.run(agentSessionCurrentCmd)
	if code != 1 || out != "" || !strings.Contains(errOut, "session current --set") {
		t.Fatalf("current after close: exit %d, out %q, err %q", code, out, errOut)
	}
}

func TestSessionOpenWithoutAnOrientation(t *testing.T) {
	w := newSessWorld(t)
	w.on(w.a)
	out, errOut, code := w.open()
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, errOut)
	}
	env := envOf(t, out)
	if got := strings.Join(w.a.names(), ","); got != "SessionInitializeProgrammatic" {
		t.Fatalf("no report, no upload: %s", got)
	}
	if e := entryOf(t, w.repoA, w.a.url); e == nil || e.SessionUuid != env["REARM_SESSION"] {
		t.Fatalf("still the current session: %+v", e)
	}
	if !strings.Contains(errOut, "no ORIENTATION report was given") {
		t.Fatalf("says no report was filed: %s", errOut)
	}
}

func TestSessionOpenFilesAnOrientationText(t *testing.T) {
	w := newSessWorld(t)
	w.on(w.a)
	if _, errOut, code := w.open("--orientation-text", "plan: build it"); code != 0 {
		t.Fatalf("exit %d\n%s", code, errOut)
	}
	up := w.a.last("SessionAddArtifact")
	if _, disp, tag := tagOf(up); up.file != "plan: build it" || disp != "orient" || tag != "agenticPhase=ORIENTATION" {
		t.Fatalf("the text is the report: %q %s %s", up.file, disp, tag)
	}
}

// A failed upload leaves the session open (no close is sent) and current, says which step failed and how to file the
// report, and is not retried.
func TestSessionOpenFailedUploadLeavesTheSessionOpen(t *testing.T) {
	w := newSessWorld(t)
	w.on(w.a)
	w.a.failUpload = true
	orient := writeFile(t, t.TempDir(), "orient.md", "plan\n")
	out, errOut, code := w.open("--orientation", orient)
	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	env := envOf(t, out)
	if got := strings.Join(w.a.names(), ","); got != "SessionInitializeProgrammatic,SessionAddArtifact" {
		t.Fatalf("one upload, no close: %s", got)
	}
	for _, want := range []string{"step 3 (ORIENTATION report) failed", "artifact storage unavailable", "stays open",
		"rearm agent session add-artifact " + env["REARM_SESSION"] + " --file " + orient + " --type AGENTIC_REPORT --display-id orient --tag agenticPhase=ORIENTATION"} {
		if !strings.Contains(errOut, want) {
			t.Fatalf("want %q in: %s", want, errOut)
		}
	}
	if e := entryOf(t, w.repoA, w.a.url); e == nil || e.SessionUuid != env["REARM_SESSION"] {
		t.Fatalf("the open session stays current: %+v", e)
	}
}

func TestSessionOpenStopsAtARefusedInit(t *testing.T) {
	w := newSessWorld(t)
	w.on(w.a)
	w.a.failInit = true
	orient := writeFile(t, t.TempDir(), "orient.md", "plan\n")
	out, errOut, code := w.open("--orientation", orient)
	if code != 1 || out != "" {
		t.Fatalf("exit %d, out %q", code, out)
	}
	if !strings.Contains(errOut, "step 1 (init) failed: Agent registration refused") {
		t.Fatalf("names the step: %s", errOut)
	}
	if got := strings.Join(w.a.names(), ","); got != "SessionInitializeProgrammatic" {
		t.Fatalf("nothing after the refusal: %s", got)
	}
	if e := entryOf(t, w.repoA, w.a.url); e != nil {
		t.Fatalf("nothing recorded: %+v", e)
	}
}

func TestSessionOpenRefusesLocallyBeforeInit(t *testing.T) {
	w := newSessWorld(t)
	w.on(w.a)
	orient := writeFile(t, t.TempDir(), "orient.md", "plan\n")
	for _, tc := range []struct {
		words []string
		want  string
	}{
		{[]string{"--orientation", orient, "--orientation-text", "x"}, "not both"},
		{[]string{"--orientation", filepath.Join(t.TempDir(), "missing.md")}, "no such file"},
		{[]string{"--orientation", writeFile(t, t.TempDir(), "empty.md", "\n")}, "is empty"},
	} {
		_, errOut, code := w.open(tc.words...)
		if code != 1 || !strings.Contains(errOut, tc.want) || !strings.Contains(errOut, "nothing was opened") {
			t.Fatalf("%v: exit %d: %s", tc.words, code, errOut)
		}
	}
	rearmUri = ""
	if _, errOut, code := w.open(); code != 1 || !strings.Contains(errOut, "the credentials name no instance") {
		t.Fatalf("no instance: exit %d: %s", code, errOut)
	}
	if n := len(w.a.names()); n != 0 {
		t.Fatalf("nothing is sent: %v", w.a.names())
	}
}

// open takes every flag init takes, with the same requirement.
func TestSessionOpenTakesEveryInitFlag(t *testing.T) {
	n := 0
	agentSessionInitCmd.PersistentFlags().VisitAll(func(f *pflag.Flag) {
		n++
		g := agentSessionOpenCmd.PersistentFlags().Lookup(f.Name)
		if g == nil {
			t.Errorf("open lacks init's --%s", f.Name)
			return
		}
		if g.Usage != f.Usage || fmt.Sprint(g.Annotations[cobra.BashCompOneRequiredFlag]) != fmt.Sprint(f.Annotations[cobra.BashCompOneRequiredFlag]) {
			t.Errorf("--%s differs between init and open", f.Name)
		}
	})
	if n < 12 {
		t.Fatalf("init has %d flags, expected its full set", n)
	}
	for _, name := range []string{"orientation", "orientation-text", "board", "role"} {
		if agentSessionOpenCmd.Flags().Lookup(name) == nil {
			t.Errorf("open lacks --%s", name)
		}
	}
}

func TestSessionOpenKeepsBoardAndRoles(t *testing.T) {
	w := newSessWorld(t)
	w.on(w.b)
	out, errOut, code := w.open("--board", "board-1", "--role", "coder", "--role", "tester")
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, errOut)
	}
	st := findStateBySessionUuid(envOf(t, out)["REARM_SESSION"])
	if st == nil || st.Board != "board-1" || strings.Join(st.DeclaredRoles, ",") != "coder,tester" {
		t.Fatalf("state: %+v", st)
	}
}

func TestSessionCloseFinalStopsBeforeTheCloseWhenTheUploadFails(t *testing.T) {
	w := newSessWorld(t)
	w.on(w.a)
	out, _, _ := w.open()
	uuid := envOf(t, out)["REARM_SESSION"]
	w.a.failUpload = true
	final := writeFile(t, t.TempDir(), "final.md", "done\n")
	_, errOut, code := w.run(agentSessionCloseCmd, "--final", final)
	if code != 1 || !strings.Contains(errOut, "step 1 (FINAL report) failed") || !strings.Contains(errOut, "was not closed") {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if got := strings.Join(w.a.names()[1:], ","); got != "SessionAddArtifact" {
		t.Fatalf("no close after a failed upload: %s", got)
	}
	if e := entryOf(t, w.repoA, w.a.url); e == nil || e.SessionUuid != uuid {
		t.Fatalf("still current: %+v", e)
	}
	// A refused close after a filed report says the report is filed.
	w.a.failUpload, w.a.failClose = false, true
	_, errOut, code = w.run(agentSessionCloseCmd, "--final", final)
	if code != 1 || !strings.Contains(errOut, "step 2 (close) failed") || !strings.Contains(errOut, "the FINAL report is filed") {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if e := entryOf(t, w.repoA, w.a.url); e == nil {
		t.Fatalf("a refused close clears nothing")
	}
}

func TestSessionCloseWithoutFinalNeedsTheUuid(t *testing.T) {
	w := newSessWorld(t)
	w.on(w.a)
	out, _, _ := w.open()
	uuid := envOf(t, out)["REARM_SESSION"]
	if _, errOut, code := w.run(agentSessionCloseCmd); code == 0 || !strings.Contains(errOut, "accepts 1 arg") {
		t.Fatalf("close without --final and without a uuid: exit %d %s", code, errOut)
	}
	if len(w.a.names()) != 1 {
		t.Fatalf("nothing sent: %v", w.a.names())
	}
	// The plain close is unchanged, and a closed session is no one's current session.
	if _, errOut, code := w.run(agentSessionCloseCmd, uuid); code != 0 {
		t.Fatalf("close <uuid>: exit %d %s", code, errOut)
	}
	if e := entryOf(t, w.repoA, w.a.url); e != nil {
		t.Fatalf("close <uuid> clears the current session: %+v", e)
	}
	if _, errOut, code := w.run(agentSessionCloseCmd, "--final", writeFile(t, t.TempDir(), "f.md", "x")); code != 1 ||
		!strings.Contains(errOut, "session current --set") {
		t.Fatalf("close --final with no current session: exit %d %s", code, errOut)
	}
}

// The refusal for a missing session names the way the verb takes its session (ARCHITECTURE round 3 §2, tester run 1
// T-2): close takes the positional uuid and has no --session flag, so its refusal names the uuid; a verb that takes
// the flag names the flag.
func TestNoSessionRefusalNamesHowTheVerbTakesItsSession(t *testing.T) {
	w := newSessWorld(t)
	w.on(w.a)
	_, errOut, code := w.run(agentSessionCloseCmd, "--final", writeFile(t, t.TempDir(), "f.md", "x"))
	if code != 1 || !strings.Contains(errOut, "rearm: no session uuid given, and no current session is recorded for "+w.repoA) ||
		!strings.Contains(errOut, "pass its uuid: rearm agent session close <session-uuid> --final <file>") ||
		strings.Contains(errOut, "--session") {
		t.Fatalf("close: exit %d %s", code, errOut)
	}
	_, errOut, code = w.run(agentTaskAssignCmd, "t-1")
	if code != 1 || !strings.Contains(errOut, "rearm: --session is required: no current session is recorded for "+w.repoA) ||
		!strings.Contains(errOut, "pass --session <session-uuid>, or record one") || strings.Contains(errOut, "session close <session-uuid>") {
		t.Fatalf("a verb with the flag: exit %d %s", code, errOut)
	}
	if len(w.a.names()) != 0 {
		t.Fatalf("nothing sent: %v", w.a.names())
	}
}

func TestSessionCloseClearsOnlyThatSession(t *testing.T) {
	w := newSessWorld(t)
	w.on(w.a)
	outA, _, _ := w.open()
	t.Chdir(w.repoB)
	outB, _, _ := w.open()
	a, b := envOf(t, outA)["REARM_SESSION"], envOf(t, outB)["REARM_SESSION"]
	// The same session made current in a second repository is cleared there too.
	t.Chdir(w.plain)
	if _, errOut, code := w.run(agentSessionCurrentCmd, "--set", a); code != 0 {
		t.Fatalf("--set: %s", errOut)
	}
	t.Chdir(w.repoA)
	if _, errOut, code := w.run(agentSessionCloseCmd, "--final", writeFile(t, t.TempDir(), "f.md", "x")); code != 0 {
		t.Fatalf("close: %s", errOut)
	}
	if entryOf(t, w.repoA, w.a.url) != nil || entryOf(t, w.plain, w.a.url) != nil {
		t.Fatalf("every entry naming the closed session is cleared")
	}
	if e := entryOf(t, w.repoB, w.a.url); e == nil || e.SessionUuid != b {
		t.Fatalf("the other repository keeps its session: %+v", e)
	}
}

func TestSessionCurrentPrintsNothingWithExit1(t *testing.T) {
	w := newSessWorld(t)
	w.on(w.a)
	out, errOut, code := w.run(agentSessionCurrentCmd)
	if code != 1 || out != "" || !strings.Contains(errOut, "session current --set") || !strings.Contains(errOut, w.repoA) {
		t.Fatalf("exit %d, out %q, err %q", code, out, errOut)
	}
	w.open("--client-session-id", "code-7")
	out, _, code = w.run(agentSessionCurrentCmd, "--json")
	var e map[string]any
	if code != 0 || json.Unmarshal([]byte(out), &e) != nil || e["clientSessionId"] != "code-7" || e["repository"] != w.repoA ||
		e["instance"] != instanceKey(w.a.url) {
		t.Fatalf("--json: exit %d %s", code, out)
	}
}

const (
	boardSession = "bbbbbbbb-9999-4999-8999-999999999999"
	otherSession = "bbbbbbbb-8888-4888-8888-888888888888"
)

func TestSessionCurrentSetRecordsAnOpenSession(t *testing.T) {
	w := newSessWorld(t)
	w.on(w.b)
	w.b.addSession(boardSession, "scully-coder-1", "2ffebe1a-ee9c-41d9-b052-88462ea33d81", "OPEN")
	out, errOut, code := w.run(agentSessionCurrentCmd, "--set", boardSession)
	if code != 0 || !strings.Contains(out, boardSession) || !strings.Contains(out, instanceKey(w.b.url)) {
		t.Fatalf("exit %d: %s %s", code, out, errOut)
	}
	if got := strings.Join(w.b.names(), ","); got != "AgentSessionCurrentProgrammatic" {
		t.Fatalf("one read: %s", got)
	}
	e := entryOf(t, w.repoA, w.b.url)
	if e == nil || e.SessionUuid != boardSession || e.ClientSessionId != "scully-coder-1" ||
		e.AgentUuid != "2ffebe1a-ee9c-41d9-b052-88462ea33d81" || e.RecordedBy != "session current --set" {
		t.Fatalf("entry: %+v", e)
	}
	if st := lookupAgentState(boardSession); st == nil || st.ClientSessionId != "scully-coder-1" || st.AgentUuid == "" {
		t.Fatalf("the client id and agent are kept in local state: %+v", st)
	}
	if entryOf(t, w.repoA, w.a.url) != nil {
		t.Fatalf("recorded on the credentials' instance only")
	}
}

func TestSessionCurrentSetRefusesAClosedSession(t *testing.T) {
	w := newSessWorld(t)
	w.on(w.b)
	w.b.addSession(boardSession, "scully-coder-1", sAgent, "CLOSED")
	_, errOut, code := w.run(agentSessionCurrentCmd, "--set", boardSession)
	if code != 1 || !strings.Contains(errOut, "is CLOSED") || !strings.Contains(errOut, "nothing was recorded") {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if entryOf(t, w.repoA, w.b.url) != nil {
		t.Fatalf("nothing recorded")
	}
}

// A session the server does not answer to these credentials (a key with no read on it) is refused at the read; the
// refusal records nothing, and a session the server answers without an agent is refused too. A session another key
// opened that these credentials CAN read is refused by the key comparison (agentSessionCurrentSetOwner_test.go).
func TestSessionCurrentSetRefusesAnotherKeysSession(t *testing.T) {
	w := newSessWorld(t)
	w.on(w.b)
	_, errOut, code := w.run(agentSessionCurrentCmd, "--set", otherSession)
	if code != 1 || !strings.Contains(errOut, "Session not found") || !strings.Contains(errOut, "these credentials opened") {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	w.b.addSession(boardSession, "scully-coder-1", "", "OPEN")
	_, errOut, code = w.run(agentSessionCurrentCmd, "--set", boardSession)
	if code != 1 || !strings.Contains(errOut, "no client id or agent") {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if entryOf(t, w.repoA, w.b.url) != nil {
		t.Fatalf("nothing recorded")
	}
	if _, errOut, code := w.run(agentSessionCurrentCmd, "--set", "not-a-uuid"); code != 1 || !strings.Contains(errOut, "not a session uuid") {
		t.Fatalf("a reference that is not a uuid: exit %d %s", code, errOut)
	}
}

// --set never rewrites a state this host already has: the long-lived board session's state keeps its bytes.
func TestSessionCurrentSetKeepsExistingLocalState(t *testing.T) {
	w := newSessWorld(t)
	w.on(w.b)
	w.b.addSession(boardSession, "scully-coder-1", sAgent, "OPEN")
	st := &agentSessionState{SessionUuid: boardSession, ClientSessionId: "scully-coder-1",
		SeenInputs: map[string][]string{"t-1": {"doc-1"}}, HopOutputs: map[string]*hopOutputs{"t-1": {Outputs: []string{"rel-1"}}}}
	if err := writeAgentState(st); err != nil {
		t.Fatal(err)
	}
	path, _ := agentStatePath("scully-coder-1")
	before, _ := os.ReadFile(path)
	// A state filed under the uuid alone (made by a read on another host) is not renamed either.
	uuidOnly := &agentSessionState{SessionUuid: otherSession, SeenInputs: map[string][]string{"t-2": {"doc-2"}}}
	if err := writeAgentState(uuidOnly); err != nil {
		t.Fatal(err)
	}
	w.b.addSession(otherSession, "scully-coder-2", sAgent, "OPEN")
	uuidPath, _ := agentStatePath(otherSession)
	beforeUuid, _ := os.ReadFile(uuidPath)

	for _, s := range []string{boardSession, otherSession} {
		if _, errOut, code := w.run(agentSessionCurrentCmd, "--set", s); code != 0 {
			t.Fatalf("--set %s: %s", s, errOut)
		}
	}
	after, _ := os.ReadFile(path)
	afterUuid, _ := os.ReadFile(uuidPath)
	if string(after) != string(before) || string(afterUuid) != string(beforeUuid) {
		t.Fatalf("existing state rewritten:\n%s\n---\n%s", before, after)
	}
	if p, _ := agentStatePath("scully-coder-2"); fileExists(p) {
		t.Fatalf("a uuid-filed state is not copied under the client id")
	}
	if e := entryOf(t, w.repoA, w.b.url); e == nil || e.ClientSessionId != "scully-coder-2" {
		t.Fatalf("the entry still keeps the client id: %+v", e)
	}
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// task assign records nothing: it is not tied to a repository.
func TestTaskAssignRecordsNoCurrentSession(t *testing.T) {
	w := newSessWorld(t)
	w.on(w.b)
	w.b.addSession(boardSession, "scully-coder-1", sAgent, "OPEN")
	if _, errOut, code := w.run(agentTaskAssignCmd, "t-1", "--session", boardSession); code != 0 {
		t.Fatalf("assign: %s", errOut)
	}
	if w.b.last("AgentTaskAssignProgrammatic").vars["sessionUuid"] != boardSession {
		t.Fatalf("assign sends the session")
	}
	dir, _ := currentSessionsDir()
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("assign recorded a current session: %v", entries)
	}
}
