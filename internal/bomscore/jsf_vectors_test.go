package bomscore

// TestJSFVectors generates the shared JSF signature vectors under testdata/jsf (design SCORE-34
// ADR-7, T-25) and keeps them current.
//
// How to regenerate:
//
//	go test ./internal/bomscore -run TestJSFVectors -update
//	go test ./internal/bomscore -run TestJSFVectors -update -update-es256   # also re-sign signed-es256
//
// Without -update the test regenerates every file in memory and fails when a committed file
// differs. signed-es256.cdx.json is the exception: Go's ECDSA draws a random nonce, so the committed
// file keeps its value, the test splices that value into the regenerated document, compares the
// bytes and verifies the signature with the ES256 public key. jcs-vectors.json is hand-written
// from RFC 8785 and is not generated here (T-18 reads it).
//
// The keys are fixed and copied from the RFC texts: Ed25519 "current" is the RFC 8032 section 7.1
// TEST 1 secret key (the key rearm-saas's signer tests are pinned to), "revoked" the TEST 2 secret
// key, ES256 the RFC 6979 section A.2.5 P-256 private key. The documents are CycloneDX 1.6 in the
// merged-export shape, signed over signingInput (the document with the root signature in place and
// only its value removed, RFC 8785 JCS) and written compact, signature last.
//
// The private-key fixtures of design SCORE-37 (with-d.jwk.json, with-d-es256.jwk.json,
// with-d.jwks.json, embedded-d.cdx.json) embed those RFC secret keys as the JWK member d on purpose:
// they are published test vectors. No fixture may ever carry a key that is not from an RFC.

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"flag"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

var updateES256 = flag.Bool("update-es256", false, "with -update, re-sign testdata/jsf/signed-es256.cdx.json (random ECDSA nonce)")

const jsfDir = "testdata/jsf"

// The key material, from the RFC texts.
const (
	// RFC 8032 section 7.1, TEST 1: SECRET KEY, PUBLIC KEY, SIGNATURE over the empty message.
	rfc8032Test1Secret    = "9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60"
	rfc8032Test1Public    = "d75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a"
	rfc8032Test1Signature = "e5564300c360ac729086e2cc806e828a84877f1eb8e5d974d873e06522490155" +
		"5fb8821590a33bacc61e39701cf9b46bd25bf5f0595bbe24655141438e7a100b"
	// RFC 8032 section 7.1, TEST 2: SECRET KEY.
	rfc8032Test2Secret = "4ccd089b28ff96da9db6c346ec114e0f5b8a319f35aba624da8cf6ed4fb8a6fb"
	// RFC 6979 section A.2.5, P-256: private key x, public key Ux, Uy.
	rfc6979P256X  = "C9AFA9D845BA75166B5C215767B1D6934E50C3DB36E89B127B8A622B120F6721"
	rfc6979P256Ux = "60FED4BA255A9D31C961EB74C6356D68C049B8923B61FA6CE669622E60F29FB6"
	rfc6979P256Uy = "7903FE1008B8BC99A41AE9E95628BC64F2F1B20C2D7E9F5177A3C294D4462299"

	// The JWK x of the TEST 1 public key, design SCORE-34 ADR-7: proves the seed was copied right.
	currentJWKX = "11qYAYKxCrfVS_7TyWQHOg7hcvPapiMlrwIaaPcHURo"
	// The JWK d of the TEST 1 and TEST 2 secret keys (base64url of the seeds), design SCORE-37 3.6.
	currentJWKD = "nWGxne_9WmC6hEr0kuwsxERJxWl7MmkZcDusAxyuf2A"
	revokedJWKD = "TM0Imyj_ltqdtsNG7BFOD1uKMZ81q6Yk2oz27U-4pvs"
	// The JWK d of the RFC 6979 A.2.5 P-256 private key x.
	es256JWKD = "ya-p2EW6dRZrXCFXZ7HWk05Qw9s26JsSe4piKxIPZyE"

	kidCurrent = "00000000-0000-4000-8000-000000000001"
	kidRevoked = "00000000-0000-4000-8000-000000000002"
	kidES256   = "00000000-0000-4000-8000-000000000003"
)

