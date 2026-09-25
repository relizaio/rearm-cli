package cmd

import (
	"strings"
	"testing"

	rearm "github.com/relizaio/rearm-client-go"
)

const fullA = "aaaaaaa1111111111111111111111111111111aa"
const fullB = "bbbbbbb2222222222222222222222222222222bb"

func pr(url, state string, registered bool, head string) map[string]interface{} {
	m := map[string]interface{}{"url": url, "registered": registered}
	if state != "" {
		m["state"] = state
	}
	if head != "" {
		m["head"] = head
	}
	return m
}

// The merge plan says, per linked PR, whether it can merge at the tested head and how (task 3b97ccfd).
func TestMergePlan(t *testing.T) {
	gh := "https://github.com/relizaio/rearm/pull/396"
	gl := "https://gitlab.example.com/group/sub/app/-/merge_requests/12"
	moved := "https://github.com/relizaio/rearm/pull/397"
	untested := "https://github.com/relizaio/rearm/pull/398"
	merged := "https://github.com/relizaio/rearm/pull/399"
	task := map[string]interface{}{
		"testedHeads": []interface{}{
			map[string]interface{}{"pr": gh + "/", "head": "aaaaaaa1"},
			map[string]interface{}{"pr": gl, "head": fullB},
			map[string]interface{}{"pr": moved, "head": "aaaaaaa1"},
			map[string]interface{}{"pr": merged, "head": fullA},
		},
		"pullRequests": []interface{}{
			pr(gh, "OPEN", true, fullA),
			pr(gl, "", false, ""),
			pr(moved, "OPEN", true, fullB),
			pr(untested, "OPEN", true, fullA),
			pr(merged, "MERGED", true, fullA),
		},
	}
	steps := mergePlan(task)
	if len(steps) != 5 {
		t.Fatalf("one step per linked PR: %+v", steps)
	}
	if s := steps[0]; s.Status != "ready" || s.Command != "gh pr merge "+gh+" --merge --match-head-commit "+fullA {
		t.Errorf("GitHub merges at the PR's full head when it is the tested one: %+v", s)
	}
	if s := steps[1]; s.Status != "ready" ||
		s.Command != "glab api --hostname gitlab.example.com --method PUT projects/group%2Fsub%2Fapp/merge_requests/12/merge -f sha="+fullB {
		t.Errorf("GitLab merges through the API with sha: %+v", s)
	}
	if s := steps[2]; s.Status != "moved" || s.Command != "" {
		t.Errorf("a PR past its tested head gets no command: %+v", s)
	}
	if s := steps[3]; s.Status != "untested" || s.Command != "" || !strings.Contains(s.Note, "do not merge") {
		t.Errorf("a PR no round names is not merged: %+v", s)
	}
	if s := steps[4]; s.Status != "merged" {
		t.Errorf("a merged PR says so: %+v", s)
	}
	if mergeCommand("https://example.com/whatever", fullA) != "" {
		t.Error("neither GitHub nor GitLab: no command")
	}
	if mergeCommand("https://gitlab.com/g/app/-/merge_requests/3", fullA) != "glab api --method PUT projects/g%2Fapp/merge_requests/3/merge -f sha="+fullA {
		t.Error("gitlab.com needs no hostname")
	}
	if agentTaskMergeplanCmd.Flags().Lookup("json") == nil {
		t.Error("mergeplan takes --json")
	}
	if len(mergePlan(map[string]interface{}{})) != 0 {
		t.Error("a task with no PRs has an empty plan")
	}
}

// A short tested head with no head seen on the PR is used as it is, with a warning.
func TestMergePlanShortHeadUnseen(t *testing.T) {
	u := "https://github.com/relizaio/rearm/pull/1"
	steps := mergePlan(map[string]interface{}{
		"testedHeads":  []interface{}{map[string]interface{}{"pr": u, "head": "aaaaaaa1"}},
		"pullRequests": []interface{}{pr(u, "", false, "")},
	})
	if steps[0].Status != "ready" || !strings.HasSuffix(steps[0].Command, "--match-head-commit aaaaaaa1") ||
		!strings.Contains(steps[0].Note, "full sha") {
		t.Errorf("%+v", steps[0])
	}
}

