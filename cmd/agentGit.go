/*
The MIT License (MIT)

Copyright (c) 2020 - 2026 Reliza Incorporated (Reliza (tm), https://reliza.io)

Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated documentation files (the "Software"),
to deal in the Software without restriction, including without limitation the rights to use, copy, modify, merge, publish, distribute, sublicense,
and/or sell copies of the Software, and to permit persons to whom the Software is furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY,
WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
*/

package cmd

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
)

// rearm agent git commit / merge (task RD5-2): two thin helpers, each composing the git commands it prints and
// nothing else (operator decision 2026-10-01). They write the three-trailer block from the CLI's state rather
// than from ids the agent types, sign, and refuse the mistakes the boards kept seeing before anything is
// written: no paths, `.` or -A, a double quote, a body line that reads as a trailer. No rebase, amend, force,
// push or general pass-through: the agent runs plain git for everything else.
//
// The session is named by --session (its uuid or client id). Its client id and agent come from the local
// state `session init` wrote; when this host's state lacks either (a session opened elsewhere, or by an older
// CLI), the session is read once from the server the CLI's credentials point at and the answer is kept. A code
// session on another instance is therefore used with that instance's credentials (-u/-i/-k or --config), and
// the board session with the board's.

const (
	trailerSession  = "ReARM-Agentic-Session"
	trailerAgent    = "ReARM-Agent"
	trailerCoAuthor = "Co-Authored-By"
)

// The minimal session read: the three fields the trailers need, and nothing a server of another version may
// lack (the full session read selects fields older servers do not have).
const gitSessionRead = `query AgentGitSessionProgrammatic($sessionUuid: ID!) { sessionProgrammatic(sessionUuid: $sessionUuid) { uuid clientSessionId agent } }`

var (
	gitSession       string
	gitMessages      []string
	gitCoAuthor      string
	gitSigningKey    string
	gitSigningFormat string
	gitNoCoAuthor    bool
	gitJson          bool
)

var (
	// A body line git could read as a trailer: a token, a colon and a space.
	trailerLikeLine = regexp.MustCompile(`^[A-Za-z-]+: `)
	// "<name> <email>", as git writes an identity.
	coAuthorShape = regexp.MustCompile(`^[^<>\n]*[^<>\s][^<>\n]* <[^<>\s@]+@[^<>\s]+>$`)
)

// gitTrailers is the block a helper writes, in order. An empty coAuthor (--no-co-author) writes the two ReARM
// trailers alone; ARCHITECTURE round 2 s3: the co-author line is the operator's policy, which differs by repository.
type gitTrailers struct {
	session, agent, coAuthor string
}

func (t gitTrailers) lines() []string {
	lines := []string{trailerSession + ": " + t.session, trailerAgent + ": " + t.agent}
	if t.coAuthor != "" {
		lines = append(lines, trailerCoAuthor+": "+t.coAuthor)
	}
	return lines
}

// gitHelperRun is one helper invocation's resolved inputs and what it did.
type gitHelperRun struct {
	trailers   gitTrailers
	signing    []string // the -c pairs, already as "-c", "k=v"
	commands   []string
	staged     []dirStaged // what each directory argument staged, so the widening is visible
	jsonOutput bool
}

// dirStaged is a directory argument and the files it staged.
type dirStaged struct {
	dir   string
	files []string
}

// gitRefusal is a refusal before any git write: printed on stderr, exit 1.
func gitRefusal(format string, a ...interface{}) int {
	fmt.Fprintf(os.Stderr, "rearm: "+format+"\n", a...)
	return 1
}

