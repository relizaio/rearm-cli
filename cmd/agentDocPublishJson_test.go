package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"sync"
	"testing"

	rearm "github.com/relizaio/rearm-client-go"
)

// One JSON value per command with --json (task RD5-8): doc publish, doc publish --check and doc element-check print
// one object on stdout that carries the element-check report as checks {verdict, counts, blocking, lines}, and
// nothing on stderr on success; refusals and local errors go to stderr with exit 1 and an empty stdout; without
// --json nothing changes. Each case runs the real command in a child process, so both streams and the exit code are
// what a script sees.

const (
	pjDesign = "design/arch-1.md"
	pjPlain  = "impl/notes-1.md"
)

// pjBoard answers what the three commands ask, and counts the publishes, report reads, previews and runs.
type pjBoard struct {
	mu   sync.Mutex
	repo string
	// report is the element-check report the board cut (document.elementChecks); nil means none.
	report map[string]any
	// refuse names an operation the server refuses: "publish", "report", "preview" or "run".
	refuse string
	// nullRun makes the element-check run answer a null release with no error.
	nullRun                          bool
	publishes, reads, previews, runs int
	publishedPaths                   []string
	targets                          []any
}

func (b *pjBoard) serve() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		b.mu.Lock()
		defer b.mu.Unlock()
		refused := func(op string) bool {
			if b.refuse != op {
				return false
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"errors": []any{map[string]any{"message": "refused by the board: " + op}}})
			return true
		}
		checked := func(uuid string) map[string]any {
			return map[string]any{"uuid": uuid, "lifecycle": "DRAFT",
				"document": map[string]any{"round": 1, "elementChecks": b.report}}
		}
		data := map[string]any{}
		switch q := req.Query; {
		case strings.Contains(q, "agentDocumentPublishProgrammatic("):
			b.publishes++
			in, _ := req.Variables["input"].(map[string]any)
			b.publishedPaths = append(b.publishedPaths, str(in["path"]))
			if refused("publish") {
				return
			}
			data["agentDocumentPublishProgrammatic"] = map[string]any{"uuid": "r-new", "version": "1", "lifecycle": "DRAFT"}
		case strings.Contains(q, "agentElementCheckReportProgrammatic("):
			b.reads++
			if refused("report") {
				return
			}
			data["agentElementCheckReportProgrammatic"] = checked(str(req.Variables["releaseUuid"]))
		case strings.Contains(q, "agentElementCheckPreviewProgrammatic("):
			b.previews++
			if refused("preview") {
				return
			}
			data["agentElementCheckPreviewProgrammatic"] = b.report
		case strings.Contains(q, "agentElementCheckRunProgrammatic("):
			b.runs++
			if refused("run") {
				return
			}
			if b.nullRun {
				data["agentElementCheckRunProgrammatic"] = nil
				break
			}
			data["agentElementCheckRunProgrammatic"] = checked(str(req.Variables["releaseUuid"]))
		case strings.Contains(q, "query AgentTaskElementCheckTargetsProgrammatic"):
			data["agentTaskProgrammatic"] = map[string]any{"uuid": "t-1", "documents": b.targets}
		case strings.Contains(q, "query AgentTaskProgrammatic "):
			data["agentTaskProgrammatic"] = map[string]any{"uuid": "t-1", "key": "RD-1", "board": "b-1", "documents": []any{}}
		case strings.Contains(q, "query AgentBoardProgrammatic "):
			data["agentBoardProgrammatic"] = map[string]any{"uuid": "b-1", "documentsRoot": "",
				"documentsRepo": map[string]any{"uri": b.repo}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
}

// pjReport is a report with three passing checks (one of them blocking, which is never listed), one skipped, one non-blocking failure and, when blocking, a blocking
// failure with two offences: the three counts differ, so a swapped count shows.
func pjReport(blocking bool) map[string]any {
	results := []any{
		map[string]any{"check": "ids.family", "result": "PASS", "blocking": false},
		map[string]any{"check": "ids.unique", "result": "PASS", "blocking": true},
		map[string]any{"check": "ids.grammar", "result": "PASS", "blocking": false},
		map[string]any{"check": "trace.cycle", "result": "SKIP", "blocking": false},
		map[string]any{"check": "glossary.terms_defined", "result": "FAIL", "blocking": false,
			"offences": []any{map[string]any{"message": "term X undefined"}}},
	}
	if blocking {
		results = append(results, map[string]any{"check": "trace.parent_exists", "result": "FAIL", "blocking": true,
			"offences": []any{map[string]any{"message": "FN-1 → REQ-9 not found"}, map[string]any{"message": "FN-2 → REQ-8 not found"}}})
	}
	return map[string]any{"catalogueVersion": "1.4", "results": results}
}

// pjWorld is a pushed documents checkout holding an element-bearing design and a plain note, and a board that
// answers with report.
func pjWorld(t *testing.T, report map[string]any) (*pjBoard, string, string) {
	t.Helper()
	withStateDir(t)
	bare := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", "--bare", bare).CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v %s", err, out)
	}
	dir := newRepo(t, bare)
	commitFile(t, dir, pjDesign, designDoc)
	commitFile(t, dir, pjPlain, "# Notes\n\nNo ids here.\n")
	for _, args := range [][]string{
		{"config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*"},
		{"push", "-q", "origin", "HEAD:refs/heads/main"},
		{"fetch", "-q", "origin"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	b := &pjBoard{repo: bare, report: report}
	srv := b.serve()
	t.Cleanup(srv.Close)
	return b, dir, srv.URL
}

// TestDocJsonChildProcess is the child: it runs the command named in the environment and exits as the CLI does. Run
// directly, it does nothing.
func TestDocJsonChildProcess(t *testing.T) {
	url := os.Getenv("REARM_TEST_DOCJSON_URL")
	if url == "" {
		return
	}
	c, err := rearm.New(url, "id", "secret", rearm.WithoutTokenExchange())
	if err != nil {
		os.Exit(3)
	}
	apiClient = c
	var args []string
	if err := json.Unmarshal([]byte(os.Getenv("REARM_TEST_DOCJSON_ARGS")), &args); err != nil {
		os.Exit(4)
	}
	cmd := agentDocPublishCmd
	if os.Getenv("REARM_TEST_DOCJSON_CMD") == "element-check" {
		cmd = agentDocElementCheckCmd
	}
	if err := cmd.Flags().Parse(args); err != nil {
		os.Exit(5)
	}
	cmd.Run(cmd, nil)
	os.Exit(0)
}

type pjRun struct {
	stdout, stderr string
	code           int
}

// pjExec runs a doc command ("publish" or "element-check") with args against url in a child process.
func pjExec(t *testing.T, url, command string, args ...string) pjRun {
	t.Helper()
	raw, _ := json.Marshal(args)
	child := exec.Command(os.Args[0], "-test.run=^TestDocJsonChildProcess$")
	child.Env = append(os.Environ(), "REARM_TEST_DOCJSON_URL="+url, "REARM_TEST_DOCJSON_CMD="+command,
		"REARM_TEST_DOCJSON_ARGS="+string(raw))
	var stdout, stderr strings.Builder
	child.Stdout, child.Stderr = &stdout, &stderr
	err := child.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("child: %v", err)
	}
	return pjRun{stdout.String(), stderr.String(), code}
}

