package cmd

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// rearm releasebycommit (task TPF-99, design 7.2) on the exportreleasebom rig: the required flags,
// the request it sends, what reaches stdout, the error lines and the credentials kept out of both
// streams.

const byCommitSha = "1f2e3d4c5b6a79880716253443526170f1e2d3c4"

const byCommitFieldUndefined = "Validation error (FieldUndefined@[getReleaseByCommitProgrammatic]) : " +
	"Field 'getReleaseByCommitProgrammatic' in type 'Query' is undefined"

func byCommitAnswer(doc interface{}) map[string]interface{} {
	return map[string]interface{}{"data": map[string]interface{}{"getReleaseByCommitProgrammatic": doc}}
}

func byCommitErrors(message string) map[string]interface{} {
	return map[string]interface{}{"errors": []interface{}{map[string]interface{}{"message": message}}}
}

// runReleaseByCommitCmd runs a fresh releasebycommit with args, the exit seam recording the code; it
// returns stdout, stderr and the exit code (0 when the seam was not called).
func runReleaseByCommitCmd(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	code := 0
	saved := releaseByCommitExit
	releaseByCommitExit = func(c int) { code = c }
	t.Cleanup(func() { releaseByCommitExit = saved })
	cmd := newReleaseByCommitCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("%v: %v", args, err)
	}
	return stdout.String(), stderr.String(), code
}

func TestReleaseByCommitRequiresBothFlagsBeforeAnyRequest(t *testing.T) {
	for _, c := range []struct {
		args    []string
		missing string
	}{
		{[]string{"--component", "my-service"}, `required flag(s) "commit" not set`},
		{[]string{"--commit", byCommitSha}, `required flag(s) "component" not set`},
	} {
		s, _ := releaseBomWorld(t, byCommitAnswer("{}"))
		called := false
		saved := releaseByCommitExit
		releaseByCommitExit = func(int) { called = true }
		cmd := newReleaseByCommitCmd()
		var stdout, stderr bytes.Buffer
		cmd.SetOut(&stdout)
		cmd.SetErr(&stderr)
		cmd.SetArgs(c.args)
		err := cmd.Execute()
		releaseByCommitExit = saved
		if err == nil || !strings.Contains(err.Error(), c.missing) {
			t.Fatalf("%v: want %q, got %v", c.args, c.missing, err)
		}
		if n := len(s.calls()); n != 0 || called {
			t.Fatalf("%v: no request and no exit before the flags are complete, got %d requests, exit called %v", c.args, n, called)
		}
	}
}

func TestReleaseByCommitSendsTheCommitAndTheComponentUntouched(t *testing.T) {
	basic := "Basic " + base64.StdEncoding.EncodeToString([]byte("id:secret"))
	for _, component := range []string{"my-service", "0b6c3f0e-5d2a-4e8b-9a71-2f4c6d8e1a3b"} {
		s, _ := releaseBomWorld(t, byCommitAnswer("{}"))
		runReleaseByCommitCmd(t, "--commit", byCommitSha, "--component", component)
		calls := s.calls()
		if len(calls) != 1 {
			t.Fatalf("one request, got %d", len(calls))
		}
		got := calls[0]
		if got.OperationName != "GetReleaseByCommitProgrammatic" {
			t.Fatalf("operation name, got %q", got.OperationName)
		}
		if want := map[string]interface{}{"commit": byCommitSha, "componentId": component}; !jsonEqual(t, want, got.Variables) {
			t.Fatalf("variables exactly commit and componentId, want %v got %v", want, got.Variables)
		}
		if got.Authorization != basic {
			t.Fatalf("the rig's Basic credential, got %q", got.Authorization)
		}
	}
}

func TestReleaseByCommitPrintsTheDocumentTheMissAndNothingOnNull(t *testing.T) {
	for _, c := range []struct {
		answer interface{}
		want   string
	}{
		{releaseBomDoc, releaseBomDoc + "\n"},
		{"{}", "{}\n"},
		{nil, ""},
	} {
		releaseBomWorld(t, byCommitAnswer(c.answer))
		stdout, stderr, code := runReleaseByCommitCmd(t, "--commit", byCommitSha, "--component", "my-service")
		if stdout != c.want || stderr != "" || code != 0 {
			t.Fatalf("answer %v: want %q on stdout, nothing on stderr, exit 0; got %q %q %d", c.answer, c.want, stdout, stderr, code)
		}
	}
}

func TestReleaseByCommitPrintsOneErrorLineAndExitsOne(t *testing.T) {
	for _, message := range []string{byCommitFieldUndefined, "Not authorized"} {
		releaseBomWorld(t, byCommitErrors(message))
		stdout, stderr, code := runReleaseByCommitCmd(t, "--commit", byCommitSha, "--component", "my-service")
		if want := "Error: " + message + "\n"; stdout != want || stderr != "" || code != 1 {
			t.Fatalf("want %q on stdout, nothing on stderr, exit 1; got %q %q %d", want, stdout, stderr, code)
		}
	}
}

func TestReleaseByCommitDebugGoesToStderrAndNoSecretReachesEitherStream(t *testing.T) {
	_, srv := releaseBomWorld(t, byCommitAnswer(releaseBomDoc))
	debug = "true"
	savedURI := rearmUri
	rearmUri = srv.URL
	t.Cleanup(func() { rearmUri = savedURI })
	stdout, stderr, _ := runReleaseByCommitCmd(t, "--commit", byCommitSha, "--component", "my-service")
	if stdout != releaseBomDoc+"\n" || stderr != "Using ReARM at "+srv.URL+"\n" {
		t.Fatalf("debug on stderr only, got %q %q", stdout, stderr)
	}

	for _, answer := range []map[string]interface{}{byCommitErrors("Not authorized"), byCommitAnswer(releaseBomDoc)} {
		s, _ := releaseBomWorld(t, answer)
		var out, errOut string
		pipeOut, pipeErr := both(t, func() {
			out, errOut, _ = runReleaseByCommitCmd(t, "--commit", byCommitSha, "--component", "my-service")
		})
		if pipeOut != "" || pipeErr != "" {
			t.Fatalf("everything goes through the command's writers, got %q %q on the process streams", pipeOut, pipeErr)
		}
		auth := s.calls()[0].Authorization
		if auth == "" {
			t.Fatal("the rig recorded no Authorization header")
		}
		for _, text := range []string{out, errOut, pipeOut, pipeErr} {
			if strings.Contains(text, auth) || strings.Contains(text, "secret") {
				t.Fatalf("a credential reached the output: %q", text)
			}
		}
	}
}

func TestReleaseByCommitIsRegisteredWithBothFlagsRequired(t *testing.T) {
	var found *cobra.Command
	for _, c := range rootCmd.Commands() {
		if c.Use == "releasebycommit" {
			found = c
		}
	}
	if found == nil {
		t.Fatal("releasebycommit is not registered on rootCmd")
	}
	if found.Short != "Outputs the OBOM (CycloneDX 1.6) of the newest release of a component or product built from a head commit" {
		t.Fatalf("Short, got %q", found.Short)
	}
	for _, name := range []string{"commit", "component"} {
		f := found.Flags().Lookup(name)
		if f == nil {
			t.Fatalf("--%s missing", name)
		}
		if v := f.Annotations[cobra.BashCompOneRequiredFlag]; len(v) != 1 || v[0] != "true" {
			t.Fatalf("--%s is not marked required, annotations %v", name, f.Annotations)
		}
	}
}
