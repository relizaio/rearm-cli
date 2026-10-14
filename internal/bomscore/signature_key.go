package bomscore

import (
	"bytes"
	"crypto"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// keyStatus is the rearm:status of a JWKS entry (SCORE-32 key lifecycle), upper-cased when read;
// the verification reads REVOKED and RETIRED, any other value verifies as usual.
type keyStatus string

const (
	keyStatusRevoked keyStatus = "REVOKED"
	keyStatusRetired keyStatus = "RETIRED"
)

// JWK key types and the members the engine reads (RFC 7517, RFC 7518; rearm:* SCORE-32).
const (
	jwkKtyOKP       = "OKP"
	jwkKtyEC        = "EC"
	jwkKtyRSA       = "RSA"
	jwkKty          = "kty"
	jwkCrv          = "crv"
	jwkX            = "x"
	jwkY            = "y"
	jwkKid          = "kid"
	jwkKeys         = "keys"
	jwkRearmStatus  = "rearm:status"
	jwkRearmAfter   = "rearm:notAfter"
	jwkRearmRevoked = "rearm:revokedAt"
)

// supportedKeys is the tail of every refusal of an unsupported key.
const (
	supportedPEMKeys = "(PEM SubjectPublicKeyInfo of an Ed25519 or EC P-256/P-384/P-521 key)"
	supportedJWKKeys = "(JWK OKP Ed25519 or EC P-256/P-384/P-521)"
	pemPublicKey     = "PUBLIC KEY"
)

// jwkCurves are the EC curves a JWK may name, by crv.
var jwkCurves = map[string]elliptic.Curve{curveP256: elliptic.P256(), curveP384: elliptic.P384(), curveP521: elliptic.P521()}

// KeySet is the content of one --signature-key / --key file: one PEM key, one JWK or a JWKS.
type KeySet struct {
	path    string
	entries []KeyEntry
}

// KeyEntry is one key of a KeySet. pub is nil for a JWKS entry whose kty or crv the engine does not
// support (kept so selection by kid can still name it).
type KeyEntry struct {
	kid       string
	kty, crv  string
	pub       crypto.PublicKey
	status    keyStatus
	notAfter  string
	revokedAt string
}

// describe names the key type for a message: "key curve Ed448", "key type RSA". Never key material.
func (e KeyEntry) describe() string {
	if e.crv != "" {
		return "key curve " + e.crv
	}
	return "key type " + e.kty
}

// keyFileError prefixes a key-file refusal with the path.
func keyFileError(path, format string, args ...any) error {
	return fmt.Errorf("signature key %s: %s", path, fmt.Sprintf(format, args...))
}

// LoadKeySet reads a key file (design SCORE-34 ADR-3): PEM SubjectPublicKeyInfo, else a JWKS (a
// top-level keys array), else a single JWK; anything else is refused. Every error is a refusal of
// the run, never a verification outcome.
func LoadKeySet(path string) (*KeySet, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, keyFileError(path, "%v", err)
	}
	neither := keyFileError(path, "not a PEM public key, a JWK or a JWKS")
	if bytes.Contains(b, []byte("-----BEGIN ")) {
		block, _ := pem.Decode(b)
		if block == nil {
			return nil, neither
		}
		pub, err := pemPublicKeyOf(path, block)
		if err != nil {
			return nil, err
		}
		return &KeySet{path: path, entries: []KeyEntry{{pub: pub, crv: keyCurve(pub)}}}, nil
	}
	var top map[string]json.RawMessage
	if json.Unmarshal(b, &top) != nil {
		return nil, neither
	}
	if raw, ok := top[jwkKeys]; ok {
		var list []json.RawMessage
		if json.Unmarshal(raw, &list) != nil {
			return nil, neither
		}
		if len(list) == 0 {
			return nil, keyFileError(path, "the JWKS holds no key")
		}
		set := &KeySet{path: path}
		for i, raw := range list {
			e, err := parseJWK(raw)
			if err != nil {
				return nil, keyFileError(path, "keys[%d]: %v", i, err)
			}
			set.entries = append(set.entries, e)
		}
		return set, nil
	}
	if _, ok := top[jwkKty]; ok {
		e, err := parseJWK(b)
		if err != nil {
			return nil, keyFileError(path, "%v", err)
		}
		if e.pub == nil {
			return nil, keyFileError(path, "JWK %s is not a supported public key %s", jwkName(e), supportedJWKKeys)
		}
		return &KeySet{path: path, entries: []KeyEntry{e}}, nil
	}
	return nil, neither
}

