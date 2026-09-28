package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	rearm "github.com/relizaio/rearm-client-go"
)

// Running notes (task RD3-9): a dated line in notes/<role>.md, committed alone with the session's trailers
// and pushed; a rejected push is merged and pushed again, never forced; tracing refuses; tail reads the end.

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func cloneOf(t *testing.T, remote, dir string) string {
	t.Helper()
	if out, err := exec.Command("git", "clone", "-q", remote, dir).CombinedOutput(); err != nil {
		t.Fatalf("clone: %v\n%s", err, out)
	}
	gitIn(t, dir, "config", "user.name", "Notes Test")
	gitIn(t, dir, "config", "user.email", "notes@test.io")
	gitIn(t, dir, "config", "commit.gpgsign", "false")
	return dir
}

// notesWorld: a bare remote with one commit, a checkout of it, and a server answering the board and the session.
func notesWorld(t *testing.T) (remote, work string) {
	t.Helper()
	withStateDir(t)
	root := t.TempDir()
	remote = filepath.Join(root, "docs.git")
	if out, err := exec.Command("git", "init", "-q", "--bare", "-b", "main", remote).CombinedOutput(); err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	work = cloneOf(t, remote, filepath.Join(root, "work"))
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("docs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, work, "add", "README.md")
	gitIn(t, work, "commit", "-q", "-m", "start")
	gitIn(t, work, "push", "-q", "origin", "HEAD:main")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Query string `json:"query"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		data := map[string]any{}
		if strings.Contains(req.Query, "sessionProgrammatic") {
			data["sessionProgrammatic"] = map[string]any{"uuid": "s-1", "clientSessionId": "notes-client-1", "agent": "agent-9"}
		} else {
			data["agentBoardProgrammatic"] = map[string]any{"uuid": "b-1", "documentsRoot": "boards/x/",
				"documentsRepo": map[string]any{"uri": remote}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	t.Cleanup(srv.Close)
	c, err := rearm.New(srv.URL, "id", "secret", rearm.WithoutTokenExchange())
	if err != nil {
		t.Fatal(err)
	}
	apiClient = c
	docRepoPath = work
	t.Cleanup(func() { apiClient, docRepoPath = nil, "" })
	return remote, work
}

var noteTime = time.Date(2026, 9, 28, 15, 4, 0, 0, time.UTC)

func TestAppendCommitsTheLineWithTheTrailersAndPushes(t *testing.T) {
	remote, work := notesWorld(t)
	out, err := appendNote("b-1", "Coder", "s-1", "the harness needs a local CLI\nfor wait", noteTime)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "appended to boards/x/notes/coder.md (") || !strings.HasSuffix(out, "), pushed") {
		t.Errorf("printed %q", out)
	}
	b, _ := os.ReadFile(filepath.Join(work, "boards/x/notes/coder.md"))
	if string(b) != "- 2026-09-28 15:04 UTC: the harness needs a local CLI for wait\n" {
		t.Errorf("file: %q", b)
	}
	msg := gitIn(t, remote, "log", "-1", "--format=%B", "main")
	for _, want := range []string{"notes(coder): the harness needs a local CLI for wait", "ReARM-Agentic-Session: notes-client-1", "ReARM-Agent: agent-9"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the pushed commit lacks %q:\n%s", want, msg)
		}
	}
	if files := gitIn(t, remote, "show", "--name-only", "--format=", "main"); files != "boards/x/notes/coder.md" {
		t.Errorf("the commit holds only the notes file, got %q", files)
	}
}

func TestARejectedPushIsMergedAndPushedAgainNeverForced(t *testing.T) {
	remote, work := notesWorld(t)
	other := cloneOf(t, remote, filepath.Join(filepath.Dir(work), "other"))
	if err := os.WriteFile(filepath.Join(other, "elsewhere.md"), []byte("theirs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, other, "add", "elsewhere.md")
	gitIn(t, other, "commit", "-q", "-m", "theirs first")
	gitIn(t, other, "push", "-q", "origin", "HEAD:main")

	if _, err := appendNote("b-1", "coder", "s-1", "ours second", noteTime); err != nil {
		t.Fatal(err)
	}
	log := gitIn(t, remote, "log", "--format=%s", "main")
	if !strings.Contains(log, "theirs first") || !strings.Contains(log, "notes(coder): ours second") {
		t.Errorf("both commits kept on the remote:\n%s", log)
	}
	if parents := strings.Fields(gitIn(t, remote, "log", "-1", "--format=%P", "main")); len(parents) != 2 {
		t.Errorf("a merge, not a rewrite: parents %v", parents)
	}
}

func TestTracingRefusesToWriteNotes(t *testing.T) {
	notesWorld(t)
	t.Setenv("SHELLOPTS", "braceexpand:hashall:xtrace")
	if _, err := appendNote("b-1", "coder", "s-1", "x", noteTime); err == nil || !strings.Contains(err.Error(), "tracing") {
		t.Errorf("shell tracing: %v", err)
	}
	t.Setenv("SHELLOPTS", "braceexpand:hashall")
	t.Setenv("GIT_TRACE", "1")
	if _, err := appendNote("b-1", "coder", "s-1", "x", noteTime); err == nil || !strings.Contains(err.Error(), "tracing") {
		t.Errorf("git tracing: %v", err)
	}
}

func TestTailReadsTheEndOfTheRolesNotes(t *testing.T) {
	_, work := notesWorld(t)
	lines, rel, err := tailNotes("b-1", "tester", "", 5)
	if err != nil || len(lines) != 0 || rel != "boards/x/notes/tester.md" {
		t.Fatalf("absent: %v %q %v", lines, rel, err)
	}
	for i := 0; i < 7; i++ {
		if _, err := appendNote("b-1", "tester", "s-1", "line "+string(rune('a'+i)), noteTime); err != nil {
			t.Fatal(err)
		}
	}
	lines, _, _ = tailNotes("b-1", "tester", "", 3)
	if len(lines) != 3 || !strings.HasSuffix(lines[2], ": line g") || !strings.HasSuffix(lines[0], ": line e") {
		t.Errorf("tail 3: %v", lines)
	}
	_ = work
}
