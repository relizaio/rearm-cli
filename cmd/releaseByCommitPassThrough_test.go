package cmd

import (
	"strings"
	"testing"

	rearm "github.com/relizaio/rearm-client-go"
)

// rearm releasebycommit passes what it is given through (task TPF-99, mutation round 2): the
// server trims and matches, the CLI adds nothing, takes nothing away and checks nothing.

// debugOffAtTheProductionDefault sets the -d flag variable the way a run without -d has it.
func debugOffAtTheProductionDefault(t *testing.T) {
	t.Helper()
	saved := debug
	debug = "false"
	t.Cleanup(func() { debug = saved })
}

func TestReleaseByCommitSendsTheCommitAndTheComponentByteForByte(t *testing.T) {
	for _, c := range []struct{ commit, component string }{
		{"  1F2E3d4C5b6A79880716253443526170F1E2d3C4\n", "My-Service"},
		{byCommitSha, "  my-service \t"},
		{byCommitSha, "0B6C3F0E-5D2A-4E8B-9A71-2F4C6D8E1A3B"},
		{"\t" + strings.ToUpper(byCommitSha), "\nOrg Component Name "},
	} {
		s, _ := releaseBomWorld(t, byCommitAnswer("{}"))
		runReleaseByCommitCmd(t, "--commit", c.commit, "--component", c.component)
		calls := s.calls()
		if len(calls) != 1 {
			t.Fatalf("%q %q: one request, got %d", c.commit, c.component, len(calls))
		}
		vars := calls[0].Variables
		if vars["commit"] != c.commit || vars["componentId"] != c.component || len(vars) != 2 {
			t.Fatalf("commit and component as typed, want %q %q, got %q", c.commit, c.component, vars)
		}
	}
}

func TestReleaseByCommitSendsTheDocumentOfTheByCommitField(t *testing.T) {
	s, _ := releaseBomWorld(t, byCommitAnswer("{}"))
	runReleaseByCommitCmd(t, "--commit", byCommitSha, "--component", "my-service")
	calls := s.calls()
	if len(calls) != 1 {
		t.Fatalf("one request, got %d", len(calls))
	}
	query := calls[0].Query
	if query != rearm.GetReleaseByCommitProgrammatic_Operation {
		t.Fatalf("the generated operation text, got %q", query)
	}
	if !strings.Contains(query, "getReleaseByCommitProgrammatic(commit: $commit, componentId: $componentId)") {
		t.Fatalf("the single-release field with both variables, got %q", query)
	}
	if strings.Contains(query, "getReleasesByCommitProgrammatic") {
		t.Fatalf("the list field is API-only and is never sent by the CLI, got %q", query)
	}
}

func TestReleaseByCommitPrintsTheServerStringAsItIsPlusOneNewline(t *testing.T) {
	for _, doc := range []string{" " + releaseBomDoc, releaseBomDoc + "\n", "\n\t" + releaseBomDoc + " \n\n", "  {}  "} {
		releaseBomWorld(t, byCommitAnswer(doc))
		stdout, stderr, code := runReleaseByCommitCmd(t, "--commit", byCommitSha, "--component", "my-service")
		if stdout != doc+"\n" || stderr != "" || code != 0 {
			t.Fatalf("the string untouched and one newline after it, want %q; got %q %q %d", doc+"\n", stdout, stderr, code)
		}
	}
}

func TestReleaseByCommitPrintsNoDebugLineAtTheProductionDefault(t *testing.T) {
	releaseBomWorld(t, byCommitAnswer(releaseBomDoc))
	debugOffAtTheProductionDefault(t) // after the rig, which clears the flag
	stdout, stderr, code := runReleaseByCommitCmd(t, "--commit", byCommitSha, "--component", "my-service")
	if stdout != releaseBomDoc+"\n" || stderr != "" || code != 0 {
		t.Fatalf("-d false: the document alone on stdout and nothing on stderr; got %q %q %d", stdout, stderr, code)
	}

	releaseBomWorld(t, byCommitErrors("Not authorized"))
	debugOffAtTheProductionDefault(t)
	stdout, stderr, code = runReleaseByCommitCmd(t, "--commit", byCommitSha, "--component", "my-service")
	if stdout != "Error: Not authorized\n" || stderr != "" || code != 1 {
		t.Fatalf("-d false on an error: the one Error line and nothing on stderr; got %q %q %d", stdout, stderr, code)
	}
}

