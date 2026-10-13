package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	rearm "github.com/relizaio/rearm-client-go"
)

// What exportreleasebom and scorereleasebom leave on stdout when they fail before or after the
// request (task SCORE-24, design 3.3 and 5.4): the pipe into rearm bomutils score
// reads stdout, so it carries the document or nothing.

// childHome: the home directory the child process runs with.
type childHome int

const (
	homeEmpty      childHome = iota // a fresh empty home
	homeWithConfig                  // a fresh home holding a .rearm.yaml
	homeMissing                     // no HOME and no PATH, so the home directory cannot be found
)

// T-12: errors raised before the request (a flag cobra does not know, no session, no URI, a bad
// --auth, no credentials at all, no home directory) and the --debug configuration lines go to stderr
// through the real Execute, in a child process because they end it. Every line round 2 moved from
// stdout to stderr has a case here (task SCORE-24, review item T-7).
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
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "REARM_") && !strings.HasPrefix(kv, "HOME=") && !strings.HasPrefix(kv, "ACTIONS_ID_TOKEN_") {
			env = append(env, kv)
		}
	}
	const noKey = "Error: rearm: API key id and secret are required"
	for _, c := range []struct {
		args   []string
		home   childHome
		errors []string
	}{
		{[]string{"scorereleasebom", "--releaseid", "r", "--profile", "fda", "--mediatype", "CSV"}, homeEmpty, []string{"unknown flag: --mediatype"}},
		{[]string{"exportreleasebom", "--releaseid", "r", "--outfile"}, homeEmpty, []string{"flag needs an argument"}},
		{[]string{"--auth", "session", "-u", "http://127.0.0.1:9", "exportreleasebom", "--releaseid", "r"}, homeEmpty, []string{"Error: no browser-login session on file"}},
		{[]string{"--auth", "session", "-u", "http://127.0.0.1:9", "scorereleasebom", "--releaseid", "r", "--profile", "fda"}, homeEmpty, []string{"Error: no browser-login session on file"}},
		{[]string{"exportreleasebom", "--releaseid", "r"}, homeEmpty, []string{"Error: ReARM URI is required"}},
		{[]string{"--auth", "bogus", "-u", "http://127.0.0.1:9", "exportreleasebom", "--releaseid", "r"}, homeEmpty, []string{"Error: --auth must be key, session or github-oidc"}},
		// no key and no session: the client cannot be built, the first error a new user of the pipe meets
		{[]string{"-u", "http://127.0.0.1:9", "exportreleasebom", "--releaseid", "r"}, homeEmpty, []string{noKey}},
		{[]string{"-u", "http://127.0.0.1:9", "scorereleasebom", "--releaseid", "r", "--profile", "fda"}, homeEmpty, []string{noKey}},
		// --debug names the configuration file it read, or why it read none
		{[]string{"--debug", "true", "-u", "http://127.0.0.1:9", "exportreleasebom", "--releaseid", "r"}, homeWithConfig, []string{"Using config file: ", ".rearm.yaml", noKey}},
		{[]string{"--debug", "true", "-u", "http://127.0.0.1:9", "exportreleasebom", "--releaseid", "r"}, homeEmpty, []string{`Config File ".rearm" Not Found`, noKey}},
		{[]string{"-u", "http://127.0.0.1:9", "exportreleasebom", "--releaseid", "r"}, homeMissing, []string{"executable file not found"}},
	} {
		childEnv := append([]string(nil), env...)
		switch c.home {
		case homeMissing:
			childEnv = append(childEnv, "PATH=")
		default:
			home := t.TempDir()
			if c.home == homeWithConfig {
				if err := os.WriteFile(filepath.Join(home, ".rearm.yaml"), []byte("{}\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			childEnv = append(childEnv, "HOME="+home)
		}
		argv, _ := json.Marshal(c.args)
		child := exec.Command(os.Args[0], "-test.run=^TestErrorsBeforeTheRequestLeaveStdoutEmpty$")
		child.Env = append(childEnv, "REARM_TEST_RELEASEBOM_ARGS="+string(argv))
		var stdout, stderr strings.Builder
		child.Stdout, child.Stderr = &stdout, &stderr
		err := child.Run()
		if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 1 {
			t.Fatalf("%v (home %d): want exit 1, got %v\nstdout: %s\nstderr: %s", c.args, c.home, err, stdout.String(), stderr.String())
		}
		// the child's own test framework prints nothing on an os.Exit, so stdout is the command's alone
		if stdout.String() != "" {
			t.Fatalf("%v (home %d): want nothing on stdout, got\nstdout: %q\nstderr: %q", c.args, c.home, stdout.String(), stderr.String())
		}
		for _, want := range c.errors {
			if !strings.Contains(stderr.String(), want) {
				t.Fatalf("%v (home %d): want %q on stderr, got %q", c.args, c.home, want, stderr.String())
			}
		}
	}
}

// T-10: a document whose answer is larger than the 64 MiB the client read before reaches stdout
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
