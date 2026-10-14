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
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/relizaio/rearm/cmd"
	"github.com/relizaio/rearm/internal/bomscore"
)

// Design SCORE-37: a key file carrying the private JWK member d (T-4, T-5 key-file rows) and a
// document whose embedded publicKey carries d (T-4, T-5 document rows), through the commands.

const (
	wantPrivateJWKRefusal = "private JWK (member d) is not a supported public key (JWK OKP Ed25519 or EC P-256/P-384/P-521)"
	wantLeakDetail        = "signature.publicKey carries private key material (d)"
	wantLeakNoKeyNote     = "present, not verified (no key given); signature.publicKey carries private key material (d)"
	wantLeakWithKeyNote   = "signature not verified with the given key (KEY_LEAKED: signature.publicKey carries private key material (d))"
)

// privateKeyFileRefusal is the one stderr line of a refused key file.
func privateKeyFileRefusal(key string) string {
	prefix := "signature key " + key + ": "
	if strings.HasSuffix(key, ".jwks.json") {
		prefix += "keys[1]: "
	}
	return prefix + wantPrivateJWKRefusal + "\n"
}

// noKeyMaterial fails when output carries any part of a fixture's d (the currentJWKD, revokedJWKD
// and es256JWKD of internal/bomscore/jsf_vectors_test.go).
func noKeyMaterial(t *testing.T, name, output string) {
	t.Helper()
	for _, d := range []string{"nWGxne_9", "TM0Imyj_", "ya-p2EW6"} {
		if strings.Contains(output, d) {
			t.Errorf("%s: the output carries key material %q: %q", name, d, output)
		}
	}
}

// T-4: bomutils score refuses a private key file (exit 1) and reports an embedded leaked key.
func TestBomScorePrivateKey(t *testing.T) {
	signed := jsf("signed-ed25519.cdx.json")
	for _, c := range []struct{ doc, key string }{
		{signed, jsf("with-d.jwk.json")},
		{signed, jsf("with-d.jwks.json")},
		{jsf("signed-es256.cdx.json"), jsf("with-d-es256.jwk.json")},
	} {
		for _, format := range []cmd.BomScoreFormat{cmd.BomScoreFormatText, cmd.BomScoreFormatJSON} {
			run := runBomScore(cmd.BomScoreOptions{Infile: c.doc, Format: format, SignatureKey: c.key}, nil)
			if run.code != 1 || run.stdout != "" || run.stderr != privateKeyFileRefusal(c.key) {
				t.Errorf("--signature-key %s --format %s: exit %d stdout %.80q stderr %q; want 1, nothing, %q",
					c.key, format, run.code, run.stdout, run.stderr, privateKeyFileRefusal(c.key))
			}
			noKeyMaterial(t, c.key, run.stderr)
		}
	}

	leaked := jsf("embedded-d.cdx.json")
	pub := jsf("pub.pem")
	run := runBomScore(cmd.BomScoreOptions{Infile: leaked, Format: cmd.BomScoreFormatJSON, SignatureKey: pub}, nil)
	var report bomscore.Report
	if run.code != 0 || json.Unmarshal([]byte(run.stdout), &report) != nil {
		t.Fatalf("embedded-d --signature-key pub.pem: exit %d stderr %q, want 0 and a report", run.code, run.stderr)
	}
	if s := report.Input.Signature; s.Verification != bomscore.VerificationKeyLeaked || s.Detail != wantLeakDetail || !s.WellFormed {
		t.Errorf("embedded-d with pub.pem: input.signature %+v, want KEY_LEAKED %q", s, wantLeakDetail)
	}
	if check := signatureCheck(report); check == nil || check.Passed != 0 || check.Note != wantLeakWithKeyNote {
		t.Errorf("embedded-d with pub.pem: signature check %+v, want passed 0 and note %q", check, wantLeakWithKeyNote)
	}
	if run := runBomScore(cmd.BomScoreOptions{Infile: leaked, Format: cmd.BomScoreFormatJSON, SignatureKey: pub, FailOnNotReady: true}, nil); run.code != 3 || run.stdout == "" {
		t.Errorf("embedded-d --fail-on-not-ready: exit %d stderr %q, want 3 and the report", run.code, run.stderr)
	}
	if run := runBomScore(cmd.BomScoreOptions{Infile: leaked, SignatureKey: pub}, nil); run.code != 0 ||
		!strings.HasPrefix(run.stdout, "signature: KEY_LEAKED, "+wantLeakDetail+"\n") {
		t.Errorf("embedded-d --format text: exit %d stdout %.120q, want 0 starting with the KEY_LEAKED line", run.code, run.stdout)
	}

	run = runBomScore(cmd.BomScoreOptions{Infile: leaked, Format: cmd.BomScoreFormatJSON}, nil)
	report = bomscore.Report{}
	if run.code != 0 || json.Unmarshal([]byte(run.stdout), &report) != nil {
		t.Fatalf("embedded-d without a key: exit %d stderr %q, want 0 and a report", run.code, run.stderr)
	}
	if s := report.Input.Signature; s.Verification != bomscore.VerificationNotRequested || s.Detail != wantLeakDetail {
		t.Errorf("embedded-d without a key: input.signature %+v, want NOT_REQUESTED %q", s, wantLeakDetail)
	}
	if check := signatureCheck(report); check == nil || check.Status != bomscore.StatusPass || check.Note != wantLeakNoKeyNote {
		t.Errorf("embedded-d without a key: signature check %+v, want PASS and note %q", check, wantLeakNoKeyNote)
	}
}

