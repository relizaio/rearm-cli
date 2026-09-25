package cmd

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ---------- local repository matching ----------

func TestSameRepositoryAcrossTheFormsARemoteIsWrittenIn(t *testing.T) {
	// A local heuristic for picking the right checkout, NOT a mirror of the server's identity
	// rule: the server resolves whatever uri it is sent to a repository row and compares uuids.
	// That is why this can be approximate and why --repo exists when it guesses wrong.
	board := "https://github.com/acme/docs"
	for _, remote := range []string{
		"https://github.com/acme/docs",
		"https://github.com/acme/docs.git",
		"git@github.com:acme/docs.git",
		"ssh://git@github.com/acme/docs.git",
		"git://github.com/acme/docs",
		"https://github.com/acme/docs/",
	} {
		if !sameRepository(remote, board) {
			t.Errorf("%q should match the board's %q", remote, board)
		}
	}
}

func TestSameRepositoryWorksForAnyHost(t *testing.T) {
	if !sameRepository("git@git.example.com:team/docs.git", "https://git.example.com/team/docs") {
		t.Error("self-hosted repositories must match across forms")
	}
}

func TestDifferentRepositoriesDoNotMatch(t *testing.T) {
	if sameRepository("https://github.com/acme/code", "https://github.com/acme/docs") {
		t.Error("different repositories must not match")
	}
	if sameRepository("", "https://github.com/acme/docs") {
		t.Error("an unknown remote must not match anything")
	}
}

// ---------- the index / markdown cross-check ----------

func indexWith(ids ...string) map[string]interface{} {
	var findings []interface{}
	for _, id := range ids {
		findings = append(findings, map[string]interface{}{"id": id})
	}
	return map[string]interface{}{"findings": findings}
}

func TestCrossCheckAcceptsAMatchingPair(t *testing.T) {
	md := "# Round 2\n\n### F-1: something\ntext\n\n### F-2 - something else\nmore\n"
	if err := crossCheckIds(indexWith("F-1", "F-2"), md); err != nil {
		t.Errorf("expected a match, got %v", err)
	}
}

func TestCrossCheckCatchesAFindingWithNoHeading(t *testing.T) {
	// The index promises the next hop a finding it cannot read about. Caught here because only the
	// client has the markdown -- the server never sees it.
	md := "### F-1: something\n"
	err := crossCheckIds(indexWith("F-1", "F-2"), md)
	if err == nil || !strings.Contains(err.Error(), "F-2") {
		t.Errorf("expected F-2 to be named, got %v", err)
	}
}

func TestCrossCheckCatchesAHeadingWithNoIndexEntry(t *testing.T) {
	// The opposite direction, and the worse one: a finding written up but absent from the index is
	// invisible to routing, which is the same silent loss the carry-forward rule exists to stop.
	md := "### F-1: something\n\n### F-9: written up but never filed\n"
	err := crossCheckIds(indexWith("F-1"), md)
	if err == nil || !strings.Contains(err.Error(), "F-9") {
		t.Errorf("expected F-9 to be named, got %v", err)
	}
}

func TestCrossCheckAcceptsTheHeadingFormsTheRolePromptsDescribe(t *testing.T) {
	for _, heading := range []string{
		"### F-1: colon form\n",
		"## F-1 - dash form\n",
		"#### F-1 – en dash form\n",
		"# F-1: h1\n",
	} {
		if err := crossCheckIds(indexWith("F-1"), heading); err != nil {
			t.Errorf("heading %q should be recognised: %v", heading, err)
		}
	}
}

func TestCrossCheckIgnoresOrdinaryProse(t *testing.T) {
	// A heading that is not a finding must not be read as one. The colon forms matter most: an
	// earlier rule took any word before a colon as an id, so "## Context: what was reviewed"
	// became finding "Context" and refused a document that was perfectly correct. Being too
	// permissive here blocks real work.
	for _, md := range []string{
		"## Summary\n\nWe looked at it.\n\n### F-1: the actual finding\n",
		"## Context: what was reviewed\n\n### F-1: the actual finding\n",
		"## Scope - files touched\n\n### F-1: the actual finding\n",
		"# Review: round 2\n\n### F-1: the actual finding\n",
		"## Note: F-1 is discussed below\n\n### F-1: the actual finding\n",
	} {
		if err := crossCheckIds(indexWith("F-1"), md); err != nil {
			t.Errorf("prose heading should be ignored in %q: %v", md, err)
		}
	}
}

