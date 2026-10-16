package bomscore

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// T-26 (design SCORE-34 ADR-3): key files, their refusals and the selection by kid.

const (
	wantPEMTail = " is not a supported public key (PEM SubjectPublicKeyInfo of an Ed25519 or EC P-256/P-384/P-521 key)"
	wantJWKTail = " is not a supported public key (JWK OKP Ed25519 or EC P-256/P-384/P-521)"
)

func writeKeyFile(t *testing.T, dir, name string, content []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, content, 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestKeyFiles(t *testing.T) {
	for _, c := range []struct {
		file    string
		entries int
		curve   string
	}{
		{"pub.pem", 1, "Ed25519"},
		{"pub-es256.pem", 1, "P-256"},
		{"pub.jwk.json", 1, "Ed25519"},
		{"keys.jwks.json", 2, "Ed25519"},
		{"wrong-key.jwks.json", 1, "Ed25519"},
	} {
		set, err := LoadKeySet(jsfKey(c.file))
		if err != nil {
			t.Errorf("%s: %v", c.file, err)
			continue
		}
		if len(set.entries) != c.entries || keyCurve(set.entries[0].pub) != c.curve {
			t.Errorf("%s: %d keys, first on %s; want %d on %s", c.file, len(set.entries), keyCurve(set.entries[0].pub), c.entries, c.curve)
		}
	}
	set, err := LoadKeySet(jsfKey("keys.jwks.json"))
	if err != nil {
		t.Fatal(err)
	}
	cur, rev := set.entries[0], set.entries[1]
	if cur.kid != "00000000-0000-4000-8000-000000000001" || cur.status != "CURRENT" || cur.notAfter != "" ||
		rev.kid != "00000000-0000-4000-8000-000000000002" || rev.status != "REVOKED" || rev.revokedAt != "2026-10-13T00:00:00Z" {
		t.Errorf("JWKS entries %+v %+v, want CURRENT then REVOKED with revokedAt", cur, rev)
	}
	if pemKey, _ := LoadKeySet(jsfKey("pub.pem")); !sameKey(pemKey.entries[0].pub, cur.pub) {
		t.Error("pub.pem and the JWKS CURRENT entry are not the same key")
	}

	dir := t.TempDir()
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	p224, err := ecdsa.GenerateKey(elliptic.P224(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ecDER, err := x509.MarshalECPrivateKey(ecKey)
	if err != nil {
		t.Fatal(err)
	}
	edPub, edPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pkcs8, err := x509.MarshalPKCS8PrivateKey(edPriv)
	if err != nil {
		t.Fatal(err)
	}
	certDER, err := x509.CreateCertificate(rand.Reader, &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "t"},
		NotBefore: time.Unix(0, 0), NotAfter: time.Unix(1, 0)}, &x509.Certificate{SerialNumber: big.NewInt(1)}, edPub, edPriv)
	if err != nil {
		t.Fatal(err)
	}
	pemBlock := func(typ string, der []byte) []byte { return pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der}) }
	x := currentJWKX
	missing := filepath.Join(dir, "nonexistent", "key.pem")
	for _, c := range []struct {
		name    string
		content []byte // nil: the path does not exist
		want    string // the message after "signature key <path>: "
	}{
		{"missing", nil, "open " + missing + ": no such file or directory"},
		{"rsa.pem", pemOf(t, &rsaKey.PublicKey), "RSA public key" + wantPEMTail},
		{"p224.pem", pemOf(t, &p224.PublicKey), "EC P-224 public key" + wantPEMTail},
		{"ec-private.pem", pemBlock("EC PRIVATE KEY", ecDER), "PEM block EC PRIVATE KEY" + wantPEMTail},
		{"pkcs8-private.pem", pemBlock("PRIVATE KEY", pkcs8), "PEM block PRIVATE KEY" + wantPEMTail},
		{"certificate.pem", pemBlock("CERTIFICATE", certDER), "PEM block CERTIFICATE" + wantPEMTail},
		{"broken.pem", []byte("-----BEGIN PUBLIC KEY-----\nnot base64\n"), "not a PEM public key, a JWK or a JWKS"},
		{"garbled.pem", pemBlock("PUBLIC KEY", []byte{1, 2, 3}), "PEM PUBLIC KEY that does not parse ("},
		{"rsa.jwk.json", []byte(`{"kty":"RSA","n":"AQAB","e":"AQAB"}`), "JWK kty RSA" + wantJWKTail},
		{"ed448.jwk.json", []byte(`{"kty":"OKP","crv":"Ed448","x":"AA"}`), "JWK kty OKP crv Ed448" + wantJWKTail},
		{"secp256k1.jwk.json", []byte(`{"kty":"EC","crv":"secp256k1","x":"AA","y":"AA"}`), "JWK kty EC crv secp256k1" + wantJWKTail},
		{"short-x.jwk.json", []byte(`{"kty":"OKP","crv":"Ed25519","x":"` + x[:42] + `"}`), "JWK OKP x is not 32 bytes of base64url"},
		{"padded-x.jwk.json", []byte(`{"kty":"OKP","crv":"Ed25519","x":"` + x + `="}`), "JWK OKP x is not 32 bytes of base64url"},
		{"off-curve.jwk.json", []byte(`{"kty":"EC","crv":"P-256","x":"` + b64u(make([]byte, 32)) + `","y":"` + b64u(make([]byte, 32)) + `"}`), "JWK EC P-256 point is not on the curve"},
		{"empty.jwks.json", []byte(`{"keys":[]}`), "the JWKS holds no key"},
		{"bad-entry.jwks.json", []byte(`{"keys":[{"kty":"OKP","crv":"Ed25519","x":"` + x + `"},{"kty":"EC","crv":"P-256","x":"AA","y":"AA"}]}`), "keys[1]: JWK EC x is not 32 bytes of base64url"},
		{"keys-not-array.jwks.json", []byte(`{"keys":{}}`), "not a PEM public key, a JWK or a JWKS"},
		{"empty-object.json", []byte(`{}`), "not a PEM public key, a JWK or a JWKS"},
		{"text.txt", []byte("an Ed25519 key, honestly\n"), "not a PEM public key, a JWK or a JWKS"},
		{"empty.txt", []byte{}, "not a PEM public key, a JWK or a JWKS"},
	} {
		path := missing
		if c.content != nil {
			path = writeKeyFile(t, dir, c.name, c.content)
		}
		_, err := LoadKeySet(path)
		prefix := "signature key " + path + ": "
		if err == nil || !strings.HasPrefix(err.Error(), prefix+c.want) {
			t.Errorf("%s: %v, want %q", c.name, err, prefix+c.want)
			continue
		}
		// A refusal names types and the path, never key material.
		if strings.Contains(err.Error(), x) || strings.Contains(err.Error(), "AQAB") {
			t.Errorf("%s: the message carries key material: %v", c.name, err)
		}
	}

	// An unsupported JWKS entry is kept so selection by kid names it; it cannot verify Ed25519.
	mixed := writeKeyFile(t, dir, "mixed.jwks.json", []byte(`{"keys":[{"kty":"OKP","crv":"Ed448","x":"AA","kid":"00000000-0000-4000-8000-000000000001"},`+
		`{"kty":"OKP","crv":"Ed25519","x":"`+x+`","kid":"other"}]}`))
	signatureCase{"keyid-only / Ed448 entry by kid", readJSF(t, "keyid-only.cdx.json"), mixed, VerificationFailed,
		"key curve Ed448 does not match Ed25519", StatusFail, wantNotVerifyNote}.run(t)
	signatureCase{"signed-ed25519 / Ed448 entry by kid", readJSF(t, "signed-ed25519.cdx.json"), mixed, VerificationKeyMismatch,
		"embedded publicKey differs from the given key", StatusFail, wantNotVerifyNote}.run(t)

	// Selection: one key without a kid is that key whatever the keyId; a JWK with a kid is matched.
	single := writeKeyFile(t, dir, "nokid.jwk.json", []byte(`{"kty":"OKP","crv":"Ed25519","x":"`+x+`"}`))
	for _, c := range []struct {
		key, keyID, detail string
		ok                 bool
	}{
		{jsfKey("pub.pem"), "anything", "", true},
		{single, "anything", "", true},
		{jsfKey("pub.jwk.json"), "00000000-0000-4000-8000-000000000001", "", true},
		{jsfKey("pub.jwk.json"), "other", "no key with kid other in testdata/jsf/pub.jwk.json", false},
		{jsfKey("pub.jwk.json"), "", "", true},
		{jsfKey("keys.jwks.json"), "00000000-0000-4000-8000-000000000002", "", true},
		{jsfKey("keys.jwks.json"), "", "testdata/jsf/keys.jwks.json holds 2 keys and the signature names no keyId", false},
	} {
		set, err := LoadKeySet(c.key)
		if err != nil {
			t.Fatal(err)
		}
		e, detail, ok := set.Select(c.keyID)
		if ok != c.ok || detail != c.detail || (ok && e.pub == nil) {
			t.Errorf("Select(%q) on %s: %v %q, want %v %q", c.keyID, c.key, ok, detail, c.ok, c.detail)
		}
		if ok && c.keyID == "00000000-0000-4000-8000-000000000002" && e.status != "REVOKED" {
			t.Errorf("Select by kid picked %+v, want the REVOKED entry", e)
		}
	}
}

// A RETIRED key verifies and the detail says so.
func TestRetiredKeyVerifies(t *testing.T) {
	retired := writeKeyFile(t, t.TempDir(), "retired.jwks.json", []byte(`{"keys":[{"kty":"OKP","crv":"Ed25519","x":"`+currentJWKX+
		`","kid":"00000000-0000-4000-8000-000000000001","rearm:status":"RETIRED","rearm:notAfter":"2026-10-12T00:00:00Z"}]}`))
	signatureCase{"signed-ed25519 / retired", readJSF(t, "signed-ed25519.cdx.json"), retired, VerificationVerified,
		"verified with key 00000000-0000-4000-8000-000000000001; key 00000000-0000-4000-8000-000000000001 is RETIRED since 2026-10-12T00:00:00Z",
		StatusPass, "verified with key 00000000-0000-4000-8000-000000000001"}.run(t)
}
