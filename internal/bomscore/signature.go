package bomscore

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"strconv"
	"strings"
	"unicode"

	"github.com/gowebpki/jcs"
)

// Verification is the outcome of verifying the root signature (input.signature.verification).
type Verification string

const (
	VerificationNotRequested         Verification = "NOT_REQUESTED"
	VerificationVerified             Verification = "VERIFIED"
	VerificationFailed               Verification = "FAILED"
	VerificationKeyMismatch          Verification = "KEY_MISMATCH"
	VerificationKeyRevoked           Verification = "KEY_REVOKED"
	VerificationUnsupportedAlgorithm Verification = "UNSUPPORTED_ALGORITHM"
	VerificationUnsupportedForm      Verification = "UNSUPPORTED_FORM"
	// VerificationNoKey: verify-signature without a key on a signature that embeds no publicKey.
	// Score never reports it.
	VerificationNoKey Verification = "NO_KEY"
)

// SignatureReport is input.signature of the report and the JSON output of verify-signature.
// Field order is the JSON key order; every field is always emitted.
type SignatureReport struct {
	Present      bool         `json:"present"`
	WellFormed   bool         `json:"wellFormed"`
	Algorithm    string       `json:"algorithm"`
	KeyID        string       `json:"keyId"`
	Verification Verification `json:"verification"`
	Detail       string       `json:"detail"`
}

// signatureForm is the shape of a root signature.
type signatureForm int

const (
	formSingle signatureForm = iota
	formSigners
	formChain
	formXML
)

// The fixed notes and texts of the signature check (design SCORE-34 3.2 and 3.5).
const (
	remedySignature = "configure an organization SBOM signing key in ReARM (Organization settings, SBOM export signing); " +
		"exports are then signed"
	noteSignatureNotVerified = "present, not verified (no key given)"
	noteSignatureMalformed   = "signature present but malformed"
	noteSignatureNotVerify   = "signature does not verify with the given key"
)

// The malformed reasons, in the order they are tried.
const (
	reasonValueMissing     = "value missing"
	reasonValueEmpty       = "value empty"
	reasonValueNotBase64   = "value is not base64url"
	reasonAlgorithmMissing = "algorithm missing"
	reasonAlgorithmEmpty   = "algorithm empty"
	reasonSignersEmpty     = "signers empty"
	reasonChainEmpty       = "chain empty"
)

// The members of a JSF signature object the engine reads.
const (
	jsfValue           = "value"
	jsfAlgorithm       = "algorithm"
	jsfKeyID           = "keyId"
	jsfPublicKey       = "publicKey"
	jsfExcludes        = "excludes"
	jsfCertificatePath = "certificatePath"
	jsfSigners         = "signers"
	jsfChain           = "chain"
	rootSignature      = "signature"
)

// SignatureState is the root signature as the loader read it, completed by Score with the
// verification outcome.
type SignatureState struct {
	Present      bool
	WellFormed   bool
	Algorithm    string
	KeyID        string
	Verification Verification
	// Detail is the malformed reason after loading, then the verification detail.
	Detail string

	form    signatureForm
	signers int       // entries of signers or chain
	signer  jsfSigner // the single signer, or the first entry of signers or chain
}

// jsfSigner is one well-formed signer: value decoded, publicKey kept raw (nil when absent).
type jsfSigner struct {
	value           []byte
	publicKey       json.RawMessage
	excludes        bool
	certificatePath bool
}

func (s *SignatureState) report() SignatureReport {
	return SignatureReport{Present: s.Present, WellFormed: s.WellFormed, Algorithm: s.Algorithm, KeyID: s.KeyID,
		Verification: s.Verification, Detail: s.Detail}
}

