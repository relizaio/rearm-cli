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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// The current session (task RD5-4): one per repository AND instance, ARCHITECTURE round 2 §1. The repository is
// the checkout the command runs in (git's top level, else the working directory); the instance is the server the
// verb is about to call, as the credentials resolve it (the URI's scheme and host). A code worktree on a
// two-instance host therefore holds a code session for the controlling instance and, once set, the board session
// for the board's instance, and a verb only ever falls back to the entry of the instance it calls.
//
// Kept under $XDG_STATE_HOME/rearm/current-sessions/<digest of the top-level path>.json, one file per checkout, as
// task push keeps its --base (a separate file: that one is rewritten whole by task push).

// currentSessionEntry is one instance's current session for a repository. The client id and the agent are kept so
// verify's fallback and the git helpers need no read of an instance the verb is not calling (round 2 §2).
type currentSessionEntry struct {
	SessionUuid     string `json:"sessionUuid"`
	ClientSessionId string `json:"clientSessionId,omitempty"`
	AgentUuid       string `json:"agentUuid,omitempty"`
	Instance        string `json:"instance"`
	RecordedAt      string `json:"recordedAt,omitempty"`
	// "session open" or "session current --set".
	RecordedBy string `json:"recordedBy,omitempty"`
}

type currentSessionsFile struct {
	Path     string                          `json:"path"`
	Sessions map[string]*currentSessionEntry `json:"sessions"`
}

// sessionRequiredByHand are the verbs that refuse a missing --session by hand rather than through cobra's
// required-flag annotation, with the exit code of that refusal (task verify refuses with 2). Keyed by command, not
// by flag annotation: the verbs register their flags in init functions of files that run after this one.
var sessionRequiredByHand = map[*cobra.Command]int{}

// sessionFallbackSkip are the verbs whose missing --session means something of its own, so no current session is
// put in its place: task commission without one commissions as a person.
var sessionFallbackSkip = map[string]string{
	"rearm agent task commission": "without --session the key commissions as a person",
}

// fallbackInUse is the entry a verb of this invocation took its --session from, or nil; read when the server
// answers that the session is closed, to clear it.
var fallbackInUse *currentSessionUse

type currentSessionUse struct {
	repo  string
	entry currentSessionEntry
}

// markSessionRequired records that the verb refuses without --session, with the exit code it refuses with.
func markSessionRequired(c *cobra.Command, exitCode int) { sessionRequiredByHand[c] = exitCode }

// sessionRequiredExit is the exit code the verb refuses a missing --session with, or 0 when it runs without one.
func sessionRequiredExit(cmd *cobra.Command, f *pflag.Flag) int {
	if code, ok := sessionRequiredByHand[cmd]; ok {
		return code
	}
	if v := f.Annotations[cobra.BashCompOneRequiredFlag]; len(v) > 0 && v[0] == "true" {
		return 1
	}
	return 0
}

// repositoryKey is the checkout the command runs in: git's top level, else the working directory.
func repositoryKey() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	c := exec.Command("git", "rev-parse", "--show-toplevel")
	c.Dir = cwd
	if out, err := c.Output(); err == nil {
		if top := strings.TrimSpace(string(out)); top != "" {
			return filepath.Clean(top), nil
		}
	}
	if real, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = real
	}
	return filepath.Clean(cwd), nil
}

// instanceKey is the server a URI names: its scheme and host, lower case, without the scheme's default port and
// without any path. A URI without a scheme is read as https, as a bare host is. Empty for an empty URI.
func instanceKey(uri string) string {
	u := strings.TrimSpace(uri)
	if u == "" {
		return ""
	}
	if !strings.Contains(u, "://") {
		u = "https://" + u
	}
	parsed, err := url.Parse(u)
	if err != nil || parsed.Host == "" {
		return strings.ToLower(strings.TrimRight(strings.TrimSpace(uri), "/"))
	}
	scheme := strings.ToLower(parsed.Scheme)
	host := strings.ToLower(parsed.Host)
	if (scheme == "https" && strings.HasSuffix(host, ":443")) || (scheme == "http" && strings.HasSuffix(host, ":80")) {
		host = host[:strings.LastIndex(host, ":")]
	}
	return scheme + "://" + host
}

