package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// The --session fallback (task RD5-4): a verb without --session takes the current session recorded for this
// repository on the instance it calls, through the agent hook, and an explicit one (flag, environment) wins; each
// instance keeps its own entry and the other's is never used; a verb that needs a session and finds no entry is
// refused naming session current --set, and one that needs none runs as before; two repositories keep two sessions,
// a subdirectory is its repository, and outside a repository the working directory is the key; a verb whose current
// session the server says is closed clears it; every verb under rearm agent that takes --session is covered.

const (
	codeSession = "aaaaaaaa-1111-4111-8111-000000000077"
	boardSess   = "bbbbbbbb-1111-4111-8111-000000000088"
)

// setCurrent records an entry directly, as open or --set would.
func (w *sessWorld) setCurrent(repo string, s *sessServer, uuid, clientId string) {
	w.t.Helper()
	if err := recordCurrentSession(repo, currentSessionEntry{SessionUuid: uuid, ClientSessionId: clientId, AgentUuid: sAgent,
		Instance: instanceKey(s.url), RecordedBy: "session open"}); err != nil {
		w.t.Fatal(err)
	}
}

// hook parses the words and runs the agent command's real pre-run (configuration, then the fallback); a refusal
// exits, so callers use it only where the fallback finds an entry or needs none.
func (w *sessWorld) hook(c *cobra.Command, words ...string) string {
	w.t.Helper()
	resetFlags(c)
	if err := c.ParseFlags(words); err != nil {
		w.t.Fatal(err)
	}
	agentCmd.PersistentPreRun(c, c.Flags().Args())
	return c.Flags().Lookup("session").Value.String()
}

func TestVerbWithoutSessionUsesTheCurrentSession(t *testing.T) {
	w := newSessWorld(t)
	w.on(w.b)
	w.b.addSession(boardSess, "scully-coder-1", sAgent, "OPEN")
	w.setCurrent(w.repoA, w.b, boardSess, "scully-coder-1")
	// Through the hook cobra runs, then the verb itself: the server is sent the current session.
	if got := w.hook(agentTaskAssignCmd, "t-1"); got != boardSess {
		t.Fatalf("the hook gave --session %q", got)
	}
	stdoutOf(t, func() { agentTaskAssignCmd.Run(agentTaskAssignCmd, []string{"t-1"}) })
	if got := w.b.last("AgentTaskAssignProgrammatic").vars["sessionUuid"]; got != boardSess {
		t.Fatalf("assign sent %v", got)
	}
}

// An explicit --session wins, and so does one the environment gives (the configuration is loaded before the
// fallback runs); --session="" is explicit too, and the verb refuses it as before.
func TestExplicitSessionWins(t *testing.T) {
	w := newSessWorld(t)
	w.on(w.b)
	w.setCurrent(w.repoA, w.b, boardSess, "scully-coder-1")
	if got := w.hook(agentTaskAssignCmd, "t-1", "--session", otherSession); got != otherSession {
		t.Fatalf("the flag lost: %q", got)
	}
	t.Setenv("REARM_SESSION", otherSession)
	if got := w.hook(agentTaskAssignCmd, "t-1"); got != otherSession {
		t.Fatalf("REARM_SESSION lost to the current session: %q", got)
	}
	os.Unsetenv("REARM_SESSION")
	if got := w.hook(agentTaskAssignCmd, "t-1", "--session="); got != "" {
		t.Fatalf("an explicit empty --session was replaced: %q", got)
	}
}

func TestTwoRepositoriesKeepTwoSessions(t *testing.T) {
	w := newSessWorld(t)
	w.on(w.b)
	w.setCurrent(w.repoA, w.b, boardSess, "s-a")
	w.setCurrent(w.repoB, w.b, otherSession, "s-b")
	if got := w.hook(agentTaskAssignCmd, "t-1"); got != boardSess {
		t.Fatalf("repository A: %q", got)
	}
	t.Chdir(w.repoB)
	if got := w.hook(agentTaskAssignCmd, "t-1"); got != otherSession {
		t.Fatalf("repository B: %q", got)
	}
}