// ---------- repository resolution ----------

func newRepo(t *testing.T, remote string) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "t"},
		{"config", "remote.origin.url", remote},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	return dir
}

func commitFile(t *testing.T, dir, rel, body string) {
	t.Helper()
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", rel}, {"commit", "-q", "-m", "add " + rel}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
}

func TestRepoResolutionRefusesACheckoutOfTheWrongRepository(t *testing.T) {
	// The failure this whole resolution exists to prevent: publishing from the CODE checkout would
	// pin a commit that never touched the document, and nothing downstream could tell.
	docRepoPath = newRepo(t, "https://github.com/acme/code")
	defer func() { docRepoPath = "" }()
	_, err := resolveDocumentsRepo(nil, "https://github.com/acme/docs")
	if err == nil || !strings.Contains(err.Error(), "acme/docs") {
		t.Errorf("expected a refusal naming the wanted repository, got %v", err)
	}
}

func TestRepoResolutionAcceptsAMatchingCheckoutInAnyRemoteForm(t *testing.T) {
	// The board holds a tracker shorthand or an https url; the checkout's remote may be ssh. They
	// are the same repository and must resolve.
	docRepoPath = newRepo(t, "git@github.com:acme/docs.git")
	defer func() { docRepoPath = "" }()
	for _, configured := range []string{
		"https://github.com/acme/docs",
		"ssh://git@github.com/acme/docs.git",
		"git@github.com:acme/docs.git",
	} {
		if _, err := resolveDocumentsRepo(nil, configured); err != nil {
			t.Errorf("board configured as %q should match an ssh checkout: %v", configured, err)
		}
	}
}

func TestRepoResolutionFallsBackToTheRememberedPath(t *testing.T) {
	dir := newRepo(t, "https://github.com/acme/docs")
	docRepoPath = ""
	st := &agentSessionState{DocumentsRepoPath: dir}
	got, err := resolveDocumentsRepo(st, "https://github.com/acme/docs")
	if err != nil || got != dir {
		t.Errorf("expected the remembered path, got %q (%v)", got, err)
	}
}