// resolveGitIdentity finds the trailers and the signing flags for the session, keeping what it learns.
func resolveGitIdentity() (*gitHelperRun, string) {
	ref := strings.TrimSpace(gitSession)
	if ref == "" {
		return nil, "--session is required: the trailers come from the session your code commits belong to"
	}
	st := lookupAgentState(ref)
	if st == nil || st.ClientSessionId == "" || st.AgentUuid == "" {
		uuid := ref
		if st != nil && st.SessionUuid != "" {
			uuid = st.SessionUuid
		}
		if !isUUID(strings.ToLower(uuid)) {
			return nil, fmt.Sprintf("no local state for session %s, and only a session uuid can be read from the server: "+
				"pass --session <session uuid>, or open the session here with rearm agent session init", ref)
		}
		data, err := sendGraphQLRequest(gitSessionRead, map[string]interface{}{"sessionUuid": uuid})
		if err != nil {
			return nil, fmt.Sprintf("this host does not know session %s's client id and agent, and reading it failed: %s. "+
				"Pass the credentials of the instance the session lives on (-u/-i/-k or --config), "+
				"or open the session here with rearm agent session init", ref, describeError(err))
		}
		s, _ := data["sessionProgrammatic"].(map[string]interface{})
		clientId, _ := s["clientSessionId"].(string)
		agent, _ := s["agent"].(string)
		if s == nil || clientId == "" || agent == "" {
			return nil, fmt.Sprintf("the server answered no client id or agent for session %s: "+
				"pass the credentials of the instance the session lives on (-u/-i/-k or --config)", ref)
		}
		if st == nil {
			st = &agentSessionState{SessionUuid: uuid}
		}
		st.ClientSessionId, st.AgentUuid = clientId, agent
		if err := writeAgentState(st); err != nil {
			fmt.Fprintf(os.Stderr, "rearm: could not keep the session's agent locally: %v\n", err)
		}
	}
	id, err := readAgentIdentity(st.AgentUuid)
	if err != nil {
		return nil, err.Error()
	}
	changed := false
	if gitNoCoAuthor && strings.TrimSpace(gitCoAuthor) != "" {
		return nil, "--co-author and --no-co-author together: give one"
	}
	if c := strings.TrimSpace(gitCoAuthor); c != "" {
		if !coAuthorShape.MatchString(c) || strings.Contains(c, `"`) {
			return nil, fmt.Sprintf("--co-author must read '<name> <email>' with no double quote, got: %s", c)
		}
		if id.CoAuthor != c {
			id.CoAuthor, changed = c, true
		}
	}
	if f := strings.ToLower(strings.TrimSpace(gitSigningFormat)); f != "" {
		if f != "ssh" && f != "gpg" {
			return nil, fmt.Sprintf("--signing-format is ssh or gpg, got: %s", gitSigningFormat)
		}
		if id.SigningFormat != f {
			id.SigningFormat, changed = f, true
		}
	}
	if k := strings.TrimSpace(gitSigningKey); k != "" {
		// An ssh key is a file, kept as an absolute path so the next commit from another directory finds it; a
		// gpg key is an id and is kept as given.
		if id.SigningFormat != "gpg" {
			abs, err := filepath.Abs(k)
			if err != nil {
				return nil, err.Error()
			}
			if _, err := os.Stat(abs); err != nil {
				return nil, fmt.Sprintf("--signing-key %s: %v", k, err)
			}
			k = abs
		}
		if id.SigningKey != k {
			id.SigningKey, changed = k, true
		}
	}
	if id.CoAuthor == "" && !gitNoCoAuthor {
		return nil, fmt.Sprintf("no co-author line is kept for agent %s: pass --co-author '<name> <email>' once, and it is kept for the agent", st.AgentUuid)
	}
	if changed {
		if err := writeAgentIdentity(id); err != nil {
			return nil, fmt.Sprintf("could not keep the agent's co-author line and signing key: %v", err)
		}
	}
	coAuthor := id.CoAuthor
	if gitNoCoAuthor {
		coAuthor = ""
	}
	run := &gitHelperRun{
		trailers:   gitTrailers{session: st.ClientSessionId, agent: st.AgentUuid, coAuthor: coAuthor},
		jsonOutput: gitJson,
	}
	if id.SigningFormat != "" {
		run.signing = append(run.signing, "-c", "gpg.format="+id.SigningFormat)
	}
	if id.SigningKey != "" {
		run.signing = append(run.signing, "-c", "user.signingkey="+id.SigningKey)
	}
	return run, ""
}

