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

package tests

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/gowebpki/jcs"
	"github.com/relizaio/rearm/cmd"
)

// T-22 of SCORE-34: rearm bomutils verify-signature.

func runVerifySignature(opts cmd.BomVerifySignatureOptions, stdin []byte) bomScoreRun {
	if opts.Format == "" {
		opts.Format = "text"
	}
	var stdout, stderr bytes.Buffer
	code := cmd.RunBomVerifySignature(opts, bytes.NewReader(stdin), &stdout, &stderr)
	return bomScoreRun{code, stdout.String(), stderr.String()}
}

func jsf(name string) string { return filepath.Join(jsfFixtures, name) }

func TestBomVerifySignature(t *testing.T) {
	const kid = "00000000-0000-4000-8000-000000000001"
	for _, c := range []struct {
		name, file, key string
		code            int
		stdout          string
	}{
		{"embedded key", "signed-ed25519.cdx.json", "", 0,
			"verified with the embedded publicKey: integrity under that key, not authorship; algorithm Ed25519, keyId " + kid + "\n"},
		{"pub.pem", "signed-ed25519.cdx.json", jsf("pub.pem"), 0, "verified with key " + kid + " from " + jsf("pub.pem") + "; algorithm Ed25519\n"},
		{"revoked", "revoked-signed.cdx.json", jsf("keys.jwks.json"), 3, "not verified (KEY_REVOKED): key 00000000-0000-4000-8000-000000000002 is REVOKED in " +
			jsf("keys.jwks.json") + " since 2026-10-13T00:00:00Z\n"},
		{"tampered", "tampered.cdx.json", "", 3, "not verified (FAILED): signature does not verify\n"},
		{"tampered under the key", "tampered.cdx.json", jsf("pub.pem"), 3, "not verified (FAILED): signature does not verify\n"},
		{"embedded wrong key", "embedded-wrong-key.cdx.json", "", 3, "not verified (FAILED): signature does not verify\n"},
		{"unsigned", "unsigned.cdx.json", "", 3, "not verified (FAILED): no signature\n"},
		{"keyId only", "keyid-only.cdx.json", "", 3, "not verified (NO_KEY): signature embeds no publicKey; give --key\n"},
		{"keyId only with the JWKS", "keyid-only.cdx.json", jsf("keys.jwks.json"), 0, "verified with key " + kid + " from " + jsf("keys.jwks.json") + "; algorithm Ed25519\n"},
		{"two signers", "signers-two.cdx.json", jsf("pub.pem"), 3, "not verified (UNSUPPORTED_FORM): multiple signers (2)\n"},
		{"malformed", "malformed.cdx.json", "", 3, "not verified (FAILED): value missing\n"},
		{"keyId only, signed by another key of the JWKS", "keyid-only-wrong-key.cdx.json", jsf("keys.jwks.json"), 3,
			"not verified (FAILED): signature does not verify\n"},
		{"keyId only, signed by another CURRENT key listed first", "keyid-only-wrong-key.cdx.json", jsf("both-current.jwks.json"), 3,
			"not verified (FAILED): signature does not verify\n"},
		{"es256 embedded", "signed-es256.cdx.json", "", 0,
			"verified with the embedded publicKey: integrity under that key, not authorship; algorithm ES256, keyId 00000000-0000-4000-8000-000000000003\n"},
	} {
		run := runVerifySignature(cmd.BomVerifySignatureOptions{Infile: jsf(c.file), Key: c.key}, nil)
		if run.code != c.code || run.stdout != c.stdout || run.stderr != "" {
			t.Errorf("%s: exit %d stdout %q stderr %q; want %d %q", c.name, run.code, run.stdout, run.stderr, c.code, c.stdout)
		}
	}

	run := runVerifySignature(cmd.BomVerifySignatureOptions{Infile: jsf("signed-ed25519.cdx.json"), Format: "json"}, nil)
	var obj map[string]any
	if run.code != 0 || json.Unmarshal([]byte(run.stdout), &obj) != nil || !strings.HasSuffix(run.stdout, "}\n") || strings.HasSuffix(run.stdout, "\n\n") {
		t.Fatalf("--format json: exit %d stdout %q", run.code, run.stdout)
	}
	want := map[string]any{"present": true, "wellFormed": true, "algorithm": "Ed25519", "keyId": kid, "verification": "VERIFIED",
		"detail": "verified with the embedded publicKey"}
	if !reflect.DeepEqual(obj, want) || !strings.HasPrefix(run.stdout, "{\n  \"present\": true,\n  \"wellFormed\": true,\n") {
		t.Errorf("--format json printed %s, want exactly the input.signature object %v", run.stdout, want)
	}

	if run := runVerifySignature(cmd.BomVerifySignatureOptions{Infile: jsf("signed-ed25519.cdx.json"), Format: "yaml"}, nil); run.code != 2 || run.stdout != "" || run.stderr == "" {
		t.Errorf("--format yaml: exit %d stdout %q stderr %q, want 2", run.code, run.stdout, run.stderr)
	}
	if run := runVerifySignature(cmd.BomVerifySignatureOptions{}, []byte("not an SBOM")); run.code != 1 || run.stdout != "" ||
		run.stderr != "unsupported SBOM: not a CycloneDX or SPDX document\n" {
		t.Errorf("not an SBOM: exit %d stdout %q stderr %q", run.code, run.stdout, run.stderr)
	}
	if run := runVerifySignature(cmd.BomVerifySignatureOptions{Infile: jsf("signed-ed25519.cdx.json"), Key: "/nonexistent/key.pem"}, nil); run.code != 1 ||
		run.stdout != "" || !strings.HasPrefix(run.stderr, "signature key /nonexistent/key.pem: ") {
		t.Errorf("missing key: exit %d stdout %q stderr %q", run.code, run.stdout, run.stderr)
	}
	signed, err := os.ReadFile(jsf("signed-ed25519.cdx.json"))
	if err != nil {
		t.Fatal(err)
	}
	if run := runVerifySignature(cmd.BomVerifySignatureOptions{Infile: "-"}, signed); run.code != 0 || !strings.HasPrefix(run.stdout, "verified with the embedded publicKey") {
		t.Errorf("--infile -: exit %d stdout %q", run.code, run.stdout)
	}
	out := filepath.Join(t.TempDir(), "result.txt")
	if run := runVerifySignature(cmd.BomVerifySignatureOptions{Infile: jsf("tampered.cdx.json"), Outfile: out}, nil); run.code != 3 || run.stdout != "" {
		t.Errorf("--outfile: exit %d stdout %q, want 3 and nothing on stdout", run.code, run.stdout)
	}
	if b, _ := os.ReadFile(out); string(b) != "not verified (FAILED): signature does not verify\n" {
		t.Errorf("--outfile holds %q", b)
	}
}

