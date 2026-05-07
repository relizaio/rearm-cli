/*
The MIT License (MIT)

Copyright (c) 2020 - 2026 Reliza Incorporated (Reliza (tm), https://reliza.io)

Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated documentation files (the "Software"),
to deal in the Software without restriction, including without limitation the rights to use, copy, modify, merge, publish, distribute, sublicense,
and/or sell copies of the Software, and to permit persons to whom the Software is furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY,
WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.

*/

package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Flags for pullrequest upsert. Distinct from the addrelease/getversion
// --pr-* flags so this command can stand alone — runs unconditionally
// from CI on pull_request events, independent of any release create.
var (
	prUpsertIdentity         string
	prUpsertState            string
	prUpsertTitle            string
	prUpsertSourceBranchName string
	prUpsertTargetBranchName string
	prUpsertEndpoint         string
	prUpsertVcsUri           string
	prUpsertRepoPath         string
	prUpsertVcsDisplayName   string
	prUpsertComponent        string
	prUpsertCommit           string
)

var pullRequestCmd = &cobra.Command{
	Use:   "pullrequest",
	Short: "Manage ReARM PullRequest entities",
	Long:  `Subcommand group for managing first-class PullRequest entities in ReARM.`,
}

var pullRequestUpsertCmd = &cobra.Command{
	Use:   "upsert",
	Short: "Idempotent upsert of a PullRequest, keyed by (target VCS, identity)",
	Long: `Registers (or refreshes) a PullRequest entity in ReARM. Designed to be called
unconditionally on every pull_request CI event so the PR row exists regardless
of whether a release is also being created on the same commit.

The PR is keyed on (targetVcsRepository, identity). Identity is opaque to
ReARM — GitHub PR numbers, GitLab MR iids and Gerrit change-ids all flow
through the same flag. Resolves the target VCS by --component (typical for
COMPONENT-typed API keys) or by --vcsuri / --repo-path (typical for
ORG-wide / FREEFORM keys).

When --commit is supplied and an SCE already exists for (targetVcs, commit)
the PR head is advanced to that SCE in the same call; otherwise the PR row
is registered without a head and the subsequent addrelease call (which
carries the same --pr-* flags) advances the head once the SCE is persisted.

Idempotent on (targetVcs, identity) — safe to call multiple times per CI
run (e.g. once per component in a monorepo).`,
	Run: func(cmd *cobra.Command, args []string) {
		if debug == "true" {
			fmt.Println("Using ReARM instance at", rearmUri)
		}

		input := map[string]interface{}{
			"identity": prUpsertIdentity,
			"state":    prUpsertState,
		}
		if prUpsertTitle != "" {
			input["title"] = prUpsertTitle
		}
		if prUpsertSourceBranchName != "" {
			input["sourceBranchName"] = prUpsertSourceBranchName
		}
		if prUpsertTargetBranchName != "" {
			input["targetBranchName"] = prUpsertTargetBranchName
		}
		if prUpsertEndpoint != "" {
			input["endpoint"] = prUpsertEndpoint
		}
		if prUpsertComponent != "" {
			input["component"] = prUpsertComponent
		}
		if prUpsertVcsUri != "" {
			input["vcsUri"] = prUpsertVcsUri
		}
		if prUpsertRepoPath != "" {
			input["repoPath"] = prUpsertRepoPath
		}
		if prUpsertVcsDisplayName != "" {
			input["vcsDisplayName"] = prUpsertVcsDisplayName
		}
		if prUpsertCommit != "" {
			input["commit"] = prUpsertCommit
		}
		if prUpsertComponent == "" && prUpsertVcsUri == "" {
			fmt.Fprintln(os.Stderr, "Either --component or --vcsuri is required")
			os.Exit(1)
		}

		if debug == "true" {
			jsonBody, _ := json.Marshal(input)
			fmt.Println("Request input =", string(jsonBody))
		}

		query := `
			mutation upsertPullRequestProgrammatic($input: PullRequestUpsertProgrammaticInput!) {
				upsertPullRequestProgrammatic(input: $input) {
					uuid
					identity
					state
					title
					targetVcsRepository
					commits
				}
			}
		`
		variables := map[string]interface{}{"input": input}
		fmt.Println(sendRequest(query, variables, "upsertPullRequestProgrammatic"))
	},
}

func init() {
	pullRequestUpsertCmd.PersistentFlags().StringVar(&prUpsertIdentity, "identity", "", "SCM-side PR identity (string). GitHub PR number, GitLab MR iid, Gerrit change-id (required)")
	pullRequestUpsertCmd.PersistentFlags().StringVar(&prUpsertState, "state", "", "PR state — OPEN | CLOSED | MERGED (required)")
	pullRequestUpsertCmd.PersistentFlags().StringVar(&prUpsertTitle, "title", "", "(Optional) PR title")
	pullRequestUpsertCmd.PersistentFlags().StringVar(&prUpsertSourceBranchName, "source-branch-name", "", "(Optional) Source branch name (the branch the PR is being merged from)")
	pullRequestUpsertCmd.PersistentFlags().StringVar(&prUpsertTargetBranchName, "target-branch-name", "", "(Optional) Target branch name (the branch the PR is being merged into)")
	pullRequestUpsertCmd.PersistentFlags().StringVar(&prUpsertEndpoint, "endpoint", "", "(Optional) URL of the PR in the upstream SCM")
	pullRequestUpsertCmd.PersistentFlags().StringVar(&prUpsertComponent, "component", "", "Component UUID (use with COMPONENT-typed API keys; mutually exclusive with --vcsuri)")
	pullRequestUpsertCmd.PersistentFlags().StringVar(&prUpsertVcsUri, "vcsuri", "", "VCS repository URI (use with ORG/FREEFORM API keys; mutually exclusive with --component)")
	pullRequestUpsertCmd.PersistentFlags().StringVar(&prUpsertRepoPath, "repo-path", "", "(Optional) Repository path for monorepo components")
	pullRequestUpsertCmd.PersistentFlags().StringVar(&prUpsertVcsDisplayName, "vcs-display-name", "", "(Optional) Display name for VCS repository (used when auto-creating)")
	pullRequestUpsertCmd.PersistentFlags().StringVar(&prUpsertCommit, "commit", "", "(Optional) Commit SHA. When set and the SCE for (targetVcs, commit) already exists, the PR head is advanced to that SCE")
	pullRequestUpsertCmd.MarkPersistentFlagRequired("identity")
	pullRequestUpsertCmd.MarkPersistentFlagRequired("state")

	pullRequestCmd.AddCommand(pullRequestUpsertCmd)
	rootCmd.AddCommand(pullRequestCmd)
}