// PrintableText escapes control and format characters (C0, DEL, C1, bidi and zero-width
// marks) as \uXXXX, so a value read from a document (algorithm, keyId, a JWK crv) cannot break a
// text line or spoof one: a line verify-signature prints is one line. Printable text, non-ASCII
// letters included, is kept as is.
func PrintableText(s string) string {
	if strings.IndexFunc(s, isUnprintable) < 0 {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		if isUnprintable(r) {
			fmt.Fprintf(&b, "\\u%04x", r)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func isUnprintable(r rune) bool {
	return r == unicode.ReplacementChar || unicode.Is(unicode.Cc, r) || unicode.Is(unicode.Cf, r)
}

// isNull: an absent member or a JSON null.
func isNull(raw json.RawMessage) bool {
	t := bytes.TrimSpace(raw)
	return len(t) == 0 || string(t) == "null"
}

// jsonString reads a member that must be a JSON string; ok is false for an absent member, null or
// any other type (a member that is not a string counts as missing).
func jsonString(raw json.RawMessage) (string, bool) {
	t := bytes.TrimSpace(raw)
	if len(t) == 0 || t[0] != '"' {
		return "", false
	}
	var s string
	if json.Unmarshal(t, &s) != nil {
		return "", false
	}
	return s, true
}

// decodeBase64URL decodes base64url without padding (RFC 4648 section 5) strictly: no padding, no
// line breaks (the standard decoder skips them), no non-zero trailing bits.
func decodeBase64URL(s string) ([]byte, error) {
	if strings.ContainsAny(s, "\r\n") {
		return nil, errors.New("line break in base64url")
	}
	return base64.RawURLEncoding.Strict().DecodeString(s)
}

// readJSONSignature reads the root signature member of a CycloneDX JSON document, raw, never
// through cyclonedx-go (its JSFSignature drops the single-signer members).
func readJSONSignature(raw json.RawMessage) *SignatureState {
	if isNull(raw) {
		return &SignatureState{}
	}
	s := &SignatureState{Present: true}
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		s.Detail = reasonValueMissing
		return s
	}
	var entries []json.RawMessage
	emptyReason := ""
	switch {
	case !isNull(m[jsfSigners]):
		s.form, emptyReason = formSigners, reasonSignersEmpty
		_ = json.Unmarshal(m[jsfSigners], &entries)
	case !isNull(m[jsfChain]):
		s.form, emptyReason = formChain, reasonChainEmpty
		_ = json.Unmarshal(m[jsfChain], &entries)
	default:
		s.signer, s.Algorithm, s.KeyID, s.Detail = readSigner(m)
		s.WellFormed = s.Detail == ""
		return s
	}
	s.signers = len(entries)
	if len(entries) == 0 {
		s.Detail = emptyReason
		return s
	}
	for i, e := range entries {
		var em map[string]json.RawMessage
		if json.Unmarshal(e, &em) != nil {
			em = map[string]json.RawMessage{}
		}
		signer, alg, keyID, reason := readSigner(em)
		if i == 0 {
			s.signer, s.Algorithm, s.KeyID = signer, alg, keyID
		}
		if reason != "" && s.Detail == "" {
			s.Detail = reason
		}
	}
	s.WellFormed = s.Detail == ""
	return s
}

// readSigner checks one signer object; reason is the first malformed reason, empty when
// well-formed. algorithm and keyId are returned whenever they are strings.
func readSigner(m map[string]json.RawMessage) (signer jsfSigner, algorithm, keyID, reason string) {
	algorithm, hasAlgorithm := jsonString(m[jsfAlgorithm])
	keyID, _ = jsonString(m[jsfKeyID])
	if !isNull(m[jsfPublicKey]) {
		signer.publicKey = m[jsfPublicKey]
	}
	signer.excludes = !isNull(m[jsfExcludes])
	signer.certificatePath = !isNull(m[jsfCertificatePath])

	value, hasValue := jsonString(m[jsfValue])
	switch {
	case !hasValue:
		return signer, algorithm, keyID, reasonValueMissing
	case value == "":
		return signer, algorithm, keyID, reasonValueEmpty
	}
	decoded, err := decodeBase64URL(value)
	if err != nil {
		return signer, algorithm, keyID, reasonValueNotBase64
	}
	signer.value = decoded
	switch {
	case !hasAlgorithm:
		return signer, algorithm, keyID, reasonAlgorithmMissing
	case algorithm == "":
		return signer, algorithm, keyID, reasonAlgorithmEmpty
	}
	if signer.publicKey != nil {
		if why := publicKeyMalformed(signer.publicKey); why != "" {
			return signer, algorithm, keyID, "publicKey malformed (" + why + ")"
		}
	}
	return signer, algorithm, keyID, ""
}

// publicKeyMalformed checks the shape of an embedded JWK (JSF publicKey): an object with kty OKP
// (crv, x), EC (crv, x, y) or RSA (n, e), every coordinate base64url. Empty when well-formed.
func publicKeyMalformed(raw json.RawMessage) string {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return "not an object"
	}
	kty, ok := jsonString(m[jwkKty])
	if !ok {
		return "kty missing"
	}
	var names, coordinates []string
	switch kty {
	case jwkKtyOKP:
		names, coordinates = []string{jwkCrv}, []string{jwkX}
	case jwkKtyEC:
		names, coordinates = []string{jwkCrv}, []string{jwkX, jwkY}
	case jwkKtyRSA:
		coordinates = []string{"n", "e"}
	default:
		return "kty " + strconv.Quote(kty) + " is not OKP, EC or RSA"
	}
	for _, n := range names {
		if v, ok := jsonString(m[n]); !ok || v == "" {
			return n + " missing"
		}
	}
	for _, n := range coordinates {
		v, ok := jsonString(m[n])
		if !ok {
			return n + " missing"
		}
		if _, err := decodeBase64URL(v); err != nil {
			return n + " is not base64url"
		}
	}
	return ""
}

// unsupportedForm names a well-formed JSF shape the engine does not verify (design SCORE-34 ADR-8);
// empty for the single-signer form and for signers with exactly one entry.
func (s *SignatureState) unsupportedForm() string {
	switch {
	case s.form == formXML:
		return "XML signature (XML-DSig) is not verified"
	case s.form == formChain:
		return "signature chain"
	case s.form == formSigners && s.signers > 1:
		return "multiple signers (" + strconv.Itoa(s.signers) + ")"
	case s.signer.excludes:
		return jsfExcludes
	case s.signer.certificatePath:
		return jsfCertificatePath
	}
	return ""
}

// objectMembers decodes one JSON object into its members and refuses a member name given twice:
// RFC 8785 input is I-JSON, and a map would keep only the last of the two, so the signed input and
// what another reader sees could differ.
func objectMembers(raw []byte) (map[string]json.RawMessage, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, errors.New("not a JSON object")
	}
	members := map[string]json.RawMessage{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		name, _ := tok.(string)
		if _, dup := members[name]; dup {
			return nil, fmt.Errorf("duplicate member %s", strconv.Quote(name))
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, err
		}
		members[name] = v
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("data after the JSON object")
	}
	return members, nil
}

