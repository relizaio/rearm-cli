package cmd

import (
	"bytes"
	"strings"
	"testing"
)

// rearm releasebycommit takes flags only (task TPF-99, mutation round): a stray positional argument is
// refused before any request, so a mistyped command line cannot look up the wrong thing.
func TestReleaseByCommitRefusesAStrayPositionalArgumentBeforeAnyRequest(t *testing.T) {
	s, _ := releaseBomWorld(t, byCommitAnswer("{}"))
	called := false
	saved := releaseByCommitExit
	releaseByCommitExit = func(int) { called = true }
	defer func() { releaseByCommitExit = saved }()
	cmd := newReleaseByCommitCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"stray", "--commit", byCommitSha, "--component", "my-service"})

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "stray") {
		t.Fatalf("want a refusal naming the stray argument, got %v", err)
	}
	if n := len(s.calls()); n != 0 || called {
		t.Fatalf("no request and no exit for a refused command line, got %d requests, exit called %v", n, called)
	}
}
