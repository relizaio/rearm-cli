package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	rearm "github.com/relizaio/rearm-client-go"
)

// rearm agent task push (task RD5-3), against a temporary bare remote and a scripted client: a push that lands
// and verifies; the branch found by the row's head sha, from the upstream, and by --branch, in that order; the
// fast-forward refusal; already pushed; the merged, superseded and abandoned refusals; no linked PR; two linked
// PRs and --pr; the verification mismatch; --json; the printed commands; and no credential in anything printed.

const (
	pTask   = "66666666-6666-4666-8666-666666666666"
	pPR     = "https://github.com/acme/app/pull/7"
	pPR2    = "https://github.com/acme/app/pull/8"
	pSecret = "ghp_SECRET0123456789abcdef"
)

type pushWorld struct {
	t    *testing.T
	srv  *verifyServer
	repo string
	bare string
	tmp  string
}

// newPushWorld: acme/app's origin is a bare repository with main and a PR branch feature, one commit ahead of
// main and reported as PR 7's head; the checkout is on a local branch work (no upstream) at feature's tip, with
// one more commit to push. The task links PR 7, open.
func newPushWorld(t *testing.T) *pushWorld {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	for _, v := range []string{"GIT_TRACE", "GIT_TRACE_PACKET", "GIT_TRACE_CURL", "GIT_CURL_VERBOSE"} {
		t.Setenv(v, "")
	}
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("HOME", t.TempDir())
	tmp := t.TempDir()
	w := &pushWorld{t: t, tmp: tmp, bare: filepath.Join(tmp, "app.git"), repo: filepath.Join(tmp, "app")}
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
	w.commit("base.txt")
	vGit(t, w.repo, "", "push", "-q", "origin", "main")
	vGit(t, w.repo, "", "checkout", "-q", "-b", "work")
	head := w.commit("feature.txt")
	vGit(t, w.repo, "", "push", "-q", "origin", "HEAD:refs/heads/feature")
	w.commit("fix.txt")

	w.srv = &verifyServer{reports: map[string]map[string]any{}}
	w.srv.task = map[string]any{
		"uuid": pTask, "key": "RD-1", "board": "b-1", "role": "coder",
		"prUrls":       []any{pPR},
		"pullRequests": []any{map[string]any{"url": pPR, "state": "OPEN", "head": head}},
	}
	srv := w.srv.serve()
	t.Cleanup(srv.Close)
	c, err := rearm.New(srv.URL, "id", "secret", rearm.WithoutTokenExchange())
	if err != nil {
		t.Fatal(err)
	}
	apiClient = c
	prevLookup, prevGit := taskKeyLookup, pushGit
	taskKeyLookup = func(key string) (string, error) {
		if key == "RD-1" {
			return pTask, nil
		}
		return "", nil
	}
	pushSession = "s-1"
	t.Cleanup(func() {
		apiClient, taskKeyLookup, pushGit = nil, prevLookup, prevGit
		pushSession, pushPr, pushBranch, pushBase, pushJson = "", "", "", "", false
	})
	t.Chdir(w.repo)
	return w
}

// commit writes a file and commits it.
func (w *pushWorld) commit(file string) string {
	w.t.Helper()
	if err := os.WriteFile(filepath.Join(w.repo, file), []byte(file+w.t.Name()), 0o644); err != nil {
		w.t.Fatal(err)
	}
	vGit(w.t, w.repo, "", "add", file)
	vGit(w.t, w.repo, "", "commit", "-q", "-m", "add "+file)
	return vGit(w.t, w.repo, "", "rev-parse", "HEAD")
}

func (w *pushWorld) head() string { return vGit(w.t, w.repo, "", "rev-parse", "HEAD") }

// remote is the bare repository's tip of a branch, or "".
func (w *pushWorld) remote(branch string) string {
	w.t.Helper()
	out := vGit(w.t, w.bare, "", "for-each-ref", "--format=%(objectname)", "refs/heads/"+branch)
	return strings.TrimSpace(out)
}

func (w *pushWorld) pr(i int) map[string]any {
	return w.srv.task["pullRequests"].([]any)[i].(map[string]any)
}

func (w *pushWorld) run() (string, int) {
	w.t.Helper()
	code := -1
	out := stdoutOf(w.t, func() { code = runTaskPush([]string{"RD-1"}) })
	return out, code
}