// The repository is git's top level, so a subdirectory finds its entry; outside a repository the working directory
// is the key, and its subdirectory is another key.
func TestTheRepositoryKey(t *testing.T) {
	w := newSessWorld(t)
	w.on(w.b)
	w.setCurrent(w.repoA, w.b, boardSess, "s-a")
	sub := filepath.Join(w.repoA, "cmd", "deep")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)
	if got := w.hook(agentTaskAssignCmd, "t-1"); got != boardSess {
		t.Fatalf("a subdirectory is its repository: %q", got)
	}
	t.Chdir(w.plain)
	key, err := repositoryKey()
	if real, _ := filepath.EvalSymlinks(w.plain); err != nil || key != real {
		t.Fatalf("outside a repository the key is the working directory: %q %v", key, err)
	}
	w.setCurrent(key, w.b, otherSession, "s-plain")
	if got := w.hook(agentTaskAssignCmd, "t-1"); got != otherSession {
		t.Fatalf("outside a repository: %q", got)
	}
	inner := filepath.Join(w.plain, "inner")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(inner)
	if code := applyFallbackQuietly(t, agentTaskAssignCmd, "t-1"); code != 1 {
		t.Fatalf("another directory outside a repository has no entry: exit %d", code)
	}
}

// applyFallbackQuietly parses and runs the fallback alone, returning its exit code.
func applyFallbackQuietly(t *testing.T, c *cobra.Command, words ...string) int {
	t.Helper()
	resetFlags(c)
	if err := c.ParseFlags(words); err != nil {
		t.Fatal(err)
	}
	code := 0
	stderrOf(t, func() { code = applySessionFallback(c) })
	return code
}

// Keyed by repository and instance: each instance's entry answers its own verbs, and a verb on an instance with no
// entry is refused naming session current --set, never given the other instance's session.
func TestEachInstanceKeepsItsOwnEntry(t *testing.T) {
	w := newSessWorld(t)
	w.setCurrent(w.repoA, w.a, codeSession, "code-r2")
	w.on(w.b)
	resetFlags(agentTaskAssignCmd)
	_ = agentTaskAssignCmd.ParseFlags([]string{"t-1"})
	var code int
	errOut := stderrOf(t, func() { code = applySessionFallback(agentTaskAssignCmd) })
	if code != 1 {
		t.Fatalf("exit %d, want 1: %s", code, errOut)
	}
	for _, want := range []string{"--session is required", "no current session is recorded for " + w.repoA + " on " + instanceKey(w.b.url),
		"rearm agent session current --set <session-uuid>", "the entry on " + instanceKey(w.a.url) + " is that instance's and is not used here"} {
		if !strings.Contains(errOut, want) {
			t.Fatalf("want %q in: %s", want, errOut)
		}
	}
	if got := agentTaskAssignCmd.Flags().Lookup("session").Value.String(); got != "" {
		t.Fatalf("the other instance's session was used: %q", got)
	}
	w.setCurrent(w.repoA, w.b, boardSess, "scully-coder-1")
	if got := w.hook(agentTaskAssignCmd, "t-1"); got != boardSess {
		t.Fatalf("instance B: %q", got)
	}
	w.on(w.a)
	if got := w.hook(agentTaskAssignCmd, "t-1"); got != codeSession {
		t.Fatalf("instance A: %q", got)
	}
}

// The two-instance worktree: a code-side verb (agent git commit) resolves to the code session on the controlling
// instance and writes its client id as the trailer; a board verb resolves to the board session on the board's.
func TestCodeVerbAndBoardVerbResolveToTheirOwnInstance(t *testing.T) {
	w := newSessWorld(t)
	w.on(w.a)
	out, _, code := w.open("--client-session-id", "code-r2-1")
	if code != 0 {
		t.Fatal("open")
	}
	code1 := envOf(t, out)["REARM_SESSION"]
	w.on(w.b)
	w.b.addSession(boardSess, "scully-coder-1", "2ffebe1a-ee9c-41d9-b052-88462ea33d81", "OPEN")
	if _, errOut, c := w.run(agentSessionCurrentCmd, "--set", boardSess); c != 0 {
		t.Fatal(errOut)
	}

	if got := w.hook(agentTaskAssignCmd, "t-1"); got != boardSess {
		t.Fatalf("the board verb: %q", got)
	}
	w.on(w.a)
	if got := w.hook(agentGitCommitCmd, "-m", "x", "--no-co-author"); got != code1 {
		t.Fatalf("the git helper: %q", got)
	}
	gitNoCoAuthor = true
	t.Cleanup(func() { gitSession, gitNoCoAuthor = "", false })
	run, why := resolveGitIdentity()
	if why != "" || run.trailers.session != "code-r2-1" || run.trailers.agent != sAgent {
		t.Fatalf("the trailers: %+v %s", run, why)
	}
}