// composeGitMessage is the subject, the body paragraphs and the block, or why it is refused.
func composeGitMessage(messages []string, t gitTrailers) (string, string) {
	if len(messages) == 0 || strings.TrimSpace(messages[0]) == "" {
		return "", "-m <subject> is required"
	}
	subject := strings.TrimSpace(messages[0])
	if strings.Contains(subject, "\n") {
		return "", "the subject is one line; put the rest in a second -m"
	}
	for i, m := range messages {
		if strings.Contains(m, `"`) {
			where := "subject"
			if i > 0 {
				where = "body"
			}
			return "", fmt.Sprintf("the %s has a double quote, which breaks the rearm-actions command templates: reword without it", where)
		}
	}
	var b strings.Builder
	b.WriteString(subject)
	b.WriteString("\n")
	for _, m := range messages[1:] {
		body := strings.Trim(m, "\n")
		if strings.TrimSpace(body) == "" {
			continue
		}
		for _, line := range strings.Split(body, "\n") {
			if trailerLikeLine.MatchString(line) {
				return "", fmt.Sprintf("the body line %q reads as a trailer and would split the trailer block: reword it (no 'Word: ' at the start of a line)", line)
			}
		}
		b.WriteString("\n")
		b.WriteString(body)
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(strings.Join(t.lines(), "\n"))
	b.WriteString("\n")
	return b.String(), ""
}

// helperGit runs one git command in the current directory and returns its combined output.
func helperGit(stdin string, args ...string) (string, error) {
	c := exec.Command("git", args...)
	c.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if stdin != "" {
		c.Stdin = strings.NewReader(stdin)
	}
	var out bytes.Buffer
	c.Stdout, c.Stderr = &out, &out
	err := c.Run()
	return strings.TrimRight(out.String(), "\n"), err
}

// shellWord quotes an argument for the printed command when the shell would split or expand it.
func shellWord(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\n'\"$`\\*?[]{}()<>|&;!#~") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func printedCommand(args []string) string {
	words := []string{"git"}
	for _, a := range args {
		words = append(words, shellWord(a))
	}
	return strings.Join(words, " ")
}

// mergeInProgress says whether the repository has a merge waiting to be committed.
func mergeInProgress() bool {
	out, err := helperGit("", "rev-parse", "-q", "--verify", "MERGE_HEAD")
	return err == nil && out != ""
}

// literalPathspecs is the git option that makes every path a file name, never a pattern (ARCHITECTURE round 2
// s1): with it, '*', '?', '[', ':(top)', ':/' and ':!x' name files, not matches. It goes on git add and git
// commit, and shows in the printed command.
const literalPathspecs = "--literal-pathspecs"

// checkStagingPaths refuses what would stage more than the agent named: every argument must name a file or a
// directory that exists in the worktree or the index. The repository root, '.' and anything beginning with '-'
// (-A, --all, -u, --update, and a file so named) are refused too.
func checkStagingPaths(paths []string) string {
	top, _ := helperGit("", "rev-parse", "--show-toplevel")
	for _, p := range paths {
		if strings.HasPrefix(p, "-") {
			return fmt.Sprintf("name the files: %s is not a file here (an argument beginning with - is refused)", p)
		}
		if filepath.Clean(p) == "." {
			return "name the files: '.' stages everything"
		}
		if top != "" {
			if abs, err := filepath.Abs(p); err == nil && sameDir(abs, top) {
				return fmt.Sprintf("name the files: %s is the repository's root and stages everything", p)
			}
		}
		if !pathIsHere(p) {
			return fmt.Sprintf("name the files: %s is not a file here", p)
		}
	}
	return ""
}

// pathIsHere says whether p names a file or directory in the worktree, or a path the index knows (a deleted
// file), read literally.
func pathIsHere(p string) bool {
	if p == "" {
		return false
	}
	if _, err := os.Lstat(p); err == nil {
		return true
	}
	out, err := helperGit("", literalPathspecs, "ls-files", "--cached", "--", p)
	return err == nil && out != ""
}

// directoryArgs are the paths that name a directory in the worktree.
func directoryArgs(paths []string) []string {
	var dirs []string
	for _, p := range paths {
		if fi, err := os.Stat(p); err == nil && fi.IsDir() {
			dirs = append(dirs, p)
		}
	}
	return dirs
}

func sameDir(a, b string) bool {
	ea, err1 := filepath.EvalSymlinks(a)
	eb, err2 := filepath.EvalSymlinks(b)
	if err1 != nil || err2 != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return ea == eb
}

// commitWithBlock stages the paths (when given), commits with the block and signs, then checks the block as
// git parses it. paths nil means commit the index as it is (a merge in progress).
func (r *gitHelperRun) commitWithBlock(message string, paths []string, merge bool) int {
	if len(paths) > 0 {
		add := append([]string{literalPathspecs, "add", "--"}, paths...)
		r.commands = append(r.commands, printedCommand(add))
		if out, err := helperGit("", add...); err != nil {
			fmt.Fprintln(os.Stderr, out)
			return r.fail("git add failed; nothing was committed")
		}
		// A directory stages what is under it: the one widening allowed, so it is shown.
		for _, d := range directoryArgs(paths) {
			out, _ := helperGit("", literalPathspecs, "diff", "--cached", "--name-only", "--", d)
			files := []string{}
			for _, f := range strings.Split(out, "\n") {
				if f != "" {
					files = append(files, f)
				}
			}
			r.staged = append(r.staged, dirStaged{dir: d, files: files})
		}
	}
	commit := append(append([]string{}, r.signing...), literalPathspecs, "commit", "-q", "-S", "--cleanup=verbatim", "-F", "-")
	if len(paths) > 0 && !merge {
		// Only the named paths, even when other changes were staged before.
		commit = append(append(commit, "--"), paths...)
	}
	r.commands = append(r.commands, printedCommand(commit))
	if out, err := helperGit(message, commit...); err != nil {
		fmt.Fprintln(os.Stderr, out)
		return r.fail("git commit failed; nothing was committed")
	}
	sha, _ := helperGit("", "rev-parse", "HEAD")
	body, _ := helperGit("", "log", "-1", "--format=%B", "HEAD")
	// --no-divider: a '---' line in the body is prose, not a patch divider (ARCHITECTURE round 2 s2); git log
	// and the server read the whole message the same way.
	parsed, _ := helperGit(body+"\n", "interpret-trailers", "--no-divider", "--parse")
	subject, _, _ := strings.Cut(message, "\n")
	if problem := checkParsedBlock(parsed, r.trailers); problem != "" {
		fmt.Fprintf(os.Stderr, "rearm: commit %s was made, but its trailer block as git parses it is not the %d lines written: %s.\n"+
			"git interpret-trailers --no-divider --parse printed:\n%s\nAmend it by hand before you push (git commit --amend); a commit you pushed is replaced, never force-pushed.\n",
			shortSha(sha), len(r.trailers.lines()), problem, parsed)
		r.report(sha, subject, merge, false)
		return 1
	}
	r.report(sha, subject, merge, true)
	return 0
}

// checkParsedBlock compares what git parsed with the three lines written.
func checkParsedBlock(parsed string, t gitTrailers) string {
	got := []string{}
	for _, l := range strings.Split(strings.TrimSpace(parsed), "\n") {
		if strings.TrimSpace(l) != "" {
			got = append(got, strings.TrimSpace(l))
		}
	}
	want := t.lines()
	if len(got) != len(want) {
		return fmt.Sprintf("git found %d trailer line(s), not %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			return fmt.Sprintf("line %d is %q, not %q", i+1, got[i], want[i])
		}
	}
	return ""
}

func (r *gitHelperRun) fail(msg string) int {
	if r.jsonOutput {
		emitJson(map[string]interface{}{"committed": false, "commands": r.commands, "error": msg})
	} else {
		for _, c := range r.commands {
			fmt.Println("ran: " + c)
		}
	}
	fmt.Fprintln(os.Stderr, "rearm: "+msg)
	return 1
}

func (r *gitHelperRun) report(sha, subject string, merge, blockOk bool) {
	if r.jsonOutput {
		dirs := map[string][]string{}
		for _, d := range r.staged {
			dirs[d.dir] = d.files
		}
		emitJson(map[string]interface{}{
			"committed": true, "sha": sha, "subject": subject, "merge": merge, "commands": r.commands,
			"directories": dirs, "trailers": r.trailers.lines(), "trailerBlockOk": blockOk,
		})
		return
	}
	for _, c := range r.commands {
		fmt.Println("ran: " + c)
	}
	for _, d := range r.staged {
		fmt.Printf("directory %s staged: %s\n", d.dir, strings.Join(d.files, " "))
	}
	fmt.Println(sha + " " + subject)
}

// runGitCommit is `rearm agent git commit`; returns the exit code.
func runGitCommit(paths []string) int {
	merge := mergeInProgress()
	if len(paths) == 0 && !merge {
		return gitRefusal("name the files to commit after --: rearm agent git commit --session <s> -m <subject> -- <path>...")
	}
	if problem := checkStagingPaths(paths); problem != "" {
		return gitRefusal("%s", problem)
	}
	run, problem := resolveGitIdentity()
	if problem != "" {
		return gitRefusal("%s", problem)
	}
	message, problem := composeGitMessage(gitMessages, run.trailers)
	if problem != "" {
		return gitRefusal("%s", problem)
	}
	return run.commitWithBlock(message, paths, merge)
}

// mergeFinishCommand is the commit helper call that finishes a merge left in progress by a conflict. It carries
// --no-co-author when the merge ran with it, so following the printed command writes the block the merge would
// have (tester run 2, T-8). The co-author line, the signing key and format are kept per agent, and the session's
// client id and agent are kept on the first read, so the commit helper finds them without those flags.
func mergeFinishCommand(subject string) string {
	words := []string{"rearm agent git commit --session", shellWord(gitSession)}
	if gitNoCoAuthor {
		words = append(words, "--no-co-author")
	}
	words = append(words, "-m", shellWord(subject))
	return strings.Join(words, " ")
}

// runGitMerge is `rearm agent git merge`; returns the exit code.
func runGitMerge(ref string) int {
	if mergeInProgress() {
		return gitRefusal("a merge is already in progress: resolve it, git add the files, and run rearm agent git commit --session <s> -m <subject>")
	}
	sha, err := helperGit("", "rev-parse", "-q", "--verify", ref+"^{commit}")
	if err != nil || sha == "" {
		return gitRefusal("%s names no commit here (fetch it first?)", ref)
	}
	branch, err := helperGit("", "symbolic-ref", "-q", "--short", "HEAD")
	if err != nil || branch == "" {
		branch = "HEAD"
	}
	run, problem := resolveGitIdentity()
	if problem != "" {
		return gitRefusal("%s", problem)
	}
	messages := append([]string{}, gitMessages...)
	if len(messages) == 0 || strings.TrimSpace(messages[0]) == "" {
		subject := fmt.Sprintf("Merge %s (%s) into %s", ref, shortSha(sha), branch)
		messages = append([]string{subject}, messages[min(1, len(messages)):]...)
	}
	message, problem := composeGitMessage(messages, run.trailers)
	if problem != "" {
		return gitRefusal("%s", problem)
	}
	if _, err := helperGit("", "merge-base", "--is-ancestor", sha, "HEAD"); err == nil {
		if run.jsonOutput {
			emitJson(map[string]interface{}{"committed": false, "upToDate": true, "ref": ref, "sha": sha, "commands": []string{}})
		} else {
			fmt.Printf("Already up to date: %s (%s) is in %s; nothing to merge.\n", ref, shortSha(sha), branch)
		}
		return 0
	}
	mergeArgs := []string{"merge", "--no-ff", "--no-commit", ref}
	run.commands = append(run.commands, printedCommand(mergeArgs))
	out, err := helperGit("", mergeArgs...)
	if err != nil {
		if mergeInProgress() {
			subject, _, _ := strings.Cut(message, "\n")
			fmt.Fprintln(os.Stderr, out)
			fmt.Fprintf(os.Stderr, "rearm: the merge stopped on a conflict and is left in progress. Resolve it, git add the files, then:\n"+
				"  %s\n", mergeFinishCommand(subject))
			if run.jsonOutput {
				emitJson(map[string]interface{}{"committed": false, "conflict": true, "commands": run.commands, "gitOutput": out})
			} else {
				for _, c := range run.commands {
					fmt.Println("ran: " + c)
				}
			}
			return 1
		}
		fmt.Fprintln(os.Stderr, out)
		return run.fail("git merge failed; nothing was merged")
	}
	return run.commitWithBlock(message, nil, true)
}

var agentGitCmd = &cobra.Command{
	Use:   "git",
	Short: "Thin git helpers that write the session's trailer block and sign (commit, merge)",
	Long: `Two helpers, each composing the git commands it prints and nothing else. They write the three
contiguous trailers (ReARM-Agentic-Session, ReARM-Agent, Co-Authored-By) from the CLI's state, sign
with -S, and refuse known mistakes before anything is written. Rebase, amend, force and push stay
plain git.

The session's client id and agent come from the local state 'session init' wrote. When this host
lacks them, the session is read once from the server the CLI's credentials point at, and kept: use a
code session on another instance with that instance's credentials (-u/-i/-k or --config).`,
}

var agentGitCommitCmd = &cobra.Command{
	Use:   "commit --session <s> -m <subject> [-m <body>]... -- <path>...",
	Short: "Stage exactly the named paths and commit them, signed, with the session's trailer block",
	Long: `Runs git --literal-pathspecs add -- <path>... and git commit -S with the subject, the body
paragraphs and the three trailers, then checks the block as git interpret-trailers --no-divider
--parse reads it.

Every path is a file name, never a pattern: '*', ':(top)', ':/' and ':!x' name files. A directory
stages what is under it, and the files it staged are printed.

Refused before anything is written:
  - no path; a path that names no file or directory in the worktree or the index; '.', the
    repository's root, or anything beginning with - (-A, --all, -u, --update): name the files;
  - a double quote in the subject or the body (it breaks the rearm-actions templates);
  - a body line that reads as a trailer ('Word: ' at its start);
  - a session whose client id or agent is unknown, or an agent with no co-author line.

--co-author '<name> <email>' is kept per agent once given. --no-co-author writes the two ReARM
trailers alone for that commit, where the repository's policy wants no co-author line. --signing-key and
--signing-format, passed to git as -c user.signingkey and -c gpg.format (give the key you enrolled
with 'rearm agent enrollkey'). Without them git's own signing configuration is used.

While a merge is in progress (after 'rearm agent git merge' stopped on a conflict) the paths are
optional: the index is committed as the merge, with the block.`,
	Example: `  rearm agent git commit --session <code session> -m 'feat: the thing' -m 'Why it changed.' -- cmd/a.go cmd/a_test.go
  rearm agent git commit --session <code session> --co-author 'Claude Opus 5.5 (1M context) <noreply@anthropic.com>' \
      --signing-key ~/.ssh/agent_signing_key.pub --signing-format ssh -m 'fix: x' -- cmd/x.go`,
	Run: func(cmd *cobra.Command, args []string) {
		os.Exit(runGitCommit(args))
	},
}

var agentGitMergeCmd = &cobra.Command{
	Use:   "merge --session <s> <ref> [-m <subject>]",
	Short: "Merge a ref with --no-ff and commit the merge with the session's trailer block",
	Long: `Runs git merge --no-ff --no-commit <ref>, then the commit path of 'agent git commit' with the
subject 'Merge <ref> (<sha7>) into <branch>' unless -m gives one, so the merge commit carries the
same trailers and signature.

A conflict leaves the merge in progress, prints git's message and exits 1: resolve, git add, and run
'rearm agent git commit --session <s> -m <subject>'. Already up to date: says so and exits 0.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		os.Exit(runGitMerge(args[0]))
	},
}

func init() {
	for _, c := range []*cobra.Command{agentGitCommitCmd, agentGitMergeCmd} {
		f := c.Flags()
		f.StringVar(&gitSession, "session", "", "the session the commit belongs to (uuid or client id) — required")
		f.StringArrayVarP(&gitMessages, "message", "m", nil, "the subject; repeat for body paragraphs")
		f.StringVar(&gitCoAuthor, "co-author", "", "the agent's Co-Authored-By line, '<name> <email>'; kept per agent")
		f.StringVar(&gitSigningKey, "signing-key", "", "the signing key (an ssh key file, or a gpg key id); kept per agent")
		f.StringVar(&gitSigningFormat, "signing-format", "", "ssh or gpg; kept per agent")
		f.BoolVar(&gitNoCoAuthor, "no-co-author", false, "write the two ReARM trailers alone, without Co-Authored-By, for this commit")
		f.BoolVar(&gitJson, "json", false, "print the result as JSON")
	}
	agentGitCmd.AddCommand(agentGitCommitCmd)
	agentGitCmd.AddCommand(agentGitMergeCmd)
	agentCmd.AddCommand(agentGitCmd)
}
