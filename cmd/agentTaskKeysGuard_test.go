package cmd

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// The guard that every task-taking verb resolves task keys (task TPF-96, design 3.4): a command whose
// Use names a task argument, or that has a flag taking a task, must be declared in taskKeyCommands
// (acceptTaskKeys, or resolvesTaskKeysInRun for a verb resolving in its own run) or exempted with a
// reason in taskUUIDOnlyCommands.

// taskArgInUse matches <task>, <task-uuid>, <task-key-or-uuid> (and show's [<task-uuid>...]); not
// tasks <board>, <board>, <session-uuid> or <group-key>.
var taskArgInUse = regexp.MustCompile(`<task(-[a-z-]+)?>`)

// taskRefInUsage matches a flag documented as a task uuid or key.
var taskRefInUsage = regexp.MustCompile(`(?i)\btasks? (uuid|key)s?\b`)

// taskFlagsOf names the command's own flags that take a task: named task or *-task, or documented as a
// task uuid or key. Inherited flags are the parents' and are counted there.
func taskFlagsOf(c *cobra.Command) []string {
	var names []string
	c.LocalFlags().VisitAll(func(f *pflag.Flag) {
		if f.Name == "task" || strings.HasSuffix(f.Name, "-task") || taskRefInUsage.MatchString(f.Usage) {
			names = append(names, "--"+f.Name)
		}
	})
	return names
}

// taskTakingCommands walks the tree under root and returns every command that takes a task.
func taskTakingCommands(root *cobra.Command) []*cobra.Command {
	var out []*cobra.Command
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		for _, sub := range c.Commands() {
			if taskArgInUse.MatchString(sub.Use) || len(taskFlagsOf(sub)) > 0 {
				out = append(out, sub)
			}
			walk(sub)
		}
	}
	walk(root)
	return out
}

// taskKeyGuardProblems lists, for the tree under root, every task-taking command that does not resolve
// keys, resolves the wrong arguments, is declared more than once, or is exempted without a reason.
func taskKeyGuardProblems(root *cobra.Command) []string {
	var problems []string
	for _, c := range taskTakingCommands(root) {
		positional := taskArgInUse.MatchString(c.Use)
		flags := taskFlagsOf(c)
		reg, registered := taskKeyCommands[c]
		why, exempt := taskUUIDOnlyCommands[c]
		switch {
		case exempt:
			if strings.TrimSpace(why) == "" {
				problems = append(problems, fmt.Sprintf("%s is exempted from key resolution without a reason: give takesTaskUUIDOnly a sentence", c.CommandPath()))
			}
		case !registered:
			problems = append(problems, fmt.Sprintf("%s (Use %q, task flags %v) takes a task but does not resolve keys: register it with acceptTaskKeys in agentTaskKeys.go, or takesTaskUUIDOnly with a reason", c.CommandPath(), c.Use, flags))
		default:
			if positional != (reg.positional != noTaskArgs) {
				problems = append(problems, fmt.Sprintf("%s: Use %q names a task argument %v, but its registration resolves positional %d", c.CommandPath(), c.Use, positional, reg.positional))
			}
			if reg.flags != len(flags) {
				problems = append(problems, fmt.Sprintf("%s: %d task flags %v, but its registration resolves %d flag pointers", c.CommandPath(), len(flags), flags, reg.flags))
			}
			if reg.calls > 1 {
				problems = append(problems, fmt.Sprintf("%s is declared %d times; each acceptTaskKeys wraps the last, so a key would resolve %d times", c.CommandPath(), reg.calls, reg.calls))
			}
		}
	}
	return problems
}

