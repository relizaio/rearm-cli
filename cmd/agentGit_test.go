package cmd

import (
	"encoding/json"
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
)

// rearm agent git commit / merge (task RD5-2), against real temporary repositories: the block is three contiguous
// lines on a commit and on a merge commit; the session and agent come from state, or from one server read that is
// then kept (a session on another instance, read with that instance's client); the co-author refusal names its
// flag and the line is kept per agent; the staging, quote and trailer-like body refusals write nothing; the
// signing flags are passed as -c and kept; the parse check catches a block a hook broke; a conflict leaves the
// merge in progress and the commit helper finishes it; up to date; the printed commands; --json.

const (
	gSession  = "aaaaaaaa-1111-4111-8111-111111111111"
	gSession2 = "bbbbbbbb-2222-4222-8222-222222222222"
	gAgent    = "62df357e-a3a4-4df5-82d4-049e629d1c6b"
	gAgent2   = "2ffebe1a-ee9c-41d9-b052-88462ea33d81"
	gCoAuthor = "Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
)

type gitWorld struct {
	t       *testing.T
	repo    string
	keyRepo string // the repository's configured signing key (public half)
	keyB    string // a second key, for --signing-key
	allowed string
}

func gRun(t *testing.T, dir, stdin string, args ...string) string {
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

func gKey(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if out, err := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-C", name, "-f", path).CombinedOutput(); err != nil {
		t.Fatalf("ssh-keygen: %v\n%s", err, out)
	}
	return path + ".pub"
}

func gFingerprint(t *testing.T, pub string) string {
	t.Helper()
	out, err := exec.Command("ssh-keygen", "-lf", pub).Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.Fields(string(out))[1]
}

// newGitWorld: a repository on main with one commit, signing configured with keyRepo, and local state for
// gSession (client id code-1, agent gAgent). The helpers run in the repository.
func newGitWorld(t *testing.T) *gitWorld {
	t.Helper()
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("ssh-keygen is needed to sign")
	}
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	keys := t.TempDir()
	w := &gitWorld{t: t, repo: filepath.Join(t.TempDir(), "app")}
	w.keyRepo = gKey(t, keys, "repo_key")
	w.keyB = gKey(t, keys, "agent_key")
	w.allowed = filepath.Join(keys, "allowed")
	var allowed strings.Builder
	for _, k := range []string{w.keyRepo, w.keyB} {
		pub, _ := os.ReadFile(k)
		allowed.WriteString("t@example.com " + strings.TrimSpace(string(pub)) + "\n")
	}
	if err := os.WriteFile(w.allowed, []byte(allowed.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(w.repo, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "t"},
		{"config", "gpg.format", "ssh"},
		{"config", "user.signingkey", w.keyRepo},
		{"config", "gpg.ssh.allowedSignersFile", w.allowed},
		{"config", "commit.gpgsign", "false"},
	} {
		gRun(t, w.repo, "", args...)
	}
	w.write("base.txt", "base\n")
	gRun(t, w.repo, "", "add", "base.txt")
	gRun(t, w.repo, "the base\n", "commit", "-q", "-F", "-")
	if err := writeAgentState(&agentSessionState{SessionUuid: gSession, ClientSessionId: "code-1", AgentUuid: gAgent}); err != nil {
		t.Fatal(err)
	}
	gitSession = gSession
	t.Cleanup(func() {
		gitSession, gitMessages, gitCoAuthor, gitSigningKey, gitSigningFormat, gitJson = "", nil, "", "", "", false
		apiClient = nil
	})
	t.Chdir(w.repo)
	return w
}

func (w *gitWorld) write(name, body string) {
	w.t.Helper()
	p := filepath.Join(w.repo, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		w.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		w.t.Fatal(err)
	}
}

func (w *gitWorld) head() string { return gRun(w.t, w.repo, "", "rev-parse", "HEAD") }

func (w *gitWorld) staged() string {
	return gRun(w.t, w.repo, "", "diff", "--cached", "--name-only")
}

