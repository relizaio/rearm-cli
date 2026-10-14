package bomscore

import (
	"strings"
	"testing"
)

// Design SCORE-37: a key file carrying the private JWK member d is refused (ADR-1, T-1); an embedded
// publicKey carrying d is KEY_LEAKED (ADR-2, T-2, T-3); the rule is the member name d, any value,
// any kty (ADR-3). The expected strings are the design's literals, not the production constants.

const (
	wantPrivateJWK  = "private JWK (member d)" + wantJWKTail
	wantLeakDetail  = "signature.publicKey carries private key material (d)"
	wantLeakNoKey   = "present, not verified (no key given); signature.publicKey carries private key material (d)"
	wantLeakWithKey = "signature not verified with the given key (KEY_LEAKED: signature.publicKey carries private key material (d))"
)

// The d values of the fixtures (RFC 8032 TEST 1 and TEST 2, RFC 6979 A.2.5): a refusal never
// carries any part of them.
var fixtureDPrefixes = []string{currentJWKD[:8], revokedJWKD[:8], es256JWKD[:8]}

// assertKeyFileRefused loads path and wants exactly "signature key <path>: <want>", without key
// material.
func assertKeyFileRefused(t *testing.T, name, path, want string) {
	t.Helper()
	_, err := LoadKeySet(path)
	full := "signature key " + path + ": " + want
	if err == nil || err.Error() != full {
		t.Errorf("%s: %v, want %q", name, err, full)
		return
	}
	for _, d := range append(fixtureDPrefixes, "AQAB", "c2VjcmV0") {
		if strings.Contains(err.Error(), d) {
			t.Errorf("%s: the message carries key material %q: %v", name, d, err)
		}
	}
}