// The read the plan and task show use carries both heads; without them every PR reads untested.
func TestTheTaskReadSelectsTheHeads(t *testing.T) {
	op := strings.Join(strings.Fields(rearm.AgentTaskProgrammatic_Operation), " ")
	// "registered head " rather than "registered head }": the PR selection goes on to the
	// attestation since client-go#43 (task 18c5c293).
	for _, want := range []string{"testedHeads { pr head }", "registered head "} {
		if !strings.Contains(op, want) {
			t.Errorf("AgentTaskProgrammatic lacks %q; is rearm-client-go pinned at #40 or later?", want)
		}
	}
}

// The attest line (task 18c5c293): after a ready PR's merge command when the board requires it or
// cannot see the merge, runnable as printed but for the merge sha; never on a PR that is not ready.
func TestMergePlanAttestLines(t *testing.T) {
	steps := []mergeStep{
		{PR: "https://github.com/o/r/pull/1", Registered: false, Status: "ready", at: fullA},
		{PR: "https://github.com/o/r/pull/2", Registered: true, Status: "ready", at: fullA},
		{PR: "https://github.com/o/r/pull/3", Registered: false, Status: "moved"},
	}
	cp := func() []mergeStep { return append([]mergeStep(nil), steps...) }
	rows := withProcedure(cp(), "t-1", procedureOf(map[string]interface{}{"mode": "PR_ROWS"}), true, "")
	want := "rearm agent task delivered t-1 --session <seat-session> --unit https://github.com/o/r/pull/1 --commit <merge sha>"
	if rows[0].Attest != want {
		t.Errorf("an unregistered ready PR is attested on any board: %q", rows[0].Attest)
	}
	if rows[1].Attest != "" || rows[2].Attest != "" {
		t.Errorf("a registered PR on a PR_ROWS board, and a moved one, get none: %+v", rows)
	}
	attested := withProcedure(cp(), "t-1", procedureOf(map[string]interface{}{"mode": "ATTESTED"}), true, "")
	if !strings.Contains(attested[1].Attest, "--unit https://github.com/o/r/pull/2 ") || attested[2].Attest != "" {
		t.Errorf("on an ATTESTED board every ready PR is attested: %+v", attested)
	}
	required := withProcedure(cp(), "t-1", procedureOf(map[string]interface{}{"mode": "PR_ROWS",
		"merge": map[string]interface{}{"requireAttestation": true}}), true, "")
	if required[1].Attest == "" {
		t.Error("requireAttestation attests a registered PR too")
	}
}

// The board's merge procedure (task 71a3dd22): the command for its method on GitHub and GitLab, the
// head held only at the tested head, and a git sequence for a fast-forward.
func TestMergeCommandForEachMethod(t *testing.T) {
	gh := "https://github.com/o/r/pull/7"
	gl := "https://gitlab.example.com/g/app/-/merge_requests/3"
	api := "glab api --hostname gitlab.example.com --method PUT projects/g%2Fapp/merge_requests/3/merge"
	cases := []struct {
		url, method string
		held        bool
		want        string
	}{
		{gh, "MERGE", true, "gh pr merge " + gh + " --merge --match-head-commit " + fullA},
		{gh, "SQUASH", true, "gh pr merge " + gh + " --squash --match-head-commit " + fullA},
		{gh, "REBASE", true, "gh pr merge " + gh + " --rebase --match-head-commit " + fullA},
		{gh, "SQUASH", false, "gh pr merge " + gh + " --squash"},
		{gl, "MERGE", true, api + " -f sha=" + fullA},
		{gl, "SQUASH", true, api + " -f squash=true -f sha=" + fullA},
		{gl, "REBASE", false, api},
		{gh, "FAST_FORWARD", true, "git fetch origin <pr-branch> && git switch main && git merge --ff-only " + fullA +
			" && git push origin main"},
		{gl, "FAST_FORWARD", false, "git fetch origin <pr-branch> && git switch main && git merge --ff-only origin/<pr-branch>" +
			" && git push origin main"},
	}
	for _, c := range cases {
		if got := mergeCommandFor(c.url, fullA, "main", c.method, c.held); got != c.want {
			t.Errorf("%s %s held=%v:\n got %q\nwant %q", c.url, c.method, c.held, got, c.want)
		}
	}
	if mergeCommand(gh, fullA) != mergeCommandFor(gh, fullA, "", "MERGE", true) {
		t.Error("the default is the merge commit at the tested head, as before")
	}
}

