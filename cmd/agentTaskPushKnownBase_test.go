package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// rearm agent task push knows the PR's base before it reads origin (task RD5-3 round 3, ARCHITECTURE round 3): the
// base is the row's targetBranch, else --base, else the --base kept for this checkout, and targetBranch wins over a
// --base given or kept. With none known the push is refused before any remote read, so every choice is made with
// the base dropped from the candidates, and a lone candidate is the branch whatever the upstream (tester run 2,
// T-6 and T-7).

// keepBase records b as the checkout's kept --base, as an earlier task push --base b would have.
func (w *pushWorld) keepBase(b string) {
	w.t.Helper()
	if err := keepPushBase(w.top(), b); err != nil {
		w.t.Fatal(err)
	}
}

func (w *pushWorld) top() string {
	return vGit(w.t, w.repo, "", "rev-parse", "--show-toplevel")
}

// recordRemoteReads wraps pushGit and returns the git commands that talk to origin.
func (w *pushWorld) recordRemoteReads() *[]string {
	var seen []string
	real := pushGit
	pushGit = func(dir string, args ...string) (string, string, error) {
		switch args[0] {
		case "ls-remote", "push", "fetch", "pull", "remote":
			seen = append(seen, strings.Join(args, " "))
		}
		return real(dir, args...)
	}
	return &seen
}

// pullRefOnly makes the row unregistered as this board's are (no head, no targetBranch) and puts the PR's head at
// origin's refs/pull/7/head.
func (w *pushWorld) pullRefOnly() string {
	w.t.Helper()
	feature := w.pr(0)["head"].(string)
	vGit(w.t, w.repo, "", "push", "-q", "origin", feature+":refs/pull/7/head")
	w.pr(0)["head"], w.pr(0)["targetBranch"] = "", ""
	return feature
}

const noBaseRefusal = "refused: the PR row has no base branch (an unregistered PR), and no --base is given or kept for this checkout; task push reads nothing from origin until it knows the base, which it never pushes to. Remedy: pass --base <branch>, the PR's base branch; it is kept for this checkout\n"

// Round 3 §1: no targetBranch and no --base, given or kept: refused before any read of origin, --branch or not.
func TestPushRefusesAnUnregisteredRowBeforeReadingOrigin(t *testing.T) {
	for _, c := range []struct{ name, base, branch string }{
		{"plain", "", ""},
		{"--branch", "", "feature"},
		{"blank --base", "  ", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := newPushWorld(t)
			w.pr(0)["targetBranch"] = ""
			pushBase, pushBranch = c.base, c.branch
			reads := w.recordRemoteReads()
			before := map[string]string{"main": w.remote("main"), "feature": w.remote("feature")}
			out, code := w.run()
			if code != 1 {
				t.Fatalf("exit %d:\n%s", code, out)
			}
			if out != noBaseRefusal {
				t.Fatalf("printed:\n%s\nwant:\n%s", out, noBaseRefusal)
			}
			if len(*reads) != 0 {
				t.Fatalf("read origin without a base: %v", *reads)
			}
			w.pushedNothing(out, before)

			pushJson = true
			out, _ = w.run()
			var r pushResult
			if err := json.Unmarshal([]byte(out), &r); err != nil {
				t.Fatal(err)
			}
			if r.Ok || r.Outcome != "refused" || len(r.Commands) != 0 || r.Base != "" || len(*reads) != 0 {
				t.Fatalf("json: %+v, origin reads %v", r, *reads)
			}
		})
	}
}

