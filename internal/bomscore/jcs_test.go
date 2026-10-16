package bomscore

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/gowebpki/jcs"
)

// T-18 (design SCORE-34): gowebpki/jcs against the hand-written RFC 8785 vectors, and signingInput
// against the canonical signing inputs the generator recorded (the files rearm-saas compares its
// canonicaliser's bytes with).
func TestJCSVectors(t *testing.T) {
	var vectors []struct {
		Name     string `json:"name"`
		Input    string `json:"input"`
		Expected string `json:"expected"`
	}
	if err := json.Unmarshal(readJSF(t, "jcs-vectors.json"), &vectors); err != nil {
		t.Fatal(err)
	}
	if len(vectors) < 10 {
		t.Fatalf("%d vectors, want at least ten", len(vectors))
	}
	for _, v := range vectors {
		got, err := jcs.Transform([]byte(v.Input))
		if err != nil {
			t.Errorf("%s: %v", v.Name, err)
			continue
		}
		if string(got) != v.Expected {
			t.Errorf("%s:\n got %q\nwant %q", v.Name, got, v.Expected)
		}
	}

	ed := readJSF(t, "canonical/signed-ed25519.jcs")
	for _, name := range []string{"signed-ed25519.cdx.json", "signed-ed25519.pretty.cdx.json"} {
		got, err := signingInput(readJSF(t, name))
		if err != nil || !bytes.Equal(got, ed) {
			t.Errorf("signingInput(%s) = %q, %v; want canonical/signed-ed25519.jcs", name, got, err)
		}
	}
	if got, err := signingInput(readJSF(t, "signed-es256.cdx.json")); err != nil || !bytes.Equal(got, readJSF(t, "canonical/signed-es256.jcs")) {
		t.Errorf("signingInput(signed-es256) = %q, %v; want canonical/signed-es256.jcs", got, err)
	}
	// The signing input keeps every signature member but value, sorted, and nothing else changes:
	// it is the canonical form of the document with value deleted.
	if !bytes.Contains(ed, []byte(`"signature":{"algorithm":"Ed25519","keyId":"`+kidCurrent+`","publicKey":{"crv":"Ed25519","kty":"OKP","x":"`+currentJWKX+`"}},"specVersion"`)) {
		t.Errorf("canonical signing input does not hold the signature without value: %s", ed)
	}
}