func TestReleaseByCommitSendsAnEmptyOrBlankCommitOrComponentAndPrintsWhatTheServerSays(t *testing.T) {
	for _, c := range []struct {
		args            []string
		commit, compone string
	}{
		{[]string{"--commit", "", "--component", "my-service"}, "", "my-service"},
		{[]string{"--commit", byCommitSha, "--component", ""}, byCommitSha, ""},
		{[]string{"--commit=", "--component="}, "", ""},
		{[]string{"--commit", "   ", "--component", "my-service"}, "   ", "my-service"},
		{[]string{"--commit", byCommitSha, "--component", "\t"}, byCommitSha, "\t"},
		{[]string{"--commit", " \t\r\n", "--component", "\u00a0\u3000"}, " \t\r\n", "\u00a0\u3000"},
	} {
		s, _ := releaseBomWorld(t, byCommitAnswer("{}"))
		stdout, stderr, code := runReleaseByCommitCmd(t, c.args...)
		calls := s.calls()
		if len(calls) != 1 {
			t.Fatalf("%q: an empty value is the server's to answer, one request wanted, got %d", c.args, len(calls))
		}
		if vars := calls[0].Variables; vars["commit"] != c.commit || vars["componentId"] != c.compone {
			t.Fatalf("%q: the empty value reaches the request, got %q", c.args, vars)
		}
		if stdout != "{}\n" || stderr != "" || code != 0 {
			t.Fatalf("%q: the server's {} on stdout, exit 0; got %q %q %d", c.args, stdout, stderr, code)
		}
	}
}

// Every kind of whitespace at either end, at both ends and inside, of the three things the command
// passes through: the commit, the component and the printed document.
var byCommitWhitespace = []string{" ", "\t", "\n", "\r", "\r\n", "\u00a0", "\u3000", "  \t"}

func byCommitPadded(core string) []string {
	var out []string
	for _, w := range byCommitWhitespace {
		out = append(out, w+core, core+w, w+core+w, core[:3]+w+core[3:])
	}
	return out
}

func TestReleaseByCommitPassesEveryKindOfWhitespaceThroughTheCommitAndTheComponent(t *testing.T) {
	for _, commit := range byCommitPadded(byCommitSha) {
		s, _ := releaseBomWorld(t, byCommitAnswer("{}"))
		runReleaseByCommitCmd(t, "--commit", commit, "--component", "my-service")
		if calls := s.calls(); len(calls) != 1 || calls[0].Variables["commit"] != commit || calls[0].Variables["componentId"] != "my-service" {
			t.Fatalf("commit %q as typed, got %v", commit, calls)
		}
	}
	for _, component := range byCommitPadded("My-Service") {
		s, _ := releaseBomWorld(t, byCommitAnswer("{}"))
		runReleaseByCommitCmd(t, "--commit", byCommitSha, "--component", component)
		if calls := s.calls(); len(calls) != 1 || calls[0].Variables["commit"] != byCommitSha || calls[0].Variables["componentId"] != component {
			t.Fatalf("component %q as typed, got %v", component, calls)
		}
	}
}

func TestReleaseByCommitPrintsEveryKindOfWhitespaceAroundTheDocumentAndAnEmptyDocument(t *testing.T) {
	docs := append(byCommitPadded(releaseBomDoc), "", "\n", "\r\n", " ", "\u00a0", "\u3000")
	for _, doc := range docs {
		releaseBomWorld(t, byCommitAnswer(doc))
		stdout, stderr, code := runReleaseByCommitCmd(t, "--commit", byCommitSha, "--component", "my-service")
		if stdout != doc+"\n" || stderr != "" || code != 0 {
			t.Fatalf("the string %q untouched and one newline after it; got %q %q %d", doc, stdout, stderr, code)
		}
	}
}

func TestReleaseByCommitPrintsTheDebugLineBeforeAFailedRequestToo(t *testing.T) {
	_, srv := releaseBomWorld(t, byCommitErrors("Not authorized"))
	debug = "true" // after the rig, which clears the flag
	t.Cleanup(func() { debug = "" })
	savedURI := rearmUri
	rearmUri = srv.URL
	t.Cleanup(func() { rearmUri = savedURI })
	stdout, stderr, code := runReleaseByCommitCmd(t, "--commit", byCommitSha, "--component", "my-service")
	if stdout != "Error: Not authorized\n" || stderr != "Using ReARM at "+srv.URL+"\n" || code != 1 {
		t.Fatalf("-d true on a failed request: the Error line on stdout, the debug line once on stderr, exit 1; got %q %q %d", stdout, stderr, code)
	}
}