// signingInput is the JSF signing input of a document (design SCORE-34 ADR-2): the whole document
// as read, with the root signature in place and only its value removed (for signers or chain with
// one entry, that entry's value), canonicalised with RFC 8785 JCS.
func signingInput(input []byte) ([]byte, error) {
	top, err := objectMembers(bytes.TrimPrefix(input, utf8BOM))
	if err != nil {
		return nil, err
	}
	sig, err := objectMembers(top[rootSignature])
	if err != nil {
		return nil, fmt.Errorf("signature: %w", err)
	}
	listName := ""
	switch {
	case !isNull(sig[jsfSigners]):
		listName = jsfSigners
	case !isNull(sig[jsfChain]):
		listName = jsfChain
	}
	if listName == "" {
		delete(sig, jsfValue)
	} else {
		var entries []json.RawMessage
		if err := json.Unmarshal(sig[listName], &entries); err != nil || len(entries) != 1 {
			return nil, fmt.Errorf("signature: %s does not hold exactly one entry", listName)
		}
		entry, err := objectMembers(entries[0])
		if err != nil {
			return nil, fmt.Errorf("signature: %s[0]: %w", listName, err)
		}
		delete(entry, jsfValue)
		if entries[0], err = json.Marshal(entry); err != nil {
			return nil, err
		}
		if sig[listName], err = json.Marshal(entries); err != nil {
			return nil, err
		}
	}
	if top[rootSignature], err = json.Marshal(sig); err != nil {
		return nil, err
	}
	b, err := json.Marshal(top)
	if err != nil {
		return nil, err
	}
	return jcs.Transform(b)
}

