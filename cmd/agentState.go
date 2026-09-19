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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Local session state, one file per session under
// $XDG_STATE_HOME/rearm/agent-sessions/<clientSessionId>.json.
//
// The point of the file is that a Stop hook gets a Claude Code session id and nothing else: no
// ReARM session uuid, no API round trip budget, and no way to know how much of the transcript it
// already sent. Everything the hook needs to answer those is written here by the commands the agent
// already runs.
//
// XDG_STATE_HOME rather than XDG_CONFIG_HOME or a dotfile in the repo: this is state the user never
// edits and that is worthless on another machine, which is exactly what the state directory is
// specified for. It is also not the cache directory -- losing LastSeq mid-session would re-send the
// whole transcript, which is merely wasteful, but losing SessionUuid would silently detach a
// session's usage from it.

type agentSessionState struct {
	SessionUuid     string `json:"sessionUuid"`
	ClientSessionId string `json:"clientSessionId"`
	// The id Claude Code itself uses, which is what a hook payload carries. Kept separate from
	// ClientSessionId because an agent may choose its own client id, and then the two differ.
	ClaudeSessionId string `json:"claudeSessionId,omitempty"`
	TranscriptPath  string `json:"transcriptPath,omitempty"`
	// Byte offset into the transcript after the last line already reported. The server dedupes on
	// this, so it is both the resume point and the idempotency key.
	LastSeq int64 `json:"lastSeq"`
	// The task usage should be attributed to, or empty. Set by `task assign`, cleared by
	// `task signoff` and `task return`.
	CurrentTask string `json:"currentTask,omitempty"`
	Board       string `json:"board,omitempty"`
}

// agentStateDir is the directory holding the per-session files. Honours XDG_STATE_HOME, falling
// back to the specified default rather than to the current directory: a state file written next to
// wherever the agent happened to be invoked would not be found by the next hook.
func agentStateDir() (string, error) {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "rearm", "agent-sessions"), nil
}

// sanitiseStateKey keeps a session id from escaping the state directory. Client session ids are
// agent-chosen strings, and "../../.ssh/config" is a valid one as far as the CLI is concerned.
// Anything outside a conservative set becomes an underscore; the file is an index, not a record of
// the id, and the id itself is stored inside it.
func sanitiseStateKey(id string) string {
	if id == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := b.String()
	// A name of dots only would still resolve to a directory entry.
	if strings.Trim(out, ".") == "" {
		return "_"
	}
	return out
}

func agentStatePath(id string) (string, error) {
	dir, err := agentStateDir()
	if err != nil {
		return "", err
	}
	key := sanitiseStateKey(id)
	if key == "" {
		return "", fmt.Errorf("empty session id")
	}
	return filepath.Join(dir, key+".json"), nil
}

// readAgentState returns the state for a session id, or nil when there is none. A missing file is
// not an error: the common case is a session opened before hooks were installed.
func readAgentState(id string) (*agentSessionState, error) {
	path, err := agentStatePath(id)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var st agentSessionState
	if err := json.Unmarshal(raw, &st); err != nil {
		return nil, fmt.Errorf("state file %s is not readable JSON: %w", path, err)
	}
	return &st, nil
}

// writeAgentState persists state under BOTH the client session id and, when known and different,
// the Claude session id.
//
// Two names for one file's worth of content because the lookups come from opposite directions: the
// agent knows its client session id, while a hook payload carries only Claude's. Writing both means
// neither side needs a server call to find the other. They are written as separate files rather
// than one plus a symlink because a stale symlink on Windows is a worse failure than a duplicated
// 200-byte JSON file.
func writeAgentState(st *agentSessionState) error {
	dir, err := agentStateDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	body, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	ids := []string{st.ClientSessionId}
	if st.ClaudeSessionId != "" && st.ClaudeSessionId != st.ClientSessionId {
		ids = append(ids, st.ClaudeSessionId)
	}
	for _, id := range ids {
		if id == "" {
			continue
		}
		path, err := agentStatePath(id)
		if err != nil {
			return err
		}
		// Written to a temporary file and renamed: a Stop hook can fire while a `task assign` is
		// mid-write, and a half-written state file would strand the session's usage.
		tmp := path + ".tmp"
		if err := os.WriteFile(tmp, body, 0o600); err != nil {
			return err
		}
		if err := os.Rename(tmp, path); err != nil {
			return err
		}
	}
	return nil
}

// updateAgentState applies a mutation to the state for a session id, if that state exists.
//
// Absence is deliberately not an error and not a creation: these callers (`task assign`, `signoff`,
// `return`) are wired into commands whose real job is the server call. An agent that never ran
// `session init` locally -- CI, a different machine -- must not have `task assign` start failing
// because of a local file it does not need.
func updateAgentState(id string, mutate func(*agentSessionState)) {
	if id == "" {
		return
	}
	st, err := readAgentState(id)
	if err != nil || st == nil {
		return
	}
	mutate(st)
	if err := writeAgentState(st); err != nil {
		fmt.Fprintf(os.Stderr, "rearm: could not update local session state: %v\n", err)
	}
}

