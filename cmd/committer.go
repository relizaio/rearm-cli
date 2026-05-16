/*
Copyright Reliza Incorporated. 2019 - 2026. Licensed under the terms of Apache-2.0.
*/

package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// `rearm committer ...` — manage human / external-bot committers
// and their enrolled signing keys. Parallel to `rearm agent enrollkey`.

var committerCmd = &cobra.Command{
	Use:   "committer",
	Short: "Manage committers and their signing keys",
	Long: `Committers are the natural-person (or external-bot) identity
the verifier resolves a commit author header to. Owning enrolled
signing keys lets policies enforce "commits in this release must
be signed by an approved committer".`,
}

var (
	committerOrg        string
	committerName       string
	committerEmail      string
	committerAliases    string
	committerUser       string
	committerUuid       string
	committerArchiveAll bool

	enrollCommitterUuid string
)

var committerCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create or update a committer",
	Run: func(cmd *cobra.Command, args []string) {
		if committerOrg == "" || committerName == "" || committerEmail == "" {
			fmt.Fprintln(os.Stderr, "--org, --name, and --email are required")
			os.Exit(1)
		}
		input := map[string]interface{}{
			"org":   committerOrg,
			"name":  committerName,
			"email": strings.ToLower(committerEmail),
		}
		if committerUuid != "" {
			input["uuid"] = committerUuid
		}
		if committerUser != "" {
			input["user"] = committerUser
		}
		if committerAliases != "" {
			parts := []string{}
			for _, a := range strings.Split(committerAliases, ",") {
				a = strings.TrimSpace(strings.ToLower(a))
				if a != "" {
					parts = append(parts, a)
				}
			}
			input["aliases"] = parts
		}
		query := `
			mutation ($input: CommitterInput!) {
				upsertCommitter(input: $input) {
					uuid
					org
					name
					email
					user
					aliases
					status
					createdDate
				}
			}
		`
		variables := map[string]interface{}{"input": input}
		data, err := sendGraphQLRequest(query, variables, rearmUri+"/graphql")
		if err != nil {
			printGqlError(err)
			os.Exit(1)
		}
		emitJson(data["upsertCommitter"])
	},
}

var committerArchiveCmd = &cobra.Command{
	Use:   "archive <committer-uuid>",
	Short: "Archive a committer (keys remain valid for historical verdicts)",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		query := `
			mutation ($uuid: ID!) {
				archiveCommitter(uuid: $uuid) {
					uuid
					status
				}
			}
		`
		data, err := sendGraphQLRequest(query, map[string]interface{}{"uuid": args[0]}, rearmUri+"/graphql")
		if err != nil {
			printGqlError(err)
			os.Exit(1)
		}
		emitJson(data["archiveCommitter"])
	},
}

var committerEnrollkeyCmd = &cobra.Command{
	Use:   "enrollkey",
	Short: "Enrol a public key for a committer",
	Long: `Mirrors agent enrollkey but binds the key to a committer
instead of an agent. Same --format / --pubkey-file / --identity
contract — see rearm agent enrollkey --help for the underlying
fingerprint-derivation rules.`,
	Run: func(cmd *cobra.Command, args []string) {
		if enrollCommitterUuid == "" || enrollKeyOrg == "" || enrollKeyFormat == "" || enrollPubkeyFile == "" {
			fmt.Fprintln(os.Stderr, "--committer, --org, --format, and --pubkey-file are required")
			os.Exit(1)
		}
		pubKey, err := os.ReadFile(enrollPubkeyFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to read pubkey file: %v\n", err)
			os.Exit(1)
		}
		runEnrollkey("COMMITTER", enrollCommitterUuid, enrollKeyOrg, enrollKeyFormat,
			enrollPubkeyFile, string(pubKey), enrollFingerprint, enrollKeyIdentity)
	},
}

func init() {
	committerCreateCmd.PersistentFlags().StringVar(&committerOrg, "org", "", "Org UUID — required")
	committerCreateCmd.PersistentFlags().StringVar(&committerName, "name", "", "Committer display name — required")
	committerCreateCmd.PersistentFlags().StringVar(&committerEmail, "email", "", "Primary email — required (becomes the trust binding)")
	committerCreateCmd.PersistentFlags().StringVar(&committerAliases, "aliases", "", "Comma-separated list of alias emails this committer has used")
	committerCreateCmd.PersistentFlags().StringVar(&committerUser, "user", "", "Optional binding to a ReARM user UUID")
	committerCreateCmd.PersistentFlags().StringVar(&committerUuid, "uuid", "", "Existing committer uuid for update; omit to create")
	_ = committerCreateCmd.MarkPersistentFlagRequired("org")
	_ = committerCreateCmd.MarkPersistentFlagRequired("name")
	_ = committerCreateCmd.MarkPersistentFlagRequired("email")

	committerEnrollkeyCmd.PersistentFlags().StringVar(&enrollCommitterUuid, "committer", "", "Committer UUID — required")
	committerEnrollkeyCmd.PersistentFlags().StringVar(&enrollKeyOrg, "org", "", "Org UUID — required")
	committerEnrollkeyCmd.PersistentFlags().StringVar(&enrollKeyFormat, "format", "", "SSH or GPG — required")
	committerEnrollkeyCmd.PersistentFlags().StringVar(&enrollPubkeyFile, "pubkey-file", "", "Pubkey file path — required")
	committerEnrollkeyCmd.PersistentFlags().StringVar(&enrollFingerprint, "fingerprint", "", "Override the auto-derived fingerprint")
	committerEnrollkeyCmd.PersistentFlags().StringVar(&enrollKeyIdentity, "identity", "", "Allowed-signers principal (required for SSH)")
	_ = committerEnrollkeyCmd.MarkPersistentFlagRequired("committer")
	_ = committerEnrollkeyCmd.MarkPersistentFlagRequired("org")
	_ = committerEnrollkeyCmd.MarkPersistentFlagRequired("format")
	_ = committerEnrollkeyCmd.MarkPersistentFlagRequired("pubkey-file")

	committerCmd.AddCommand(committerCreateCmd)
	committerCmd.AddCommand(committerArchiveCmd)
	committerCmd.AddCommand(committerEnrollkeyCmd)
	rootCmd.AddCommand(committerCmd)
}