func mustHex(t testing.TB, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func b64u(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// vectorKeys holds the three fixed keys.
type vectorKeys struct {
	current, revoked ed25519.PrivateKey
	es256            *ecdsa.PrivateKey
}

func loadVectorKeys(t testing.TB) vectorKeys {
	t.Helper()
	es, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), mustHex(t, rfc6979P256X))
	if err != nil {
		t.Fatal(err)
	}
	return vectorKeys{
		current: ed25519.NewKeyFromSeed(mustHex(t, rfc8032Test1Secret)),
		revoked: ed25519.NewKeyFromSeed(mustHex(t, rfc8032Test2Secret)),
		es256:   es,
	}
}

// Ordered JSON shapes: struct field order is the member order of the written documents.

type vecJWK struct {
	Kty string `json:"kty"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y,omitempty"`
	D   string `json:"d,omitempty"` // private fixtures of SCORE-37 only, RFC secrets
}

type vecRSAJWK struct {
	Kty string `json:"kty"`
	N   string `json:"n"`
	E   string `json:"e"`
}

type vecSigner struct {
	Algorithm       string   `json:"algorithm"`
	KeyID           string   `json:"keyId,omitempty"`
	PublicKey       any      `json:"publicKey,omitempty"`
	Excludes        []string `json:"excludes,omitempty"`
	CertificatePath []string `json:"certificatePath,omitempty"`
	Value           string   `json:"value,omitempty"`
}

type vecSigners struct {
	Signers []vecSigner `json:"signers,omitempty"`
	Chain   []vecSigner `json:"chain,omitempty"`
}

type vecProperty struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type vecComponent struct {
	Type        string `json:"type"`
	BOMRef      string `json:"bom-ref"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description,omitempty"`
	Purl        string `json:"purl"`
}

type vecTool struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type vecMetadata struct {
	Timestamp  string               `json:"timestamp"`
	Tools      map[string][]vecTool `json:"tools"`
	Authors    []map[string]string  `json:"authors"`
	Component  vecComponent         `json:"component"`
	Properties []vecProperty        `json:"properties"`
	Lifecycles []map[string]string  `json:"lifecycles"`
}

type vecDependency struct {
	Ref       string   `json:"ref"`
	DependsOn []string `json:"dependsOn"`
}

type vecDocument struct {
	BOMFormat    string          `json:"bomFormat"`
	SpecVersion  string          `json:"specVersion"`
	SerialNumber string          `json:"serialNumber"`
	Version      int             `json:"version"`
	Metadata     *vecMetadata    `json:"metadata,omitempty"`
	Components   []vecComponent  `json:"components"`
	Dependencies []vecDependency `json:"dependencies"`
	Signature    any             `json:"signature,omitempty"`
}

// marshalCompact writes JSON without HTML escaping and without a trailing newline.
func marshalCompact(t testing.TB, v any) []byte {
	t.Helper()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		t.Fatal(err)
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
}

// vectorDocument is the merged-export shape; signedKeyID empty means an unsigned export (NO_KEY).
func vectorDocument(signedKeyID string) vecDocument {
	props := []vecProperty{{Name: "reliza:export:signed", Value: "false"}, {Name: "reliza:export:unsignedReason", Value: "NO_KEY"}}
	if signedKeyID != "" {
		props = []vecProperty{{Name: "reliza:export:signed", Value: "true"}, {Name: "reliza:export:signatureKeyId", Value: signedKeyID}}
	}
	return vecDocument{
		BOMFormat:    "CycloneDX",
		SpecVersion:  "1.6",
		SerialNumber: "urn:uuid:00000000-0000-4000-8000-0000000000aa",
		Version:      1,
		Metadata: &vecMetadata{
			Timestamp:  "2026-10-13T00:00:00Z",
			Tools:      map[string][]vecTool{"components": {{Name: "ReARM", Version: "26.10.8"}}},
			Authors:    []map[string]string{{"name": "Example Org"}},
			Component:  vecComponent{Type: "application", BOMRef: "pkg:generic/example/app@1.0.0", Name: "app", Version: "1.0.0", Purl: "pkg:generic/example/app@1.0.0"},
			Properties: props,
			Lifecycles: []map[string]string{{"phase": "build"}},
		},
		Components: []vecComponent{
			{Type: "library", BOMRef: "pkg:npm/alpha@1.0.0", Name: "alpha", Version: "1.0.0",
				Description: "JSON & JCS <check>: caf\u00e9", Purl: "pkg:npm/alpha@1.0.0"},
			{Type: "library", BOMRef: "pkg:npm/beta@2.0.0", Name: "beta", Version: "2.0.0", Purl: "pkg:npm/beta@2.0.0"},
		},
		Dependencies: []vecDependency{
			{Ref: "pkg:generic/example/app@1.0.0", DependsOn: []string{"pkg:npm/alpha@1.0.0", "pkg:npm/beta@2.0.0"}},
			{Ref: "pkg:npm/alpha@1.0.0", DependsOn: []string{}},
			{Ref: "pkg:npm/beta@2.0.0", DependsOn: []string{}},
		},
	}
}