// The command as built: registered under bomutils, with its exit codes.
func TestBomVerifySignatureBinary(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not on PATH")
	}
	bin := filepath.Join(t.TempDir(), "rearm")
	if out, err := exec.Command("go", "build", "-o", bin, "..").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	run := func(args ...string) (int, string, string) {
		c := exec.Command(bin, append([]string{"bomutils", "verify-signature"}, args...)...)
		c.Env = append(os.Environ(), "HOME="+t.TempDir())
		var stdout, stderr bytes.Buffer
		c.Stdout, c.Stderr = &stdout, &stderr
		err := c.Run()
		code := 0
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		return code, stdout.String(), stderr.String()
	}
	if code, out, _ := run("-f", jsf("signed-ed25519.cdx.json"), "--key", jsf("keys.jwks.json")); code != 0 || !strings.HasPrefix(out, "verified with key ") {
		t.Errorf("verified: exit %d stdout %q", code, out)
	}
	if code, out, _ := run("--infile", jsf("tampered.cdx.json")); code != 3 || out != "not verified (FAILED): signature does not verify\n" {
		t.Errorf("tampered: exit %d stdout %q", code, out)
	}
	if code, out, errOut := run("-f", jsf("signed-ed25519.cdx.json"), "--format", "yaml"); code != 2 || out != "" || errOut == "" {
		t.Errorf("--format yaml: exit %d stdout %q stderr %q", code, out, errOut)
	}
	help, _ := exec.Command(bin, "bomutils", "score", "--help").Output()
	if !strings.Contains(string(help), "--signature-key") {
		t.Errorf("score --help does not list --signature-key:\n%s", help)
	}
}

