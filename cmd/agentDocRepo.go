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

// canonicalVcsUri reduces every way of writing one repository to a single string.
//
// This mirrors the server's canonicalisation exactly, and it has to: the publish is refused unless
// the uri the CLI sends and the board's documents repository canonicalise the same way. A git
// remote is configured in whatever form its owner chose -- https, ssh, with or without .git -- and
// none of those is the form an operator typed into the board.
//
//	https://github.com/acme/docs      -> github.com/acme/docs
//	git@github.com:acme/docs.git      -> github.com/acme/docs
//	github:acme/docs                  -> github.com/acme/docs   (tracker shorthand)
//	https://git.example.com/team/docs -> git.example.com/team/docs
func canonicalVcsUri(uri string) string {
	s := strings.TrimSpace(uri)
	if s == "" {
		return s
	}
	s = expandTrackerShorthand(s)
	s = strings.TrimPrefix(s, "git@")
	s = strings.TrimSuffix(s, ".git")
	for _, scheme := range []string{"https://", "http://", "ssh://", "git://"} {
		s = strings.TrimPrefix(s, scheme)
	}
	// Credentials, if the remote carries them. The '@' only counts before the first '/', or a
	// path containing '@' would be truncated.
	if at := strings.Index(s, "@"); at > 0 {
		if slash := strings.Index(s, "/"); slash < 0 || at < slash {
			s = s[at+1:]
		}
	}
	// scp-style host:path becomes host/path. Only the FIRST colon, so a port survives as part of
	// the host exactly as the server treats it.
	s = strings.Replace(s, ":", "/", 1)
	return s
}

// trackerShorthandHosts accepts boards configured with a tracker reference; it does not restrict
// which git hosts work. An unknown prefix is simply not a shorthand.
var trackerShorthandHosts = map[string]string{
	"github":    "github.com",
	"gitlab":    "gitlab.com",
	"bitbucket": "bitbucket.org",
	"codeberg":  "codeberg.org",
}

// expandTrackerShorthand turns `<tracker>:owner/repo` into `<host>/owner/repo`.
//
// Only when the part before the colon is a known prefix AND has no dot. The dot is what keeps
// git@github.com:acme/docs out of here: that colon is the scp separator, not a shorthand.
func expandTrackerShorthand(uri string) string {
	colon := strings.Index(uri, ":")
	if colon <= 0 {
		return uri
	}
	prefix := uri[:colon]
	if strings.ContainsAny(prefix, "./") {
		return uri
	}
	host, ok := trackerShorthandHosts[strings.ToLower(prefix)]
	if !ok {
		return uri
	}
	return host + "/" + strings.TrimPrefix(uri[colon+1:], "/")
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

// gitRemote returns the canonical uri of a checkout's origin, or "".
func gitRemote(dir string) string {
	out, err := git(dir, "config", "--get", "remote.origin.url")
	if err != nil {
		return ""
	}
	return canonicalVcsUri(out)
}

// resolveDocumentsRepo finds the checkout the board's documents live in.
//
// In order: an explicit --repo; the current directory when its origin IS the board's documents
// repository; the path remembered from an earlier --repo for this session. Otherwise it refuses and
// names what it wanted, because every remaining guess is wrong in a way the agent cannot see:
// publishing from the code checkout would pin a commit that never touched the document.
func resolveDocumentsRepo(st *agentSessionState, documentsRepo string) (string, error) {
	want := canonicalVcsUri(documentsRepo)

	if docRepoPath != "" {
		have := gitRemote(docRepoPath)
		if have != "" && have != want {
			return "", fmt.Errorf("--repo %s has remote %s, but this board's documents repository is %s",
				docRepoPath, have, documentsRepo)
		}
		return docRepoPath, nil
	}

	cwd, err := os.Getwd()
	if err == nil && gitRemote(cwd) == want {
		return cwd, nil
	}

	if st != nil && st.DocumentsRepoPath != "" {
		if gitRemote(st.DocumentsRepoPath) == want {
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
