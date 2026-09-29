package cmd

import (
	"reflect"
	"strings"
	"testing"
)

// rearm apikey (task RD3-11): the group and its verbs, the mint's checks and its note, and a list that
// sends names only when given.
func TestApiKeyCommandsAreThere(t *testing.T) {
	var verbs []string
	for _, c := range apiKeyCmd.Commands() {
		verbs = append(verbs, c.Name())
	}
	if !reflect.DeepEqual(verbs, []string{"apply", "export", "list", "mint"}) {
		t.Errorf("apikey verbs: %v", verbs)
	}
	if f := apiKeyMintCmd.Flag("slot"); f == nil || f.DefValue != "1" {
		t.Errorf("mint --slot defaults to 1: %v", f)
	}
	for _, flag := range []string{"rotate"} {
		if apiKeyMintCmd.Flag(flag) == nil {
			t.Errorf("mint has no --%s", flag)
		}
	}
	if apiKeyExportCmd.Flag("key") == nil || apiKeyListCmd.Flag("key") == nil {
		t.Error("export and list take --key")
	}
	if apiKeyApplyCmd.Flag("file") == nil || apiKeyApplyCmd.Flag("dry-run") == nil {
		t.Error("apply takes -f and --dry-run")
	}
	if !strings.Contains(apiKeyApplyCmd.Long, "FREEFORM keys only") {
		t.Errorf("the apply says which keys a file declares: %s", apiKeyApplyCmd.Long)
	}
	if !strings.Contains(apiKeyExportCmd.Long, "authoritative: true") || !strings.Contains(apiKeyExportCmd.Long, "deactivated") {
		t.Errorf("the export says what authoritative means: %s", apiKeyExportCmd.Long)
	}
	if !strings.Contains(apiKeyMintCmd.Long, "no file\ndeclares") {
		t.Errorf("the mint says it reaches declared keys only: %s", apiKeyMintCmd.Long)
	}
	if !strings.Contains(apiKeyMintCmd.Long, "prints it once") {
		t.Errorf("the mint says its value is shown once: %s", apiKeyMintCmd.Long)
	}
}

func TestApiKeyMintChecksBeforeSending(t *testing.T) {
	got, err := apiKeyMintVariables("ci-release", 2, true)
	if err != nil || !reflect.DeepEqual(got, map[string]interface{}{"key": "ci-release", "slot": 2, "rotate": true}) {
		t.Errorf("mint variables: %v %v", got, err)
	}
	for _, bad := range []struct {
		key  string
		slot int
	}{{"ci-release", 0}, {"ci-release", 3}, {" ", 1}} {
		if _, err := apiKeyMintVariables(bad.key, bad.slot, false); err == nil {
			t.Errorf("%+v accepted", bad)
		}
	}
	if note := apiKeyMintNote(map[string]interface{}{"minted": true, "slot": 1}); note != "" {
		t.Errorf("a minted secret needs no note: %q", note)
	}
	if note := apiKeyMintNote(map[string]interface{}{"minted": false, "slot": 2}); !strings.Contains(note, "slot 2 already holds a secret") ||
		!strings.Contains(note, "--rotate") {
		t.Errorf("a held slot says so and names --rotate: %q", note)
	}
}

func TestApiKeyListSendsNamesOnlyWhenGiven(t *testing.T) {
	if _, has := apiKeyListVariables(nil)["keys"]; has {
		t.Error("no names lists every key")
	}
	if got := apiKeyListVariables([]string{"ci-release"})["keys"]; !reflect.DeepEqual(got, []string{"ci-release"}) {
		t.Errorf("names: %v", got)
	}
}
