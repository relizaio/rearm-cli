package bomscore

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/asn1"
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Design SCORE-34: the signature check (T-17), verification outcomes (T-19) and unsupported JSF
// shapes (T-24), plus negative and malformed-input cases. The expected strings are the design's
// literals, not the production constants.

const (
	signatureCheckID  = "cisa-2026.sbom-author-signature"
	wantRemedy        = "configure an organization SBOM signing key in ReARM (Organization settings, SBOM export signing); exports are then signed"
	wantNoKeyNote     = "present, not verified (no key given)"
	wantMalformedNote = "signature present but malformed"
	wantNotVerifyNote = "signature does not verify with the given key"
)

// jsfKey is the path of a key file under testdata/jsf, as a user would give it.
func jsfKey(name string) string { return filepath.Join(jsfDir, name) }

// jsfMap decodes a jsf fixture for mutation.
func jsfMap(t *testing.T, name string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(readJSF(t, name), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func signatureOf(m map[string]any) map[string]any { return m["signature"].(map[string]any) }

// scoreSignature scores data under cisa-2026 with an optional key file; it returns the report and
// the signature check.
func scoreSignature(t *testing.T, data []byte, key string) (Report, CheckResult) {
	t.Helper()
	r, err := Score(data, []string{string(ProfileCISA2026)}, testEngineVersion, Options{SignatureKey: key})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	return r, checkOf(t, r, signatureCheckID)
}

type signatureCase struct {
	name         string
	data         []byte
	key          string
	verification Verification
	detail       string
	status       Status
	note         string
}

func (c signatureCase) run(t *testing.T) {
	t.Helper()
	r, check := scoreSignature(t, c.data, c.key)
	got := r.Input.Signature
	if got.Verification != c.verification || got.Detail != c.detail {
		t.Errorf("%s: verification %s detail %q, want %s %q", c.name, got.Verification, got.Detail, c.verification, c.detail)
	}
	if check.Status != c.status || check.Note != c.note {
		t.Errorf("%s: check %s note %q, want %s %q", c.name, check.Status, check.Note, c.status, c.note)
	}
}

// T-17
func TestSignatureCheckPresenceAndWellFormedness(t *testing.T) {
	for _, name := range []string{"signed-ed25519.cdx.json", "signed-ed25519.pretty.cdx.json", "signed-es256.cdx.json", "keyid-only.cdx.json"} {
		r, check := scoreSignature(t, readJSF(t, name), "")
		if check.Status != StatusPass || check.Note != wantNoKeyNote || check.Remedy != wantRemedy {
			t.Errorf("%s: %s note %q remedy %q, want PASS %q with the remedy", name, check.Status, check.Note, check.Remedy, wantNoKeyNote)
		}
		s := r.Input.Signature
		if !s.Present || !s.WellFormed || s.Verification != VerificationNotRequested || s.Detail != "" || s.KeyID == "" || s.Algorithm == "" {
			t.Errorf("%s: input.signature %+v, want present, well-formed, NOT_REQUESTED with algorithm and keyId", name, s)
		}
	}

	r, check := scoreSignature(t, readJSF(t, "unsigned.cdx.json"), "")
	if check.Status != StatusFail || check.Note != "" || check.Remedy != wantRemedy {
		t.Errorf("unsigned: %s note %q remedy %q, want FAIL, no note, the remedy", check.Status, check.Note, check.Remedy)
	}
	if s := r.Input.Signature; s.Present || s.WellFormed || s.Algorithm != "" || s.KeyID != "" {
		t.Errorf("unsigned: input.signature %+v, want absent", s)
	}

	malformed := func(name string, data []byte, detail string) {
		t.Helper()
		r, check := scoreSignature(t, data, "")
		if check.Status != StatusFail || check.Note != wantMalformedNote || check.Remedy != wantRemedy {
			t.Errorf("%s: %s note %q, want FAIL %q", name, check.Status, check.Note, wantMalformedNote)
		}
		if s := r.Input.Signature; !s.Present || s.WellFormed || s.Detail != detail || s.Verification != VerificationNotRequested {
			t.Errorf("%s: input.signature %+v, want present, malformed, detail %q", name, s, detail)
		}
	}
	malformed("malformed", readJSF(t, "malformed.cdx.json"), "value missing")
	malformed("malformed-base64", readJSF(t, "malformed-base64.cdx.json"), "value is not base64url")

	mutate := func(f func(sig map[string]any)) []byte {
		m := jsfMap(t, "signed-ed25519.cdx.json")
		f(signatureOf(m))
		return encode(t, m)
	}
	malformed("algorithm deleted", mutate(func(s map[string]any) { delete(s, "algorithm") }), "algorithm missing")
	malformed("signers empty", mutate(func(s map[string]any) {
		for k := range s {
			delete(s, k)
		}
		s["signers"] = []any{}
	}), "signers empty")

	// The remaining reasons, and their order (the first that applies).
	for _, c := range []struct {
		name   string
		mutate func(s map[string]any)
		detail string
	}{
		{"value empty", func(s map[string]any) { s["value"] = "" }, "value empty"},
		{"value null", func(s map[string]any) { s["value"] = nil }, "value missing"},
		{"value not a string", func(s map[string]any) { s["value"] = 64 }, "value missing"},
		{"value padded", func(s map[string]any) { s["value"] = s["value"].(string) + "==" }, "value is not base64url"},
		{"value standard base64", func(s map[string]any) { s["value"] = "ab+/" }, "value is not base64url"},
		{"value with a line break", func(s map[string]any) { v := s["value"].(string); s["value"] = v[:40] + "\n" + v[40:] }, "value is not base64url"},
		{"value with non-zero trailing bits", func(s map[string]any) { v := s["value"].(string); s["value"] = v[:len(v)-1] + "B" }, "value is not base64url"},
		{"value and algorithm missing", func(s map[string]any) { delete(s, "value"); delete(s, "algorithm") }, "value missing"},
		{"algorithm empty", func(s map[string]any) { s["algorithm"] = "" }, "algorithm empty"},
		{"algorithm not a string", func(s map[string]any) { s["algorithm"] = 1 }, "algorithm missing"},
		{"publicKey not an object", func(s map[string]any) { s["publicKey"] = "key" }, "publicKey malformed (not an object)"},
		{"publicKey without kty", func(s map[string]any) { delete(s["publicKey"].(map[string]any), "kty") }, "publicKey malformed (kty missing)"},
		{"publicKey kty oct", func(s map[string]any) { s["publicKey"].(map[string]any)["kty"] = "oct" }, `publicKey malformed (kty "oct" is not OKP, EC or RSA)`},
		{"publicKey OKP without x", func(s map[string]any) { delete(s["publicKey"].(map[string]any), "x") }, "publicKey malformed (x missing)"},
		{"publicKey OKP without crv", func(s map[string]any) { delete(s["publicKey"].(map[string]any), "crv") }, "publicKey malformed (crv missing)"},
		{"publicKey EC without y", func(s map[string]any) { s["publicKey"] = map[string]any{"kty": "EC", "crv": "P-256", "x": "AA"} }, "publicKey malformed (y missing)"},
		{"publicKey RSA n not base64url", func(s map[string]any) { s["publicKey"] = map[string]any{"kty": "RSA", "n": "a=b", "e": "AQAB"} }, "publicKey malformed (n is not base64url)"},
		{"chain empty", func(s map[string]any) {
			for k := range s {
				delete(s, k)
			}
			s["chain"] = []any{}
		}, "chain empty"},
		{"second signer malformed", func(s map[string]any) {
			first := map[string]any{}
			for k, v := range s {
				first[k] = v
				delete(s, k)
			}
			s["signers"] = []any{first, map[string]any{"algorithm": "Ed25519"}}
		}, "value missing"},
	} {
		malformed(c.name, mutate(c.mutate), c.detail)
	}

	// The engine's own fixtures: full.cdx.json carries an ES256 value of 33 bytes and passes.
	if _, check := scoreSignature(t, readFixture(t, "full.cdx.json"), ""); check.Status != StatusPass || check.Note != wantNoKeyNote {
		t.Errorf("full.cdx.json: %s %q, want PASS %q", check.Status, check.Note, wantNoKeyNote)
	}
	full := jsonFixture(t, "full.cdx.json")
	sig := full["signature"]
	delete(full, "signature")
	full["components"].([]any)[0].(map[string]any)["signature"] = sig
	if r, check := scoreSignature(t, encode(t, full), ""); check.Status != StatusFail || r.Input.Signature.Present {
		t.Errorf("signature on a component only: %s present %v, want FAIL and absent", check.Status, r.Input.Signature.Present)
	}
	if r, check := scoreSignature(t, readFixture(t, "full.cdx.xml"), ""); check.Status != StatusPass || check.Note != wantNoKeyNote ||
		!r.Input.Signature.Present || !r.Input.Signature.WellFormed || r.Input.Signature.Algorithm != "" {
		t.Errorf("full.cdx.xml: %s %q %+v, want PASS, present and well-formed, no algorithm", check.Status, check.Note, r.Input.Signature)
	}
	if r, check := scoreSignature(t, readFixture(t, "full.spdx.json"), ""); check.Status != StatusFail || check.Level != LevelRequired ||
		check.Note != "not representable in SPDX 2.3" || r.Input.Signature.Present {
		t.Errorf("full.spdx.json: %s %s %q, want REQUIRED FAIL not representable", check.Level, check.Status, check.Note)
	}
}

// derOf re-encodes a raw r || s value as ASN.1 DER.
func derOf(t *testing.T, raw []byte) []byte {
	t.Helper()
	half := len(raw) / 2
	der, err := asn1.Marshal(struct{ R, S *big.Int }{new(big.Int).SetBytes(raw[:half]), new(big.Int).SetBytes(raw[half:])})
	if err != nil {
		t.Fatal(err)
	}
	return der
}

// T-19
func TestVerificationOutcomes(t *testing.T) {
	pub, jwk, jwks, wrong, es := jsfKey("pub.pem"), jsfKey("pub.jwk.json"), jsfKey("keys.jwks.json"), jsfKey("wrong-key.jwks.json"), jsfKey("pub-es256.pem")
	const verifiedNote = "verified with key 00000000-0000-4000-8000-000000000001"

	noKeyID := jsfMap(t, "signed-ed25519.cdx.json")
	delete(signatureOf(noKeyID), "keyId")

	esDER := jsfMap(t, "signed-es256.cdx.json")
	raw, err := decodeBase64URL(signatureOf(esDER)["value"].(string))
	if err != nil || len(raw) != 64 {
		t.Fatalf("signed-es256 value: %v %d", err, len(raw))
	}
	der := derOf(t, raw)
	signatureOf(esDER)["value"] = b64u(der)

	keyIDOnlyES := jsfMap(t, "keyid-only.cdx.json")
	signatureOf(keyIDOnlyES)["algorithm"] = "ES256"

	for _, c := range []signatureCase{
		{"signed-ed25519 / pub.pem", readJSF(t, "signed-ed25519.cdx.json"), pub, VerificationVerified, verifiedNote, StatusPass, verifiedNote},
		{"signed-ed25519.pretty / pub.pem", readJSF(t, "signed-ed25519.pretty.cdx.json"), pub, VerificationVerified, verifiedNote, StatusPass, verifiedNote},
		{"tampered / pub.pem", readJSF(t, "tampered.cdx.json"), pub, VerificationFailed, "signature does not verify", StatusFail, wantNotVerifyNote},
		{"embedded-wrong-key / pub.pem", readJSF(t, "embedded-wrong-key.cdx.json"), pub, VerificationKeyMismatch, "embedded publicKey differs from the given key", StatusFail, wantNotVerifyNote},
		{"unsigned / pub.pem", readJSF(t, "unsigned.cdx.json"), pub, VerificationFailed, "no signature", StatusFail, ""},
		{"malformed / pub.pem", readJSF(t, "malformed.cdx.json"), pub, VerificationFailed, "value missing", StatusFail, wantMalformedNote},
		{"signed-ed25519 / wrong-key.jwks", readJSF(t, "signed-ed25519.cdx.json"), wrong, VerificationKeyMismatch, "embedded publicKey differs from the given key", StatusFail, wantNotVerifyNote},
		{"signed-ed25519 / keys.jwks", readJSF(t, "signed-ed25519.cdx.json"), jwks, VerificationVerified, verifiedNote, StatusPass, verifiedNote},
		{"revoked-signed / keys.jwks", readJSF(t, "revoked-signed.cdx.json"), jwks, VerificationKeyRevoked,
			"key 00000000-0000-4000-8000-000000000002 is REVOKED in testdata/jsf/keys.jwks.json since 2026-10-13T00:00:00Z", StatusFail, wantNotVerifyNote},
		{"keyid-only / keys.jwks", readJSF(t, "keyid-only.cdx.json"), jwks, VerificationVerified,
			"verified with the given key (signature embeds no publicKey)", StatusPass, verifiedNote},
		{"no keyId / keys.jwks", encode(t, noKeyID), jwks, VerificationKeyMismatch,
			"testdata/jsf/keys.jwks.json holds 2 keys and the signature names no keyId", StatusFail, wantNotVerifyNote},
		{"signed-ed25519 / pub.jwk", readJSF(t, "signed-ed25519.cdx.json"), jwk, VerificationVerified, verifiedNote, StatusPass, verifiedNote},
		{"signed-es256 / pub-es256.pem", readJSF(t, "signed-es256.cdx.json"), es, VerificationVerified,
			"verified with key 00000000-0000-4000-8000-000000000003", StatusPass, "verified with key 00000000-0000-4000-8000-000000000003"},
		{"signed-es256 DER value / pub-es256.pem", encode(t, esDER), es, VerificationFailed,
			"value is " + itoa(len(der)) + " bytes, ES256 needs 64", StatusFail, wantNotVerifyNote},
		{"signed-es256 / pub.pem", readJSF(t, "signed-es256.cdx.json"), pub, VerificationKeyMismatch, "embedded publicKey differs from the given key", StatusFail, wantNotVerifyNote},
		{"keyid-only as ES256 / pub.pem", encode(t, keyIDOnlyES), pub, VerificationFailed, "key curve Ed25519 does not match ES256", StatusFail, wantNotVerifyNote},
		{"rs256 / pub.pem", readJSF(t, "rs256.cdx.json"), pub, VerificationUnsupportedAlgorithm, "RS256", StatusPass, "present, not verified (UNSUPPORTED_ALGORITHM: RS256)"},
		{"full.cdx.xml / pub.pem", readFixture(t, "full.cdx.xml"), pub, VerificationUnsupportedForm, "XML signature (XML-DSig) is not verified", StatusPass,
			"present, not verified (UNSUPPORTED_FORM: XML signature (XML-DSig) is not verified)"},
		{"full.spdx.json / pub.pem", readFixture(t, "full.spdx.json"), pub, VerificationFailed, "no signature (not representable in SPDX 2.x)", StatusFail, "not representable in SPDX 2.3"},
	} {
		c.run(t)
	}
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

// T-24
func TestUnsupportedFormsAreWellFormedNotFailed(t *testing.T) {
	for _, c := range []struct{ name, detail string }{
		{"signers-two.cdx.json", "multiple signers (2)"},
		{"chain.cdx.json", "signature chain"},
		{"excludes.cdx.json", "excludes"},
		{"certificate-path.cdx.json", "certificatePath"},
	} {
		data := readJSF(t, c.name)
		r, check := scoreSignature(t, data, "")
		if check.Status != StatusPass || check.Note != wantNoKeyNote || !r.Input.Signature.WellFormed {
			t.Errorf("%s without a key: %s %q %+v, want PASS, well-formed", c.name, check.Status, check.Note, r.Input.Signature)
		}
		signatureCase{c.name + " / pub.pem", data, jsfKey("pub.pem"), VerificationUnsupportedForm, c.detail, StatusPass,
			"present, not verified (UNSUPPORTED_FORM: " + c.detail + ")"}.run(t)
		if rep, err := VerifySignature(data, ""); err != nil || rep.Verification != VerificationUnsupportedForm || !rep.WellFormed {
			t.Errorf("%s verify-signature without a key: %+v %v, want UNSUPPORTED_FORM", c.name, rep, err)
		}
	}
	// excludes on the outer object of signers (the JSF multiple-signature place) is the excludes form.
	outer := jsfMap(t, "signers-two.cdx.json")
	signatureOf(outer)["signers"] = signatureOf(outer)["signers"].([]any)[:1]
	signatureOf(outer)["excludes"] = []any{"metadata"}
	signatureCase{"signers with outer excludes / pub.pem", encode(t, outer), jsfKey("pub.pem"), VerificationUnsupportedForm, "excludes",
		StatusPass, "present, not verified (UNSUPPORTED_FORM: excludes)"}.run(t)
	one := jsfMap(t, "signers-two.cdx.json")
	sig := signatureOf(one)
	sig["signers"] = sig["signers"].([]any)[:1]
	signatureCase{"signers with one entry / pub.pem", encode(t, one), jsfKey("pub.pem"), VerificationVerified,
		"verified with key 00000000-0000-4000-8000-000000000001", StatusPass, "verified with key 00000000-0000-4000-8000-000000000001"}.run(t)
}

// Forgeries and mismatches never verify; each names why.
func TestVerificationRejectsForgeries(t *testing.T) {
	pub := jsfKey("pub.pem")
	signed := func(f func(m map[string]any)) []byte {
		m := jsfMap(t, "signed-ed25519.cdx.json")
		f(m)
		return encode(t, m)
	}
	flipped := func(s string) string {
		raw, _ := decodeBase64URL(s)
		raw[10] ^= 1
		return b64u(raw)
	}
	const noVerify = "signature does not verify"
	for _, c := range []signatureCase{
		{"value with one bit flipped", signed(func(m map[string]any) { signatureOf(m)["value"] = flipped(signatureOf(m)["value"].(string)) }),
			pub, VerificationFailed, noVerify, StatusFail, wantNotVerifyNote},
		{"value of the revoked key's document", signed(func(m map[string]any) {
			signatureOf(m)["value"] = signatureOf(jsfMap(t, "revoked-signed.cdx.json"))["value"]
		}), pub, VerificationFailed, noVerify, StatusFail, wantNotVerifyNote},
		{"value truncated", signed(func(m map[string]any) {
			raw, _ := decodeBase64URL(signatureOf(m)["value"].(string))
			signatureOf(m)["value"] = b64u(raw[:63])
		}), pub, VerificationFailed, "value is 63 bytes, Ed25519 needs 64", StatusFail, wantNotVerifyNote},
		{"keyId changed (signed member)", signed(func(m map[string]any) { signatureOf(m)["keyId"] = "00000000-0000-4000-8000-000000000009" }),
			pub, VerificationFailed, noVerify, StatusFail, wantNotVerifyNote},
		{"export property flipped", signed(func(m map[string]any) {
			m["metadata"].(map[string]any)["properties"].([]any)[0].(map[string]any)["value"] = "false"
		}), pub, VerificationFailed, noVerify, StatusFail, wantNotVerifyNote},
		{"root member added", signed(func(m map[string]any) { m["extra"] = true }), pub, VerificationFailed, noVerify, StatusFail, wantNotVerifyNote},
		{"excludes added after signing", signed(func(m map[string]any) { signatureOf(m)["excludes"] = []any{"components"} }),
			pub, VerificationUnsupportedForm, "excludes", StatusPass, "present, not verified (UNSUPPORTED_FORM: excludes)"},
		{"algorithm ES256 on an Ed25519 key", signed(func(m map[string]any) { signatureOf(m)["algorithm"] = "ES256" }),
			pub, VerificationFailed, "key curve Ed25519 does not match ES256", StatusFail, wantNotVerifyNote},
		{"algorithm none", signed(func(m map[string]any) { signatureOf(m)["algorithm"] = "none" }),
			pub, VerificationUnsupportedAlgorithm, "none", StatusPass, "present, not verified (UNSUPPORTED_ALGORITHM: none)"},
		{"algorithm EdDSA", signed(func(m map[string]any) { signatureOf(m)["algorithm"] = "EdDSA" }),
			pub, VerificationUnsupportedAlgorithm, "EdDSA", StatusPass, "present, not verified (UNSUPPORTED_ALGORITHM: EdDSA)"},
		{"algorithm in lower case", signed(func(m map[string]any) { signatureOf(m)["algorithm"] = "ed25519" }),
			pub, VerificationUnsupportedAlgorithm, "ed25519", StatusPass, "present, not verified (UNSUPPORTED_ALGORITHM: ed25519)"},
		{"keyid-only under a key with its kid but another x", readJSF(t, "keyid-only.cdx.json"), jsfKey("wrong-key.jwks.json"),
			VerificationFailed, noVerify, StatusFail, wantNotVerifyNote},
		{"kid not in the JWKS", signed(func(m map[string]any) { signatureOf(m)["keyId"] = "k-9"; delete(signatureOf(m), "publicKey") }),
			jsfKey("keys.jwks.json"), VerificationKeyMismatch, "no key with kid k-9 in testdata/jsf/keys.jwks.json", StatusFail, wantNotVerifyNote},
	} {
		c.run(t)
	}

	// A member given twice is not RFC 8785 input: the signed document and another reader's view
	// could differ, so it never verifies, at the root, in the signature or deeper.
	doc := string(readJSF(t, "signed-ed25519.cdx.json"))
	for name, data := range map[string]string{
		"root":      strings.Replace(doc, `{"bomFormat":"CycloneDX",`, `{"bomFormat":"CycloneDX","version":7,`, 1),
		"signature": strings.Replace(doc, `"signature":{`, `"signature":{"value":"AAAA",`, 1),
		"component": strings.Replace(doc, `"name":"beta",`, `"name":"evil","name":"beta",`, 1),
	} {
		if data == doc {
			t.Fatalf("%s: duplicate not inserted", name)
		}
		r, check := scoreSignature(t, []byte(data), pub)
		if s := r.Input.Signature; s.Verification != VerificationFailed || !strings.HasPrefix(s.Detail, "document is not canonicalizable (RFC 8785): ") ||
			!strings.Contains(strings.ToLower(s.Detail), "duplicate") || check.Status != StatusFail {
			t.Errorf("duplicate member in %s: %+v %s, want FAILED naming the duplicate", name, s, check.Status)
		}
	}

	// ECDSA out-of-range and zero values, and the hash of each curve.
	es := jsfMap(t, "signed-es256.cdx.json")
	for name, value := range map[string][]byte{
		"zero r and s": make([]byte, 64),
		"r = n":        append(elliptic.P256().Params().N.FillBytes(make([]byte, 32)), bytes.Repeat([]byte{1}, 32)...),
	} {
		signatureOf(es)["value"] = b64u(value)
		signatureCase{"ES256 " + name, encode(t, es), jsfKey("pub-es256.pem"), VerificationFailed, noVerify, StatusFail, wantNotVerifyNote}.run(t)
	}
}

// ES384 and ES512 verify with their own curve and hash, and refuse another curve's key.
func TestECDSACurvesAndHashes(t *testing.T) {
	dir := t.TempDir()
	for _, c := range []struct {
		alg   string
		curve elliptic.Curve
		size  int
		hash  func([]byte) []byte
	}{
		{"ES384", elliptic.P384(), 48, func(b []byte) []byte { h := sha512.Sum384(b); return h[:] }},
		{"ES512", elliptic.P521(), 66, func(b []byte) []byte { h := sha512.Sum512(b); return h[:] }},
	} {
		key, err := ecdsa.GenerateKey(c.curve, rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		keyFile := filepath.Join(dir, c.alg+".pem")
		if err := os.WriteFile(keyFile, pemOf(t, &key.PublicKey), 0644); err != nil {
			t.Fatal(err)
		}
		sign := func(hash func([]byte) []byte) []byte {
			doc := vectorDocument("k-" + c.alg)
			signer := &vecSigner{Algorithm: c.alg, KeyID: "k-" + c.alg}
			doc.Signature = signer
			r, s, err := ecdsa.Sign(rand.Reader, key, hash(canonicalOf(t, doc)))
			if err != nil {
				t.Fatal(err)
			}
			raw := make([]byte, 2*c.size)
			r.FillBytes(raw[:c.size])
			s.FillBytes(raw[c.size:])
			signer.Value = b64u(raw)
			return marshalCompact(t, doc)
		}
		signatureCase{c.alg, sign(c.hash), keyFile, VerificationVerified, "verified with the given key (signature embeds no publicKey)",
			StatusPass, "verified with key k-" + c.alg}.run(t)
		sha256Hash := func(b []byte) []byte { h := sha256.Sum256(b); return h[:] }
		signatureCase{c.alg + " signed over SHA-256", sign(sha256Hash), keyFile, VerificationFailed, "signature does not verify",
			StatusFail, wantNotVerifyNote}.run(t)
		signatureCase{c.alg + " under a P-256 key", sign(c.hash), jsfKey("pub-es256.pem"), VerificationFailed,
			"key curve P-256 does not match " + c.alg, StatusFail, wantNotVerifyNote}.run(t)
	}
}

// Malformed input of any kind never panics, never verifies and is never refused once it loads.
func TestMalformedSignaturesNeverPanicOrVerify(t *testing.T) {
	base := string(readJSF(t, "signed-ed25519.cdx.json"))
	i := strings.Index(base, `"signature":`)
	prefix := base[:i]
	for _, sig := range []string{
		`{}`, `null`, `{"value":null,"algorithm":null}`, `{"value":"","algorithm":""}`, `{"value":"AA","algorithm":"Ed25519"}`,
		`{"value":"AA","algorithm":"Ed25519","publicKey":{}}`, `{"value":"AA","algorithm":"Ed25519","publicKey":{"kty":"OKP","crv":"Ed25519","x":""}}`,
		`{"value":"AA","algorithm":"ES256","publicKey":{"kty":"EC","crv":"P-256","x":"AA","y":"AA"}}`,
		`{"value":"AA","algorithm":"ES512","publicKey":{"kty":"EC","crv":"P-999","x":"AA","y":"AA"}}`,
		`{"value":"AA","algorithm":"Ed25519","publicKey":{"kty":"RSA","n":"AA","e":"AQAB"}}`,
		`{"signers":[{"value":"AA","algorithm":"Ed25519"}]}`, `{"chain":[{"value":"AA","algorithm":"ES256"}]}`,
		`{"signers":[{}],"chain":[{}]}`, `{"value":"AA","algorithm":"Ed25519","excludes":null,"certificatePath":null}`,
		`{"value":"` + strings.Repeat("A", 86) + `","algorithm":"Ed25519","keyId":5}`,
		`{"value":"` + strings.Repeat("_", 86) + `","algorithm":"Ed25519","publicKey":{"kty":"OKP","crv":"Ed25519","x":"` + strings.Repeat("_", 43) + `"}}`,
	} {
		data := []byte(prefix + `"signature":` + sig + `}`)
		for _, key := range []string{"", jsfKey("pub.pem"), jsfKey("keys.jwks.json"), jsfKey("pub-es256.pem")} {
			r, err := Score(data, []string{string(ProfileCISA2026)}, testEngineVersion, Options{SignatureKey: key})
			if err != nil {
				t.Errorf("%s with %q: %v", sig, key, err)
				continue
			}
			if r.Input.Signature.Verification == VerificationVerified {
				t.Errorf("%s with %q verified", sig, key)
			}
			if rep, err := VerifySignature(data, key); err != nil || rep.Verification == VerificationVerified {
				t.Errorf("verify-signature %s with %q: %+v %v", sig, key, rep, err)
			}
		}
	}
}

// Fuzz: whatever the input, verification under the current key never panics, and VERIFIED only
// for a document whose signing input one of the committed signed documents has (Ed25519 is not
// forgeable, so anything else that verifies is a canonicalisation or selection bug).
func FuzzVerifySignature(f *testing.F) {
	entries, err := os.ReadDir(jsfDir)
	if err != nil {
		f.Fatal(err)
	}
	signedInputs := map[string]bool{}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".cdx.json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(jsfDir, e.Name()))
		if err != nil {
			f.Fatal(err)
		}
		f.Add(b)
		if rep, err := VerifySignature(b, jsfKey("pub.pem")); err == nil && rep.Verification == VerificationVerified {
			c, _ := signingInput(b)
			signedInputs[string(c)] = true
		}
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		for _, key := range []string{"", jsfKey("pub.pem")} {
			rep, err := VerifySignature(data, key)
			if err != nil || rep.Verification != VerificationVerified || key == "" {
				continue
			}
			c, err := signingInput(data)
			if err != nil || !signedInputs[string(c)] {
				t.Fatalf("verified a document no committed signature covers: %q", data)
			}
		}
	})
}

// Strings a document brings (algorithm, keyId, a JWK crv) reach details, notes and text lines
// escaped: a document cannot add a line to the output.
func TestDocumentStringsCannotBreakALine(t *testing.T) {
	m := jsfMap(t, "signed-ed25519.cdx.json")
	signatureOf(m)["algorithm"] = "RS256\nverified with key x\u202e"
	r, check := scoreSignature(t, encode(t, m), jsfKey("pub.pem"))
	if r.Input.Signature.Detail != "RS256\\u000averified with key x\\u202e" || strings.Contains(check.Note, "\n") || strings.Contains(r.Text(), "\nverified") {
		t.Errorf("detail %q note %q, want the control characters escaped", r.Input.Signature.Detail, check.Note)
	}
	if r.Input.Signature.Algorithm != "RS256\nverified with key x\u202e" {
		t.Errorf("input.signature.algorithm %q, want the document's value as is", r.Input.Signature.Algorithm)
	}
	m = jsfMap(t, "signed-ed25519.cdx.json")
	signatureOf(m)["keyId"] = "k\r\nsignature: VERIFIED"
	delete(signatureOf(m), "publicKey")
	r, _ = scoreSignature(t, encode(t, m), jsfKey("keys.jwks.json"))
	if r.Input.Signature.Detail != `no key with kid k\u000d\u000asignature: VERIFIED in testdata/jsf/keys.jwks.json` {
		t.Errorf("detail %q, want the keyId escaped", r.Input.Signature.Detail)
	}
	if got := PrintableText("caf\u00e9 \u4e2d"); got != "caf\u00e9 \u4e2d" {
		t.Errorf("printable non-ASCII text changed: %q", got)
	}
}

// rearm:status is matched without regard to case: a revoked key never verifies.
func TestRevokedStatusIgnoresCase(t *testing.T) {
	keys := filepath.Join(t.TempDir(), "keys.jwks.json")
	if err := os.WriteFile(keys, []byte(`{"keys":[{"kty":"OKP","crv":"Ed25519","x":"`+currentJWKX+`","kid":"00000000-0000-4000-8000-000000000001","rearm:status":"revoked"}]}`), 0644); err != nil {
		t.Fatal(err)
	}
	signatureCase{"lower-case revoked", readJSF(t, "signed-ed25519.cdx.json"), keys, VerificationKeyRevoked,
		"key 00000000-0000-4000-8000-000000000001 is REVOKED in " + keys, StatusFail, wantNotVerifyNote}.run(t)
}
