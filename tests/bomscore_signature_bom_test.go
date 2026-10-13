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
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/relizaio/rearm/cmd"
	"github.com/relizaio/rearm/internal/bomscore"
)

// T-4 of SCORE-34 run 2 (design SCORE-34 ADR-10 step 1): a signed document behind a UTF-8 BOM, as
// some Windows tools write it, verifies through verify-signature and score --signature-key.

func TestBomSignatureWithUTF8BOM(t *testing.T) {
	const kid = "00000000-0000-4000-8000-000000000001"
	doc, err := os.ReadFile(jsf("signed-ed25519.cdx.json"))
	if err != nil {
		t.Fatal(err)
	}
	bomDoc := append([]byte{0xEF, 0xBB, 0xBF}, doc...)
	infile := filepath.Join(t.TempDir(), "signed-ed25519.bom.cdx.json")
	if err := os.WriteFile(infile, bomDoc, 0o600); err != nil {
		t.Fatal(err)
	}
	key := jsf("pub.pem")

	if run := runVerifySignature(cmd.BomVerifySignatureOptions{Key: key}, bomDoc); run.code != 0 ||
		run.stdout != "verified with key "+kid+" from "+key+"; algorithm Ed25519\n" {
		t.Errorf("verify-signature, stdin: exit %d stdout %q stderr %q, want 0 verified", run.code, run.stdout, run.stderr)
	}
	if run := runVerifySignature(cmd.BomVerifySignatureOptions{Infile: infile, Key: key}, nil); run.code != 0 ||
		!strings.HasPrefix(run.stdout, "verified with key "+kid+" from ") {
		t.Errorf("verify-signature, --infile: exit %d stdout %q stderr %q, want 0 verified", run.code, run.stdout, run.stderr)
	}
	if run := runVerifySignature(cmd.BomVerifySignatureOptions{}, bomDoc); run.code != 0 ||
		!strings.HasPrefix(run.stdout, "verified with the embedded publicKey") {
		t.Errorf("verify-signature, embedded key: exit %d stdout %q stderr %q, want 0 verified", run.code, run.stdout, run.stderr)
	}

	run := runBomScore(cmd.BomScoreOptions{Infile: infile, Format: "json", SignatureKey: key}, nil)
	var report bomscore.Report
	if run.code != 0 || json.Unmarshal([]byte(run.stdout), &report) != nil {
		t.Fatalf("score --signature-key: exit %d stderr %q, want 0 and a report", run.code, run.stderr)
	}
	if report.Input.Signature.Verification != bomscore.VerificationVerified {
		t.Errorf("score --signature-key: verification %s detail %q, want VERIFIED", report.Input.Signature.Verification, report.Input.Signature.Detail)
	}
	seen := 0
	for _, p := range report.Profiles {
		for _, c := range p.Checks {
			if c.ID == "cisa-2026.sbom-author-signature" {
				seen++
				if c.Status != bomscore.StatusPass {
					t.Errorf("score --signature-key: %s %s %q, want PASS", c.ID, c.Status, c.Note)
				}
			}
		}
	}
	if seen == 0 {
		t.Error("score --signature-key: no cisa-2026.sbom-author-signature check in the report")
	}
}