func wantIn(t *testing.T, out string, parts ...string) {
	t.Helper()
	for _, p := range parts {
		if !strings.Contains(out, p) {
			t.Fatalf("want %q in:\n%s", p, out)
		}
	}
}

func TestPushLandsAndVerifies(t *testing.T) {
	w := newPushWorld(t)
	before, head := w.remote("feature"), w.head()
	out, code := w.run()
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	if got := w.remote("feature"); got != head {
		t.Fatalf("origin/feature is %s, want HEAD %s", got, head)
	}
	want := "ran: git ls-remote --heads origin\n" +
		"ran: git merge-base --is-ancestor " + before + " HEAD\n" +
		"ran: git push origin HEAD:refs/heads/feature\n" +
		"ran: git ls-remote origin refs/heads/feature\n" +
		"pushed " + head + " to " + pPR + " (branch feature, found by head sha): origin/feature was " + shortSha(before) +
		" and is now " + shortSha(head) + ", verified by ls-remote\n"
	if out != want {
		t.Fatalf("printed:\n%s\nwant:\n%s", out, want)
	}
	if w.remote("work") != "" {
		t.Fatal("the local branch name was pushed as a new branch")
	}
}

func TestPushNeverForces(t *testing.T) {
	w := newPushWorld(t)
	pushJson = true
	out, code := w.run()
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	var r pushResult
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatal(err)
	}
	for _, c := range r.Commands {
		if strings.Contains(c, "--force") || strings.Contains(c, " -f") || strings.Contains(c, " +") ||
			strings.Contains(c, "--mirror") || strings.Contains(c, "--delete") {
			t.Fatalf("a force or destructive flag in %q", c)
		}
	}
}

func TestPushFindsTheBranchByHeadShaOverTheUpstream(t *testing.T) {
	w := newPushWorld(t)
	// Another branch on origin, and an upstream naming it: the row's head sha still names feature.
	vGit(t, w.repo, "", "push", "-q", "origin", "main:refs/heads/other")
	vGit(t, w.repo, "", "config", "branch.work.remote", "origin")
	vGit(t, w.repo, "", "config", "branch.work.merge", "refs/heads/other")
	out, code := w.run()
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	wantIn(t, out, "branch feature, found by head sha")
	if w.remote("feature") != w.head() || w.remote("other") == w.head() {
		t.Fatalf("pushed to the wrong branch:\n%s", out)
	}
}

func TestPushUsesTheUpstreamWhenTheRowHasNoHead(t *testing.T) {
	w := newPushWorld(t)
	w.pr(0)["head"], w.pr(0)["targetBranch"] = "", "main"
	vGit(t, w.repo, "", "config", "branch.work.remote", "origin")
	vGit(t, w.repo, "", "config", "branch.work.merge", "refs/heads/feature")
	out, code := w.run()
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	wantIn(t, out, "branch feature, found by upstream")
	if w.remote("feature") != w.head() {
		t.Fatal("not pushed")
	}
}

func TestPushUsesTheUpstreamWhenTheHeadShaIsAmbiguous(t *testing.T) {
	w := newPushWorld(t)
	vGit(t, w.repo, "", "push", "-q", "origin", w.pr(0)["head"].(string)+":refs/heads/twin")
	w.pr(0)["targetBranch"] = "main"
	vGit(t, w.repo, "", "config", "branch.work.remote", "origin")
	vGit(t, w.repo, "", "config", "branch.work.merge", "refs/heads/twin")
	out, code := w.run()
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	wantIn(t, out, "branch twin, found by upstream")
	if w.remote("twin") != w.head() || w.remote("feature") == w.head() {
		t.Fatal("pushed to the wrong branch")
	}
}

// The worktree hazard: git worktree add -b <name> origin/<base> tracks the base. With no head on the row, origin's
// refs/pull/<n>/head names the PR's branch, and the upstream (the base) is not used.
func TestPushReadsThePullRefWhenTheRowHasNoHead(t *testing.T) {
	w := newPushWorld(t)
	feature, mainBefore := w.pr(0)["head"].(string), w.remote("main")
	vGit(t, w.repo, "", "push", "-q", "origin", feature+":refs/pull/7/head")
	w.pr(0)["head"] = ""
	vGit(t, w.repo, "", "config", "branch.work.remote", "origin")
	vGit(t, w.repo, "", "config", "branch.work.merge", "refs/heads/main")
	out, code := w.run()
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	wantIn(t, out, "ran: git ls-remote origin refs/heads/* refs/pull/7/head\n", "branch feature, found by pull ref head sha")
	if w.remote("feature") != w.head() || w.remote("main") != mainBefore {
		t.Fatalf("pushed to the wrong branch:\n%s", out)
	}
}

