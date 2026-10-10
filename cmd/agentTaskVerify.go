package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	rearm "github.com/relizaio/rearm-client-go"
	"github.com/spf13/cobra"
)

// rearm agent task verify (task RD5-1): the hand-over checks, run locally before the sign-off.
//
// A refused sign-off costs a round trip, and in a fresh context per task that is the whole context
// again. The server refuses for what it can see (an output missing, a round unread, a blocking
// element check failing, no linked PR moved), and the coordinator's merge check later refuses for
// what it cannot see until CI reports (a commit or a merge commit without its trailers, a subject
// with a double quote). This verb prints all of them at once, in the server's words.
//
// It reads and never writes: one task read, the board's role configs, the check report of each
// pending output, the local state file (without the migration a normal read may write back), and
// `git log` / `git ls-remote` in the current repository. It does not wrap git generally (operator
// decision 2026-10-01).

var (
	verifySession     string
	verifyBase        string
	verifyNoCode      bool
	verifyJson        bool
	verifyCodeSession []string
)

// The eight checks, in the order they print.
const (
	checkOutputs   = "outputs"
	checkInputs    = "inputs"
	checkBlocking  = "blocking-checks"
	checkCodeMoved = "code-moved"
	checkTrailers  = "trailers"
	checkSubjects  = "subjects"
	checkHead      = "head"
	checkBase      = "base"
)

// The trailers every commit's final paragraph carries, contiguous.
var verifyTrailerKeys = []string{"ReARM-Agentic-Session", "ReARM-Agent", "Co-Authored-By"}

// verifyCheck is one line of the report. A skipped check is ok: it found nothing it could test.
type verifyCheck struct {
	Name    string `json:"name"`
	Ok      bool   `json:"ok"`
	Skipped bool   `json:"skipped,omitempty"`
	Reason  string `json:"reason"`
	Remedy  string `json:"remedy,omitempty"`
}

type verifyReport struct {
	Task   string        `json:"task,omitempty"`
	Checks []verifyCheck `json:"checks"`
	Ok     bool          `json:"ok"`
}

func verifyPass(name, reason string) verifyCheck {
	return verifyCheck{Name: name, Ok: true, Reason: reason}
}
func verifySkip(name, reason string) verifyCheck {
	return verifyCheck{Name: name, Ok: true, Skipped: true, Reason: reason}
}
func verifyFail(name, reason, remedy string) verifyCheck {
	return verifyCheck{Name: name, Reason: reason, Remedy: remedy}
}

// verifyHop is what the server checks read: the task, the hop's local record and the reports.
type verifyHop struct {
	task         map[string]interface{}
	taskUuid     string
	label        string
	session      string // the session's uuid
	sessionRef   string // as given on the command line, for the remedies
	heldBy       string // the assignment's session, "" when unassigned
	assignedAt   time.Time
	outputs      []string
	seen         []string
	clientId     string // the state's client session id
	role         string
	pushesCode   bool
	noCode       bool
	reports      map[string]map[string]interface{} // newest version of an output -> its newest check report
	codeSessions []string
	// Where codeSessions came from when no --code-session was given: the instances whose current sessions for this
	// repository supplied them (task RD5-4), else empty.
	codeSessionsFrom []string
}

// verifyTaskOperation is the task read with what the checks need and the pinned client does not select: each
// document's createdDate, publishing session, replacement and the release a check report is about (task RD5-1).
// The schema has them all; the pin's operation is completed here, as withBaseMovedBy does for RD4-2, and refused
// when its shape no longer matches, rather than sent without them.
var (
	verifyDocsSelect = regexp.MustCompile(`documents\s*\{\s*uuid`)
	verifyDocSelect  = regexp.MustCompile(`document\s*\{\s*specification`)
)

func verifyTaskOperation() (string, error) {
	op := rearm.AgentTaskProgrammatic_Operation
	if n := len(verifyDocsSelect.FindAllStringIndex(op, -1)); n != 1 {
		return "", fmt.Errorf("the task read's documents selection matched %d times, not once", n)
	}
	if n := len(verifyDocSelect.FindAllStringIndex(op, -1)); n != 1 {
		return "", fmt.Errorf("the task read's document selection matched %d times, not once", n)
	}
	op = verifyDocsSelect.ReplaceAllString(op, "documents { uuid createdDate")
	op = verifyDocSelect.ReplaceAllString(op,
		"document { specification session supersededBy elementChecks { scope { checked } }")
	return withBaseMovedBy(op), nil
}

// peekAgentState finds the session's local state without writing anything: readAgentState rewrites a pre-RD4-7
// file in place, and a preflight changes nothing. The old per-task lists are folded into the hop entries in
// memory, as that read would, so the outputs seen here are the ones the sign-off would send.
func peekAgentState(ref string) *agentSessionState {
	if ref == "" {
		return nil
	}
	read := func(path string) *agentSessionState {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		var st agentSessionState
		if json.Unmarshal(raw, &st) != nil {
			return nil
		}
		return &st
	}
	var st *agentSessionState
	if path, err := agentStatePath(ref); err == nil {
		st = read(path)
	}
	if st == nil {
		if dir, err := agentStateDir(); err == nil {
			entries, _ := os.ReadDir(dir)
			for _, e := range entries {
				if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
					continue
				}
				if s := read(filepath.Join(dir, e.Name())); s != nil && s.SessionUuid == ref {
					st = s
					break
				}
			}
		}
	}
	if st != nil {
		migratePendingOutputs(st)
	}
	return st
}

