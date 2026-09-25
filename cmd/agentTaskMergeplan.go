/*
The MIT License (MIT)

Copyright (c) 2020 - 2026 Reliza Incorporated (Reliza (tm), https://reliza.io)

Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated documentation files (the "Software"), to deal in the Software without restriction, including without limitation the rights to use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the Software, and to permit persons to whom the Software is furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
*/

package cmd

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	rearm "github.com/relizaio/rearm-client-go"
	"github.com/spf13/cobra"
)

// mergeStep is one linked PR of a task in a merge plan (task 3b97ccfd): the head the newest
// passing review or test covered, the head ReARM last saw, and how to merge at the tested one.
type mergeStep struct {
	PR         string `json:"pr"`
	State      string `json:"state,omitempty"`
	Registered bool   `json:"registered"`
	TestedHead string `json:"testedHead,omitempty"`
	Head       string `json:"head,omitempty"`
	// ready: merge with the command; moved: the PR is past the tested head; untested: no passing
	// round names a head for it; merged: already merged.
	Status string `json:"status"`
	// Target is the branch the PR merges into, as CI reported it.
	Target string `json:"target,omitempty"`
	// Method, By and AtTestedHead are the board's merge procedure (task 71a3dd22).
	Method       string `json:"method,omitempty"`
	By           string `json:"by,omitempty"`
	AtTestedHead bool   `json:"atTestedHead"`
	Command      string `json:"command,omitempty"`
	// Attest is the command that records the merge where the board cannot see it (task 18c5c293):
	// set on a ready PR that is unregistered here, or on a board whose delivery mode is ATTESTED.
	Attest string `json:"attest,omitempty"`
	Note   string `json:"note,omitempty"`
	// at is the full head a ready PR merges at.
	at string
}

// prKey matches a PR URL as the board does: scheme and host lower-cased, a trailing slash and
// .git dropped, then compared case-insensitively.
func prKey(u string) string {
	s := strings.TrimSpace(u)
	if i := strings.IndexAny(s, "?#"); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimRight(s, "/")
	s = strings.TrimSuffix(s, ".git")
	return strings.ToLower(s)
}

// sameCommit is a prefix match either way, as a short tested head names a full one.
func sameCommit(a, b string) bool {
	a, b = strings.ToLower(a), strings.ToLower(b)
	return a != "" && b != "" && (strings.HasPrefix(a, b) || strings.HasPrefix(b, a))
}

// mergeCommand is the forge command that merges only at head: GitHub's --match-head-commit, or
// GitLab's merge API with sha. Empty when the URL is neither.
func mergeCommand(prURL, head string) string {
	u, err := url.Parse(strings.TrimSpace(prURL))
	if err != nil || u.Host == "" {
		return ""
	}
	path := strings.Trim(u.Path, "/")
	if i := strings.Index(path, "/-/merge_requests/"); i >= 0 {
		project := path[:i]
		iid := strings.Trim(path[i+len("/-/merge_requests/"):], "/")
		api := "projects/" + url.PathEscape(project) + "/merge_requests/" + iid + "/merge"
		host := ""
		if !strings.EqualFold(u.Host, "gitlab.com") {
			host = " --hostname " + u.Host
		}
		return "glab api" + host + " --method PUT " + api + " -f sha=" + head
	}
	if strings.Contains(path, "/pull/") {
		return "gh pr merge " + prURL + " --merge --match-head-commit " + head
	}
	return ""
}

// mergePlan reads a task as `task show` returns it and says, per linked PR, how to merge at the
// head that passed.
func mergePlan(task map[string]interface{}) []mergeStep {
	tested := map[string]string{}
	if heads, ok := task["testedHeads"].([]interface{}); ok {
		for _, h := range heads {
			if m, ok := h.(map[string]interface{}); ok {
				pr, _ := m["pr"].(string)
				head, _ := m["head"].(string)
				tested[prKey(pr)] = head
			}
		}
	}
	var steps []mergeStep
	prs, _ := task["pullRequests"].([]interface{})
	for _, p := range prs {
		m, ok := p.(map[string]interface{})
		if !ok {
			continue
		}
		s := mergeStep{}
		s.PR, _ = m["url"].(string)
		s.State, _ = m["state"].(string)
		s.Registered, _ = m["registered"].(bool)
		s.Head, _ = m["head"].(string)
		s.Target, _ = m["targetBranch"].(string)
		s.TestedHead = tested[prKey(s.PR)]
		switch {
		case s.State == "MERGED":
			s.Status = "merged"
		case s.TestedHead == "":
			s.Status = "untested"
			s.Note = "no passing review or test names a head for this PR; do not merge until one does"
		case s.Head != "" && !sameCommit(s.Head, s.TestedHead):
			s.Status = "moved"
			s.Note = "the PR moved past the tested head; the new head has to be tested before it merges"
		default:
			s.Status = "ready"
			// The forge wants the full sha: the PR's own head when it is the tested one.
			head := s.TestedHead
			if s.Head != "" && len(s.Head) > len(head) {
				head = s.Head
			}
			s.at = head
			s.Command = mergeCommand(s.PR, head)
			if s.Command == "" {
				s.Note = "merge at " + head + " by hand: not a GitHub or GitLab URL"
			} else if len(head) < 40 {
				s.Note = "the tested head is short and ReARM has not seen the PR's head; the forge may want the full sha"
			}
		}
		steps = append(steps, s)
	}
	return steps
}