// A verb whose --session is optional runs without one when nothing is recorded, as before.
func TestVerbThatNeedsNoSessionRunsWithoutAnEntry(t *testing.T) {
	w := newSessWorld(t)
	w.on(w.b)
	if code := applyFallbackQuietly(t, agentTaskShowCmd, "t-1"); code != 0 {
		t.Fatalf("task show without an entry: exit %d", code)
	}
	if got := agentTaskShowCmd.Flags().Lookup("session").Value.String(); got != "" {
		t.Fatalf("set to %q", got)
	}
	w.setCurrent(w.repoA, w.b, boardSess, "s")
	if got := w.hook(agentTaskShowCmd, "t-1"); got != boardSess {
		t.Fatalf("task show with an entry acknowledges for the current session: %q", got)
	}
}

// task commission without --session commissions as a person, so the current session is not put in its place; agent
// release show with --client-session-id has its session.
func TestFallbackLeavesVerbsWhoseMissingSessionMeansSomething(t *testing.T) {
	w := newSessWorld(t)
	w.on(w.b)
	w.setCurrent(w.repoA, w.b, boardSess, "s")
	if got := w.hook(agentTaskCommissionCmd, "--board", "b"); got != "" {
		t.Fatalf("commission was given %q", got)
	}
	if got := w.hook(agentReleaseShowCmd, "r-1", "--client-session-id", "s"); got != "" {
		t.Fatalf("release show with --client-session-id was given %q", got)
	}
	if got := w.hook(agentReleaseShowCmd, "r-1"); got != boardSess {
		t.Fatalf("release show without either: %q", got)
	}
}

// Every runnable verb under rearm agent that takes --session is covered: with an entry the fallback fills it (bar the
// verbs that mean something without one), and without one every verb that needs a session refuses, with the exit
// code it refuses with today. No verb replaces the agent hook with its own.
func TestEveryVerbWithSessionIsCoveredByTheFallback(t *testing.T) {
	w := newSessWorld(t)
	w.on(w.b)
	needs := map[string]int{}
	var all []*cobra.Command
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		if c != agentCmd && (c.PersistentPreRun != nil || c.PersistentPreRunE != nil) {
			t.Errorf("%s has its own PersistentPreRun, which replaces the agent hook", c.CommandPath())
		}
		if c.Runnable() && c.Flags().Lookup("session") != nil || c.Runnable() && c.PersistentFlags().Lookup("session") != nil {
			all = append(all, c)
		}
		for _, s := range c.Commands() {
			walk(s)
		}
	}
	walk(agentCmd)
	if len(all) < 40 {
		t.Fatalf("found %d verbs with --session; expected the board's full set", len(all))
	}
	for _, c := range all {
		code := applyFallbackQuietly(t, c)
		f := c.Flags().Lookup("session")
		if f.Value.String() != "" {
			t.Errorf("%s: set without an entry", c.CommandPath())
		}
		if code != 0 {
			needs[c.CommandPath()] = code
		}
	}
	for path, want := range map[string]int{
		"rearm agent task verify": 2, "rearm agent doc publish": 1, "rearm agent doc element-check": 1, "rearm agent task brief": 1,
		"rearm agent task push": 1, "rearm agent git commit": 1, "rearm agent git merge": 1, "rearm agent wait": 1,
		"rearm agent notes append": 1, "rearm agent task signoff": 1, "rearm agent task assign": 1, "rearm agent task next": 1,
		"rearm agent task return": 1,
	} {
		if needs[path] != want {
			t.Errorf("%s: without an entry exit %d, want %d", path, needs[path], want)
		}
	}
	for _, path := range []string{"rearm agent task show", "rearm agent notes tail", "rearm agent task commission",
		"rearm agent task register", "rearm agent release show"} {
		if _, refused := needs[path]; refused {
			t.Errorf("%s runs without a session, but the fallback refused it", path)
		}
	}

	w.setCurrent(w.repoA, w.b, boardSess, "s")
	for _, c := range all {
		applyFallbackQuietly(t, c)
		got := c.Flags().Lookup("session").Value.String()
		_, skip := sessionFallbackSkip[c.CommandPath()]
		if skip && got != "" || !skip && got != boardSess {
			t.Errorf("%s: with an entry got %q", c.CommandPath(), got)
		}
	}
	for _, c := range all {
		resetFlags(c)
	}
}

