package cmd

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"

	rearm "github.com/relizaio/rearm-client-go"
	"github.com/spf13/cobra"
)

// rearm agent task push (task RD5-3): HEAD to the linked PR's head branch, fast-forward only, verified.
//
// A returning task must push to the branch its PR is open from and confirm the PR head moved; a push to a fresh
// branch or to the base costs a tester round (the RD3-5 lesson), and force-pushing is refused by the board's
// rules. The PR row carries the PR's url, head sha, state and declaration but not the head branch name, and the
// operator wants the CLI alone touched on this board, so the branch is found from git: the remote head whose tip
// is the row's head sha, then the current branch's upstream, then --branch is required.
//
// Thin, by the operator's decision of 2026-10-01: one task read, two git reads (ls-remote, merge-base) and one
// push, each printed. It does not link, merge, rebase, fetch or open anything.

var (
	pushSession string
	pushPr      string
	pushBranch  string
	pushJson    bool
)

// pushGit runs one git command in dir and returns its stdout and stderr. A variable so tests can script a push
// that reports success without landing, or one whose error echoes a credentialed remote URL.
var pushGit = func(dir string, args ...string) (string, string, error) {
	c := exec.Command("git", args...)
	c.Dir = dir
	c.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	var stdout, stderr bytes.Buffer
	c.Stdout, c.Stderr = &stdout, &stderr
	err := c.Run()
	return strings.TrimRight(stdout.String(), "\n"), strings.TrimRight(stderr.String(), "\n"), err
}

// remoteCredentials matches the userinfo of a URL (user:password@ or token@), and anything credential-shaped
// redactSecrets knows; a remote URL a git message echoes is printed only through redactRemote.
var remoteCredentials = regexp.MustCompile(`\b([a-zA-Z][a-zA-Z0-9+.-]*://)[^\s/@'"]+@`)

// redactRemote strips credentials from text git printed: the userinfo of every URL, and the patterns
// redactSecrets knows (tokens, authorization headers, key=value secrets).
func redactRemote(s string) string {
	return redactSecrets(remoteCredentials.ReplaceAllString(s, "${1}[REDACTED]@"))
}

// pushResult is what the verb prints; --json prints it as is.
type pushResult struct {
	Task         string   `json:"task,omitempty"`
	Ok           bool     `json:"ok"`
	Outcome      string   `json:"outcome"` // pushed, already-pushed, refused, mismatch, error
	Pr           string   `json:"pr,omitempty"`
	Branch       string   `json:"branch,omitempty"`
	BranchSource string   `json:"branchSource,omitempty"` // --branch, head sha, upstream
	RemoteBefore string   `json:"remoteBefore,omitempty"`
	Head         string   `json:"head,omitempty"`
	RemoteAfter  string   `json:"remoteAfter,omitempty"`
	Reason       string   `json:"reason,omitempty"`
	Remedy       string   `json:"remedy,omitempty"`
	Commands     []string `json:"commands"`
}

// pushRun is one invocation: the repository, the task's PR rows and what has run so far.
type pushRun struct {
	dir    string
	task   map[string]interface{}
	label  string
	result pushResult
}

// git runs a git command in the repository and records it among the printed commands.
func (p *pushRun) git(args ...string) (string, string, error) {
	p.result.Commands = append(p.result.Commands, "git "+strings.Join(args, " "))
	return pushGit(p.dir, args...)
}

func (p *pushRun) refuse(reason, remedy string) int {
	p.result.Outcome, p.result.Reason, p.result.Remedy = "refused", reason, remedy
	return 1
}

// sameUrl compares two PR urls the way a person would: case and a trailing slash do not matter.
func sameUrl(a, b string) bool {
	return strings.EqualFold(strings.TrimRight(strings.TrimSpace(a), "/"), strings.TrimRight(strings.TrimSpace(b), "/"))
}

// prClosedReason is why a PR row is not pushed to (merged, or declared superseded or abandoned), or "".
func prClosedReason(pr map[string]interface{}) string {
	url := str(pr["url"])
	if str(pr["mergedDate"]) != "" || strings.EqualFold(str(pr["state"]), "MERGED") {
		return url + " is merged"
	}
	decl, _ := pr["declaration"].(map[string]interface{})
	if by := str(decl["supersededBy"]); by != "" {
		return url + " is declared superseded by " + by
	}
	switch o := strings.ToUpper(str(decl["outcome"])); o {
	case "SUPERSEDED":
		return url + " is declared superseded"
	case "ABANDONED":
		return url + " is declared abandoned"
	}
	return ""
}

