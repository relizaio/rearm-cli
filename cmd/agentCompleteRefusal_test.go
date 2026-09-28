package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"

	rearm "github.com/relizaio/rearm-client-go"
)

// task complete refuses out loud (task RD3-16): the server's reason reaches stderr and the command exits 1, so a
// coordinator reading stdout never takes a refusal for the completed task.

const completeRefusal = "Task RD3-4 passed, but its delivery will not land: https://github.com/relizaio/rearm-saas/pull/692" +
	" attested abandoned (superseded by #694). Reopen it to the role that must redo the work, or link the PR that replaces it."

func TestARefusedCompleteExitsOneWithTheReasonOnStderr(t *testing.T) {
	if url := os.Getenv("REARM_TEST_COMPLETE_CHILD"); url != "" {
		c, err := rearm.New(url, "id", "secret", rearm.WithoutTokenExchange())
		if err != nil {
			os.Exit(3)
		}
		apiClient = c
		taskSessionUuid = "seat"
		agentTaskCompleteCmd.Run(agentTaskCompleteCmd, []string{"t-1"})
		os.Exit(0)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"errors": []any{map[string]any{"message": completeRefusal}},
			"data": map[string]any{"agentTaskCompleteProgrammatic": nil}})
	}))
	defer srv.Close()
	child := exec.Command(os.Args[0], "-test.run=^TestARefusedCompleteExitsOneWithTheReasonOnStderr$")
	child.Env = append(os.Environ(), "REARM_TEST_COMPLETE_CHILD="+srv.URL)
	var stdout, stderr strings.Builder
	child.Stdout, child.Stderr = &stdout, &stderr
	err := child.Run()
	if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 1 {
		t.Fatalf("a refused complete should exit 1, got %v\nstdout: %s\nstderr: %s", err, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), completeRefusal) {
		t.Errorf("the reason is not on stderr: %q", stderr.String())
	}
	if strings.Contains(stdout.String(), "delivery will not land") {
		t.Errorf("the refusal reached stdout: %q", stdout.String())
	}
}
