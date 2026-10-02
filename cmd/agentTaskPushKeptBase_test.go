package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// rearm agent task push keeps --base per checkout (task RD5-3 round 4, ARCHITECTURE round 3 §2, the round 3 note's
// rules on keeping it): two checkouts keep their own base at once (tester run 3, T-8); a --base that cannot be kept
// warns and the run still uses it (T-9), and a given --base wins over the kept one even then; a --base is kept as
// soon as it is given, so a run refused later still keeps it (T-10); a kept base is read as a branch name.

const notKeptWarning = "rearm: warning: --base main is used but not kept for this checkout: "

// runBoth runs the verb and returns its stdout, its stderr and its exit code.
func (w *pushWorld) runBoth() (string, string, int) {
	w.t.Helper()
	var out string
	code := -1
	errOut := stderrOf(w.t, func() { out = stdoutOf(w.t, func() { code = runTaskPush([]string{"RD-1"}) }) })
	return out, errOut, code
}

// Tester run 3, T-8: two checkouts of one repository (the clone and a worktree of it) each keep their own --base,
// at the same time. A --base given in the worktree does not replace the clone's; each later run without the flag
// uses its own checkout's.
func TestPushTwoCheckoutsKeepTheirOwnBase(t *testing.T) {
	w := newPushWorld(t)
	w.pr(0)["targetBranch"] = ""
	pushBase = "main"
	out, code := w.run()
	if code != 0 {
		t.Fatalf("clone, --base main: exit %d:\n%s", code, out)
	}
	wantIn(t, out, "base: main (from --base, kept for this checkout)\n", "ran: git push origin HEAD:refs/heads/feature\n")
	w.pr(0)["head"] = w.remote("feature")

	wt := filepath.Join(w.tmp, "wt")
	vGit(t, w.repo, "", "worktree", "add", "-q", "--detach", wt, "HEAD")
	wtTop := vGit(t, wt, "", "rev-parse", "--show-toplevel")
	t.Chdir(wt)
	pushBase = "other"
	out, code = w.run()
	if code != 0 {
		t.Fatalf("worktree, --base other: exit %d:\n%s", code, out)
	}
	wantIn(t, out, "base: other (from --base, kept for this checkout)\n", "already pushed: ")

	pushBase = ""
	out, code = w.run()
	if code != 0 {
		t.Fatalf("worktree, kept: exit %d:\n%s", code, out)
	}
	wantIn(t, out, "base: other (from the --base kept for this checkout)\n")

	t.Chdir(w.repo)
	out, code = w.run()
	if code != 0 {
		t.Fatalf("clone, kept, after the worktree kept its own: exit %d:\n%s", code, out)
	}
	wantIn(t, out, "base: main (from the --base kept for this checkout)\n")

	if a, b := keptPushBase(w.top()), keptPushBase(wtTop); a != "main" || b != "other" {
		t.Fatalf("kept: clone %q, worktree %q", a, b)
	}
	path, err := pushBaseStatePath(w.top())
	if err != nil {
		t.Fatal(err)
	}
	if files, _ := filepath.Glob(filepath.Join(filepath.Dir(path), "*.json")); len(files) != 2 {
		t.Fatalf("want one kept file per checkout, got %v", files)
	}
}

// Tester run 3, T-9: a --base that cannot be written to the state directory warns on stderr, the run uses the given
// base anyway and pushes, and the base line says it was not kept.
func TestPushABaseThatCannotBeKeptWarnsAndStillPushes(t *testing.T) {
	w := newPushWorld(t)
	w.pr(0)["targetBranch"] = ""
	blocker := filepath.Join(t.TempDir(), "state-is-a-file")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_STATE_HOME", blocker)
	pushBase = "main"
	out, errOut, code := w.runBoth()
	if code != 0 {
		t.Fatalf("exit %d:\n%s\nstderr:\n%s", code, out, errOut)
	}
	wantIn(t, errOut, notKeptWarning)
	wantIn(t, out, "base: main (from --base, not kept for this checkout: see the warning)\n", "ran: git push origin HEAD:refs/heads/feature\n")
	if w.remote("feature") != w.head() || keptPushBase(w.top()) != "" {
		t.Fatalf("not pushed, or kept anyway:\n%s", out)
	}
}