func TestPushNeverTakesTheBaseAsTheUpstream(t *testing.T) {
	w := newPushWorld(t)
	mainBefore := w.remote("main")
	w.pr(0)["head"], w.pr(0)["targetBranch"] = "", "main"
	vGit(t, w.repo, "", "config", "branch.work.remote", "origin")
	vGit(t, w.repo, "", "config", "branch.work.merge", "refs/heads/main")
	out, code := w.run()
	if code != 1 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	wantIn(t, out, "refused: "+pPR+" has no head on its row (CI never reported it), and origin names none for it; the current branch's upstream is main, the PR's base, and task push never pushes to the base.",
		"Remedy: say --branch")
	if w.remote("main") != mainBefore || strings.Contains(out, "ran: git push") {
		t.Fatalf("pushed to the base:\n%s", out)
	}
}

func TestPushTakesAnUpstreamOnlyFromTheBranchesAtTheHead(t *testing.T) {
	w := newPushWorld(t)
	vGit(t, w.repo, "", "push", "-q", "origin", w.pr(0)["head"].(string)+":refs/heads/twin")
	w.pr(0)["targetBranch"] = "main"
	vGit(t, w.repo, "", "config", "branch.work.remote", "origin")
	vGit(t, w.repo, "", "config", "branch.work.merge", "refs/heads/main")
	mainBefore := w.remote("main")
	out, code := w.run()
	if code != 1 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	wantIn(t, out, "is the tip of 2 branches on origin (feature, twin), and the current branch's upstream main is not one of them.")
	if w.remote("main") != mainBefore {
		t.Fatal("pushed to the upstream")
	}
}

func TestPushRefusesAnAmbiguousHeadWithoutAnUpstream(t *testing.T) {
	w := newPushWorld(t)
	vGit(t, w.repo, "", "push", "-q", "origin", w.pr(0)["head"].(string)+":refs/heads/twin")
	w.pr(0)["targetBranch"] = "main"
	before := w.remote("feature")
	out, code := w.run()
	if code != 1 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	wantIn(t, out, "refused: ", "is the tip of 2 branches on origin (feature, twin)", "Remedy: say --branch")
	if w.remote("feature") != before {
		t.Fatal("pushed anyway")
	}
}

func TestPushUpstreamMustBeOnOrigin(t *testing.T) {
	w := newPushWorld(t)
	w.pr(0)["head"], w.pr(0)["targetBranch"] = "", "main"
	vGit(t, w.repo, "", "remote", "add", "fork", w.bare)
	vGit(t, w.repo, "", "config", "branch.work.remote", "fork")
	vGit(t, w.repo, "", "config", "branch.work.merge", "refs/heads/feature")
	out, code := w.run()
	if code != 1 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	wantIn(t, out, "has no head on its row (CI never reported it), and origin names none for it, and the current branch has no upstream on origin.", "Remedy: say --branch")
}

func TestPushRefusesWithoutHeadOrUpstream(t *testing.T) {
	w := newPushWorld(t)
	w.pr(0)["head"] = ""
	out, code := w.run()
	if code != 1 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	wantIn(t, out, "refused: "+pPR+" has no head on its row", "Remedy: say --branch <the PR's head branch>")
	if strings.Contains(out, "git push") {
		t.Fatalf("pushed anyway:\n%s", out)
	}
}

// With no base known, the upstream could be the base: a head no branch carries is refused, asking for --base.
func TestPushRefusesAHeadNoBranchHasWithoutABase(t *testing.T) {
	w := newPushWorld(t)
	w.pr(0)["head"] = strings.Repeat("ab", 20)
	vGit(t, w.repo, "", "config", "branch.work.remote", "origin")
	vGit(t, w.repo, "", "config", "branch.work.merge", "refs/heads/feature")
	before := w.remote("feature")
	out, code := w.run()
	if code != 1 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	wantIn(t, out, "no branch on origin has "+pPR+"'s head abababa at its tip: the row is stale or the branch moved; the current branch's upstream is feature, but the PR's row names no base branch",
		"Remedy: pass --base <the PR's base branch> so task push can leave it out, or say --branch")
	if w.remote("feature") != before {
		t.Fatal("pushed anyway")
	}
}