// removeAgentState deletes both names a session may be filed under.
func removeAgentState(st *agentSessionState) {
	if st == nil {
		return
	}
	for _, id := range []string{st.ClientSessionId, st.ClaudeSessionId} {
		if id == "" {
			continue
		}
		if path, err := agentStatePath(id); err == nil {
			os.Remove(path)
		}
	}
}

// findStateBySessionUuid scans the state directory for the session with this uuid.
//
// A reverse lookup is needed because `session close` takes the uuid while the files are named by
// client and Claude session id. The directory holds one file per live session on this machine, so
// a scan is cheap and beats maintaining a second index that could fall out of step with the first.
func findStateBySessionUuid(uuid string) *agentSessionState {
	if uuid == "" {
		return nil
	}
	dir, err := agentStateDir()
	if err != nil {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var st agentSessionState
		if json.Unmarshal(raw, &st) == nil && st.SessionUuid == uuid {
			return &st
		}
	}
	return nil
}

// setCurrentTask records the task a session is working on, for usage attribution.
//
// Keyed by session uuid rather than by client id because that is what the task commands take. No-op
// when there is no local state, which is the CI case: the server still attributes implicitly when
// the session holds exactly one assignment.
func setCurrentTask(sessionUuid, taskUuid string) {
	st := findStateBySessionUuid(sessionUuid)
	if st == nil {
		return
	}
	st.CurrentTask = taskUuid
	if err := writeAgentState(st); err != nil {
		fmt.Fprintf(os.Stderr, "rearm: could not record current task locally: %v\n", err)
	}
}

// clearCurrentTask forgets the assignment when the hop closes.
//
// Only clears when the recorded task is the one being closed. A sign-off on some OTHER task must
// not silently detach the task this session is still holding -- that would send the rest of the
// session's usage to the server unattributed, and nothing would ever show it was wrong.
func clearCurrentTask(sessionUuid, taskUuid string) {
	st := findStateBySessionUuid(sessionUuid)
	if st == nil || st.CurrentTask == "" {
		return
	}
	if taskUuid != "" && st.CurrentTask != taskUuid {
		return
	}
	st.CurrentTask = ""
	if err := writeAgentState(st); err != nil {
		fmt.Fprintf(os.Stderr, "rearm: could not clear current task locally: %v\n", err)
	}
}

// firstNonEmptyEnv returns the first of these variables that is set and non-blank.
func firstNonEmptyEnv(names ...string) string {
	for _, n := range names {
		if v := strings.TrimSpace(os.Getenv(n)); v != "" {
			return v
		}
	}
	return ""
}

// adoptStateForClaudeSession binds a Claude session id to local state that does not have one yet.
//
// The hook resolves its session by the id in its payload, which only works if `session init`
// recorded that id. It usually does -- Claude Code exports CLAUDE_CODE_SESSION_ID -- but not when
// init ran somewhere that variable was absent: a wrapper script, a different shell, CI. The failure
// mode there is the worst kind: the hook finds nothing, exits 0 by design, and the session records
// no usage at all with nothing anywhere saying why.
//
// So the hook binds itself. If exactly one state file is missing a Claude id, it is unambiguously
// this one -- a machine running two ReARM-tracked Claude sessions both initialised without the
// variable is the only case this cannot resolve, and there it does nothing rather than guess and
// bind usage to the wrong session.
func adoptStateForClaudeSession(claudeSessionId string) *agentSessionState {
	if claudeSessionId == "" {
		return nil
	}
	dir, err := agentStateDir()
	if err != nil {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var candidates []*agentSessionState
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var st agentSessionState
		if json.Unmarshal(raw, &st) != nil {
			continue
		}
		if st.ClaudeSessionId == "" && st.SessionUuid != "" {
			candidates = append(candidates, &st)
		}
	}
	if len(candidates) != 1 {
		if len(candidates) > 1 {
			fmt.Fprintf(os.Stderr, "rearm: %d local sessions have no Claude session id; cannot tell "+
				"which one this is, so usage is not being reported. Re-run `rearm agent session init` "+
				"with --claude-session-id.\n", len(candidates))
		}
		return nil
	}
	adopted := candidates[0]
	adopted.ClaudeSessionId = claudeSessionId
	if err := writeAgentState(adopted); err != nil {
		fmt.Fprintf(os.Stderr, "rearm: could not bind this Claude session to %s: %v\n", adopted.SessionUuid, err)
		return nil
	}
	fmt.Fprintf(os.Stderr, "rearm: bound Claude session %s to ReARM session %s\n",
		claudeSessionId, adopted.SessionUuid)
	return adopted
}