// runTaskVerify runs the verb and returns its exit code: 0 when every check passes, 1 when one fails, 2 when a
// read failed.
func runTaskVerify(args []string) int {
	readErr := func(format string, a ...interface{}) int {
		fmt.Fprintf(os.Stderr, "rearm: "+format+"\n", a...)
		return 2
	}
	if strings.TrimSpace(verifySession) == "" {
		return readErr("--session is required: the checks are about the hop this session holds")
	}
	taskUuid, err := resolveTaskRef(args[0])
	if err != nil {
		return readErr("%v", err)
	}
	op, err := verifyTaskOperation()
	if err != nil {
		return readErr("%v", err)
	}
	data, err := sendGraphQLRequest(op, map[string]interface{}{"taskUuid": taskUuid})
	if err != nil {
		return readErr("reading task %s: %s", args[0], describeError(err))
	}
	task, _ := data["agentTaskProgrammatic"].(map[string]interface{})
	if task == nil {
		return readErr("no task %s", args[0])
	}

	st := peekAgentState(verifySession)
	hop := &verifyHop{task: task, taskUuid: taskUuid, label: orElse(str(task["key"]), taskUuid),
		session: sessionUuidOf(st, verifySession), sessionRef: verifySession, noCode: verifyNoCode,
		reports: map[string]map[string]interface{}{}, codeSessions: cleanSessions(verifyCodeSession)}
	if len(hop.codeSessions) == 0 {
		hop.codeSessions, hop.codeSessionsFrom = codeSessionFallback()
	}
	if a, _ := task["assignment"].(map[string]interface{}); a != nil {
		hop.heldBy = str(a["session"])
		hop.role = str(a["role"])
		hop.assignedAt, _ = time.Parse(time.RFC3339Nano, str(a["assignedAt"]))
	}
	hop.role = orElse(hop.role, str(task["role"]))
	if st != nil {
		hop.clientId = st.ClientSessionId
		if h := st.HopOutputs[taskUuid]; h != nil {
			hop.outputs = append(hop.outputs, h.Outputs...)
		}
		hop.seen = append(hop.seen, st.SeenInputs[taskUuid]...)
	}

	roles, err := sendGraphQLRequest(rearm.AgentTaskRoleConfigsProgrammatic_Operation,
		map[string]interface{}{"boardUuid": str(task["board"])})
	if err != nil {
		return readErr("reading the board's roles: %s", describeError(err))
	}
	for _, r := range asList(roles["agentTaskRoleConfigsProgrammatic"]) {
		if !strings.EqualFold(str(r["name"]), hop.role) {
			continue
		}
		for _, c := range func() []interface{} { l, _ := r["requiredCapabilities"].([]interface{}); return l }() {
			if str(c) == "CODE_PUSH" {
				hop.pushesCode = true
			}
		}
	}

	docs := verifyDocuments(task)
	for _, out := range hop.outputs {
		newest := newestVersion(docs, out)
		if _, done := hop.reports[newest]; done {
			continue
		}
		rep, err := sendGraphQLRequest(rearm.AgentElementCheckReportProgrammatic_Operation,
			map[string]interface{}{"releaseUuid": newest})
		if err != nil {
			return readErr("reading the check report of %s: %s", newest, describeError(err))
		}
		report, _ := rep["agentElementCheckReportProgrammatic"].(map[string]interface{})
		hop.reports[newest] = report
	}

	cwd, _ := os.Getwd()
	report := verifyReport{Task: hop.label}
	report.Checks = append(report.Checks, verifyOutputsCheck(hop), verifyInputsCheck(hop), verifyBlockingCheck(hop),
		verifyCodeMovedCheck(hop, cwd))
	report.Checks = append(report.Checks, verifyGitChecks(hop, cwd)...)
	report.Ok = true
	for _, c := range report.Checks {
		if !c.Ok {
			report.Ok = false
		}
	}
	printVerifyReport(report)
	if !report.Ok {
		return 1
	}
	return 0
}

func printVerifyReport(r verifyReport) {
	if verifyJson {
		emitJson(r)
		return
	}
	passed, failed, skipped := 0, 0, 0
	for _, c := range r.Checks {
		word := "PASS"
		switch {
		case c.Skipped:
			word = "SKIP"
			skipped++
		case !c.Ok:
			word = "FAIL"
			failed++
		default:
			passed++
		}
		line := word + " " + c.Name + ": " + c.Reason
		if c.Remedy != "" {
			line += " Remedy: " + c.Remedy
		}
		fmt.Println(line)
	}
	fmt.Printf("%s: %d pass, %d fail, %d skip\n", r.Task, passed, failed, skipped)
}

// ---------- the server's checks ----------

// verifyDocuments indexes the task's document releases by uuid.
func verifyDocuments(task map[string]interface{}) map[string]map[string]interface{} {
	out := map[string]map[string]interface{}{}
	for _, d := range asList(task["documents"]) {
		out[str(d["uuid"])] = d
	}
	return out
}

func docRef(rd map[string]interface{}) map[string]interface{} {
	d, _ := rd["document"].(map[string]interface{})
	if d == nil {
		return map[string]interface{}{}
	}
	return d
}

// newestVersion follows a release's supersededBy among the task's documents, as the server takes an output.
func newestVersion(docs map[string]map[string]interface{}, release string) string {
	seen := map[string]bool{}
	for !seen[release] {
		seen[release] = true
		next := str(docRef(docs[release])["supersededBy"])
		if next == "" {
			break
		}
		release = next
	}
	return release
}

func roundLabel(rd map[string]interface{}) string {
	d := docRef(rd)
	label := str(d["specification"])
	if r, ok := d["round"].(float64); ok {
		label += fmt.Sprintf(" round %d", int(r))
	}
	if b, _ := d["advisory"].(bool); b {
		label += " (advisory)"
	}
	return label
}

