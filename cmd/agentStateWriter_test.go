package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The session state file's writer under parallel writers (task TPF-110, F-2): writeAgentState and
// writeAgentIdentity replace the file through a temp file unique per writer, so two processes writing one
// session at once never share it. With the old fixed path + ".tmp" name, three such writers corrupted the
// file or failed their renames with ENOENT.

const (
	writerChildEnv = "REARM_TEST_STATE_WRITER_CHILD"
	writerModeEnv  = "REARM_TEST_STATE_WRITER_MODE"
	writerWrites   = 50
)

// TestStateWriterChild is the child process: it does nothing unless the parent set writerChildEnv.
func TestStateWriterChild(t *testing.T) {
	name := os.Getenv(writerChildEnv)
	if name == "" {
		t.Skip("run by TestParallelStateWritersNeverCorruptTheFile")
	}
	for i := 0; i < writerWrites; i++ {
		marker := fmt.Sprintf("%s-%d", name, i)
		switch os.Getenv(writerModeEnv) {
		case "identity":
			if err := writeAgentIdentity(&agentIdentityState{AgentUuid: "a-1", CoAuthor: marker}); err != nil {
				fmt.Fprintln(os.Stderr, "write:", err)
				os.Exit(3)
			}
		case "rmw":
			// The hooks' shape: read the state, change it, write it back.
			st, err := readAgentState("c-1")
			if err != nil || st == nil {
				fmt.Fprintln(os.Stderr, "read:", err)
				os.Exit(3)
			}
			st.CurrentTask = marker
			if err := writeAgentState(st); err != nil {
				fmt.Fprintln(os.Stderr, "write:", err)
				os.Exit(3)
			}
		default:
			if err := writeAgentState(&agentSessionState{SessionUuid: "s-1", ClientSessionId: "c-1", CurrentTask: marker}); err != nil {
				fmt.Fprintln(os.Stderr, "write:", err)
				os.Exit(3)
			}
		}
	}
}

type writerChild struct {
	cmd    *exec.Cmd
	stderr *bytes.Buffer
}

func startWriters(t *testing.T, modes map[string]string) []writerChild {
	t.Helper()
	var kids []writerChild
	for name, mode := range modes {
		c := testChild(t, "TestStateWriterChild", "XDG_STATE_HOME="+os.Getenv("XDG_STATE_HOME"),
			writerChildEnv+"="+name, writerModeEnv+"="+mode)
		var errb bytes.Buffer
		c.Stderr = &errb
		if err := c.Start(); err != nil {
			t.Fatal(err)
		}
		kids = append(kids, writerChild{c, &errb})
	}
	return kids
}

func waitWriters(t *testing.T, kids []writerChild) {
	t.Helper()
	for _, k := range kids {
		if err := k.cmd.Wait(); err != nil {
			t.Errorf("a writer failed: %v\n%s", err, k.stderr.String())
		}
		if strings.Contains(k.stderr.String(), "write:") || strings.Contains(k.stderr.String(), "read:") {
			t.Errorf("a writer reported an error:\n%s", k.stderr.String())
		}
	}
}

func noTempFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("a temp file was left behind: %s", e.Name())
		}
	}
}

func mode0600(t *testing.T, path string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("%s is %v, want 0600", path, fi.Mode().Perm())
	}
}

func TestParallelStateWritersNeverCorruptTheFile(t *testing.T) {
	if os.Getenv(writerChildEnv) != "" {
		t.Skip("child")
	}
	withStateDir(t)
	if err := writeAgentState(&agentSessionState{SessionUuid: "s-1", ClientSessionId: "c-1"}); err != nil {
		t.Fatal(err)
	}
	dir, err := agentStateDir()
	if err != nil {
		t.Fatal(err)
	}
	path, _ := agentStatePath("c-1")
	markers := map[string]bool{}
	for _, n := range []string{"w1", "w2", "w3"} {
		for i := 0; i < writerWrites; i++ {
			markers[fmt.Sprintf("%s-%d", n, i)] = true
		}
	}
	for run := 0; run < 3; run++ {
		waitWriters(t, startWriters(t, map[string]string{"w1": "direct", "w2": "direct", "w3": "rmw"}))
		st, err := readAgentState("c-1")
		if err != nil || st == nil {
			t.Fatalf("run %d: the state file does not parse after three parallel writers: %v", run, err)
		}
		if !markers[st.CurrentTask] || st.SessionUuid != "s-1" {
			t.Errorf("run %d: the file holds one writer's whole body, got currentTask %q session %q", run, st.CurrentTask, st.SessionUuid)
		}
		noTempFiles(t, dir)
		mode0600(t, path)
	}
}

func TestParallelIdentityWritersLeaveAParseableFile(t *testing.T) {
	if os.Getenv(writerChildEnv) != "" {
		t.Skip("child")
	}
	withStateDir(t)
	waitWriters(t, startWriters(t, map[string]string{"i1": "identity", "i2": "identity"}))
	path, err := agentIdentityPath("a-1")
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var id agentIdentityState
	if err := json.Unmarshal(body, &id); err != nil || id.AgentUuid != "a-1" ||
		!(strings.HasPrefix(id.CoAuthor, "i1-") || strings.HasPrefix(id.CoAuthor, "i2-")) {
		t.Fatalf("the identity file parses as one writer's body: %v %+v", err, id)
	}
	noTempFiles(t, filepath.Dir(path))
	mode0600(t, path)
}
