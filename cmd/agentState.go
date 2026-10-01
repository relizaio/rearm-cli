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
// The point of the file is that a hook gets its tool's own session id and nothing else: no
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
	// The id the AGENT TOOL uses for its own session -- Claude Code's session_id, and whatever
	// the equivalent is for the next integration. Kept separate from ClientSessionId because an
	// agent may choose its own client id, and then the two differ. Named generically because this
	// file is shared: only the code under `rearm agent claude` knows which tool filled it in.
	ExternalSessionId string `json:"externalSessionId,omitempty"`
	TranscriptPath    string `json:"transcriptPath,omitempty"`
	// The sequence of the last report the server took, and so the idempotency key. For a Claude
	// session that never spawned a context it is the byte offset into the transcript; since task
	// RD3-12 it is the running total of bytes read across the transcript and its subagent files.
	LastSeq int64 `json:"lastSeq"`
	// Where each transcript file was read to: the parent's and every subagent file's, by path. A
	// state written before RD3-12 has none, and its LastSeq is the parent's entry.
	TranscriptOffsets map[string]int64 `json:"transcriptOffsets,omitempty"`
	// The task usage should be attributed to, or empty. Set by `task assign`, cleared by
	// `task signoff` and `task return`.
	CurrentTask string `json:"currentTask,omitempty"`
	// Set once the stale-transcript warning has been printed for this mapping, so it is not
	// repeated on every turn for the rest of the session.
	TruncationWarned bool   `json:"truncationWarned,omitempty"`
	Board            string `json:"board,omitempty"`
	// Where this session last found the board's documents repository, so an agent that passed
	// --repo once does not have to keep passing it.
	DocumentsRepoPath string `json:"documentsRepoPath,omitempty"`
	// The pre-RD4-7 record of published documents: one list per TASK, kept across hops, so a sign-off
	// could send a release an earlier hop of the same session published. Read once, into HopOutputs, and
	// never written again.
	PendingOutputs map[string][]string `json:"pendingOutputs,omitempty"`
	// Document releases published during the current hop on each task, so `task signoff` can send them
	// without the agent copying uuids by hand (task RD4-7). Keyed by TASK uuid, and each entry names the
	// hop it belongs to by the assignment's assignedAt: `task assign` starts a fresh entry, so a sign-off
	// carries what this hop published and nothing older.
	HopOutputs map[string]*hopOutputs `json:"hopOutputs,omitempty"`
	// The roles the last 'task next' declared, so 'task assign' can pass the same ones. Empty
	// when the last poll declared none.
	DeclaredRoles []string `json:"declaredRoles,omitempty"`
	// The task documents this session has read, keyed by TASK uuid (task RD2-34): what `task show` and
	// `task assign` printed, sent by `task signoff` as seenInputs and forgotten once it is accepted.
	SeenInputs map[string][]string `json:"seenInputs,omitempty"`
	// The orientation sections a brief has already printed for this session (task RD3-10), so the
	// once-a-session ones are not printed again.
	OrientationShown []string `json:"orientationShown,omitempty"`
}

// hopOutputs is one hop's published documents on a task (task RD4-7).
type hopOutputs struct {
	// The assignment's assignedAt as the server printed it at `task assign`; empty when the hop was
	// assigned on another host, or the entry was migrated from the per-task list.
	AssignedAt string   `json:"assignedAt,omitempty"`
	Outputs    []string `json:"outputs,omitempty"`
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
	if migratePendingOutputs(&st) {
		// Once: the old list is gone from the file after this write.
		if err := writeAgentState(&st); err != nil {
			fmt.Fprintf(os.Stderr, "rearm: could not migrate the recorded outputs: %v\n", err)
		}
	}
	return &st, nil
}

// migratePendingOutputs moves a pre-RD4-7 state's per-task lists into each task's hop entry, and drops
// them. The old list was the only record of what the current hop published, so it becomes that hop's
// entry; one left over from an earlier hop is cleared by the next `task assign` on its task, as any hop's
// entry is. Reports whether anything moved.
func migratePendingOutputs(st *agentSessionState) bool {
	if st == nil || st.PendingOutputs == nil {
		return false
	}
	for task, releases := range st.PendingOutputs {
		for _, r := range releases {
			addHopOutput(st, task, r)
		}
	}
	st.PendingOutputs = nil
	return true
}

