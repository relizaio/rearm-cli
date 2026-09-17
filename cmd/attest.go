/*
Copyright Reliza Incorporated. 2019 - 2026. Licensed under the terms of Apache-2.0.
*/

package cmd

import (
	"fmt"
	"os"
	"strings"

	rearm "github.com/relizaio/rearm-client-go"
	"github.com/spf13/cobra"
)

// `rearm attest` is the command a lock refusal tells you to run.
//
// A commit ReARM cannot hold anybody to -- unsigned, unattributed and unclaimed -- is what an
// integrity rule refuses a build over, and what a lock waits on. Claiming one says "this is
// mine". It does not certify the content, it does not move any release's lifecycle and it is not
// a review: a release the rule rejected stays rejected. What changes is that somebody is
// accountable for the commit, so the next build's rule passes and any lock waiting only on it
// can be released -- automatically, when the lock was raised to allow that.
//
// Disowning one (--verdict NOT_MINE) does the opposite: it leaves the commit unaccounted for and
// puts every lock it is a cause of into an administrator's hands.

var (
	attestCommit   string
	attestVerdict  string
	attestNote     string
	attestSession  string
	lockReleaseId  string
	lockReleaseWhy string
)

var attestCmd = &cobra.Command{
	Use:   "attest",
	Short: "Claim or disown a commit, so somebody is accountable for it",
	Long: `Records an attestation about a commit.

  rearm attest --commit <sha> --verdict MINE --note 'malformed trailer, corrected in <sha>'

MINE makes the commit accountable: a rule that refuses unrecognized commits passes on the next
build, and a lock waiting only on this commit can be released. It certifies nothing about the
content and changes no release's lifecycle -- a rejected release stays rejected. Whether the
claim means "fixed later" or "fine as it is" belongs in the note.

NOT_MINE disowns it. The commit stays unaccounted for and any lock it is a cause of becomes an
administrator's decision.

The commit is named the way getversion and addrelease name a component -- by --component, or by
--vcsuri plus --repopath -- and resolved through that component's repository. A sha ReARM has
never seen is refused rather than recorded.

Requires an open agent session (rearm agent session init), because an attestation records who
stood behind the claim and an API key is not a who.`,
	Run: func(cmd *cobra.Command, args []string) {
		if attestCommit == "" {
			fmt.Fprintln(os.Stderr, "--commit is required")
			os.Exit(1)
		}
		if attestSession == "" {
			fmt.Fprintln(os.Stderr, "--session is required: run 'rearm agent session init' first, "+
				"or pass the uuid of the session this work is happening under")
			os.Exit(1)
		}
		verdict := strings.ToUpper(attestVerdict)
		if verdict != "MINE" && verdict != "NOT_MINE" {
			fmt.Fprintln(os.Stderr, "--verdict must be MINE or NOT_MINE")
			os.Exit(1)
		}
		if component == "" && vcsUri == "" {
			fmt.Fprintln(os.Stderr, "name the component with --component, or with --vcsuri and --repopath")
			os.Exit(1)
		}
		componentRef := map[string]interface{}{}
		if component != "" {
			componentRef["component"] = component
		}
		if vcsUri != "" {
			componentRef["vcsUri"] = vcsUri
			if repoPath != "" {
				componentRef["repoPath"] = repoPath
			}
		}
		variables := map[string]interface{}{
			"componentRef": componentRef,
			"commit":       attestCommit,
			"verdict":      verdict,
			"session":      attestSession,
		}
		if attestNote != "" {
			variables["note"] = attestNote
		}
		data, err := sendGraphQLRequest(rearm.AttestProgrammatic_Operation, variables)
		if err != nil {
			printGqlError(err)
			os.Exit(1)
		}
		emitJson(data["attestProgrammatic"])
	},
}

var lockCmd = &cobra.Command{
	Use:   "lock",
	Short: "Locks that are stopping builds",
}

var lockReleaseCmd = &cobra.Command{
	Use:   "release",
	Short: "Release a lock this key is allowed to release",
	Long: `Releases a lock whose level is AGENT and whose causes are all claimed.

Most locks release themselves the moment their last cause is claimed, so this is for the ones
that do not -- and a lock that needs a person, or that has escalated because a commit was
disowned or contested, refuses here and says which level it needs.

Every release, including this one, is recorded as an attestation.`,
	Run: func(cmd *cobra.Command, args []string) {
		if lockReleaseId == "" || lockReleaseWhy == "" {
			fmt.Fprintln(os.Stderr, "--lock and --reason are required")
			os.Exit(1)
		}
		variables := map[string]interface{}{
			"lockUuid": lockReleaseId,
			"reason":   lockReleaseWhy,
		}
		data, err := sendGraphQLRequest(rearm.ReleaseLockProgrammatic_Operation, variables)
		if err != nil {
			printGqlError(err)
			os.Exit(1)
		}
		emitJson(data["releaseLockProgrammatic"])
	},
}

func init() {
	attestCmd.PersistentFlags().StringVar(&attestCommit, "commit", "", "Commit sha to attest to (required)")
	attestCmd.PersistentFlags().StringVar(&attestVerdict, "verdict", "MINE", "MINE or NOT_MINE")
	attestCmd.PersistentFlags().StringVar(&attestNote, "note", "", "Why -- read by whoever looks at this later")
	attestCmd.PersistentFlags().StringVar(&attestSession, "session", "", "Agent session uuid this is filed under (required)")
	attestCmd.PersistentFlags().StringVar(&component, "component", "", "Component UUID the commit belongs to")
	attestCmd.PersistentFlags().StringVar(&vcsUri, "vcsuri", "", "VCS URI of the component, instead of --component")
	attestCmd.PersistentFlags().StringVar(&repoPath, "repopath", "", "Path within the VCS repository, with --vcsuri")
	rootCmd.AddCommand(attestCmd)

	lockReleaseCmd.PersistentFlags().StringVar(&lockReleaseId, "lock", "", "Lock UUID, as printed in the refusal (required)")
	lockReleaseCmd.PersistentFlags().StringVar(&lockReleaseWhy, "reason", "", "Why it may be released (required)")
	lockCmd.AddCommand(lockReleaseCmd)
	rootCmd.AddCommand(lockCmd)
}
