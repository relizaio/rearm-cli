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
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	rearm "github.com/relizaio/rearm-client-go"
	"github.com/spf13/cobra"
)

// session open, session close --final and session current (task RD5-4): thin composites over init, add-artifact
// and close, which call the existing operations in order and stop at the first refusal, naming the step; and the
// current session they record (agentCurrentSession.go).

var (
	openOrientation     string
	openOrientationText string
	openBoard           string
	openRoles           []string
	closeFinal          string
	currentSet          string
	currentJson         bool
)

// The read session current --set makes: the session's client id, agent, the API key that opened it and its status,
// and nothing a server of another version may lack.
const sessionCurrentRead = `query AgentSessionCurrentProgrammatic($sessionUuid: ID!) { sessionProgrammatic(sessionUuid: $sessionUuid) { uuid clientSessionId agent apiKey status } }`

// The report phases and their display ids, as orientation §2.6 names them.
const (
	reportOrientation = "ORIENTATION"
	reportFinal       = "FINAL"
)

var reportDisplayIds = map[string]string{reportOrientation: "orient", reportFinal: "final"}

var agentSessionOpenCmd = &cobra.Command{
	Use:   "open --agent-name <name> --agent-model <model> [--title <t>] [--orientation <file>|--orientation-text <text>] [--board <b>] [--role <r>]",
	Short: "Open a session: init, the ORIENTATION report, and the current session for this repository",
	Long: `Runs session init (every init flag applies), records the session as the current one for this
repository on the instance the credentials point at, and files the ORIENTATION report when one is given
(--orientation <file> or --orientation-text <text>: an AGENTIC_REPORT artifact, display id orient, tag
agenticPhase=ORIENTATION). It stops at the first refusal and says which step it was; a failed upload leaves
the session open and current, and is not retried.

--board records the board the session works on, and --role the roles 'task assign' declares, in the
session's local state.

Prints the ids once, as three lines a shell can read:

  REARM_SESSION=<session uuid>
  REARM_CLIENT_SESSION_ID=<client session id, the ReARM-Agentic-Session trailer>
  REARM_AGENT=<agent uuid, the ReARM-Agent trailer>

Do not export them: the CLI reads REARM_<FLAG> from the environment, so an exported REARM_SESSION acts as
--session on every verb, on every instance, and REARM_CLIENT_SESSION_ID as the next init's
--client-session-id. The verbs need neither: each falls back to the current session.`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		os.Exit(runSessionOpen())
	},
}

var agentSessionCurrentCmd = &cobra.Command{
	Use:   "current [--set <session-uuid>]",
	Short: "Print the current session for this repository on this instance, or record an open one as current",
	Long: `Prints the session uuid recorded as current for this repository (git's top level, else the working
directory) on the instance the credentials point at, or nothing with exit 1. --json prints the entry: the
session, its client id and agent, the instance, and when and how it was recorded.

--set <session-uuid> records an existing session as current here, on the instance the credentials point at:
how a long-lived board session becomes current in a new worktree. The session is read once from that
instance, and only a session these credentials' API key opened is taken: the key that opened it is compared
with the key the credentials act as (the subject of their access token), so another key's session is refused
even when these credentials can read it (an admin key, a board's coordinator seat, a reader of a board the
session worked). A session that is not open is refused too. Its client id and agent are kept with the entry,
as session open keeps them. 'task assign' records nothing.`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		os.Exit(runSessionCurrent())
	},
}

// openStep prints which step of a composite failed.
func openStep(verb string, n int, step, format string, a ...interface{}) {
	fmt.Fprintf(os.Stderr, "rearm: session %s: step %d (%s) failed: "+format+"\n", append([]interface{}{verb, n, step}, a...)...)
}

// sessionReport is a report to file: its bytes and the name it is uploaded as.
type sessionReport struct {
	body     []byte
	filename string
	source   string // the file, for the retry command
}

// readReport reads --orientation / --final; text is the inline alternative (session open only).
func readReport(file, text, inlineName string) (*sessionReport, error) {
	if strings.TrimSpace(file) != "" && text != "" {
		return nil, fmt.Errorf("give --orientation <file> or --orientation-text <text>, not both")
	}
	if strings.TrimSpace(file) != "" {
		body, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		if len(bytes.TrimSpace(body)) == 0 {
			return nil, fmt.Errorf("%s is empty", file)
		}
		return &sessionReport{body: body, filename: filepath.Base(file), source: file}, nil
	}
	if strings.TrimSpace(text) != "" {
		return &sessionReport{body: []byte(text), filename: inlineName, source: "<file>"}, nil
	}
	return nil, nil
}

