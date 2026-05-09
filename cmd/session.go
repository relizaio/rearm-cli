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

// `rearm session` — agentic session lifecycle for FREEFORM keys nominated
// as an agentic identity. Implements the contract described in the
// rearm-saas /agentic branch (see backend AgenticSessionService /
// AgenticSessionDataFetcher). The CLI is intentionally thin: every
// subcommand is one GraphQL round-trip, no client-side state caching.

var (
	sessionMetadataPath string
	sessionUuidArg      string
)

var sessionCmd = &cobra.Command{
	Use:   "session",
	Short: "Manage ReARM agentic sessions (FREEFORM key with IdentityType set)",
	Long: `Subcommand group for the AgenticSession lifecycle. The FREEFORM API key
authenticating these calls must have an IdentityType nominated on it
(CODING_AGENT / CI_AGENT / CD_AGENT) — otherwise the server rejects with
"FREEFORM key is not nominated as an agentic identity".`,
}

var sessionInitializeCmd = &cobra.Command{
	Use:   "initialize",
	Short: "Open a new agentic session, superseding any prior active one on the same key",
	Long: `Opens a fresh AgenticSession owned by the FREEFORM key authenticating the
call. Any prior INITIALIZING/ACTIVE session on the same key is closed
(state CLOSED) before the new row is inserted — at most one active
session per key is the invariant.

The new session is returned in INITIALIZING state. Submit metadata via
"rearm session submit-metadata --metadata-file <path>" to flip it to
ACTIVE.`,
	Run: func(cmd *cobra.Command, args []string) {
		query := `
			mutation initializeAgenticSession {
				initializeAgenticSession {
					uuid
					state
					createdDate
					lastActivityDate
				}
			}
		`
		fmt.Println(sendRequest(query, map[string]interface{}{}, "initializeAgenticSession"))
	},
}

var sessionStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show the current active session for the authenticating FREEFORM key",
	Long: `Read-only poll. Returns null when the key has no active session. Does NOT
bump the inactivity clock — the server treats reads as non-activity by
design, so a polling agent can't accidentally keep a dead session
ACTIVE forever.`,
	Run: func(cmd *cobra.Command, args []string) {
		query := `
			query myAgenticSession {
				myAgenticSession {
					uuid
					state
					createdDate
					lastActivityDate
					closedDate
					taintReason
					agentMetadata
				}
			}
		`
		fmt.Println(sendRequest(query, map[string]interface{}{}, "myAgenticSession"))
	},
}

var sessionSubmitMetadataCmd = &cobra.Command{
	Use:   "submit-metadata",
	Short: "Attach the agent's metadata blob and flip the session to ACTIVE",
	Long: `Reads --metadata-file (JSON) and POSTs it to submitAgentMetadata. The
session moves from INITIALIZING to ACTIVE on success. lastActivityDate
is bumped.

The org's required-fields policy (PR 4 once landed) gates the shape of
the metadata; today the call always succeeds.`,
	Run: func(cmd *cobra.Command, args []string) {
		if sessionUuidArg == "" {
			fmt.Println("Error: --session is required")
			os.Exit(1)
		}
		if sessionMetadataPath == "" {
			fmt.Println("Error: --metadata-file is required")
			os.Exit(1)
		}
		raw, err := os.ReadFile(sessionMetadataPath)
		if err != nil {
			fmt.Printf("Error reading %s: %v\n", sessionMetadataPath, err)
			os.Exit(1)
		}
		var metadata interface{}
		if err := json.Unmarshal(raw, &metadata); err != nil {
			fmt.Printf("Error parsing %s as JSON: %v\n", sessionMetadataPath, err)
			os.Exit(1)
		}
		query := `
			mutation submitAgentMetadata($sessionUuid: ID!, $metadata: Object!) {
				submitAgentMetadata(sessionUuid: $sessionUuid, metadata: $metadata) {
					uuid
					state
					lastActivityDate
				}
			}
		`
		variables := map[string]interface{}{
			"sessionUuid": sessionUuidArg,
			"metadata":    metadata,
		}
		fmt.Println(sendRequest(query, variables, "submitAgentMetadata"))
	},
}

var sessionCloseCmd = &cobra.Command{
	Use:   "close",
	Short: "Explicitly close an agentic session (state CLOSED)",
	Long: `No-op when the session is already terminal. Either the agent (FREEFORM
key) or an org admin (JWT) can close. Idempotent.`,
	Run: func(cmd *cobra.Command, args []string) {
		if sessionUuidArg == "" {
			fmt.Println("Error: --session is required")
			os.Exit(1)
		}
		query := `
			mutation closeAgenticSession($sessionUuid: ID!) {
				closeAgenticSession(sessionUuid: $sessionUuid) {
					uuid
					state
					closedDate
				}
			}
		`
		variables := map[string]interface{}{"sessionUuid": sessionUuidArg}
		fmt.Println(sendRequest(query, variables, "closeAgenticSession"))
	},
}

func init() {
	sessionSubmitMetadataCmd.PersistentFlags().StringVar(&sessionUuidArg, "session", "", "Session UUID returned by `rearm session initialize` (required)")
	sessionSubmitMetadataCmd.PersistentFlags().StringVar(&sessionMetadataPath, "metadata-file", "", "Path to a JSON file with the agent's metadata payload (required)")
	sessionSubmitMetadataCmd.MarkPersistentFlagRequired("session")
	sessionSubmitMetadataCmd.MarkPersistentFlagRequired("metadata-file")

	sessionCloseCmd.PersistentFlags().StringVar(&sessionUuidArg, "session", "", "Session UUID to close (required)")
	sessionCloseCmd.MarkPersistentFlagRequired("session")

	sessionCmd.AddCommand(sessionInitializeCmd)
	sessionCmd.AddCommand(sessionStatusCmd)
	sessionCmd.AddCommand(sessionSubmitMetadataCmd)
	sessionCmd.AddCommand(sessionCloseCmd)
	rootCmd.AddCommand(sessionCmd)
}
