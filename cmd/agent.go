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

// AI-Agent commands. The full design lives in
// rearm-core/backend/ai-plans/agentic/README.md §9. Locked CLI shape:
//
//   rearm agent session init  --agent-name <name> --agent-model <model>
//                              [--agent-vendor <v>] [--agent-model-version <v>]
//                              [--client-session-id <id>] [--title <t>] [--branch <b>]
//   rearm agent session touch <session-uuid>
//   rearm agent session close <session-uuid>
//
// Not in this PR (follow-up): spawn, submit-metadata, attach-artifact,
// list, model attach.

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "AI Agent commands (coding agents — Claude Code, Cursor, Codex, …)",
	Long:  `Commands for managing AI coding agents and their sessions. See https://github.com/relizaio/rearm-saas/blob/main/backend/ai-plans/agentic/README.md for the full design.`,
}

var agentSessionCmd = &cobra.Command{
	Use:   "session",
	Short: "AI agent session lifecycle (init / touch / close)",
	Long:  `Sub-commands managing a single agent session — the working window in which an agent produces artifacts, commits, and pull requests.`,
}

// session init flags
var (
	agentName         string
	agentVendor       string
	agentModel        string
	agentModelVersion string
	agentIconKind     string
	agentColor        string
	clientSessionId   string
	sessionTitle      string
	sessionBranch     string
)

var agentSessionInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Open a new session for an AI agent (auto-registers the agent on first call)",
	Long: `Opens a new agent session via sessionInitializeProgrammatic. The calling
FREEFORM API key is bound to the named agent on first use; subsequent
calls with the same --agent-name resolve to the same agent row.

The session's clientSessionId is what the commit trailer
(ReARM-Agentic-Session:) references later; if --client-session-id is
omitted, the server defaults it to the new row's uuid. Calling init
twice with the same --client-session-id on an OPEN session is
idempotent — the existing session is returned.`,
	Run: func(cmd *cobra.Command, args []string) {
		query := `
			mutation ($input: SessionInitializeInput!) {
				sessionInitializeProgrammatic(input: $input) {
					uuid
					agent
					clientSessionId
					status
					title
					branch
					startedAt
				}
			}
		`
		input := map[string]interface{}{
			"agentName": agentName,
		}
		if agentModel != "" {
			input["agentModel"] = agentModel
		}
		if agentModelVersion != "" {
			input["agentModelVersion"] = agentModelVersion
		}
		if agentVendor != "" {
			input["agentVendor"] = agentVendor
		}
		if agentIconKind != "" {
			input["agentIconKind"] = agentIconKind
		}
		if agentColor != "" {
			input["agentColor"] = agentColor
		}
		if clientSessionId != "" {
			input["clientSessionId"] = clientSessionId
		}
		if sessionTitle != "" {
			input["title"] = sessionTitle
		}
		if sessionBranch != "" {
			input["branch"] = sessionBranch
		}
		variables := map[string]interface{}{"input": input}
		data, err := sendGraphQLRequest(query, variables, rearmUri+"/graphql")
		if err != nil {
			printGqlError(err)
			os.Exit(1)
		}
		emitJson(data["sessionInitializeProgrammatic"])
	},
}

var agentSessionTouchCmd = &cobra.Command{
	Use:   "touch <session-uuid>",
	Short: "Heartbeat — bump lastActivityAt on the session so the dashboard stays honest",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		query := `
			mutation ($uuid: ID!) {
				sessionTouchProgrammatic(uuid: $uuid) {
					uuid
					status
					lastActivityAt
				}
			}
		`
		variables := map[string]interface{}{"uuid": args[0]}
		data, err := sendGraphQLRequest(query, variables, rearmUri+"/graphql")
		if err != nil {
			printGqlError(err)
			os.Exit(1)
		}
		emitJson(data["sessionTouchProgrammatic"])
	},
}

var agentSessionCloseCmd = &cobra.Command{
	Use:   "close <session-uuid>",
	Short: "Close the session (terminal — re-init creates a new row)",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		query := `
			mutation ($uuid: ID!) {
				sessionCloseProgrammatic(uuid: $uuid) {
					uuid
					status
					closedAt
				}
			}
		`
		variables := map[string]interface{}{"uuid": args[0]}
		data, err := sendGraphQLRequest(query, variables, rearmUri+"/graphql")
		if err != nil {
			printGqlError(err)
			os.Exit(1)
		}
		emitJson(data["sessionCloseProgrammatic"])
	},
}

