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
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-resty/resty/v2"
	"github.com/spf13/cobra"
)

// AI-Agent commands. The full design lives in
// rearm-core/backend/ai-plans/agentic/README.md §9. Locked CLI shape:
//
//   rearm agent session init  --agent-name <name> --agent-model <model>
//                              [--agent-vendor <v>] [--agent-model-version <v>]
//                              [--client-session-id <id>] [--title <t>]
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
			mutation ($sessionInit: SessionInitializeInput!) {
				sessionInitializeProgrammatic(sessionInit: $sessionInit) {
					uuid
					agent
					clientSessionId
					status
					title
					startedAt
					policyEvents {
						policyName
						kind
						state
						severity
						message
						evaluatedAt
						policy { uuid name cel description enabled severity kind }
					}
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
		variables := map[string]interface{}{"sessionInit": input}
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
			mutation ($sessionUuid: ID!) {
				sessionTouchProgrammatic(sessionUuid: $sessionUuid) {
					uuid
					status
					lastActivityAt
				}
			}
		`
		variables := map[string]interface{}{"sessionUuid": args[0]}
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
			mutation ($sessionUuid: ID!) {
				sessionCloseProgrammatic(sessionUuid: $sessionUuid) {
					uuid
					status
					closedAt
				}
			}
		`
		variables := map[string]interface{}{"sessionUuid": args[0]}
		data, err := sendGraphQLRequest(query, variables, rearmUri+"/graphql")
		if err != nil {
			printGqlError(err)
			os.Exit(1)
		}
		emitJson(data["sessionCloseProgrammatic"])
	},
}

var agentSessionShowCmd = &cobra.Command{
	Use:   "show <session-uuid>",
	Short: "Show full session state — status, artifacts, policy verdicts, releases, PRs",
	Long: `Returns the full Session shape including policyEvents (with the
embedded AgentPolicy snapshot for each verdict so the calling agent
can decide if a FAILED / PENDING policy is recoverable on its own).

Useful for the "what should I do now?" decision at startup and after
each inbox event — the inbox tells you what changed, this tells you
the current full state.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		query := `
			query ($sessionUuid: ID!) {
				sessionProgrammatic(sessionUuid: $sessionUuid) {
					uuid clientSessionId status startedAt closedAt lastActivityAt
					agent title parentSession
					artifacts
					commits
					policyEvents {
						policyName kind state severity message evaluatedAt
						policy { uuid name cel description enabled severity kind }
					}
					releases { uuid version lifecycle }
					pullRequests { uuid identity title state }
				}
			}
		`
		variables := map[string]interface{}{"sessionUuid": args[0]}
		data, err := sendGraphQLRequest(query, variables, rearmUri+"/graphql")
		if err != nil {
			printGqlError(err)
			os.Exit(1)
		}
		emitJson(data["sessionProgrammatic"])
	},
}

var agentReleaseCmd = &cobra.Command{
	Use:   "release",
	Short: "Release helpers for AI agents (read-only inspection by uuid)",
	Long:  `Read-side queries an agent needs after seeing an inbox event pointing at a release. Mutations on releases continue to go through ` + "`rearm addrelease`" + ` / ` + "`rearm approverelease`" + `.`,
}

var (
	releaseShowSessionUuid     string
	releaseShowClientSessionId string
)

var agentReleaseShowCmd = &cobra.Command{
	Use:   "show <release-uuid>",
	Short: "Show a release attributed to your session — lifecycle, update events, approval events",
	Long: `Looks up a release by uuid and returns the shape an agent typically
needs after seeing a LIFECYCLE_CHANGE or APPROVAL inbox event:

  - updateEvents[].message — the human-readable reason a CEL gate
    flipped lifecycle ("Triggered by '...' (CEL: ...)").
  - approvalEvents[] — full approval history with reviewer comments.
  - sourceCodeEntryDetails — per-commit attribution + signature state.

The release must be attributed to YOUR session (one of the session's
commits must trace through to this release). Pass either --session
(the session row uuid) or --client-session-id (the agent-chosen id
from the commit trailer). The backend verifies the calling key owns
the session before returning anything.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if releaseShowSessionUuid == "" && releaseShowClientSessionId == "" {
			fmt.Fprintln(os.Stderr, "--session or --client-session-id is required")
			os.Exit(1)
		}
		query := `
			query ($releaseUuid: ID!, $sessionUuid: ID, $clientSessionId: String) {
				agenticReleaseProgrammatic(releaseUuid: $releaseUuid, sessionUuid: $sessionUuid, clientSessionId: $clientSessionId) {
					uuid version lifecycle
					updateEvents { rus rua oldValue newValue message date }
					approvalEvents { approvalEntry approvalRoleId state comment date }
					sourceCodeEntryDetails { uuid commit attributionState attributionReason }
				}
			}
		`
		variables := map[string]interface{}{"releaseUuid": args[0]}
		if releaseShowSessionUuid != "" {
			variables["sessionUuid"] = releaseShowSessionUuid
		}
		if releaseShowClientSessionId != "" {
			variables["clientSessionId"] = releaseShowClientSessionId
		}
		data, err := sendGraphQLRequest(query, variables, rearmUri+"/graphql")
		if err != nil {
			printGqlError(err)
			os.Exit(1)
		}
		emitJson(data["agenticReleaseProgrammatic"])
	},
}