// currentInstance is the instance this invocation's credentials point at.
func currentInstance() string { return instanceKey(rearmUri) }

func currentSessionsDir() (string, error) {
	dir, err := agentStateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(dir), "current-sessions"), nil
}

func currentSessionsPath(repo string) (string, error) {
	dir, err := currentSessionsDir()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(filepath.Clean(repo)))
	return filepath.Join(dir, hex.EncodeToString(sum[:])[:32]+".json"), nil
}

// readCurrentSessions is the repository's entries; an absent file, or one for another path, is none.
func readCurrentSessions(repo string) (*currentSessionsFile, error) {
	empty := &currentSessionsFile{Path: filepath.Clean(repo), Sessions: map[string]*currentSessionEntry{}}
	path, err := currentSessionsPath(repo)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return empty, nil
		}
		return nil, err
	}
	var f currentSessionsFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("current-session file %s is not readable JSON: %w", path, err)
	}
	if f.Path != filepath.Clean(repo) {
		return empty, nil
	}
	if f.Sessions == nil {
		f.Sessions = map[string]*currentSessionEntry{}
	}
	return &f, nil
}

func writeCurrentSessions(f *currentSessionsFile) error {
	path, err := currentSessionsPath(f.Path)
	if err != nil {
		return err
	}
	if len(f.Sessions) == 0 {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	body, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// currentSessionFor is the repository's entry on the instance, or nil. Only that instance's entry: another
// instance's is never an answer (round 2 §1).
func currentSessionFor(repo, instance string) *currentSessionEntry {
	if repo == "" || instance == "" {
		return nil
	}
	f, err := readCurrentSessions(repo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "rearm: %v\n", err)
		return nil
	}
	return f.Sessions[instance]
}

// recordCurrentSession makes the entry the repository's current session on its instance, replacing any before.
func recordCurrentSession(repo string, e currentSessionEntry) error {
	if e.Instance == "" {
		return fmt.Errorf("the credentials name no instance (--uri, REARM_URI or --config)")
	}
	f, err := readCurrentSessions(repo)
	if err != nil {
		return err
	}
	if e.RecordedAt == "" {
		e.RecordedAt = time.Now().UTC().Format(time.RFC3339)
	}
	f.Sessions[e.Instance] = &e
	return writeCurrentSessions(f)
}

// clearCurrentSessionEverywhere drops every entry, in every repository, that names the session: a closed session
// is no one's current session. Returns the repositories it was cleared from.
func clearCurrentSessionEverywhere(sessionUuid string) []string {
	dir, err := currentSessionsDir()
	if err != nil || sessionUuid == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var cleared []string
	for _, de := range entries {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, de.Name()))
		if err != nil {
			continue
		}
		var f currentSessionsFile
		if json.Unmarshal(raw, &f) != nil || f.Path == "" {
			continue
		}
		changed := false
		for inst, e := range f.Sessions {
			if e != nil && strings.EqualFold(e.SessionUuid, sessionUuid) {
				delete(f.Sessions, inst)
				changed = true
			}
		}
		if changed {
			if err := writeCurrentSessions(&f); err != nil {
				fmt.Fprintf(os.Stderr, "rearm: could not clear the current session for %s: %v\n", f.Path, err)
				continue
			}
			cleared = append(cleared, f.Path)
		}
	}
	return cleared
}

// otherInstanceSessions are the repository's entries on every instance but this one, by instance: verify's
// code-session fallback (round 2 §2). Local state only; nothing is read from those instances.
func otherInstanceSessions(repo, instance string) []currentSessionEntry {
	f, err := readCurrentSessions(repo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "rearm: %v\n", err)
		return nil
	}
	var out []currentSessionEntry
	for inst, e := range f.Sessions {
		if inst == instance || e == nil {
			continue
		}
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Instance < out[j].Instance })
	return out
}

