package cmd

import (
	"strings"
	"testing"

	"github.com/relizaio/rearm/internal/bomscore"
)

// SCORE-34: the verify-signature text is one line even when the signer put control characters
// into keyId or algorithm.
func TestVerifySignatureLineEscapesTheSignersStrings(t *testing.T) {
	rep := bomscore.SignatureReport{Present: true, WellFormed: true, Algorithm: "Ed25519\r", KeyID: "k\nverified with key other",
		Verification: bomscore.VerificationVerified}
	for _, key := range []string{"", "keys.jwks.json"} {
		line := verifySignatureLine(rep, key)
		if strings.ContainsAny(line, "\r\n") || !strings.Contains(line, `k\u000averified with key other`) || !strings.Contains(line, `Ed25519\u000d`) {
			t.Errorf("key %q: line %q, want one line with the control characters escaped", key, line)
		}
	}
}