// algorithmSpec is one algorithm the engine verifies.
type algorithmSpec struct {
	curve     string // the key's curve name as keyCurve reports it
	valueSize int    // bytes of the decoded value
	hash      func([]byte) []byte
}

// The JSF algorithm names the engine verifies (case-sensitive, as JSF writes them).
const (
	algorithmEd25519 = "Ed25519"
	algorithmES256   = "ES256"
	algorithmES384   = "ES384"
	algorithmES512   = "ES512"
)

// Curve names as keyCurve reports them and JWK crv writes them.
const (
	curveEd25519 = "Ed25519"
	curveP256    = "P-256"
	curveP384    = "P-384"
	curveP521    = "P-521"
)

var supportedAlgorithms = map[string]algorithmSpec{
	algorithmEd25519: {curve: curveEd25519, valueSize: ed25519.SignatureSize},
	algorithmES256:   {curve: curveP256, valueSize: 64, hash: func(b []byte) []byte { h := sha256.Sum256(b); return h[:] }},
	algorithmES384:   {curve: curveP384, valueSize: 96, hash: func(b []byte) []byte { h := sha512.Sum384(b); return h[:] }},
	algorithmES512:   {curve: curveP521, valueSize: 132, hash: func(b []byte) []byte { h := sha512.Sum512(b); return h[:] }},
}

// keyCurve names the curve of a supported public key: Ed25519, P-256, P-384, P-521.
func keyCurve(pub crypto.PublicKey) string {
	switch k := pub.(type) {
	case ed25519.PublicKey:
		return curveEd25519
	case *ecdsa.PublicKey:
		return k.Curve.Params().Name
	}
	return ""
}

func sameKey(a, b crypto.PublicKey) bool {
	k, ok := a.(interface{ Equal(crypto.PublicKey) bool })
	return ok && b != nil && k.Equal(b)
}

// verifyValue checks value over canonical under pub with the algorithm's curve and hash. ECDSA
// values are the JWS raw r || s (RFC 7518 section 3.4), not ASN.1 DER.
func verifyValue(spec algorithmSpec, pub crypto.PublicKey, canonical, value []byte) bool {
	switch k := pub.(type) {
	case ed25519.PublicKey:
		return len(k) == ed25519.PublicKeySize && ed25519.Verify(k, canonical, value)
	case *ecdsa.PublicKey:
		half := spec.valueSize / 2
		r := new(big.Int).SetBytes(value[:half])
		s := new(big.Int).SetBytes(value[half:])
		return ecdsa.Verify(k, spec.hash(canonical), r, s)
	}
	return false
}

