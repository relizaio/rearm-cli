/*
Copyright Reliza Incorporated. 2019 - 2026. Licensed under the terms of Apache-2.0.
*/

package cmd

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

// Enrols a public key against an existing Agent so the verifier can
// match commit signatures back to that agent. The corresponding
// committer-side enrolment lives in `rearm committer enrollkey`.
//
// Fingerprint is derived from the local --pubkey-file:
//
//   SSH — `ssh-keygen -lf <pubkey>` → `SHA256:<base64>`
//   GPG — `gpg --with-colons --show-keys` → `fpr:::...:<hex>:`
//
// The user can override with --fingerprint when the local tool is
// not available (e.g. CI shipping a pre-computed value).

var (
	enrollAgentUuid   string
	enrollKeyFormat   string
	enrollPubkeyFile  string
	enrollFingerprint string
	enrollKeyIdentity string
)

var agentEnrollkeyCmd = &cobra.Command{
	Use:   "enrollkey",
	Short: "Enrol a public key for an Agent so the verifier can match its commits",
	Long: `Binds a public key to an existing agent row. Subsequent signed commits
attributed to this agent (via the ReARM-Agent: trailer) verify only
when the signature matches one of the keys enrolled here.

Fingerprint defaults to the value derived from --pubkey-file via
the local ssh-keygen or gpg binary. Pass --fingerprint explicitly
to skip the local-tool dependency.

For SSH, --identity is required and must match the
allowed_signers principal the verifier will check against
(usually the user / agent email).`,
	Run: func(cmd *cobra.Command, args []string) {
		if enrollAgentUuid == "" || enrollKeyFormat == "" || enrollPubkeyFile == "" {
			fmt.Fprintln(os.Stderr, "--agent, --format, and --pubkey-file are required")
			os.Exit(1)
		}
		pubKey, err := os.ReadFile(enrollPubkeyFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to read pubkey file: %v\n", err)
			os.Exit(1)
		}
		runEnrollkey(enrollAgentUuid, enrollKeyFormat,
			enrollPubkeyFile, string(pubKey), enrollFingerprint, enrollKeyIdentity)
	},
}

func runEnrollkey(ownerUuid, format, pubkeyFile, pubKey, fingerprint, identity string) {
	if fingerprint == "" {
		fp, err := deriveFingerprint(format, pubkeyFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Could not derive fingerprint locally: %v\n", err)
			fmt.Fprintln(os.Stderr, "Pass --fingerprint explicitly or install ssh-keygen / gpg.")
			os.Exit(1)
		}
		fingerprint = fp
	}
	// AGENT enrolment goes via the FREEFORM-auth'd programmatic mutation
	// — that's what lets an agent bootstrap its own signing key without
	// an operator JWT. The org is not sent: the server always uses the
	// org the calling key resolves to (AgentSigningKeyInput has no org
	// field). COMMITTER enrolment stays on the JWT-authenticated
	// `enrollSigningKey`, which is operator-only and not exposed here.
	const op = "enrollSigningKeyProgrammatic"
	const argName = "signingKey"
	query := `
		mutation ($` + argName + `: AgentSigningKeyInput!) {
			` + op + `(` + argName + `: $` + argName + `) {
				uuid
				format
				ownerType
				ownerUuid
				fingerprint
				identity
				createdDate
			}
		}
	`
	input := map[string]interface{}{
		"format":      strings.ToUpper(format),
		"ownerType":   "AGENT",
		"ownerUuid":   ownerUuid,
		"fingerprint": fingerprint,
		"pubKey":      strings.TrimSpace(pubKey),
	}
	if identity != "" {
		input["identity"] = identity
	}
	variables := map[string]interface{}{argName: input}
	data, err := sendGraphQLRequest(query, variables, rearmUri+"/graphql")
	if err != nil {
		printGqlError(err)
		os.Exit(1)
	}
	emitJson(data[op])
}

func deriveFingerprint(format, pubkeyFile string) (string, error) {
	switch strings.ToUpper(format) {
	case "SSH":
		out, err := exec.Command("ssh-keygen", "-lf", pubkeyFile).Output()
		if err != nil {
			return "", fmt.Errorf("ssh-keygen -lf failed: %w", err)
		}
		// Output: <bits> SHA256:<fp> <comment> (<type>)
		parts := strings.Fields(string(out))
		if len(parts) < 2 {
			return "", fmt.Errorf("unexpected ssh-keygen output: %q", string(out))
		}
		return parts[1], nil
	case "GPG":
		var stdout bytes.Buffer
		cmd := exec.Command("gpg", "--batch", "--with-colons", "--show-keys", pubkeyFile)
		cmd.Stdout = &stdout
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("gpg --show-keys failed: %w", err)
		}
		for _, line := range strings.Split(stdout.String(), "\n") {
			if strings.HasPrefix(line, "fpr:") {
				cols := strings.Split(line, ":")
				if len(cols) > 9 && cols[9] != "" {
					return cols[9], nil
				}
			}
		}
		return "", fmt.Errorf("no fpr line in gpg --show-keys output")
	default:
		return "", fmt.Errorf("unsupported format for fingerprint derivation: %s", format)
	}
}

func init() {
	agentEnrollkeyCmd.PersistentFlags().StringVar(&enrollAgentUuid, "agent", "", "UUID of the agent — required")
	agentEnrollkeyCmd.PersistentFlags().StringVar(&enrollKeyFormat, "format", "", "Signature format: SSH or GPG — required")
	agentEnrollkeyCmd.PersistentFlags().StringVar(&enrollPubkeyFile, "pubkey-file", "", "Path to the public key (single-line SSH or ASCII-armoured GPG) — required")
	agentEnrollkeyCmd.PersistentFlags().StringVar(&enrollFingerprint, "fingerprint", "", "Override the auto-derived fingerprint")
	agentEnrollkeyCmd.PersistentFlags().StringVar(&enrollKeyIdentity, "identity", "", "Allowed-signers principal (required for SSH; e.g. email)")
	_ = agentEnrollkeyCmd.MarkPersistentFlagRequired("agent")
	_ = agentEnrollkeyCmd.MarkPersistentFlagRequired("format")
	_ = agentEnrollkeyCmd.MarkPersistentFlagRequired("pubkey-file")

	agentCmd.AddCommand(agentEnrollkeyCmd)
}
