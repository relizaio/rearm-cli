package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// rearm agent git commit's message and block (task RD5-2): a '---' body line commits and passes the parse check,
// which runs with --no-divider (ARCHITECTURE round 2 s2); a newline in the subject is refused; --no-co-author
// writes the two ReARM trailers and the check proves two (round 2 s3); --json reports trailerBlockOk false on a
// block a hook broke.

// logTrailers is HEAD's trailers as git log reads a commit's own trailers, independent of the helper's check.
func (w *gitWorld) logTrailers() string {
	return gRun(w.t, w.repo, "", "log", "-1", "--format=%(trailers:only,unfold)")
}

// Tester run 1, T-2: a markdown rule in the body made the check report 0 trailers and ask for an amend.
func TestGitCommitBodyDividerLinePassesTheCheck(t *testing.T) {
	w := newGitWorld(t)
	gitCoAuthor = gCoAuthor
	gitMessages = []string{"test: divider", "Notes\n---\nmore notes"}
	w.write("a.txt", "a\n")
	code, out, errOut := w.commit("a.txt")
	if code != 0 || errOut != "" {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	want := "ReARM-Agentic-Session: code-1\nReARM-Agent: " + gAgent + "\nCo-Authored-By: " + gCoAuthor
	if got := w.logTrailers(); got != want {
		t.Fatalf("git log trailers:\n%s\nwant:\n%s", got, want)
	}
	if msg := gRun(t, w.repo, "", "log", "-1", "--format=%B"); !strings.Contains(msg, "\n\nNotes\n---\nmore notes\n\nReARM-Agentic-Session: ") {
		t.Fatalf("message:\n%s", msg)
	}
}

// Tester run 1, T-5: the subject is one line.
func TestGitCommitNewlineInTheSubjectIsRefused(t *testing.T) {
	w := newGitWorld(t)
	gitCoAuthor = gCoAuthor
	w.write("a.txt", "a\n")
	before := w.head()
	for _, subject := range []string{"feat: x\nsecond line", "feat: x\n\nbody in the subject"} {
		gitMessages = []string{subject}
		code, _, errOut := w.commit("a.txt")
		if code != 1 || !strings.Contains(errOut, "the subject is one line; put the rest in a second -m") {
			t.Fatalf("%q: exit %d\n%s", subject, code, errOut)
		}
		if w.head() != before || w.staged() != "" {
			t.Fatalf("%q: a refused commit wrote to the repository", subject)
		}
	}
}

// --no-co-author writes the two ReARM trailers alone, even with a co-author line kept, and the check proves two;
// without it, three; with --co-author as well, refused.
func TestGitCommitNoCoAuthorWritesTwoTrailers(t *testing.T) {
	w := newGitWorld(t)
	two := "ReARM-Agentic-Session: code-1\nReARM-Agent: " + gAgent
	// No co-author line kept, and none needed with the flag.
	gitNoCoAuthor, gitMessages = true, []string{"docs: two lines"}
	w.write("a.txt", "a\n")
	code, out, errOut := w.commit("a.txt")
	if code != 0 {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	if got := w.logTrailers(); got != two {
		t.Fatalf("trailers:\n%s\nwant:\n%s", got, two)
	}
	if strings.Contains(gRun(t, w.repo, "", "log", "-1", "--format=%B"), "Co-Authored-By") {
		t.Fatal("--no-co-author wrote a co-author line")
	}
	// Both flags: refused before anything is written.
	before := w.head()
	gitCoAuthor = gCoAuthor
	w.write("b.txt", "b\n")
	if code, _, errOut := w.commit("b.txt"); code != 1 || !strings.Contains(errOut, "--co-author and --no-co-author together") || w.head() != before {
		t.Fatalf("both flags: exit %d\n%s", code, errOut)
	}
	// Keep a line, then commit with the flag: still two.
	gitNoCoAuthor, gitMessages = false, []string{"feat: three lines"}
	if code, out, errOut := w.commit("b.txt"); code != 0 {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	w.wantBlock("code-1", gAgent)
	gitNoCoAuthor, gitCoAuthor, gitMessages = true, "", []string{"docs: two again"}
	w.write("c.txt", "c\n")
	if code, out, errOut := w.commit("c.txt"); code != 0 || w.logTrailers() != two {
		t.Fatalf("exit %d, trailers %q\n%s\n%s", code, w.logTrailers(), out, errOut)
	}
	// The check proves the two lines it wrote: a hook that adds a co-author line is caught.
	hookPath := filepath.Join(w.repo, ".git", "hooks", "commit-msg")
	if err := os.WriteFile(hookPath, []byte("#!/bin/sh\nprintf 'Co-Authored-By: "+gCoAuthor+"\\n' >> \"$1\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	gitMessages = []string{"docs: hooked"}
	w.write("d.txt", "d\n")
	if code, _, errOut := w.commit("d.txt"); code != 1 || !strings.Contains(errOut, "is not the 2 lines written: git found 3 trailer line(s), not 2") {
		t.Fatalf("hooked: exit %d\n%s", code, errOut)
	}
}

// Tester run 1, T-7: a script reading --json must see a broken block.
func TestGitCommitJsonReportsABrokenBlock(t *testing.T) {
	w := newGitWorld(t)
	hookPath := filepath.Join(w.repo, ".git", "hooks", "commit-msg")
	if err := os.WriteFile(hookPath, []byte("#!/bin/sh\nprintf 'Change-Id: I0123\\n' >> \"$1\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	gitCoAuthor, gitMessages, gitJson = gCoAuthor, []string{"feat: hooked"}, true
	w.write("a.txt", "a\n")
	code, out, errOut := w.commit("a.txt")
	if code != 1 || !strings.Contains(errOut, "Amend it by hand") {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	var got struct {
		Committed      bool   `json:"committed"`
		Sha            string `json:"sha"`
		TrailerBlockOk *bool  `json:"trailerBlockOk"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !got.Committed || got.Sha != w.head() || got.TrailerBlockOk == nil || *got.TrailerBlockOk {
		t.Fatalf("json: %s", out)
	}
}
