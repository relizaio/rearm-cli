package cmd

import (
	"strings"
	"testing"
)

// rearm agent task push never pushes to the PR's base (task RD5-3 round 2, ARCHITECTURE round 2): it is never a
// candidate and --branch naming it is refused. Where the base comes from, and the refusal before origin is read
// when none is known (round 3), are in agentTaskPushKnownBase_test.go. Also the flag spellings the note promises (--pr in any case,
// --branch refs/heads/<name>) and the prUrls fallback, which no test covered in round 1 (tester run 1, T-3 to T-5).

// upstream sets the work branch's upstream to origin/<branch>, as git worktree add -b work origin/<branch> does.
func (w *pushWorld) upstream(branch string) {
	w.t.Helper()
	vGit(w.t, w.repo, "", "config", "branch.work.remote", "origin")
	vGit(w.t, w.repo, "", "config", "branch.work.merge", "refs/heads/"+branch)
}

// pushedNothing fails the test when the run pushed, or when any of the named branches moved.
func (w *pushWorld) pushedNothing(out string, before map[string]string) {
	w.t.Helper()
	if strings.Contains(out, "ran: git push") {
		w.t.Fatalf("pushed:\n%s", out)
	}
	for b, tip := range before {
		if got := w.remote(b); got != tip {
			w.t.Fatalf("origin/%s moved from %s to %s:\n%s", b, tip, got, out)
		}
	}
}

// Tester run 1, T-1: a registered row whose head is also its targetBranch's tip, the upstream on the base. The base
// is not a candidate, so the one candidate left, the PR's branch, takes the push.
func TestPushHeadAtTheTargetBranchTipGoesToThePRBranch(t *testing.T) {
	w := newPushWorld(t)
	feature := w.pr(0)["head"].(string)
	vGit(t, w.repo, "", "push", "-q", "origin", feature+":refs/heads/main")
	w.pr(0)["targetBranch"] = "main"
	w.upstream("main")
	out, code := w.run()
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	wantIn(t, out, "ran: git push origin HEAD:refs/heads/feature\n", "branch feature, found by head sha")
	if w.remote("feature") != w.head() || w.remote("main") != feature {
		t.Fatalf("pushed to the wrong branch:\n%s", out)
	}
}

// Tester run 1, T-2, this board's rows: unregistered (no head, no targetBranch), refs/pull/7/head is the PR's branch
// and the base's tip, the upstream is the base. Refused before origin is read (round 3); with --base, the PR's branch.
func TestPushUnregisteredRowWithThePullHeadAtTheBaseTip(t *testing.T) {
	w := newPushWorld(t)
	feature := w.pr(0)["head"].(string)
	vGit(t, w.repo, "", "push", "-q", "origin", feature+":refs/pull/7/head", feature+":refs/heads/main")
	w.pr(0)["head"], w.pr(0)["targetBranch"] = "", ""
	w.upstream("main")
	before := map[string]string{"main": feature, "feature": feature}
	out, code := w.run()
	if code != 1 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	wantIn(t, out, "refused: the PR row has no base branch (an unregistered PR)", "Remedy: pass --base <branch>")
	if strings.Contains(out, "ran: ") {
		t.Fatalf("read origin without a base:\n%s", out)
	}
	w.pushedNothing(out, before)

	pushBase = "origin/main"
	out, code = w.run()
	if code != 0 {
		t.Fatalf("--base: exit %d:\n%s", code, out)
	}
	wantIn(t, out, "ran: git push origin HEAD:refs/heads/feature\n", "branch feature, found by pull ref head sha")
	if w.remote("feature") != w.head() || w.remote("main") != feature {
		t.Fatalf("--base: pushed to the wrong branch:\n%s", out)
	}
}

// --branch naming the base is refused by name, whether the row or --base names it, in any spelling.
func TestPushBranchNamingTheBaseIsRefused(t *testing.T) {
	for _, c := range []struct{ name, target, base, branch string }{
		{"targetBranch", "main", "", "main"},
		{"targetBranch, refs/heads/", "refs/heads/main", "", "refs/heads/main"},
		{"--base", "", "main", "origin/main"},
		{"kept --base", "", "kept:main", "main"},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := newPushWorld(t)
			w.pr(0)["targetBranch"], pushBase, pushBranch = c.target, c.base, c.branch
			if kept, ok := strings.CutPrefix(c.base, "kept:"); ok {
				w.keepBase(kept)
				pushBase = ""
			}
			before := map[string]string{"main": w.remote("main"), "feature": w.remote("feature")}
			out, code := w.run()
			if code != 1 {
				t.Fatalf("exit %d:\n%s", code, out)
			}
			wantIn(t, out, "refused: --branch main is the PR's base branch, and task push never pushes to the base.", "Remedy: say --branch <the PR's head branch>")
			if strings.Contains(out, "ran: ") {
				t.Fatalf("read origin:\n%s", out)
			}
			w.pushedNothing(out, before)
		})
	}
}

// Several candidates and the base at the same commit: every candidate carries the base's tip, so the upstream does
// not break the tie, even when it is one of them.
func TestPushTieAtTheBaseTipIsRefusedEvenWithTheUpstreamAmongThem(t *testing.T) {
	w := newPushWorld(t)
	feature := w.pr(0)["head"].(string)
	vGit(t, w.repo, "", "push", "-q", "origin", feature+":refs/heads/main", feature+":refs/heads/twin")
	w.pr(0)["targetBranch"] = "main"
	w.upstream("twin")
	before := map[string]string{"main": feature, "feature": feature, "twin": feature}
	out, code := w.run()
	if code != 1 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	wantIn(t, out, "is the tip of 2 branches on origin (feature, twin), and the PR's base main is at the same commit, so none of them can be told from a copy of the base, and the current branch's upstream is twin.",
		"Remedy: say --branch")
	w.pushedNothing(out, before)
}

