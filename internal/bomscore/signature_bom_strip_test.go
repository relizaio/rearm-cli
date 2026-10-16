package bomscore

import (
	"bytes"
	"testing"
)

// T-4 of SCORE-34 run 2 (design SCORE-34 ADR-10 step 1): a UTF-8 BOM before a signed document is
// stripped before the first JCS transform, which refuses it; the signing input is that of the
// document without the BOM, and the signature verifies.

// withBOM is data behind a UTF-8 byte order mark, spelled as bytes so the source stays ASCII.
func withBOM(data []byte) []byte {
	return append([]byte{0xEF, 0xBB, 0xBF}, data...)
}

func TestSigningInputStripsTheUTF8BOM(t *testing.T) {
	for _, c := range []struct{ doc, canonical string }{
		{"signed-ed25519.cdx.json", "canonical/signed-ed25519.jcs"},
		{"signed-ed25519.pretty.cdx.json", "canonical/signed-ed25519.jcs"},
		{"signed-es256.cdx.json", "canonical/signed-es256.jcs"},
	} {
		got, err := signingInput(withBOM(readJSF(t, c.doc)))
		if err != nil {
			t.Errorf("%s with a BOM: %v", c.doc, err)
			continue
		}
		if want := readJSF(t, c.canonical); !bytes.Equal(got, want) {
			t.Errorf("%s with a BOM: signing input\n%s\nwant %s\n%s", c.doc, got, c.canonical, want)
		}
	}
}

func TestBOMPrefixedSignedDocumentVerifies(t *testing.T) {
	const kid = "00000000-0000-4000-8000-000000000001"
	for _, c := range []struct{ doc, key string }{
		{"signed-ed25519.cdx.json", "pub.pem"},
		{"signed-ed25519.pretty.cdx.json", "keys.jwks.json"},
		{"keyid-only.cdx.json", "pub.jwk.json"},
		{"signed-es256.cdx.json", "pub-es256.pem"},
	} {
		data := withBOM(readJSF(t, c.doc))
		rep, err := VerifySignature(data, jsfKey(c.key))
		if err != nil || rep.Verification != VerificationVerified {
			t.Errorf("%s with a BOM / %s: %+v %v, want VERIFIED", c.doc, c.key, rep, err)
			continue
		}
		r, check := scoreSignature(t, data, jsfKey(c.key))
		if r.Input.Signature.Verification != VerificationVerified || check.Status != StatusPass {
			t.Errorf("%s with a BOM / %s under --signature-key: %s, check %s %q, want VERIFIED and PASS",
				c.doc, c.key, r.Input.Signature.Verification, check.Status, check.Note)
		}
	}
	// The embedded publicKey path reads the same signing input.
	if rep, err := VerifySignature(withBOM(readJSF(t, "signed-ed25519.cdx.json")), ""); err != nil ||
		rep.Verification != VerificationVerified || rep.KeyID != kid {
		t.Errorf("signed-ed25519 with a BOM, embedded key: %+v %v, want VERIFIED with keyId %s", rep, err, kid)
	}
	// A BOM in front of a tampered document does not make it verify.
	if rep, err := VerifySignature(withBOM(readJSF(t, "tampered.cdx.json")), jsfKey("pub.pem")); err != nil ||
		rep.Verification != VerificationFailed || rep.Detail != "signature does not verify" {
		t.Errorf("tampered with a BOM: %+v %v, want FAILED, signature does not verify", rep, err)
	}
}