// T-1: a JWK or a JWKS carrying d is refused with the private-key message, whatever the value or
// the kty; near misses of the name d are not.
func TestPrivateJWKKeyFilesAreRefused(t *testing.T) {
	assertKeyFileRefused(t, "with-d.jwk.json", jsfKey("with-d.jwk.json"), wantPrivateJWK)
	assertKeyFileRefused(t, "with-d-es256.jwk.json", jsfKey("with-d-es256.jwk.json"), wantPrivateJWK)
	// The clean entry the keyId would select comes first: the file is refused whole all the same.
	assertKeyFileRefused(t, "with-d.jwks.json", jsfKey("with-d.jwks.json"), "keys[1]: "+wantPrivateJWK)

	dir := t.TempDir()
	x := currentJWKX
	okp := func(extra string) []byte { return []byte(`{"kty":"OKP","crv":"Ed25519","x":"` + x + `"` + extra + `}`) }
	const rsaPrivate = `{"kty":"RSA","n":"AQAB","e":"AQAB","d":"AQAB"}`
	for _, c := range []struct {
		name    string
		content []byte
		want    string
	}{
		// ADR-3: any value.
		{"d null", okp(`,"d":null`), wantPrivateJWK},
		{"d empty", okp(`,"d":""`), wantPrivateJWK},
		{"d a number", okp(`,"d":42`), wantPrivateJWK},
		{"d an object", okp(`,"d":{}`), wantPrivateJWK},
		{"d not base64url", okp(`,"d":"not*base64*"`), wantPrivateJWK},
		{"d that is not the key of x", okp(`,"d":"` + b64u([]byte("secret")) + `"`), wantPrivateJWK},
		// The member name as decoded: the JSON escape of d names d.
		{"d written as an escape", okp(`,"` + "\x5cu0064" + `":"AA"`), wantPrivateJWK},
		// ADR-1: the private check comes before the coordinates and before the unsupported type.
		{"d with a 42-character x", []byte(`{"kty":"OKP","crv":"Ed25519","x":"` + x[:42] + `","d":"AA"}`), wantPrivateJWK},
		{"d without x", []byte(`{"kty":"OKP","crv":"Ed25519","d":"AA"}`), wantPrivateJWK},
		{"EC d with an off-curve point", []byte(`{"kty":"EC","crv":"P-256","x":"` + b64u(make([]byte, 32)) + `","y":"` + b64u(make([]byte, 32)) + `","d":"AA"}`), wantPrivateJWK},
		// ADR-3: any kty.
		{"RSA", []byte(rsaPrivate), wantPrivateJWK},
		{"Ed448", []byte(`{"kty":"OKP","crv":"Ed448","x":"AA","d":"AA"}`), wantPrivateJWK},
		{"oct", []byte(`{"kty":"oct","k":"c2VjcmV0","d":"AA"}`), wantPrivateJWK},
		{"unknown kty", []byte(`{"kty":"XYZ","d":"AA"}`), wantPrivateJWK},
		// Without kty the file is no JWK, and a JWKS entry stays "JWK has no kty".
		{"d without kty", []byte(`{"crv":"Ed25519","x":"` + x + `","d":"AA"}`), "not a PEM public key, a JWK or a JWKS"},
		{"JWKS entry with d without kty", []byte(`{"keys":[{"crv":"Ed25519","x":"` + x + `","d":"AA"}]}`), "keys[0]: JWK has no kty"},
		// JWKS: the first entry carrying d, in list order, refuses the file.
		{"JWKS RSA private second", []byte(`{"keys":[` + string(okp(`,"kid":"a"`)) + `,` + rsaPrivate + `]}`), "keys[1]: " + wantPrivateJWK},
		{"JWKS private first", []byte(`{"keys":[` + string(okp(`,"d":"AA"`)) + `,` + string(okp(`,"kid":"b"`)) + `]}`), "keys[0]: " + wantPrivateJWK},
		{"JWKS two private", []byte(`{"keys":[` + string(okp(`,"kid":"a"`)) + `,` + string(okp(`,"d":null`)) + `,` + rsaPrivate + `]}`), "keys[1]: " + wantPrivateJWK},
		{"JWKS private with a bad x", []byte(`{"keys":[{"kty":"OKP","crv":"Ed25519","x":"AA","d":"AA"}]}`), "keys[0]: " + wantPrivateJWK},
		// ADR-3 near misses: not the member d, so not private; each is judged as before.
		{"RSA private members without d", []byte(`{"kty":"RSA","n":"AQAB","e":"AQAB","p":"AQAB","q":"AQAB","dp":"AQAB","dq":"AQAB","qi":"AQAB"}`),
			"JWK kty RSA" + wantJWKTail},
	} {
		assertKeyFileRefused(t, c.name, writeKeyFile(t, dir, strings.ReplaceAll(c.name, " ", "-")+".json", c.content), c.want)
	}

	// ADR-3 near misses that are public keys: D, a padded name, dp, a nested d, d inside another
	// member's value. Each loads as the public key it is.
	for name, content := range map[string][]byte{
		"capital D":                      okp(`,"D":"AA"`),
		"space before d":                 okp(`," d":"AA"`),
		"d after a space":                okp(`,"d ":"AA"`),
		"dp":                             okp(`,"dp":"AA"`),
		"nested d":                       okp(`,"ext":{"d":"AA"}`),
		"d as a value":                   okp(`,"use":"d"`),
		"JWKS, d beside keys and nested": []byte(`{"keys":[` + string(okp(`,"ext":{"d":"AA"}`)) + `],"d":"AA"}`),
		"JWKS entry with dq":             []byte(`{"keys":[` + string(okp(`,"dq":"AA"`)) + `]}`),
		"JWKS entry with D null":         []byte(`{"keys":[` + string(okp(`,"D":null`)) + `]}`),
	} {
		path := writeKeyFile(t, dir, "public-"+strings.ReplaceAll(name, " ", "-")+".json", content)
		set, err := LoadKeySet(path)
		if err != nil || len(set.entries) != 1 || set.entries[0].pub == nil || set.entries[0].private {
			t.Errorf("%s: %v, want one public Ed25519 key", name, err)
		}
	}

	// Regression: the private PEM refusals and the accepted files are unchanged.
	for _, name := range []string{"pub.pem", "pub.jwk.json", "keys.jwks.json", "both-current.jwks.json"} {
		if _, err := LoadKeySet(jsfKey(name)); err != nil {
			t.Errorf("%s: %v, want accepted", name, err)
		}
	}
}

