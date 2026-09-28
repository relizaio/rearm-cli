package cmd

import (
	"fmt"
	"os"
)

// What a hop has read of its task's documents (task RD2-34). The server refuses a sign-off that does not
// acknowledge a document published on the task since the assignment; `task show --session` and `task assign` print
// the task's documents and record them here, per session and task, so `task signoff` sends them as seenInputs with
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

// rememberSeenFromRead records a task read's documents as read by the named session, into that session's state
// only. A read that names no session records nothing: the state directory is per host and user, so a
// coordinator's, another role's or a person's read there must not acknowledge for the holder (RD2-34 run 1 T-1,
// architecture-3 §1). A session with no state on this host gets one (RD3-14), so the read counts wherever it was made.
func rememberSeenFromRead(sessionRef string, read interface{}) {
	if sessionRef == "" {
		return
	}
	st := ensureAgentState(sessionRef)
	if st == nil {
		fmt.Fprintf(os.Stderr, "rearm: no local state for session %s, so nothing is recorded as read; pass --seen at sign-off\n", sessionRef)
		return
	}
	for taskUuid, releases := range taskDocumentReleases(read) {
		rememberSeen(st, taskUuid, releases)
	}
}

// seenInputsFor is what the sign-off sends as seenInputs: what the session recorded for the task, and the
// releases passed with --seen. Never nil (RD3-14, withdrawing RD2-34's departure): a session with no local state
// sends an empty list, so the server refuses it when a document was published since the assignment, and the
// refusal's remedy, `task show --session`, creates the state and records the read.
func seenInputsFor(sessionRef, taskUuid string, extra []string) []string {
	st := lookupAgentState(sessionRef)
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
