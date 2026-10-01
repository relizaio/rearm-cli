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
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Locating the documents repository, and reading git facts out of it.
//
// The agent's working tree is normally the CODE repository. A board's documents live somewhere
// else, often in a different repository entirely, so "read HEAD from wherever I happen to be" would
// pin a code commit onto a document -- a release claiming a commit that never touched the file it
// points at.

// sameRepository decides whether a checkout's remote is the board's documents repository.
//
// A LOCAL HEURISTIC, not a contract. The server decides repository identity by row: it resolves
// whatever uri the CLI sends through the same get-or-create the board used, so equality there is a
// uuid comparison and this function has no say in it. That is why there is no canonicaliser here
// mirroring the server's -- keeping two implementations in step across two codebases is a coupling
// that drifts silently, and did, for ssh:// remotes.
//
// All this has to do is pick the right checkout on this machine, and `--repo` is the escape hatch
// when it guesses wrong. So it compares the trailing host-and-path, which is stable across the
// forms a remote is written in:
//
//	https://github.com/acme/docs        -> github.com/acme/docs
//	git@github.com:acme/docs.git        -> github.com/acme/docs
//	ssh://git@github.com/acme/docs.git  -> github.com/acme/docs
func sameRepository(a, b string) bool {
	na, nb := repoKey(a), repoKey(b)
	return na != "" && na == nb
}

// repoKey reduces a remote to host-and-path for the local comparison above.
func repoKey(uri string) string {
	s := strings.TrimSpace(uri)
	if s == "" {
		return ""
	}
	for _, scheme := range []string{"https://", "http://", "ssh://", "git://", "git+ssh://"} {
		s = strings.TrimPrefix(s, scheme)
	}
	s = strings.TrimSuffix(s, ".git")
	// Credentials, when the remote carries them. Only before the first '/', or a path containing
	// '@' would be truncated.
	if at := strings.Index(s, "@"); at > 0 {
		if slash := strings.Index(s, "/"); slash < 0 || at < slash {
			s = s[at+1:]
		}
	}
	// scp-style host:path becomes host/path; only the first colon, so a port survives.
	s = strings.Replace(s, ":", "/", 1)
	return strings.ToLower(strings.Trim(s, "/"))
}

// git runs a git command in dir and returns trimmed stdout.
func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s in %s: %w", strings.Join(args, " "), dir, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// gitRemote returns a checkout's origin as configured, or "".
//
// Returned RAW: the server resolves it to a repository row, so the shape it is written in does not
// matter there, and the local comparison handles the shapes itself.
func gitRemote(dir string) string {
	out, err := git(dir, "config", "--get", "remote.origin.url")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// resolveDocumentsRepo finds the checkout the board's documents live in.
//
// In order: an explicit --repo; the current directory when its origin IS the board's documents
// repository; the path remembered from an earlier --repo for this session. Otherwise it refuses and
// names what it wanted, because every remaining guess is wrong in a way the agent cannot see:
// publishing from the code checkout would pin a commit that never touched the document.
func resolveDocumentsRepo(st *agentSessionState, documentsRepo string) (string, error) {
	if docRepoPath != "" {
		have := gitRemote(docRepoPath)
		if have != "" && !sameRepository(have, documentsRepo) {
			return "", fmt.Errorf("--repo %s has remote %s, but this board's documents repository is %s",
				docRepoPath, have, documentsRepo)
		}
		return docRepoPath, nil
	}

	cwd, err := os.Getwd()
	if err == nil && sameRepository(gitRemote(cwd), documentsRepo) {
		return cwd, nil
	}

	if st != nil && st.DocumentsRepoPath != "" {
		if sameRepository(gitRemote(st.DocumentsRepoPath), documentsRepo) {
			return st.DocumentsRepoPath, nil
		}
	}

	return "", fmt.Errorf("cannot find a checkout of this board's documents repository (%s). "+
		"Pass --repo <path>; the current directory is not it", documentsRepo)
}

// headFacts is what a document release pins: the commit the files were read at.
type headFacts struct {
	Commit  string
	Message string
	Date    string
}

func readHead(dir string) (headFacts, error) {
	commit, err := git(dir, "rev-parse", "HEAD")
	if err != nil {
		return headFacts{}, err
	}
	message, _ := git(dir, "log", "-1", "--format=%s")
	date, _ := git(dir, "log", "-1", "--format=%cI")
	return headFacts{Commit: commit, Message: message, Date: date}, nil
}

// assertPushed refuses when no remote-tracking branch contains HEAD.
//
// The release pins HEAD, and everything downstream -- the board's link to the file, the next role's
// pinned input, the trailers' attribution -- reads that commit from the remote. A commit only this
// checkout has is one nobody else can fetch, so it is refused rather than published. There is no
// flag to skip it: there is no honest case for pinning a commit the server cannot see.
func assertPushed(dir string) error {
	head, err := git(dir, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	out, err := git(dir, "branch", "-r", "--contains", "HEAD")
	if err != nil {
		return err
	}
	if strings.TrimSpace(out) == "" {
		short := head
		if len(short) > 12 {
			short = short[:12]
		}
		return fmt.Errorf("push the documents repository first: the release will pin commit %s, "+
			"which no remote branch has. Push it (never force-push there: published releases pin "+
			"its commits), then publish", short)
	}
	return nil
}

// assertCommitted refuses when any of the given paths has uncommitted changes.
//
// A document release pins a commit, and the digest is taken from the working tree. If the two
// disagree, the release points at bytes that are not at that commit -- and nothing downstream can
// detect it, because both halves look well-formed.
func assertCommitted(dir string, paths []string) error {
	args := append([]string{"status", "--porcelain", "--"}, paths...)
	out, err := git(dir, args...)
	if err != nil {
		return err
	}
	if strings.TrimSpace(out) != "" {
		var dirty []string
		for _, line := range strings.Split(out, "\n") {
			if f := strings.Fields(line); len(f) > 1 {
				dirty = append(dirty, f[len(f)-1])
			}
		}
		return fmt.Errorf("uncommitted changes in %s: a document release pins a commit, so commit "+
			"them first", strings.Join(dirty, ", "))
	}
	return nil
}