// notHeld is why a server check cannot pass for a session that does not hold the task, or "".
func (h *verifyHop) notHeld() string {
	if h.heldBy == "" {
		return fmt.Sprintf("%s is not assigned to anyone", h.label)
	}
	if !strings.EqualFold(h.heldBy, h.session) {
		return fmt.Sprintf("this session does not hold %s (held by %s)", h.label, short(h.heldBy))
	}
	return ""
}

const notHeldRemedy = "assign the task with task assign, or run verify with --session of the holding session"

// verifyOutputsCheck (check 1): every output the hop recorded is one of the task's documents, published by
// this session in this hop, DRAFT and not advisory.
func verifyOutputsCheck(h *verifyHop) verifyCheck {
	if why := h.notHeld(); why != "" {
		return verifyFail(checkOutputs, why+".", notHeldRemedy)
	}
	if len(h.outputs) == 0 {
		return verifyFail(checkOutputs, "no output recorded for this hop.",
			"publish, or pass `--outputs` at sign-off")
	}
	docs := verifyDocuments(h.task)
	var problems, ok []string
	// Each version a hop republishes is recorded; they all resolve to the newest, which is named once.
	done := map[string]bool{}
	for _, out := range h.outputs {
		r := newestVersion(docs, out)
		if done[r] {
			continue
		}
		done[r] = true
		rd, found := docs[r]
		if !found {
			problems = append(problems, fmt.Sprintf("release %s is not among %s's documents", r, h.label))
			continue
		}
		d := docRef(rd)
		created, _ := time.Parse(time.RFC3339Nano, str(rd["createdDate"]))
		switch {
		case str(rd["lifecycle"]) != "DRAFT":
			problems = append(problems, fmt.Sprintf("%s (%s) is %s, not DRAFT: a sign-off handed it over already",
				roundLabel(rd), r, str(rd["lifecycle"])))
		case d["advisory"] == true:
			problems = append(problems, fmt.Sprintf("%s (%s) is an advisory round, never a hop's output", roundLabel(rd), r))
		case str(d["session"]) != "" && !strings.EqualFold(str(d["session"]), h.session):
			problems = append(problems, fmt.Sprintf("Release %s was published by another session, so this hop cannot claim it as an output", r))
		case !created.IsZero() && !h.assignedAt.IsZero() && created.Before(h.assignedAt):
			problems = append(problems, fmt.Sprintf("Release %s was published before this hop began, so it is not an output of it", r))
		default:
			ok = append(ok, fmt.Sprintf("%s (%s)", roundLabel(rd), r))
		}
	}
	if len(problems) > 0 {
		return verifyFail(checkOutputs, strings.Join(problems, "; ")+".",
			"publish this hop's document again, or pass the right release with `--outputs` at sign-off")
	}
	return verifyPass(checkOutputs, "recorded and DRAFT: "+strings.Join(ok, ", ")+".")
}

// verifyInputsCheck (check 2): every round published on the task since the assignment by anyone but this hop is
// among what the hop recorded as read (RD2-34, RD3-14), as the server's guard counts them.
func verifyInputsCheck(h *verifyHop) verifyCheck {
	if why := h.notHeld(); why != "" {
		return verifyFail(checkInputs, why+".", notHeldRemedy)
	}
	seen := map[string]bool{}
	for _, r := range append(append([]string{}, h.seen...), h.outputs...) {
		seen[r] = true
	}
	var rounds []map[string]interface{}
	own := map[string]bool{}
	for _, rd := range asList(h.task["documents"]) {
		d := docRef(rd)
		if str(d["task"]) != "" && str(d["task"]) != h.taskUuid {
			continue
		}
		if lc := str(rd["lifecycle"]); lc == "CANCELLED" || lc == "REJECTED" {
			continue
		}
		if strings.EqualFold(str(d["session"]), h.session) {
			own[str(rd["uuid"])] = true
		}
		rounds = append(rounds, rd)
	}
	var unseen []string
	since := 0
	for _, rd := range rounds {
		uuid := str(rd["uuid"])
		created, err := time.Parse(time.RFC3339Nano, str(rd["createdDate"]))
		if err != nil || h.assignedAt.IsZero() || !created.After(h.assignedAt) || own[uuid] {
			continue
		}
		if ec, _ := docRef(rd)["elementChecks"].(map[string]interface{}); ec != nil {
			if scope, _ := ec["scope"].(map[string]interface{}); scope != nil && own[str(scope["checked"])] {
				continue
			}
		}
		since++
		if seen[uuid] {
			continue
		}
		unseen = append(unseen, roundLabel(rd)+" v"+str(rd["version"]))
	}
	if len(unseen) > 0 {
		return verifyFail(checkInputs, fmt.Sprintf("Task %s has documents published since your assignment that this sign-off"+
			" does not acknowledge: %s.", h.label, strings.Join(unseen, ", ")),
			fmt.Sprintf("Run task show --session %s, read it, then sign off again.", h.sessionRef))
	}
	if since == 0 {
		return verifyPass(checkInputs, "nothing was published on the task since your assignment by anyone else.")
	}
	return verifyPass(checkInputs, fmt.Sprintf("the %d round(s) published since your assignment are recorded as read.", since))
}