// uploadSessionReport files the report on the session as an AGENTIC_REPORT with the phase's display id and tag, as
// add-artifact does, and returns the error rather than exiting.
func uploadSessionReport(sessionUuid string, r *sessionReport, phase string) error {
	art := map[string]interface{}{
		"type":              "AGENTIC_REPORT",
		"storedIn":          "REARM",
		"file":              nil,
		"displayIdentifier": reportDisplayIds[phase],
		"tags":              []map[string]interface{}{{"key": "agenticPhase", "value": phase}},
	}
	variables := map[string]interface{}{
		"addArtifact": map[string]interface{}{
			"sessionUuid": sessionUuid,
			"artifacts":   []map[string]interface{}{art},
		},
	}
	query := rearm.SessionAddArtifact_Operation
	_, err := rearm.UploadMultipart(context.Background(), rearmClient(), opNameOf(query), query, variables,
		[]rearm.FilePart{{Filename: r.filename, Content: bytes.NewReader(r.body), VariablePath: "variables.addArtifact.artifacts.0.file"}})
	return err
}

// reportRetry is the add-artifact line that files the report by hand.
func reportRetry(sessionUuid string, r *sessionReport, phase string) string {
	return fmt.Sprintf("rearm agent session add-artifact %s --file %s --type AGENTIC_REPORT --display-id %s --tag agenticPhase=%s",
		sessionUuid, shellWord(r.source), reportDisplayIds[phase], phase)
}

func runSessionOpen() int {
	// Everything that can be refused locally is, before the session is opened.
	report, err := readReport(openOrientation, openOrientationText, "orientation.md")
	if err != nil {
		openStep("open", 0, "read the ORIENTATION report", "%v; nothing was opened", err)
		return 1
	}
	repo, err := repositoryKey()
	if err != nil {
		openStep("open", 0, "find the repository", "%v; nothing was opened", err)
		return 1
	}
	instance := currentInstance()
	if instance == "" {
		openStep("open", 0, "find the instance", "the credentials name no instance (--uri, REARM_URI or --config); nothing was opened")
		return 1
	}

	session, err := initializeSession()
	if err != nil {
		openStep("open", 1, "init", "%s", describeError(err))
		return 1
	}
	m, _ := session.(map[string]interface{})
	uuid := str(m["uuid"])
	if uuid == "" {
		openStep("open", 1, "init", "the server answered no session uuid")
		return 1
	}
	st := findStateBySessionUuid(uuid)
	clientId, agent := str(m["clientSessionId"]), str(m["agent"])
	if st != nil {
		clientId, agent = orElse(clientId, st.ClientSessionId), orElse(agent, st.AgentUuid)
	}
	clientId = orElse(clientId, orElse(clientSessionId, uuid))
	// The ids first, so a later step's failure never loses them.
	fmt.Printf("REARM_SESSION=%s\nREARM_CLIENT_SESSION_ID=%s\nREARM_AGENT=%s\n", uuid, clientId, agent)
	if events, _ := m["policyEvents"].([]interface{}); len(events) > 0 {
		fmt.Fprintf(os.Stderr, "rearm: the session has %d policy event(s); read them with: rearm agent session show %s\n", len(events), uuid)
	}
	if st != nil && (openBoard != "" || len(openRoles) > 0) {
		if openBoard != "" {
			st.Board = openBoard
		}
		if len(openRoles) > 0 {
			st.DeclaredRoles = openRoles
		}
		if err := writeAgentState(st); err != nil {
			fmt.Fprintf(os.Stderr, "rearm: could not keep --board/--role in the session's local state: %v\n", err)
		}
	}

	if err := recordCurrentSession(repo, currentSessionEntry{SessionUuid: uuid, ClientSessionId: clientId, AgentUuid: agent,
		Instance: instance, RecordedBy: "session open"}); err != nil {
		openStep("open", 2, "record the current session", "%v; session %s is open: record it with rearm agent session current --set %s", err, uuid, uuid)
		return 1
	}

	if report != nil {
		if err := uploadSessionReport(uuid, report, reportOrientation); err != nil {
			openStep("open", 3, "ORIENTATION report", "%s; session %s stays open and current; file the report with: %s",
				describeError(err), uuid, reportRetry(uuid, report, reportOrientation))
			return 1
		}
		fmt.Fprintf(os.Stderr, "rearm: ORIENTATION report filed on session %s; it is the current session for %s on %s\n", uuid, repo, instance)
	} else {
		fmt.Fprintf(os.Stderr, "rearm: session %s is the current session for %s on %s; no ORIENTATION report was given\n", uuid, repo, instance)
	}
	return 0
}