func ed25519JWK(pub ed25519.PublicKey) vecJWK {
	return vecJWK{Kty: "OKP", Crv: "Ed25519", X: b64u(pub)}
}

func es256JWK(t *testing.T, pub *ecdsa.PublicKey) vecJWK {
	t.Helper()
	b, err := pub.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return vecJWK{Kty: "EC", Crv: "P-256", X: b64u(b[1:33]), Y: b64u(b[33:])}
}

// canonicalOf is the signing input of a document whose signature has no value yet.
func canonicalOf(t *testing.T, doc vecDocument) []byte {
	t.Helper()
	c, err := signingInput(marshalCompact(t, doc))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// signEd25519 signs doc (its signature a *vecSigner without value) and sets the value.
func signEd25519(t *testing.T, doc vecDocument, signer *vecSigner, key ed25519.PrivateKey) []byte {
	t.Helper()
	doc.Signature = signer
	signer.Value = ""
	canonical := canonicalOf(t, doc)
	signer.Value = b64u(ed25519.Sign(key, canonical))
	return marshalCompact(t, doc)
}

// es256Value signs canonical with the ES256 key, raw r || s.
func es256Value(t *testing.T, key *ecdsa.PrivateKey, canonical []byte) string {
	t.Helper()
	h := sha256.Sum256(canonical)
	r, s, err := ecdsa.Sign(rand.Reader, key, h[:])
	if err != nil {
		t.Fatal(err)
	}
	raw := make([]byte, 64)
	r.FillBytes(raw[:32])
	s.FillBytes(raw[32:])
	return b64u(raw)
}

func pemOf(t *testing.T, pub any) []byte {
	t.Helper()
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
}

// jwksEntry is one key of the SCORE-32 public JWKS shape.
type jwksEntry struct {
	Kty       string  `json:"kty"`
	Crv       string  `json:"crv"`
	X         string  `json:"x"`
	D         string  `json:"d,omitempty"` // with-d.jwks.json only, an RFC secret
	Kid       string  `json:"kid"`
	Use       string  `json:"use"`
	Alg       string  `json:"alg"`
	Status    string  `json:"rearm:status"`
	NotBefore *string `json:"rearm:notBefore"`
	NotAfter  *string `json:"rearm:notAfter"`
	RevokedAt string  `json:"rearm:revokedAt,omitempty"`
}

// generateVectors returns every generated file by name. es256Kept is the committed value of
// signed-es256.cdx.json (empty to sign afresh).
func generateVectors(t *testing.T, k vectorKeys, es256Kept string) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	curPub := k.current.Public().(ed25519.PublicKey)
	revPub := k.revoked.Public().(ed25519.PublicKey)
	curJWK, revJWK := ed25519JWK(curPub), ed25519JWK(revPub)

	signed := signEd25519(t, vectorDocument(kidCurrent), &vecSigner{Algorithm: "Ed25519", KeyID: kidCurrent, PublicKey: curJWK}, k.current)
	files["signed-ed25519.cdx.json"] = signed
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, signed, "", "  "); err != nil {
		t.Fatal(err)
	}
	files["signed-ed25519.pretty.cdx.json"] = pretty.Bytes()
	canonical, err := signingInput(signed)
	if err != nil {
		t.Fatal(err)
	}
	files["canonical/signed-ed25519.jcs"] = canonical

	esDoc := vectorDocument(kidES256)
	esSigner := &vecSigner{Algorithm: "ES256", KeyID: kidES256, PublicKey: es256JWK(t, &k.es256.PublicKey)}
	esDoc.Signature = esSigner
	esCanonical := canonicalOf(t, esDoc)
	files["canonical/signed-es256.jcs"] = esCanonical
	esSigner.Value = es256Kept
	if esSigner.Value == "" {
		esSigner.Value = es256Value(t, k.es256, esCanonical)
	}
	files["signed-es256.cdx.json"] = marshalCompact(t, esDoc)

	files["keyid-only.cdx.json"] = signEd25519(t, vectorDocument(kidCurrent), &vecSigner{Algorithm: "Ed25519", KeyID: kidCurrent}, k.current)
	// T-27: names the current kid, signed with the TEST 2 key; only the kid-selected key may verify.
	files["keyid-only-wrong-key.cdx.json"] = signEd25519(t, vectorDocument(kidCurrent), &vecSigner{Algorithm: "Ed25519", KeyID: kidCurrent}, k.revoked)
	files["revoked-signed.cdx.json"] = signEd25519(t, vectorDocument(kidRevoked), &vecSigner{Algorithm: "Ed25519", KeyID: kidRevoked, PublicKey: revJWK}, k.revoked)
	files["embedded-wrong-key.cdx.json"] = signEd25519(t, vectorDocument(kidCurrent), &vecSigner{Algorithm: "Ed25519", KeyID: kidCurrent, PublicKey: revJWK}, k.current)

	// One byte of the second component's name changed after signing.
	if bytes.Count(signed, []byte(`"name":"beta"`)) != 1 {
		t.Fatal("the signed document must name beta once")
	}
	files["tampered.cdx.json"] = bytes.Replace(signed, []byte(`"name":"beta"`), []byte(`"name":"betA"`), 1)

	files["unsigned.cdx.json"] = marshalCompact(t, vectorDocument(""))

	malformed := vectorDocument(kidCurrent)
	malformed.Signature = &vecSigner{Algorithm: "Ed25519", KeyID: kidCurrent, PublicKey: curJWK}
	files["malformed.cdx.json"] = marshalCompact(t, malformed)
	malformedB64 := vectorDocument(kidCurrent)
	malformedB64.Signature = &vecSigner{Algorithm: "Ed25519", KeyID: kidCurrent, PublicKey: curJWK, Value: "not*base64*"}
	files["malformed-base64.cdx.json"] = marshalCompact(t, malformedB64)

	// RS256 with fixed bytes: well-formed, never verified.
	modulus := make([]byte, 256)
	value := make([]byte, 256)
	for i := range modulus {
		modulus[i] = byte(0xC1 + i*7)
		value[i] = byte(0x5A ^ i)
	}
	rs := vectorDocument(kidCurrent)
	rs.Signature = &vecSigner{Algorithm: "RS256", KeyID: kidCurrent, PublicKey: vecRSAJWK{Kty: "RSA", N: b64u(modulus), E: "AQAB"}, Value: b64u(value)}
	files["rs256.cdx.json"] = marshalCompact(t, rs)

	// T-24 shapes. Each signer of signers or chain signs the document with only its own entry,
	// value removed, so the first entry alone verifies under signingInput.
	sign1 := func(list string, signer vecSigner, key ed25519.PrivateKey) vecSigner {
		doc := vectorDocument(kidCurrent)
		alone := signer
		alone.Value = ""
		if list == jsfSigners {
			doc.Signature = vecSigners{Signers: []vecSigner{alone}}
		} else {
			doc.Signature = vecSigners{Chain: []vecSigner{alone}}
		}
		signer.Value = b64u(ed25519.Sign(key, canonicalOf(t, doc)))
		return signer
	}
	two := vectorDocument(kidCurrent)
	two.Signature = vecSigners{Signers: []vecSigner{
		sign1(jsfSigners, vecSigner{Algorithm: "Ed25519", KeyID: kidCurrent, PublicKey: curJWK}, k.current),
		sign1(jsfSigners, vecSigner{Algorithm: "Ed25519", KeyID: kidRevoked, PublicKey: revJWK}, k.revoked),
	}}
	files["signers-two.cdx.json"] = marshalCompact(t, two)
	chain := vectorDocument(kidCurrent)
	chain.Signature = vecSigners{Chain: []vecSigner{sign1(jsfChain, vecSigner{Algorithm: "Ed25519", KeyID: kidCurrent, PublicKey: curJWK}, k.current)}}
	files["chain.cdx.json"] = marshalCompact(t, chain)

	// excludes: JSF leaves the named root properties out of the signed data.
	excluded := vectorDocument(kidCurrent)
	excluded.Metadata = nil
	excludesSigner := &vecSigner{Algorithm: "Ed25519", KeyID: kidCurrent, PublicKey: curJWK, Excludes: []string{"metadata"}}
	excluded.Signature = excludesSigner
	excludesSigner.Value = b64u(ed25519.Sign(k.current, canonicalOf(t, excluded)))
	withMetadata := vectorDocument(kidCurrent)
	withMetadata.Signature = excludesSigner
	files["excludes.cdx.json"] = marshalCompact(t, withMetadata)

	certificate := sha256.Sum256([]byte("rearm-cli jsf vectors: not a certificate"))
	files["certificate-path.cdx.json"] = signEd25519(t, vectorDocument(kidCurrent),
		&vecSigner{Algorithm: "Ed25519", KeyID: kidCurrent, CertificatePath: []string{base64.StdEncoding.EncodeToString(certificate[:])}}, k.current)

	files["pub.pem"] = pemOf(t, curPub)
	files["pub-es256.pem"] = pemOf(t, &k.es256.PublicKey)
	type keyFileJWK struct {
		vecJWK
		Kid string `json:"kid"`
		Use string `json:"use"`
		Alg string `json:"alg"`
	}
	files["pub.jwk.json"] = append(marshalCompact(t, keyFileJWK{curJWK, kidCurrent, "sig", "Ed25519"}), '\n')

	// Design SCORE-37 3.6: the private-key fixtures, d the RFC secret of each key.
	curPrivJWK := curJWK
	curPrivJWK.D = b64u(k.current.Seed())
	esPrivJWK := es256JWK(t, &k.es256.PublicKey)
	esPrivJWK.D = b64u(k.es256.D.FillBytes(make([]byte, 32)))
	files["with-d.jwk.json"] = append(marshalCompact(t, keyFileJWK{curPrivJWK, kidCurrent, "sig", "Ed25519"}), '\n')
	files["with-d-es256.jwk.json"] = append(marshalCompact(t, keyFileJWK{esPrivJWK, kidES256, "sig", "ES256"}), '\n')
	files["embedded-d.cdx.json"] = signEd25519(t, vectorDocument(kidCurrent),
		&vecSigner{Algorithm: "Ed25519", KeyID: kidCurrent, PublicKey: curPrivJWK}, k.current)
	revokedAt := "2026-10-13T00:00:00Z"
	entry := func(jwk vecJWK, kid, status, revoked string) jwksEntry {
		return jwksEntry{Kty: jwk.Kty, Crv: jwk.Crv, X: jwk.X, Kid: kid, Use: "sig", Alg: "Ed25519", Status: status, RevokedAt: revoked}
	}
	files["keys.jwks.json"] = append(marshalCompact(t, map[string][]jwksEntry{"keys": {
		entry(curJWK, kidCurrent, "CURRENT", ""),
		entry(revJWK, kidRevoked, "REVOKED", revokedAt),
	}}), '\n')
	files["wrong-key.jwks.json"] = append(marshalCompact(t, map[string][]jwksEntry{"keys": {
		entry(revJWK, kidCurrent, "CURRENT", ""),
	}}), '\n')
	// SCORE-37: both CURRENT, the clean TEST 1 entry (the one signed-ed25519's keyId selects) first,
	// the TEST 2 entry carrying d second: the whole file is refused.
	leakedEntry := entry(revJWK, kidRevoked, "CURRENT", "")
	leakedEntry.D = b64u(k.revoked.Seed())
	files["with-d.jwks.json"] = append(marshalCompact(t, map[string][]jwksEntry{"keys": {
		entry(curJWK, kidCurrent, "CURRENT", ""),
		leakedEntry,
	}}), '\n')
	// T-27: both keys CURRENT, the TEST 2 key listed first.
	files["both-current.jwks.json"] = append(marshalCompact(t, map[string][]jwksEntry{"keys": {
		entry(revJWK, kidRevoked, "CURRENT", ""),
		entry(curJWK, kidCurrent, "CURRENT", ""),
	}}), '\n')
	return files
}

