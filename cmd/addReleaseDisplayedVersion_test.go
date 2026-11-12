package cmd

import (
	"strings"
	"testing"
)

// FDAW-74 row 23 (3.7a): the --displayed-version flag of rearm addrelease.

func TestSetDisplayedVersionSendsTheGivenValue(t *testing.T) {
	body := map[string]interface{}{}
	setDisplayedVersion(body, true, "GV-2.4.1 (Build 118)")
	if body["displayedVersion"] != "GV-2.4.1 (Build 118)" {
		t.Fatalf("displayedVersion = %v", body["displayedVersion"])
	}
}

func TestSetDisplayedVersionSendsAnEmptyValueAsTheClear(t *testing.T) {
	body := map[string]interface{}{}
	setDisplayedVersion(body, true, "")
	v, ok := body["displayedVersion"]
	if !ok || v != "" {
		t.Fatalf("want the key present with \"\", got %v (present %v)", v, ok)
	}
}

func TestSetDisplayedVersionOmitsTheKeyWithoutTheFlag(t *testing.T) {
	for _, value := range []string{"ignored", ""} {
		body := map[string]interface{}{}
		setDisplayedVersion(body, false, value)
		if _, ok := body["displayedVersion"]; ok {
			t.Fatalf("flag not given (%q): the key must be absent", value)
		}
	}
}

func TestSetDisplayedVersionNeitherCutsNorRefuses(t *testing.T) {
	over := strings.Repeat("9", 129)
	body := map[string]interface{}{}
	setDisplayedVersion(body, true, over)
	if body["displayedVersion"] != over {
		t.Fatalf("want all 129 characters sent")
	}
}

func TestDisplayedVersionFlagIsDeclared(t *testing.T) {
	f := addreleaseCmd.PersistentFlags().Lookup("displayed-version")
	if f == nil {
		t.Fatal("no --displayed-version flag")
	}
	if f.DefValue != "" {
		t.Fatalf("default = %q", f.DefValue)
	}
	want := "(Optional) The version the device shows its user, at most 128 characters on one line. Written into the release SBOM and the support documents."
	if f.Usage != want {
		t.Fatalf("usage = %q", f.Usage)
	}
}