func runSessionClose(args []string) int {
	final, err := readReport(closeFinal, "", "")
	if err != nil {
		openStep("close", 0, "read the FINAL report", "%v; nothing was sent", err)
		return 1
	}
	var uuid string
	if len(args) == 1 {
		uuid = strings.TrimSpace(args[0])
	} else {
		repo, err := repositoryKey()
		if err != nil {
			repo = ""
		}
		instance := currentInstance()
		e := currentSessionFor(repo, instance)
		if e == nil || e.SessionUuid == "" {
			fmt.Fprintln(os.Stderr, "rearm: no session uuid given, and "+noCurrentSessionRefusal(repo, instance, sessionByPositional))
			return 1
		}
		uuid = e.SessionUuid
		fallbackInUse = &currentSessionUse{repo: repo, entry: *e}
	}

	if final != nil {
		if err := uploadSessionReport(uuid, final, reportFinal); err != nil {
			noticeClosedCurrentSession(err)
			openStep("close", 1, "FINAL report", "%s; session %s was not closed; file the report with: %s",
				describeError(err), uuid, reportRetry(uuid, final, reportFinal))
			return 1
		}
		fmt.Fprintf(os.Stderr, "rearm: FINAL report filed on session %s\n", uuid)
	}

	data, err := sendGraphQLRequest(rearm.SessionCloseProgrammatic_Operation, map[string]interface{}{"sessionUuid": uuid})
	if err != nil {
		if final != nil {
			openStep("close", 2, "close", "%s; the FINAL report is filed; close with: rearm agent session close %s", describeError(err), uuid)
		} else {
			printRefusal(err)
		}
		return 1
	}
	// The session is over; its local state is now just a stale mapping that a later Claude
	// session reusing the id would pick up. Removed after the close succeeds, never before.
	if st := findStateBySessionUuid(uuid); st != nil {
		removeAgentState(st)
	}
	if cleared := clearCurrentSessionEverywhere(uuid); len(cleared) > 0 {
		fmt.Fprintf(os.Stderr, "rearm: session %s is no longer the current session for %s\n", uuid, strings.Join(cleared, ", "))
	}
	emitJson(data["sessionCloseProgrammatic"])
	return 0
}

func runSessionCurrent() int {
	repo, err := repositoryKey()
	if err != nil {
		fmt.Fprintf(os.Stderr, "rearm: %v\n", err)
		return 1
	}
	instance := currentInstance()
	if strings.TrimSpace(currentSet) != "" {
		return runSessionCurrentSet(repo, instance, strings.TrimSpace(currentSet))
	}
	e := currentSessionFor(repo, instance)
	if e == nil || e.SessionUuid == "" {
		where := repo
		if instance != "" {
			where += " on " + instance
		}
		fmt.Fprintf(os.Stderr, "rearm: no current session for %s; open one with rearm agent session open, or record one with rearm agent session current --set <session-uuid>\n", where)
		return 1
	}
	if currentJson {
		out := map[string]interface{}{"repository": repo}
		raw, _ := json.Marshal(e)
		_ = json.Unmarshal(raw, &out)
		out["repository"] = repo
		emitJson(out)
		return 0
	}
	fmt.Println(e.SessionUuid)
	return 0
}

