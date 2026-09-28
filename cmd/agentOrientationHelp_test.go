package cmd

import (
	"strings"
	"testing"
)

// The help describes the core as architecture-2 cut it (task RD3-10 run 3 T-3): the final report and the CLI's
// install moved to sections of their own, so the help names them among the sections, not in the core.
func TestTheOrientationHelpDescribesTheCutCore(t *testing.T) {
	help := strings.Join(strings.Fields(agentOrientationCmd.Long), " ")
	core := help[:strings.Index(help, "--section")]
	for _, gone := range []string{"final report", "prerequisites"} {
		if strings.Contains(strings.ToLower(core), gone) {
			t.Errorf("the core's description still names %q: %s", gone, core)
		}
	}
	for _, kept := range []string{"environment variables", "opening a session", "usage hooks' install command",
		"heartbeat and close", "credential and tracing rules", "which section to read for which action"} {
		if !strings.Contains(core, kept) {
			t.Errorf("the core's description misses %q: %s", kept, core)
		}
	}
	for _, section := range []string{"install-cli", "final-report"} {
		if !strings.Contains(help[len(core):], section) {
			t.Errorf("the sections named miss %s: %s", section, help)
		}
	}
}
