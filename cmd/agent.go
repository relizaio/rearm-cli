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
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"

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
			mutation ($input: SessionInitializeInput!) {
				sessionInitializeProgrammatic(input: $input) {
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
			query ($uuid: ID!) {
				sessionProgrammatic(uuid: $uuid) {
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
		variables := map[string]interface{}{"uuid": args[0]}
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
			query ($u: ID!, $s: ID, $c: String) {
				releaseProgrammatic(uuid: $u, sessionUuid: $s, clientSessionId: $c) {
					uuid version lifecycle
					updateEvents { rus rua oldValue newValue message date }
					approvalEvents { approvalEntry approvalRoleId state comment date }
					sourceCodeEntryDetails { uuid commit attributionState attributionReason }
				}
			}
		`
		variables := map[string]interface{}{"u": args[0]}
		if releaseShowSessionUuid != "" {
			variables["s"] = releaseShowSessionUuid
		}
		if releaseShowClientSessionId != "" {
			variables["c"] = releaseShowClientSessionId
		}
		data, err := sendGraphQLRequest(query, variables, rearmUri+"/graphql")
		if err != nil {
			printGqlError(err)
			os.Exit(1)
		}
		emitJson(data["releaseProgrammatic"])
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
			query ($input: AgentSessionInboxInput!) {
				agentSessionInboxProgrammatic(input: $input) {
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
		input := map[string]interface{}{"sessionUuid": args[0]}
		if inboxSince != "" {
			input["since"] = inboxSince
		}
		if len(inboxKinds) > 0 {
			input["kinds"] = inboxKinds
		}
		if inboxLimit > 0 {
			input["limit"] = inboxLimit
		}
		variables := map[string]interface{}{"input": input}
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
			mutation SessionAddArtifact($input: SessionAddArtifactInput!) {
				sessionAddArtifactProgrammatic(input: $input) {
					uuid
					status
					artifacts
					policyEvents { policyName state severity message evaluatedAt }
				}
			}
		`
		variables := map[string]interface{}{
			"input": map[string]interface{}{
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
			"0": {"variables.input.artifacts.0.file"},
		}
		mapJson, _ := json.Marshal(locationMap)

		if debug == "true" {
			fmt.Println("GraphQL operations:", string(opsJson))
			fmt.Println("Multipart map:", string(mapJson))
		}

		// CSRF must be passed on the multipart POST or Spring's
		// CsrfFilter rejects with 401 before the resolver runs. Fetch
		// the CSRF token ONCE up-front; using applySessionToRestyClient
		// would call getSession again with a different token, causing
		// the cookie value and the X-XSRF-TOKEN header value to fall
		// out of sync (Spring's CookieCsrfTokenRepository mints a fresh
		// token each call, and CsrfFilter rejects when cookie != header).
		// CSRF must be passed on the multipart POST or Spring's
		// CsrfFilter rejects with 401 before the resolver runs. Build
		// the request with net/http directly — resty (v2.17) has a
		// quirk where headers set on a multipart request can be
		// dropped on the wire, and CSRF is one of them.
		session, _ := getSession()

		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		opsPart, _ := writer.CreatePart(textproto.MIMEHeader{
			"Content-Disposition": []string{`form-data; name="operations"`},
			"Content-Type":        []string{"application/json"},
		})
		opsPart.Write(opsJson)
		mapPart, _ := writer.CreatePart(textproto.MIMEHeader{
			"Content-Disposition": []string{`form-data; name="map"`},
			"Content-Type":        []string{"application/json"},
		})
		mapPart.Write(mapJson)
		filePart, _ := writer.CreatePart(textproto.MIMEHeader{
			"Content-Disposition": []string{fmt.Sprintf(`form-data; name="0"; filename=%q`, fileName)},
			"Content-Type":        []string{"application/octet-stream"},
		})
		filePart.Write(fileBytes)
		writer.Close()

		hreq, err := http.NewRequest("POST", rearmUri+"/graphql", body)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to build request: %v\n", err)
			os.Exit(1)
		}
		hreq.Header.Set("Content-Type", writer.FormDataContentType())
		hreq.Header.Set("User-Agent", "ReARM CLI")
		// No Accept-Encoding: net/http won't transparently decode gzip
		// unless the http.Transport's DisableCompression is left to
		// default and we don't set the header explicitly. Letting it
		// auto-negotiate keeps the response body decoded.
		hreq.Header.Set("Apollo-Require-Preflight", "true")
		if len(apiKeyId) > 0 && len(apiKey) > 0 {
			hreq.SetBasicAuth(apiKeyId, apiKey)
		}
		if session != nil {
			hreq.Header.Set("X-XSRF-TOKEN", session.XsrfToken)
			// Build a clean Cookie header — skip JSESSIONID when empty
			// (Spring rejects `JSESSIONID=;` as malformed and the whole
			// Cookie line gets discarded, taking the XSRF-TOKEN cookie
			// with it and triggering a CSRF 401).
			cookieVal := "XSRF-TOKEN=" + session.XsrfToken
			if session.JSessionId != "" {
				cookieVal = "JSESSIONID=" + session.JSessionId + "; " + cookieVal
			}
			hreq.Header.Set("Cookie", cookieVal)
		}

		hresp, err := http.DefaultClient.Do(hreq)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Request failed: %v\n", err)
			os.Exit(1)
		}
		defer hresp.Body.Close()
		respBytes, _ := io.ReadAll(hresp.Body)
		if hresp.StatusCode != 200 {
			fmt.Fprintf(os.Stderr, "HTTP %d: %s\n", hresp.StatusCode, string(respBytes))
			os.Exit(1)
		}
		// Parse for GraphQL errors and emit data shape consistent with sendGraphQLRequest.
		var parsed map[string]interface{}
		if err := json.Unmarshal(respBytes, &parsed); err != nil {
			fmt.Fprintf(os.Stderr, "Could not parse response JSON: %v\nRaw: %s\n", err, string(respBytes))
			os.Exit(1)
		}
		if errsRaw, has := parsed["errors"]; has && errsRaw != nil {
			eb, _ := json.Marshal(errsRaw)
			fmt.Fprintf(os.Stderr, "GraphQL errors: %s\n", string(eb))
			os.Exit(1)
		}
		dataMap, _ := parsed["data"].(map[string]interface{})
		emitJson(dataMap["sessionAddArtifactProgrammatic"])
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
