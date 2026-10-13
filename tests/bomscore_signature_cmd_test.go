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
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/relizaio/rearm/cmd"
	"github.com/relizaio/rearm/internal/bomscore"
)

// T-21 of SCORE-34: rearm bomutils score --signature-key.

const jsfFixtures = "../internal/bomscore/testdata/jsf"

func TestBomScoreSignatureKey(t *testing.T) {
	key := filepath.Join(jsfFixtures, "pub.pem")
	for name, want := range map[string]bomscore.Verification{
		"signed-ed25519.cdx.json":     bomscore.VerificationVerified,
		"tampered.cdx.json":           bomscore.VerificationFailed,
		"embedded-wrong-key.cdx.json": bomscore.VerificationKeyMismatch,
		"unsigned.cdx.json":           bomscore.VerificationFailed,
	} {
		run := runBomScore(cmd.BomScoreOptions{Infile: filepath.Join(jsfFixtures, name), Format: "json", SignatureKey: key}, nil)
		var report bomscore.Report
		if run.code != 0 || json.Unmarshal([]byte(run.stdout), &report) != nil {
			t.Errorf("%s: exit %d stderr %q, want 0 and a report", name, run.code, run.stderr)
			continue
		}
		if report.Input.Signature.Verification != want || report.Options.SignatureKey != key {
			t.Errorf("%s: verification %s options %+v, want %s and the key echoed", name, report.Input.Signature.Verification, report.Options, want)
		}
	}
	tampered := filepath.Join(jsfFixtures, "tampered.cdx.json")
	if run := runBomScore(cmd.BomScoreOptions{Infile: tampered, SignatureKey: key, FailOnNotReady: true}, nil); run.code != 3 ||
		!strings.HasPrefix(run.stdout, "signature: FAILED, signature does not verify\n") {
		t.Errorf("tampered --fail-on-not-ready: exit %d stdout %.200q, want 3 and the report", run.code, run.stdout)
	}

	// ADR-9: under a key the check passes only on VERIFIED; a signature the engine cannot verify
	// fails it, so --fail-on-not-ready exits 3.
	for name, want := range map[string]struct {
		verification bomscore.Verification
		detail       string
	}{
		"rs256.cdx.json":       {bomscore.VerificationUnsupportedAlgorithm, "RS256"},
		"excludes.cdx.json":    {bomscore.VerificationUnsupportedForm, "excludes"},
		"signers-two.cdx.json": {bomscore.VerificationUnsupportedForm, "multiple signers (2)"},
	} {
		infile := filepath.Join(jsfFixtures, name)
		note := "signature not verified with the given key (" + string(want.verification) + ": " + want.detail + ")"
		run := runBomScore(cmd.BomScoreOptions{Infile: infile, Format: "json", SignatureKey: key, FailOnNotReady: true}, nil)
		var report bomscore.Report
		if run.code != 3 || json.Unmarshal([]byte(run.stdout), &report) != nil {
			t.Errorf("%s --fail-on-not-ready: exit %d stderr %q, want 3 and a report", name, run.code, run.stderr)
			continue
		}
		if check := signatureCheck(report); check == nil || check.Passed != 0 || check.Note != note {
			t.Errorf("%s: signature check %+v, want passed 0 and note %q", name, check, note)
		}
		run = runBomScore(cmd.BomScoreOptions{Infile: infile, Format: "json", SignatureKey: key}, nil)
		report = bomscore.Report{}
		if run.code != 0 || json.Unmarshal([]byte(run.stdout), &report) != nil || report.Input.Signature.Verification != want.verification {
			t.Errorf("%s without --fail-on-not-ready: exit %d verification %s, want 0 and %s", name, run.code, report.Input.Signature.Verification, want.verification)
		}
	}

	missing := "/nonexistent/key.pem"
	if run := runBomScore(cmd.BomScoreOptions{Infile: tampered, SignatureKey: missing}, nil); run.code != 1 || run.stdout != "" ||
		!strings.Contains(run.stderr, missing) || strings.Count(run.stderr, "\n") != 1 {
		t.Errorf("missing key: exit %d stdout %q stderr %q, want 1, nothing, one line naming the path", run.code, run.stdout, run.stderr)
	}

	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&rsaKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	rsaPEM := filepath.Join(t.TempDir(), "rsa.pem")
	if err := os.WriteFile(rsaPEM, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), 0644); err != nil {
		t.Fatal(err)
	}
	want := "signature key " + rsaPEM + ": RSA public key is not a supported public key (PEM SubjectPublicKeyInfo of an Ed25519 or EC P-256/P-384/P-521 key)\n"
	if run := runBomScore(cmd.BomScoreOptions{Infile: tampered, SignatureKey: rsaPEM}, nil); run.code != 1 || run.stdout != "" || run.stderr != want {
		t.Errorf("RSA key: exit %d stdout %q stderr %q, want 1 and %q", run.code, run.stdout, run.stderr, want)
	}
}

// signatureCheck finds the cisa-2026 signature check of a report.
func signatureCheck(r bomscore.Report) *bomscore.CheckResult {
	for _, p := range r.Profiles {
		for i := range p.Checks {
			if p.Checks[i].ID == "cisa-2026.sbom-author-signature" {
				return &p.Checks[i]
			}
		}
	}
	return nil
}

// The --signature-key help line says the check is a gate on verification (ADR-9).
func TestBomScoreSignatureKeyHelp(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not on PATH")
	}
	bin := filepath.Join(t.TempDir(), "rearm")
	if out, err := exec.Command("go", "build", "-o", bin, "..").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	c := exec.Command(bin, "bomutils", "score", "--help")
	c.Env = append(os.Environ(), "HOME="+t.TempDir())
	help, err := c.Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(help), "passes only when the signature verifies") {
		t.Errorf("score --help does not say the check passes only when the signature verifies:\n%s", help)
	}
}