func TestPushBranchFlagComesFirst(t *testing.T) {
	w := newPushWorld(t)
	vGit(t, w.repo, "", "push", "-q", "origin", "main:refs/heads/other")
	pushBranch = "origin/other"
	out, code := w.run()
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	wantIn(t, out, "branch other, found by --branch")
	if w.remote("other") != w.head() || w.remote("feature") == w.head() {
		t.Fatal("pushed to the wrong branch")
	}
}

func TestPushRefusesABranchOriginLacks(t *testing.T) {
	w := newPushWorld(t)
	pushBranch = "brand-new"
	out, code := w.run()
	if code != 1 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	wantIn(t, out, "origin has no branch brand-new")
	if w.remote("brand-new") != "" {
		t.Fatal("created a branch")
	}
}

func TestPushRefusesWhatWouldNotFastForward(t *testing.T) {
	w := newPushWorld(t)
	// Someone else pushed to feature from another clone; this checkout has fetched it but not merged it.
	other := filepath.Join(w.tmp, "other")
	vGit(t, w.tmp, "", "clone", "-q", "-b", "feature", w.bare, other)
	vGit(t, other, "", "-c", "user.email=o@example.com", "-c", "user.name=o", "commit", "-q", "--allow-empty", "-m", "theirs")
	vGit(t, other, "", "push", "-q", "origin", "feature")
	theirs := w.remote("feature")
	vGit(t, w.repo, "", "fetch", "-q", "origin")
	w.pr(0)["head"] = theirs
	out, code := w.run()
	if code != 1 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	wantIn(t, out, "refused: would not fast-forward: origin/feature is at "+shortSha(theirs)+", which HEAD does not contain.",
		"Remedy: merge origin/feature and retry", "never force")
	if strings.Contains(out, "ran: git push") {
		t.Fatalf("pushed:\n%s", out)
	}
	if w.remote("feature") != theirs {
		t.Fatal("origin/feature moved")
	}
	// Merged, it goes.
	vGit(t, w.repo, "", "merge", "-q", "--no-edit", "origin/feature")
	if out, code = w.run(); code != 0 {
		t.Fatalf("after the merge, exit %d:\n%s", code, out)
	}
	if w.remote("feature") != w.head() {
		t.Fatal("not pushed after the merge")
	}
}

func TestPushRefusesATipThisRepositoryLacks(t *testing.T) {
	w := newPushWorld(t)
	other := filepath.Join(w.tmp, "other")
	vGit(t, w.tmp, "", "clone", "-q", "-b", "feature", w.bare, other)
	vGit(t, other, "", "-c", "user.email=o@example.com", "-c", "user.name=o", "commit", "-q", "--allow-empty", "-m", "theirs")
	vGit(t, other, "", "push", "-q", "origin", "feature")
	theirs := w.remote("feature")
	w.pr(0)["head"] = theirs
	out, code := w.run()
	if code != 1 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	wantIn(t, out, "origin/feature is at "+shortSha(theirs)+", which is not in this repository", "git fetch origin feature")
	if w.remote("feature") != theirs {
		t.Fatal("origin/feature moved")
	}
}

func TestPushSaysAlreadyPushed(t *testing.T) {
	w := newPushWorld(t)
	vGit(t, w.repo, "", "push", "-q", "origin", "HEAD:refs/heads/feature")
	w.pr(0)["head"] = w.head()
	out, code := w.run()
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	want := "ran: git ls-remote --heads origin\nalready pushed: " + pPR + " (branch feature, found by head sha) is at HEAD " + w.head() + "\n"
	if out != want {
		t.Fatalf("printed:\n%s\nwant:\n%s", out, want)
	}
}

