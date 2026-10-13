package bomscore

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// T-5 of SCORE-34 run 2 (design SCORE-34 ADR-3): when a kid appears twice in a JWKS, the first entry
// under it is the key, never a later one, even when the later one would verify.

const duplicateKid = "00000000-0000-4000-8000-000000000001"

// duplicateKidJWKS writes a JWKS holding the RFC 8032 TEST 1 key (the signer of keyid-only.cdx.json)
// and the TEST 2 key, both CURRENT, both under duplicateKid, TEST 2 first when test2First.
func duplicateKidJWKS(t *testing.T, test2First bool) string {
	t.Helper()
	var set struct {
		Keys []map[string]any `json:"keys"`
	}
	if err := json.Unmarshal(readJSF(t, "both-current.jwks.json"), &set); err != nil {
		t.Fatal(err)
	}
	// both-current.jwks.json lists TEST 2 (kid ...0002) then TEST 1 (kid ...0001).
	test2, test1 := set.Keys[0], set.Keys[1]
	if test2["x"] != "PUAXw-hDiVqStwqnTRt-vJyYLM8uxJaMwM1V8Sr0Zgw" || test1["x"] != "11qYAYKxCrfVS_7TyWQHOg7hcvPapiMlrwIaaPcHURo" {
		t.Fatalf("both-current.jwks.json changed: %v", set.Keys)
	}
	test2["kid"], test1["kid"] = duplicateKid, duplicateKid
	set.Keys = []map[string]any{test1, test2}
	if test2First {
		set.Keys = []map[string]any{test2, test1}
	}
	data, err := json.Marshal(set)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "duplicate-kid.jwks.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDuplicateKidFirstEntryWins(t *testing.T) {
	doc := readJSF(t, "keyid-only.cdx.json")
	signatureCase{"keyid-only / TEST 2 first under the kid", doc, duplicateKidJWKS(t, true), VerificationFailed,
		"signature does not verify", StatusFail, wantNotVerifyNote}.run(t)
	signatureCase{"keyid-only / TEST 1 first under the kid", doc, duplicateKidJWKS(t, false), VerificationVerified,
		"verified with the given key (signature embeds no publicKey)", StatusPass, "verified with key " + duplicateKid}.run(t)

	for _, c := range []struct {
		test2First bool
		x          string
	}{
		{true, "PUAXw-hDiVqStwqnTRt-vJyYLM8uxJaMwM1V8Sr0Zgw"},
		{false, "11qYAYKxCrfVS_7TyWQHOg7hcvPapiMlrwIaaPcHURo"},
	} {
		keys, err := LoadKeySet(duplicateKidJWKS(t, c.test2First))
		if err != nil {
			t.Fatal(err)
		}
		x, err := base64.RawURLEncoding.DecodeString(c.x)
		if err != nil {
			t.Fatal(err)
		}
		e, detail, ok := keys.Select(duplicateKid)
		if got, isEd := e.pub.(ed25519.PublicKey); !ok || detail != "" || !isEd || !bytes.Equal(got, x) {
			t.Errorf("Select(%s), TEST 2 first %v: %v %q, want the first entry under the kid (x %s)", duplicateKid, c.test2First, ok, detail, c.x)
		}
	}
}
