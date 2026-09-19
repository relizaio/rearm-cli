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
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// Claude Code specific support: `rearm agent claude ...`.
//
// Everything under this subcommand knows something about Claude Code in particular -- the shape of
// its transcript, the payload its hooks pass on stdin, where its settings live, the environment
// variables it exports. Anything an agent of any kind would need -- the session state file, the
// usage report itself, context bands, chunking -- stays at `rearm agent ...` so a second agent can
// be added beside this one without moving it.
//
//   rearm agent claude hooks install | uninstall [--project | --user]
//   rearm agent claude usage --from-hook [--final]
//   rearm agent claude usage --from-transcript <path> [--since-seq N]

var agentClaudeCmd = &cobra.Command{
	Use:   "claude",
	Short: "Claude Code integration: usage hooks and transcript reporting",
	Long: `Commands specific to Claude Code.

Usage reporting for any other agent is 'rearm agent session usage' with explicit
numbers; these subcommands exist because Claude Code writes a transcript and runs
hooks, and both have shapes only it has.`,
}

// claudeHookPayload is what Claude Code writes to a hook's stdin.
type claudeHookPayload struct {
	SessionId      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	Cwd            string `json:"cwd"`
	HookEventName  string `json:"hook_event_name"`
	StopHookActive bool   `json:"stop_hook_active"`
	Reason         string `json:"reason"`
}

// claudeReasoningFromEnv reads the effort level when the transcript did not carry one.
//
// The name was verified against a running Claude Code rather than guessed: it is CLAUDE_EFFORT,
// not the CLAUDE_CODE_EFFORT_LEVEL this first assumed. The older spelling is kept as a fallback
// because it costs nothing and a wrong guess here silently drops the field.
func claudeReasoningFromEnv() string {
	for _, name := range []string{"CLAUDE_EFFORT", "CLAUDE_CODE_EFFORT_LEVEL"} {
		if v := strings.TrimSpace(os.Getenv(name)); v != "" {
			return v
		}
	}
	return ""
}

// adoptClaudeSession binds a Claude session id to local state that does not have one yet.
//
// The hook resolves its session by the id in its payload, which only works if `session init`
// recorded that id. It usually does -- Claude Code exports CLAUDE_CODE_SESSION_ID -- but not when
// init ran somewhere that variable was absent: a wrapper script, a different shell, CI. The failure
// mode there is the worst kind: the hook finds nothing, exits 0 by design, and the session records
// no usage at all with nothing anywhere saying why.
//
// So the hook binds itself. If exactly one state file is missing a Claude id, it is unambiguously
// this one -- a machine running two ReARM-tracked Claude sessions both initialised without the
// variable is the only case this cannot resolve, and there it does nothing rather than guess and
// bind usage to the wrong session.
func adoptClaudeSession(claudeSessionId string) *agentSessionState {
	if claudeSessionId == "" {
		return nil
	}
	dir, err := agentStateDir()
	if err != nil {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var candidates []*agentSessionState
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var st agentSessionState
		if json.Unmarshal(raw, &st) != nil {
			continue
		}
		if st.ExternalSessionId == "" && st.SessionUuid != "" {
			candidates = append(candidates, &st)
		}
	}
	if len(candidates) != 1 {
		if len(candidates) > 1 {
			fmt.Fprintf(os.Stderr, "rearm: %d local sessions have no Claude session id; cannot tell "+
				"which one this is, so usage is not being reported. Re-run `rearm agent session init` "+
				"with --claude-session-id.\n", len(candidates))
		}
		return nil
	}
	adopted := candidates[0]
	adopted.ExternalSessionId = claudeSessionId
	if err := writeAgentState(adopted); err != nil {
		fmt.Fprintf(os.Stderr, "rearm: could not bind this Claude session to %s: %v\n", adopted.SessionUuid, err)
		return nil
	}
	fmt.Fprintf(os.Stderr, "rearm: bound Claude session %s to ReARM session %s\n",
		claudeSessionId, adopted.SessionUuid)
	return adopted
}

// claudeSessionIdFromEnv reads the session id Claude Code exports to what it runs.
//
// Verified against a running instance rather than assumed: it is CLAUDE_CODE_SESSION_ID, and its
// value is exactly the sessionId the transcript carries. The plainer CLAUDE_SESSION_ID is unset in
// practice and is kept only as a fallback.
func claudeSessionIdFromEnv() string {
	return firstNonEmptyEnv("CLAUDE_CODE_SESSION_ID", "CLAUDE_SESSION_ID")
}

