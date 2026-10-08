package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	rearm "github.com/relizaio/rearm-client-go"
	"github.com/relizaio/rearm-client-go/sessiontest"
)

// S401-1: a running `rearm agent wait` survives refreshes made by other processes of the same login
// (the Claude usage hooks, one-off commands, other waits), in every mode.

// waitBoardAnswers answers the wait's operations with an empty board.
func waitBoardAnswers(op string, _ json.RawMessage) json.RawMessage {
	switch op {
	case "AgentTaskNextProgrammatic":
		return json.RawMessage(`{"data":{"agentTaskNextProgrammatic":null}}`)
	case "SessionTouchProgrammatic":
		return json.RawMessage(`{"data":{"sessionTouchProgrammatic":{"uuid":"s"}}}`)
	case "AgentBoardEventsProgrammatic":
		return json.RawMessage(`{"data":{"agentBoardEventsProgrammatic":{"events":[],"nextAfter":null,"hasMore":false}}}`)
	case "AgentBoardSnapshotProgrammatic":
		return json.RawMessage(`{"data":{"agentBoardSnapshotProgrammatic":{"tasks":[]}}}`)
	case "AgentWaitWatchScope":
		return json.RawMessage(`{"data":{"sessionProgrammatic":{"tasksWorked":[]},"agentTaskRoleConfigsProgrammatic":[]}}`)
	case "AgentWaitWatchTasks":
		return json.RawMessage(`{"data":{"agentTasksByUuidProgrammatic":[]}}`)
	}
	return json.RawMessage(`{"data":{}}`)
}

// otherProcessClock is the wait's fake time; each sleep runs what the other processes do meanwhile.
type otherProcessClock struct {
	t      time.Time
	sleeps int
	during func(n int)
}

func (c *otherProcessClock) now() time.Time { return c.t }
func (c *otherProcessClock) sleep(d time.Duration) {
	c.t = c.t.Add(d)
	c.sleeps++
	c.during(c.sleeps)
}

// hookRefresh is a short-lived process (a usage hook) that starts from the file with its token due
// and refreshes through the lock, as every rearm of this version does.
func hookRefresh(t *testing.T, srv *sessiontest.Server) {
	t.Helper()
	on, err := credentialsStore{}.Load()
	if err != nil {
		t.Fatal(err)
	}
	due := on
	due.AccessTokenExpiry = time.Now().Add(-time.Second)
	c, err := rearm.NewWithSession(srv.URL, due.RefreshToken, due, nil, rearm.WithSessionStore(credentialsStore{}))
	if err != nil {
		t.Fatal(err)
	}
	if err := ping(c); err != nil {
		t.Fatalf("the hook's request failed: %v", err)
	}
}

// captureStderr runs f and returns what it wrote to stderr (the shape of captureStdout).
func captureStderr(t *testing.T, f func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stderr
	os.Stderr = w
	var buf bytes.Buffer
	done := make(chan struct{})
	go func() { _, _ = io.Copy(&buf, r); close(done) }()
	f()
	os.Stderr = saved
	_ = w.Close()
	<-done
	return buf.String()
}

func TestAWaitSurvivesARefreshMadeByAnotherProcess(t *testing.T) {
	modes := map[string]func(o *waitOpts){
		"worker":      func(o *waitOpts) {},
		"watch":       func(o *waitOpts) { o.watch, o.board = true, "b-1" },
		"coordinator": func(o *waitOpts) { o.coordinator, o.board = true, "b-1" },
	}
	for name, mode := range modes {
		t.Run(name, func(t *testing.T) {
			srv := withSessionFile(t, sessiontest.Options{GraphQL: waitBoardAnswers, HTML401: true})
			o := waitOpts{session: "s-1", interval: 30 * time.Second, timeout: 90 * time.Second,
				statePath: filepath.Join(t.TempDir(), "wait-state.json")}
			mode(&o)
			clk := &otherProcessClock{t: time.Date(2026, 10, 8, 17, 34, 0, 0, time.UTC), during: func(n int) {
				switch n {
				case 1:
					hookRefresh(t, srv) // another process refreshes: the wait's access token is dead
				case 2:
					srv.Advance(time.Hour + time.Minute) // the hour boundary: every token has expired
				}
			}}
			var out bytes.Buffer
			var code int
			errText := captureStderr(t, func() { code = runWait(o, cliWaitClient{}, clk, &out) })
			if code != waitExitTimeout || errText != "" {
				t.Fatalf("expected exit 2 with nothing on stderr, got %d and %q (stdout %s)", code, errText, out.String())
			}
			if clk.sleeps != 2 {
				t.Fatalf("three polls, two sleeps, got %d sleeps", clk.sleeps)
			}
			n := srv.Counts()
			if n.Refreshes != 2 {
				t.Fatalf("the hook once, the wait once after the boundary, got %+v", n)
			}
			noSessionHarm(t, srv)
			on, _ := credentialsStore{}.Load()
			if !srv.IsCurrent(on.RefreshToken) {
				t.Fatal("the file holds the server's current refresh token")
			}
		})
	}
}