// session inbox flags
var (
	inboxSince string
	inboxKinds []string
	inboxLimit int
)

var agentSessionInboxCmd = &cobra.Command{
	Use:   "inbox <session-uuid>",
	Short: "Poll the agent's inbox — release lifecycle / approval / policy events scoped to this session",
	Long: `Returns events the agent should react to: release lifecycle moves
(LIFECYCLE_CHANGE — e.g. REJECTED by a CEL gate, ASSEMBLED), approval
verdicts on releases minted from this session's commits
(APPROVAL — DISAPPROVED with reviewer comment is the canonical fix-loop
trigger), and policy verdicts landing on the session itself
(POLICY_VERDICT — orientation-artifact / commit-attribution re-eval).

Use --since with the cursor from the most recent event you already
processed to fetch strictly newer events. Default limit is 50, capped
at 200. Filter to specific event kinds with --kind (repeatable).

Pair with a sleep loop on the agent side — 30-60s between polls is the
recommended cadence. See $REARM_URL/api/agents/orientation.md.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		query := `
			query ($inboxRequest: AgentSessionInboxInput!) {
				agentSessionInboxProgrammatic(inboxRequest: $inboxRequest) {
					cursor
					occurredAt
					kind
					release { uuid version lifecycle }
					oldValue
					newValue
					reason
					source
					actorUuid
					actorRoleId
				}
			}
		`
		inboxRequest := map[string]interface{}{"sessionUuid": args[0]}
		if inboxSince != "" {
			inboxRequest["since"] = inboxSince
		}
		if len(inboxKinds) > 0 {
			inboxRequest["kinds"] = inboxKinds
		}
		if inboxLimit > 0 {
			inboxRequest["limit"] = inboxLimit
		}
		variables := map[string]interface{}{"inboxRequest": inboxRequest}
		data, err := sendGraphQLRequest(query, variables, rearmUri+"/graphql")
		if err != nil {
			printGqlError(err)
			os.Exit(1)
		}
		emitJson(data["agentSessionInboxProgrammatic"])
	},
}

// session add-artifact flags
var (
	addArtifactFile          string
	addArtifactType          string
	addArtifactDisplayId     string
	addArtifactTags          []string
	addArtifactDigests       []string
)

var agentSessionAddArtifactCmd = &cobra.Command{
	Use:   "add-artifact <session-uuid>",
	Short: "Upload an artifact and bind it to the session in one round-trip",
	Long: `Uploads a file to ReARM artifact storage and binds the resulting
artifact row directly to the session. The artifact lives only on the
session (belongsTo=AGENT_SESSION) — it does not appear on any release
or component. This is the only way to put artifacts onto a session;
attaching pre-existing release / sce artifacts is intentionally not
supported (artifacts that originate elsewhere don't belong here).

The canonical AGENTIC_REPORT case:

  rearm agent session add-artifact <session-uuid> \
    --file ./orientation.json \
    --type AGENTIC_REPORT \
    --display-id orient \
    --tag agenticPhase=ORIENTATION

--tag is repeatable. Tags are stored verbatim and surface to the
CEL session.* policy surface.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if addArtifactFile == "" {
			fmt.Fprintln(os.Stderr, "--file is required")
			os.Exit(1)
		}
		if addArtifactType == "" {
			fmt.Fprintln(os.Stderr, "--type is required (e.g. AGENTIC_REPORT)")
			os.Exit(1)
		}
		fileBytes, err := os.ReadFile(addArtifactFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to read --file %s: %v\n", addArtifactFile, err)
			os.Exit(1)
		}
		fileName := filepath.Base(addArtifactFile)

		// Parse --tag k=v pairs into [{key, value}] for ArtifactInput.tags.
		var tags []map[string]interface{}
		for _, t := range addArtifactTags {
			eq := strings.Index(t, "=")
			if eq <= 0 {
				fmt.Fprintf(os.Stderr, "Invalid --tag %q — expected key=value\n", t)
				os.Exit(1)
			}
			tags = append(tags, map[string]interface{}{
				"key":   t[:eq],
				"value": t[eq+1:],
			})
		}

		// Build the single ArtifactInput. file:null is the placeholder
		// that the multipart map[] rewrites to the uploaded part.
		art := map[string]interface{}{
			"type":              addArtifactType,
			"storedIn":          "REARM",
			"file":              nil,
		}
		if addArtifactDisplayId != "" {
			art["displayIdentifier"] = addArtifactDisplayId
		} else {
			art["displayIdentifier"] = fileName
		}
		if len(tags) > 0 {
			art["tags"] = tags
		}
		if len(addArtifactDigests) > 0 {
			art["digestRecords"] = addArtifactDigests
		}

		mutation := `
			mutation SessionAddArtifact($addArtifact: SessionAddArtifactInput!) {
				sessionAddArtifactProgrammatic(addArtifact: $addArtifact) {
					uuid
					status
					artifacts
					policyEvents { policyName state severity message evaluatedAt }
				}
			}
		`
		variables := map[string]interface{}{
			"addArtifact": map[string]interface{}{
				"sessionUuid": args[0],
				"artifacts":   []map[string]interface{}{art},
			},
		}

		// Apollo multipart spec: operations + map + files keyed "0", "1", ...
		operations := map[string]interface{}{
			"operationName": "SessionAddArtifact",
			"query":         mutation,
			"variables":     variables,
		}
		opsJson, _ := json.Marshal(operations)
		// Apollo spec: map value is an array of dot-separated paths.
		locationMap := map[string][]string{
			"0": {"variables.addArtifact.artifacts.0.file"},
		}
		mapJson, _ := json.Marshal(locationMap)

		if debug == "true" {
			fmt.Println("GraphQL operations:", string(opsJson))
			fmt.Println("Multipart map:", string(mapJson))
		}

		// Standard CLI multipart pattern: applySessionToRestyClient
		// fetches the CSRF token + cookie pair and sets both on the
		// client; resty propagates them onto the multipart POST. Then
		// add the Basic-auth header and the Apollo-Require-Preflight
		// signal Spring needs for multipart-upload validation.
		client := resty.New()
		applySessionToRestyClient(client)
		if len(apiKeyId) > 0 && len(apiKey) > 0 {
			auth := base64.StdEncoding.EncodeToString([]byte(apiKeyId + ":" + apiKey))
			client.SetHeader("Authorization", "Basic "+auth)
		}
		resp, err := client.R().
			SetFileReader("0", fileName, bytes.NewReader(fileBytes)).
			SetHeader("Content-Type", "multipart/form-data").
			SetHeader("User-Agent", "ReARM CLI").
			SetHeader("Accept-Encoding", "gzip, deflate").
			SetHeader("Apollo-Require-Preflight", "true").
			SetMultipartFormData(map[string]string{
				"operations": string(opsJson),
				"map":        string(mapJson),
			}).
			SetBasicAuth(apiKeyId, apiKey).
			Post(rearmUri + "/graphql")
		handleResponse(err, resp)
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
	_ = agentSessionInitCmd.MarkPersistentFlagRequired("agent-name")
	_ = agentSessionInitCmd.MarkPersistentFlagRequired("agent-model")

	// session add-artifact flags
	agentSessionAddArtifactCmd.PersistentFlags().StringVar(&addArtifactFile, "file", "", "Local file path to upload (required)")
	agentSessionAddArtifactCmd.PersistentFlags().StringVar(&addArtifactType, "type", "", "ArtifactType enum (e.g. AGENTIC_REPORT) — required")
	agentSessionAddArtifactCmd.PersistentFlags().StringVar(&addArtifactDisplayId, "display-id", "", "Display identifier; defaults to the file basename")
	agentSessionAddArtifactCmd.PersistentFlags().StringSliceVar(&addArtifactTags, "tag", nil, "Tag in key=value form — repeatable (e.g. --tag agenticPhase=ORIENTATION)")
	agentSessionAddArtifactCmd.PersistentFlags().StringSliceVar(&addArtifactDigests, "digest", nil, "Pre-computed digest record(s) — optional, server auto-computes when omitted")
	_ = agentSessionAddArtifactCmd.MarkPersistentFlagRequired("file")
	_ = agentSessionAddArtifactCmd.MarkPersistentFlagRequired("type")

	// inbox flags
	agentSessionInboxCmd.PersistentFlags().StringVar(&inboxSince, "since", "", "Cursor from a prior poll — fetch events strictly after this cursor")
	agentSessionInboxCmd.PersistentFlags().StringSliceVar(&inboxKinds, "kind", nil, "Filter by event kind (LIFECYCLE_CHANGE / APPROVAL / POLICY_VERDICT) — repeatable")
	agentSessionInboxCmd.PersistentFlags().IntVar(&inboxLimit, "limit", 0, "Max events to return (default 50, server-capped at 200)")

	// release show flags
	agentReleaseShowCmd.PersistentFlags().StringVar(&releaseShowSessionUuid, "session", "", "Session row uuid (required when --client-session-id is not provided)")
	agentReleaseShowCmd.PersistentFlags().StringVar(&releaseShowClientSessionId, "client-session-id", "", "Agent-chosen session id from the commit trailer (alternative to --session)")

	agentSessionCmd.AddCommand(agentSessionInitCmd)
	agentSessionCmd.AddCommand(agentSessionTouchCmd)
	agentSessionCmd.AddCommand(agentSessionCloseCmd)
	agentSessionCmd.AddCommand(agentSessionAddArtifactCmd)
	agentSessionCmd.AddCommand(agentSessionInboxCmd)
	agentSessionCmd.AddCommand(agentSessionShowCmd)
	agentReleaseCmd.AddCommand(agentReleaseShowCmd)
	agentCmd.AddCommand(agentSessionCmd)
	agentCmd.AddCommand(agentReleaseCmd)
	rootCmd.AddCommand(agentCmd)
}

func emitJson(v interface{}) {
	out, _ := json.Marshal(v)
	fmt.Println(string(out))
}