// verifyBlockingCheck (check 3): the newest check report on each pending DRAFT output has no failing blocking
// check. Reads the report the publish cut; never re-runs the checks.
func verifyBlockingCheck(h *verifyHop) verifyCheck {
	if len(h.outputs) == 0 {
		return verifySkip(checkBlocking, "no output recorded, so no check report to read.")
	}
	docs := verifyDocuments(h.task)
	var problems []string
	read := 0
	done := map[string]bool{}
	for _, out := range h.outputs {
		r := newestVersion(docs, out)
		if done[r] {
			continue
		}
		done[r] = true
		if rd, ok := docs[r]; ok && str(rd["lifecycle"]) != "" && str(rd["lifecycle"]) != "DRAFT" {
			continue
		}
		report := h.reports[r]
		ec, _ := docRef(report)["elementChecks"].(map[string]interface{})
		if ec == nil {
			continue
		}
		read++
		var failing []string
		for _, c := range asList(ec["results"]) {
			if str(c["result"]) != "FAIL" || c["blocking"] != true {
				continue
			}
			var msgs []string
			for _, o := range asList(c["offences"]) {
				if len(msgs) == 3 {
					msgs = append(msgs, "and more")
					break
				}
				msgs = append(msgs, str(o["message"]))
			}
			failing = append(failing, str(c["check"])+" ("+strings.Join(msgs, "; ")+")")
		}
		if len(failing) == 0 {
			continue
		}
		label := r
		if rd, ok := docs[r]; ok {
			label = roundLabel(rd)
		}
		reportRound := ""
		if n, ok := docRef(report)["round"].(float64); ok {
			reportRound = fmt.Sprintf(" (check report round %d)", int(n))
		}
		problems = append(problems, label+reportRound+": "+strings.Join(failing, ", "))
	}
	if len(problems) > 0 {
		return verifyFail(checkBlocking, "hand-over refused: blocking check(s) failed on "+strings.Join(problems, "; ")+".",
			"fix and republish, or run `rearm agent doc element-check` after the inputs change")
	}
	if read == 0 {
		return verifyPass(checkBlocking, "no check report on the pending outputs (no elements), so nothing blocks.")
	}
	return verifyPass(checkBlocking, fmt.Sprintf("no blocking check fails on the newest report of %d output(s).", read))
}

// verifyCodeMovedCheck (check 4): a PASSED sign-off by a role that pushes code needs a linked PR in play to have
// moved since the assignment (RD4-2). The heads the server recorded at the assignment are not in the programmatic
// read, so a PR head counts as moved when its commit, read from this repository, was committed after the
// assignment; a head this repository does not have cannot be told, and is said.
func verifyCodeMovedCheck(h *verifyHop, dir string) verifyCheck {
	if h.noCode {
		return verifyPass(checkCodeMoved, "--no-code: the sign-off records that this round changed no code, and the server skips the head check.")
	}
	if !h.pushesCode {
		return verifyPass(checkCodeMoved, fmt.Sprintf("the %s role does not push code (no CODE_PUSH), so the server does not check.", orElse(h.role, "task's")))
	}
	type inPlay struct{ url, head string }
	var prs []inPlay
	for _, pr := range asList(h.task["pullRequests"]) {
		decl, _ := pr["declaration"].(map[string]interface{})
		if pr["registered"] != true || str(pr["head"]) == "" || str(decl["supersededBy"]) != "" {
			continue
		}
		prs = append(prs, inPlay{str(pr["url"]), str(pr["head"])})
	}
	if len(prs) == 0 {
		return verifyPass(checkCodeMoved, "no linked PR is in play here (none registered with a head), so the server does not check.")
	}
	var still, unknown []string
	for _, pr := range prs {
		at := pr.url + " at " + shortSha(pr.head)
		committed, err := commitTime(dir, pr.head)
		if err != nil || h.assignedAt.IsZero() {
			unknown = append(unknown, at)
			continue
		}
		if committed.After(h.assignedAt) {
			return verifyPass(checkCodeMoved, fmt.Sprintf("%s moved since your assignment (its head was committed after it).", at))
		}
		still = append(still, at)
	}
	if len(unknown) > 0 {
		return verifySkip(checkCodeMoved, fmt.Sprintf("cannot tell whether %s moved: the head is not in this repository, and "+
			"the assignment-time heads are not in the programmatic read; run verify from that PR's checkout.",
			strings.Join(unknown, ", ")))
	}
	return verifyFail(checkCodeMoved, fmt.Sprintf("No linked PR moved since your assignment (%s): each head was committed "+
		"before it.", strings.Join(still, ", ")),
		"push your commits to the PR branches, or sign off with --no-code for a round that changed no code")
}

// commitTime is when the commit was committed, read from this repository: the date the code-moved and trailers checks
// compare with the assignment.
func commitTime(dir, sha string) (time.Time, error) {
	out, err := verifyGit(dir, "", "log", "-1", "--format=%cI", sha)
	if err != nil {
		return time.Time{}, err
	}
	return time.Parse(time.RFC3339, strings.TrimSpace(out))
}

// earlierRound is whether the commit was committed before the assignment, by the code-moved check's rule (a commit
// committed after it is this round's); no assignment time, or a date that cannot be read, is this round's.
func earlierRound(h *verifyHop, dir, sha string) bool {
	if h.assignedAt.IsZero() {
		return false
	}
	committed, err := commitTime(dir, sha)
	return err == nil && !committed.After(h.assignedAt)
}

