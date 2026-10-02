package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// RD5-8 round 2: a dry run is a success too (ARCHITECTURE round 2, T-1 of run 1). With --json, doc publish --dry-run
// prints one object, {dryRun: true, input, notices}, and nothing on stderr; without --json it prints the input and
// its lines on stderr, as before. The local-state warnings a publish that succeeded can print (the output not
// recorded, the documents repository not remembered) are notices under --json too, and stay on stderr without it.

// pjState files local state for session s-1 (client id c-1); broken makes every later write of it fail, as a
// directory where the write's temporary file goes does, for root too.
func pjState(t *testing.T, broken bool) {
	t.Helper()
	if err := writeAgentState(&agentSessionState{SessionUuid: "s-1", ClientSessionId: "c-1"}); err != nil {
		t.Fatal(err)
	}
	if !broken {
		return
	}
	path, err := agentStatePath("c-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path+".tmp", 0o700); err != nil {
		t.Fatal(err)
	}
}

// pjMerged is what a script capturing both streams (2>&1) gets, parsed as one JSON object.
func pjMerged(t *testing.T, r pjRun) map[string]any {
	t.Helper()
	var obj map[string]any
	if err := json.Unmarshal([]byte(r.stdout+r.stderr), &obj); err != nil {
		t.Fatalf("stdout and stderr together parse as one JSON value: %v\nstdout %q\nstderr %q", err, r.stdout, r.stderr)
	}
	return obj
}

// A dry run with elements and --json: one object on stdout, {dryRun: true, input, notices}, nothing on stderr, so a
// 2>&1 capture parses; the input is what would be sent; the element count is a notice; nothing is sent.
func TestADryRunWithJsonPrintsOneObject(t *testing.T) {
	b, dir, url := pjWorld(t, pjReport(true))
	r := pjExec(t, url, "publish", pjPublishArgs(dir, pjDesign, "--json", "--dry-run")...)
	obj := pjOneObject(t, r, 0)
	pjMerged(t, r)
	if obj["dryRun"] != true {
		t.Errorf("dryRun: true, got %v", obj["dryRun"])
	}
	in, _ := obj["input"].(map[string]any)
	if in["path"] != pjDesign || in["specification"] != "ARCHITECTURE" || in["taskUuid"] != "t-1" || in["elements"] == nil {
		t.Errorf("input is what would be sent, got %v", obj["input"])
	}
	if n := pjLines(obj["notices"]); len(n) != 1 || !strings.HasPrefix(n[0], "elements: 2 element(s)") {
		t.Errorf("the element count is the one notice, got %q", n)
	}
	if b.publishes != 0 || b.reads != 0 {
		t.Errorf("a dry run sends nothing, got %d publishes, %d reads", b.publishes, b.reads)
	}
}

// Without --json a dry run is unchanged: the input alone on stdout, indented, and the element count on stderr.
func TestADryRunWithoutJsonKeepsTheInputAndItsLinesApart(t *testing.T) {
	b, dir, url := pjWorld(t, pjReport(true))
	r := pjExec(t, url, "publish", pjPublishArgs(dir, pjDesign, "--dry-run")...)
	var in map[string]any
	if err := json.Unmarshal([]byte(r.stdout), &in); err != nil || r.code != 0 {
		t.Fatalf("stdout is the input, got exit %d %q (%v)", r.code, r.stdout, err)
	}
	if in["path"] != pjDesign || in["dryRun"] != nil || !strings.HasPrefix(r.stdout, "{\n  ") {
		t.Errorf("the input itself, indented, not the --json object; got %q", r.stdout)
	}
	if r.stderr != "elements: 2 element(s), 0 warning(s)\n" {
		t.Errorf("the element count on stderr, got %q", r.stderr)
	}
	if b.publishes != 0 {
		t.Errorf("a dry run sends nothing, got %d publishes", b.publishes)
	}
}

// The RD5-6 version line under --dry-run --json is a notice of the dry-run object, not a line on stderr.
func TestTheVersionLineIsANoticeOfADryRunWithJson(t *testing.T) {
	b := dpWorld(t, []any{dpDoc("r-n1", 1, dpNotes1, false)}, "r-n1")
	compactJson, docDryRun = true, true
	out, errOut := dpPublish(t)
	var obj map[string]any
	if err := json.Unmarshal([]byte(out), &obj); err != nil || strings.Count(out, "\n") != 1 {
		t.Fatalf("stdout is one JSON line, got %q (%v)", out, err)
	}
	in, _ := obj["input"].(map[string]any)
	if obj["dryRun"] != true || in["path"] != dpNotes1 {
		t.Errorf("{dryRun: true, input} naming the hop's path, got %v", obj)
	}
	if n := pjLines(obj["notices"]); len(n) != 1 || n[0] != "republishing "+dpNotes1+" as a new version of round 1, published in this hop" {
		t.Errorf("the version line is the notice, got %q", n)
	}
	if errOut != "" {
		t.Errorf("nothing on stderr, got %q", errOut)
	}
	if len(b.published) != 0 {
		t.Errorf("a dry run publishes nothing, got %v", b.published)
	}
}

