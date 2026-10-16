package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// T-23 (task SCORE-34, design ADR-6), updated by T-10 (task SCORE-38): exportreleasebom says on
// stderr whether the JSON document it wrote is signed, the stamp read exactly and checked against
// the root signature; stdout and the file stay the server's string byte for byte, exit 0.

func exportedDoc(props string) string {
	return exportedDocWith(props, "")
}

// exportedDocWith appends rest (a root signature member) to the document's root object.
func exportedDocWith(props, rest string) string {
	return `{"bomFormat":"CycloneDX","specVersion":"1.6","metadata":{"properties":[` +
		`{"name":"reliza:export:bomRefsDeduplicated","value":"2"}` + props + `]},"components":[{"name":"caf` + "\u00e9" + `"}]` + rest + `}`
}

func engineFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "internal", "bomscore", "testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(b)
}

func TestExportSigningNoticeOnStderr(t *testing.T) {
	const keyID = "00000000-0000-4000-8000-000000000001"
	noKeySentence := "not signed: no organization SBOM signing key is configured (Organization settings, SBOM export signing)\n"
	stamp := `,{"name":"reliza:export:signed","value":"true"},{"name":"reliza:export:signatureKeyId","value":"` + keyID + `"}`
	sig := func(k string) string {
		return `,"signature":{"algorithm":"Ed25519","keyId":"` + k + `","value":"AAAA"}`
	}
	signed := engineFixture(t, "jsf/signed-ed25519.cdx.json")
	for _, c := range []struct {
		name, doc, stderr string
	}{
		{"signed", signed, "signed with organization key " + keyID + "\n"},
		{"signed, inline", exportedDocWith(stamp, sig(keyID)), "signed with organization key " + keyID + "\n"},
		{"signed without the key id", exportedDocWith(`,{"name":"reliza:export:signed","value":"true"}`, sig(keyID)),
			"signing record inconsistent: reliza:export:signed is true but reliza:export:signatureKeyId is missing\n"},
		{"signed without a root signature", exportedDoc(stamp),
			"signing record inconsistent: reliza:export:signed is true but the document has no root signature\n"},
		{"key id mismatch", exportedDocWith(stamp, sig("other")),
			"signing record inconsistent: reliza:export:signatureKeyId is " + keyID + " but the root signature names keyId other\n"},
		{"no key", exportedDoc(`,{"name":"reliza:export:signed","value":"false"},{"name":"reliza:export:unsignedReason","value":"NO_KEY"}`), noKeySentence},
		{"signing error", exportedDoc(`,{"name":"reliza:export:signed","value":"false"},{"name":"reliza:export:unsignedReason","value":"SIGNING_ERROR"}`),
			"not signed: signing failed on the server (SIGNING_ERROR)\n"},
		{"other reason", exportedDoc(`,{"name":"reliza:export:signed","value":"false"},{"name":"reliza:export:unsignedReason","value":"LATER"}`), "not signed: LATER\n"},
		{"no reason", exportedDoc(`,{"name":"reliza:export:signed","value":"false"}`), "not signed\n"},
		{"forged NO_KEY export", engineFixture(t, "notice/forged-nokey.cdx.json"), noKeySentence},
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
