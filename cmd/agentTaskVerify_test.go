package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	rearm "github.com/relizaio/rearm-client-go"
)

// rearm agent task verify (task RD5-1): each of the eight checks passing and failing with its reason and remedy,
// the server checks alone outside a repository, --no-code, --base, --code-session, the --json shape, exit codes
// 0, 1 and 2, and that it writes nothing: no mutation, no state write, no git change.

const (
	vTask    = "11111111-1111-4111-8111-111111111111"
	vSession = "22222222-2222-4222-8222-222222222222"
	vOther   = "33333333-3333-4333-8333-333333333333"
	vOut     = "44444444-4444-4444-8444-444444444444"
	vArch    = "55555555-5555-4555-8555-555555555555"
	vPR      = "https://github.com/acme/app/pull/7"
	vAgent   = "62df357e-a3a4-4df5-82d4-049e629d1c6b"
)

// verifyServer answers the three reads verify makes and records every operation it was sent.
type verifyServer struct {
	mu        sync.Mutex
	task      map[string]any
	caps      []any
	reports   map[string]map[string]any
	failTask  bool
	operation []string
}

func (s *verifyServer) serve() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		s.mu.Lock()
		defer s.mu.Unlock()
		s.operation = append(s.operation, strings.TrimSpace(req.Query))
		data := map[string]any{}
		switch {
		case strings.Contains(req.Query, "AgentTaskRoleConfigsProgrammatic"):
			data["agentTaskRoleConfigsProgrammatic"] = []any{
				map[string]any{"name": "coder", "requiredCapabilities": s.caps},
				map[string]any{"name": "tester", "requiredCapabilities": []any{}},
			}
		case strings.Contains(req.Query, "AgentElementCheckReportProgrammatic"):
			if rep, ok := s.reports[str(req.Variables["releaseUuid"])]; ok {
				data["agentElementCheckReportProgrammatic"] = rep
			} else {
				data["agentElementCheckReportProgrammatic"] = nil
			}
		case strings.Contains(req.Query, "AgentTaskProgrammatic"):
			if s.failTask {
				_ = json.NewEncoder(w).Encode(map[string]any{"errors": []any{map[string]any{"message": "Not authorized"}}})
				return
			}
			data["agentTaskProgrammatic"] = s.task
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
}

type verifyWorld struct {
	t    *testing.T
	srv  *verifyServer
	repo string
	bare string
}

