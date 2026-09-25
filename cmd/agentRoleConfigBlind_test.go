package cmd

import (
	"strings"
	"testing"

	rearm "github.com/relizaio/rearm-client-go"
)

// The role reads the CLI sends select blindReview (board task 0192a587). They come from the
// pinned rearm-client-go, so this fails when the pin falls back to a client that predates the
// field -- the way roleconfig list silently lost it once (T-6).
func TestRoleReadsSelectBlindReview(t *testing.T) {
	for name, op := range map[string]string{
		"roleconfig list": rearm.AgentTaskRoleConfigsProgrammatic_Operation,
		"board export":    rearm.ExportBoard_Operation,
	} {
		if !strings.Contains(op, "blindReview") {
			t.Errorf("%s does not select blindReview; is rearm-client-go pinned at #31 or later?", name)
		}
	}
}
