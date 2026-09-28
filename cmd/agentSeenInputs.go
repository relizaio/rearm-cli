package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// What a hop has read of its task's documents (task RD2-34). The server refuses a sign-off that does not
// acknowledge a document published on the task since the assignment; `task show` and `task assign` print the
// task's documents and record them here, per session and task, so `task signoff` sends them as seenInputs with
// no uuids copied by hand -- as `doc publish` records outputs.

// taskDocumentReleases picks the document releases of each task in a task read (one task, a list of tasks,
// or an assign's {task: ...}), by task uuid.
func taskDocumentReleases(read interface{}) map[string][]string {
	out := map[string][]string{}
	var visit func(v interface{})
	visit = func(v interface{}) {
		switch t := v.(type) {
		case []interface{}:
			for _, e := range t {
				visit(e)
			}
		case map[string]interface{}:
			if inner, ok := t["task"]; ok {
				visit(inner)
				return
			}
			uuid, _ := t["uuid"].(string)
			docs, _ := t["documents"].([]interface{})
			if uuid == "" {
				return
			}
			releases := []string{}
			for _, d := range docs {
				if m, ok := d.(map[string]interface{}); ok {
					if r, _ := m["uuid"].(string); r != "" {
						releases = append(releases, r)
					}
				}
			}
			out[uuid] = releases
		}
	}
	visit(read)
	return out
}

// rememberSeen adds releases to what the session has read of a task.
func rememberSeen(st *agentSessionState, taskUuid string, releases []string) {
	if st == nil || taskUuid == "" {
		return
	}
	if st.SeenInputs == nil {
		st.SeenInputs = map[string][]string{}
	}
	have := map[string]bool{}
	for _, r := range st.SeenInputs[taskUuid] {
		have[r] = true
	}
	list := append([]string{}, st.SeenInputs[taskUuid]...)
	for _, r := range releases {
		if !have[r] {
			have[r] = true
			list = append(list, r)
		}
	}
	st.SeenInputs[taskUuid] = list
	if err := writeAgentState(st); err != nil {
		fmt.Fprintf(os.Stderr, "rearm: could not record the documents shown; pass --seen at sign-off: %v\n", err)
	}
}

// statesHoldingTask is every local session whose current task is this one: the hop reading it here.
func statesHoldingTask(taskUuid string) []*agentSessionState {
	dir, err := agentStateDir()
	if err != nil || taskUuid == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []*agentSessionState
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var st agentSessionState
		if json.Unmarshal(raw, &st) != nil || st.CurrentTask != taskUuid || seen[st.SessionUuid] {
			continue
		}
		seen[st.SessionUuid] = true
		s := st
		out = append(out, &s)
	}
	return out
}

// rememberSeenFromRead records a task read's documents as read: for the named session, or else for every local
// session holding the task.
func rememberSeenFromRead(sessionRef string, read interface{}) {
	for taskUuid, releases := range taskDocumentReleases(read) {
		var targets []*agentSessionState
		if sessionRef != "" {
			if st := lookupAgentState(sessionRef); st != nil {
				targets = []*agentSessionState{st}
			}
		} else {
			targets = statesHoldingTask(taskUuid)
		}
		for _, st := range targets {
			rememberSeen(st, taskUuid, releases)
		}
	}
}

// seenInputsFor is what the sign-off sends as seenInputs: what the session recorded for the task, and the
// releases passed with --seen. Nil when the session has no local state and nothing was passed -- a caller that
// does not track what it read sends none, and the server skips its check.
func seenInputsFor(sessionRef, taskUuid string, extra []string) []string {
	st := lookupAgentState(sessionRef)
	if st == nil && len(extra) == 0 {
		return nil
	}
	out := []string{}
	if st != nil && st.SeenInputs != nil {
		out = append(out, st.SeenInputs[taskUuid]...)
	}
	return append(out, extra...)
}

// forgetSeen clears what the session read of a task, once its sign-off is accepted.
func forgetSeen(sessionRef, taskUuid string) {
	st := lookupAgentState(sessionRef)
	if st == nil || st.SeenInputs == nil {
		return
	}
	if _, ok := st.SeenInputs[taskUuid]; !ok {
		return
	}
	delete(st.SeenInputs, taskUuid)
	if err := writeAgentState(st); err != nil {
		fmt.Fprintf(os.Stderr, "rearm: could not clear the documents recorded as read: %v\n", err)
	}
}