func vGit(t *testing.T, dir, stdin string, args ...string) string {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	if stdin != "" {
		c.Stdin = strings.NewReader(stdin)
	}
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func trailered(subject, session string) string {
	return subject + "\n\nWhat changed.\n\nReARM-Agentic-Session: " + session + "\nReARM-Agent: " + vAgent +
		"\nCo-Authored-By: Claude <noreply@anthropic.com>\n"
}

// commit writes a file and commits it with the message.
func (w *verifyWorld) commit(file, message string) string {
	w.t.Helper()
	if err := os.WriteFile(filepath.Join(w.repo, file), []byte(file+time.Now().String()), 0o644); err != nil {
		w.t.Fatal(err)
	}
	vGit(w.t, w.repo, "", "add", file)
	vGit(w.t, w.repo, message, "commit", "-q", "-F", "-")
	return vGit(w.t, w.repo, "", "rev-parse", "HEAD")
}

// publishPR pushes HEAD as PR 7's head and makes the PR row report it.
func (w *verifyWorld) publishPR() string {
	w.t.Helper()
	head := vGit(w.t, w.repo, "", "rev-parse", "HEAD")
	vGit(w.t, w.repo, "", "push", "-q", "-f", "origin", "HEAD:refs/pull/7/head")
	w.pr()["head"] = head
	return head
}

func (w *verifyWorld) pr() map[string]any {
	return w.srv.task["pullRequests"].([]any)[0].(map[string]any)
}

func (w *verifyWorld) assignment() map[string]any {
	return w.srv.task["assignment"].(map[string]any)
}

func vDoc(uuid, spec string, round int, session, lifecycle, created string) map[string]any {
	return map[string]any{"uuid": uuid, "version": "0.0.1", "lifecycle": lifecycle, "createdDate": created,
		"document": map[string]any{"specification": spec, "round": float64(round), "task": vTask, "session": session}}
}

// newVerifyWorld: a hop held by vSession since 2020, which published one DRAFT note, read the architecture,
// and linked PR 7 of acme/app; the checkout's feature branch has one trailered commit, pushed as the PR head.
func newVerifyWorld(t *testing.T) *verifyWorld {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	for _, v := range []string{"GIT_TRACE", "GIT_TRACE_PACKET", "GIT_TRACE_CURL", "GIT_CURL_VERBOSE"} {
		t.Setenv(v, "")
	}
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("HOME", t.TempDir())
	tmp := t.TempDir()
	w := &verifyWorld{t: t, bare: filepath.Join(tmp, "app.git"), repo: filepath.Join(tmp, "app")}
	vGit(t, tmp, "", "init", "-q", "--bare", "-b", "main", w.bare)
	if err := os.MkdirAll(w.repo, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "t"},
		{"config", "commit.gpgsign", "false"},
		{"remote", "add", "origin", "https://github.com/acme/app.git"},
		{"config", "url." + w.bare + ".insteadOf", "https://github.com/acme/app.git"},
	} {
		vGit(t, w.repo, "", args...)
	}
	w.commit("base.txt", "the base\n")
	vGit(t, w.repo, "", "push", "-q", "origin", "main")
	vGit(t, w.repo, "", "checkout", "-q", "-b", "feature")
	w.commit("feature.txt", trailered("feat: the feature", "c-1"))

	w.srv = &verifyServer{caps: []any{"CODE_PUSH"}, reports: map[string]map[string]any{}}
	w.srv.task = map[string]any{
		"uuid": vTask, "key": "RD-1", "board": "b-1", "role": "coder",
		"assignment":   map[string]any{"session": vSession, "role": "coder", "assignedAt": "2020-01-01T00:00:00Z"},
		"prUrls":       []any{vPR},
		"pullRequests": []any{map[string]any{"url": vPR, "registered": true, "state": "OPEN", "targetBranch": "main"}},
		"documents": []any{
			vDoc(vOut, "DETAILED_DESIGN", 1, vSession, "DRAFT", "2021-01-01T00:00:00Z"),
			vDoc(vArch, "ARCHITECTURE", 1, vOther, "ASSEMBLED", "2019-06-01T00:00:00Z"),
		},
	}
	w.publishPR()
	if err := writeAgentState(&agentSessionState{SessionUuid: vSession, ClientSessionId: "c-1",
		HopOutputs: map[string]*hopOutputs{vTask: {AssignedAt: "2020-01-01T00:00:00Z", Outputs: []string{vOut}}},
		SeenInputs: map[string][]string{vTask: {vArch}}}); err != nil {
		t.Fatal(err)
	}

	srv := w.srv.serve()
	t.Cleanup(srv.Close)
	c, err := rearm.New(srv.URL, "id", "secret", rearm.WithoutTokenExchange())
	if err != nil {
		t.Fatal(err)
	}
	apiClient = c
	prevLookup := taskKeyLookup
	taskKeyLookup = func(key string) (string, error) {
		if key == "RD-1" {
			return vTask, nil
		}
		return "", nil
	}
	verifySession = vSession
	t.Cleanup(func() {
		apiClient, taskKeyLookup = nil, prevLookup
		verifySession, verifyBase, verifyNoCode, verifyJson, verifyCodeSession = "", "", false, false, ""
	})
	t.Chdir(w.repo)
	return w
}

func (w *verifyWorld) run() (string, int) {
	w.t.Helper()
	code := -1
	out := stdoutOf(w.t, func() { code = runTaskVerify([]string{"RD-1"}) })
	return out, code
}

// line is the printed line of one check.
func line(t *testing.T, out, check string) string {
	t.Helper()
	for _, l := range strings.Split(out, "\n") {
		for _, word := range []string{"PASS ", "FAIL ", "SKIP "} {
			if strings.HasPrefix(l, word+check+": ") {
				return l
			}
		}
	}
	t.Fatalf("no line for %s in:\n%s", check, out)
	return ""
}

func wantLine(t *testing.T, out, check, word string, parts ...string) {
	t.Helper()
	l := line(t, out, check)
	if !strings.HasPrefix(l, word+" ") {
		t.Fatalf("%s: want %s, got: %s\n%s", check, word, l, out)
	}
	for _, p := range parts {
		if !strings.Contains(l, p) {
			t.Fatalf("%s: want %q in: %s", check, p, l)
		}
	}
}