// Who merges (task 71a3dd22): the coordinator; a role's session that signed the task off in that
// role; never a person's board. The plan says so instead of printing commands.
func TestMayMergeAndThePlanWhenItIsNotYours(t *testing.T) {
	task := map[string]interface{}{"signOffs": []interface{}{
		map[string]interface{}{"role": "tester", "session": "s-tester"},
		map[string]interface{}{"role": "Releaser", "session": "s-rel"},
	}}
	by := func(b string) mergeProcedure {
		return procedureOf(map[string]interface{}{"mode": "PR_ROWS", "merge": map[string]interface{}{"by": b}})
	}
	if ok, _ := mayMerge(by("COORDINATOR"), task, ""); !ok {
		t.Error("the coordinator merges")
	}
	if ok, _ := mayMerge(by("ROLE:releaser"), task, "s-rel"); !ok {
		t.Error("the releaser's session merges")
	}
	ok, line := mayMerge(by("ROLE:releaser"), task, "s-tester")
	if ok || !strings.Contains(line, "the releaser role's") {
		t.Errorf("another session is told whose it is: %v %q", ok, line)
	}
	if ok, _ := mayMerge(by("ROLE:releaser"), task, ""); ok {
		t.Error("without --session nobody is the role")
	}
	ok, line = mayMerge(by("PERSON"), task, "s-rel")
	if ok || !strings.HasPrefix(line, "merges on this board are a person's -- do not merge") {
		t.Errorf("a person's board: %v %q", ok, line)
	}
	steps := []mergeStep{{PR: "https://github.com/o/r/pull/1", Registered: true, Status: "ready", at: fullA,
		Command: "gh pr merge x"}}
	plan := withProcedure(steps, "t-1", by("PERSON"), false, line)
	if plan[0].Command != "" || plan[0].Note != line || plan[0].By != "PERSON" || plan[0].Method != "MERGE" || !plan[0].AtTestedHead {
		t.Errorf("no command on a person's board, and the procedure on the step: %+v", plan[0])
	}
	squash := withProcedure([]mergeStep{{PR: "https://github.com/o/r/pull/1", Registered: true, Status: "ready", at: fullA}},
		"t-1", procedureOf(map[string]interface{}{"mode": "PR_ROWS", "merge": map[string]interface{}{"by": "COORDINATOR",
			"method": "SQUASH", "atTestedHead": false}}), true, "")
	if squash[0].Command != "gh pr merge https://github.com/o/r/pull/1 --squash" || squash[0].AtTestedHead {
		t.Errorf("the declared method and hold: %+v", squash[0])
	}
}

// A server from before the setting serves no merge: the coordinator's merge commit at the tested head.
func TestProcedureDefaultsWithoutAMerge(t *testing.T) {
	p := procedureOf(map[string]interface{}{"mode": "ATTESTED"})
	if p != (mergeProcedure{By: "COORDINATOR", Method: "MERGE", AtTestedHead: true, RequireAttestation: true, Order: "NOTE_ORDER"}) {
		t.Errorf("%+v", p)
	}
}
