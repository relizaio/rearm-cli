package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// T-23 (task SCORE-34, design ADR-6): exportreleasebom says on stderr whether the JSON document it
// wrote is signed; stdout and the file stay the server's string byte for byte.

func exportedDoc(props string) string {
	return `{"bomFormat":"CycloneDX","specVersion":"1.6","metadata":{"properties":[` +
		`{"name":"reliza:export:bomRefsDeduplicated","value":"2"}` + props + `]},"components":[{"name":"caf` + "\u00e9" + `"}]}`
}

func TestExportSigningNoticeOnStderr(t *testing.T) {
	const keyID = "00000000-0000-4000-8000-000000000001"
	noKeySentence := "not signed: no organization SBOM signing key is configured (Organization settings, SBOM export signing)\n"
	for _, c := range []struct {
		name, doc, stderr string
	}{
		{"signed", exportedDoc(`,{"name":"reliza:export:signed","value":"true"},{"name":"reliza:export:signatureKeyId","value":"` + keyID + `"}`),
			"signed with organization key " + keyID + "\n"},
		{"signed without the key id", exportedDoc(`,{"name":"reliza:export:signed","value":"true"}`), "signed with organization key <unknown>\n"},
		{"no key", exportedDoc(`,{"name":"reliza:export:signed","value":"false"},{"name":"reliza:export:unsignedReason","value":"NO_KEY"}`), noKeySentence},
		{"signing error", exportedDoc(`,{"name":"reliza:export:signed","value":"false"},{"name":"reliza:export:unsignedReason","value":"SIGNING_ERROR"}`),
			"not signed: signing failed on the server (SIGNING_ERROR)\n"},
		{"other reason", exportedDoc(`,{"name":"reliza:export:signed","value":"false"},{"name":"reliza:export:unsignedReason","value":"LATER"}`), "not signed: LATER\n"},
		{"no reason", exportedDoc(`,{"name":"reliza:export:signed","value":"false"}`), "not signed\n"},
		{"older server", exportedDoc(""), ""},
		{"no metadata", releaseBomDoc, ""},
		{"not JSON", "name,version\ncaf\u00e9,1.0\n", ""},
	} {
		releaseBomWorld(t, exportAnswer(c.doc))
		stdout, stderr, code := runReleaseBomCmd(t, newExportReleaseBomCmd, "--releaseid", "r-1")
		if code != 0 || stdout != c.doc || stderr != c.stderr {
			t.Errorf("%s: exit %d stdout %q stderr %q; want 0, the document byte for byte, stderr %q", c.name, code, stdout, stderr, c.stderr)
		}
	}

	signed := exportedDoc(`,{"name":"reliza:export:signed","value":"true"},{"name":"reliza:export:signatureKeyId","value":"` + keyID + `"}`)
	releaseBomWorld(t, exportAnswer(signed))
	out := filepath.Join(t.TempDir(), "release.cdx.json")
	stdout, stderr, code := runReleaseBomCmd(t, newExportReleaseBomCmd, "--releaseid", "r-1", "--outfile", out)
	written, err := os.ReadFile(out)
	if code != 0 || err != nil || string(written) != signed || stdout != out+"\n" || stderr != "signed with organization key "+keyID+"\n" {
		t.Errorf("--outfile: exit %d err %v file %q stdout %q stderr %q", code, err, written, stdout, stderr)
	}

	// CSV carries no CycloneDX document: nothing about signing, even if the text looks like one.
	releaseBomWorld(t, exportAnswer(signed))
	stdout, stderr, code = runReleaseBomCmd(t, newExportReleaseBomCmd, "--releaseid", "r-1", "--mediatype", "CSV")
	if code != 0 || stdout != signed || strings.Contains(stderr, "signed") {
		t.Errorf("CSV: exit %d stdout %q stderr %q; want nothing about signing", code, stdout, stderr)
	}
}