func TestVerifyPassesAWholeHop(t *testing.T) {
	w := newVerifyWorld(t)
	out, code := w.run()
	if code != 0 {
		t.Fatalf("exit %d, want 0:\n%s", code, out)
	}
	order := []string{checkOutputs, checkInputs, checkBlocking, checkCodeMoved, checkTrailers, checkSubjects, checkHead, checkBase}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 9 {
		t.Fatalf("want 8 checks and a summary:\n%s", out)
	}
	for i, name := range order {
		if !strings.HasPrefix(lines[i], "PASS "+name+": ") {
			t.Fatalf("line %d: want PASS %s, got %s", i, name, lines[i])
		}
	}
	if lines[8] != "RD-1: 8 pass, 0 fail, 0 skip" {
		t.Fatalf("summary: %s", lines[8])
	}
	wantLine(t, out, checkOutputs, "PASS", "DETAILED_DESIGN round 1 ("+vOut+")")
	wantLine(t, out, checkTrailers, "PASS", "1 commit(s) since the merge base with origin/main", "session c-1")
	wantLine(t, out, checkHead, "PASS", vPR+" is at HEAD", "as CI reported it")
	wantLine(t, out, checkBase, "PASS", "origin/main at")
}

func TestVerifyOutputsFailsWithoutARecordedOutput(t *testing.T) {
	w := newVerifyWorld(t)
	if err := writeAgentState(&agentSessionState{SessionUuid: vSession, ClientSessionId: "c-1",
		SeenInputs: map[string][]string{vTask: {vArch}}}); err != nil {
		t.Fatal(err)
	}
	out, code := w.run()
	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	wantLine(t, out, checkOutputs, "FAIL", "no output recorded for this hop.", "Remedy: publish, or pass `--outputs` at sign-off")
	wantLine(t, out, checkBlocking, "SKIP", "no output recorded")
}

func TestVerifyOutputsFailsOnAnOutputAlreadyHandedOver(t *testing.T) {
	w := newVerifyWorld(t)
	w.srv.task["documents"].([]any)[0].(map[string]any)["lifecycle"] = "ASSEMBLED"
	out, _ := w.run()
	wantLine(t, out, checkOutputs, "FAIL", "DETAILED_DESIGN round 1 ("+vOut+") is ASSEMBLED, not DRAFT")
}

func TestVerifyOutputsFailsForAnOutputFromAnEarlierHop(t *testing.T) {
	w := newVerifyWorld(t)
	w.srv.task["documents"].([]any)[0].(map[string]any)["createdDate"] = "2019-12-31T00:00:00Z"
	out, _ := w.run()
	wantLine(t, out, checkOutputs, "FAIL", "Release "+vOut+" was published before this hop began, so it is not an output of it")
}

// T-1 (design 3.2.1): an advisory round is never a hop's output, even a DRAFT this session published in this hop.
func TestVerifyOutputsRefusesAnAdvisoryRound(t *testing.T) {
	w := newVerifyWorld(t)
	w.srv.task["documents"].([]any)[0].(map[string]any)["document"].(map[string]any)["advisory"] = true
	out, code := w.run()
	if code != 1 {
		t.Fatalf("exit %d, want 1:\n%s", code, out)
	}
	wantLine(t, out, checkOutputs, "FAIL", "DETAILED_DESIGN round 1 (advisory) ("+vOut+") is an advisory round, never a hop's output",
		"Remedy: publish this hop's document again")
}

// A hop that published three versions of one round recorded all three; the line names the newest once.
func TestVerifyOutputsNamesARepublishedRoundOnce(t *testing.T) {
	w := newVerifyWorld(t)
	v2, v3 := "99999999-9999-4999-8999-999999999992", "99999999-9999-4999-8999-999999999993"
	docs := w.srv.task["documents"].([]any)
	docs[0].(map[string]any)["document"].(map[string]any)["supersededBy"] = v2
	second := vDoc(v2, "DETAILED_DESIGN", 1, vSession, "DRAFT", "2021-02-01T00:00:00Z")
	second["document"].(map[string]any)["supersededBy"] = v3
	w.srv.task["documents"] = append([]any{vDoc(v3, "DETAILED_DESIGN", 1, vSession, "DRAFT", "2021-03-01T00:00:00Z"), second}, docs...)
	if err := writeAgentState(&agentSessionState{SessionUuid: vSession, ClientSessionId: "c-1",
		HopOutputs: map[string]*hopOutputs{vTask: {AssignedAt: "2020-01-01T00:00:00Z", Outputs: []string{vOut, v2, v3}}},
		SeenInputs: map[string][]string{vTask: {vArch}}}); err != nil {
		t.Fatal(err)
	}
	out, code := w.run()
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	wantLine(t, out, checkOutputs, "PASS", "recorded and DRAFT: DETAILED_DESIGN round 1 ("+v3+").")
	if l := line(t, out, checkOutputs); strings.Count(l, "DETAILED_DESIGN") != 1 {
		t.Fatalf("the round is named once: %s", l)
	}
}

