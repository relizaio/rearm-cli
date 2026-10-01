package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func settingsWith(t *testing.T, body string) (string, func()) {
	t.Helper()
	dir := t.TempDir()
	prev, _ := os.Getwd()
	if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ".claude", "settings.json")
	if body != "" {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return path, func() { os.Chdir(prev) }
}

func loadSettings(t *testing.T, path string) map[string]interface{} {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestInstallPreservesEverythingElseInTheSettingsFile(t *testing.T) {
	// The settings file is the user's. Losing an unrelated key to a usage feature would be a far
	// worse bug than never reporting usage at all, and it is the kind that is noticed late.
	path, done := settingsWith(t, `{
      "model": "opus",
      "permissions": {"allow": ["Bash(ls:*)"]},
      "hooks": {"PreToolUse": [{"hooks":[{"type":"command","command":"echo mine"}]}]}
    }`)
	defer done()

	settings, err := readClaudeSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	installClaudeHook(settings, "Stop", claudeStopHookCommand)
	installClaudeHook(settings, "SessionEnd", claudeSessionEndHookCommand)
	if err := writeClaudeSettings(path, settings); err != nil {
		t.Fatal(err)
	}

	got := loadSettings(t, path)
	if got["model"] != "opus" {
		t.Errorf("unrelated top-level key lost: %+v", got)
	}
	if _, ok := got["permissions"]; !ok {
		t.Errorf("permissions block lost: %+v", got)
	}
	hooks := got["hooks"].(map[string]interface{})
	if _, ok := hooks["PreToolUse"]; !ok {
		t.Errorf("someone else's hook event was dropped: %+v", hooks)
	}
	if len(hooks["Stop"].([]interface{})) != 1 {
		t.Errorf("expected our Stop hook, got %+v", hooks["Stop"])
	}
}

func TestInstallingTwiceDoesNotDoubleTheHook(t *testing.T) {
	// A duplicated Stop hook fires the report twice per turn. Harmless to the numbers (the server
	// dedupes on the sequence) but it doubles the work on every turn forever.
	path, done := settingsWith(t, "")
	defer done()
	settings, _ := readClaudeSettings(path)
	installClaudeHook(settings, "Stop", claudeStopHookCommand)
	installClaudeHook(settings, "Stop", claudeStopHookCommand)
	writeClaudeSettings(path, settings)

	hooks := loadSettings(t, path)["hooks"].(map[string]interface{})
	if n := len(hooks["Stop"].([]interface{})); n != 1 {
		t.Errorf("expected exactly one Stop entry, got %d", n)
	}
}

func TestUninstallRemovesOnlyWhatWeAdded(t *testing.T) {
	path, done := settingsWith(t, `{
      "hooks": {"Stop": [{"hooks":[{"type":"command","command":"echo someone-elses"}]}]}
    }`)
	defer done()
	settings, _ := readClaudeSettings(path)
	installClaudeHook(settings, "Stop", claudeStopHookCommand)
	writeClaudeSettings(path, settings)

	settings, _ = readClaudeSettings(path)
	if !uninstallClaudeHook(settings, "Stop", claudeStopHookCommand) {
		t.Fatal("uninstall reported nothing removed")
	}
	writeClaudeSettings(path, settings)

	hooks := loadSettings(t, path)["hooks"].(map[string]interface{})
	entries := hooks["Stop"].([]interface{})
	if len(entries) != 1 {
		t.Fatalf("expected the other hook to survive alone, got %+v", entries)
	}
	if cmds := claudeHookCommandOf(entries[0]); len(cmds) != 1 || cmds[0] != "echo someone-elses" {
		t.Errorf("wrong entry survived: %+v", cmds)
	}
}

func TestUninstallLeavesASharedEntryAlone(t *testing.T) {
	// An entry that bundles our command with someone else's is not ours to delete. A stray usage
	// report is a much smaller harm than silently removing a hook the user depends on.
	path, done := settingsWith(t, `{
      "hooks": {"Stop": [{"hooks":[
        {"type":"command","command":"rearm agent claude usage --from-hook"},
        {"type":"command","command":"echo also-mine"}
      ]}]}
    }`)
	defer done()
	settings, _ := readClaudeSettings(path)
	if uninstallClaudeHook(settings, "Stop", claudeStopHookCommand) {
		t.Error("a shared entry must not be removed")
	}
}

func TestUnparseableSettingsAreRefusedRatherThanOverwritten(t *testing.T) {
	path, done := settingsWith(t, `{"model": "opus",`)
	defer done()
	if _, err := readClaudeSettings(path); err == nil {
		t.Fatal("expected a refusal on invalid JSON, not a silent overwrite")
	}
	// And the file is untouched.
	raw, _ := os.ReadFile(path)
	if string(raw) != `{"model": "opus",` {
		t.Errorf("the unreadable file was modified: %q", raw)
	}
}

func TestAMissingSettingsFileIsAnEmptyStartNotAnError(t *testing.T) {
	dir := t.TempDir()
	settings, err := readClaudeSettings(filepath.Join(dir, "nope.json"))
	if err != nil {
		t.Fatalf("a missing settings file should not be an error: %v", err)
	}
	if len(settings) != 0 {
		t.Errorf("expected an empty map, got %+v", settings)
	}
}
