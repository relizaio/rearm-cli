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
	"path/filepath"
	"strings"

	rearm "github.com/relizaio/rearm-client-go"
	"github.com/spf13/cobra"
)

// AI-Agent commands. Locked CLI shape:
//
//   rearm agent session init  --agent-name <name> --agent-model <model>
//                              [--agent-vendor <v>] [--agent-model-version <v>]
//                              [--client-session-id <id>] [--title <t>]
//   rearm agent session touch <session-uuid>
//   rearm agent session close <session-uuid>
//
// The authoritative runtime contract is served at
// $REARM_URL/api/agents/orientation.md by every backend that supports
// these commands; agent runtimes should fetch it on first connection.

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "AI Agent commands (coding agents — Claude Code, Cursor, Codex, …)",
	Long:  `Commands for managing AI coding agents and their sessions. The authoritative runtime contract is served by the backend at $REARM_URL/api/agents/orientation.md — point your agent runtime at that URL on first connection.`,
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
	claudeSessionId   string
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
omitted, the server defaults it to the new row's uuid. A
--client-session-id is unique forever within the agent: init refuses
one already used by any session, OPEN, CLOSED or BLOCKED, and names
that session. To retry after a crash, keep using the session you have;
after a BLOCKED or CLOSED one, pick a fresh id.

Under Claude Code the session also records Claude Code's own session id
($CLAUDE_CODE_SESSION_ID), so it can be traced back to the conversation.
Pass --provider-remote-session-id for a hosted (bridge) session id,
--no-provider-session to opt out, or --require-provider-session to fail
when no id can be found.