// T-2: an embedded publicKey carrying d is KEY_LEAKED and fails the check under a key; without one
// the check passes with the leak in its note and in input.signature.detail.
func TestEmbeddedPrivateKeyIsLeaked(t *testing.T) {
	leaked := readJSF(t, "embedded-d.cdx.json")
	for _, key := range []string{"pub.pem", "keys.jwks.json", "wrong-key.jwks.json"} {
		signatureCase{"embedded-d / " + key, leaked, jsfKey(key), VerificationKeyLeaked, wantLeakDetail, StatusFail, wantLeakWithKey}.run(t)
	}

	// verify-signature without a key: never "verified with the embedded publicKey".
	rep, err := VerifySignature(leaked, "")
	want := SignatureReport{Present: true, WellFormed: true, Algorithm: "Ed25519", KeyID: "00000000-0000-4000-8000-000000000001",
		Verification: VerificationKeyLeaked, Detail: wantLeakDetail}
	if err != nil || rep != want {
		t.Errorf("VerifySignature(embedded-d, no key): %+v %v, want %+v", rep, err, want)
	}

	// Score without a key: a presence check that passes with the warning, detail set at load.
	r, check := scoreSignature(t, leaked, "")
	if s := r.Input.Signature; !s.Present || !s.WellFormed || s.Verification != VerificationNotRequested || s.Detail != wantLeakDetail {
		t.Errorf("embedded-d without a key: input.signature %+v, want present, well-formed, NOT_REQUESTED, detail %q", s, wantLeakDetail)
	}
	if check.Status != StatusPass || check.Note != wantLeakNoKey || check.Remedy != wantRemedy {
		t.Errorf("embedded-d without a key: %s note %q, want PASS %q", check.Status, check.Note, wantLeakNoKey)
	}
	if text := r.Text(); strings.Contains(text, "signature:") {
		t.Errorf("embedded-d without a key: text report carries a signature line:\n%s", text)
	}

	// A clean signature without a key keeps an empty detail and the plain note.
	r, check = scoreSignature(t, readJSF(t, "signed-ed25519.cdx.json"), "")
	if r.Input.Signature.Detail != "" || check.Status != StatusPass || check.Note != wantNoKeyNote {
		t.Errorf("signed-ed25519 without a key: detail %q, %s %q, want empty, PASS %q", r.Input.Signature.Detail, check.Status, check.Note, wantNoKeyNote)
	}

	// The text report with a key leads with the leak.
	r, _ = scoreSignature(t, leaked, jsfKey("pub.pem"))
	if text := r.Text(); !strings.HasPrefix(text, "signature: KEY_LEAKED, "+wantLeakDetail+"\n") {
		t.Errorf("embedded-d with pub.pem: text starts %.120q", text)
	}
}

// withPublicKeyMember adds a member to the publicKey of every signer of a jsf fixture.
func withPublicKeyMember(t *testing.T, name, member string, value any, onlySigner int) []byte {
	t.Helper()
	m := jsfMap(t, name)
	sig := signatureOf(m)
	var signers []any
	for _, list := range []string{"signers", "chain"} {
		if l, ok := sig[list].([]any); ok {
			signers = l
		}
	}
	if signers == nil {
		signers = []any{sig}
	}
	for i, s := range signers {
		if onlySigner < 0 || i == onlySigner {
			s.(map[string]any)["publicKey"].(map[string]any)[member] = value
		}
	}
	return encode(t, m)
}