// committedES256Value reads the value of the committed signed-es256.cdx.json, empty when absent.
func committedES256Value(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(jsfDir, "signed-es256.cdx.json"))
	if err != nil {
		return ""
	}
	var doc struct {
		Signature struct {
			Value string `json:"value"`
		} `json:"signature"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	return doc.Signature.Value
}

func TestJSFVectors(t *testing.T) {
	k := loadVectorKeys(t)

	// The seed is the RFC 8032 TEST 1 secret key: its public key and its signature over the empty
	// message are the RFC's.
	curPub := k.current.Public().(ed25519.PublicKey)
	if got := hex.EncodeToString(curPub); got != rfc8032Test1Public {
		t.Fatalf("TEST 1 public key %s, want %s", got, rfc8032Test1Public)
	}
	if got := hex.EncodeToString(ed25519.Sign(k.current, nil)); got != rfc8032Test1Signature {
		t.Fatalf("TEST 1 signature %s, want the RFC's", got)
	}
	if got := ed25519JWK(curPub).X; got != currentJWKX {
		t.Fatalf("current JWK x %s, want %s", got, currentJWKX)
	}
	// SCORE-37 3.6: the d members are the base64url RFC 8032 TEST 1 and TEST 2 secret keys.
	if got := b64u(k.current.Seed()); got != currentJWKD {
		t.Fatalf("current JWK d %s, want %s", got, currentJWKD)
	}
	if got := b64u(k.revoked.Seed()); got != revokedJWKD {
		t.Fatalf("revoked JWK d %s, want %s", got, revokedJWKD)
	}
	if got := b64u(k.es256.D.FillBytes(make([]byte, 32))); got != es256JWKD {
		t.Fatalf("ES256 JWK d %s, want %s", got, es256JWKD)
	}
	// The ES256 key is the RFC 6979 A.2.5 key: its public point is Ux, Uy.
	if k.es256.X.Cmp(new(big.Int).SetBytes(mustHex(t, rfc6979P256Ux))) != 0 || k.es256.Y.Cmp(new(big.Int).SetBytes(mustHex(t, rfc6979P256Uy))) != 0 {
		t.Fatal("ES256 public key is not the RFC 6979 A.2.5 Ux, Uy")
	}

	kept := committedES256Value(t)
	if *update && *updateES256 {
		kept = ""
	}
	files := generateVectors(t, k, kept)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		path := filepath.Join(jsfDir, filepath.FromSlash(name))
		if *update {
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, files[name], 0644); err != nil {
				t.Fatal(err)
			}
		}
		committed, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%v (run go test ./internal/bomscore -run TestJSFVectors -update)", err)
		}
		if !bytes.Equal(committed, files[name]) {
			t.Errorf("%s differs from the regenerated file (run go test ./internal/bomscore -run TestJSFVectors -update):\n%s", path, files[name])
		}
	}

	// signed-es256 keeps its value across regenerations: verify it under the ES256 public key.
	es := readJSF(t, "signed-es256.cdx.json")
	canonical, err := signingInput(es)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := decodeBase64URL(committedES256Value(t))
	if err != nil || len(raw) != 64 {
		t.Fatalf("signed-es256 value: %v, %d bytes", err, len(raw))
	}
	h := sha256.Sum256(canonical)
	if !ecdsa.Verify(&k.es256.PublicKey, h[:], new(big.Int).SetBytes(raw[:32]), new(big.Int).SetBytes(raw[32:])) {
		t.Error("signed-es256.cdx.json does not verify under the RFC 6979 A.2.5 key")
	}
	// SCORE-37 T-6: with-d.jwk.json carries the pinned TEST 1 d, and embedded-d.cdx.json is a valid
	// signature under the TEST 1 public key, so only the leak check keeps it from verifying.
	var withD struct {
		D string `json:"d"`
	}
	if err := json.Unmarshal(files["with-d.jwk.json"], &withD); err != nil || withD.D != currentJWKD {
		t.Errorf("with-d.jwk.json d %q (%v), want %s", withD.D, err, currentJWKD)
	}
	leaked := files["embedded-d.cdx.json"]
	leakedInput, err := signingInput(leaked)
	if err != nil {
		t.Fatal(err)
	}
	var leakedDoc struct {
		Signature struct {
			Value string `json:"value"`
		} `json:"signature"`
	}
	if err := json.Unmarshal(leaked, &leakedDoc); err != nil {
		t.Fatal(err)
	}
	leakedValue, err := decodeBase64URL(leakedDoc.Signature.Value)
	if err != nil || !ed25519.Verify(curPub, leakedInput, leakedValue) {
		t.Errorf("embedded-d.cdx.json is not a valid signature under the TEST 1 public key (%v)", err)
	}
	// Every generated file is plain ASCII except the documents, whose one non-ASCII character is
	// the component description that exercises UTF-8 through JCS.
	for _, name := range names {
		if strings.HasSuffix(name, ".pem") || strings.HasSuffix(name, ".jwks.json") || strings.HasSuffix(name, ".jwk.json") {
			for _, c := range files[name] {
				if c > 127 {
					t.Errorf("%s is not plain ASCII", name)
					break
				}
			}
		}
	}
}

// readJSF reads one file of testdata/jsf.
func readJSF(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(jsfDir, filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	return b
}