// verify fills d.Signature.Verification and Detail. keys is the key the user gave; nil verifies
// against the embedded publicKey (verify-signature without --key only).
func verify(input []byte, d *Doc, keys *KeySet) {
	s := d.Signature
	set := func(v Verification, detail string) { s.Verification, s.Detail = v, PrintableText(detail) }
	switch {
	case d.notRepresentable(FieldSignature):
		set(VerificationFailed, "no signature (not representable in SPDX 2.x)")
		return
	case !s.Present:
		set(VerificationFailed, "no signature")
		return
	case !s.WellFormed:
		set(VerificationFailed, s.Detail)
		return
	}
	if form := s.unsupportedForm(); form != "" {
		set(VerificationUnsupportedForm, form)
		return
	}
	spec, ok := supportedAlgorithms[s.Algorithm]
	if !ok {
		set(VerificationUnsupportedAlgorithm, s.Algorithm)
		return
	}

	var pub crypto.PublicKey
	verified := ""
	if keys == nil {
		if s.signer.publicKey == nil {
			set(VerificationNoKey, "signature embeds no publicKey; give --key")
			return
		}
		embedded, err := parseJWK(s.signer.publicKey)
		if err != nil {
			set(VerificationFailed, "embedded publicKey: "+err.Error())
			return
		}
		if embedded.pub == nil {
			set(VerificationFailed, "embedded publicKey: "+embedded.describe()+" is not supported")
			return
		}
		pub, verified = embedded.pub, "verified with the embedded publicKey"
	} else {
		entry, detail, ok := keys.Select(s.KeyID)
		if !ok {
			set(VerificationKeyMismatch, detail)
			return
		}
		if entry.status == keyStatusRevoked {
			detail := "key " + entry.kid + " is " + string(keyStatusRevoked) + " in " + keys.path
			if entry.revokedAt != "" {
				detail += " since " + entry.revokedAt
			}
			set(VerificationKeyRevoked, detail)
			return
		}
		if s.signer.publicKey != nil {
			embedded, err := parseJWK(s.signer.publicKey)
			if err != nil || entry.pub == nil || !sameKey(entry.pub, embedded.pub) {
				set(VerificationKeyMismatch, "embedded publicKey differs from the given key")
				return
			}
		}
		if entry.pub == nil {
			set(VerificationFailed, entry.describe()+" does not match "+s.Algorithm)
			return
		}
		pub = entry.pub
		switch {
		case s.signer.publicKey == nil:
			verified = "verified with the given key (signature embeds no publicKey)"
		case s.KeyID != "":
			verified = "verified with key " + s.KeyID
		default:
			verified = "verified with the given key"
		}
		if entry.status == keyStatusRetired {
			verified += "; key " + entry.kid + " is " + string(keyStatusRetired)
			if entry.notAfter != "" {
				verified += " since " + entry.notAfter
			}
		}
	}

	if curve := keyCurve(pub); curve != spec.curve {
		set(VerificationFailed, "key curve "+curve+" does not match "+s.Algorithm)
		return
	}
	if n := len(s.signer.value); n != spec.valueSize {
		set(VerificationFailed, fmt.Sprintf("value is %d bytes, %s needs %d", n, s.Algorithm, spec.valueSize))
		return
	}
	canonical, err := signingInput(input)
	if err != nil {
		set(VerificationFailed, "document is not canonicalizable (RFC 8785): "+err.Error())
		return
	}
	if !verifyValue(spec, pub, canonical, s.signer.value) {
		set(VerificationFailed, "signature does not verify")
		return
	}
	set(VerificationVerified, verified)
}

// hasSignature: cisa-2026.sbom-author-signature. Present and well-formed passes; with a key given,
// a FAILED, KEY_MISMATCH or KEY_REVOKED verification fails the check (operator point O-5).
func hasSignature(d *Doc) Outcome {
	if d.notRepresentable(FieldSignature) {
		return docNotRepresentable(d)
	}
	s := d.Signature
	if s == nil || !s.Present {
		return docOutcome(false)
	}
	if !s.WellFormed {
		return Outcome{Passed: 0, Total: 1, Note: noteSignatureMalformed}
	}
	o := docOutcome(true)
	switch s.Verification {
	case VerificationVerified:
		o.Note = "verified with the given key"
		if s.KeyID != "" {
			o.Note = "verified with key " + PrintableText(s.KeyID)
		}
	case VerificationFailed, VerificationKeyMismatch, VerificationKeyRevoked:
		o = Outcome{Passed: 0, Total: 1, Note: noteSignatureNotVerify}
	case VerificationUnsupportedAlgorithm, VerificationUnsupportedForm:
		o.Note = "present, not verified (" + string(s.Verification) + ": " + s.Detail + ")"
	default:
		o.Note = noteSignatureNotVerified
	}
	return o
}

// VerifySignature loads input and verifies its root signature: with keyPath under that key file
// (the path Score takes with Options.SignatureKey), without it against the embedded publicKey. A
// refused input returns a *RefusalError, an unusable key file its error.
func VerifySignature(input []byte, keyPath string) (SignatureReport, error) {
	d, err := Load(input)
	if err != nil {
		return SignatureReport{}, err
	}
	var keys *KeySet
	if keyPath != "" {
		if keys, err = LoadKeySet(keyPath); err != nil {
			return SignatureReport{}, err
		}
	}
	d.ensureSignature()
	verify(input, d, keys)
	return d.Signature.report(), nil
}

// ensureSignature gives a document whose loader read no signature (SPDX) an absent one.
func (d *Doc) ensureSignature() {
	if d.Signature == nil {
		d.Signature = &SignatureState{}
	}
}