// mergeProcedure is the board's delivery.merge as effectiveDeliveryPolicy resolves it (task 71a3dd22).
type mergeProcedure struct {
	By                 string `json:"by"`
	Method             string `json:"method"`
	AtTestedHead       bool   `json:"atTestedHead"`
	RequireAttestation bool   `json:"requireAttestation"`
	Order              string `json:"order"`
}

// procedureOf reads a board's effectiveDeliveryPolicy. A server from before the setting serves no
// merge: the coordinator merges with a merge commit at the tested head, attesting on ATTESTED.
func procedureOf(policy map[string]interface{}) mergeProcedure {
	mode, _ := policy["mode"].(string)
	p := mergeProcedure{By: "COORDINATOR", Method: "MERGE", AtTestedHead: true, RequireAttestation: mode == "ATTESTED",
		Order: "NOTE_ORDER"}
	m, ok := policy["merge"].(map[string]interface{})
	if !ok {
		return p
	}
	if v, ok := m["by"].(string); ok && v != "" {
		p.By = v
	}
	if v, ok := m["method"].(string); ok && v != "" {
		p.Method = v
	}
	if v, ok := m["atTestedHead"].(bool); ok {
		p.AtTestedHead = v
	}
	if v, ok := m["requireAttestation"].(bool); ok {
		p.RequireAttestation = v || mode == "ATTESTED"
	}
	if v, ok := m["order"].(string); ok && v != "" {
		p.Order = v
	}
	return p
}

// boardProcedure reads the merge procedure of a task's board.
func boardProcedure(board string) (mergeProcedure, error) {
	data, err := sendGraphQLRequest(rearm.AgentBoardProgrammatic_Operation, map[string]interface{}{"boardUuid": board})
	if err != nil {
		return mergeProcedure{}, err
	}
	b, _ := data["agentBoardProgrammatic"].(map[string]interface{})
	p, _ := b["effectiveDeliveryPolicy"].(map[string]interface{})
	return procedureOf(p), nil
}

// mayMerge is whether the caller merges on this board, and the line it prints when it does not: a
// person's board is never the caller's; a role's is the caller's when its session signed the task
// off in that role.
func mayMerge(p mergeProcedure, task map[string]interface{}, session string) (bool, string) {
	const after = " -- do not merge; attest after they do if the board requires it"
	switch {
	case p.By == "COORDINATOR":
		return true, ""
	case strings.HasPrefix(strings.ToUpper(p.By), "ROLE:"):
		role := strings.TrimSpace(p.By[len("ROLE:"):])
		if session != "" {
			offs, _ := task["signOffs"].([]interface{})
			for _, o := range offs {
				m, _ := o.(map[string]interface{})
				r, _ := m["role"].(string)
				s, _ := m["session"].(string)
				if s == session && strings.EqualFold(r, role) {
					return true, ""
				}
			}
		}
		return false, "merges on this board are the " + role + " role's (run with --session <your session> if that is you)" + after
	default:
		return false, "merges on this board are a person's" + after
	}
}

// mergeCommandFor is the command that merges a PR by the board's method: gh pr merge with --merge,
// --squash or --rebase on GitHub; GitLab's merge API; a git fast-forward sequence for FAST_FORWARD.
// The head is held (--match-head-commit, sha) only when the board merges at the tested head.
func mergeCommandFor(prURL, head, target, method string, atTestedHead bool) string {
	if method == "FAST_FORWARD" {
		onto := target
		if onto == "" {
			onto = "<target>"
		}
		at := "origin/<pr-branch>"
		if atTestedHead {
			at = head
		}
		return "git fetch origin <pr-branch> && git switch " + onto + " && git merge --ff-only " + at +
			" && git push origin " + onto
	}
	u, err := url.Parse(strings.TrimSpace(prURL))
	if err != nil || u.Host == "" {
		return ""
	}
	path := strings.Trim(u.Path, "/")
	if i := strings.Index(path, "/-/merge_requests/"); i >= 0 {
		project := path[:i]
		iid := strings.Trim(path[i+len("/-/merge_requests/"):], "/")
		api := "projects/" + url.PathEscape(project) + "/merge_requests/" + iid + "/merge"
		host := ""
		if !strings.EqualFold(u.Host, "gitlab.com") {
			host = " --hostname " + u.Host
		}
		cmd := "glab api" + host + " --method PUT " + api
		if method == "SQUASH" {
			cmd += " -f squash=true"
		}
		if atTestedHead {
			cmd += " -f sha=" + head
		}
		return cmd
	}
	if strings.Contains(path, "/pull/") {
		flag := map[string]string{"SQUASH": "--squash", "REBASE": "--rebase"}[method]
		if flag == "" {
			flag = "--merge"
		}
		cmd := "gh pr merge " + prURL + " " + flag
		if atTestedHead {
			cmd += " --match-head-commit " + head
		}
		return cmd
	}
	return ""
}

