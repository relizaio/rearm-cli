package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// rearm agent git commit's paths (task RD5-2, ARCHITECTURE round 2 s1): every path is a file name, never a
// pattern. Glob and magic pathspecs name no file here and are refused; a file literally named '*' or ':(top)'
// stages that file alone; anything beginning with '-' is refused even when a file has that name; a deleted file
// the index knows is a path; a directory stages what is under it and the files are printed.

// committedFiles is HEAD's changed paths, sorted as git prints them.
func (w *gitWorld) committedFiles() string {
	return gRun(w.t, w.repo, "", "show", "--name-only", "--format=", "HEAD")
}

// Tester run 1, T-1: each of these staged every change (untracked files included) past the refusal.
func TestGitCommitGlobAndMagicPathspecsAreRefused(t *testing.T) {
	w := newGitWorld(t)
	gitCoAuthor, gitMessages = gCoAuthor, []string{"feat: x"}
	w.write("base.txt", "changed\n")
	w.write("a.txt", "a\n")
	w.write("c1.txt", "c\n")
	before := w.head()
	for _, p := range []string{"*", ":(top)", ":/*", ":(glob)**", "./*", ":!a.txt", ":/", "?.txt", "[ab].txt", "*.txt", ":(icase)A.TXT"} {
		code, _, errOut := w.commit(p)
		if code != 1 || !strings.Contains(errOut, "name the files: "+p+" is not a file here") {
			t.Fatalf("path %q: exit %d, stderr:\n%s", p, code, errOut)
		}
		if w.head() != before || w.staged() != "" {
			t.Fatalf("path %q: a refused commit wrote to the repository", p)
		}
	}
}

// A file whose name looks like a pattern is staged as that file, and nothing else is: git add and git commit run
// with --literal-pathspecs, so neither reads it as a pattern.
func TestGitCommitPatternLikeFileNamesAreStagedLiterally(t *testing.T) {
	w := newGitWorld(t)
	gitCoAuthor = gCoAuthor
	w.write("base.txt", "changed\n")
	w.write("stray.txt", "s\n")
	for _, name := range []string{"*", ":(top)"} {
		gitMessages = []string{"feat: the file named " + name}
		w.write(name, "literal\n")
		code, out, errOut := w.commit(name)
		if code != 0 {
			t.Fatalf("%q: exit %d\n%s\n%s", name, code, out, errOut)
		}
		if got := w.committedFiles(); got != name {
			t.Fatalf("%q: committed %q, want only that file", name, got)
		}
		if !strings.Contains(out, "ran: git --literal-pathspecs add -- '"+name+"'\n") {
			t.Fatalf("%q: printed:\n%s", name, out)
		}
	}
	if st := gRun(t, w.repo, "", "status", "--porcelain"); !strings.Contains(st, "M base.txt") || !strings.Contains(st, "?? stray.txt") {
		t.Fatalf("the other changes should be left alone:\n%s", st)
	}
}

// Tester run 1, T-3: -u and --update (and any argument beginning with '-') are refused, even when a file of that
// name exists, so the refusal is the '-' rule and not only the missing file.
func TestGitCommitDashArgumentsAreRefused(t *testing.T) {
	w := newGitWorld(t)
	gitCoAuthor, gitMessages = gCoAuthor, []string{"feat: x"}
	w.write("base.txt", "changed\n")
	before := w.head()
	for _, withFile := range []bool{false, true} {
		for _, p := range []string{"-u", "--update", "-A", "--all", "-x"} {
			if withFile {
				w.write(p, "a file so named\n")
			}
			code, _, errOut := w.commit(p)
			if code != 1 || !strings.Contains(errOut, "name the files: "+p+" is not a file here (an argument beginning with - is refused)") {
				t.Fatalf("%q (file exists: %v): exit %d, stderr:\n%s", p, withFile, code, errOut)
			}
			if w.head() != before || w.staged() != "" {
				t.Fatalf("%q (file exists: %v): a refused commit wrote to the repository", p, withFile)
			}
		}
	}
}

// A file deleted from the worktree is still a path: the index knows it.
func TestGitCommitDeletedFileIsAPath(t *testing.T) {
	w := newGitWorld(t)
	gitCoAuthor, gitMessages = gCoAuthor, []string{"chore: drop base"}
	if err := os.Remove(filepath.Join(w.repo, "base.txt")); err != nil {
		t.Fatal(err)
	}
	if code, out, errOut := w.commit("base.txt"); code != 0 {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	if got := gRun(t, w.repo, "", "show", "--name-status", "--format=", "HEAD"); got != "D\tbase.txt" {
		t.Fatalf("committed %q", got)
	}
}

// A directory stages what is under it, the one widening allowed, and the helper prints the files it staged.
func TestGitCommitDirectoryPrintsTheFilesItStaged(t *testing.T) {
	w := newGitWorld(t)
	gitCoAuthor, gitMessages = gCoAuthor, []string{"feat: sub"}
	w.write("sub/b.txt", "b\n")
	w.write("sub/deep/c.txt", "c\n")
	w.write("a.txt", "a\n")
	code, out, errOut := w.commit("sub", "a.txt")
	if code != 0 {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	if !strings.Contains(out, "directory sub staged: sub/b.txt sub/deep/c.txt\n") || strings.Contains(out, "directory a.txt") {
		t.Fatalf("printed:\n%s", out)
	}
	if got := w.committedFiles(); got != "a.txt\nsub/b.txt\nsub/deep/c.txt" {
		t.Fatalf("committed %q", got)
	}
	// --json names them too.
	gitJson, gitMessages = true, []string{"feat: sub again"}
	w.write("sub/d.txt", "d\n")
	code, out, errOut = w.commit("sub/")
	if code != 0 {
		t.Fatalf("json: exit %d\n%s\n%s", code, out, errOut)
	}
	var got struct {
		Directories map[string][]string `json:"directories"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if files := got.Directories["sub/"]; len(files) != 1 || files[0] != "sub/d.txt" {
		t.Fatalf("json directories %v", got.Directories)
	}
}
