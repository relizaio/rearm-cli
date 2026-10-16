package bomscore

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// T-20 (design SCORE-34 3.5): input.signature in the report, the signatureKey echo and the text
// line.

// memberNames lists the member names of the JSON object at path, in document order.
func memberNames(t *testing.T, doc []byte, path ...string) []string {
	t.Helper()
	var m map[string]json.RawMessage
	raw := json.RawMessage(doc)
	for _, p := range path {
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatal(err)
		}
		raw = m[p]
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	if _, err := dec.Token(); err != nil {
		t.Fatal(err)
	}
	var names []string
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, tok.(string))
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			t.Fatal(err)
		}
	}
	return names
}

func TestInputSignatureInReport(t *testing.T) {
	data := readJSF(t, "signed-ed25519.cdx.json")
	r, _ := scoreSignature(t, data, "")
	out, err := r.JSON()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := memberNames(t, out, "input", "signature"), []string{"present", "wellFormed", "algorithm", "keyId", "verification", "detail"}; !reflect.DeepEqual(got, want) {
		t.Errorf("input.signature members %v, want %v", got, want)
	}
	if got := memberNames(t, out, "input"); got[len(got)-2] != "sha256" || got[len(got)-1] != "signature" {
		t.Errorf("input members %v, want signature right after sha256", got)
	}
	var decoded struct {
		ReportVersion int `json:"reportVersion"`
		Input         struct {
			SHA256    string         `json:"sha256"`
			Signature map[string]any `json:"signature"`
		} `json:"input"`
		Options map[string]any `json:"options"`
	}
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	if decoded.ReportVersion != 1 || decoded.Input.SHA256 != hex.EncodeToString(sum[:]) {
		t.Errorf("reportVersion %d sha256 %s, want 1 and the hash of the bytes as read", decoded.ReportVersion, decoded.Input.SHA256)
	}
	want := map[string]any{"present": true, "wellFormed": true, "algorithm": "Ed25519", "keyId": "00000000-0000-4000-8000-000000000001",
		"verification": "NOT_REQUESTED", "detail": ""}
	if !reflect.DeepEqual(decoded.Input.Signature, want) {
		t.Errorf("input.signature %v, want %v", decoded.Input.Signature, want)
	}
	if _, ok := decoded.Options["signatureKey"]; ok {
		t.Errorf("options %v, want no signatureKey without a key", decoded.Options)
	}
	if strings.Contains(r.Text(), "signature:") {
		t.Errorf("text without a key has a signature line:\n%s", r.Text())
	}

	key := jsfKey("pub.pem")
	r, _ = scoreSignature(t, data, key)
	out, err = r.JSON()
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Options["signatureKey"] != key || decoded.Input.Signature["verification"] != "VERIFIED" {
		t.Errorf("with a key: options %v, signature %v; want the path echoed and VERIFIED", decoded.Options, decoded.Input.Signature)
	}
	if first := lines(r.Text())[0]; first != "signature: VERIFIED, verified with key 00000000-0000-4000-8000-000000000001" {
		t.Errorf("text first line %q, want the signature line", first)
	}
	r, _ = scoreSignature(t, readJSF(t, "tampered.cdx.json"), key)
	if text := r.Text(); lines(text)[0] != "signature: FAILED, signature does not verify" ||
		!strings.Contains(text, "  FAIL  SBOM Author Signature  (Table 1, SBOM Author Signature)\n        remedy: "+wantRemedy+"\n        note: "+wantNotVerifyNote+"\n") {
		t.Errorf("tampered text:\n%s", text)
	}
	// With --skip-files too, the options line stays first.
	r, err = Score(data, []string{string(ProfileCISA2026)}, testEngineVersion, Options{SkipFiles: true, SignatureKey: key})
	if err != nil {
		t.Fatal(err)
	}
	if l := lines(r.Text()); !strings.HasPrefix(l[0], "options: --skip-files") || !strings.HasPrefix(l[1], "signature: VERIFIED") {
		t.Errorf("text with both options:\n%s", r.Text())
	}
}
