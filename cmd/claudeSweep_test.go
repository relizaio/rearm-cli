package cmd

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Spawned contexts write their turns to <session>/subagents/*.jsonl, not to the parent transcript
// (task RD3-12): the usage report sweeps them all, each from its own offset. The layout below is the
// one observed in Claude Code on 2026-09-28.

type sweepFixture struct {
	dir, parent, subDir string
}

func newSweepFixture(t *testing.T) *sweepFixture {
	t.Helper()
	dir := t.TempDir()
	f := &sweepFixture{dir: dir, parent: filepath.Join(dir, "cs-1.jsonl"), subDir: filepath.Join(dir, "cs-1", "subagents")}
	if err := os.MkdirAll(f.subDir, 0o700); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *sweepFixture) sub(name string) string { return filepath.Join(f.subDir, name) }

func appendLines(t *testing.T, path string, lines ...string) {
	t.Helper()
	fh, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer fh.Close()
	if _, err := fh.WriteString(strings.Join(lines, "\n") + "\n"); err != nil {
		t.Fatal(err)
	}
}

func sideRow(id string, in int64) string {
	return strings.Replace(assistantRow(id, "claude-opus-5", "text", 0, in, 1, 0, 0), `"type":"assistant"`, `"type":"assistant","isSidechain":true`, 1)
}

func size(t *testing.T, path string) int64 {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Size()
}

func inputTokens(d *usageDelta) int64 {
	var n int64
	for _, l := range d.Lines {
		n += l.InputTokens
	}
	return n
}

// commitSweep is what reportDelta records once a report is in.
func commitSweep(st *agentSessionState, d *usageDelta) {
	st.LastSeq = d.EndOffset
	st.TranscriptOffsets = d.FileOffsets
}

func TestASweepSumsTheParentAndItsSpawnedContextsAndKeepsAnOffsetEach(t *testing.T) {
	f := newSweepFixture(t)
	appendLines(t, f.parent, assistantRow("p1", "claude-opus-5", "text", 0, 10, 1, 0, 0))
	appendLines(t, f.sub("agent-a.jsonl"), sideRow("a1", 100), sideRow("a2", 200))
	appendLines(t, f.sub("agent-b.jsonl"), sideRow("b1", 1000))
	st := &agentSessionState{SessionUuid: "s-1"}

	d, err := sweepClaudeTranscripts(st, f.parent)
	if err != nil {
		t.Fatal(err)
	}
	if got := inputTokens(d); got != 1310 {
		t.Fatalf("the parent and both contexts, got %d", got)
	}
	if len(d.Lines) != 1 || d.Lines[0].Requests != 4 {
		t.Fatalf("one line for one model and band, four requests, got %+v", d.Lines)
	}
	want := map[string]int64{f.parent: size(t, f.parent), f.sub("agent-a.jsonl"): size(t, f.sub("agent-a.jsonl")), f.sub("agent-b.jsonl"): size(t, f.sub("agent-b.jsonl"))}
	if len(d.FileOffsets) != 3 {
		t.Fatalf("three offsets, got %v", d.FileOffsets)
	}
	var total int64
	for k, v := range want {
		if d.FileOffsets[k] != v {
			t.Fatalf("offset of %s: got %d want %d", k, d.FileOffsets[k], v)
		}
		total += v
	}
	if d.EndOffset != total {
		t.Fatalf("the sequence is the bytes read across all files, got %d want %d", d.EndOffset, total)
	}
	if d.Extra["spawnedContexts"] != 2 || d.Extra["sidechainRows"] != 3 || d.Extra["claudeSessionId"] != "cs-1" {
		t.Fatalf("the report says what it summed, got %v", d.Extra)
	}

	commitSweep(st, d)
	again, err := sweepClaudeTranscripts(st, f.parent)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Lines) != 0 || again.EndOffset != st.LastSeq {
		t.Fatalf("a second sweep reports nothing new, got %+v", again)
	}
}

func TestAContextThatAppearsBetweenSweepsIsReadFromItsStart(t *testing.T) {
	f := newSweepFixture(t)
	appendLines(t, f.parent, assistantRow("p1", "claude-opus-5", "text", 0, 10, 1, 0, 0))
	st := &agentSessionState{SessionUuid: "s-1"}
	d, _ := sweepClaudeTranscripts(st, f.parent)
	commitSweep(st, d)
	first := st.LastSeq

	appendLines(t, f.sub("agent-new.jsonl"), sideRow("n1", 70), sideRow("n2", 30))
	d, err := sweepClaudeTranscripts(st, f.parent)
	if err != nil {
		t.Fatal(err)
	}
	if got := inputTokens(d); got != 100 {
		t.Fatalf("the new context from byte 0, and nothing of the parent again, got %d", got)
	}
	if d.EndOffset != first+size(t, f.sub("agent-new.jsonl")) {
		t.Fatalf("the sequence moves on by the new file's bytes, got %d", d.EndOffset)
	}
}

func TestATruncatedContextFileIsReadAgainFromTheStart(t *testing.T) {
	f := newSweepFixture(t)
	appendLines(t, f.parent, assistantRow("p1", "claude-opus-5", "text", 0, 10, 1, 0, 0))
	appendLines(t, f.sub("agent-a.jsonl"), sideRow("a1", 100), sideRow("a2", 200))
	st := &agentSessionState{SessionUuid: "s-1"}
	d, _ := sweepClaudeTranscripts(st, f.parent)
	commitSweep(st, d)
	before := st.LastSeq

	// the file is rewritten shorter than its recorded offset
	if err := os.WriteFile(f.sub("agent-a.jsonl"), []byte(sideRow("a9", 5)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	d, err := sweepClaudeTranscripts(st, f.parent)
	if err != nil {
		t.Fatal(err)
	}
	if d.Truncated || inputTokens(d) != 5 {
		t.Fatalf("the parser's rule: read again from the start, and report it, got truncated=%v tokens=%d", d.Truncated, inputTokens(d))
	}
	if d.EndOffset <= before {
		t.Fatalf("the sequence still only grows, got %d after %d", d.EndOffset, before)
	}
}

func TestAStateWithOneOffsetMigratesToTheParentsEntry(t *testing.T) {
	f := newSweepFixture(t)
	appendLines(t, f.parent, assistantRow("p1", "claude-opus-5", "text", 0, 10, 1, 0, 0))
	reported := size(t, f.parent)
	appendLines(t, f.parent, assistantRow("p2", "claude-opus-5", "text", 0, 20, 1, 0, 0))
	// written by a CLI before RD3-12: the parent's offset is LastSeq, and there is no map
	st := &agentSessionState{SessionUuid: "s-1", TranscriptPath: f.parent, LastSeq: reported}

	d, err := sweepClaudeTranscripts(st, f.parent)
	if err != nil {
		t.Fatal(err)
	}
	if got := inputTokens(d); got != 20 {
		t.Fatalf("only what came after the old offset, got %d", got)
	}
	if d.EndOffset != size(t, f.parent) || d.FileOffsets[f.parent] != size(t, f.parent) {
		t.Fatalf("for a session that spawned nothing the sequence is the parent's offset, as before: %d / %v", d.EndOffset, d.FileOffsets)
	}
}

func TestARemovedContextFileLeavesTheStateAndTheSequenceKeepsGrowing(t *testing.T) {
	f := newSweepFixture(t)
	appendLines(t, f.parent, assistantRow("p1", "claude-opus-5", "text", 0, 10, 1, 0, 0))
	appendLines(t, f.sub("agent-a.jsonl"), sideRow("a1", 100))
	st := &agentSessionState{SessionUuid: "s-1"}
	d, _ := sweepClaudeTranscripts(st, f.parent)
	commitSweep(st, d)
	before := st.LastSeq

	if err := os.Remove(f.sub("agent-a.jsonl")); err != nil {
		t.Fatal(err)
	}
	appendLines(t, f.parent, assistantRow("p2", "claude-opus-5", "text", 0, 20, 1, 0, 0))
	d, err := sweepClaudeTranscripts(st, f.parent)
	if err != nil {
		t.Fatal(err)
	}
	if _, kept := d.FileOffsets[f.sub("agent-a.jsonl")]; kept || len(d.FileOffsets) != 1 {
		t.Fatalf("the removed file leaves the state, got %v", d.FileOffsets)
	}
	if inputTokens(d) != 20 || d.EndOffset <= before {
		t.Fatalf("the parent's new line under a larger sequence, got tokens=%d seq=%d after %d", inputTokens(d), d.EndOffset, before)
	}
}

func TestAShorterParentStillStopsReportingIntoTheStaleSession(t *testing.T) {
	f := newSweepFixture(t)
	appendLines(t, f.parent, assistantRow("p1", "claude-opus-5", "text", 0, 10, 1, 0, 0))
	st := &agentSessionState{SessionUuid: "s-1", TranscriptOffsets: map[string]int64{f.parent: 999999}, LastSeq: 999999}
	d, err := sweepClaudeTranscripts(st, f.parent)
	if err != nil {
		t.Fatal(err)
	}
	if !d.Truncated {
		t.Fatal("a parent shorter than recorded is still the reused-path case reportDelta refuses to report")
	}
}

func TestInstallWiresSubagentStopAndUninstallRemovesIt(t *testing.T) {
	path, done := settingsWith(t, "")
	defer done()
	settings, _ := readClaudeSettings(path)
	if !installClaudeUsageHooks(settings) {
		t.Fatal("a first install changes the settings")
	}
	if installClaudeUsageHooks(settings) {
		t.Fatal("a second install changes nothing")
	}
	if err := writeClaudeSettings(path, settings); err != nil {
		t.Fatal(err)
	}
	hooks := loadSettings(t, path)["hooks"].(map[string]interface{})
	for _, ev := range []string{"Stop", "SubagentStop", "SessionEnd"} {
		entries, _ := hooks[ev].([]interface{})
		if len(entries) != 1 {
			t.Fatalf("%s: one entry, got %v", ev, hooks[ev])
		}
	}
	raw, _ := json.Marshal(hooks["SubagentStop"])
	if !strings.Contains(string(raw), claudeStopHookCommand) || strings.Contains(string(raw), "--final") {
		t.Fatalf("SubagentStop runs the sweep, not the final flush: %s", raw)
	}
	settings, _ = readClaudeSettings(path)
	if !uninstallClaudeUsageHooks(settings) {
		t.Fatal("uninstall finds them")
	}
	if _, left := settings["hooks"]; left {
		t.Fatalf("all three are removed, got %v", settings["hooks"])
	}
}

// withUsageState points the state directory at a temp dir and files a session under Claude id cs-1.
func withUsageState(t *testing.T) *agentSessionState {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	savedDry, savedFrom, savedSince, savedClient := usageDryRun, usageFromTranscript, usageSinceSeq, usageClientSessionId
	t.Cleanup(func() {
		usageDryRun, usageFromTranscript, usageSinceSeq, usageClientSessionId = savedDry, savedFrom, savedSince, savedClient
	})
	usageDryRun, usageSinceSeq, usageClientSessionId = true, -1, ""
	st := &agentSessionState{SessionUuid: "rearm-session-1", ClientSessionId: "client-1", ExternalSessionId: "cs-1", CurrentTask: "task-1"}
	if err := writeAgentState(st); err != nil {
		t.Fatal(err)
	}
	return st
}

func captureSweepStdout(t *testing.T, f func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stdout
	os.Stdout = w
	f()
	os.Stdout = saved
	_ = w.Close()
	b, _ := io.ReadAll(r)
	return string(b)
}

func TestFromTranscriptOnAContextFileReportsToItsSessionOnce(t *testing.T) {
	withUsageState(t)
	f := newSweepFixture(t)
	appendLines(t, f.sub("agent-a.jsonl"), sideRow("a1", 123))
	usageFromTranscript = f.sub("agent-a.jsonl")

	out := captureSweepStdout(t, func() { runClaudeUsageFromTranscript(nil) })
	if !strings.Contains(out, `"sessionUuid": "rearm-session-1"`) || !strings.Contains(out, `"taskUuid": "task-1"`) ||
		!strings.Contains(out, `"inputTokens": 123`) {
		t.Fatalf("the file's session id finds the ReARM session and its task, got %s", out)
	}
	out = captureSweepStdout(t, func() { runClaudeUsageFromTranscript(nil) })
	if strings.TrimSpace(out) != `{"lines":0}` {
		t.Fatalf("a second run reports nothing new, got %s", out)
	}
	st, _ := readAgentState("cs-1")
	if st.TranscriptPath != "" || st.TranscriptOffsets[f.sub("agent-a.jsonl")] != size(t, f.sub("agent-a.jsonl")) {
		t.Fatalf("the context file is kept under its own path and is not the parent, got %+v", st)
	}
}

func TestTheStopHookReportsTheSpawnedContextsToo(t *testing.T) {
	withUsageState(t)
	f := newSweepFixture(t)
	appendLines(t, f.parent, assistantRow("p1", "claude-opus-5", "text", 0, 10, 1, 0, 0))
	appendLines(t, f.sub("agent-a.jsonl"), sideRow("a1", 90))
	payload, _ := json.Marshal(map[string]string{"session_id": "cs-1", "transcript_path": f.parent, "hook_event_name": "SubagentStop",
		"agent_transcript_path": f.sub("agent-a.jsonl")})
	r, w, _ := os.Pipe()
	_, _ = w.Write(payload)
	_ = w.Close()
	savedIn := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = savedIn }()

	out := captureSweepStdout(t, func() { runClaudeUsageFromHook() })
	if !strings.Contains(out, `"inputTokens": 100`) || !strings.Contains(out, `"spawnedContexts": 1`) {
		t.Fatalf("one report with the parent and the context, got %s", out)
	}
}