func TestVerifyServerChecksFailForASessionThatDoesNotHoldTheTask(t *testing.T) {
	w := newVerifyWorld(t)
	w.assignment()["session"] = vOther
	out, code := w.run()
	if code != 1 {
		t.Fatalf("exit %d", code)
	}
	wantLine(t, out, checkOutputs, "FAIL", "this session does not hold RD-1 (held by 33333333)", "task assign")
	wantLine(t, out, checkInputs, "FAIL", "this session does not hold RD-1")
}

func TestVerifyInputsNamesARoundPublishedSinceTheAssignment(t *testing.T) {
	w := newVerifyWorld(t)
	docs := w.srv.task["documents"].([]any)
	w.srv.task["documents"] = append([]any{vDoc("66666666-6666-4666-8666-666666666666", "ARCHITECTURE", 2, vOther, "ASSEMBLED",
		"2022-01-01T00:00:00Z")}, docs...)
	w.srv.task["documents"].([]any)[0].(map[string]any)["document"].(map[string]any)["advisory"] = true
	out, code := w.run()
	if code != 1 {
		t.Fatalf("exit %d", code)
	}
	wantLine(t, out, checkInputs, "FAIL",
		"Task RD-1 has documents published since your assignment that this sign-off does not acknowledge: ARCHITECTURE round 2 (advisory) v0.0.1.",
		"Remedy: Run task show --session "+vSession+", read it, then sign off again.")

	// Read, it passes; the hop's own output and the board's check report on it never count.
	report := vDoc("77777777-7777-4777-8777-777777777777", "BOARD_ELEMENT_CHECK_REPORT", 1, "", "ASSEMBLED", "2022-02-01T00:00:00Z")
	report["document"].(map[string]any)["elementChecks"] = map[string]any{"scope": map[string]any{"checked": vOut}}
	w.srv.task["documents"] = append(w.srv.task["documents"].([]any), report)
	if err := writeAgentState(&agentSessionState{SessionUuid: vSession, ClientSessionId: "c-1",
		HopOutputs: map[string]*hopOutputs{vTask: {Outputs: []string{vOut}}},
		SeenInputs: map[string][]string{vTask: {vArch, "66666666-6666-4666-8666-666666666666"}}}); err != nil {
		t.Fatal(err)
	}
	out, _ = w.run()
	wantLine(t, out, checkInputs, "PASS", "the 1 round(s) published since your assignment are recorded as read")
}

// T-2 (design 3.2.2): only rounds published after the assignment are inputs the sign-off must acknowledge; another
// session's round from before it is left out, read or not, as the server's RD2-34 guard leaves it out.
func TestVerifyInputsLeavesOutRoundsFromBeforeTheAssignment(t *testing.T) {
	w := newVerifyWorld(t)
	after := "66666666-6666-4666-8666-666666666666"
	w.srv.task["documents"] = append([]any{vDoc(after, "ARCHITECTURE", 2, vOther, "ASSEMBLED", "2022-01-01T00:00:00Z")},
		w.srv.task["documents"].([]any)...)
	// The 2019 architecture round (vArch) predates the 2020 assignment and is not recorded as read; round 2 is.
	if err := writeAgentState(&agentSessionState{SessionUuid: vSession, ClientSessionId: "c-1",
		HopOutputs: map[string]*hopOutputs{vTask: {AssignedAt: "2020-01-01T00:00:00Z", Outputs: []string{vOut}}},
		SeenInputs: map[string][]string{vTask: {after}}}); err != nil {
		t.Fatal(err)
	}
	out, code := w.run()
	if code != 0 {
		t.Fatalf("exit %d, want 0:\n%s", code, out)
	}
	wantLine(t, out, checkInputs, "PASS", "the 1 round(s) published since your assignment are recorded as read")
}

