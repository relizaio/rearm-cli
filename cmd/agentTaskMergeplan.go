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
	Status  string `json:"status"`
	Command string `json:"command,omitempty"`
	Note    string `json:"note,omitempty"`
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

func printMergePlan(steps []mergeStep) {
	if len(steps) == 0 {
		fmt.Println("The task links no PR.")
		return
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
needs a review or test of its current head first.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		data, err := sendGraphQLRequest(rearm.AgentTaskProgrammatic_Operation, map[string]interface{}{"taskUuid": args[0]})
		if err != nil {
			printGqlError(err)
			os.Exit(1)
		}
		task, _ := data["agentTaskProgrammatic"].(map[string]interface{})
		steps := mergePlan(task)
		if taskMergeplanJson {
			emitJson(steps)
			return
		}
		printMergePlan(steps)
	},
}

func init() {
	agentTaskMergeplanCmd.Flags().BoolVar(&taskMergeplanJson, "json", false, "print the plan as JSON")
	agentTaskCmd.AddCommand(agentTaskMergeplanCmd)
}