func TestUncommittedChangesAreRefused(t *testing.T) {
	// A release pins a commit while the digest is taken from the working tree. If they disagree
	// the release points at bytes that were never at that commit, and both halves look fine.
	dir := newRepo(t, "https://github.com/acme/docs")
	commitFile(t, dir, "findings/a/round-1.md", "### F-1: x\n")
	if err := assertCommitted(dir, []string{"findings/a/round-1.md"}); err != nil {
		t.Fatalf("a clean file should pass: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "findings/a/round-1.md"), []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := assertCommitted(dir, []string{"findings/a/round-1.md"})
	if err == nil || !strings.Contains(err.Error(), "round-1.md") {
		t.Errorf("expected the dirty file to be named, got %v", err)
	}
}

func TestHeadFactsComeFromTheDocumentsCheckout(t *testing.T) {
	dir := newRepo(t, "https://github.com/acme/docs")
	commitFile(t, dir, "findings/a/round-1.md", "### F-1: x\n")
	head, err := readHead(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(head.Commit) < 7 {
		t.Errorf("expected a commit sha, got %q", head.Commit)
	}
	if !strings.Contains(head.Message, "round-1.md") {
		t.Errorf("expected the subject, got %q", head.Message)
	}
	if head.Date == "" {
		t.Error("expected a commit date")
	}
}

// ---------- pending outputs ----------

func TestPendingOutputsAreKeyedByTaskAndTakenOnce(t *testing.T) {
	// Keyed by task because a session works several tasks in its life, and taken because the hop is
	// closing: leaving them would offer the same documents at the next hop, where the server
	// refuses them for falling outside the assignment window.
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	st := &agentSessionState{SessionUuid: "u-1", ClientSessionId: "c-1"}
	if err := writeAgentState(st); err != nil {
		t.Fatal(err)
	}
	rememberPendingOutput(st, "task-a", "rel-1")
	rememberPendingOutput(st, "task-a", "rel-1") // a retry returns the same release
	rememberPendingOutput(st, "task-b", "rel-2")

	// Looked up by SESSION UUID, which is what every board command takes and what the orientation
	// documents. State files are named by client id, so a uuid lookup that did not scan found
	// nothing and silently behaved as though the session had no state: publish recorded no output
	// and the sign-off sent none. The original test passed because it used the client id.
	if len(takePendingOutputsProbe("u-1", "task-b")) != 1 {
		t.Fatal("state must be findable by session uuid, not only by client session id")
	}

	got := takePendingOutputs("c-1", "task-a")
	if len(got) != 1 || got[0] != "rel-1" {
		t.Fatalf("expected one release for task-a, got %v", got)
	}
	if again := takePendingOutputs("c-1", "task-a"); len(again) != 0 {
		t.Errorf("outputs should be taken once, got %v", again)
	}
}

// takePendingOutputsProbe is takePendingOutputs, named so the uuid assertion above reads as what it
// is: the same call the sign-off path makes, with the id an agent actually holds.
func takePendingOutputsProbe(sessionRef, taskUuid string) []string {
	return takePendingOutputs(sessionRef, taskUuid)
}

func TestPublishWithoutAFileAsksTheBoard(t *testing.T) {
	var asked []string
	queryDocumentPath = func(board, spec, task, component string) (string, error) {
		asked = append(asked, board, spec, task, component)
		return "docs/detailed_design/1a2b3c4d/round-2.md", nil
	}
	defer func() { queryDocumentPath = defaultQueryDocumentPath }()
	docFile, docTask, docComponent = "", "1a2b3c4d-0000-0000-0000-000000000000", ""
	defer func() { docFile, docTask, docComponent = "", "", "" }()

	got, err := documentFile(map[string]interface{}{"uuid": "board-1"}, "DETAILED_DESIGN")
	if err != nil {
		t.Fatal(err)
	}
	if got != "docs/detailed_design/1a2b3c4d/round-2.md" {
		t.Errorf("the server's path is used, got %q", got)
	}
	if want := []string{"board-1", "DETAILED_DESIGN", "1a2b3c4d-0000-0000-0000-000000000000", ""}; strings.Join(asked, "|") != strings.Join(want, "|") {
		t.Errorf("asked with %v, want %v", asked, want)
	}
}

func TestAFileBypassesTheBoard(t *testing.T) {
	queryDocumentPath = func(_, _, _, _ string) (string, error) {
		t.Fatal("--file must not ask the board")
		return "", nil
	}
	defer func() { queryDocumentPath = defaultQueryDocumentPath }()
	docFile = "impl/mine.md"
	defer func() { docFile = "" }()
	if got, err := documentFile(map[string]interface{}{"uuid": "board-1"}, "DETAILED_DESIGN"); err != nil || got != "impl/mine.md" {
		t.Errorf("got %q, %v", got, err)
	}
}

func TestTheServersRefusalIsPassedOnWithTheWayRoundIt(t *testing.T) {
	_, err := pathFromResponse(nil, errors.New("the DETAILED_DESIGN path on board b (docs/{type}/{task}/round-{round}.md) is per task; name the task"), "DETAILED_DESIGN")
	if err == nil || !strings.Contains(err.Error(), "name the task") || !strings.Contains(err.Error(), "pass --file") {
		t.Errorf("the refusal and the way round it are both said: %v", err)
	}
	if _, err := pathFromResponse(map[string]interface{}{"agentBoardProgrammatic": map[string]interface{}{"documentPath": nil}}, nil, "GLOSSARY"); err == nil {
		t.Error("an empty answer is an error, not an empty path")
	}
	got, err := pathFromResponse(map[string]interface{}{"agentBoardProgrammatic": map[string]interface{}{"documentPath": "docs/glossary/api.md"}}, nil, "GLOSSARY")
	if err != nil || got != "docs/glossary/api.md" {
		t.Errorf("got %q, %v", got, err)
	}
}
