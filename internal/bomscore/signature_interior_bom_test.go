package bomscore

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"testing"

	"github.com/gowebpki/jcs"
)

// T-6 of SCORE-34 run 3 (design SCORE-34 ADR-10 step 1, ADR-2): only ONE LEADING UTF-8 BOM is
// stripped from the signing input, as Load strips it. A U+FEFF anywhere else is content: inside a
// string or a member name it is signed, so inserting or removing one after signing does not
// verify; between tokens or after the document it is not JSON, and Load refuses the document. The
// other byte-level spellings JCS (RFC 8785) folds are pinned next to it: whitespace between tokens
// (incl. CRLF) and string escapes are not content; whitespace and Unicode inside strings are, with
// no normalisation (no NFC).

// bomBytes is U+FEFF in UTF-8, spelled as bytes so the source stays ASCII.
var bomBytes = []byte{0xEF, 0xBB, 0xBF}

// spliced is doc with the first occurrence of old replaced by the parts joined; it fails the
// test when old is not in doc, so a fixture change cannot make a case vacuous.
func spliced(t *testing.T, doc []byte, old string, parts ...[]byte) []byte {
	t.Helper()
	if !bytes.Contains(doc, []byte(old)) {
		t.Fatalf("fixture has no %q", old)
	}
	return bytes.Replace(doc, []byte(old), bytes.Join(parts, nil), 1)
}

// interiorBOMDocuments are signed-ed25519.cdx.json changed after signing so that the signed
// content differs: each must give FAILED, signature does not verify.
func interiorBOMDocuments(t *testing.T) map[string][]byte {
	t.Helper()
	doc := readJSF(t, "signed-ed25519.cdx.json")
	b := func(s string) []byte { return []byte(s) }
	threeTimes := spliced(t, doc, `"name":"alpha"`, b(`"name":"al`), bomBytes, b(`pha"`))
	threeTimes = spliced(t, threeTimes, `"name":"beta"`, b(`"name":"`), bomBytes, b(`beta"`))
	threeTimes = spliced(t, threeTimes, `"version":"2.0.0"`, b(`"version":"2.0.0`), bomBytes, b(`"`))
	return map[string][]byte{
		"U+FEFF inside a string value":           spliced(t, doc, `"name":"alpha"`, b(`"name":"al`), bomBytes, b(`pha"`)),
		"U+FEFF at the start of a string value":  spliced(t, doc, `"name":"alpha"`, b(`"name":"`), bomBytes, b(`alpha"`)),
		"U+FEFF at the end of a string value":    spliced(t, doc, `"name":"alpha"`, b(`"name":"alpha`), bomBytes, b(`"`)),
		"U+FEFF three times":                     threeTimes,
		"U+FEFF inside a member name":            spliced(t, doc, `"description"`, b(`"descr`), bomBytes, b(`iption"`)),
		"U+FEFF inside a signed property value":  spliced(t, doc, `"value":"true"`, b(`"value":"tr`), bomBytes, b(`ue"`)),
		"leading BOM and U+FEFF inside a value":  append(withBOM(nil), spliced(t, doc, `"name":"alpha"`, b(`"name":"al`), bomBytes, b(`pha"`))...),
		"escaped \\ufeff inside a string value":  spliced(t, doc, `"name":"alpha"`, b(`"name":"al\ufeffpha"`)),
		"a space inside a string value":          spliced(t, doc, `"name":"alpha"`, b(`"name":"al pha"`)),
		"a decomposed e-acute (no NFC)":          spliced(t, doc, "caf\xc3\xa9", b("cafe\xcc\x81")),
		"U+FEFF in a value, pretty-printed form": spliced(t, readJSF(t, "signed-ed25519.pretty.cdx.json"), `"name": "alpha"`, b(`"name": "al`), bomBytes, b(`pha"`)),
	}
}

func TestInteriorBOMIsSignedContent(t *testing.T) {
	for name, data := range interiorBOMDocuments(t) {
		for _, key := range []string{"", jsfKey("pub.pem"), jsfKey("keys.jwks.json")} {
			rep, err := VerifySignature(data, key)
			if err != nil || rep.Verification != VerificationFailed || rep.Detail != "signature does not verify" {
				t.Errorf("%s, key %q: %+v %v, want FAILED, signature does not verify", name, key, rep, err)
			}
			if key == "" {
				continue
			}
			r, check := scoreSignature(t, data, key)
			if r.Input.Signature.Verification != VerificationFailed || check.Status != StatusFail {
				t.Errorf("%s under --signature-key %s: %s, check %s %q, want FAILED and FAIL",
					name, key, r.Input.Signature.Verification, check.Status, check.Note)
			}
		}
	}
}

// The signing input keeps an interior U+FEFF as it is and drops only the leading one.
func TestSigningInputKeepsInteriorBOM(t *testing.T) {
	doc := readJSF(t, "signed-ed25519.cdx.json")
	interior := spliced(t, doc, `"name":"alpha"`, []byte(`"name":"al`), bomBytes, []byte(`pha"`))
	got, err := signingInput(interior)
	if err != nil {
		t.Fatal(err)
	}
	if n := bytes.Count(got, bomBytes); n != 1 || !bytes.Contains(got, append(append([]byte(`"name":"al`), bomBytes...), `pha"`...)) {
		t.Errorf("interior U+FEFF: signing input holds %d U+FEFF, want the one inside the value:\n%s", n, got)
	}
	if want := readJSF(t, "canonical/signed-ed25519.jcs"); bytes.Equal(got, want) {
		t.Error("interior U+FEFF: signing input equals that of the document without it")
	}
	lead, err := signingInput(withBOM(interior))
	if err != nil || !bytes.Equal(lead, got) {
		t.Errorf("leading BOM and interior U+FEFF: %v, signing input\n%s\nwant\n%s", err, lead, got)
	}
}

