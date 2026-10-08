package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	rearm "github.com/relizaio/rearm-client-go"
)

// What exportreleasebom and scorereleasebom leave on stdout when they fail before or after the
// request (task SCORE-24, review items T-1 and T-5 of run 1): the pipe into rearm bomutils score
// reads stdout, so it carries the document or nothing.

// T-5: errors raised before the command runs (a flag cobra does not know, no session, no URI, a bad
// --auth) go to stderr through the real Execute, in a child process because they end it.
func TestErrorsBeforeTheRequestLeaveStdoutEmpty(t *testing.T) {
	if args := os.Getenv("REARM_TEST_RELEASEBOM_ARGS"); args != "" {
		var argv []string
		if err := json.Unmarshal([]byte(args), &argv); err != nil {
			os.Exit(3)
		}
		rootCmd.SetArgs(argv)
		Execute()
		os.Exit(0)
	}
	home := t.TempDir()
	env := []string{"HOME=" + home}
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "REARM_") && !strings.HasPrefix(kv, "HOME=") && !strings.HasPrefix(kv, "ACTIONS_ID_TOKEN_") {
			env = append(env, kv)
		}
	}
	for _, c := range []struct {
		args  []string
		error string
	}{
		{[]string{"scorereleasebom", "--releaseid", "r", "--profile", "fda", "--mediatype", "CSV"}, "unknown flag: --mediatype"},
		{[]string{"exportreleasebom", "--releaseid", "r", "--outfile"}, "flag needs an argument"},
		{[]string{"--auth", "session", "-u", "http://127.0.0.1:9", "exportreleasebom", "--releaseid", "r"}, "Error: no browser-login session on file"},
		{[]string{"--auth", "session", "-u", "http://127.0.0.1:9", "scorereleasebom", "--releaseid", "r", "--profile", "fda"}, "Error: no browser-login session on file"},
		{[]string{"exportreleasebom", "--releaseid", "r"}, "Error: ReARM URI is required"},
		{[]string{"--auth", "bogus", "-u", "http://127.0.0.1:9", "exportreleasebom", "--releaseid", "r"}, "Error: --auth must be key, session or github-oidc"},
	} {
		argv, _ := json.Marshal(c.args)
		child := exec.Command(os.Args[0], "-test.run=^TestErrorsBeforeTheRequestLeaveStdoutEmpty$")
		child.Env = append(env, "REARM_TEST_RELEASEBOM_ARGS="+string(argv))
		var stdout, stderr strings.Builder
		child.Stdout, child.Stderr = &stdout, &stderr
		err := child.Run()
		if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 1 {
			t.Fatalf("%v: want exit 1, got %v\nstdout: %s\nstderr: %s", c.args, err, stdout.String(), stderr.String())
		}
		// the child's own test framework prints nothing on an os.Exit, so stdout is the command's alone
		if stdout.String() != "" || !strings.Contains(stderr.String(), c.error) {
			t.Fatalf("%v: want nothing on stdout and %q on stderr, got\nstdout: %q\nstderr: %q", c.args, c.error, stdout.String(), stderr.String())
		}
	}
}

// T-1: a document whose answer is larger than the 64 MiB the client read before reaches stdout
// whole; an answer over what the client reads is one error line naming the cause.
func TestAnExportLargerThanTheOldReadLimitReachesStdoutWholeAndAnOversizedAnswerSaysSo(t *testing.T) {
	// 40 MiB that JSON escaping doubles: an 80 MiB answer
	doc := strings.Repeat(`"\`, 20<<20)
	releaseBomWorld(t, exportAnswer(doc))
	stdout, stderr, code := runReleaseBomCmd(t, newExportReleaseBomCmd, "--releaseid", "r-1")
	if code != 0 || stderr != "" || stdout != doc {
		t.Fatalf("the document reaches stdout whole, got exit %d, %d bytes, stderr %q", code, len(stdout), stderr)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(1<<30))
		_, _ = w.Write([]byte(`{"data":{"releaseSbomExportProgrammatic":"`))
	}))
	defer srv.Close()
	c, err := rearm.New(srv.URL, "id", "secret", rearm.WithoutTokenExchange())
	if err != nil {
		t.Fatal(err)
	}
	apiClient = c
	stdout, stderr, code = runReleaseBomCmd(t, newExportReleaseBomCmd, "--releaseid", "r-1")
	if code != 1 || stdout != "" || !strings.HasPrefix(stderr, "Error: rearm: response too large") || strings.Contains(stderr, "malformed") {
		t.Fatalf("an oversized answer is one error line saying so, got %d %q %q", code, stdout, stderr)
	}
}