func shortSha(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// ---------- the git checks ----------

// verifyGit runs one read-only git command in dir; stdin, when given, is fed to it. Never prompts for
// credentials, and its stderr (which may carry a remote URL) is not printed.
func verifyGit(dir, stdin string, args ...string) (string, error) {
	c := exec.Command("git", args...)
	c.Dir = dir
	c.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if stdin != "" {
		c.Stdin = strings.NewReader(stdin)
	}
	out, err := c.Output()
	if err != nil {
		return "", fmt.Errorf("git %s failed", args[0])
	}
	return strings.TrimRight(string(out), "\n"), nil
}

// prRepository is a PR URL without its "pull/N" (or "merge_requests/N") part, and the PR number.
func prRepository(url string) (repo, number string) {
	s := strings.TrimRight(strings.TrimSpace(url), "/")
	parts := strings.Split(s, "/")
	if len(parts) < 3 {
		return "", ""
	}
	number = parts[len(parts)-1]
	parts = parts[:len(parts)-2]
	if len(parts) > 0 && parts[len(parts)-1] == "-" {
		parts = parts[:len(parts)-1]
	}
	return strings.Join(parts, "/"), number
}

type verifyPR struct {
	url, number, base, head string
}

type verifyCommit struct {
	sha, message string
	merge        bool
}

// verifyGitChecks runs checks 5 to 8 in the current repository when its origin is one a linked PR names;
// otherwise each is skipped, saying why.
func verifyGitChecks(h *verifyHop, cwd string) []verifyCheck {
	skipAll := func(reason string) []verifyCheck {
		return []verifyCheck{verifySkip(checkTrailers, reason), verifySkip(checkSubjects, reason), verifySkip(checkHead, reason), verifySkip(checkBase, reason)}
	}
	top, err := verifyGit(cwd, "", "rev-parse", "--show-toplevel")
	if err != nil {
		return skipAll("not in a git repository; run verify from the PR's checkout for the git checks.")
	}
	key := repoKey(gitRemote(top))
	var prs []verifyPR
	rows := asList(h.task["pullRequests"])
	if len(rows) == 0 {
		for _, u := range func() []interface{} { l, _ := h.task["prUrls"].([]interface{}); return l }() {
			rows = append(rows, map[string]interface{}{"url": str(u)})
		}
	}
	for _, pr := range rows {
		repo, number := prRepository(str(pr["url"]))
		if repo == "" || key == "" || repoKey(repo) != key {
			continue
		}
		base := strings.TrimSpace(verifyBase)
		base = strings.TrimPrefix(strings.TrimPrefix(base, "refs/heads/"), "origin/")
		if base == "" {
			base = str(pr["targetBranch"])
		}
		prs = append(prs, verifyPR{url: str(pr["url"]), number: number, base: base, head: str(pr["head"])})
	}
	if len(prs) == 0 {
		if len(rows) == 0 {
			return skipAll(fmt.Sprintf("%s links no PR yet; link it with task linkpr, then verify again.", h.label))
		}
		return skipAll(fmt.Sprintf("this repository (%s) is not one a PR linked to %s names.", orElse(key, "no origin"), h.label))
	}
	localHead, err := verifyGit(top, "", "rev-parse", "HEAD")
	if err != nil {
		return skipAll("this repository has no commit yet.")
	}
	return []verifyCheck{
		verifyTrailersCheck(h, top, prs),
		verifySubjectsCheck(top, prs),
		verifyHeadCheck(top, localHead, prs),
		verifyBaseCheck(top, prs),
	}
}

// commitsSinceBase is every commit from the merge base with origin/<base> to HEAD, merges included.
func commitsSinceBase(dir, base string) ([]verifyCommit, error) {
	ref := "refs/remotes/origin/" + base
	if _, err := verifyGit(dir, "", "rev-parse", "--verify", "--quiet", ref); err != nil {
		ref = "refs/heads/" + base
		if _, err := verifyGit(dir, "", "rev-parse", "--verify", "--quiet", ref); err != nil {
			return nil, fmt.Errorf("origin/%s is not in this repository", base)
		}
	}
	read := func(args ...string) (string, error) { return verifyGit(dir, "", args...) }
	return commitsInRange(read, nil, ref+"..HEAD", "since "+base)
}

// commitsInRange is every commit in a revision range, merges included, read with one git log. The range follows
// --end-of-options, so no revision built from input reads as an option; opts, revision options such as --not,
// come before it. --no-show-signature keeps a log.showSignature line out of the message. what ends the error
// when the range cannot be listed.
func commitsInRange(read func(args ...string) (string, error), opts []string, rng, what string) ([]verifyCommit, error) {
	args := append([]string{"log", "--no-show-signature", "-z", "--format=%H %P%n%B"}, opts...)
	out, err := read(append(args, "--end-of-options", rng)...)
	if err != nil {
		return nil, fmt.Errorf("could not list the commits %s", what)
	}
	var commits []verifyCommit
	for _, rec := range strings.Split(out, "\x00") {
		head, message, _ := strings.Cut(strings.TrimLeft(rec, "\n"), "\n")
		f := strings.Fields(head)
		if len(f) == 0 {
			continue
		}
		commits = append(commits, verifyCommit{sha: f[0], message: strings.TrimRight(message, "\n"), merge: len(f) > 2})
	}
	return commits, nil
}

// basesOf groups the PRs by base: two PRs of one repository on one base read one range.
func basesOf(prs []verifyPR) ([]string, []string) {
	var bases, missing []string
	have := map[string]bool{}
	for _, pr := range prs {
		if pr.base == "" {
			missing = append(missing, pr.url)
			continue
		}
		if !have[pr.base] {
			have[pr.base] = true
			bases = append(bases, pr.base)
		}
	}
	return bases, missing
}

func noBaseReason(missing []string) string {
	return fmt.Sprintf("no base branch known for %s: the PR row names no targetBranch (not registered here).",
		strings.Join(missing, ", "))
}

const noBaseRemedy = "pass --base <branch>, the branch the PR merges into"

func capList(items []string, n int) string {
	if len(items) <= n {
		return strings.Join(items, "; ")
	}
	return strings.Join(items[:n], "; ") + fmt.Sprintf("; and %d more", len(items)-n)
}

func commitLabel(c verifyCommit) string {
	if c.merge {
		return shortSha(c.sha) + " (merge)"
	}
	return shortSha(c.sha)
}

// cleanSessions is the --code-session ids, trimmed, without empties or repeats.
// codeSessionFallback is verify's code session when no --code-session is given (ARCHITECTURE round 2 §2): the client
// ids of the current sessions recorded for this repository on every instance other than the one the board is read
// from, from local state, so no other instance is read. None recorded: nil, and the board session's client id is
// compared, as before.
func codeSessionFallback() (ids, from []string) {
	repo, err := repositoryKey()
	if err != nil {
		return nil, nil
	}
	for _, e := range otherInstanceSessions(repo, currentInstance()) {
		if e.ClientSessionId == "" || containsString(ids, e.ClientSessionId) {
			continue
		}
		ids = append(ids, e.ClientSessionId)
		from = append(from, e.Instance)
	}
	return ids, from
}

func cleanSessions(ids []string) []string {
	var out []string
	for _, id := range ids {
		if id = strings.TrimSpace(id); id != "" && !containsString(out, id) {
			out = append(out, id)
		}
	}
	return out
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// sessionsLabel names the session ids the trailers were compared with.
func sessionsLabel(ids []string) string {
	if len(ids) == 1 {
		return "session " + ids[0]
	}
	return "sessions " + strings.Join(ids, ", ")
}

// parseTrailers is the final paragraph's trailers as git reads them: the same parser the server's
// %(trailers) placeholder uses, so a blank line inside the block drops what precedes it here too.
func parseTrailers(dir, message string) map[string][]string {
	out := map[string][]string{}
	parsed, err := verifyGit(dir, message+"\n", "interpret-trailers", "--parse")
	if err != nil {
		return out
	}
	for _, line := range strings.Split(parsed, "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		out[strings.ToLower(strings.TrimSpace(k))] = append(out[strings.ToLower(strings.TrimSpace(k))], strings.TrimSpace(v))
	}
	return out
}

// verifyTrailersCheck (check 5): every commit since the merge base carries the three trailers in its final
// paragraph, merges included; the session trailer is this session's client id (or one of the --code-session ids), and the
// agent trailer is the same on every commit. A commit committed before the assignment is an earlier round's, checked at
// that round's sign-off under a session that may since have closed: any session trailer is accepted on it.
func verifyTrailersCheck(h *verifyHop, dir string, prs []verifyPR) verifyCheck {
	bases, missing := basesOf(prs)
	if len(missing) > 0 {
		return verifyFail(checkTrailers, noBaseReason(missing), noBaseRemedy)
	}
	wants := h.codeSessions
	if len(wants) == 0 && h.clientId != "" {
		wants = []string{h.clientId}
	}
	var problems, earlierSessions []string
	total, merges, earlier := 0, 0, 0
	agents := map[string]string{}
	for _, base := range bases {
		commits, err := commitsSinceBase(dir, base)
		if err != nil {
			return verifyFail(checkTrailers, err.Error()+".", "git fetch origin "+base+", then verify again")
		}
		for _, c := range commits {
			total++
			if c.merge {
				merges++
			}
			tr := parseTrailers(dir, c.message)
			var lacks []string
			for _, k := range verifyTrailerKeys {
				if len(tr[strings.ToLower(k)]) == 0 {
					lacks = append(lacks, k)
				}
			}
			if len(lacks) > 0 {
				problems = append(problems, fmt.Sprintf("%s lacks %s in its final paragraph", commitLabel(c), strings.Join(lacks, ", ")))
				continue
			}
			if got := tr["rearm-agentic-session"][0]; earlierRound(h, dir, c.sha) {
				earlier++
				if !containsString(earlierSessions, got) {
					earlierSessions = append(earlierSessions, got)
				}
			} else if len(wants) == 0 {
				problems = append(problems, fmt.Sprintf("%s carries ReARM-Agentic-Session %s, and this host knows no client id for the session to compare", commitLabel(c), got))
			} else if !containsString(wants, got) {
				problems = append(problems, fmt.Sprintf("%s carries ReARM-Agentic-Session %s, not %s", commitLabel(c), got, strings.Join(wants, " or ")))
			}
			agents[tr["rearm-agent"][0]] = commitLabel(c)
		}
	}
	if len(agents) > 1 {
		var parts []string
		for a, c := range agents {
			parts = append(parts, c+" has "+a)
		}
		problems = append(problems, "ReARM-Agent differs between commits: "+strings.Join(parts, ", "))
	}
	if len(problems) > 0 {
		remedy := "put ReARM-Agentic-Session, ReARM-Agent and Co-Authored-By in one final paragraph, no blank line between; " +
			"amend a commit you have not pushed; a pushed one is replaced by a new PR from the base (task supersedepr), never force-pushed"
		if len(wants) == 0 {
			remedy = "pass --code-session <client session id> when your code commits carry the session you opened on the controlling instance; " + remedy
		} else if len(h.codeSessions) == 0 && !h.assignedAt.IsZero() {
			remedy += "; commits from before your assignment are accepted under any session; when this round's commits carry " +
				"a code session on another instance, pass --code-session <its client id>"
		} else if len(h.codeSessions) == 0 {
			remedy += "; when your code commits carry a code session on another instance, pass --code-session <its client id>"
		} else if len(h.codeSessionsFrom) > 0 {
			used := "the task's rounds used"
			if !h.assignedAt.IsZero() {
				used = "this round's commits used"
			}
			remedy += "; the session compared is the current session recorded for this repository on " + strings.Join(h.codeSessionsFrom, ", ") +
				": pass --code-session for each code session " + used + ", which replaces it"
		} else if !h.assignedAt.IsZero() {
			remedy += "; commits from before your assignment are accepted under any session, so only this round's commits made under " +
				"another instance's session need theirs: repeat --code-session for each"
		} else {
			remedy += "; a returning task's earlier rounds carry their own code sessions: repeat --code-session for each"
		}
		return verifyFail(checkTrailers, capList(problems, 5)+".", remedy)
	}
	if total == 0 {
		return verifyPass(checkTrailers, "no commit since the merge base with "+strings.Join(bases, ", ")+".")
	}
	label := ""
	if earlier < total {
		label = ", " + sessionsLabel(wants)
		if len(h.codeSessionsFrom) > 0 {
			label += " (the current session recorded here on " + strings.Join(h.codeSessionsFrom, ", ") + ")"
		}
	}
	if earlier > 0 {
		label += fmt.Sprintf("; %d earlier-round commit(s), committed before your assignment, accepted with %s", earlier, sessionsLabel(earlierSessions))
	}
	return verifyPass(checkTrailers, fmt.Sprintf("%d commit(s) since the merge base with origin/%s (%d merge(s)) carry the three trailers%s.",
		total, strings.Join(bases, ", origin/"), merges, label))
}

// verifySubjectsCheck (check 6): no commit message in the range carries a double quote, which breaks the
// rearm-actions command templates the message is substituted into.
func verifySubjectsCheck(dir string, prs []verifyPR) verifyCheck {
	bases, missing := basesOf(prs)
	if len(missing) > 0 {
		return verifyFail(checkSubjects, noBaseReason(missing), noBaseRemedy)
	}
	var problems []string
	total := 0
	for _, base := range bases {
		commits, err := commitsSinceBase(dir, base)
		if err != nil {
			return verifyFail(checkSubjects, err.Error()+".", "git fetch origin "+base+", then verify again")
		}
		total += len(commits)
		problems = append(problems, quoteProblems(commits)...)
	}
	if len(problems) > 0 {
		return verifyFail(checkSubjects, capList(problems, 5)+".",
			"reword without double quotes (the rearm-actions templates break on them): amend a commit you have not pushed (task push refuses one before pushing it); a pushed one is replaced by a new PR from the base")
	}
	return verifyPass(checkSubjects, fmt.Sprintf("no double quote in the %d commit message(s) since the merge base.", total))
}

// quoteProblems names each commit whose message carries a double quote, and says whether the subject or only
// the body has it; only the subject is printed.
func quoteProblems(commits []verifyCommit) []string {
	var problems []string
	for _, c := range commits {
		if !strings.Contains(c.message, `"`) {
			continue
		}
		subject := subjectOf(c.message)
		where := "body"
		if strings.Contains(subject, `"`) {
			where = "subject"
		}
		problems = append(problems, fmt.Sprintf("%s has a double quote in its %s: %s", commitLabel(c), where, subject))
	}
	return problems
}

// subjectOf is a message's subject as git's %s reads it, which is what the templates get: the first paragraph
// after any blank lines, its lines joined by spaces.
func subjectOf(message string) string {
	var lines []string
	for _, line := range strings.Split(message, "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, strings.TrimRight(line, " \t\r"))
		} else if len(lines) > 0 {
			break
		}
	}
	return strings.Join(lines, " ")
}

// lsRemote reads one ref's sha on origin, or "" when origin has no such ref.
func lsRemote(dir, ref string) (string, error) {
	if gitTraced() {
		return "", fmt.Errorf("git tracing is on (GIT_TRACE and kin), and ls-remote would print the credential headers")
	}
	out, err := verifyGit(dir, "", "ls-remote", "origin", ref)
	if err != nil {
		return "", fmt.Errorf("git ls-remote origin %s failed", ref)
	}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && f[1] == ref {
			return f[0], nil
		}
	}
	return "", nil
}