func pjPublishArgs(dir, file string, extra ...string) []string {
	spec := "ARCHITECTURE"
	if file == pjPlain {
		spec = "DETAILED_DESIGN"
	}
	return append([]string{"--session", "s-1", "--type", spec, "--task", "t-1", "--file", file, "--repo", dir}, extra...)
}

// pjOneObject asserts stdout is exactly one JSON line holding one object, stderr is empty and the exit code is
// code, and returns the object.
func pjOneObject(t *testing.T, r pjRun, code int) map[string]any {
	t.Helper()
	if r.code != code {
		t.Fatalf("exit %d, want %d\nstdout: %s\nstderr: %s", r.code, code, r.stdout, r.stderr)
	}
	if r.stderr != "" {
		t.Errorf("nothing on stderr with --json, got %q", r.stderr)
	}
	if !strings.HasSuffix(r.stdout, "\n") || strings.Count(r.stdout, "\n") != 1 {
		t.Errorf("stdout is exactly one line, got %q", r.stdout)
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(r.stdout), &obj); err != nil {
		t.Fatalf("stdout is one JSON object: %v\n%q", err, r.stdout)
	}
	return obj
}

// pjRefused asserts a refusal: exit 1, stdout empty, stderr carrying want.
func pjRefused(t *testing.T, r pjRun, want string) {
	t.Helper()
	if r.code != 1 || r.stdout != "" || !strings.Contains(r.stderr, want) {
		t.Errorf("want exit 1, empty stdout, %q on stderr; got exit %d\nstdout: %q\nstderr: %q", want, r.code, r.stdout, r.stderr)
	}
}

