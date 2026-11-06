package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	rearm "github.com/relizaio/rearm-client-go"
)

// task show prints the waiver lines on stderr, keyed by task, beside the base-moved lines (task TPF-153 T-16): the
// JSON on stdout stays whole, and a waiver that does not stand says "(not standing)" alone, since show reads no board.

func waivedTask(key, uuid string, standing bool) map[string]any {
	return map[string]any{"key": key, "uuid": uuid, "status": "QUEUED", "role": "coder",
		"documents": architectureDocs("a1"),
		"designCheckpoint": map[string]any{"state": "SENT_ON", "beforeRole": "coder", "afterRole": "architect",
			"settledBy": map[string]any{"kind": "SESSION", "uuid": "s-arch", "name": "architect-seat"},
			"settledAt": "2026-11-05T03:00:00Z", "note": "waived by architect: fits one coder pass"},
		"designCheckpointWaiver": architectWaiver("a1", standing)}
}

// showTasks runs task show against a fake serving tasks, and returns stdout and stderr apart.
func showTasks(t *testing.T, tasks ...map[string]any) (string, string) {
	t.Helper()
	withStateDir(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var all []any
		for _, task := range tasks {
			all = append(all, task)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"agentTaskProgrammatic": tasks[0], "agentTasksByUuidProgrammatic": all}})
	}))
	defer srv.Close()
	c, err := rearm.New(srv.URL, "id", "secret", rearm.WithoutTokenExchange())
	if err != nil {
		t.Fatal(err)
	}
	apiClient = c
	t.Cleanup(func() { apiClient, taskShowSession = nil, "" })
	var args []string
	for _, task := range tasks {
		args = append(args, orElse(str(task["uuid"]), "t-x"))
	}
	var stdout string
	stderr := captureStderr(t, func() {
		stdout = captureStdout(t, func() { agentTaskShowCmd.Run(agentTaskShowCmd, args) })
	})
	return stdout, stderr
}

const (
	waivedShowLine = "TPF-153: design checkpoint waived by architect: \"fits one coder pass\" (before coder, 2026-11-05T03:00:00Z)\n"
	waiverShowLine = "TPF-153: waiver: --no-split by architect on ARCHITECTURE round 1 at 2026-11-05T02:59:00Z"
)

func TestTaskShowPrintsTheWaiverOnStderr(t *testing.T) {
	// (a) a waived task: the two lines on stderr, the JSON on stdout, standing as the server said.
	stdout, stderr := showTasks(t, waivedTask("TPF-153", "t-1", true))
	if want := waivedShowLine + waiverShowLine + "\n"; stderr != want {
		t.Errorf("(a) stderr:\n%q\nwant:\n%q", stderr, want)
	}
	var read map[string]any
	if err := json.Unmarshal([]byte(stdout), &read); err != nil {
		t.Fatalf("(a) stdout is not the task JSON: %v\n%s", err, stdout)
	}
	if w, _ := read["designCheckpointWaiver"].(map[string]any); w == nil || w["standing"] != true {
		t.Errorf("(a) stdout does not carry the waiver: %v", read["designCheckpointWaiver"])
	}
	if strings.Contains(stdout, "waiver: --no-split") {
		t.Error("(a) a waiver line reached stdout")
	}

	// (b) not standing: "(not standing)" alone, never the brief's ignored clause or its reasons.
	_, stderr = showTasks(t, waivedTask("TPF-153", "t-1", false))
	if want := waivedShowLine + waiverShowLine + " (not standing)\n"; stderr != want {
		t.Errorf("(b) stderr:\n%q\nwant:\n%q", stderr, want)
	}
	if strings.Contains(stderr, "ignored") || strings.Contains(stderr, "reopen") {
		t.Errorf("(b) the brief's clause on task show: %q", stderr)
	}

	// (c) a blind reader's copy: no quoted note, no actor, no placeholder; the waiver line unchanged.
	blind := waivedTask("TPF-153", "t-1", true)
	rec := blind["designCheckpoint"].(map[string]any)
	rec["settledBy"], rec["note"] = nil, "waived by architect"
	w := blind["designCheckpointWaiver"].(map[string]any)
	w["bySession"], w["byAgent"], w["note"] = nil, nil, nil
	_, stderr = showTasks(t, blind)
	if want := "TPF-153: design checkpoint waived by architect (before coder, 2026-11-05T03:00:00Z)\n" + waiverShowLine + "\n"; stderr != want {
		t.Errorf("(c) stderr:\n%q\nwant:\n%q", stderr, want)
	}
	if strings.Contains(stderr, "null") || strings.Contains(stderr, "<nil>") {
		t.Errorf("(c) a placeholder on stderr: %q", stderr)
	}

	// (d) a seat's send-on, a task without a record and a pending record without a waiver: nothing.
	seat := map[string]any{"key": "TPF-1", "uuid": "t-1", "designCheckpoint": map[string]any{"state": "SENT_ON",
		"beforeRole": "coder", "settledAt": "2026-11-05T03:00:00Z", "note": "authorized for coder"}}
	bare := map[string]any{"key": "TPF-2", "uuid": "t-2"}
	pending := map[string]any{"key": "TPF-3", "uuid": "t-3", "designCheckpoint": pendingCheckpoint()}
	if _, stderr = showTasks(t, seat, bare, pending); stderr != "" {
		t.Errorf("(d) stderr carries lines: %q", stderr)
	}

	// (e) two tasks in one show: each line keyed by its own task, the uuid when it has no key.
	other := waivedTask("", "t-9", false)
	_, stderr = showTasks(t, waivedTask("TPF-153", "t-1", true), other)
	want := waivedShowLine + waiverShowLine + "\n" +
		strings.Replace(waivedShowLine, "TPF-153", "t-9", 1) + strings.Replace(waiverShowLine, "TPF-153", "t-9", 1) + " (not standing)\n"
	if stderr != want {
		t.Errorf("(e) stderr:\n%q\nwant:\n%q", stderr, want)
	}

	// (f) the base-moved line first, then the two waiver lines.
	moved := waivedTask("TPF-153", "t-1", true)
	moved["pullRequests"] = []any{map[string]any{"url": "https://github.com/o/r/pull/1", "baseMovedBy": float64(2)}}
	_, stderr = showTasks(t, moved)
	if want := "TPF-153: base moved: 2 commits since your round: https://github.com/o/r/pull/1\n" + waivedShowLine +
		waiverShowLine + "\n"; stderr != want {
		t.Errorf("(f) stderr:\n%q\nwant:\n%q", stderr, want)
	}
}