// parsed is HEAD's trailer block as git parses it.
func (w *gitWorld) parsed() []string {
	body := gRun(w.t, w.repo, "", "log", "-1", "--format=%B")
	out := gRun(w.t, w.repo, body+"\n", "interpret-trailers", "--parse")
	return strings.Split(out, "\n")
}

func (w *gitWorld) wantBlock(session, agent string) {
	w.t.Helper()
	got := w.parsed()
	want := []string{"ReARM-Agentic-Session: " + session, "ReARM-Agent: " + agent, "Co-Authored-By: " + gCoAuthor}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		w.t.Fatalf("trailer block:\n%s\nwant:\n%s\nmessage:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"),
			gRun(w.t, w.repo, "", "log", "-1", "--format=%B"))
	}
}

// signer is HEAD's signature state and the key's fingerprint, as git verifies it.
func (w *gitWorld) signer() (string, string) {
	out := gRun(w.t, w.repo, "", "log", "-1", "--format=%G?|%GF")
	state, fp, _ := strings.Cut(out, "|")
	return state, fp
}

// both runs f and returns what it printed on stdout and on stderr.
func both(t *testing.T, f func()) (string, string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stderr
	os.Stderr = w
	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	out := stdoutOf(t, f)
	w.Close()
	os.Stderr = saved
	return out, <-done
}

func (w *gitWorld) commit(paths ...string) (int, string, string) {
	w.t.Helper()
	code := -1
	out, errOut := both(w.t, func() { code = runGitCommit(paths) })
	return code, out, errOut
}

func (w *gitWorld) merge(ref string) (int, string, string) {
	w.t.Helper()
	code := -1
	out, errOut := both(w.t, func() { code = runGitMerge(ref) })
	return code, out, errOut
}

