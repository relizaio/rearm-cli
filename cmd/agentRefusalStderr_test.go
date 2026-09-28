package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every agent verb refuses the same way (task RD3-16 run 1, observation 2): the reason on stderr and exit 1,
// through printRefusal. printGqlError, which prints on stdout, stays for the commands outside `rearm agent`.
func TestNoAgentVerbPrintsItsRefusalOnStdout(t *testing.T) {
	files, err := filepath.Glob("agent*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("no agent sources found: %v", err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "printGqlError(") {
			t.Errorf("%s prints a refusal on stdout; use printRefusal", f)
		}
	}
}
