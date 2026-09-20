package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ---------- canonical uris ----------

func TestCanonicalVcsUriMatchesEveryFormOfOneRepository(t *testing.T) {
	// This has to agree with the server's canonicaliser exactly: the publish is refused unless the
	// uri the CLI sends and the board's documents repository reduce to the same string. A git
	// remote is configured in whatever form its owner chose, and none of those is what an operator
	// typed into the board.
	for _, form := range []string{
		"https://github.com/acme/docs",
		"http://github.com/acme/docs",
		"https://github.com/acme/docs.git",
		"git@github.com:acme/docs.git",
		"git@github.com:acme/docs",
		"ssh://git@github.com/acme/docs.git",
		"github:acme/docs",
		"  github:acme/docs  ",
	} {
		if got := canonicalVcsUri(form); got != "github.com/acme/docs" {
			t.Errorf("%q canonicalised to %q", form, got)
		}
	}
}

func TestCanonicalVcsUriWorksForAnyHost(t *testing.T) {
	// Nothing here is GitHub-specific; a self-hosted repository has no tracker shorthand and must
	// not need one.
	if got := canonicalVcsUri("https://git.example.com/team/docs"); got != "git.example.com/team/docs" {
		t.Errorf("self-hosted https: %q", got)
	}
	if got := canonicalVcsUri("git@git.example.com:team/docs.git"); got != "git.example.com/team/docs" {
		t.Errorf("self-hosted ssh: %q", got)
	}
	// An unknown prefix is not a shorthand and is left to be canonicalised as the uri it looks
	// like, rather than refused.
	if got := canonicalVcsUri("mytracker:acme/docs"); got != "mytracker/acme/docs" {
		t.Errorf("unknown prefix: %q", got)
	}
}

func TestScpStyleHostIsNotMistakenForAShorthand(t *testing.T) {
	// "word colon path" is both an scp remote and a tracker shorthand. The dot in the host tells
	// them apart; confusing them would mangle every ssh remote an agent has.
	if got := canonicalVcsUri("git@github.com:acme/docs"); got != "github.com/acme/docs" {
		t.Errorf("scp form: %q", got)
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
	// A heading that is not a finding must not be read as one, or every document with a "## Summary"
	// section would be refused.
	md := "## Summary\n\nWe looked at it.\n\n### F-1: the actual finding\n"
	if err := crossCheckIds(indexWith("F-1"), md); err != nil {
		t.Errorf("prose headings should be ignored: %v", err)
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
		"github:acme/docs",
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
	got, err := resolveDocumentsRepo(st, "github:acme/docs")
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

	got := takePendingOutputs("c-1", "task-a")
	if len(got) != 1 || got[0] != "rel-1" {
		t.Fatalf("expected one release for task-a, got %v", got)
	}
	if again := takePendingOutputs("c-1", "task-a"); len(again) != 0 {
		t.Errorf("outputs should be taken once, got %v", again)
	}
	if other := takePendingOutputs("c-1", "task-b"); len(other) != 1 {
		t.Errorf("another task's outputs must survive, got %v", other)
	}
}