// Tester run 2, T-7: the PR's branch deleted and the base fast-forwarded onto its head, so the base is the one
// branch at refs/pull/7/head. Without a base it is refused before origin is read; with --base it is no candidate,
// and the refusal asks for --branch, from a detached HEAD, a checkout named like the base, and a worktree on it.
func TestPushTheLoneBaseAtThePullHeadIsNeverPushedTo(t *testing.T) {
	for _, c := range []struct {
		name  string
		setup func(w *pushWorld)
		tail  string
	}{
		{"detached HEAD", func(w *pushWorld) { vGit(w.t, w.repo, "", "checkout", "-q", "--detach") },
			", and the current branch has no upstream on origin. Remedy: say --branch <the PR's head branch>\n"},
		{"checkout named like the base", func(w *pushWorld) {
			vGit(w.t, w.repo, "", "checkout", "-q", "-B", "main", "work")
			vGit(w.t, w.repo, "", "config", "branch.main.remote", "origin")
			vGit(w.t, w.repo, "", "config", "branch.main.merge", "refs/heads/main")
		}, "; the current branch's upstream is main, the PR's base, and task push never pushes to the base. Remedy: say --branch <the PR's head branch>\n"},
		{"worktree on the base", func(w *pushWorld) { w.upstream("main") },
			"; the current branch's upstream is main, the PR's base, and task push never pushes to the base. Remedy: say --branch <the PR's head branch>\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := newPushWorld(t)
			feature := w.pullRefOnly()
			vGit(t, w.repo, "", "push", "-q", "origin", feature+":refs/heads/main", ":refs/heads/feature")
			c.setup(w)
			reads := w.recordRemoteReads()
			before := map[string]string{"main": feature, "feature": ""}
			out, code := w.run()
			if code != 1 || out != noBaseRefusal || len(*reads) != 0 {
				t.Fatalf("no base: exit %d, origin reads %v:\n%s", code, *reads, out)
			}
			w.pushedNothing(out, before)

			pushBase = "main"
			out, code = w.run()
			if code != 1 {
				t.Fatalf("--base: exit %d:\n%s", code, out)
			}
			wantIn(t, out, "base: main (from --base, kept for this checkout)\nran: git ls-remote origin refs/heads/* refs/pull/7/head\n",
				"refused: no branch on origin carries "+pPR+"'s head "+shortSha(feature)+" other than its base main, which task push never pushes to"+c.tail)
			w.pushedNothing(out, before)
		})
	}
}

// Tester run 2, T-6 first rule, as round 3 leaves it: the base is known and dropped, so a lone candidate is the
// branch, also when it is the upstream of a local branch of another name (git worktree add -b work origin/feature)
// and from a detached HEAD, whichever source named the base. Round 2's lone-candidate guard is gone.
func TestPushALoneCandidateIsTheBranchWhateverTheUpstream(t *testing.T) {
	for _, c := range []struct {
		name, target, base, kept string
		detached                 bool
	}{
		{name: "targetBranch", target: "main"},
		{name: "--base", base: "main"},
		{name: "kept --base", kept: "main"},
		{name: "detached, kept --base", kept: "main", detached: true},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := newPushWorld(t)
			feature := w.pullRefOnly()
			// The base also sits at the PR's head, so a base dropped by the wrong name would leave a tie.
			vGit(t, w.repo, "", "push", "-q", "origin", feature+":refs/heads/main")
			w.pr(0)["targetBranch"], pushBase = c.target, c.base
			if c.kept != "" {
				w.keepBase(c.kept)
			}
			w.upstream("feature")
			if c.detached {
				vGit(t, w.repo, "", "checkout", "-q", "--detach")
			}
			out, code := w.run()
			if code != 0 {
				t.Fatalf("exit %d:\n%s", code, out)
			}
			wantIn(t, out, "ran: git push origin HEAD:refs/heads/feature\n", "branch feature, found by pull ref head sha")
			if w.remote("feature") != w.head() || w.remote("main") != feature {
				t.Fatalf("pushed to the wrong branch:\n%s", out)
			}
		})
	}
}