The session also records how it was opened: the credential and, for a
CLI login, who approved it; the address the server saw; and what this
CLI reports about the machine -- hostname, OS, time zone and version.
Hostname and address are shown only to org admins and the session's
owner. --no-device-info stops the CLI reporting the machine.`,
	Run: func(cmd *cobra.Command, args []string) {
		// Resolved before anything is sent, so --require-provider-session refuses without
		// opening a session it would then have to explain.
		ps, err := resolveProviderSession(currentProviderSessionOpts())
		if err != nil {
			fmt.Fprintln(os.Stderr, "rearm:", err)
			os.Exit(1)
		}
		query := rearm.SessionInitializeProgrammatic_Operation
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
		if ps != nil {
			input["providerSession"] = ps
		}
		if device := sessionDeviceInput(noDeviceInfo); device != nil {
			input["device"] = device
		}
		variables := map[string]interface{}{"sessionInit": input}
		data, err := sendGraphQLRequest(query, variables)
		if err != nil {
			printGqlError(err)
			os.Exit(1)
		}
		session := data["sessionInitializeProgrammatic"]
		recordInitState(session)
		emitJson(session)
	},
}

// recordInitState writes the local state file the usage hooks read.
//
// Best-effort and never fatal: init's job is to open the session on the server, and that has
// already succeeded by the time we get here. Failing the command because a state file could not be
// written would turn a degraded feature into a broken one.
func recordInitState(session interface{}) {
	m, ok := session.(map[string]interface{})
	if !ok {
		return
	}
	uuid, _ := m["uuid"].(string)
	if uuid == "" {
		return
	}
	clientId, _ := m["clientSessionId"].(string)
	if clientId == "" {
		clientId = clientSessionId
	}
	if clientId == "" {
		// The server defaulted it to the row uuid.
		clientId = uuid
	}
	claudeId := claudeSessionId
	if claudeId == "" && providerSessionId != "" && (providerName == "" || providerName == claudeCodeProvider) {
		claudeId = providerSessionId
	}
	if claudeId == "" {
		// Claude Code exports its session id to what it runs, so an agent that did not pass the
		// flag still gets the mapping for free. The name was checked against a running instance
		// rather than assumed -- it is CLAUDE_CODE_SESSION_ID, and its value is exactly the
		// sessionId the transcript carries. An earlier guess of CLAUDE_SESSION_ID is unset in
		// practice, which would have left every hook unable to find its session.
		claudeId = firstNonEmptyEnv("CLAUDE_CODE_SESSION_ID", "CLAUDE_SESSION_ID")
	}
	st := &agentSessionState{
		SessionUuid:       uuid,
		ClientSessionId:   clientId,
		ExternalSessionId: claudeId,
	}
	if err := writeAgentState(st); err != nil {
		fmt.Fprintf(os.Stderr, "rearm: session opened, but local usage state could not be written: %v\n", err)
	}
}

var agentSessionTouchCmd = &cobra.Command{
	Use:   "touch <session-uuid>",
	Short: "Heartbeat — bump lastActivityAt on the session so the dashboard stays honest",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		query := rearm.SessionTouchProgrammatic_Operation
		variables := map[string]interface{}{"sessionUuid": args[0]}
		data, err := sendGraphQLRequest(query, variables)
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
		query := rearm.SessionCloseProgrammatic_Operation
		variables := map[string]interface{}{"sessionUuid": args[0]}
		data, err := sendGraphQLRequest(query, variables)
		if err != nil {
			printGqlError(err)
			os.Exit(1)
		}
		// The session is over; its local state is now just a stale mapping that a later Claude
		// session reusing the id would pick up. Removed after the close succeeds, never before.
		if st := findStateBySessionUuid(args[0]); st != nil {
			removeAgentState(st)
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
		query := rearm.SessionProgrammatic_Operation
		variables := map[string]interface{}{"sessionUuid": args[0]}
		data, err := sendGraphQLRequest(query, variables)
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
  - sourceCodeEntryDetails — per-commit attribution + signature state,
    plus its source-code SBOM artifacts (artifactDetails).
  - metrics — release-level (AGGREGATE) security posture: severity
    counts (critical/high/...), policy-violation totals, and the
    per-finding lists vulnerabilityDetails[] (purl, vulnId, severity,
    analysisState) and violationDetails[] (purl, type, license,
    analysisState). Populated from the latest scan (see lastScanned).
  - per-artifact metrics — every artifact carries its OWN metrics, so
    you can tell WHERE findings live: a clean source-code SBOM vs a
    vulnerable deliverable/container SBOM, plus SARIF / VDR results.
    Walk: sourceCodeEntryDetails.artifactDetails (source SBOMs),
    artifactDetails (release-level), and
    variantDetails[].outboundDeliverableDetails[].artifactDetails
    (deliverable SBOMs / scan results).
    NB: release-level metrics reflect release-scope vuln suppressions
    while per-artifact metrics are raw — the two detail lists can
    differ; that's expected.

The release must be attributed to YOUR session (one of the session's
commits must trace through to this release). Pass either --session
(the session row uuid) or --client-session-id (the agent-chosen id
from the commit trailer). The backend verifies the calling key owns
the session before returning anything — so a release your own session
built needs no extra permission. A release NOT attributed to your
session is only returned if the calling key has explicit RESOURCE read
permission on its component/product.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if releaseShowSessionUuid == "" && releaseShowClientSessionId == "" {
			fmt.Fprintln(os.Stderr, "--session or --client-session-id is required")
			os.Exit(1)
		}
		query := rearm.AgenticReleaseProgrammatic_Operation
		variables := map[string]interface{}{"releaseUuid": args[0]}
		if releaseShowSessionUuid != "" {
			variables["sessionUuid"] = releaseShowSessionUuid
		}
		if releaseShowClientSessionId != "" {
			variables["clientSessionId"] = releaseShowClientSessionId
		}
		data, err := sendGraphQLRequest(query, variables)
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
		query := rearm.AgentSessionInboxProgrammatic_Operation
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
		data, err := sendGraphQLRequest(query, variables)
		if err != nil {
			printGqlError(err)
			os.Exit(1)
		}
		emitJson(data["agentSessionInboxProgrammatic"])
	},
}

// session add-artifact flags
var (
	addArtifactFile      string
	addArtifactType      string
	addArtifactDisplayId string
	addArtifactTags      []string
	addArtifactDigests   []string
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
CEL session.* policy surface.

--digest is optional and repeatable: <algo>:<hex>[:<scope>], e.g.
--digest sha256:<64 hex chars>. The scope defaults to ORIGINAL_FILE (the file as
you had it); OCI_STORAGE and REARM may also be declared. ReARM computes the
digest of what it stores itself, so leave --digest out unless you have one to
declare.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if addArtifactFile == "" {
			fmt.Fprintln(os.Stderr, "--file is required")
			os.Exit(1)
		}
		// Parsed before the file is read: a bad digest should fail before anything is uploaded.
		digestRecords, err := parseDigestFlags(addArtifactDigests)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
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
			"type":     addArtifactType,
			"storedIn": "REARM",
			"file":     nil,
		}
		if addArtifactDisplayId != "" {
			art["displayIdentifier"] = addArtifactDisplayId
		} else {
			art["displayIdentifier"] = fileName
		}
		if len(tags) > 0 {
			art["tags"] = tags
		}
		if len(digestRecords) > 0 {
			// DigestRecordInput objects: the server refuses a bare string here.
			art["digestRecords"] = digestRecords
		}

		mutation := rearm.SessionAddArtifact_Operation
		variables := map[string]interface{}{
			"addArtifact": map[string]interface{}{
				"sessionUuid": args[0],
				"artifacts":   []map[string]interface{}{art},
			},
		}

		printGraphQLMultipart(mutation, variables,
			map[string][]string{"0": {"variables.addArtifact.artifacts.0.file"}},
			map[string]interface{}{"0": FileData{Bytes: fileBytes, Filename: fileName}})
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
	agentSessionInitCmd.PersistentFlags().StringVar(&claudeSessionId, "claude-session-id", "", "Deprecated alias for --provider-session-id with --provider claude-code (defaults to $CLAUDE_CODE_SESSION_ID)")
	agentSessionInitCmd.PersistentFlags().StringVar(&sessionTitle, "title", "", "Human-readable session title")
	_ = agentSessionInitCmd.MarkPersistentFlagRequired("agent-name")
	_ = agentSessionInitCmd.MarkPersistentFlagRequired("agent-model")

	// session add-artifact flags
	agentSessionAddArtifactCmd.PersistentFlags().StringVar(&addArtifactFile, "file", "", "Local file path to upload (required)")
	agentSessionAddArtifactCmd.PersistentFlags().StringVar(&addArtifactType, "type", "", "ArtifactType enum (e.g. AGENTIC_REPORT) — required")
	agentSessionAddArtifactCmd.PersistentFlags().StringVar(&addArtifactDisplayId, "display-id", "", "Display identifier; defaults to the file basename")
	agentSessionAddArtifactCmd.PersistentFlags().StringSliceVar(&addArtifactTags, "tag", nil, "Tag in key=value form — repeatable (e.g. --tag agenticPhase=ORIENTATION)")
	agentSessionAddArtifactCmd.PersistentFlags().StringArrayVar(&addArtifactDigests, "digest", nil, "Declared digest, <algo>:<hex>[:<scope>], e.g. sha256:<hex>; scope defaults to ORIGINAL_FILE (repeatable, optional)")
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
	agentSessionCmd.AddCommand(agentSessionUsageCmd)
	// Claude Code specifics live one level down, so `rearm agent claude ...` is clearly one
	// agent's integration rather than something every agent is expected to have.
	agentCmd.AddCommand(agentClaudeCmd)
	agentCmd.AddCommand(agentDocCmd)
	agentReleaseCmd.AddCommand(agentReleaseShowCmd)
	agentCmd.AddCommand(agentSessionCmd)
	agentCmd.AddCommand(agentReleaseCmd)
	rootCmd.AddCommand(agentCmd)
}

func emitJson(v interface{}) {
	out, _ := json.Marshal(v)
	fmt.Println(string(out))
}