// pickPr finds the linked PR of this repository (task RD5-3 §3.1 step 1): --pr when given, else the one PR of
// the origin's repository that is neither merged nor declared superseded or abandoned.
func (p *pushRun) pickPr(origin string) (map[string]interface{}, int) {
	key := repoKey(origin)
	rows := asList(p.task["pullRequests"])
	if len(rows) == 0 {
		for _, u := range func() []interface{} { l, _ := p.task["prUrls"].([]interface{}); return l }() {
			rows = append(rows, map[string]interface{}{"url": str(u)})
		}
	}
	var mine []map[string]interface{}
	for _, pr := range rows {
		repo, _ := prRepository(str(pr["url"]))
		if repo != "" && key != "" && repoKey(repo) == key {
			mine = append(mine, pr)
		}
	}
	where := orElse(key, "this repository (no origin remote)")
	if len(mine) == 0 {
		return nil, p.refuse(fmt.Sprintf("no linked PR for %s on %s.", where, p.label),
			fmt.Sprintf("open one and link it: rearm agent task linkpr %s --pr-url <url>", p.label))
	}
	if strings.TrimSpace(pushPr) != "" {
		for _, pr := range mine {
			if sameUrl(str(pr["url"]), pushPr) {
				if why := prClosedReason(pr); why != "" {
					return nil, p.refuse(why+": its head branch is not pushed to.", closedRemedy)
				}
				return pr, 0
			}
		}
		var urls []string
		for _, pr := range mine {
			urls = append(urls, str(pr["url"]))
		}
		return nil, p.refuse(fmt.Sprintf("--pr %s is not a PR linked to %s for %s (linked: %s).", pushPr, p.label, where,
			strings.Join(urls, ", ")), "pass one of the linked PRs, or link it first with task linkpr")
	}
	var open, closed []map[string]interface{}
	for _, pr := range mine {
		if prClosedReason(pr) == "" {
			open = append(open, pr)
		} else {
			closed = append(closed, pr)
		}
	}
	switch {
	case len(open) == 1:
		return open[0], 0
	case len(open) == 0:
		var why []string
		for _, pr := range closed {
			why = append(why, prClosedReason(pr))
		}
		return nil, p.refuse(strings.Join(why, "; ")+": no linked PR for "+where+" is open to push to.", closedRemedy)
	}
	var urls []string
	for _, pr := range open {
		urls = append(urls, str(pr["url"]))
	}
	return nil, p.refuse(fmt.Sprintf("%d linked PRs for %s: %s.", len(open), where, strings.Join(urls, ", ")),
		"say which with --pr <url> (two PRs at the same head sha cannot be told apart by it)")
}

const closedRemedy = "a merged or replaced PR takes no more commits: push to the PR that replaces it (--pr), or open a new PR from the base and task linkpr it"

// remoteHeads is origin's branches, branch name -> tip, read with one ls-remote.
func (p *pushRun) remoteHeads() (map[string]string, error) {
	out, stderr, err := p.git("ls-remote", "--heads", "origin")
	if err != nil {
		return nil, fmt.Errorf("git ls-remote --heads origin failed: %s", redactRemote(orElse(stderr, err.Error())))
	}
	heads := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && strings.HasPrefix(f[1], "refs/heads/") {
			heads[strings.TrimPrefix(f[1], "refs/heads/")] = f[0]
		}
	}
	return heads, nil
}