func TestVerifyBlockingChecksReadsTheNewestReport(t *testing.T) {
	w := newVerifyWorld(t)
	report := func(blocking bool) map[string]any {
		return map[string]any{"uuid": "r-1", "lifecycle": "ASSEMBLED", "document": map[string]any{"round": float64(2),
			"elementChecks": map[string]any{"results": []any{
				map[string]any{"check": "glossary.terms_defined", "result": "FAIL", "blocking": blocking,
					"offences": []any{map[string]any{"message": "term Hop is not defined"}}},
				map[string]any{"check": "tests.no_orphans", "result": "PASS", "blocking": true},
			}}}}
	}
	w.srv.reports[vOut] = report(true)
	out, code := w.run()
	if code != 1 {
		t.Fatalf("exit %d", code)
	}
	wantLine(t, out, checkBlocking, "FAIL",
		"hand-over refused: blocking check(s) failed on DETAILED_DESIGN round 1 (check report round 2): glossary.terms_defined (term Hop is not defined).",
		"Remedy: fix and republish, or run `rearm agent doc element-check` after the inputs change")

	w.srv.reports[vOut] = report(false)
	out, _ = w.run()
	wantLine(t, out, checkBlocking, "PASS", "no blocking check fails on the newest report of 1 output(s)")
}

func TestVerifyBlockingChecksFollowsTheReplacingVersion(t *testing.T) {
	w := newVerifyWorld(t)
	newer := "88888888-8888-4888-8888-888888888888"
	w.srv.task["documents"].([]any)[0].(map[string]any)["document"].(map[string]any)["supersededBy"] = newer
	w.srv.task["documents"] = append([]any{vDoc(newer, "DETAILED_DESIGN", 1, vSession, "DRAFT", "2021-02-01T00:00:00Z")},
		w.srv.task["documents"].([]any)...)
	w.srv.reports[newer] = map[string]any{"document": map[string]any{"round": float64(3), "elementChecks": map[string]any{
		"results": []any{map[string]any{"check": "c.x", "result": "FAIL", "blocking": true, "offences": []any{}}}}}}
	out, _ := w.run()
	wantLine(t, out, checkBlocking, "FAIL", "(check report round 3): c.x")
	wantLine(t, out, checkOutputs, "PASS", "("+newer+")")
}

func TestVerifyCodeMoved(t *testing.T) {
	w := newVerifyWorld(t)
	head := shortSha(w.pr()["head"].(string))
	// Assigned after the head was committed: nothing moved.
	w.assignment()["assignedAt"] = time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	out, code := w.run()
	if code != 1 {
		t.Fatalf("exit %d", code)
	}
	wantLine(t, out, checkCodeMoved, "FAIL", "No linked PR moved since your assignment ("+vPR+" at "+head+")",
		"Remedy: push your commits to the PR branches, or sign off with --no-code for a round that changed no code")

	verifyNoCode = true
	out, _ = w.run()
	wantLine(t, out, checkCodeMoved, "PASS", "--no-code: the sign-off records that this round changed no code")
	verifyNoCode = false

	w.srv.caps = []any{"TRACKER_READ"}
	out, _ = w.run()
	wantLine(t, out, checkCodeMoved, "PASS", "the coder role does not push code (no CODE_PUSH)")
	w.srv.caps = []any{"CODE_PUSH"}

	w.pr()["registered"] = false
	out, _ = w.run()
	wantLine(t, out, checkCodeMoved, "PASS", "no linked PR is in play here")
}

func TestVerifyCodeMovedCannotDateAHeadThisRepositoryLacks(t *testing.T) {
	w := newVerifyWorld(t)
	w.pr()["head"] = "0123456789abcdef0123456789abcdef01234567"
	out, _ := w.run()
	wantLine(t, out, checkCodeMoved, "SKIP", "cannot tell whether "+vPR+" at 0123456 moved")
}

func TestVerifyTrailersFailsOnABlankLineInTheBlock(t *testing.T) {
	w := newVerifyWorld(t)
	sha := w.commit("two.txt", "fix: split block\n\nReARM-Agentic-Session: c-1\nReARM-Agent: "+vAgent+
		"\n\nCo-Authored-By: Claude <noreply@anthropic.com>\n")
	w.publishPR()
	out, code := w.run()
	if code != 1 {
		t.Fatalf("exit %d", code)
	}
	wantLine(t, out, checkTrailers, "FAIL", shortSha(sha)+" lacks ReARM-Agentic-Session, ReARM-Agent in its final paragraph",
		"one final paragraph", "never force-pushed")
}