func pjLines(v any) []string {
	var out []string
	list, _ := v.([]any)
	for _, l := range list {
		out = append(out, str(l))
	}
	return out
}

// The publish with elements and --json: one object, the release as the server returned it plus checks with the
// verdict, the counts, each blocking offence and the report lines; the report read once; nothing on stderr.
func TestAPublishWithElementsAndJsonPrintsOneObjectCarryingTheChecks(t *testing.T) {
	b, dir, url := pjWorld(t, pjReport(true))
	obj := pjOneObject(t, pjExec(t, url, "publish", pjPublishArgs(dir, pjDesign, "--json")...), 0)
	if obj["uuid"] != "r-new" || obj["version"] != "1" || obj["lifecycle"] != "DRAFT" {
		t.Errorf("the release as the server returned it, got %v", obj)
	}
	checks, _ := obj["checks"].(map[string]any)
	if checks["verdict"] != "FAIL" {
		t.Errorf("a blocking failure is FAIL, got %v", checks["verdict"])
	}
	if !reflect.DeepEqual(checks["counts"], map[string]any{"pass": 3.0, "fail": 2.0, "skip": 1.0}) {
		t.Errorf("counts %v", checks["counts"])
	}
	wantBlocking := []any{
		map[string]any{"check": "trace.parent_exists", "offence": "FN-1 → REQ-9 not found"},
		map[string]any{"check": "trace.parent_exists", "offence": "FN-2 → REQ-8 not found"},
	}
	if !reflect.DeepEqual(checks["blocking"], wantBlocking) {
		t.Errorf("each offence of a blocking failure, and no non-blocking one, got %v", checks["blocking"])
	}
	wantLines := elementCheckSummary(map[string]any{"document": map[string]any{"round": 1.0, "elementChecks": pjReport(true)}})
	if got := pjLines(checks["lines"]); !reflect.DeepEqual(got, wantLines) {
		t.Errorf("lines are the report as the terminal prints it\ngot  %q\nwant %q", got, wantLines)
	}
	if b.publishes != 1 || b.reads != 1 {
		t.Errorf("one publish and one report read, got %d and %d", b.publishes, b.reads)
	}
}

// The publish's own line (the element count) goes into notices under --json.
func TestThePublishLinesAreNoticesUnderJson(t *testing.T) {
	_, dir, url := pjWorld(t, pjReport(false))
	obj := pjOneObject(t, pjExec(t, url, "publish", pjPublishArgs(dir, pjDesign, "--json")...), 0)
	notices := pjLines(obj["notices"])
	if len(notices) != 1 || !strings.HasPrefix(notices[0], "elements: 2 element(s)") {
		t.Errorf("the element count is a notice, got %q", notices)
	}
}

// A document without elements: checks is null, the key present; no report is read; no notices key.
func TestAPublishWithoutElementsCarriesNullChecks(t *testing.T) {
	b, dir, url := pjWorld(t, pjReport(true))
	obj := pjOneObject(t, pjExec(t, url, "publish", pjPublishArgs(dir, pjPlain, "--json")...), 0)
	if v, ok := obj["checks"]; !ok || v != nil {
		t.Errorf("checks: null, got %v (present %v)", v, ok)
	}
	if _, ok := obj["notices"]; ok {
		t.Errorf("no notices when the publish said nothing, got %v", obj["notices"])
	}
	if b.reads != 0 {
		t.Errorf("no report is read for a document without elements, got %d reads", b.reads)
	}
}

// A component-scoped publish (no --task) with elements reads no report, so checks is null.
func TestAPublishWithoutATaskReadsNoReport(t *testing.T) {
	b, dir, url := pjWorld(t, pjReport(true))
	args := []string{"--session", "s-1", "--type", "ARCHITECTURE", "--board", "0e23b4cb-0000-4000-8000-0000000000b1", "--component", "c-1",
		"--file", pjDesign, "--repo", dir, "--json"}
	obj := pjOneObject(t, pjExec(t, url, "publish", args...), 0)
	if v, ok := obj["checks"]; !ok || v != nil || b.reads != 0 {
		t.Errorf("checks: null and no report read, got %v (present %v), %d reads", v, ok, b.reads)
	}
}

