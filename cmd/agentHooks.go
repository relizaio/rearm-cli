/*
The MIT License (MIT)

Copyright (c) 2020 - 2026 Reliza Incorporated (Reliza (tm), https://reliza.io)

Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated documentation files (the "Software"),
to deal in the Software without restriction, including without limitation the rights to use, copy, modify, merge, publish, distribute, sublicense,
and/or sell copies of the Software, and to permit persons to whom the Software is furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY,
WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
*/

package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

// Hook installation for Claude Code.
//
// Two hooks: Stop reports each turn as it finishes, SessionEnd sends the final flush. Both read the
// transcript Claude Code already writes, so they add no token cost.
//
// The settings file belongs to the user, not to us. Everything here merges rather than replaces,
// touches only entries whose command is recognisably ours, and rewrites the file with its other
// contents intact -- including keys this CLI knows nothing about.

const (
	stopHookCommand       = "rearm agent session usage --from-hook"
	sessionEndHookCommand = "rearm agent session usage --from-hook --final"
)

var (
	hooksProject bool
	hooksUser    bool
	hooksAgent   string
)

// hooksSettingsPath resolves which settings file to edit. Project scope is the default: usage
// belongs to the work being done, and a user-scope hook would report every unrelated Claude Code
// session on the machine.
func hooksSettingsPath() (string, error) {
	if hooksUser {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".claude", "settings.json"), nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Join(cwd, ".claude", "settings.json"), nil
}

// readSettings loads the settings file as a generic map. A missing file is an empty map, not an
// error -- installing into a project that has no settings yet is the common case.
//
// Generic map rather than a typed struct on purpose: settings.json holds many keys this CLI has no
// business knowing about, and unmarshalling into a struct would silently drop every one of them on
// the way back out.
func readSettings(path string) (map[string]interface{}, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]interface{}{}, nil
		}
		return nil, err
	}
	if len(raw) == 0 {
		return map[string]interface{}{}, nil
	}
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		// Refusing is the only safe answer: writing our version of a file we could not parse would
		// destroy whatever the user actually had in it.
		return nil, fmt.Errorf("%s is not valid JSON; fix or move it before installing hooks: %w", path, err)
	}
	if m == nil {
		m = map[string]interface{}{}
	}
	return m, nil
}

func writeSettings(path string, settings map[string]interface{}) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	body, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// hookCommandOf digs the command string out of one entry of a hooks array, or returns "".
func hookCommandOf(entry interface{}) []string {
	m, ok := entry.(map[string]interface{})
	if !ok {
		return nil
	}
	inner, ok := m["hooks"].([]interface{})
	if !ok {
		return nil
	}
	var cmds []string
	for _, h := range inner {
		hm, ok := h.(map[string]interface{})
		if !ok {
			continue
		}
		if c, ok := hm["command"].(string); ok {
			cmds = append(cmds, c)
		}
	}
	return cmds
}

func hasHookCommand(entries []interface{}, command string) bool {
	for _, e := range entries {
		for _, c := range hookCommandOf(e) {
			if c == command {
				return true
			}
		}
	}
	return false
}

func newHookEntry(command string) map[string]interface{} {
	return map[string]interface{}{
		"hooks": []interface{}{
			map[string]interface{}{"type": "command", "command": command},
		},
	}
}

// installHook adds one hook to one event, preserving everything already there. Idempotent: an
// entry carrying our exact command is left alone rather than duplicated, so running install twice
// does not fire the report twice per turn.
func installHook(settings map[string]interface{}, event, command string) bool {
	hooks, _ := settings["hooks"].(map[string]interface{})
	if hooks == nil {
		hooks = map[string]interface{}{}
		settings["hooks"] = hooks
	}
	entries, _ := hooks[event].([]interface{})
	if hasHookCommand(entries, command) {
		return false
	}
	hooks[event] = append(entries, newHookEntry(command))
	return true
}