// An index-only dry run with --json prints the same object: {dryRun: true, input}, with no notices.
func TestAnIndexOnlyDryRunWithJsonPrintsOneObject(t *testing.T) {
	b, _, url := pjWorld(t, nil)
	index := filepath.Join(t.TempDir(), "round-1.json")
	if err := os.WriteFile(index, []byte(`{"about": {"specification": "ARCHITECTURE"}, "items": []}`), 0o600); err != nil {
		t.Fatal(err)
	}
	r := pjExec(t, url, "publish", "--session", "s-1", "--type", "BOARD_QUESTIONS", "--task", "t-1",
		"--index-only", "--index", index, "--dry-run", "--json")
	obj := pjOneObject(t, r, 0)
	in, _ := obj["input"].(map[string]any)
	if obj["dryRun"] != true || in["specification"] != "BOARD_QUESTIONS" || in["index"] == nil {
		t.Errorf("{dryRun: true, input} with the index, got %v", obj)
	}
	if _, ok := obj["notices"]; ok {
		t.Errorf("no notices when the dry run said nothing, got %v", obj["notices"])
	}
	if b.publishes != 0 {
		t.Errorf("a dry run sends nothing, got %d", b.publishes)
	}
}

const (
	pjOutputsWarning = "rearm: could not record the published document locally; pass --outputs r-new at sign-off: "
	pjRepoWarning    = "rearm: could not remember the documents repository path: "
)

// A publish that succeeded but could not write its local state: under --json both warnings are notices, after the
// element count and in the order they happen, nothing is on stderr, and the exit code stays 0.
func TestLocalStateWarningsAreNoticesUnderJson(t *testing.T) {
	_, dir, url := pjWorld(t, pjReport(true))
	pjState(t, true)
	r := pjExec(t, url, "publish", pjPublishArgs(dir, pjDesign, "--json")...)
	obj := pjOneObject(t, r, 0)
	pjMerged(t, r)
	n := pjLines(obj["notices"])
	if len(n) != 3 || !strings.HasPrefix(n[0], "elements: ") || !strings.HasPrefix(n[1], pjOutputsWarning) ||
		!strings.HasPrefix(n[2], pjRepoWarning) {
		t.Errorf("the element count, then the two warnings, as notices; got %q", n)
	}
	if checks, _ := obj["checks"].(map[string]any); obj["uuid"] != "r-new" || checks["verdict"] != "FAIL" {
		t.Errorf("the release and its checks as before, got %v", obj)
	}
}

// A dry run that could not remember the documents repository: under --json the warning is a notice of the dry-run
// object; a dry run records no output, so that warning never appears.
func TestALocalStateWarningOfADryRunIsANoticeUnderJson(t *testing.T) {
	_, dir, url := pjWorld(t, pjReport(true))
	pjState(t, true)
	r := pjExec(t, url, "publish", pjPublishArgs(dir, pjDesign, "--json", "--dry-run")...)
	obj := pjOneObject(t, r, 0)
	if n := pjLines(obj["notices"]); obj["dryRun"] != true || len(n) != 2 || !strings.HasPrefix(n[1], pjRepoWarning) {
		t.Errorf("{dryRun: true} with the element count and the repository warning, got %v", obj)
	}
}

// Without --json the two warnings stay on stderr, and stdout is the compact line alone.
func TestWithoutJsonLocalStateWarningsStayOnStderr(t *testing.T) {
	_, dir, url := pjWorld(t, pjReport(true))
	pjState(t, true)
	r := pjExec(t, url, "publish", pjPublishArgs(dir, pjDesign)...)
	if r.code != 0 || r.stdout != "published ARCHITECTURE v1 (draft); release r-new\n" {
		t.Errorf("exit %d, stdout %q", r.code, r.stdout)
	}
	if !strings.Contains(r.stderr, "\n"+pjOutputsWarning) || !strings.Contains(r.stderr, "\n"+pjRepoWarning) {
		t.Errorf("both warnings on stderr, got %q", r.stderr)
	}
}

// The documents repository is still remembered for the session after a publish and after a dry run, as before, and
// not after a refused publish.
func TestThePublishRemembersTheDocumentsRepository(t *testing.T) {
	for _, c := range []struct {
		name, refuse string
		extra        []string
		remembered   bool
	}{
		{"publish", "", []string{"--json"}, true},
		{"dry run", "", []string{"--json", "--dry-run"}, true},
		{"refused", "publish", []string{"--json"}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			b, dir, url := pjWorld(t, pjReport(false))
			b.refuse = c.refuse
			pjState(t, false)
			if r := pjExec(t, url, "publish", pjPublishArgs(dir, pjDesign, c.extra...)...); (r.code == 0) != c.remembered {
				t.Fatalf("exit %d\nstderr %q", r.code, r.stderr)
			}
			st, err := readAgentState("c-1")
			if err != nil || st == nil {
				t.Fatalf("state: %v", err)
			}
			if got := st.DocumentsRepoPath == dir; got != c.remembered {
				t.Errorf("remembered %v, want %v (path %q)", got, c.remembered, st.DocumentsRepoPath)
			}
		})
	}
}