// withProcedure sets each step to the board's procedure: its method, who merges and whether at the
// tested head; for a ready PR, the command for that method when the caller merges, or the line that
// says who does; and the attest line when the board requires one or CI does not report the PR here.
func withProcedure(steps []mergeStep, task string, p mergeProcedure, merges bool, notYours string) []mergeStep {
	for i := range steps {
		s := &steps[i]
		s.Method, s.By, s.AtTestedHead = p.Method, p.By, p.AtTestedHead
		if s.Status != "ready" {
			continue
		}
		if !merges {
			s.Command = ""
			s.Note = notYours
		} else if cmd := mergeCommandFor(s.PR, s.at, s.Target, p.Method, p.AtTestedHead); cmd != "" {
			s.Command = cmd
			if p.Method == "REBASE" && strings.Contains(cmd, "glab api") {
				s.Note = "the GitLab project's merge method has to be rebase for this to rebase"
			}
		}
		if p.RequireAttestation || !s.Registered {
			s.Attest = "rearm agent task delivered " + task + " --session <seat-session> --unit " + s.PR +
				" --commit <merge sha>"
		}
	}
	return steps
}

func printMergePlan(steps []mergeStep) {
	if len(steps) == 0 {
		fmt.Println("The task links no PR.")
		return
	}
	if steps[0].Method != "" {
		held := "at the tested head"
		if !steps[0].AtTestedHead {
			held = "not held to the tested head"
		}
		fmt.Printf("merge procedure: by %s, method %s, %s\n", steps[0].By, steps[0].Method, held)
	}
	for _, s := range steps {
		fmt.Printf("%s\n  status: %s", s.PR, s.Status)
		if s.State != "" {
			fmt.Printf("  (PR %s)", s.State)
		} else if !s.Registered {
			fmt.Print("  (unregistered: CI does not report this PR)")
		}
		fmt.Println()
		if s.TestedHead != "" {
			fmt.Printf("  tested: %s\n", s.TestedHead)
		}
		if s.Head != "" {
			fmt.Printf("  head:   %s\n", s.Head)
		}
		if s.Command != "" {
			fmt.Printf("  merge:  %s\n", s.Command)
		}
		if s.Attest != "" {
			fmt.Printf("  attest: %s\n", s.Attest)
		}
		if s.Note != "" {
			fmt.Printf("  note:   %s\n", s.Note)
		}
	}
}

var taskMergeplanJson bool

var agentTaskMergeplanCmd = &cobra.Command{
	Use:   "mergeplan <task-uuid>",
	Short: "Per linked PR: the tested head, the current head, and the command that merges only at the tested one",
	Long: `Prints, for every PR the task links, the head the newest passing review or test covered, the
head ReARM last saw, and the command that merges only at the tested head:
gh pr merge <url> --merge --match-head-commit <head> for GitHub, the merge API with sha for GitLab.
The forge refuses a moved head, which covers PRs ReARM does not see. A PR marked moved or untested
needs a review or test of its current head first.

The board's delivery.merge decides the rest (task 71a3dd22): the method (--merge, --squash or
--rebase; a git fast-forward sequence for FAST_FORWARD), whether the head is held (atTestedHead),
and who merges. On a board where a person merges, or a role and --session is not a session that
signed the task off in that role, the plan says so instead of printing merge commands. After each
command, an attest line records the merge where the board requires it or cannot see it: fill in
the merge sha.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		data, err := sendGraphQLRequest(rearm.AgentTaskProgrammatic_Operation, map[string]interface{}{"taskUuid": args[0]})
		if err != nil {
			printGqlError(err)
			os.Exit(1)
		}
		task, _ := data["agentTaskProgrammatic"].(map[string]interface{})
		steps := mergePlan(task)
		board, _ := task["board"].(string)
		procedure, err := boardProcedure(board)
		if err != nil {
			printGqlError(err)
			os.Exit(1)
		}
		merges, notYours := mayMerge(procedure, task, taskSessionUuid)
		steps = withProcedure(steps, args[0], procedure, merges, notYours)
		if taskMergeplanJson {
			emitJson(steps)
			return
		}
		printMergePlan(steps)
	},
}

func init() {
	agentTaskMergeplanCmd.Flags().BoolVar(&taskMergeplanJson, "json", false, "print the plan as JSON")
	agentTaskMergeplanCmd.Flags().StringVar(&taskSessionUuid, "session", "", "your session, when the board's merges are a role's")
	agentTaskCmd.AddCommand(agentTaskMergeplanCmd)
}