// The note's "it is read only when no --base is given": a given --base wins over the kept one, also when it cannot
// replace it. Here the kept base names the PR's branch (wrong), its directory is read-only, and --base main still
// pushes to the PR's branch; the wrong kept base stays as it was.
func TestPushAGivenBaseWinsOverTheKeptOneEvenWhenItCannotReplaceIt(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes a read-only directory")
	}
	w := newPushWorld(t)
	w.pr(0)["targetBranch"] = ""
	w.keepBase("feature")
	path, err := pushBaseStatePath(w.top())
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(path)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	pushBase = "main"
	out, errOut, code := w.runBoth()
	if code != 0 {
		t.Fatalf("exit %d:\n%s\nstderr:\n%s", code, out, errOut)
	}
	wantIn(t, errOut, notKeptWarning)
	wantIn(t, out, "base: main (from --base, not kept for this checkout: see the warning)\n", "branch feature, found by head sha")
	if w.remote("feature") != w.head() {
		t.Fatalf("not pushed:\n%s", out)
	}
	if got := keptPushBase(w.top()); got != "feature" {
		t.Fatalf("kept base changed to %q", got)
	}
}

// Tester run 3, T-10: --base is kept as soon as it is given, before the task is read, so a run refused or failed
// later keeps it: no linked PR for this repository (exit 1), and a task read that fails (exit 2). A later run without
// the flag then uses it.
func TestPushKeepsTheBaseOfARefusedRun(t *testing.T) {
	for _, c := range []struct {
		name    string
		breakIt func(w *pushWorld) func()
		code    int
		want    string
	}{
		{"no linked PR", func(w *pushWorld) func() {
			rows := w.srv.task["pullRequests"]
			w.srv.task["pullRequests"] = []any{map[string]any{"url": "https://github.com/acme/other/pull/3", "state": "OPEN"}}
			return func() { w.srv.task["pullRequests"] = rows }
		}, 1, "refused: no linked PR for github.com/acme/app on RD-1."},
		{"the task read fails", func(w *pushWorld) func() {
			w.srv.failTask = true
			return func() { w.srv.failTask = false }
		}, 2, "rearm: reading task RD-1: "},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := newPushWorld(t)
			w.pr(0)["targetBranch"] = ""
			restore := c.breakIt(w)
			pushBase = "main"
			out, errOut, code := w.runBoth()
			if code != c.code {
				t.Fatalf("exit %d, want %d:\n%s\nstderr:\n%s", code, c.code, out, errOut)
			}
			wantIn(t, out+errOut, c.want)
			if strings.Contains(out, "ran: ") {
				t.Fatalf("read origin:\n%s", out)
			}
			if got := keptPushBase(w.top()); got != "main" {
				t.Fatalf("a refused run kept %q, want main", got)
			}

			restore()
			pushBase = ""
			out, code = w.run()
			if code != 0 {
				t.Fatalf("next run: exit %d:\n%s", code, out)
			}
			wantIn(t, out, "base: main (from the --base kept for this checkout)\n", "ran: git push origin HEAD:refs/heads/feature\n")
		})
	}
}

// The round 3 note: refs/heads/ and origin/ are dropped from each base, the kept one included, so a kept file
// written by hand with a ref spelling reads as the branch.
func TestPushAKeptBaseIsReadAsABranchName(t *testing.T) {
	w := newPushWorld(t)
	path, err := pushBaseStatePath(w.top())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, spelled := range []string{"origin/main", "refs/heads/main"} {
		body, err := json.Marshal(pushBaseState{Path: w.top(), Base: spelled})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, body, 0o600); err != nil {
			t.Fatal(err)
		}
		if got := keptPushBase(w.top()); got != "main" {
			t.Fatalf("kept %q read as %q, want main", spelled, got)
		}
	}
}