func TestPushRefusesAMergedOrReplacedPR(t *testing.T) {
	cases := []struct {
		name string
		set  func(pr map[string]any)
		want string
	}{
		{"merged date", func(pr map[string]any) { pr["mergedDate"] = "2026-10-01T00:00:00Z" }, pPR + " is merged"},
		{"merged state", func(pr map[string]any) { pr["state"] = "MERGED" }, pPR + " is merged"},
		{"superseded by", func(pr map[string]any) {
			pr["declaration"] = map[string]any{"outcome": "SUPERSEDED", "supersededBy": pPR2}
		}, pPR + " is declared superseded by " + pPR2},
		{"superseded", func(pr map[string]any) { pr["declaration"] = map[string]any{"outcome": "SUPERSEDED"} }, pPR + " is declared superseded"},
		{"abandoned", func(pr map[string]any) { pr["declaration"] = map[string]any{"outcome": "ABANDONED"} }, pPR + " is declared abandoned"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := newPushWorld(t)
			before := w.remote("feature")
			c.set(w.pr(0))
			out, code := w.run()
			if code != 1 {
				t.Fatalf("exit %d:\n%s", code, out)
			}
			wantIn(t, out, "refused: "+c.want, "no linked PR for github.com/acme/app is open to push to", "Remedy: a merged or replaced PR takes no more commits")
			if strings.Contains(out, "ran: ") || w.remote("feature") != before {
				t.Fatalf("ran git or pushed:\n%s", out)
			}
			// Named with --pr, it is refused by name too.
			pushPr = pPR
			out, code = w.run()
			if code != 1 {
				t.Fatalf("--pr: exit %d:\n%s", code, out)
			}
			wantIn(t, out, "refused: "+c.want+": its head branch is not pushed to.")
		})
	}
}

func TestPushAnOpenPRDeliveredDeclarationIsNotRefused(t *testing.T) {
	w := newPushWorld(t)
	w.pr(0)["declaration"] = map[string]any{"outcome": "DELIVERED"}
	if out, code := w.run(); code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
}

func TestPushRefusesWithoutALinkedPRForTheOrigin(t *testing.T) {
	w := newPushWorld(t)
	w.srv.task["pullRequests"] = []any{map[string]any{"url": "https://github.com/acme/other/pull/3", "head": w.head()}}
	out, code := w.run()
	if code != 1 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	wantIn(t, out, "refused: no linked PR for github.com/acme/app on RD-1.",
		"Remedy: open one and link it: rearm agent task linkpr RD-1 --pr-url <url>")

	w.srv.task["pullRequests"], w.srv.task["prUrls"] = []any{}, []any{}
	out, code = w.run()
	if code != 1 {
		t.Fatalf("no PRs: exit %d:\n%s", code, out)
	}
	wantIn(t, out, "refused: no linked PR for github.com/acme/app on RD-1.")
}

// secondPR links PR 8 of the same repository, open from branch second: a commit on HEAD's line past feature,
// pushed as second, and one more local commit after it.
func (w *pushWorld) secondPR() {
	w.t.Helper()
	w.commit("second.txt")
	vGit(w.t, w.repo, "", "push", "-q", "origin", "HEAD:refs/heads/second")
	w.commit("after-second.txt")
	w.srv.task["pullRequests"] = append(w.srv.task["pullRequests"].([]any),
		map[string]any{"url": pPR2, "state": "OPEN", "head": w.remote("second")})
}

func TestPushTwoLinkedPRsNeedPr(t *testing.T) {
	w := newPushWorld(t)
	w.secondPR()
	out, code := w.run()
	if code != 1 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	wantIn(t, out, "refused: 2 linked PRs for github.com/acme/app: "+pPR+", "+pPR2+".", "Remedy: say which with --pr <url>")
	if strings.Contains(out, "ran: ") {
		t.Fatalf("ran git:\n%s", out)
	}

	pushPr = pPR2 + "/"
	out, code = w.run()
	if code != 0 {
		t.Fatalf("--pr: exit %d:\n%s", code, out)
	}
	wantIn(t, out, "to "+pPR2+" (branch second, found by head sha)")
	if w.remote("second") != w.head() || w.remote("feature") == w.head() {
		t.Fatal("pushed to the wrong PR's branch")
	}

	pushPr = "https://github.com/acme/app/pull/99"
	out, code = w.run()
	if code != 1 {
		t.Fatalf("unlinked --pr: exit %d:\n%s", code, out)
	}
	wantIn(t, out, "--pr https://github.com/acme/app/pull/99 is not a PR linked to RD-1")
}