// uninstallHook removes exactly the entries this CLI added and nothing else. An entry that also
// carries someone else's command is left in place -- removing it would take an unrelated hook with
// it, and a stray usage report is a far smaller harm than a silently deleted hook.
func uninstallHook(settings map[string]interface{}, event, command string) bool {
	hooks, _ := settings["hooks"].(map[string]interface{})
	if hooks == nil {
		return false
	}
	entries, _ := hooks[event].([]interface{})
	if entries == nil {
		return false
	}
	kept := make([]interface{}, 0, len(entries))
	removed := false
	for _, e := range entries {
		cmds := hookCommandOf(e)
		if len(cmds) == 1 && cmds[0] == command {
			removed = true
			continue
		}
		kept = append(kept, e)
	}
	if !removed {
		return false
	}
	if len(kept) == 0 {
		delete(hooks, event)
	} else {
		hooks[event] = kept
	}
	if len(hooks) == 0 {
		delete(settings, "hooks")
	}
	return true
}

var agentHooksCmd = &cobra.Command{
	Use:   "hooks",
	Short: "Install or remove the usage-reporting hooks for your agent",
}

var agentHooksInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Wire usage reporting into Claude Code's settings (merges; safe to re-run)",
	Long: `Adds two hooks to .claude/settings.json (--project, the default) or
~/.claude/settings.json (--user):

  Stop        rearm agent session usage --from-hook
  SessionEnd  rearm agent session usage --from-hook --final

Existing hooks are preserved and ours is added once, so re-running changes
nothing. 'hooks uninstall' removes exactly these two.

The hooks read the transcript Claude Code already writes, so they cost no extra
tokens, and they exit 0 on any failure so they cannot block your work.`,
	Run: func(cmd *cobra.Command, args []string) {
		if hooksAgent != "" && hooksAgent != "claude-code" {
			fmt.Fprintf(os.Stderr, "rearm: only --agent claude-code is supported\n")
			os.Exit(1)
		}
		path, err := hooksSettingsPath()
		if err != nil {
			fmt.Fprintf(os.Stderr, "rearm: %v\n", err)
			os.Exit(1)
		}
		settings, err := readSettings(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "rearm: %v\n", err)
			os.Exit(1)
		}
		a := installHook(settings, "Stop", stopHookCommand)
		b := installHook(settings, "SessionEnd", sessionEndHookCommand)
		if !a && !b {
			fmt.Printf("hooks already installed in %s\n", path)
			return
		}
		if err := writeSettings(path, settings); err != nil {
			fmt.Fprintf(os.Stderr, "rearm: could not write %s: %v\n", path, err)
			os.Exit(1)
		}
		fmt.Printf("installed usage hooks in %s\n", path)
	},
}

var agentHooksUninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove the usage-reporting hooks this CLI installed",
	Run: func(cmd *cobra.Command, args []string) {
		path, err := hooksSettingsPath()
		if err != nil {
			fmt.Fprintf(os.Stderr, "rearm: %v\n", err)
			os.Exit(1)
		}
		settings, err := readSettings(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "rearm: %v\n", err)
			os.Exit(1)
		}
		a := uninstallHook(settings, "Stop", stopHookCommand)
		b := uninstallHook(settings, "SessionEnd", sessionEndHookCommand)
		if !a && !b {
			fmt.Printf("no rearm usage hooks found in %s\n", path)
			return
		}
		if err := writeSettings(path, settings); err != nil {
			fmt.Fprintf(os.Stderr, "rearm: could not write %s: %v\n", path, err)
			os.Exit(1)
		}
		fmt.Printf("removed usage hooks from %s\n", path)
	},
}

func init() {
	for _, c := range []*cobra.Command{agentHooksInstallCmd, agentHooksUninstallCmd} {
		c.Flags().BoolVar(&hooksProject, "project", false, "edit .claude/settings.json in the current directory (default)")
		c.Flags().BoolVar(&hooksUser, "user", false, "edit ~/.claude/settings.json instead")
		c.Flags().StringVar(&hooksAgent, "agent", "claude-code", "agent whose hooks to manage")
	}
	agentHooksCmd.AddCommand(agentHooksInstallCmd)
	agentHooksCmd.AddCommand(agentHooksUninstallCmd)
}