// T-3: the leak precedes the form, the algorithm and the key selection; shape comes first.
func TestLeakPrecedesOtherOutcomes(t *testing.T) {
	pub, jwks := jsfKey("pub.pem"), jsfKey("keys.jwks.json")
	for _, c := range []struct {
		name string
		data []byte
		key  string
	}{
		{"signers-two, d on the second entry (not UNSUPPORTED_FORM)", withPublicKeyMember(t, "signers-two.cdx.json", "d", "AA", 1), pub},
		{"chain, d on its entry (not UNSUPPORTED_FORM)", withPublicKeyMember(t, "chain.cdx.json", "d", "AA", -1), pub},
		{"revoked-signed with d (not KEY_REVOKED)", withPublicKeyMember(t, "revoked-signed.cdx.json", "d", "AA", -1), jwks},
		{"rs256 with d (not UNSUPPORTED_ALGORITHM)", withPublicKeyMember(t, "rs256.cdx.json", "d", "AQAB", -1), pub},
		{"embedded-wrong-key with d (not KEY_MISMATCH)", withPublicKeyMember(t, "embedded-wrong-key.cdx.json", "d", "AA", -1), pub},
		{"signed-ed25519 with d null", withPublicKeyMember(t, "signed-ed25519.cdx.json", "d", nil, -1), pub},
		{"signed-ed25519 with d empty", withPublicKeyMember(t, "signed-ed25519.cdx.json", "d", "", -1), pub},
	} {
		signatureCase{c.name, c.data, c.key, VerificationKeyLeaked, wantLeakDetail, StatusFail, wantLeakWithKey}.run(t)
		if rep, err := VerifySignature(c.data, ""); err != nil || rep.Verification != VerificationKeyLeaked || rep.Detail != wantLeakDetail {
			t.Errorf("%s without a key: %+v %v, want KEY_LEAKED", c.name, rep, err)
		}
	}

	// Shape first: a publicKey carrying d without x is malformed, and FAILED under a key.
	noX := jsfMap(t, "embedded-d.cdx.json")
	delete(signatureOf(noX)["publicKey"].(map[string]any), "x")
	data := encode(t, noX)
	r, check := scoreSignature(t, data, "")
	if s := r.Input.Signature; s.WellFormed || s.Detail != "publicKey malformed (x missing)" || check.Status != StatusFail || check.Note != wantMalformedNote {
		t.Errorf("d without x: %+v %s %q, want malformed (x missing)", s, check.Status, check.Note)
	}
	signatureCase{"d without x / pub.pem", data, pub, VerificationFailed, "publicKey malformed (x missing)", StatusFail, wantMalformedNote}.run(t)

	// A publicKey with d is well-formed: d is not a malformed reason.
	r, _ = scoreSignature(t, readJSF(t, "embedded-d.cdx.json"), "")
	if !r.Input.Signature.WellFormed {
		t.Errorf("embedded-d: %+v, want well-formed", r.Input.Signature)
	}

	// ADR-3 near misses on the embedded key: not leaked. The added member changes the signed
	// input, so under a key the value no longer verifies; without a key the check is the plain
	// presence pass with an empty detail.
	for _, c := range []struct {
		name, file, member string
		value              any
		verification       Verification
		detail             string
	}{
		{"capital D", "signed-ed25519.cdx.json", "D", "AA", VerificationFailed, "signature does not verify"},
		{"space before d", "signed-ed25519.cdx.json", " d", "AA", VerificationFailed, "signature does not verify"},
		{"nested d", "signed-ed25519.cdx.json", "ext", map[string]any{"d": "AA"}, VerificationFailed, "signature does not verify"},
		{"RSA dp", "rs256.cdx.json", "dp", "AQAB", VerificationUnsupportedAlgorithm, "RS256"},
	} {
		data := withPublicKeyMember(t, c.file, c.member, c.value, -1)
		r, check := scoreSignature(t, data, "")
		if r.Input.Signature.Detail != "" || check.Status != StatusPass || check.Note != wantNoKeyNote {
			t.Errorf("%s without a key: detail %q, %s %q, want empty, PASS %q", c.name, r.Input.Signature.Detail, check.Status, check.Note, wantNoKeyNote)
		}
		if r, _ := scoreSignature(t, data, pub); r.Input.Signature.Verification != c.verification || r.Input.Signature.Detail != c.detail {
			t.Errorf("%s / pub.pem: %s %q, want %s %q", c.name, r.Input.Signature.Verification, r.Input.Signature.Detail, c.verification, c.detail)
		}
	}
}