func runClaudeUsageFromHook() {
	raw, err := io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
	if err != nil || len(raw) == 0 {
		usageBail("no hook payload on stdin")
		return
	}
	var p claudeHookPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		usageBail("hook payload is not readable JSON: %v", err)
		return
	}
	// A Stop hook that itself triggered this run: reporting again would loop.
	if p.StopHookActive {
		os.Exit(0)
	}
	if p.SessionId == "" {
		usageBail("hook payload carries no session_id")
		return
	}
	st, err := readAgentState(p.SessionId)
	if err != nil {
		usageBail("could not read session state: %v", err)
		return
	}
	if st == nil {
		// Nothing filed under this Claude id. Before giving up, try to bind: `session init` may
		// have run somewhere CLAUDE_CODE_SESSION_ID was not set, which would otherwise mean this
		// session silently reports nothing for its whole life.
		st = adoptClaudeSession(p.SessionId)
	}
	if st == nil {
		// Genuinely not a tracked session -- a developer running Claude Code outside any ReARM
		// session. Silent by design: warning here would fire on every turn of every such run.
		os.Exit(0)
	}
	transcript := p.TranscriptPath
	if transcript == "" {
		transcript = st.TranscriptPath
	}
	if transcript == "" {
		usageBail("no transcript path in the hook payload or session state")
		return
	}
	st.TranscriptPath = transcript
	delta, err := parseClaudeTranscript(transcript, st.LastSeq)
	if err != nil {
		usageBail("could not read transcript: %v", err)
		return
	}
	reportDelta(st, delta, "TRANSCRIPT", usageFinal)
}

func runClaudeUsageFromTranscript(args []string) {
	st := resolveUsageState(args)
	if st == nil {
		usageBail("need a session uuid, --client-session-id, or local session state")
		return
	}
	since := st.LastSeq
	if usageSinceSeq >= 0 {
		since = usageSinceSeq
	}
	st.TranscriptPath = usageFromTranscript
	delta, err := parseClaudeTranscript(usageFromTranscript, since)
	if err != nil {
		usageBail("could not read transcript: %v", err)
		return
	}
	reportDelta(st, delta, "TRANSCRIPT", usageFinal)
}

var agentClaudeUsageCmd = &cobra.Command{
	Use:   "usage",
	Short: "Report this Claude Code session's usage from its transcript",
	Long: `Reads a Claude Code transcript and reports the delta since the last report.

  --from-hook         read the hook payload from stdin. This is what
                      'rearm agent claude hooks install' wires up; you rarely run
                      it by hand.
  --from-transcript   parse the transcript at the given path.

Either way a failure exits 0 with a message on stderr: usage reporting must never
block the agent it is measuring. Reports are idempotent -- the sequence is the
transcript byte offset, kept in the local state file, and the server drops a delta
it has already filed.

For agents that are not Claude Code, use 'rearm agent session usage' with explicit
numbers instead.`,
	Args: cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		switch {
		case usageFromHook:
			runClaudeUsageFromHook()
		case usageFromTranscript != "":
			runClaudeUsageFromTranscript(args)
		default:
			usageBail("give --from-hook or --from-transcript; for explicit numbers " +
				"use `rearm agent session usage`")
		}
	},
}

func init() {
	f := agentClaudeUsageCmd.Flags()
	f.StringVar(&usageFromTranscript, "from-transcript", "", "parse this Claude Code transcript")
	f.Int64Var(&usageSinceSeq, "since-seq", -1, "start at this byte offset instead of the recorded one")
	f.BoolVar(&usageFromHook, "from-hook", false, "read a Claude Code hook payload from stdin")
	f.BoolVar(&usageFinal, "final", false, "mark this as the session's last flush")
	f.StringVar(&usageClientSessionId, "client-session-id", "", "resolve the session by its client id")
	f.BoolVar(&usageDryRun, "dry-run", false, "print the report that would be sent and exit")

	agentClaudeCmd.AddCommand(agentClaudeUsageCmd)
	agentClaudeCmd.AddCommand(agentClaudeHooksCmd)
}