func hasObject(dir, sha string) bool {
	_, err := verifyGit(dir, "", "cat-file", "-e", sha+"^{commit}")
	return err == nil
}

func isAncestor(dir, a, b string) bool {
	_, err := verifyGit(dir, "", "merge-base", "--is-ancestor", a, b)
	return err == nil
}

// verifyHeadCheck (check 7): each linked PR's head equals the local HEAD (the RD4-2 head check, read locally).
// The head is the PR row's when CI reported one, else origin's refs/pull/<n>/head.
func verifyHeadCheck(dir, localHead string, prs []verifyPR) verifyCheck {
	var problems, ok, unknown []string
	for _, pr := range prs {
		head, source := pr.head, "as CI reported it"
		if head == "" && pr.number != "" && strings.Contains(pr.url, "/pull/") {
			sha, err := lsRemote(dir, "refs/pull/"+pr.number+"/head")
			if err != nil {
				unknown = append(unknown, pr.url+": "+err.Error())
				continue
			}
			head, source = sha, "from git ls-remote"
		}
		if head == "" {
			unknown = append(unknown, pr.url+": no head known (not registered here, and origin has no PR ref for it)")
			continue
		}
		switch {
		case strings.EqualFold(head, localHead):
			ok = append(ok, fmt.Sprintf("%s is at HEAD %s (%s)", pr.url, shortSha(head), source))
		case !hasObject(dir, head):
			problems = append(problems, fmt.Sprintf("%s is at %s (%s), which is not in this repository", pr.url, shortSha(head), source))
		case isAncestor(dir, head, localHead):
			n, _ := verifyGit(dir, "", "rev-list", "--count", head+"..HEAD")
			unit := "commits"
			if n == "1" {
				unit = "commit"
			}
			problems = append(problems, fmt.Sprintf("%s %s not pushed: %s is at %s, HEAD is %s", n, unit, pr.url, shortSha(head), shortSha(localHead)))
		default:
			problems = append(problems, fmt.Sprintf("%s is at %s (%s), which is not in HEAD's history: your commits went to another branch, or the PR moved", pr.url, shortSha(head), source))
		}
	}
	if len(problems) > 0 {
		return verifyFail(checkHead, strings.Join(problems, "; ")+".",
			"push HEAD to the PR's head branch (git push origin HEAD:<head ref>, the ref the PR names, which may differ from your local branch); fetch and merge first when the PR moved")
	}
	if len(ok) == 0 {
		return verifySkip(checkHead, strings.Join(unknown, "; ")+".")
	}
	reason := strings.Join(ok, "; ") + "."
	if len(unknown) > 0 {
		reason += " Not read: " + strings.Join(unknown, "; ") + "."
	}
	return verifyPass(checkHead, reason)
}