// No branch carries the row's head (the branch moved past it): the upstream, when the base is known and HEAD
// contains the upstream's tip.
func TestPushNoCandidateTakesAnUpstreamHeadContains(t *testing.T) {
	w := newPushWorld(t)
	w.pr(0)["head"], w.pr(0)["targetBranch"] = strings.Repeat("ab", 20), "main"
	w.upstream("feature")
	out, code := w.run()
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	wantIn(t, out, "branch feature, found by upstream")
	if w.remote("feature") != w.head() {
		t.Fatal("not pushed")
	}
}

// The same fallback, but HEAD does not contain the upstream's tip: it may not be the PR's branch, so the refusal
// asks for --branch first.
func TestPushNoCandidateUpstreamHeadLacksAsksForBranch(t *testing.T) {
	w := newPushWorld(t)
	vGit(t, w.repo, "", "checkout", "-q", "-b", "side", "main")
	w.commit("side.txt")
	vGit(t, w.repo, "", "push", "-q", "origin", "HEAD:refs/heads/side")
	vGit(t, w.repo, "", "checkout", "-q", "work")
	w.pr(0)["head"], w.pr(0)["targetBranch"] = strings.Repeat("ab", 20), "main"
	w.upstream("side")
	before := map[string]string{"side": w.remote("side"), "feature": w.remote("feature"), "main": w.remote("main")}
	out, code := w.run()
	if code != 1 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	wantIn(t, out, "would not fast-forward: origin/side is at "+shortSha(before["side"])+", which HEAD does not contain. The upstream was taken only because no branch on origin carries the PR's head, so it may not be the PR's branch.",
		"Remedy: say --branch <the PR's head branch>; merge origin/side and retry only if it is the PR's branch; never force")
	w.pushedNothing(out, before)
}

// No candidate, and the upstream is not the base by name but sits at the base's tip: refused.
func TestPushNoCandidateUpstreamAtTheBaseTipIsRefused(t *testing.T) {
	w := newPushWorld(t)
	vGit(t, w.repo, "", "push", "-q", "origin", "main:refs/heads/copy")
	w.pr(0)["head"], w.pr(0)["targetBranch"] = strings.Repeat("ab", 20), "main"
	w.upstream("copy")
	before := map[string]string{"copy": w.remote("copy"), "main": w.remote("main")}
	out, code := w.run()
	if code != 1 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	wantIn(t, out, "; the current branch's upstream copy is at the PR's base main's tip, so it cannot be told from a copy of the base.", "Remedy: say --branch")
	w.pushedNothing(out, before)
}

// The only branch at the head is the base (the PR's branch is gone): no candidate, and the upstream on the base is
// refused, so nothing goes to the base.
func TestPushOnlyTheBaseCarriesTheHead(t *testing.T) {
	w := newPushWorld(t)
	feature := w.pr(0)["head"].(string)
	vGit(t, w.repo, "", "push", "-q", "origin", feature+":refs/heads/main", ":refs/heads/feature")
	w.pr(0)["targetBranch"] = "main"
	w.upstream("main")
	out, code := w.run()
	if code != 1 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	wantIn(t, out, "refused: no branch on origin carries "+pPR+"'s head "+shortSha(feature)+" other than its base main, which task push never pushes to; the current branch's upstream is main, the PR's base, and task push never pushes to the base.",
		"Remedy: say --branch")
	w.pushedNothing(out, map[string]string{"main": feature})
}

// Tester run 1, T-3: --pr is matched without regard to case (note step 2).
func TestPushPrFlagIgnoresCase(t *testing.T) {
	w := newPushWorld(t)
	w.secondPR()
	pushPr = strings.ToUpper(pPR2)
	out, code := w.run()
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	wantIn(t, out, "to "+pPR2+" (branch second, found by head sha)")
	if w.remote("second") != w.head() || w.remote("feature") == w.head() {
		t.Fatal("pushed to the wrong PR's branch")
	}
}

// Tester run 1, T-4: --branch refs/heads/<name> names the branch <name> (note step 3).
func TestPushBranchFlagDropsRefsHeads(t *testing.T) {
	w := newPushWorld(t)
	vGit(t, w.repo, "", "push", "-q", "origin", "main:refs/heads/other")
	pushBranch = "refs/heads/other"
	out, code := w.run()
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	wantIn(t, out, "ran: git push origin HEAD:refs/heads/other\n", "branch other, found by --branch")
	if w.remote("other") != w.head() || w.remote("refs/heads/other") != "" {
		t.Fatal("pushed to the wrong branch")
	}
}

// Tester run 1, T-5: a task read with no pullRequests rows falls back to prUrls; the head then comes from the
// pull ref, as on a row CI never reported.
func TestPushFallsBackToPrUrls(t *testing.T) {
	w := newPushWorld(t)
	feature := w.pr(0)["head"].(string)
	vGit(t, w.repo, "", "push", "-q", "origin", feature+":refs/pull/7/head")
	w.srv.task["pullRequests"] = []any{}
	pushBase = "main" // a prUrls row names no base
	out, code := w.run()
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	wantIn(t, out, "ran: git ls-remote origin refs/heads/* refs/pull/7/head\n", "to "+pPR+" (branch feature, found by pull ref head sha)")
	if w.remote("feature") != w.head() {
		t.Fatal("not pushed")
	}
}