// upstreamBranch is the current branch's upstream when it is on origin, else "".
func upstreamBranch(dir string) string {
	cur, _, err := pushGit(dir, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil || cur == "" {
		return ""
	}
	remote, _, _ := pushGit(dir, "config", "--get", "branch."+cur+".remote")
	merge, _, _ := pushGit(dir, "config", "--get", "branch."+cur+".merge")
	if strings.TrimSpace(remote) != "origin" || !strings.HasPrefix(strings.TrimSpace(merge), "refs/heads/") {
		return ""
	}
	return strings.TrimPrefix(strings.TrimSpace(merge), "refs/heads/")
}

// cleanBranch is a --branch value as a branch name: refs/heads/ and origin/ are dropped.
func cleanBranch(b string) string {
	b = strings.TrimSpace(b)
	b = strings.TrimPrefix(b, "refs/heads/")
	return strings.TrimPrefix(b, "origin/")
}

// pickBranch finds the PR's head branch (task RD5-3 §3.1 step 2): --branch; else the one remote head whose tip
// is the row's head sha; else, when the row has no head or several heads share it, the upstream; else refused.
func (p *pushRun) pickBranch(pr map[string]interface{}, heads map[string]string) (string, string, string) {
	if b := cleanBranch(pushBranch); b != "" {
		return b, "--branch", ""
	}
	head := strings.ToLower(str(pr["head"]))
	var matches []string
	if head != "" {
		for name, tip := range heads {
			if strings.EqualFold(tip, head) {
				matches = append(matches, name)
			}
		}
	}
	if len(matches) == 1 {
		return matches[0], "head sha", ""
	}
	if head == "" || len(matches) > 1 {
		if up := upstreamBranch(p.dir); up != "" {
			return up, "upstream", ""
		}
	}
	switch {
	case head == "":
		return "", "", fmt.Sprintf("%s has no head on its row (CI never reported it), and the current branch has no upstream on origin.", str(pr["url"]))
	case len(matches) > 1:
		return "", "", fmt.Sprintf("%s's head %s is the tip of %d branches on origin (%s), and the current branch has no upstream on origin.",
			str(pr["url"]), shortSha(head), len(matches), strings.Join(sortedStrings(matches), ", "))
	}
	return "", "", fmt.Sprintf("no branch on origin has %s's head %s at its tip: the row is stale or the branch moved.", str(pr["url"]), shortSha(head))
}

func sortedStrings(in []string) []string {
	out := append([]string{}, in...)
	sort.Strings(out)
	return out
}

// runTaskPush runs the verb and returns its exit code: 0 when HEAD is on the PR's head branch (pushed now or
// already), 1 when refused or the verification found another head, 2 when a read failed.
func runTaskPush(args []string) int {
	p := &pushRun{result: pushResult{Commands: []string{}}}
	code := p.run(args)
	printPushResult(p.result)
	return code
}

func (p *pushRun) run(args []string) int {
	readErr := func(format string, a ...interface{}) int {
		p.result.Outcome, p.result.Reason = "error", fmt.Sprintf(format, a...)
		return 2
	}
	if strings.TrimSpace(pushSession) == "" {
		return readErr("--session is required: the board session that holds the task")
	}
	if gitTraced() {
		return readErr("git tracing is on (GIT_TRACE and kin), and ls-remote and push would print the credential headers; unset it and retry")
	}
	taskUuid, err := resolveTaskRef(args[0])
	if err != nil {
		return readErr("%v", err)
	}
	p.label = args[0]
	p.result.Task = args[0]
	cwd, _ := os.Getwd()
	top, _, err := pushGit(cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		return readErr("not in a git repository: run task push from the PR's checkout")
	}
	p.dir = top
	localHead, _, err := pushGit(top, "rev-parse", "HEAD")
	if err != nil {
		return readErr("this repository has no commit yet")
	}
	p.result.Head = localHead

	data, err := sendGraphQLRequest(rearm.AgentTaskProgrammatic_Operation, map[string]interface{}{"taskUuid": taskUuid})
	if err != nil {
		return readErr("reading task %s: %s", args[0], describeError(err))
	}
	p.task, _ = data["agentTaskProgrammatic"].(map[string]interface{})
	if p.task == nil {
		return readErr("no task %s", args[0])
	}
	if k := str(p.task["key"]); k != "" {
		p.label, p.result.Task = k, k
	}

	pr, code := p.pickPr(gitRemote(top))
	if pr == nil {
		return code
	}
	p.result.Pr = str(pr["url"])

	heads, err := p.remoteHeads()
	if err != nil {
		return readErr("%v", err)
	}
	branch, source, why := p.pickBranch(pr, heads)
	if branch == "" {
		return p.refuse(why, "say --branch <the PR's head branch>")
	}
	p.result.Branch, p.result.BranchSource = branch, source

	// Fast-forward only (§3.1 step 3): the branch's tip on origin is an ancestor of HEAD.
	tip, found := heads[branch]
	if !found {
		return p.refuse(fmt.Sprintf("origin has no branch %s: task push sends commits to the PR's existing head branch, never a new one.", branch),
			"say --branch <the PR's head branch>")
	}
	p.result.RemoteBefore = tip
	if strings.EqualFold(tip, localHead) {
		p.result.Ok, p.result.Outcome, p.result.RemoteAfter = true, "already-pushed", tip
		return 0
	}
	if _, _, err := p.git("merge-base", "--is-ancestor", tip, "HEAD"); err != nil {
		reason := fmt.Sprintf("would not fast-forward: origin/%s is at %s, which HEAD does not contain.", branch, shortSha(tip))
		if _, _, err := pushGit(top, "cat-file", "-e", tip+"^{commit}"); err != nil {
			reason = fmt.Sprintf("would not fast-forward: origin/%s is at %s, which is not in this repository.", branch, shortSha(tip))
		}
		return p.refuse(reason, fmt.Sprintf("merge origin/%s and retry (git fetch origin %s, then git merge --no-ff origin/%s with your trailers); never force", branch, branch, branch))
	}

	// Push and verify (§3.1 step 4): no force flag of any kind, then ls-remote must name HEAD.
	ref := "refs/heads/" + branch
	if _, stderr, err := p.git("push", "origin", "HEAD:"+ref); err != nil {
		return p.refuse("git push was refused: "+redactRemote(orElse(stderr, err.Error())),
			fmt.Sprintf("read git's message above; when the branch moved, merge origin/%s and retry; never force", branch))
	}
	out, stderr, err := p.git("ls-remote", "origin", ref)
	if err != nil {
		p.result.Outcome = "mismatch"
		p.result.Reason = "pushed, but git ls-remote origin " + ref + " failed, so the push is not verified: " + redactRemote(orElse(stderr, err.Error()))
		p.result.Remedy = "run git ls-remote origin " + ref + " and compare it with HEAD"
		return 1
	}
	after := ""
	for _, line := range strings.Split(out, "\n") {
		if f := strings.Fields(line); len(f) == 2 && f[1] == ref {
			after = f[0]
		}
	}
	p.result.RemoteAfter = after
	if !strings.EqualFold(after, localHead) {
		p.result.Outcome = "mismatch"
		p.result.Reason = fmt.Sprintf("pushed, but origin/%s is at %s, not HEAD %s.", branch, orElse(after, "(no such branch)"), localHead)
		p.result.Remedy = fmt.Sprintf("someone else may have pushed to %s: git fetch origin %s, merge, and run task push again", branch, branch)
		return 1
	}
	p.result.Ok, p.result.Outcome = true, "pushed"
	return 0
}

func printPushResult(r pushResult) {
	if pushJson {
		emitJson(r)
		return
	}
	for _, c := range r.Commands {
		fmt.Println("ran: " + c)
	}
	switch r.Outcome {
	case "pushed":
		fmt.Printf("pushed %s to %s (branch %s, found by %s): origin/%s was %s and is now %s, verified by ls-remote\n",
			r.Head, r.Pr, r.Branch, r.BranchSource, r.Branch, shortSha(r.RemoteBefore), shortSha(r.RemoteAfter))
	case "already-pushed":
		fmt.Printf("already pushed: %s (branch %s, found by %s) is at HEAD %s\n", r.Pr, r.Branch, r.BranchSource, r.Head)
	case "error":
		fmt.Fprintln(os.Stderr, "rearm: "+r.Reason)
	default:
		line := "refused: " + r.Reason
		if r.Outcome == "mismatch" {
			line = "verification failed: " + r.Reason
		}
		if r.Remedy != "" {
			line += " Remedy: " + r.Remedy
		}
		fmt.Println(line)
	}
}

var agentTaskPushCmd = &cobra.Command{
	Use:   "push <task>",
	Short: "Push HEAD to the head branch of the task's linked PR for this repository, fast-forward only, and verify it landed",
	Long: `Pushes the current repository's HEAD to the head branch of the PR linked to the task for this
repository, and verifies the remote head with git ls-remote (task RD5-3). Never forces.

  1. The PR: the linked PR whose repository is the origin remote's; --pr when several are open.
     A PR that is merged, or declared superseded or abandoned, is refused by name.
  2. The branch: --branch when given; else the branch on origin whose tip is the PR row's head sha
     (git ls-remote --heads origin); else, when the row has no head yet or several branches share
     it, the current branch's upstream on origin; else refused: say --branch.
  3. Fast-forward only: origin's tip of that branch must be an ancestor of HEAD
     (git merge-base --is-ancestor); otherwise refused: merge origin/<branch> and retry.
     Equal: already pushed, exit 0.
  4. git push origin HEAD:refs/heads/<branch>, then git ls-remote origin refs/heads/<branch> must
     be HEAD; a mismatch prints both shas and exits 1.

Prints the git commands it ran, the PR, the branch and the sha. Any git message it prints has the
remote URL's credentials removed. Exit 0 when HEAD is on the branch, 1 when refused or not verified,
2 when a read failed. --json prints {task, ok, outcome, pr, branch, branchSource, remoteBefore, head,
remoteAfter, reason, remedy, commands}.

  rearm agent task push RD5-3 --session <board-session>
  rearm agent task push RD5-3 --session <board-session> --pr https://github.com/acme/app/pull/7 --branch feature`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		os.Exit(runTaskPush(args))
	},
}

func init() {
	f := agentTaskPushCmd.Flags()
	f.StringVar(&pushSession, "session", "", "the board session holding the task — required")
	f.StringVar(&pushPr, "pr", "", "the linked PR to push to, when several linked PRs are open on this repository")
	f.StringVar(&pushBranch, "branch", "", "the PR's head branch on origin, when it cannot be found from the PR's head sha or the upstream")
	f.BoolVar(&pushJson, "json", false, "print the result as JSON")
	agentTaskCmd.AddCommand(agentTaskPushCmd)
}