// verifyBaseCheck (check 8): origin's tip of each PR's base is an ancestor of HEAD, read with ls-remote: what
// RD4-2's baseMovedBy shows later, read locally now.
func verifyBaseCheck(dir string, prs []verifyPR) verifyCheck {
	bases, missing := basesOf(prs)
	if len(missing) > 0 {
		return verifyFail(checkBase, noBaseReason(missing), noBaseRemedy)
	}
	var problems, ok []string
	for _, base := range bases {
		tip, err := lsRemote(dir, "refs/heads/"+base)
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		if tip == "" {
			problems = append(problems, "origin has no branch "+base)
			continue
		}
		if hasObject(dir, tip) && isAncestor(dir, tip, "HEAD") {
			ok = append(ok, fmt.Sprintf("origin/%s at %s is merged into HEAD", base, shortSha(tip)))
			continue
		}
		problems = append(problems, fmt.Sprintf("base moved since your merge: origin/%s is at %s, which HEAD does not contain", base, shortSha(tip)))
	}
	if len(problems) > 0 {
		return verifyFail(checkBase, strings.Join(problems, "; ")+".",
			"merge it and retry: git fetch origin <base>, then git merge --no-ff origin/<base> with your trailers, and run the tests again")
	}
	return verifyPass(checkBase, strings.Join(ok, "; ")+".")
}