func TestVerifyTrailersFailsOnAMergeCommitWithout(t *testing.T) {
	w := newVerifyWorld(t)
	vGit(t, w.repo, "", "checkout", "-q", "main")
	w.commit("sibling.txt", "a sibling lands\n")
	vGit(t, w.repo, "", "push", "-q", "origin", "main")
	vGit(t, w.repo, "", "checkout", "-q", "feature")
	vGit(t, w.repo, "", "merge", "-q", "--no-ff", "--no-edit", "origin/main")
	merge := w.publishPR()
	out, _ := w.run()
	wantLine(t, out, checkTrailers, "FAIL", shortSha(merge)+" (merge) lacks ReARM-Agentic-Session, ReARM-Agent, Co-Authored-By")
	// The sibling's own commit is the base's, outside the range.
	if strings.Count(line(t, out, checkTrailers), "lacks") != 1 {
		t.Fatalf("only the merge lacks trailers: %s", line(t, out, checkTrailers))
	}
	wantLine(t, out, checkBase, "PASS")
}

func TestVerifyTrailersComparesTheSession(t *testing.T) {
	w := newVerifyWorld(t)
	first := w.pr()["head"].(string)
	sha := w.commit("code.txt", trailered("feat: code session", "code-9"))
	w.publishPR()
	out, _ := w.run()
	wantLine(t, out, checkTrailers, "FAIL", shortSha(sha)+" carries ReARM-Agentic-Session code-9, not c-1", "pass --code-session")

	verifyCodeSession = "code-9"
	out, _ = w.run()
	l := line(t, out, checkTrailers)
	if !strings.Contains(l, shortSha(first)+" carries ReARM-Agentic-Session c-1, not code-9") || strings.Contains(l, shortSha(sha)) {
		t.Fatalf("--code-session is the session compared: %s", l)
	}
}

// T-3 (design 3.2.5): ReARM-Agent is the same on every commit; two commits with different agents fail, naming both.
func TestVerifyTrailersFailsWhenTheAgentDiffers(t *testing.T) {
	w := newVerifyWorld(t)
	first := shortSha(w.pr()["head"].(string))
	otherAgent := "00000000-0000-4000-8000-000000000000"
	sha := w.commit("agent.txt", "feat: another agent\n\nWhat changed.\n\nReARM-Agentic-Session: c-1\nReARM-Agent: "+otherAgent+
		"\nCo-Authored-By: Claude <noreply@anthropic.com>\n")
	w.publishPR()
	out, code := w.run()
	if code != 1 {
		t.Fatalf("exit %d, want 1:\n%s", code, out)
	}
	wantLine(t, out, checkTrailers, "FAIL", "ReARM-Agent differs between commits: ", first+" has "+vAgent, shortSha(sha)+" has "+otherAgent)
	if l := line(t, out, checkTrailers); strings.Contains(l, "lacks") || strings.Contains(l, "carries") {
		t.Fatalf("the agent is the only problem: %s", l)
	}
}

func TestVerifyTrailersPassWithTheCodeSession(t *testing.T) {
	w := newVerifyWorld(t)
	// The two sessions of a board hop: the code commits carry the code session's id, not the board session's.
	vGit(t, w.repo, "", "reset", "-q", "--hard", "main")
	w.commit("code.txt", trailered("feat: code session", "code-9"))
	w.publishPR()
	verifyCodeSession = "code-9"
	out, code := w.run()
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	wantLine(t, out, checkTrailers, "PASS", "1 commit(s)", "session code-9")
}

func TestVerifySubjectsFailsOnADoubleQuote(t *testing.T) {
	w := newVerifyWorld(t)
	sha := w.commit("q.txt", trailered(`fix: say "hello"`, "c-1"))
	w.publishPR()
	out, code := w.run()
	if code != 1 {
		t.Fatalf("exit %d", code)
	}
	wantLine(t, out, checkSubjects, "FAIL", shortSha(sha)+` has a double quote in its subject: fix: say "hello"`, "rearm-actions templates")
	wantLine(t, out, checkTrailers, "PASS")
}