// addHopOutput appends a release to the task's current hop entry, once; reports whether it was new.
func addHopOutput(st *agentSessionState, taskUuid, releaseUuid string) bool {
	if st.HopOutputs == nil {
		st.HopOutputs = map[string]*hopOutputs{}
	}
	h := st.HopOutputs[taskUuid]
	if h == nil {
		h = &hopOutputs{}
		st.HopOutputs[taskUuid] = h
	}
	if stringListHas(h.Outputs, releaseUuid) {
		return false
	}
	h.Outputs = append(h.Outputs, releaseUuid)
	return true
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
	for _, id := range stateKeys(st) {
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

// stateKeys are the names a session's state is filed under: its client session id and, when known and
// different, the agent tool's own id. A state that a task read created on a host where the session was not
// opened knows neither, so it is filed under the session uuid (task RD3-14).
func stateKeys(st *agentSessionState) []string {
	ids := []string{st.ClientSessionId}
	if st.ClientSessionId == "" && st.ExternalSessionId == "" {
		ids = []string{st.SessionUuid}
	}
	if st.ExternalSessionId != "" && st.ExternalSessionId != st.ClientSessionId {
		ids = append(ids, st.ExternalSessionId)
	}
	return ids
}

// ensureAgentState finds the session's local state or, when this host has none, creates it filed under the
// session uuid (task RD3-14): `task show --session` and `task assign` record what the hop read even when the
// session was opened on another host, so the sign-off's acknowledgement works anywhere. Called only after the
// server answered for the session. A reference that is not a uuid creates nothing.
func ensureAgentState(sessionRef string) *agentSessionState {
	if st := lookupAgentState(sessionRef); st != nil {
		return st
	}
	if !isUUID(strings.ToLower(sessionRef)) {
		return nil
	}
	st := &agentSessionState{SessionUuid: sessionRef}
	if err := writeAgentState(st); err != nil {
		fmt.Fprintf(os.Stderr, "rearm: could not create local state for session %s: %v\n", sessionRef, err)
		return nil
	}
	return st
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
	for _, id := range stateKeys(st) {
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

// setDeclaredRoles records the roles a 'task next' declared, or clears them when it declared
// none. A session with no local state -- opened elsewhere -- simply has nothing remembered.
func setDeclaredRoles(sessionUuid string, roles []string) {
	st := findStateBySessionUuid(sessionUuid)
	if st == nil {
		return
	}
	st.DeclaredRoles = roles
	if err := writeAgentState(st); err != nil {
		fmt.Fprintf(os.Stderr, "rearm: could not record declared roles locally: %v\n", err)
	}
}

// declaredRoles is what the last 'task next' declared for this session, or nil.
func declaredRoles(sessionUuid string) []string {
	if st := findStateBySessionUuid(sessionUuid); st != nil {
		return st.DeclaredRoles
	}
	return nil
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

// lookupAgentState finds local state by whichever id the caller has.
//
// State files are named by the CLIENT session id and the agent tool's own id, but every board
// command takes the session UUID -- which names no file. Looking up by uuid alone therefore found
// nothing and silently behaved as though the session had no local state: `doc publish` recorded no
// pending output, and the sign-off that followed sent none, which a role declaring a required
// output then refused. The task commands got this right by scanning; this is that, for both keys.
func lookupAgentState(ref string) *agentSessionState {
	if ref == "" {
		return nil
	}
	if st, err := readAgentState(ref); err == nil && st != nil {
		return st
	}
	return findStateBySessionUuid(ref)
}

// rememberDocumentsRepoPath records where the board's documents checkout was found.
func rememberDocumentsRepoPath(st *agentSessionState, path string) {
	if st == nil || path == "" || st.DocumentsRepoPath == path {
		return
	}
	st.DocumentsRepoPath = path
	if err := writeAgentState(st); err != nil {
		fmt.Fprintf(os.Stderr, "rearm: could not remember the documents repository path: %v\n", err)
	}
}

// rememberPendingOutput records a document release as an output of the current hop on a task.
func rememberPendingOutput(st *agentSessionState, taskUuid, releaseUuid string) {
	if st == nil || taskUuid == "" || releaseUuid == "" {
		return
	}
	// A re-publish returns the SAME release, by design. Recording it twice would send a duplicate
	// uuid at sign-off.
	if !addHopOutput(st, taskUuid, releaseUuid) {
		return
	}
	if err := writeAgentState(st); err != nil {
		fmt.Fprintf(os.Stderr, "rearm: could not record the published document locally; "+
			"pass --outputs %s at sign-off: %v\n", releaseUuid, err)
	}
}

// startHopOutputs begins a task's hop entry at `task assign` (task RD4-7): whatever an earlier hop of this
// session on the task recorded is dropped, so the sign-off that ends this hop cannot carry it.
func startHopOutputs(sessionRef, taskUuid, assignedAt string) {
	st := lookupAgentState(sessionRef)
	if st == nil || taskUuid == "" {
		return
	}
	if st.HopOutputs == nil {
		st.HopOutputs = map[string]*hopOutputs{}
	}
	st.HopOutputs[taskUuid] = &hopOutputs{AssignedAt: assignedAt}
	if err := writeAgentState(st); err != nil {
		fmt.Fprintf(os.Stderr, "rearm: could not start this hop's record of outputs: %v\n", err)
	}
}

// hopOutputsFor is what this session published on the task in its current hop. Read, not taken: a
// refused sign-off keeps them for the next attempt, and forgetHopOutputs drops them once one is accepted.
func hopOutputsFor(sessionRef, taskUuid string) []string {
	st := lookupAgentState(sessionRef)
	if st == nil || st.HopOutputs == nil || st.HopOutputs[taskUuid] == nil {
		return nil
	}
	return append([]string(nil), st.HopOutputs[taskUuid].Outputs...)
}

// forgetHopOutputs drops the task's hop entry once the hop has closed: an accepted sign-off or return.
func forgetHopOutputs(sessionRef, taskUuid string) {
	st := lookupAgentState(sessionRef)
	if st == nil || st.HopOutputs == nil || st.HopOutputs[taskUuid] == nil {
		return
	}
	delete(st.HopOutputs, taskUuid)
	if err := writeAgentState(st); err != nil {
		fmt.Fprintf(os.Stderr, "rearm: could not clear the recorded outputs: %v\n", err)
	}
}

// sessionUuidOf prefers the uuid recorded in local state, falling back to what the caller passed.
func sessionUuidOf(st *agentSessionState, given string) string {
	if st != nil && st.SessionUuid != "" {
		return st.SessionUuid
	}
	return given
}

// rememberBoard records the board a session is coordinating.
//
// A component-scoped document names no task, so the task read cannot supply the board. Holding the
// coordinator seat is the one thing that binds a session to a board without one.
func rememberBoard(sessionRef, boardUuid string) {
	st := lookupAgentState(sessionRef)
	if st == nil || boardUuid == "" || st.Board == boardUuid {
		return
	}
	st.Board = boardUuid
	if err := writeAgentState(st); err != nil {
		fmt.Fprintf(os.Stderr, "rearm: could not record the board locally; pass --board on "+
			"component-scoped publishes: %v\n", err)
	}
}

// orientationShown reports whether a brief already printed the section for this session.
func orientationShown(sessionRef, key string) bool {
	st := lookupAgentState(sessionRef)
	if st == nil {
		return false
	}
	for _, k := range st.OrientationShown {
		if k == key {
			return true
		}
	}
	return false
}

// rememberOrientationShown records the sections a brief printed for the session.
func rememberOrientationShown(sessionRef string, secs []briefSection) {
	st := lookupAgentState(sessionRef)
	if st == nil || len(secs) == 0 {
		return
	}
	changed := false
	for _, s := range secs {
		if !stringListHas(st.OrientationShown, s.Key) {
			st.OrientationShown = append(st.OrientationShown, s.Key)
			changed = true
		}
	}
	if changed {
		if err := writeAgentState(st); err != nil {
			fmt.Fprintf(os.Stderr, "rearm: could not record the orientation shown: %v\n", err)
		}
	}
}

func stringListHas(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