// pemPublicKeyOf parses a PEM block that must be the SubjectPublicKeyInfo of an Ed25519 or EC
// P-256/P-384/P-521 key. A private key, a certificate or another key type is refused by name.
func pemPublicKeyOf(path string, block *pem.Block) (crypto.PublicKey, error) {
	refuse := func(what string) error {
		return keyFileError(path, "%s is not a supported public key %s", what, supportedPEMKeys)
	}
	if block.Type != pemPublicKey {
		return nil, refuse("PEM block " + block.Type)
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, refuse("PEM PUBLIC KEY that does not parse (" + err.Error() + ")")
	}
	switch k := pub.(type) {
	case ed25519.PublicKey:
		return k, nil
	case *ecdsa.PublicKey:
		if _, ok := jwkCurves[k.Curve.Params().Name]; ok {
			return k, nil
		}
		return nil, refuse("EC " + k.Curve.Params().Name + " public key")
	case *rsa.PublicKey:
		return nil, refuse("RSA public key")
	case *ecdh.PublicKey:
		return nil, refuse("ECDH public key")
	}
	return nil, refuse(fmt.Sprintf("%T", pub))
}

func jwkName(e KeyEntry) string {
	if e.crv != "" {
		return "kty " + e.kty + " crv " + e.crv
	}
	return "kty " + e.kty
}

// parseJWK reads one JWK (a key file, a JWKS entry or a JSF publicKey). An OKP Ed25519 or EC
// P-256/P-384/P-521 key must decode to a valid public key, else it is an error; any other kty or
// crv returns an entry with a nil pub. kid and the rearm:* members are kept when they are strings.
func parseJWK(raw []byte) (KeyEntry, error) {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return KeyEntry{}, errors.New("JWK is not a JSON object")
	}
	kty, ok := jsonString(m[jwkKty])
	if !ok {
		return KeyEntry{}, errors.New("JWK has no kty")
	}
	e := KeyEntry{kty: kty}
	e.crv, _ = jsonString(m[jwkCrv])
	e.kid, _ = jsonString(m[jwkKid])
	status, _ := jsonString(m[jwkRearmStatus])
	e.status = keyStatus(strings.ToUpper(status))
	e.notAfter, _ = jsonString(m[jwkRearmAfter])
	e.revokedAt, _ = jsonString(m[jwkRearmRevoked])
	coordinate := func(name string, size int) ([]byte, error) {
		v, _ := jsonString(m[name])
		b, err := decodeBase64URL(v)
		if err != nil || len(b) != size {
			return nil, fmt.Errorf("JWK %s %s is not %d bytes of base64url", kty, name, size)
		}
		return b, nil
	}
	switch {
	case kty == jwkKtyOKP && e.crv == curveEd25519:
		x, err := coordinate(jwkX, ed25519.PublicKeySize)
		if err != nil {
			return KeyEntry{}, err
		}
		e.pub = ed25519.PublicKey(x)
	case kty == jwkKtyEC && jwkCurves[e.crv] != nil:
		curve := jwkCurves[e.crv]
		size := (curve.Params().BitSize + 7) / 8
		x, err := coordinate(jwkX, size)
		if err != nil {
			return KeyEntry{}, err
		}
		y, err := coordinate(jwkY, size)
		if err != nil {
			return KeyEntry{}, err
		}
		point := append(append([]byte{4}, x...), y...)
		pub, err := ecdsa.ParseUncompressedPublicKey(curve, point)
		if err != nil {
			return KeyEntry{}, errors.New("JWK EC " + e.crv + " point is not on the curve")
		}
		e.pub = pub
	}
	return e, nil
}

// Select picks the key for a signature's keyId (design SCORE-34 ADR-3): a file of one key without a
// kid is that key; else the first entry whose kid equals keyId; with no keyId the single entry. ok
// is false with the KEY_MISMATCH detail when nothing is selected.
func (k *KeySet) Select(keyID string) (KeyEntry, string, bool) {
	if len(k.entries) == 1 && k.entries[0].kid == "" {
		return k.entries[0], "", true
	}
	if keyID != "" {
		for _, e := range k.entries {
			if e.kid == keyID {
				return e, "", true
			}
		}
		return KeyEntry{}, "no key with kid " + keyID + " in " + k.path, false
	}
	if len(k.entries) == 1 {
		return k.entries[0], "", true
	}
	return KeyEntry{}, k.path + " holds " + strconv.Itoa(len(k.entries)) + " keys and the signature names no keyId", false
}