// T-3: every command in the real tree that takes a task resolves keys. The floors keep the
// predicates honest: the twelve paths below must match, and at least 45 commands must. At
// cc85dcb the count is 51: 29 in rearm agent task (push, verify and commission included), 3
// other rearm agent flag verbs (doc publish, doc element-check, session usage) and 19 in rearm boards
// (register and unassign included).
func TestEveryTaskTakingCommandResolvesKeys(t *testing.T) {
	for _, p := range taskKeyGuardProblems(rootCmd) {
		t.Error(p)
	}
	matched := map[string]bool{}
	var paths []string
	for _, c := range taskTakingCommands(rootCmd) {
		matched[c.CommandPath()] = true
		paths = append(paths, c.CommandPath())
	}
	for _, want := range []string{
		"rearm agent task tag", "rearm agent task setgroup", "rearm agent task work-level",
		"rearm boards tag", "rearm boards setgroup", "rearm boards work-level",
		"rearm agent task show", "rearm agent task authorize", "rearm agent task push",
		"rearm agent task commission", "rearm agent doc publish", "rearm boards register",
	} {
		if !matched[want] {
			t.Errorf("%s no longer matches the guard's predicates", want)
		}
	}
	if len(paths) < 45 {
		sort.Strings(paths)
		t.Errorf("only %d commands take a task by the guard's predicates; the predicates no longer match: %v", len(paths), paths)
	}
}

// T-4: an exemption needs a reason, an undeclared task verb is reported, and a command declared twice
// is reported (review item F-1 of the functional review: the declarations are counted). A throwaway
// tree, never rootCmd.
func TestTaskUUIDOnlyNeedsAReason(t *testing.T) {
	root := &cobra.Command{Use: "root"}
	parent := &cobra.Command{Use: "parent"}
	c := &cobra.Command{Use: "x <task-uuid>", Run: func(*cobra.Command, []string) {}}
	root.AddCommand(parent)
	parent.AddCommand(c)
	t.Cleanup(func() {
		delete(taskUUIDOnlyCommands, c)
		delete(taskKeyCommands, c)
	})

	got := taskKeyGuardProblems(root)
	if len(got) != 1 || !strings.Contains(got[0], "root parent x") || !strings.Contains(got[0], "does not resolve keys") {
		t.Errorf("an undeclared task verb is reported by name: %v", got)
	}

	takesTaskUUIDOnly(c, "")
	got = taskKeyGuardProblems(root)
	if len(got) != 1 || !strings.Contains(got[0], "without a reason") {
		t.Errorf("an exemption without a reason is reported: %v", got)
	}

	takesTaskUUIDOnly(c, "it reads a task of another organization by uuid")
	if got = taskKeyGuardProblems(root); len(got) != 0 {
		t.Errorf("an exemption with a reason passes: %v", got)
	}

	delete(taskUUIDOnlyCommands, c)
	acceptTaskKeys(c, firstTaskArg, nil, nil)
	if got = taskKeyGuardProblems(root); len(got) != 0 {
		t.Errorf("a registered verb passes: %v", got)
	}
	acceptTaskKeys(c, firstTaskArg, nil, nil)
	got = taskKeyGuardProblems(root)
	if len(got) != 1 || !strings.Contains(got[0], "declared 2 times") {
		t.Errorf("a second registration is reported: %v", got)
	}

	// A task flag the registration forgot, and a registration resolving a positional the Use does not name.
	var other string
	f := &cobra.Command{Use: "y", Run: func(*cobra.Command, []string) {}}
	f.Flags().StringVar(&other, "for-task", "", "the task it is for")
	parent.AddCommand(f)
	t.Cleanup(func() { delete(taskKeyCommands, f) })
	acceptTaskKeys(f, firstTaskArg, nil, nil)
	got = taskKeyGuardProblems(root)
	var flagged, positional bool
	for _, p := range got {
		flagged = flagged || strings.Contains(p, "root parent y: 1 task flags [--for-task]")
		positional = positional || strings.Contains(p, "root parent y: Use \"y\" names a task argument false")
	}
	if !flagged || !positional {
		t.Errorf("a forgotten flag pointer and a mismatched positional are reported: %v", got)
	}
}