// A verb that fell back to a session the server says is closed clears it and says so; another error clears nothing.
func TestClosedCurrentSessionIsClearedOnTheNextVerb(t *testing.T) {
	w := newSessWorld(t)
	w.on(w.b)
	w.b.addSession(boardSess, "s", sAgent, "OPEN")
	w.setCurrent(w.repoA, w.b, boardSess, "s")
	w.setCurrent(w.repoB, w.b, boardSess, "s")
	w.hook(agentTaskAssignCmd, "t-1")
	// Another refusal is not a closed session.
	w.b.mu.Lock()
	delete(w.b.sessions, boardSess)
	w.b.sessions[otherSession] = map[string]any{"uuid": otherSession, "status": "CLOSED"}
	w.b.mu.Unlock()
	errOut := stderrOf(t, func() {
		_, _ = sendGraphQLRequest(rearmAssignOp(), map[string]interface{}{"taskUuid": "t-1", "sessionUuid": otherSession})
	})
	if entryOf(t, w.repoA, w.b.url) == nil || strings.Contains(errOut, "no longer") {
		t.Fatalf("a refusal naming another session cleared the current one: %s", errOut)
	}
	// A refusal naming the current session that is not about it being closed clears nothing either.
	w.b.addSession(boardSess, "s", sAgent, "OPEN")
	errOut = stderrOf(t, func() {
		_, _ = sendGraphQLRequest(rearmAssignOp(), map[string]interface{}{"taskUuid": "t-held", "sessionUuid": boardSess})
	})
	if entryOf(t, w.repoA, w.b.url) == nil || strings.Contains(errOut, "no longer") {
		t.Fatalf("a refusal that is not a closed session cleared the current one: %s", errOut)
	}
	w.b.addSession(boardSess, "s", sAgent, "CLOSED")
	errOut = stderrOf(t, func() {
		_, err := sendGraphQLRequest(rearmAssignOp(), map[string]interface{}{"taskUuid": "t-1", "sessionUuid": boardSess})
		if err == nil {
			t.Error("the closed session was accepted")
		}
	})
	if !strings.Contains(errOut, "the current session "+boardSess) || !strings.Contains(errOut, "is closed on the server") ||
		!strings.Contains(errOut, "session current --set") {
		t.Fatalf("says so: %s", errOut)
	}
	if entryOf(t, w.repoA, w.b.url) != nil || entryOf(t, w.repoB, w.b.url) != nil {
		t.Fatalf("the closed session is cleared everywhere")
	}
}

func rearmAssignOp() string {
	return `mutation AgentTaskAssignProgrammatic($taskUuid: ID!, $sessionUuid: ID!) { agentTaskAssignProgrammatic(taskUuid: $taskUuid, sessionUuid: $sessionUuid) { assignedAt } }`
}

func TestInstanceKey(t *testing.T) {
	for in, want := range map[string]string{
		"https://agent-scully.rearmhq.com":           "https://agent-scully.rearmhq.com",
		"https://Agent-Scully.rearmhq.com/":          "https://agent-scully.rearmhq.com",
		"https://agent-scully.rearmhq.com:443/api":   "https://agent-scully.rearmhq.com",
		"agent-scully.rearmhq.com":                   "https://agent-scully.rearmhq.com",
		"HTTP://localhost:80":                        "http://localhost",
		"http://127.0.0.1:8086":                      "http://127.0.0.1:8086",
		"https://reliza.rearmhq.com":                 "https://reliza.rearmhq.com",
		"  https://reliza.rearmhq.com/graphql?x=1  ": "https://reliza.rearmhq.com",
		"":                                "",
		"https://reliza.rearmhq.com:8443": "https://reliza.rearmhq.com:8443",
		"http://agent-scully.rearmhq.com": "http://agent-scully.rearmhq.com",
		"https://agent-scully.rearmhq.com.evil.example": "https://agent-scully.rearmhq.com.evil.example",
	} {
		if got := instanceKey(in); got != want {
			t.Errorf("instanceKey(%q) = %q, want %q", in, got, want)
		}
	}
}