// A report that cannot be read after the publish succeeded: said in checks, exit 0, nothing on stderr.
func TestAnUnreadableReportIsSaidInTheObject(t *testing.T) {
	b, dir, url := pjWorld(t, pjReport(true))
	b.refuse = "report"
	obj := pjOneObject(t, pjExec(t, url, "publish", pjPublishArgs(dir, pjDesign, "--json")...), 0)
	checks, _ := obj["checks"].(map[string]any)
	if v, ok := checks["verdict"]; !ok || v != nil || !strings.HasPrefix(str(checks["error"]), "could not read the report: ") ||
		!strings.Contains(str(checks["error"]), "refused by the board: report") {
		t.Errorf("checks {verdict: null, error}, got %v", obj["checks"])
	}
}

// Without --json nothing changes: the compact line alone on stdout, the element count and the report on stderr.
func TestWithoutJsonTheReportStillGoesToStderr(t *testing.T) {
	_, dir, url := pjWorld(t, pjReport(true))
	r := pjExec(t, url, "publish", pjPublishArgs(dir, pjDesign)...)
	if r.code != 0 || r.stdout != "published ARCHITECTURE v1 (draft); release r-new\n" {
		t.Errorf("exit %d, stdout %q", r.code, r.stdout)
	}
	report := strings.Join(elementCheckSummary(map[string]any{"document": map[string]any{"round": 1.0, "elementChecks": pjReport(true)}}), "\n")
	if !strings.HasPrefix(r.stderr, "elements: 2 element(s)") || !strings.HasSuffix(r.stderr, report+"\n") {
		t.Errorf("the element count, then the report, on stderr; got %q", r.stderr)
	}
}

// Without --json, a report that cannot be read is said on stderr, and the publish still exits 0.
func TestWithoutJsonAnUnreadableReportIsSaidOnStderr(t *testing.T) {
	b, dir, url := pjWorld(t, pjReport(true))
	b.refuse = "report"
	r := pjExec(t, url, "publish", pjPublishArgs(dir, pjDesign)...)
	if r.code != 0 || !strings.Contains(r.stderr, "element checks: could not read the report: ") ||
		strings.Contains(r.stdout, "could not read") {
		t.Errorf("exit %d\nstdout %q\nstderr %q", r.code, r.stdout, r.stderr)
	}
}

// A refusal, with --json or without: exit 1, nothing on stdout, the reason on stderr.
func TestARefusedPublishPrintsNothingOnStdout(t *testing.T) {
	for _, extra := range [][]string{{"--json"}, nil} {
		b, dir, url := pjWorld(t, pjReport(true))
		b.refuse = "publish"
		pjRefused(t, pjExec(t, url, "publish", pjPublishArgs(dir, pjDesign, extra...)...), "Error: refused by the board: publish")
		if b.reads != 0 {
			t.Errorf("%v: no report is read after a refusal, got %d", extra, b.reads)
		}
	}
}

// A local error with --json: exit 1, nothing on stdout, the error on stderr, nothing sent.
func TestALocalErrorWithJsonPrintsNothingOnStdout(t *testing.T) {
	b, dir, url := pjWorld(t, pjReport(true))
	pjRefused(t, pjExec(t, url, "publish", pjPublishArgs(dir, "design/missing.md", "--json")...), "rearm: ")
	if b.publishes != 0 {
		t.Errorf("nothing is published, got %d", b.publishes)
	}
}

// publish --check --json: {check: true, checks}, the check's lines as notices, nothing published, nothing on stderr;
// exit 0 when nothing the board blocks on fails.
func TestACheckWithJsonPrintsOneObject(t *testing.T) {
	b, dir, url := pjWorld(t, pjReport(false))
	obj := pjOneObject(t, pjExec(t, url, "publish", pjPublishArgs(dir, pjDesign, "--check", "--json")...), 0)
	checks, _ := obj["checks"].(map[string]any)
	if obj["check"] != true || checks["verdict"] != "WARN" || len(checks["blocking"].([]any)) != 0 {
		t.Errorf("{check: true, checks} with WARN and nothing blocking, got %v", obj)
	}
	wantLines, _ := previewSummary(pjReport(false))
	if got := pjLines(checks["lines"]); !reflect.DeepEqual(got, wantLines) {
		t.Errorf("lines are the preview as the terminal prints it\ngot  %q\nwant %q", got, wantLines)
	}
	notices := pjLines(obj["notices"])
	if len(notices) < 2 || !strings.HasPrefix(notices[0], "elements: ") || !strings.HasPrefix(notices[1], "  defines: ") {
		t.Errorf("the element count and listing are notices, got %q", notices)
	}
	if b.publishes != 0 || b.previews != 1 {
		t.Errorf("a check publishes nothing and previews once, got %d and %d", b.publishes, b.previews)
	}
}

