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