// A U+FEFF that is not inside a string is not JSON: Load refuses the document (unparsable after
// the leading BOM, unsupported before the first token), so it is never VERIFIED.
func TestBOMOutsideStringsIsRefused(t *testing.T) {
	doc := readJSF(t, "signed-ed25519.cdx.json")
	for _, c := range []struct {
		name string
		data []byte
		kind RefusalKind
	}{
		{"U+FEFF between tokens", spliced(t, doc, `,"components"`, []byte(","), bomBytes, []byte(`"components"`)), RefusalUnparsable},
		{"U+FEFF after a value", spliced(t, doc, `"name":"alpha"`, []byte(`"name":"alpha"`), bomBytes), RefusalUnparsable},
		{"U+FEFF after the document", append(append([]byte{}, doc...), bomBytes...), RefusalUnparsable},
		{"two leading BOMs", withBOM(withBOM(doc)), RefusalUnsupported},
		{"whitespace before a leading BOM", append([]byte(" "), withBOM(doc)...), RefusalUnsupported},
	} {
		for _, key := range []string{"", jsfKey("pub.pem")} {
			rep, err := VerifySignature(c.data, key)
			var refusal *RefusalError
			if !errors.As(err, &refusal) || refusal.Kind != c.kind {
				t.Errorf("%s, key %q: %+v %v, want refused as %s", c.name, key, rep, err, c.kind)
			}
		}
	}
}

// What JCS folds is not content: JSON whitespace around and between tokens, CRLF line ends and
// the escaped spelling of a character all verify.
func TestSigningInputFoldsOnlyJSONSpelling(t *testing.T) {
	doc := readJSF(t, "signed-ed25519.cdx.json")
	pretty := readJSF(t, "signed-ed25519.pretty.cdx.json")
	want := readJSF(t, "canonical/signed-ed25519.jcs")
	for name, data := range map[string][]byte{
		"JSON whitespace around the document": append(append([]byte(" \t\r\n"), doc...), " \t\r\n"...),
		"BOM, then JSON whitespace":           withBOM(append([]byte("\r\n\t "), doc...)),
		"CRLF line ends":                      bytes.ReplaceAll(pretty, []byte("\n"), []byte("\r\n")),
		"escaped e-acute":                     spliced(t, doc, "caf\xc3\xa9", []byte(`caf\u00e9`)),
		"escaped ASCII letter (\\u0061)":      spliced(t, doc, `"name":"alpha"`, []byte(`"name":"\u0061lpha"`)),
	} {
		got, err := signingInput(data)
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("%s: %v, signing input\n%s\nwant\n%s", name, err, got, want)
		}
		if rep, err := VerifySignature(data, jsfKey("pub.pem")); err != nil || rep.Verification != VerificationVerified {
			t.Errorf("%s: %+v %v, want VERIFIED", name, rep, err)
		}
	}
}

// signedWithInteriorBOM is the vector document with a U+FEFF inside a component name, signed by
// the RFC 8032 TEST 1 key over its RFC 8785 form computed here, independently of signingInput.
func signedWithInteriorBOM(t *testing.T) []byte {
	t.Helper()
	key := loadVectorKeys(t).current
	doc := vectorDocument(kidCurrent)
	doc.Components[0].Name = "al" + string(bomBytes) + "pha"
	signer := &vecSigner{Algorithm: "Ed25519", KeyID: kidCurrent, PublicKey: ed25519JWK(key.Public().(ed25519.PublicKey))}
	doc.Signature = signer
	canonical, err := jcs.Transform(marshalCompact(t, doc))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(canonical, bomBytes) {
		t.Fatal("the RFC 8785 form lost the U+FEFF")
	}
	signer.Value = b64u(ed25519.Sign(key, canonical))
	signed := marshalCompact(t, doc)
	if got, err := signingInput(signed); err != nil || !bytes.Equal(got, canonical) {
		t.Errorf("signing input of a document signed over a U+FEFF: %v\n%s\nwant\n%s", err, got, canonical)
	}
	return signed
}

func TestDocumentSignedOverInteriorBOMVerifies(t *testing.T) {
	signed := signedWithInteriorBOM(t)
	for name, data := range map[string][]byte{
		"as signed":                    signed,
		"behind a leading BOM":         withBOM(signed),
		"with the U+FEFF escaped":      spliced(t, signed, "al"+string(bomBytes)+"pha", []byte(`al\ufeffpha`)),
		"with the U+FEFF escaped (uc)": spliced(t, signed, "al"+string(bomBytes)+"pha", []byte(`al\uFEFFpha`)),
	} {
		for _, key := range []string{"", jsfKey("pub.pem")} {
			if rep, err := VerifySignature(data, key); err != nil || rep.Verification != VerificationVerified {
				t.Errorf("%s, key %q: %+v %v, want VERIFIED", name, key, rep, err)
			}
		}
		r, check := scoreSignature(t, data, jsfKey("pub.pem"))
		if r.Input.Signature.Verification != VerificationVerified || check.Status != StatusPass {
			t.Errorf("%s under --signature-key: %s, check %s %q, want VERIFIED and PASS",
				name, r.Input.Signature.Verification, check.Status, check.Note)
		}
	}
	// Removing the signed U+FEFF is a change of content.
	stripped := spliced(t, signed, "al"+string(bomBytes)+"pha", []byte("alpha"))
	if rep, err := VerifySignature(stripped, jsfKey("pub.pem")); err != nil ||
		rep.Verification != VerificationFailed || rep.Detail != "signature does not verify" {
		t.Errorf("signed U+FEFF removed: %+v %v, want FAILED, signature does not verify", rep, err)
	}
}