// The two ways a verb takes its session, as the refusal for a missing one names them (ARCHITECTURE round 3 §2): the
// --session flag, or the positional uuid of session close.
const (
	sessionByFlag       = "pass --session <session-uuid>"
	sessionByPositional = "pass its uuid: rearm agent session close <session-uuid> --final <file>"
)

// noCurrentSessionRefusal is the words a verb that needs a session says when it has neither its session nor an
// entry for this repository on the instance it calls; remedy is how the verb takes its session.
func noCurrentSessionRefusal(repo, instance, remedy string) string {
	where := "this repository"
	if repo != "" {
		where = repo
	}
	msg := "no current session is recorded for " + where
	if instance != "" {
		msg += " on " + instance
	} else {
		msg += " (the credentials name no instance)"
	}
	msg += "; " + remedy + ", or record one for this repository with: rearm agent session current --set <session-uuid>"
	if others := otherInstanceSessions(repo, instance); repo != "" && len(others) > 0 {
		var names []string
		for _, o := range others {
			names = append(names, o.Instance)
		}
		msg += " (the entry on " + strings.Join(names, ", ") + " is that instance's and is not used here)"
	}
	return msg
}

// applySessionFallback puts the repository's current session on this instance in place of a --session the caller
// left out (round 1 §3.3, round 2 §1). An explicit --session wins, including one from the environment or the
// config file, which the configuration loader has already applied. A verb that needs a session and finds no entry
// is refused, naming session current --set; one that runs without a session runs as before. Returns the exit code
// of a refusal, or 0.
func applySessionFallback(cmd *cobra.Command) int {
	fallbackInUse = nil
	f := cmd.Flags().Lookup("session")
	if f == nil || f.Changed {
		return 0
	}
	if _, skip := sessionFallbackSkip[cmd.CommandPath()]; skip {
		return 0
	}
	// agent release show takes the session or its client id.
	if alt := cmd.Flags().Lookup("client-session-id"); alt != nil && alt.Changed {
		return 0
	}
	repo, err := repositoryKey()
	if err != nil {
		repo = ""
	}
	instance := currentInstance()
	if e := currentSessionFor(repo, instance); e != nil && e.SessionUuid != "" {
		if err := cmd.Flags().Set("session", e.SessionUuid); err != nil {
			fmt.Fprintf(os.Stderr, "rearm: could not use the current session: %v\n", err)
			return 1
		}
		fallbackInUse = &currentSessionUse{repo: repo, entry: *e}
		return 0
	}
	if code := sessionRequiredExit(cmd, f); code != 0 {
		fmt.Fprintln(os.Stderr, "rearm: --session is required: "+noCurrentSessionRefusal(repo, instance, sessionByFlag))
		return code
	}
	return 0
}

// noticeClosedCurrentSession clears the current session a verb fell back to when the server answers that it is
// closed (round 1 §3.3), and says so. Called on every failed request.
func noticeClosedCurrentSession(err error) {
	if err == nil || fallbackInUse == nil {
		return
	}
	msg := describeError(err)
	use := *fallbackInUse
	e := use.entry
	if !strings.Contains(strings.ToLower(msg), strings.ToLower(e.SessionUuid)) || !strings.Contains(msg, "is CLOSED") {
		return
	}
	fallbackInUse = nil
	clearCurrentSessionEverywhere(e.SessionUuid)
	where := use.repo
	if where == "" {
		where = "this repository"
	}
	fmt.Fprintf(os.Stderr, "rearm: the current session %s (for %s on %s) is closed on the server, so it is no longer "+
		"the current session; open a new one with rearm agent session open, or record one with rearm agent session "+
		"current --set <session-uuid>\n", e.SessionUuid, where, e.Instance)
}