func TestPushTakesTheReplacementOverASupersededPR(t *testing.T) {
	w := newPushWorld(t)
	w.secondPR()
	w.pr(0)["declaration"] = map[string]any{"outcome": "SUPERSEDED", "supersededBy": pPR2}
	out, code := w.run()
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	wantIn(t, out, "to "+pPR2+" (branch second")
}

func TestPushFailsWhenTheRemoteHeadIsNotHead(t *testing.T) {
	w := newPushWorld(t)
	before, head := w.remote("feature"), w.head()
	real := pushGit
	pushGit = func(dir string, args ...string) (string, string, error) {
		if args[0] == "push" {
			return "", "", nil // reports success, lands nothing
		}
		return real(dir, args...)
	}
	out, code := w.run()
	if code != 1 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	wantIn(t, out, "ran: git ls-remote origin refs/heads/feature",
		"verification failed: pushed, but origin/feature is at "+before+", not HEAD "+head+".", "Remedy: someone else may have pushed")

	pushJson = true
	out, _ = w.run()
	var r pushResult
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatal(err)
	}
	if r.Ok || r.Outcome != "mismatch" || r.RemoteAfter != before || r.Head != head {
		t.Fatalf("json: %+v", r)
	}
}

func TestPushJsonShape(t *testing.T) {
	w := newPushWorld(t)
	before, head := w.remote("feature"), w.head()
	pushJson = true
	out, code := w.run()
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	want := map[string]any{"task": "RD-1", "ok": true, "outcome": "pushed", "pr": pPR, "branch": "feature",
		"branchSource": "head sha", "remoteBefore": before, "head": head, "remoteAfter": head}
	for k, v := range want {
		if m[k] != v {
			t.Fatalf("%s = %v, want %v\n%s", k, m[k], v, out)
		}
	}
	if cmds, _ := m["commands"].([]any); len(cmds) != 4 || cmds[2] != "git push origin HEAD:refs/heads/feature" {
		t.Fatalf("commands: %v", m["commands"])
	}

	// A refusal is JSON too.
	w.pr(0)["mergedDate"] = "2026-10-01T00:00:00Z"
	out, code = w.run()
	if code != 1 {
		t.Fatalf("exit %d", code)
	}
	var r pushResult
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatal(err)
	}
	if r.Ok || r.Outcome != "refused" || !strings.Contains(r.Reason, "is merged") || r.Remedy == "" {
		t.Fatalf("refusal: %+v", r)
	}
}

func TestPushNeedsSessionAndNoTracing(t *testing.T) {
	w := newPushWorld(t)
	pushSession = ""
	code := -1
	errOut := stderrOf(t, func() { _, code = w.run() })
	if code != 2 || !strings.Contains(errOut, "rearm: --session is required") {
		t.Fatalf("no session: exit %d, stderr %q", code, errOut)
	}
	pushSession = "s-1"
	before := w.remote("feature")
	t.Setenv("GIT_TRACE", "1")
	errOut = stderrOf(t, func() { _, code = w.run() })
	if code != 2 || !strings.Contains(errOut, "git tracing is on") {
		t.Fatalf("traced: exit %d, stderr %q", code, errOut)
	}
	t.Setenv("GIT_TRACE", "")
	if w.remote("feature") != before {
		t.Fatal("pushed with tracing on")
	}
}

func TestPushOutsideARepository(t *testing.T) {
	w := newPushWorld(t)
	t.Chdir(t.TempDir())
	if _, code := w.run(); code != 2 {
		t.Fatalf("exit %d", code)
	}
}

// ---------- credentials ----------

func TestRedactRemote(t *testing.T) {
	for _, in := range []string{
		"fatal: unable to access 'https://x-access-token:" + pSecret + "@github.com/acme/app.git/': 403",
		"To https://" + pSecret + "@github.com/acme/app.git",
		"remote: see http://user:" + pSecret + "@example.com/x",
		"ssh://git:" + pSecret + "@example.com/acme/app.git",
	} {
		got := redactRemote(in)
		if strings.Contains(got, pSecret) || !strings.Contains(got, "[REDACTED]@") {
			t.Fatalf("not redacted: %q -> %q", in, got)
		}
	}
	if got := redactRemote("git@github.com:acme/app.git rejected"); got != "git@github.com:acme/app.git rejected" {
		t.Fatalf("an scp remote without a secret changed: %q", got)
	}
}

