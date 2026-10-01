package cmd

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// Every command the package defines is attached somewhere (RD2-1 T-1): agentTaskWorkLevelCmd was defined
// with its flags and never added to agent task, so every call stopped at the parent with "unknown
// flag", while a test of the unattached command object passed.
func TestEveryDefinedCommandIsAttached(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var src strings.Builder
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		src.Write(b)
		src.WriteString("\n")
	}
	all := src.String()
	// Each package-level command definition and its Use line, counted per Use: attached commands are
	// found by walking the tree from the root, so a slice loop or a multi-argument AddCommand counts
	// the same as a single call.
	def := regexp.MustCompile(`(?ms)^var (\w+) = &cobra\.Command\{\s*Use:\s*"([^"]*)"`)
	defined := map[string][]string{}
	for _, m := range def.FindAllStringSubmatch(all, -1) {
		if m[1] != "rootCmd" {
			defined[m[2]] = append(defined[m[2]], m[1])
		}
	}
	if len(defined) < 20 {
		t.Fatalf("found only %d command definitions; the pattern no longer matches the sources", len(defined))
	}
	attached := map[string]int{}
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		for _, sub := range c.Commands() {
			attached[sub.Use]++
			walk(sub)
		}
	}
	walk(rootCmd)
	for use, names := range defined {
		if attached[use] < len(names) {
			t.Errorf("%v (Use %q): %d defined, %d attached under the root", names, use, len(names), attached[use])
		}
	}
}

// The seat's work-level verb runs from the root, as a person types it: the command is found under
// agent task and its flags parse.
func TestAgentTaskWorkLevelIsReachableFromTheRoot(t *testing.T) {
	cmd, rest, err := rootCmd.Find([]string{"agent", "task", "work-level", "t1", "--session", "s1", "--work-level", "0"})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != agentTaskWorkLevelCmd {
		t.Fatalf("agent task work-level resolves to %q, not the work-level command", cmd.CommandPath())
	}
	defer func() {
		taskSessionUuid, taskLevel, taskLevelClear = "", 0, false
		for _, f := range []string{"session", "work-level", "clear"} {
			if fl := cmd.Flags().Lookup(f); fl != nil {
				fl.Changed = false
			}
		}
	}()
	if err := cmd.ParseFlags(rest); err != nil {
		t.Fatalf("flags do not parse from the root: %v", err)
	}
	if taskSessionUuid != "s1" || taskLevel != 0 || !cmd.Flags().Changed("work-level") {
		t.Errorf("parsed session %q level %d changed %v", taskSessionUuid, taskLevel, cmd.Flags().Changed("work-level"))
	}
	vars, err := workLevelVariables(cmd.Flags().Arg(0), taskLevel, cmd.Flags().Changed("work-level"), taskLevelClear)
	if err != nil || vars["taskUuid"] != "t1" || vars["workLevel"] != 0 {
		t.Errorf("variables %v %v", vars, err)
	}
	if boards, _, err := rootCmd.Find([]string{"boards", "work-level", "t1"}); err != nil || boards != boardsTaskWorkLevelCmd {
		t.Errorf("boards work-level resolves to %v (%v)", boards, err)
	}
}