// T-5: bomutils verify-signature refuses a private key file (exit 1) and never verifies a
// document that embeds its private key (exit 3).
func TestBomVerifySignaturePrivateKey(t *testing.T) {
	const notVerified = "not verified (KEY_LEAKED): signature.publicKey carries private key material (d)\n"
	for _, c := range []struct {
		name, file, key string
		code            int
		stdout, stderr  string
	}{
		{"embedded-d without --key", "embedded-d.cdx.json", "", 3, notVerified, ""},
		{"embedded-d with pub.pem", "embedded-d.cdx.json", jsf("pub.pem"), 3, notVerified, ""},
		{"embedded-d with keys.jwks", "embedded-d.cdx.json", jsf("keys.jwks.json"), 3, notVerified, ""},
		{"--key with-d.jwk", "signed-ed25519.cdx.json", jsf("with-d.jwk.json"), 1, "", privateKeyFileRefusal(jsf("with-d.jwk.json"))},
		{"--key with-d.jwks", "signed-ed25519.cdx.json", jsf("with-d.jwks.json"), 1, "", privateKeyFileRefusal(jsf("with-d.jwks.json"))},
		{"--key with-d-es256.jwk", "signed-es256.cdx.json", jsf("with-d-es256.jwk.json"), 1, "", privateKeyFileRefusal(jsf("with-d-es256.jwk.json"))},
		{"embedded-d with with-d.jwk", "embedded-d.cdx.json", jsf("with-d.jwk.json"), 1, "", privateKeyFileRefusal(jsf("with-d.jwk.json"))},
	} {
		run := runVerifySignature(cmd.BomVerifySignatureOptions{Infile: jsf(c.file), Key: c.key}, nil)
		if run.code != c.code || run.stdout != c.stdout || run.stderr != c.stderr {
			t.Errorf("%s: exit %d stdout %q stderr %q; want %d %q %q", c.name, run.code, run.stdout, run.stderr, c.code, c.stdout, c.stderr)
		}
		noKeyMaterial(t, c.name, run.stdout+run.stderr)
	}

	for _, key := range []string{"", jsf("pub.pem")} {
		run := runVerifySignature(cmd.BomVerifySignatureOptions{Infile: jsf("embedded-d.cdx.json"), Key: key, Format: cmd.BomScoreFormatJSON}, nil)
		var obj map[string]any
		if run.code != 3 || json.Unmarshal([]byte(run.stdout), &obj) != nil {
			t.Fatalf("--format json key %q: exit %d stdout %q", key, run.code, run.stdout)
		}
		want := map[string]any{"present": true, "wellFormed": true, "algorithm": "Ed25519", "keyId": "00000000-0000-4000-8000-000000000001",
			"verification": "KEY_LEAKED", "detail": wantLeakDetail}
		if !reflect.DeepEqual(obj, want) {
			t.Errorf("--format json key %q printed %s, want the six members %v", key, run.stdout, want)
		}
	}
}

// T-7: the help texts name the private-key refusal and the leaked key.
func TestPrivateKeyHelp(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not on PATH")
	}
	bin := filepath.Join(t.TempDir(), "rearm")
	if out, err := exec.Command("go", "build", "-o", bin, "..").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	help := func(command string) string {
		c := exec.Command(bin, "bomutils", command, "--help")
		c.Env = append(os.Environ(), "HOME="+t.TempDir())
		var out bytes.Buffer
		c.Stdout = &out
		if err := c.Run(); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}
	for command, wants := range map[string][]string{
		"score":            {"or a JWK or JWKS entry carrying the private member d"},
		"verify-signature": {"or a JWK or JWKS entry carrying the private member d", "leaked key (the embedded publicKey carries the private member d)"},
	} {
		text := help(command)
		for _, want := range wants {
			if !strings.Contains(text, want) {
				t.Errorf("%s --help does not say %q:\n%s", command, want, text)
			}
		}
	}
}