// publish --check --json with a blocking failure: the object still on stdout, nothing on stderr, exit 1.
func TestABlockingCheckWithJsonExitsOneWithTheObject(t *testing.T) {
	_, dir, url := pjWorld(t, pjReport(true))
	obj := pjOneObject(t, pjExec(t, url, "publish", pjPublishArgs(dir, pjDesign, "--check", "--json")...), 1)
	if checks, _ := obj["checks"].(map[string]any); checks["verdict"] != "FAIL" || len(checks["blocking"].([]any)) != 2 {
		t.Errorf("FAIL with the two blocking offences, got %v", obj["checks"])
	}
}

// publish --check --json on a document without element ids: {check: true, checks: null} and the line as a notice.
func TestACheckWithoutIdsWithJsonCarriesNullChecks(t *testing.T) {
	b, dir, url := pjWorld(t, pjReport(true))
	obj := pjOneObject(t, pjExec(t, url, "publish", pjPublishArgs(dir, pjPlain, "--check", "--json")...), 0)
	if v, ok := obj["checks"]; obj["check"] != true || !ok || v != nil {
		t.Errorf("{check: true, checks: null}, got %v", obj)
	}
	if n := pjLines(obj["notices"]); len(n) != 1 || !strings.Contains(n[0], "has no element ids to check") {
		t.Errorf("the line is a notice, got %q", n)
	}
	if b.previews != 0 {
		t.Errorf("nothing to preview, got %d", b.previews)
	}
}

// publish --check without --json: nothing on stdout, the listing and the preview on stderr, exit 1 on a blocking
// failure and 0 otherwise.
func TestACheckWithoutJsonStaysOnStderr(t *testing.T) {
	for _, blocking := range []bool{false, true} {
		_, dir, url := pjWorld(t, pjReport(blocking))
		r := pjExec(t, url, "publish", pjPublishArgs(dir, pjDesign, "--check")...)
		want := 0
		if blocking {
			want = 1
		}
		lines, _ := previewSummary(pjReport(blocking))
		if r.code != want || r.stdout != "" || !strings.HasPrefix(r.stderr, "elements: ") ||
			!strings.HasSuffix(r.stderr, strings.Join(lines, "\n")+"\n") {
			t.Errorf("blocking %v: exit %d (want %d)\nstdout %q\nstderr %q", blocking, r.code, want, r.stdout, r.stderr)
		}
	}
}

// publish --check --json refused by the board: exit 1, nothing on stdout, the reason on stderr.
func TestARefusedCheckWithJsonPrintsNothingOnStdout(t *testing.T) {
	b, dir, url := pjWorld(t, pjReport(true))
	b.refuse = "preview"
	pjRefused(t, pjExec(t, url, "publish", pjPublishArgs(dir, pjDesign, "--check", "--json")...), "Error: refused by the board: preview")
}

// element-check --release --json: one object, the release as the server returned it plus checks; nothing on stderr.
func TestElementCheckOfAReleaseWithJsonPrintsOneObject(t *testing.T) {
	b, _, url := pjWorld(t, pjReport(true))
	obj := pjOneObject(t, pjExec(t, url, "element-check", "--session", "s-1", "--release", "r-1", "--json"), 0)
	doc, _ := obj["document"].(map[string]any)
	checks, _ := obj["checks"].(map[string]any)
	if obj["uuid"] != "r-1" || doc["elementChecks"] == nil || checks["verdict"] != "FAIL" || b.runs != 1 {
		t.Errorf("the release plus checks, got %v (%d runs)", obj, b.runs)
	}
	wantLines := elementCheckSummary(map[string]any{"document": map[string]any{"round": 1.0, "elementChecks": pjReport(true)}})
	if got := pjLines(checks["lines"]); !reflect.DeepEqual(got, wantLines) {
		t.Errorf("lines\ngot  %q\nwant %q", got, wantLines)
	}
}