// Round 3 §2: given once, --base is kept for the checkout and used without the flag, from any directory in it; a
// later --base replaces it; another checkout of the same repository keeps its own (none here).
func TestPushKeepsTheBasePerCheckout(t *testing.T) {
	w := newPushWorld(t)
	w.pr(0)["targetBranch"] = ""
	pushBase = "main"
	out, code := w.run()
	if code != 0 {
		t.Fatalf("--base: exit %d:\n%s", code, out)
	}
	wantIn(t, out, "base: main (from --base, kept for this checkout)\n", "branch feature, found by head sha")
	if keptPushBase(w.top()) != "main" {
		t.Fatalf("not kept: %q", keptPushBase(w.top()))
	}

	// Without the flag, from a subdirectory: the kept base.
	pushBase = ""
	w.commit("again.txt")
	w.pr(0)["head"] = w.remote("feature")
	sub := filepath.Join(w.repo, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)
	out, code = w.run()
	if code != 0 {
		t.Fatalf("kept: exit %d:\n%s", code, out)
	}
	wantIn(t, out, "base: main (from the --base kept for this checkout)\n", "ran: git push origin HEAD:refs/heads/feature\n")
	if w.remote("feature") != w.head() {
		t.Fatal("not pushed with the kept base")
	}

	// Another checkout of the same repository (a worktree) keeps none: refused before origin is read.
	wt := filepath.Join(w.tmp, "wt")
	vGit(t, w.repo, "", "worktree", "add", "-q", "--detach", wt, "HEAD")
	t.Chdir(wt)
	reads := w.recordRemoteReads()
	out, code = w.run()
	if code != 1 || out != noBaseRefusal || len(*reads) != 0 {
		t.Fatalf("other checkout: exit %d, origin reads %v:\n%s", code, *reads, out)
	}

	// Back in the first checkout, a later --base replaces the kept one, and is then the kept one.
	t.Chdir(w.repo)
	w.commit("third.txt")
	w.pr(0)["head"] = w.remote("feature")
	pushBase = "feature"
	out, code = w.run()
	if code != 1 {
		t.Fatalf("replaced: exit %d:\n%s", code, out)
	}
	wantIn(t, out, "base: feature (from --base, kept for this checkout)\n", "other than its base feature, which task push never pushes to")
	pushBase = ""
	out, code = w.run()
	if code != 1 {
		t.Fatalf("replaced, kept: exit %d:\n%s", code, out)
	}
	wantIn(t, out, "base: feature (from the --base kept for this checkout)\n", "other than its base feature")
	if keptPushBase(w.top()) != "feature" {
		t.Fatalf("kept: %q", keptPushBase(w.top()))
	}
}

// Round 3 §2, and tester run 2's T-6 second rule as round 3 restates it: the row's targetBranch wins over a --base,
// kept or given. A wrong --base naming the PR's branch would drop it and refuse; targetBranch main pushes to it.
func TestPushTargetBranchWinsOverAKeptOrGivenBase(t *testing.T) {
	w := newPushWorld(t)
	w.keepBase("feature")
	out, code := w.run()
	if code != 0 {
		t.Fatalf("kept: exit %d:\n%s", code, out)
	}
	wantIn(t, out, "base: main (from the PR row's targetBranch, which wins over the --base feature)\n", "ran: git push origin HEAD:refs/heads/feature\n")
	if w.remote("feature") != w.head() {
		t.Fatal("not pushed")
	}

	// Given now, it is overridden the same way, and replaces the kept value.
	w.keepBase("other")
	w.commit("again.txt")
	w.pr(0)["head"] = w.remote("feature")
	pushBase, pushJson = "origin/feature", true
	out, code = w.run()
	if code != 0 {
		t.Fatalf("given: exit %d:\n%s", code, out)
	}
	var r pushResult
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatal(err)
	}
	if r.Base != "main" || r.BaseSource != "targetBranch" || r.BaseOverridden != "feature" || r.Branch != "feature" || !r.Ok {
		t.Fatalf("json: %+v", r)
	}
	if keptPushBase(w.top()) != "feature" {
		t.Fatalf("the given --base was not kept: %q", keptPushBase(w.top()))
	}

	// The same base as the row's: nothing is overridden.
	pushBase, pushJson = "main", false
	w.commit("third.txt")
	w.pr(0)["head"] = w.remote("feature")
	out, code = w.run()
	if code != 0 {
		t.Fatalf("same: exit %d:\n%s", code, out)
	}
	wantIn(t, out, "base: main (from the PR row's targetBranch)\n")
}

// A kept file that names another checkout (a digest collision, or a copied state directory) is no kept base.
func TestPushKeptBaseIsForItsOwnPath(t *testing.T) {
	w := newPushWorld(t)
	path, err := pushBaseStatePath(w.top())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"path":"/elsewhere","base":"main"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := keptPushBase(w.top()); got != "" {
		t.Fatalf("kept %q from another path", got)
	}
	w.keepBase("main")
	if got := keptPushBase(w.top()); got != "main" {
		t.Fatalf("kept %q", got)
	}
}