var agentTaskVerifyCmd = &cobra.Command{
	Use:   "verify <task>",
	Short: "Run the hand-over checks locally before the sign-off: outputs, inputs, element checks, moved PRs, and the git facts",
	Long: `Runs locally what the server would refuse at sign-off, and the git facts the coordinator's merge
check refuses later, and prints every failure at once (task RD5-1). Read-only: nothing on the
server, in git or in the local state changes.

Server facts, from the task read and this CLI's record of the hop:
  outputs          every output the hop recorded is the task's, DRAFT, published in this hop
  inputs           every round published since the assignment by anyone else is recorded as read
  blocking-checks  the newest check report on each pending output has no failing blocking check
  code-moved       a role that pushes code has a linked PR that moved since the assignment (RD4-2),
                   unless --no-code; a head is dated from this repository

Git facts, in the current repository when its origin is one a linked PR names (else skipped):
  trailers         every commit since the merge base with the PR's base, merges included, ends in
                   ReARM-Agentic-Session, ReARM-Agent and Co-Authored-By as one paragraph; one agent
                   throughout; on a commit committed after the assignment, the session is a
                   --code-session id; without the flag, a current session recorded for this
                   repository on another instance (session open, session current --set), else this
                   session's client id; a commit committed before it is an earlier round's, checked
                   at that round's sign-off, and any session is accepted
  subjects         no commit message carries a double quote (task push refuses one before pushing it)
  head             each linked PR's head (as CI reported it, else origin's refs/pull/<n>/head) is HEAD
  base             origin's tip of the base branch (git ls-remote) is merged into HEAD

One line per check, PASS, FAIL or SKIP, with the reason and the remedy in the server's words.
Exit 0 when nothing fails, 1 when a check fails, 2 when a read failed. --json prints
{task, checks: [{name, ok, skipped, reason, remedy}], ok}.

  rearm agent task verify RD5-1 --session <board-session> --code-session <code session client id>
  rearm agent task verify RD5-1 --session <board-session> --code-session <code id 1> --code-session <code id 2>`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		os.Exit(runTaskVerify(args))
	},
}

func init() {
	f := agentTaskVerifyCmd.Flags()
	f.StringVar(&verifySession, "session", "", "the board session holding the task — required")
	f.StringVar(&verifyBase, "base", "", "the branch the PR merges into, when the PR row names none (unregistered here)")
	f.BoolVar(&verifyNoCode, "no-code", false, "the sign-off will say this round changed no code (skips the moved-PR check, as the server does)")
	f.BoolVar(&verifyJson, "json", false, "print the checks as JSON")
	f.StringArrayVar(&verifyCodeSession, "code-session", nil, "the client session id your code commits carry, when it is not the board session's (a code session on another instance); repeat it for each code session this round's commits used (commits from before the assignment are accepted under any session). Left out: the current sessions recorded for this repository on other instances, else the board session's")
	agentTaskCmd.AddCommand(agentTaskVerifyCmd)
}