// runSessionCurrentSet records an existing open session these credentials' key opened as current (round 2 §3, round 3
// §1).
func runSessionCurrentSet(repo, instance, ref string) int {
	refuse := func(format string, a ...interface{}) int {
		fmt.Fprintf(os.Stderr, "rearm: session current --set: "+format+"; nothing was recorded\n", a...)
		return 1
	}
	if instance == "" {
		return refuse("the credentials name no instance (--uri, REARM_URI or --config)")
	}
	uuid := ref
	if !isUUID(strings.ToLower(uuid)) {
		st := lookupAgentState(ref)
		if st == nil || st.SessionUuid == "" {
			return refuse("%s is not a session uuid, and this host has no session with that client id", ref)
		}
		uuid = st.SessionUuid
	}
	data, err := sendGraphQLRequest(sessionCurrentRead, map[string]interface{}{"sessionUuid": uuid})
	if err != nil {
		return refuse("reading session %s on %s failed: %s. --set takes a session these credentials opened, on the instance they point at",
			uuid, instance, describeError(err))
	}
	s, _ := data["sessionProgrammatic"].(map[string]interface{})
	if s == nil {
		return refuse("%s answered no session %s for these credentials", instance, uuid)
	}
	// Only this key's own session (ARCHITECTURE round 3 §1): the server answers the read to more readers than the
	// key that opened the session, so the read succeeding proves nothing; the key is compared here.
	owner, caller := str(s["apiKey"]), callerKeyUuid()
	switch {
	case caller == "":
		return refuse("these credentials hold no access token for %s (a server without the token endpoint), so the key "+
			"that opened session %s cannot be compared with theirs; pass --session %s to each verb instead", instance, uuid, uuid)
	case owner == "":
		return refuse("%s answered no API key for session %s, so it cannot be told from another key's session; pass "+
			"--session %s to each verb instead", instance, uuid, uuid)
	case !strings.EqualFold(owner, caller):
		return refuse("session %s belongs to agent %s and was opened by API key %s, not by these credentials' key %s; "+
			"only a session this key opened can be current", uuid, orElse(str(s["agent"]), "(none answered)"), owner, caller)
	}
	if status := str(s["status"]); status != "OPEN" {
		return refuse("session %s is %s on %s; only an open session can be current", uuid, orElse(status, "of unknown status"), instance)
	}
	clientId, agent := str(s["clientSessionId"]), str(s["agent"])
	if clientId == "" || agent == "" {
		return refuse("%s answered no client id or agent for session %s", instance, uuid)
	}
	if err := recordCurrentSession(repo, currentSessionEntry{SessionUuid: uuid, ClientSessionId: clientId, AgentUuid: agent,
		Instance: instance, RecordedBy: "session current --set"}); err != nil {
		return refuse("%v", err)
	}
	keepSessionIdentity(uuid, clientId, agent)
	fmt.Printf("session %s (client id %s) is the current session for %s on %s\n", uuid, clientId, repo, instance)
	return 0
}

// callerKeyUuid is the API key these credentials act as: the subject of the access token the client holds, since
// the server issues every programmatic token, a key's or a browser login's, with its key's uuid as the subject.
// Empty when the client holds none: a server without the token endpoint, which is sent the key itself.
func callerKeyUuid() string { return tokenSubject(rearmClient().Tokens().AccessToken) }

// tokenSubject is the sub claim of a JWT, read without verifying it: the token is these credentials' own, as the
// server issued it, and is only read to learn which key they are.
func tokenSubject(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return ""
	}
	var claims struct {
		Sub string `json:"sub"`
	}
	if json.Unmarshal(payload, &claims) != nil {
		return ""
	}
	return strings.TrimSpace(claims.Sub)
}

// keepSessionIdentity keeps the client id and agent in local state for a session this host has no state for, as
// session open does. A state this host already has is never rewritten: it keeps its own name and content (a
// long-lived board session's hop records live there), and the entry carries the client id and agent anyway.
func keepSessionIdentity(uuid, clientId, agent string) {
	if lookupAgentState(uuid) != nil {
		return
	}
	if err := writeAgentState(&agentSessionState{SessionUuid: uuid, ClientSessionId: clientId, AgentUuid: agent}); err != nil {
		fmt.Fprintf(os.Stderr, "rearm: could not keep the session's client id and agent locally: %v\n", err)
	}
}

func init() {
	addSessionInitFlags(agentSessionOpenCmd)
	f := agentSessionOpenCmd.Flags()
	f.StringVar(&openOrientation, "orientation", "", "the ORIENTATION report to file, a file")
	f.StringVar(&openOrientationText, "orientation-text", "", "the ORIENTATION report to file, as text")
	f.StringVar(&openBoard, "board", "", "the board the session works on, kept in its local state")
	f.StringSliceVar(&openRoles, "role", nil, "a role the session declares to task assign, kept in its local state; repeatable")

	agentSessionCloseCmd.Flags().StringVar(&closeFinal, "final", "", "file this FINAL report before closing; the uuid may then be left out for the current session")

	agentSessionCurrentCmd.Flags().StringVar(&currentSet, "set", "", "record this open session as current for this repository on this instance")
	agentSessionCurrentCmd.Flags().BoolVar(&currentJson, "json", false, "print the entry as JSON")

	agentSessionCmd.AddCommand(agentSessionOpenCmd)
	agentSessionCmd.AddCommand(agentSessionCurrentCmd)

	// The verbs that refuse a missing --session by hand rather than through cobra's annotation, so the fallback
	// refuses for them too when no current session is recorded (task verify refuses with exit 2).
	for _, c := range []*cobra.Command{agentDocPublishCmd, agentDocElementCheckCmd, agentTaskBriefCmd, agentTaskPushCmd,
		agentGitCommitCmd, agentGitMergeCmd, agentWaitCmd, agentNotesAppendCmd} {
		markSessionRequired(c, 1)
	}
	markSessionRequired(agentTaskVerifyCmd, 2)
}
