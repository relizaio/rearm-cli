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
	"path/filepath"
	"strings"
	"testing"

	"github.com/relizaio/rearm/cmd"
	"github.com/relizaio/rearm/internal/bomscore"
)

// T-6 of SCORE-34 run 3 (design SCORE-34 ADR-10 step 1): only a leading UTF-8 BOM is stripped. A
// U+FEFF inserted into a signed string value or member name after signing does not verify through
// verify-signature or score --signature-key; one between tokens is refused; the leading BOM alone
// still verifies.

func TestBomSignatureInteriorBOM(t *testing.T) {
	const kid = "00000000-0000-4000-8000-000000000001"
	doc, err := os.ReadFile(jsf("signed-ed25519.cdx.json"))
	if err != nil {
		t.Fatal(err)
	}
	bom := []byte{0xEF, 0xBB, 0xBF}
	splice := func(old string, parts ...[]byte) []byte {
		if !bytes.Contains(doc, []byte(old)) {
			t.Fatalf("fixture has no %q", old)
		}
		return bytes.Replace(doc, []byte(old), bytes.Join(parts, nil), 1)
	}
	inValue := splice(`"name":"alpha"`, []byte(`"name":"al`), bom, []byte(`pha"`))
	key := jsf("pub.pem")
	const failed = "not verified (FAILED): signature does not verify\n"

	for name, data := range map[string][]byte{
		"U+FEFF inside a string value":          inValue,
		"U+FEFF inside a member name":           splice(`"description"`, []byte(`"descr`), bom, []byte(`iption"`)),
		"leading BOM and U+FEFF inside a value": append(append([]byte{}, bom...), inValue...),
	} {
		infile := filepath.Join(t.TempDir(), "interior.cdx.json")
		if err := os.WriteFile(infile, data, 0o600); err != nil {
			t.Fatal(err)
		}
		for _, run := range []struct {
			how  string
			opts cmd.BomVerifySignatureOptions
			in   []byte
		}{
			{"stdin, --key", cmd.BomVerifySignatureOptions{Key: key}, data},
			{"--infile, --key", cmd.BomVerifySignatureOptions{Infile: infile, Key: key}, nil},
			{"stdin, embedded key", cmd.BomVerifySignatureOptions{}, data},
		} {
			if r := runVerifySignature(run.opts, run.in); r.code != 3 || r.stdout != failed || r.stderr != "" {
				t.Errorf("%s, verify-signature %s: exit %d stdout %q stderr %q, want 3 %q", name, run.how, r.code, r.stdout, r.stderr, failed)
			}
		}
		// The fixture is NOT_READY on other checks, so --fail-on-not-ready would exit 3 either way: the
		// report is what tells the cases apart.
		r := runBomScore(cmd.BomScoreOptions{Infile: infile, Format: "json", SignatureKey: key}, nil)
		var report bomscore.Report
		if r.code != 0 || json.Unmarshal([]byte(r.stdout), &report) != nil {
			t.Errorf("%s, score --signature-key: exit %d stderr %q, want 0 and a report", name, r.code, r.stderr)
			continue
		}
		if report.Input.Signature.Verification != bomscore.VerificationFailed || report.Input.Signature.Detail != "signature does not verify" {
			t.Errorf("%s, score --signature-key: %s %q, want FAILED, signature does not verify",
				name, report.Input.Signature.Verification, report.Input.Signature.Detail)
		}
		if check := signatureCheck(report); check == nil || check.Status != bomscore.StatusFail {
			t.Errorf("%s, score --signature-key: signature check %+v, want FAIL", name, check)
		}
	}

	// A U+FEFF between tokens is not JSON: refused, exit 1.
	between := splice(`,"components"`, []byte(","), bom, []byte(`"components"`))
	if r := runVerifySignature(cmd.BomVerifySignatureOptions{Key: key}, between); r.code != 1 || r.stdout != "" ||
		!strings.HasPrefix(r.stderr, "unparsable SBOM: ") {
		t.Errorf("U+FEFF between tokens, verify-signature: exit %d stdout %q stderr %q, want 1 unparsable SBOM", r.code, r.stdout, r.stderr)
	}
	if r := runBomScore(cmd.BomScoreOptions{Format: "json", SignatureKey: key}, between); r.code != 1 || r.stdout != "" ||
		!strings.HasPrefix(r.stderr, "unparsable SBOM: ") {
		t.Errorf("U+FEFF between tokens, score: exit %d stdout %q stderr %q, want 1 unparsable SBOM", r.code, r.stdout, r.stderr)
	}

	// The leading BOM alone still verifies.
	leading := append(append([]byte{}, bom...), doc...)
	if r := runVerifySignature(cmd.BomVerifySignatureOptions{Key: key}, leading); r.code != 0 ||
		r.stdout != "verified with key "+kid+" from "+key+"; algorithm Ed25519\n" {
		t.Errorf("leading BOM, verify-signature: exit %d stdout %q stderr %q, want 0 verified", r.code, r.stdout, r.stderr)
	}
	r := runBomScore(cmd.BomScoreOptions{Format: "json", SignatureKey: key}, leading)
	var report bomscore.Report
	if r.code != 0 || json.Unmarshal([]byte(r.stdout), &report) != nil {
		t.Fatalf("leading BOM, score --signature-key: exit %d stderr %q, want 0 and a report", r.code, r.stderr)
	}
	if check := signatureCheck(report); report.Input.Signature.Verification != bomscore.VerificationVerified ||
		check == nil || check.Status != bomscore.StatusPass {
		t.Errorf("leading BOM, score --signature-key: %s, check %+v, want VERIFIED and PASS", report.Input.Signature.Verification, check)
	}
}