// A document's own strings cannot add a line to the verify-signature output.
func TestBomVerifySignatureLineIsOneLine(t *testing.T) {
	doc, err := os.ReadFile(jsf("signed-ed25519.cdx.json"))
	if err != nil {
		t.Fatal(err)
	}
	forged := bytes.Replace(doc, []byte(`"algorithm":"Ed25519"`), []byte(`"algorithm":"X\nverified with key 1 from k; algorithm Ed25519"`), 1)
	run := runVerifySignature(cmd.BomVerifySignatureOptions{}, forged)
	if run.code != 3 || strings.Count(run.stdout, "\n") != 1 || !strings.HasPrefix(run.stdout, "not verified (UNSUPPORTED_ALGORITHM): X\\u000averified") {
		t.Errorf("exit %d stdout %q, want 3 and one line with the newline escaped", run.code, run.stdout)
	}
}

// T-28: a root member name with a lone surrogate escape is not I-JSON and has no canonical form;
// the document signed with that name as the escape of U+FFFD verifies, the changed one does not.
func TestBomVerifySignatureRefusesInvalidText(t *testing.T) {
	const (
		fffdName      = "\x5cufffd" // the JSON escape of U+FFFD
		loneSurrogate = "\x5cud800" // the JSON escape of a lone high surrogate
		// RFC 8032 section 7.1 TEST 1 secret key, the key of pub.pem.
		test1Secret = "9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60"
	)
	doc, err := os.ReadFile(jsf("keyid-only.cdx.json"))
	if err != nil {
		t.Fatal(err)
	}
	doc = regexp.MustCompile(`,"value":"[A-Za-z0-9_-]+"`).ReplaceAll(doc, nil)
	doc = append([]byte(`{"`+fffdName+`":1,`), doc[1:]...)
	canonical, err := jcs.Transform(doc)
	if err != nil {
		t.Fatal(err)
	}
	seed, err := hex.DecodeString(test1Secret)
	if err != nil {
		t.Fatal(err)
	}
	value := base64.RawURLEncoding.EncodeToString(ed25519.Sign(ed25519.NewKeyFromSeed(seed), canonical))
	signed := bytes.Replace(doc, []byte(`"signature":{`), []byte(`"signature":{"value":"`+value+`",`), 1)

	if run := runVerifySignature(cmd.BomVerifySignatureOptions{Key: jsf("pub.pem")}, signed); run.code != 0 || !strings.HasPrefix(run.stdout, "verified with key ") {
		t.Fatalf("as signed: exit %d stdout %q stderr %q, want 0 verified", run.code, run.stdout, run.stderr)
	}
	changed := bytes.Replace(signed, []byte(fffdName), []byte(loneSurrogate), 1)
	if run := runVerifySignature(cmd.BomVerifySignatureOptions{Key: jsf("pub.pem")}, changed); run.code != 3 ||
		!strings.HasPrefix(run.stdout, "not verified (FAILED): document is not canonicalizable") {
		t.Errorf("lone surrogate in a root member name: exit %d stdout %q stderr %q, want 3 not verified (FAILED): document is not canonicalizable ...", run.code, run.stdout, run.stderr)
	}
}