var attachArtifactUuid string

var agentSessionAttachArtifactCmd = &cobra.Command{
	Use:   "attach-artifact <session-uuid>",
	Short: "Attach an existing artifact (e.g. an AGENTIC_REPORT) to the session",
	Long: `Bind a previously-created artifact to a session via the agentic
write path. The artifact must already exist — use ` + "`rearm addrelease`" + ` or
` + "`rearm addartifact`" + ` to upload it first (with the right type / tags
for the policies in effect, e.g. type=AGENTIC_REPORT + tag
agenticPhase=ORIENTATION). See docs/agentic.md §5 for the full flow.

Idempotent: re-attaching the same artifact uuid is a no-op.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if attachArtifactUuid == "" {
			fmt.Fprintln(os.Stderr, "--artifact-uuid is required")
			os.Exit(1)
		}
		query := `
			mutation ($input: SessionAttachArtifactInput!) {
				sessionAttachArtifactProgrammatic(input: $input) {
					uuid
					status
					artifacts
				}
			}
		`
		variables := map[string]interface{}{
			"input": map[string]interface{}{
				"sessionUuid":  args[0],
				"artifactUuid": attachArtifactUuid,
			},
		}
		data, err := sendGraphQLRequest(query, variables, rearmUri+"/graphql")
		if err != nil {
			printGqlError(err)
			os.Exit(1)
		}
		emitJson(data["sessionAttachArtifactProgrammatic"])
	},
}

func init() {
	// init flags
	agentSessionInitCmd.PersistentFlags().StringVar(&agentName, "agent-name", "", "Display name of the agent (e.g. \"Claude Code\") — required")
	agentSessionInitCmd.PersistentFlags().StringVar(&agentModel, "agent-model", "", "Model the agent runs (e.g. \"claude-sonnet\") — required")
	agentSessionInitCmd.PersistentFlags().StringVar(&agentModelVersion, "agent-model-version", "", "Model version (e.g. \"4.5\") — optional, defaults to \"unknown\"")
	agentSessionInitCmd.PersistentFlags().StringVar(&agentVendor, "agent-vendor", "", "Publisher / vendor (e.g. \"Anthropic\") — optional")
	agentSessionInitCmd.PersistentFlags().StringVar(&agentIconKind, "agent-icon", "", "Dashboard glyph for the agent — optional")
	agentSessionInitCmd.PersistentFlags().StringVar(&agentColor, "agent-color", "", "Dashboard accent colour (CSS hex) — optional")
	agentSessionInitCmd.PersistentFlags().StringVar(&clientSessionId, "client-session-id", "", "Agent-supplied session id; defaults to the new row uuid")
	agentSessionInitCmd.PersistentFlags().StringVar(&sessionTitle, "title", "", "Human-readable session title")
	agentSessionInitCmd.PersistentFlags().StringVar(&sessionBranch, "branch", "", "Working branch the agent is on")
	_ = agentSessionInitCmd.MarkPersistentFlagRequired("agent-name")
	_ = agentSessionInitCmd.MarkPersistentFlagRequired("agent-model")

	agentSessionAttachArtifactCmd.PersistentFlags().StringVar(&attachArtifactUuid, "artifact-uuid", "", "UUID of an existing artifact to attach — required")
	_ = agentSessionAttachArtifactCmd.MarkPersistentFlagRequired("artifact-uuid")

	agentSessionCmd.AddCommand(agentSessionInitCmd)
	agentSessionCmd.AddCommand(agentSessionTouchCmd)
	agentSessionCmd.AddCommand(agentSessionCloseCmd)
	agentSessionCmd.AddCommand(agentSessionAttachArtifactCmd)
	agentCmd.AddCommand(agentSessionCmd)
	rootCmd.AddCommand(agentCmd)
}

func emitJson(v interface{}) {
	out, _ := json.Marshal(v)
	fmt.Println(string(out))
}