// element-check --task --json: one JSON array, each release with its checks; a release without a report has
// checks null.
func TestElementCheckOfATaskWithJsonPrintsOneArray(t *testing.T) {
	b, _, url := pjWorld(t, nil)
	target := func(uuid, spec string) map[string]any {
		return map[string]any{"uuid": uuid, "document": map[string]any{"specification": spec, "round": 1, "task": "t-1", "elements": "{}"}}
	}
	b.targets = []any{target("r-a", "ARCHITECTURE"), target("r-d", "DETAILED_DESIGN")}
	r := pjExec(t, url, "element-check", "--session", "s-1", "--task", "t-1", "--json")
	if r.code != 0 || r.stderr != "" || strings.Count(r.stdout, "\n") != 1 {
		t.Fatalf("exit %d\nstdout %q\nstderr %q", r.code, r.stdout, r.stderr)
	}
	var list []map[string]any
	if err := json.Unmarshal([]byte(r.stdout), &list); err != nil || len(list) != 2 {
		t.Fatalf("one array of two releases: %v %q", err, r.stdout)
	}
	for _, rel := range list {
		if v, ok := rel["checks"]; !ok || v != nil {
			t.Errorf("no report: checks null, got %v (present %v)", v, ok)
		}
	}
}

// element-check without --json: the report on stdout, as before, nothing on stderr.
func TestElementCheckWithoutJsonPrintsTheReportOnStdout(t *testing.T) {
	_, _, url := pjWorld(t, pjReport(false))
	r := pjExec(t, url, "element-check", "--session", "s-1", "--release", "r-1")
	want := strings.Join(elementCheckSummary(map[string]any{"document": map[string]any{"round": 1.0, "elementChecks": pjReport(false)}}), "\n") + "\n"
	if r.code != 0 || r.stdout != want || r.stderr != "" {
		t.Errorf("exit %d\nstdout %q\nwant   %q\nstderr %q", r.code, r.stdout, want, r.stderr)
	}
}

// element-check refused, with --json or without: exit 1, nothing on stdout, the reason on stderr.
func TestARefusedElementCheckPrintsNothingOnStdout(t *testing.T) {
	for _, extra := range [][]string{{"--json"}, nil} {
		b, _, url := pjWorld(t, pjReport(true))
		b.refuse = "run"
		args := append([]string{"--session", "s-1", "--release", "r-1"}, extra...)
		pjRefused(t, pjExec(t, url, "element-check", args...), "rearm: checking r-1: refused by the board: run")
	}
}

// The verdict is the task page's rule: a blocking failure FAIL, another failure WARN, else PASS; a blocking failure
// with no offence is still listed.
func TestTheVerdictIsTheTaskPagesRule(t *testing.T) {
	for _, c := range []struct {
		fail, blocking int
		want           string
	}{{0, 0, "PASS"}, {1, 0, "WARN"}, {1, 1, "FAIL"}, {3, 1, "FAIL"}} {
		if got := elementCheckVerdict(c.fail, c.blocking); got != c.want {
			t.Errorf("fail %d blocking %d: %s, want %s", c.fail, c.blocking, got, c.want)
		}
	}
	got := checksObject(map[string]any{"results": []any{
		map[string]any{"check": "x.y", "result": "FAIL", "blocking": true}}}, nil)
	if !reflect.DeepEqual(got["blocking"], []any{map[string]any{"check": "x.y", "offence": nil}}) || got["verdict"] != "FAIL" {
		t.Errorf("a blocking failure with no offence, got %v", got)
	}
	if lines, ok := got["lines"].([]string); !ok || lines == nil {
		t.Errorf("lines is a list, never null, got %#v", got["lines"])
	}
}

// element-check --release --json when the board answers a null release: null on stdout, as before, and no crash.
func TestElementCheckOfANullReleaseStaysNull(t *testing.T) {
	b, _, url := pjWorld(t, pjReport(true))
	b.nullRun = true
	r := pjExec(t, url, "element-check", "--session", "s-1", "--release", "r-1", "--json")
	if r.code != 0 || r.stdout != "null\n" || r.stderr != "" {
		t.Errorf("exit %d\nstdout %q\nstderr %q", r.code, r.stdout, r.stderr)
	}
}

// Each publish starts with no notices: a line kept by an earlier publish in the same process is not carried over.
func TestEachPublishStartsWithNoNotices(t *testing.T) {
	dpWorld(t, []any{dpDoc("r-n1", 1, dpNotes1, false)}, "r-n1")
	compactJson = true
	publishNotices = []string{"left over from an earlier publish"}
	t.Cleanup(func() { publishNotices = nil })
	out, _ := dpPublish(t)
	var rel map[string]any
	if err := json.Unmarshal([]byte(out), &rel); err != nil {
		t.Fatalf("stdout is one JSON object: %v %q", err, out)
	}
	if n := pjLines(rel["notices"]); len(n) != 1 || !strings.HasPrefix(n[0], "republishing ") {
		t.Errorf("only this publish's line, got %q", n)
	}
}