// T-4 (design 3.2.6): the quote check reads the body too, not only the subject.
func TestVerifySubjectsFailsOnADoubleQuoteInTheBody(t *testing.T) {
	w := newVerifyWorld(t)
	sha := w.commit("b.txt", "fix: a plain subject\n\nThe page said \"hello\".\n\nReARM-Agentic-Session: c-1\nReARM-Agent: "+vAgent+
		"\nCo-Authored-By: Claude <noreply@anthropic.com>\n")
	w.publishPR()
	out, code := w.run()
	if code != 1 {
		t.Fatalf("exit %d, want 1:\n%s", code, out)
	}
	wantLine(t, out, checkSubjects, "FAIL", shortSha(sha)+" has a double quote in its body: fix: a plain subject", "rearm-actions templates")
	wantLine(t, out, checkTrailers, "PASS")
}

func TestVerifyHeadSaysWhatIsNotPushed(t *testing.T) {
	w := newVerifyWorld(t)
	pushed := shortSha(w.pr()["head"].(string))
	local := w.commit("local.txt", trailered("feat: unpushed", "c-1"))
	out, code := w.run()
	if code != 1 {
		t.Fatalf("exit %d", code)
	}
	wantLine(t, out, checkHead, "FAIL", "1 commit not pushed: "+vPR+" is at "+pushed+", HEAD is "+shortSha(local),
		"git push origin HEAD:<head ref>")
}

func TestVerifyHeadFailsWhenCommitsWentElsewhere(t *testing.T) {
	w := newVerifyWorld(t)
	vGit(t, w.repo, "", "checkout", "-q", "-b", "side", "main")
	other := w.commit("side.txt", trailered("feat: elsewhere", "c-1"))
	vGit(t, w.repo, "", "checkout", "-q", "feature")
	w.pr()["head"] = other
	out, _ := w.run()
	wantLine(t, out, checkHead, "FAIL", vPR+" is at "+shortSha(other)+" (as CI reported it), which is not in HEAD's history")
}

func TestVerifyHeadReadsThePullRefWhenTheRowHasNoHead(t *testing.T) {
	w := newVerifyWorld(t)
	w.pr()["head"] = nil
	w.pr()["registered"] = false
	out, code := w.run()
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	wantLine(t, out, checkHead, "PASS", "(from git ls-remote)")
}

func TestVerifyBaseFailsWhenTheBaseMoved(t *testing.T) {
	w := newVerifyWorld(t)
	other := filepath.Join(t.TempDir(), "other")
	vGit(t, filepath.Dir(other), "", "clone", "-q", "-b", "main", w.bare, other)
	vGit(t, other, "", "-c", "user.email=o@example.com", "-c", "user.name=o", "-c", "commit.gpgsign=false",
		"commit", "-q", "--allow-empty", "-m", "a sibling merged")
	vGit(t, other, "", "push", "-q", "origin", "main")
	tip := shortSha(vGit(t, other, "", "rev-parse", "HEAD"))
	out, code := w.run()
	if code != 1 {
		t.Fatalf("exit %d", code)
	}
	wantLine(t, out, checkBase, "FAIL", "base moved since your merge: origin/main is at "+tip+", which HEAD does not contain",
		"Remedy: merge it and retry")
}

func TestVerifyBaseFlagStandsInForAnUnregisteredRow(t *testing.T) {
	w := newVerifyWorld(t)
	delete(w.pr(), "targetBranch")
	out, _ := w.run()
	for _, c := range []string{checkTrailers, checkSubjects, checkBase} {
		wantLine(t, out, c, "FAIL", "no base branch known for "+vPR, "Remedy: pass --base <branch>")
	}
	verifyBase = "origin/main"
	out, code := w.run()
	if code != 0 {
		t.Fatalf("exit %d with --base:\n%s", code, out)
	}
	wantLine(t, out, checkBase, "PASS", "origin/main at")
}

func TestVerifyServerChecksAloneOutsideARepository(t *testing.T) {
	w := newVerifyWorld(t)
	t.Chdir(t.TempDir())
	out, code := w.run()
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	for _, c := range []string{checkTrailers, checkSubjects, checkHead, checkBase} {
		wantLine(t, out, c, "SKIP", "not in a git repository")
	}
	wantLine(t, out, checkOutputs, "PASS")
	// A registered head is dated from the checkout; outside one it cannot be told, and says so.
	wantLine(t, out, checkCodeMoved, "SKIP", "run verify from that PR's checkout")
	if !strings.Contains(out, "RD-1: 3 pass, 0 fail, 5 skip") {
		t.Fatalf("summary:\n%s", out)
	}
}