func TestGitCommitWritesTheBlockFromStateAndSigns(t *testing.T) {
	w := newGitWorld(t)
	w.write("a.txt", "a\n")
	gitMessages = []string{"feat: the thing", "Why it changed.\nSecond line."}
	gitCoAuthor = gCoAuthor
	code, out, errOut := w.commit("a.txt")
	if code != 0 {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	w.wantBlock("code-1", gAgent)
	msg := gRun(t, w.repo, "", "log", "-1", "--format=%B")
	if !strings.HasPrefix(msg, "feat: the thing\n\nWhy it changed.\nSecond line.\n\nReARM-Agentic-Session: code-1\n") {
		t.Fatalf("message:\n%s", msg)
	}
	if state, fp := w.signer(); state != "G" || fp != gFingerprint(t, w.keyRepo) {
		t.Fatalf("signature %s %s, want G by the repository's key", state, fp)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 || lines[0] != "ran: git add -- a.txt" ||
		lines[1] != "ran: git commit -q -S --cleanup=verbatim -F - -- a.txt" ||
		lines[2] != w.head()+" feat: the thing" {
		t.Fatalf("printed:\n%s", out)
	}
}

func TestGitCommitCoAuthorRefusalNamesTheFlagAndWritesNothing(t *testing.T) {
	w := newGitWorld(t)
	before := w.head()
	w.write("a.txt", "a\n")
	gitMessages = []string{"feat: x"}
	code, _, errOut := w.commit("a.txt")
	if code != 1 || !strings.Contains(errOut, "--co-author '<name> <email>'") || !strings.Contains(errOut, gAgent) {
		t.Fatalf("exit %d, stderr:\n%s", code, errOut)
	}
	if w.head() != before || w.staged() != "" {
		t.Fatal("a refused commit wrote to the repository")
	}
	gitCoAuthor = "no email here"
	if code, _, errOut := w.commit("a.txt"); code != 1 || !strings.Contains(errOut, "--co-author must read") {
		t.Fatalf("a malformed co-author line: exit %d\n%s", code, errOut)
	}
}

// The line is kept per AGENT: a second session of the same agent finds it, a session of another agent does not.
func TestGitCommitCoAuthorIsKeptPerAgent(t *testing.T) {
	w := newGitWorld(t)
	gitMessages = []string{"feat: one"}
	gitCoAuthor = gCoAuthor
	w.write("a.txt", "a\n")
	if code, out, errOut := w.commit("a.txt"); code != 0 {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	// A new code session for the same agent (the next round): no --co-author needed.
	if err := writeAgentState(&agentSessionState{SessionUuid: gSession2, ClientSessionId: "code-2", AgentUuid: gAgent}); err != nil {
		t.Fatal(err)
	}
	gitSession, gitCoAuthor, gitMessages = gSession2, "", []string{"feat: two"}
	w.write("b.txt", "b\n")
	if code, out, errOut := w.commit("b.txt"); code != 0 {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	w.wantBlock("code-2", gAgent)
	// The board agent has no line yet.
	if err := writeAgentState(&agentSessionState{SessionUuid: "cccccccc-3333-4333-8333-333333333333",
		ClientSessionId: "board-1", AgentUuid: gAgent2}); err != nil {
		t.Fatal(err)
	}
	gitSession = "board-1"
	w.write("c.txt", "c\n")
	if code, _, errOut := w.commit("c.txt"); code != 1 || !strings.Contains(errOut, gAgent2) || !strings.Contains(errOut, "--co-author") {
		t.Fatalf("another agent: exit %d\n%s", code, errOut)
	}
}

func TestGitCommitStagingRefusals(t *testing.T) {
	w := newGitWorld(t)
	gitCoAuthor, gitMessages = gCoAuthor, []string{"feat: x"}
	w.write("a.txt", "a\n")
	w.write("sub/b.txt", "b\n")
	before := w.head()
	for _, paths := range [][]string{nil, {"."}, {"./"}, {"a.txt", "."}, {"-A"}, {"--all"}, {":/"}, {w.repo}, {"sub/.."}} {
		code, _, errOut := w.commit(paths...)
		if code != 1 || !strings.Contains(errOut, "name the files") {
			t.Fatalf("paths %q: exit %d, stderr:\n%s", paths, code, errOut)
		}
		if w.head() != before || w.staged() != "" {
			t.Fatalf("paths %q: a refused commit wrote to the repository", paths)
		}
	}
	// '.' in a subdirectory stages only that directory, and is refused all the same.
	t.Chdir(filepath.Join(w.repo, "sub"))
	if code, _, errOut := w.commit("."); code != 1 || !strings.Contains(errOut, "'.' stages everything") || w.staged() != "" {
		t.Fatalf("'.' in a subdirectory: exit %d\n%s", code, errOut)
	}
	t.Chdir(w.repo)
	// A directory is a path.
	if code, out, errOut := w.commit("sub"); code != 0 {
		t.Fatalf("a directory: exit %d\n%s\n%s", code, out, errOut)
	}
	if got := gRun(t, w.repo, "", "show", "--name-only", "--format=", "HEAD"); got != "sub/b.txt" {
		t.Fatalf("committed %q", got)
	}
}

// Only the named paths are committed, even with other changes staged before.
func TestGitCommitCommitsOnlyTheNamedPaths(t *testing.T) {
	w := newGitWorld(t)
	gitCoAuthor, gitMessages = gCoAuthor, []string{"feat: only a"}
	w.write("a.txt", "a\n")
	w.write("other.txt", "o\n")
	w.write("stray.txt", "s\n")
	gRun(t, w.repo, "", "add", "other.txt")
	if code, out, errOut := w.commit("a.txt"); code != 0 {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	if got := gRun(t, w.repo, "", "show", "--name-only", "--format=", "HEAD"); got != "a.txt" {
		t.Fatalf("committed %q", got)
	}
	if w.staged() != "other.txt" {
		t.Fatalf("staged after: %q", w.staged())
	}
}

func TestGitCommitQuoteRefusal(t *testing.T) {
	w := newGitWorld(t)
	gitCoAuthor = gCoAuthor
	w.write("a.txt", "a\n")
	before := w.head()
	for _, m := range [][]string{{`feat: the "thing"`}, {"feat: x", `the body says "hi"`}} {
		gitMessages = m
		code, _, errOut := w.commit("a.txt")
		if code != 1 || !strings.Contains(errOut, "double quote") {
			t.Fatalf("%q: exit %d\n%s", m, code, errOut)
		}
		if w.head() != before || w.staged() != "" {
			t.Fatalf("%q: a refused commit wrote to the repository", m)
		}
	}
}

func TestGitCommitTrailerLikeBodyLineRefusal(t *testing.T) {
	w := newGitWorld(t)
	gitCoAuthor = gCoAuthor
	w.write("a.txt", "a\n")
	before := w.head()
	for _, body := range []string{"Fixes: the bug", "Prose first.\nCo-Authored-By: Someone <s@example.com>", "ReARM-Agent: 123"} {
		gitMessages = []string{"feat: x", body}
		code, _, errOut := w.commit("a.txt")
		if code != 1 || !strings.Contains(errOut, "reads as a trailer") {
			t.Fatalf("%q: exit %d\n%s", body, code, errOut)
		}
		if w.head() != before || w.staged() != "" {
			t.Fatalf("%q: a refused commit wrote to the repository", body)
		}
	}
	// A colon elsewhere in a line is prose.
	gitMessages = []string{"feat: x", "The fix: keep the block whole."}
	if code, out, errOut := w.commit("a.txt"); code != 0 {
		t.Fatalf("prose with a colon: exit %d\n%s\n%s", code, out, errOut)
	}
}

func TestGitCommitSigningFlagsArePassedAndKept(t *testing.T) {
	w := newGitWorld(t)
	gitCoAuthor, gitMessages = gCoAuthor, []string{"feat: signed by b"}
	gitSigningKey, gitSigningFormat = w.keyB, "ssh"
	w.write("a.txt", "a\n")
	code, out, errOut := w.commit("a.txt")
	if code != 0 {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	wantCmd := "ran: git -c gpg.format=ssh -c user.signingkey=" + w.keyB + " commit -q -S --cleanup=verbatim -F - -- a.txt"
	if !strings.Contains(out, wantCmd+"\n") {
		t.Fatalf("printed:\n%s\nwant %s", out, wantCmd)
	}
	if state, fp := w.signer(); state != "G" || fp != gFingerprint(t, w.keyB) {
		t.Fatalf("signature %s %s, want G by key b", state, fp)
	}
	// Kept: the next commit, from a new session of the same agent and without the flags, signs with key b.
	if err := writeAgentState(&agentSessionState{SessionUuid: gSession2, ClientSessionId: "code-2", AgentUuid: gAgent}); err != nil {
		t.Fatal(err)
	}
	gitSession, gitSigningKey, gitSigningFormat, gitCoAuthor = gSession2, "", "", ""
	w.write("b.txt", "b\n")
	code, out, errOut = w.commit("b.txt")
	if code != 0 || !strings.Contains(out, "-c user.signingkey="+w.keyB) {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	if state, fp := w.signer(); state != "G" || fp != gFingerprint(t, w.keyB) {
		t.Fatalf("second commit: signature %s %s, want G by key b", state, fp)
	}
	id, err := readAgentIdentity(gAgent)
	if err != nil || id.SigningKey != w.keyB || id.SigningFormat != "ssh" || id.CoAuthor != gCoAuthor {
		t.Fatalf("kept %+v %v", id, err)
	}
	// A key that is not there is refused before anything is written.
	before := w.head()
	gitSigningKey = filepath.Join(t.TempDir(), "missing.pub")
	w.write("c.txt", "c\n")
	if code, _, errOut := w.commit("c.txt"); code != 1 || !strings.Contains(errOut, "--signing-key") || w.head() != before {
		t.Fatalf("missing key: exit %d\n%s", code, errOut)
	}
}

// A signing failure is git's error, and nothing is committed.
func TestGitCommitSigningFailureCommitsNothing(t *testing.T) {
	w := newGitWorld(t)
	gRun(t, w.repo, "", "config", "user.signingkey", filepath.Join(t.TempDir(), "gone.pub"))
	gitCoAuthor, gitMessages = gCoAuthor, []string{"feat: x"}
	w.write("a.txt", "a\n")
	before := w.head()
	code, _, errOut := w.commit("a.txt")
	if code != 1 || !strings.Contains(errOut, "git commit failed; nothing was committed") || w.head() != before {
		t.Fatalf("exit %d\n%s", code, errOut)
	}
}

// The parse check is the proof: a commit-msg hook that adds a trailer, or a paragraph after the block, is caught
// and the commit is left for the agent.
func TestGitCommitParseCheckCatchesABrokenBlock(t *testing.T) {
	for name, hook := range map[string]string{
		"a fourth trailer":       "printf 'Change-Id: I0123\\n' >> \"$1\"\n",
		"a paragraph after":      "printf '\\nA footer paragraph.\\n' >> \"$1\"\n",
		"the block rewritten":    "sed -i 's/^ReARM-Agent: .*/ReARM-Agent: other/' \"$1\"\n",
		"a blank line inside it": "sed -i 's/^ReARM-Agent: /\\nReARM-Agent: /' \"$1\"\n",
	} {
		t.Run(name, func(t *testing.T) {
			w := newGitWorld(t)
			hookPath := filepath.Join(w.repo, ".git", "hooks", "commit-msg")
			if err := os.WriteFile(hookPath, []byte("#!/bin/sh\n"+hook), 0o755); err != nil {
				t.Fatal(err)
			}
			gitCoAuthor, gitMessages = gCoAuthor, []string{"feat: x"}
			w.write("a.txt", "a\n")
			before := w.head()
			code, _, errOut := w.commit("a.txt")
			if code != 1 || !strings.Contains(errOut, "is not the three lines") || !strings.Contains(errOut, "Amend it by hand") {
				t.Fatalf("exit %d\n%s", code, errOut)
			}
			if w.head() == before {
				t.Fatal("the commit should be left for the agent to amend")
			}
		})
	}
}

func TestGitCommitJson(t *testing.T) {
	w := newGitWorld(t)
	gitCoAuthor, gitMessages, gitJson = gCoAuthor, []string{"feat: json"}, true
	w.write("a.txt", "a\n")
	code, out, errOut := w.commit("a.txt")
	if code != 0 {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	var got struct {
		Committed      bool     `json:"committed"`
		Sha            string   `json:"sha"`
		Subject        string   `json:"subject"`
		Merge          bool     `json:"merge"`
		Commands       []string `json:"commands"`
		Trailers       []string `json:"trailers"`
		TrailerBlockOk bool     `json:"trailerBlockOk"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !got.Committed || got.Sha != w.head() || got.Subject != "feat: json" || got.Merge || !got.TrailerBlockOk ||
		len(got.Commands) != 2 || len(got.Trailers) != 3 || got.Trailers[0] != "ReARM-Agentic-Session: code-1" {
		t.Fatalf("json: %+v", got)
	}
}

// ---------- merge ----------

// branches: main moves on with m.txt after feature branched off with f.txt.
func (w *gitWorld) branches(conflict bool) {
	w.t.Helper()
	gRun(w.t, w.repo, "", "checkout", "-q", "-b", "feature")
	w.write("f.txt", "feature\n")
	if conflict {
		w.write("base.txt", "feature side\n")
	}
	gRun(w.t, w.repo, "", "add", ".")
	gRun(w.t, w.repo, "feature work\n", "commit", "-q", "-F", "-")
	gRun(w.t, w.repo, "", "checkout", "-q", "main")
	w.write("m.txt", "main\n")
	if conflict {
		w.write("base.txt", "main side\n")
	}
	gRun(w.t, w.repo, "", "add", ".")
	gRun(w.t, w.repo, "main work\n", "commit", "-q", "-F", "-")
	gRun(w.t, w.repo, "", "checkout", "-q", "feature")
}

func TestGitMergeCommitCarriesTheBlock(t *testing.T) {
	w := newGitWorld(t)
	w.branches(false)
	gitCoAuthor = gCoAuthor
	mainSha := gRun(t, w.repo, "", "rev-parse", "main")
	code, out, errOut := w.merge("main")
	if code != 0 {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	w.wantBlock("code-1", gAgent)
	subject := "Merge main (" + mainSha[:7] + ") into feature"
	if got := gRun(t, w.repo, "", "log", "-1", "--format=%s"); got != subject {
		t.Fatalf("subject %q, want %q", got, subject)
	}
	if parents := strings.Fields(gRun(t, w.repo, "", "log", "-1", "--format=%P")); len(parents) != 2 || parents[1] != mainSha {
		t.Fatalf("parents %v", parents)
	}
	if state, _ := w.signer(); state != "G" {
		t.Fatalf("merge commit signature %s", state)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 || lines[0] != "ran: git merge --no-ff --no-commit main" ||
		lines[1] != "ran: git commit -q -S --cleanup=verbatim -F -" || lines[2] != w.head()+" "+subject {
		t.Fatalf("printed:\n%s", out)
	}
}

func TestGitMergeConflictLeavesTheMergeInProgressAndCommitFinishesIt(t *testing.T) {
	w := newGitWorld(t)
	w.branches(true)
	gitCoAuthor = gCoAuthor
	before := w.head()
	mainSha := gRun(t, w.repo, "", "rev-parse", "main")
	code, out, errOut := w.merge("main")
	if code != 1 || !strings.Contains(errOut, "CONFLICT") || !strings.Contains(errOut, "left in progress") ||
		!strings.Contains(errOut, "rearm agent git commit --session "+gSession+" -m 'Merge main ("+mainSha[:7]+") into feature'") {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if !strings.Contains(out, "ran: git merge --no-ff --no-commit main") || w.head() != before || !mergeInProgress() {
		t.Fatal("the merge should be left in progress, uncommitted")
	}
	// The merge verb will not start another.
	if code, _, errOut := w.merge("main"); code != 1 || !strings.Contains(errOut, "already in progress") {
		t.Fatalf("second merge: exit %d\n%s", code, errOut)
	}
	// Resolve, stage, and finish with the commit helper (paths optional while a merge is in progress).
	w.write("base.txt", "resolved\n")
	gitMessages = []string{"Merge main (" + mainSha[:7] + ") into feature"}
	code, out, errOut = w.commit("base.txt")
	if code != 0 {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	w.wantBlock("code-1", gAgent)
	if parents := strings.Fields(gRun(t, w.repo, "", "log", "-1", "--format=%P")); len(parents) != 2 {
		t.Fatalf("parents %v", parents)
	}
	if strings.Contains(out, "-- base.txt") && strings.Contains(out, "commit -q -S --cleanup=verbatim -F - --") {
		t.Fatalf("a merge commit takes no pathspec:\n%s", out)
	}
}

func TestGitMergeUpToDate(t *testing.T) {
	w := newGitWorld(t)
	gRun(t, w.repo, "", "checkout", "-q", "-b", "feature")
	gitCoAuthor = gCoAuthor
	before := w.head()
	code, out, errOut := w.merge("main")
	if code != 0 || !strings.Contains(out, "Already up to date: main") || strings.Contains(out, "ran:") || w.head() != before {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	if code, _, errOut := w.merge("no-such-ref"); code != 1 || !strings.Contains(errOut, "names no commit") {
		t.Fatalf("unknown ref: exit %d\n%s", code, errOut)
	}
}

// ---------- the session read: a session on another instance ----------

type gitSessionServer struct {
	mu       sync.Mutex
	sessions map[string]map[string]any
	reads    int
}

func (s *gitSessionServer) serve(t *testing.T) *rearm.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		s.mu.Lock()
		defer s.mu.Unlock()
		if !strings.Contains(req.Query, "AgentGitSessionProgrammatic") {
			t.Errorf("unexpected operation %s", req.Query)
		}
		s.reads++
		sess, ok := s.sessions[str(req.Variables["sessionUuid"])]
		if !ok {
			_ = json.NewEncoder(w).Encode(map[string]any{"errors": []any{map[string]any{"message": "Session not found"}}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"sessionProgrammatic": sess}})
	}))
	t.Cleanup(srv.Close)
	c, err := rearm.New(srv.URL, "id", "secret", rearm.WithoutTokenExchange())
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// Two instances, two sessions: the code session's state (written by an older CLI) has no agent and is read from
// the code instance; the board session has no state on this host at all and is read from the board instance.
// Each read happens once and is kept, and each commit carries its own session's trailers.
func TestGitSessionReadFromTheInstanceItLivesOn(t *testing.T) {
	w := newGitWorld(t)
	if err := writeAgentState(&agentSessionState{SessionUuid: gSession2, ClientSessionId: "code-r1"}); err != nil {
		t.Fatal(err)
	}
	code := &gitSessionServer{sessions: map[string]map[string]any{
		gSession2: {"uuid": gSession2, "clientSessionId": "code-r1", "agent": gAgent}}}
	board := &gitSessionServer{sessions: map[string]map[string]any{
		gSession: {"uuid": gSession, "clientSessionId": "board-1", "agent": gAgent2}}}
	codeClient, boardClient := code.serve(t), board.serve(t)
	// gSession's state from newGitWorld is removed: this host never opened the board session.
	removeAgentState(lookupAgentState(gSession))

	apiClient, gitSession, gitCoAuthor, gitMessages = codeClient, gSession2, gCoAuthor, []string{"feat: code"}
	w.write("a.txt", "a\n")
	if c, out, errOut := w.commit("a.txt"); c != 0 {
		t.Fatalf("code session: exit %d\n%s\n%s", c, out, errOut)
	}
	w.wantBlock("code-r1", gAgent)

	apiClient, gitSession, gitMessages = boardClient, gSession, []string{"docs: board"}
	w.write("notes.md", "n\n")
	if c, out, errOut := w.commit("notes.md"); c != 0 {
		t.Fatalf("board session: exit %d\n%s\n%s", c, out, errOut)
	}
	w.wantBlock("board-1", gAgent2)

	// Kept: no further reads.
	for _, s := range []struct {
		client                     *rearm.Client
		session, file, csid, agent string
	}{
		{codeClient, gSession2, "b.txt", "code-r1", gAgent}, {boardClient, gSession, "c.txt", "board-1", gAgent2}} {
		apiClient, gitSession, gitMessages = s.client, s.session, []string{"feat: again"}
		w.write(s.file, s.file)
		if c, out, errOut := w.commit(s.file); c != 0 {
			t.Fatalf("%s again: exit %d\n%s\n%s", s.session, c, out, errOut)
		}
		w.wantBlock(s.csid, s.agent)
	}
	if code.reads != 1 || board.reads != 1 {
		t.Fatalf("reads: code %d, board %d; want one each", code.reads, board.reads)
	}
	if st := lookupAgentState(gSession2); st == nil || st.AgentUuid != gAgent {
		t.Fatalf("code session state %+v", st)
	}
}

// The wrong instance's credentials: the read fails, the refusal names the flags, nothing is written.
func TestGitSessionReadFailureRefuses(t *testing.T) {
	w := newGitWorld(t)
	removeAgentState(lookupAgentState(gSession))
	other := &gitSessionServer{sessions: map[string]map[string]any{}}
	apiClient, gitCoAuthor, gitMessages = other.serve(t), gCoAuthor, []string{"feat: x"}
	w.write("a.txt", "a\n")
	before := w.head()
	code, _, errOut := w.commit("a.txt")
	if code != 1 || !strings.Contains(errOut, "-u/-i/-k or --config") || !strings.Contains(errOut, "Session not found") {
		t.Fatalf("exit %d\n%s", code, errOut)
	}
	if w.head() != before || w.staged() != "" {
		t.Fatal("a refused commit wrote to the repository")
	}
	gitSession = "not-a-uuid"
	if code, _, errOut := w.commit("a.txt"); code != 1 || !strings.Contains(errOut, "only a session uuid can be read") {
		t.Fatalf("client id with no state: exit %d\n%s", code, errOut)
	}
}

// session init keeps the agent the server named, so a session opened here needs no read.
func TestSessionInitKeepsTheAgent(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	recordInitState(map[string]interface{}{"uuid": gSession, "clientSessionId": "init-1", "agent": gAgent})
	st := lookupAgentState(gSession)
	if st == nil || st.ClientSessionId != "init-1" || st.AgentUuid != gAgent {
		t.Fatalf("state %+v", st)
	}
}
