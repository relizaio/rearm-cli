package cmd

import (
	"strings"
	"testing"
)

// Redaction is the security boundary of this command: captured helm stderr can
// echo rendered values, and this agent runs inside the customer's cluster while
// the backend may be hosted. A miss here is credential exfiltration, so these
// cases are deliberately blunt.
func TestRedactSecretsRemovesCredentialMaterial(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"password assignment", `Error rendering: password=hunter2trustno1`},
		{"password colon yaml", `  adminPassword: "s3cr3t-value"`},
		{"api key", `apiKey = ak_live_9f8e7d6c5b4a3210`},
		{"api_key underscore", `api_key: "abcd1234efgh5678"`},
		{"token", `token: ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ012345`},
		{"client secret", `client_secret=zzzz-yyyy-xxxx`},
		{"authorization bearer", `Authorization: Bearer abc.def.ghi123`},
		{"url credentials", `failed to pull https://someuser:somepass@registry.example.com/chart`},
		{"jwt", `token was eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := redactSecrets(c.in)
			if !strings.Contains(got, "[REDACTED]") {
				t.Fatalf("expected redaction marker in %q", got)
			}
			for _, leak := range []string{"hunter2trustno1", "s3cr3t-value", "ak_live_9f8e7d6c5b4a3210",
				"abcd1234efgh5678", "ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ012345", "zzzz-yyyy-xxxx",
				"abc.def.ghi123", "somepass", "dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U"} {
				if strings.Contains(got, leak) {
					t.Fatalf("secret %q survived redaction: %q", leak, got)
				}
			}
		})
	}
}

func TestRedactSecretsRemovesPemPrivateKeyBlock(t *testing.T) {
	in := "before\n-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA1234\nabcd\n-----END RSA PRIVATE KEY-----\nafter"
	got := redactSecrets(in)
	if strings.Contains(got, "MIIEowIBAAKCAQEA1234") {
		t.Fatalf("PEM body survived redaction: %q", got)
	}
	// Surrounding diagnostic context must survive — the point is a usable error.
	if !strings.Contains(got, "before") || !strings.Contains(got, "after") {
		t.Fatalf("redaction destroyed surrounding context: %q", got)
	}
}

func TestRedactSecretsKeepsOrdinaryDiagnostics(t *testing.T) {
	// The real traefik failure that motivated this feature must survive intact.
	in := `UPGRADE FAILED: could not get information about the resource: clusterroles.rbac.authorization.k8s.io "traefik-traefik" is forbidden: User "system:serviceaccount:rearm-cd:rearm-cd" cannot get resource "clusterroles" in API group "rbac.authorization.k8s.io" at the cluster scope`
	got := redactSecrets(in)
	if got != in {
		t.Fatalf("ordinary RBAC diagnostic was altered:\n got: %q", got)
	}
}

func TestSanitizeDetailTruncatesAndMarks(t *testing.T) {
	got := sanitizeDetail(strings.Repeat("x", maxDetailChars*3))
	if len([]rune(got)) <= maxDetailChars {
		t.Fatalf("expected truncation marker to be appended, got len %d", len([]rune(got)))
	}
	if !strings.Contains(got, "[truncated]") {
		t.Fatal("truncation must be visible so an operator knows the tail is missing")
	}
	if len([]rune(got)) > maxDetailChars+32 {
		t.Fatalf("truncated payload too long: %d", len([]rune(got)))
	}
}

// Dedup only works if the same fault yields the same fingerprint across
// reconciles — otherwise a persistent failure inserts a row every 15s.
func TestDeriveFingerprintStableAcrossVolatileTokens(t *testing.T) {
	a := deriveFailureFingerprint("traefik", "HELM_INSTALL", "RBAC_FORBIDDEN",
		`UPGRADE FAILED at 2026-07-25T10:15:03Z for release 4f2c9a11-3f2e-4a55-8b21-0c1d2e3f4a5b attempt 17`)
	b := deriveFailureFingerprint("traefik", "HELM_INSTALL", "RBAC_FORBIDDEN",
		`UPGRADE FAILED at 2026-07-25T10:15:18Z for release 9c8b7a66-1d2e-4f33-9a44-5b6c7d8e9f00 attempt 18`)
	if a != b {
		t.Fatalf("fingerprint drifted on volatile tokens: %s vs %s", a, b)
	}
}

func TestDeriveFingerprintDistinguishesRealDifferences(t *testing.T) {
	base := deriveFailureFingerprint("traefik", "HELM_INSTALL", "RBAC_FORBIDDEN", "cannot get clusterroles")
	cases := map[string]string{
		"different deployment":    deriveFailureFingerprint("rearm-ui", "HELM_INSTALL", "RBAC_FORBIDDEN", "cannot get clusterroles"),
		"different phase":         deriveFailureFingerprint("traefik", "VALUES_MERGE", "RBAC_FORBIDDEN", "cannot get clusterroles"),
		"different failure class": deriveFailureFingerprint("traefik", "HELM_INSTALL", "TIMEOUT", "cannot get clusterroles"),
		"different message":       deriveFailureFingerprint("traefik", "HELM_INSTALL", "RBAC_FORBIDDEN", "chart not found"),
	}
	for name, fp := range cases {
		if fp == base {
			t.Fatalf("%s collided with base fingerprint", name)
		}
	}
}

// The batch path must not be a way around redaction/truncation.
func TestSanitizeEventBatchAppliesRedactionAndDefaults(t *testing.T) {
	raw := []byte(`[{"deploymentName":"traefik","namespace":"traefik","detail":"password=leakme123","message":"boom"}]`)
	events, err := sanitizeEventBatch(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	e := events[0]
	if strings.Contains(e["detail"].(string), "leakme123") {
		t.Fatalf("batch path bypassed redaction: %v", e["detail"])
	}
	if e["phase"] != "UNKNOWN" || e["failureClass"] != "UNKNOWN" {
		t.Fatalf("expected UNKNOWN defaults, got %v/%v", e["phase"], e["failureClass"])
	}
	if fp, _ := e["fingerprint"].(string); fp == "" {
		t.Fatal("fingerprint must be derived when omitted, else dedup breaks")
	}
	if at, _ := e["attemptedAt"].(string); at == "" {
		t.Fatal("attemptedAt must default to now")
	}
}

func TestSanitizeEventBatchRejectsMalformedInput(t *testing.T) {
	if _, err := sanitizeEventBatch([]byte(`{"deploymentName":"x"}`)); err == nil {
		t.Fatal("expected error for non-array batch")
	}
	if _, err := sanitizeEventBatch([]byte(`[{"namespace":"traefik"}]`)); err == nil {
		t.Fatal("expected error for event without deploymentName")
	}
}

func TestSanitizeEventBatchNormalizesFailureClassSeparators(t *testing.T) {
	events, err := sanitizeEventBatch([]byte(`[{"deploymentName":"t","failureClass":"rbac-forbidden","phase":"helm_install"}]`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if events[0]["failureClass"] != "RBAC_FORBIDDEN" {
		t.Fatalf("expected RBAC_FORBIDDEN, got %v", events[0]["failureClass"])
	}
	if events[0]["phase"] != "HELM_INSTALL" {
		t.Fatalf("expected HELM_INSTALL, got %v", events[0]["phase"])
	}
}

// Regression: helm values are overwhelmingly camelCase, so an anchored
// \bpassword\b misses adminPassword/registryToken/dbSecretKey entirely. This
// was a real leak in the first cut of the redactor.
func TestRedactSecretsHandlesCamelCaseAndPrefixedKeys(t *testing.T) {
	cases := []struct{ in, leak string }{
		{`  adminPassword: "s3cr3t-value"`, "s3cr3t-value"},
		{`registryToken: ghp_zzzzzzzzzzzzzzzzzzzz`, "ghp_zzzzzzzzzzzzzzzzzzzz"},
		{`dbSecretKey = topsecretvalue`, "topsecretvalue"},
		{`global.imagePullSecret: "regcred-xyz"`, "regcred-xyz"},
		{`MY_APP_PASSWORD=letmein999`, "letmein999"},
	}
	for _, c := range cases {
		got := redactSecrets(c.in)
		if strings.Contains(got, c.leak) {
			t.Fatalf("camelCase/prefixed secret survived redaction: %q -> %q", c.in, got)
		}
	}
}

// Redaction must not eat ordinary Kubernetes diagnostics that merely contain
// the substring "auth" (rbac.authorization.k8s.io) or "author".
func TestRedactSecretsDoesNotMaulAuthorizationDiagnostics(t *testing.T) {
	for _, in := range []string{
		`clusterroles.rbac.authorization.k8s.io "traefik-traefik" is forbidden`,
		`chart author: Reliza`,
		`forwarding request to authority service`,
	} {
		if got := redactSecrets(in); got != in {
			t.Fatalf("diagnostic was mangled:\n in: %q\nout: %q", in, got)
		}
	}
}