// credentialedOrigin makes origin a URL carrying a token, rewritten to the bare repository.
func (w *pushWorld) credentialedOrigin() string {
	url := "https://x-access-token:" + pSecret + "@github.com/acme/app.git"
	vGit(w.t, w.repo, "", "remote", "set-url", "origin", url)
	vGit(w.t, w.repo, "", "config", "--unset", "url."+w.bare+".insteadOf")
	vGit(w.t, w.repo, "", "config", "url."+w.bare+".insteadOf", url)
	return url
}

func TestPushPrintsNoCredentialOfTheOrigin(t *testing.T) {
	w := newPushWorld(t)
	url := w.credentialedOrigin()

	// It lands, and nothing printed carries the token.
	out, code := w.run()
	if code != 0 || strings.Contains(out, pSecret) {
		t.Fatalf("exit %d, printed:\n%s", code, out)
	}

	// No linked PR: the origin is named without its credentials.
	w.srv.task["pullRequests"] = []any{map[string]any{"url": "https://github.com/acme/other/pull/3"}}
	out, code = w.run()
	if code != 1 || strings.Contains(out, pSecret) || !strings.Contains(out, "github.com/acme/app") {
		t.Fatalf("exit %d, printed:\n%s", code, out)
	}
	pushJson = true
	out, _ = w.run()
	if strings.Contains(out, pSecret) {
		t.Fatalf("json printed the token:\n%s", out)
	}
	pushJson = false
	w.srv.task["pullRequests"] = []any{map[string]any{"url": pPR, "state": "OPEN", "head": w.remote("feature")}}

	// The remote refuses the push and echoes the credentialed URL: printed redacted.
	w.commit("again.txt")
	hook := filepath.Join(w.bare, "hooks", "pre-receive")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\necho \"denied for "+url+"\" >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	out, code = w.run()
	if code != 1 {
		t.Fatalf("refused push: exit %d:\n%s", code, out)
	}
	wantIn(t, out, "refused: git push was refused: ", "denied for https://[REDACTED]@github.com/acme/app.git")
	if strings.Contains(out, pSecret) {
		t.Fatalf("printed the token:\n%s", out)
	}
}

func TestPushRedactsWhatAFailingGitEchoes(t *testing.T) {
	w := newPushWorld(t)
	real := pushGit
	leak := "fatal: unable to access 'https://" + pSecret + "@github.com/acme/app.git/': The requested URL returned error: 403"
	fail := ""
	pushGit = func(dir string, args ...string) (string, string, error) {
		if args[0] == fail || (fail == "ls-remote origin" && args[0] == "ls-remote" && args[1] == "origin") {
			return "", leak, os.ErrPermission
		}
		return real(dir, args...)
	}
	for i, step := range []string{"push", "ls-remote origin", "ls-remote"} {
		fail = step
		for _, j := range []bool{false, true} {
			pushJson = j
			// A new commit each time, the row at origin's tip: a push that landed before is not "already pushed".
			w.commit(fmt.Sprintf("leak-%d-%v.txt", i, j))
			w.pr(0)["head"] = w.remote("feature")
			var out string
			code := -1
			errOut := stderrOf(t, func() { out = stdoutOf(t, func() { code = runTaskPush([]string{"RD-1"}) }) })
			if code == 0 {
				t.Fatalf("%s: exit 0", step)
			}
			if strings.Contains(out+errOut, pSecret) || !strings.Contains(out+errOut, "https://[REDACTED]@github.com") {
				t.Fatalf("%s (json %v): printed:\n%s%s", step, j, out, errOut)
			}
		}
	}
}

// stderrOf runs f and returns what it wrote to stderr.
func stderrOf(t *testing.T, f func()) string {
	t.Helper()
	r, wr, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = wr
	done := make(chan string)
	go func() {
		b := make([]byte, 0, 4096)
		buf := make([]byte, 1024)
		for {
			n, err := r.Read(buf)
			b = append(b, buf[:n]...)
			if err != nil {
				break
			}
		}
		done <- string(b)
	}()
	f()
	wr.Close()
	os.Stderr = old
	return <-done
}