func TestVerifySkipsTheGitChecksInAnotherRepository(t *testing.T) {
	w := newVerifyWorld(t)
	vGit(t, w.repo, "", "remote", "set-url", "origin", "https://github.com/acme/docs.git")
	out, _ := w.run()
	wantLine(t, out, checkHead, "SKIP", "this repository (github.com/acme/docs) is not one a PR linked to RD-1 names")
}

func TestVerifyJsonShape(t *testing.T) {
	w := newVerifyWorld(t)
	w.srv.task["documents"].([]any)[0].(map[string]any)["lifecycle"] = "ASSEMBLED"
	verifyJson = true
	out, code := w.run()
	if code != 1 {
		t.Fatalf("exit %d", code)
	}
	var got struct {
		Task   string           `json:"task"`
		Ok     *bool            `json:"ok"`
		Checks []map[string]any `json:"checks"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not one JSON object: %v\n%s", err, out)
	}
	if got.Task != "RD-1" || got.Ok == nil || *got.Ok || len(got.Checks) != 8 {
		t.Fatalf("shape: %s", out)
	}
	names := []string{checkOutputs, checkInputs, checkBlocking, checkCodeMoved, checkTrailers, checkSubjects, checkHead, checkBase}
	for i, c := range got.Checks {
		if c["name"] != names[i] {
			t.Fatalf("check %d is %v, want %s", i, c["name"], names[i])
		}
		if _, ok := c["ok"].(bool); !ok || str(c["reason"]) == "" {
			t.Fatalf("check %v lacks ok or reason: %v", c["name"], c)
		}
	}
	if got.Checks[0]["ok"] != false || str(got.Checks[0]["remedy"]) == "" {
		t.Fatalf("a failing check carries its remedy: %v", got.Checks[0])
	}
}

func TestVerifyExitCodes(t *testing.T) {
	w := newVerifyWorld(t)
	if _, code := w.run(); code != 0 {
		t.Fatalf("passing hop: exit %d", code)
	}
	verifySession = ""
	if _, code := w.run(); code != 2 {
		t.Fatalf("no --session: exit %d, want 2", code)
	}
	verifySession = vSession
	w.srv.failTask = true
	if _, code := w.run(); code != 2 {
		t.Fatalf("task read refused: exit %d, want 2", code)
	}
	w.srv.failTask = false
	prev := taskKeyLookup
	taskKeyLookup = func(string) (string, error) { return "", nil }
	if _, code := w.run(); code != 2 {
		t.Fatalf("unknown key: exit %d, want 2", code)
	}
	taskKeyLookup = prev
}

func TestVerifyWritesNothing(t *testing.T) {
	w := newVerifyWorld(t)
	// A pre-RD4-7 state: a normal read would migrate it and write the file back.
	if err := writeAgentState(&agentSessionState{SessionUuid: vSession, ClientSessionId: "c-1",
		PendingOutputs: map[string][]string{vTask: {vOut}}, SeenInputs: map[string][]string{vTask: {vArch}}}); err != nil {
		t.Fatal(err)
	}
	path, _ := agentStatePath("c-1")
	before, _ := os.ReadFile(path)
	refs := vGit(t, w.repo, "", "for-each-ref")
	status := vGit(t, w.repo, "", "status", "--porcelain")
	remote := vGit(t, w.bare, "", "for-each-ref")
	out, code := w.run()
	if code != 0 {
		t.Fatalf("the old record is read as this hop's outputs: exit %d\n%s", code, out)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatalf("the state file changed")
	}
	if vGit(t, w.repo, "", "for-each-ref") != refs || vGit(t, w.repo, "", "status", "--porcelain") != status ||
		vGit(t, w.bare, "", "for-each-ref") != remote {
		t.Fatalf("git changed")
	}
	for _, op := range w.srv.operation {
		if strings.HasPrefix(op, "mutation") {
			t.Fatalf("verify sent a mutation: %.80s", op)
		}
	}
	if len(w.srv.operation) != 3 {
		t.Fatalf("want the task, roles and one report read, got %d operations", len(w.srv.operation))
	}
}

func TestVerifyTaskOperationSelectsWhatTheChecksRead(t *testing.T) {
	op, err := verifyTaskOperation()
	if err != nil {
		t.Fatal(err)
	}
	flat := strings.Join(strings.Fields(op), " ")
	for _, want := range []string{"documents { uuid createdDate",
		"document { specification session supersededBy elementChecks { scope { checked } }", "baseMovedBy"} {
		if !strings.Contains(flat, want) {
			t.Fatalf("the task read lacks %q", want)
		}
	}
}
