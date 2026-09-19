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
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// Usage reporting: what a session consumed, sent to ReARM.
//
//   rearm agent session usage <session-uuid> --model m --input-tokens N ...   explicit
//   rearm agent session usage --from-transcript <path> [--since-seq N]        parse a transcript
//   rearm agent session usage --from-hook [--final]                           Claude Code hook
//
// THE GOVERNING RULE IS THAT THIS NEVER BLOCKS THE AGENT. It runs on the Stop hook of every turn,
// so a failure here -- server down, expired key, malformed transcript -- must cost the agent nothing
// but a line on stderr. Every path in the hook and transcript modes exits 0. Only the explicit mode,
// which a human or script invoked deliberately and can see the result of, reports a non-zero status.

var (
	usageSource           string
	usageModel            string
	usageInputTokens      int64
	usageOutputTokens     int64
	usageCacheReadTokens  int64
	usageCacheWriteTokens int64
	usageTurns            int
	usageToolCalls        int
	usageWallSeconds      int
	usageReportedCost     int64
	usageTask             string
	usageWindowStart      string
	usageWindowEnd        string
	usageSeq              int64
	usageFromTranscript   string
	usageSinceSeq         int64
	usageFromHook         bool
	usageFinal            bool
	usageClientSessionId  string
	usageHosting          string
	usageDryRun           bool
)

// maxRequestsPerReport chunks a backfill. The server sizes its oversize guard by request count, so
// this is the CLI's half of the same contract.
const maxRequestsPerReport = 500

// usageReportMutation is declared here rather than taken from the generated client because the
// client's operations are regenerated from the schema on its own release cadence; carrying the
// query inline lets the CLI ship with the server change instead of behind it. It moves into
// rearm-client-go at the next regeneration.
const usageReportMutation = `
mutation SessionReportUsageProgrammatic($input: SessionUsageReportInput!) {
  sessionReportUsageProgrammatic(input: $input) {
    accepted
    duplicates
    refused
    attribution
    task
  }
}`

// usageBail reports a problem and stops, exiting 0 in hook and transcript modes.
//
// This is the single place the never-block rule is enforced, so it cannot be half-applied: every
// early return in those modes goes through here.
func usageBail(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "rearm: "+format+"\n", args...)
	if usageFromHook || usageFromTranscript != "" {
		os.Exit(0)
	}
	os.Exit(1)
}

// hookPayload is what Claude Code writes to a hook's stdin.
type hookPayload struct {
	SessionId      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	Cwd            string `json:"cwd"`
	HookEventName  string `json:"hook_event_name"`
	StopHookActive bool   `json:"stop_hook_active"`
	Reason         string `json:"reason"`
}

// detectHosting reads the provider from the environment. Hosting is a pricing dimension, and the
// client is the only place that knows it for certain -- the model string alone does not say whether
// a request went through Bedrock.
func detectHosting() string {
	switch {
	case isTruthyEnv("CLAUDE_CODE_USE_BEDROCK"):
		return "BEDROCK"
	case isTruthyEnv("CLAUDE_CODE_USE_VERTEX"):
		return "VERTEX"
	case isTruthyEnv("CLAUDE_CODE_USE_AZURE"):
		return "AZURE"
	}
	// Deliberately not defaulting to DIRECT: an unset variable means "this CLI did not recognise
	// the provider", which is not the same as knowing it was direct. Omitting the field leaves the
	// server free to peel hosting out of the model string, which is the better guess of the two.
	return ""
}

func isTruthyEnv(name string) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	return v == "1" || v == "true" || v == "yes"
}

// reasoningFromEnv reads the effort level when the transcript did not carry one.
func reasoningFromEnv() string {
	for _, name := range []string{"CLAUDE_CODE_EFFORT_LEVEL", "CLAUDE_CODE_REASONING_EFFORT"} {
		if v := strings.TrimSpace(os.Getenv(name)); v != "" {
			return v
		}
	}
	return ""
}

// sendUsageReport posts one report and returns the ack, or an error.
func sendUsageReport(input map[string]interface{}) (map[string]interface{}, error) {
	data, err := sendGraphQLRequest(usageReportMutation, map[string]interface{}{"input": input})
	if err != nil {
		return nil, err
	}
	ack, _ := data["sessionReportUsageProgrammatic"].(map[string]interface{})
	return ack, nil
}

// buildLinePayload maps a parsed line onto SessionUsageLineInput.
func buildLinePayload(l usageLine, hosting string) map[string]interface{} {
	line := map[string]interface{}{
		"model":                   l.wireModel(),
		"contextBand":             l.ContextBand,
		"requests":                l.Requests,
		"inputTokens":             l.InputTokens,
		"outputTokens":            l.OutputTokens,
		"cacheReadTokens":         l.CacheReadTokens,
		"cacheWriteTokens":        l.CacheWriteTokens,
		"maxRequestContextTokens": l.MaxRequestContextTokens,
		"minRequestContextTokens": l.MinRequestContextTokens,
	}
	if hosting != "" {
		line["hosting"] = hosting
	}
	return line
}

// reportDelta sends a parsed transcript delta, chunked, stopping at the first failure.
//
// Stopping rather than continuing is what makes a resume correct: lastSeq is only advanced past
// chunks the server confirmed, so the next run picks up exactly where this one stopped instead of
// leaving a hole no later run would ever fill.
func reportDelta(st *agentSessionState, delta *transcriptDelta, source string, final bool) {
	if len(delta.Lines) == 0 {
		if usageDryRun {
			fmt.Println("{\"lines\":0}")
		}
		return
	}
	hosting := detectHosting()
	reasoning := delta.ReasoningLevel
	if reasoning == "" {
		reasoning = reasoningFromEnv()
	}
	chunks := chunkLines(delta.Lines, maxRequestsPerReport)
	for i, chunk := range chunks {
		turns, tools, requests := 0, 0, 0
		payloadLines := make([]map[string]interface{}, 0, len(chunk))
		for _, l := range chunk {
			turns += l.Turns
			tools += l.ToolCalls
			requests += l.Requests
			payloadLines = append(payloadLines, buildLinePayload(l, hosting))
		}
		// Each chunk needs its own sequence. The last chunk carries the true end offset -- that is
		// the resume point -- while earlier ones are offset backwards by their position so the
		// sequence stays monotonic and distinct without inventing offsets past the end of the file.
		seq := delta.EndOffset - int64(len(chunks)-1-i)
		input := map[string]interface{}{
			"clientSeq": seq,
			"source":    source,
			"turns":     turns,
			"toolCalls": tools,
			"lines":     payloadLines,
		}
		if st.SessionUuid != "" {
			input["sessionUuid"] = st.SessionUuid
		} else if st.ClientSessionId != "" {
			input["clientSessionId"] = st.ClientSessionId
		}
		if st.CurrentTask != "" {
			input["taskUuid"] = st.CurrentTask
		}
		if reasoning != "" {
			input["reasoningLevel"] = reasoning
		}
		raw := map[string]interface{}{"final": final, "chunk": i + 1, "chunks": len(chunks)}
		if delta.SidechainRows > 0 {
			// Subagent turns are in the parent's transcript and counted into it. Recorded so the
			// number is explainable later rather than looking like the parent talked to itself.
			raw["sidechainRows"] = delta.SidechainRows
		}
		input["raw"] = raw

		if usageDryRun {
			out, _ := json.MarshalIndent(input, "", "  ")
			fmt.Println(string(out))
			continue
		}
		ack, err := sendUsageReport(input)
		if err != nil {
			// The offset is NOT advanced past this chunk, so the next run resends it.
			usageBail("usage report failed (%d of %d), will retry next run: %v", i+1, len(chunks), err)
			return
		}
		if refused, ok := ack["refused"].([]interface{}); ok && len(refused) > 0 {
			for _, r := range refused {
				fmt.Fprintf(os.Stderr, "rearm: usage line refused: %v\n", r)
			}
		}
		// Advance only after the server confirmed this chunk.
		st.LastSeq = seq
		if err := writeAgentState(st); err != nil {
			fmt.Fprintf(os.Stderr, "rearm: could not record usage offset: %v\n", err)
		}
	}
}

var agentSessionUsageCmd = &cobra.Command{
	Use:   "usage [session-uuid]",
	Short: "Report what this session consumed (tokens, turns, tool calls)",
	Long: `Reports usage against an agent session. Three modes:

  --from-hook         read a Claude Code hook payload from stdin and report the
                      delta since the last report. This is what 'rearm agent
                      hooks install' wires up; you rarely run it by hand.
  --from-transcript   parse a Claude Code transcript at the given path.
  (neither)           report explicit numbers from the flags, for agents that
                      are not Claude Code. Use --source self-reported.

In hook and transcript modes any failure exits 0 with a message on stderr: usage
reporting must never block the agent it is measuring.

Reports are idempotent. The sequence is the transcript byte offset, kept in the
local state file, and the server drops a delta it has already filed.`,
	Args: cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		switch {
		case usageFromHook:
			runUsageFromHook()
		case usageFromTranscript != "":
			runUsageFromTranscript(args)
		default:
			runUsageExplicit(args)
		}
	},
}

func runUsageFromHook() {
	raw, err := io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
	if err != nil || len(raw) == 0 {
		usageBail("no hook payload on stdin")
		return
	}
	var p hookPayload
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
		// No local state: this Claude session was never bound to a ReARM session on this machine.
		// Silent by design -- a developer running Claude Code outside any ReARM session would
		// otherwise get a warning on every single turn.
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
	delta, err := parseTranscript(transcript, st.LastSeq)
	if err != nil {
		usageBail("could not read transcript: %v", err)
		return
	}
	reportDelta(st, delta, "TRANSCRIPT", usageFinal)
}

func runUsageFromTranscript(args []string) {
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
	delta, err := parseTranscript(usageFromTranscript, since)
	if err != nil {
		usageBail("could not read transcript: %v", err)
		return
	}
	reportDelta(st, delta, "TRANSCRIPT", usageFinal)
}

// resolveUsageState builds the state to report against, from an explicit uuid, a client session id,
// or the local state file.
func resolveUsageState(args []string) *agentSessionState {
	if len(args) > 0 && args[0] != "" {
		st := &agentSessionState{SessionUuid: args[0]}
		// An explicit uuid still picks up the local file when one exists, so the resume offset and
		// current task are not lost by naming the session directly.
		if usageClientSessionId != "" {
			if existing, err := readAgentState(usageClientSessionId); err == nil && existing != nil {
				existing.SessionUuid = args[0]
				return existing
			}
			st.ClientSessionId = usageClientSessionId
		}
		return st
	}
	if usageClientSessionId != "" {
		if st, err := readAgentState(usageClientSessionId); err == nil && st != nil {
			return st
		}
		return &agentSessionState{ClientSessionId: usageClientSessionId}
	}
	return nil
}

func runUsageExplicit(args []string) {
	st := resolveUsageState(args)
	if st == nil {
		usageBail("need a session uuid or --client-session-id")
		return
	}
	if usageModel == "" {
		usageBail("--model is required when reporting explicit numbers")
		return
	}
	if usageInputTokens < 0 || usageOutputTokens < 0 {
		usageBail("token counts cannot be negative")
		return
	}
	source := strings.ToUpper(strings.ReplaceAll(usageSource, "-", "_"))
	if source == "" {
		source = "SELF_REPORTED"
	}
	context := usageInputTokens + usageCacheReadTokens + usageCacheWriteTokens
	line := map[string]interface{}{
		"model":                   usageModel,
		"contextBand":             bandLabel(usageModel, context),
		"requests":                maxInt(usageTurns, 1),
		"inputTokens":             usageInputTokens,
		"outputTokens":            usageOutputTokens,
		"cacheReadTokens":         usageCacheReadTokens,
		"cacheWriteTokens":        usageCacheWriteTokens,
		"maxRequestContextTokens": context,
		"minRequestContextTokens": context,
	}
	if usageHosting != "" {
		line["hosting"] = strings.ToUpper(usageHosting)
	} else if h := detectHosting(); h != "" {
		line["hosting"] = h
	}
	if usageReportedCost > 0 {
		line["reportedCostMicros"] = usageReportedCost
	}
	seq := usageSeq
	if seq <= 0 {
		// No transcript offset to use, so the clock stands in: monotonic per session, which is all
		// the server requires of it.
		seq = time.Now().UnixMilli()
	}
	input := map[string]interface{}{
		"clientSeq": seq,
		"source":    source,
		"lines":     []map[string]interface{}{line},
	}
	if st.SessionUuid != "" {
		input["sessionUuid"] = st.SessionUuid
	} else {
		input["clientSessionId"] = st.ClientSessionId
	}
	if usageTurns > 0 {
		input["turns"] = usageTurns
	}
	if usageToolCalls > 0 {
		input["toolCalls"] = usageToolCalls
	}
	if usageWallSeconds > 0 {
		input["wallSeconds"] = usageWallSeconds
	}
	task := usageTask
	if task == "" {
		task = st.CurrentTask
	}
	if task != "" {
		input["taskUuid"] = task
	}
	if usageWindowStart != "" {
		input["windowStart"] = usageWindowStart
	}
	if usageWindowEnd != "" {
		input["windowEnd"] = usageWindowEnd
	}
	if usageDryRun {
		out, _ := json.MarshalIndent(input, "", "  ")
		fmt.Println(string(out))
		return
	}
	ack, err := sendUsageReport(input)
	if err != nil {
		printGqlError(err)
		os.Exit(1)
	}
	emitJson(ack)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func init() {
	f := agentSessionUsageCmd.Flags()
	f.StringVar(&usageSource, "source", "", "transcript | self-reported | otel | provider")
	f.StringVar(&usageModel, "model", "", "model identifier, sent verbatim")
	f.Int64Var(&usageInputTokens, "input-tokens", 0, "uncached input tokens")
	f.Int64Var(&usageOutputTokens, "output-tokens", 0, "output tokens")
	f.Int64Var(&usageCacheReadTokens, "cache-read-tokens", 0, "tokens read from the prompt cache")
	f.Int64Var(&usageCacheWriteTokens, "cache-write-tokens", 0, "tokens written to the prompt cache")
	f.IntVar(&usageTurns, "turns", 0, "assistant turns in this window")
	f.IntVar(&usageToolCalls, "tool-calls", 0, "tool calls in this window")
	f.IntVar(&usageWallSeconds, "wall-seconds", 0, "wall-clock seconds this window covers")
	f.Int64Var(&usageReportedCost, "reported-cost-micros", 0, "your own cost figure, in USD micros")
	f.StringVar(&usageTask, "task", "", "task uuid to attribute to (defaults to the assigned task)")
	f.StringVar(&usageWindowStart, "window-start", "", "ISO-8601 start of the window")
	f.StringVar(&usageWindowEnd, "window-end", "", "ISO-8601 end of the window")
	f.Int64Var(&usageSeq, "seq", 0, "monotonic sequence; defaults to the transcript offset or the clock")
	f.StringVar(&usageFromTranscript, "from-transcript", "", "parse this Claude Code transcript")
	f.Int64Var(&usageSinceSeq, "since-seq", -1, "start at this byte offset instead of the recorded one")
	f.BoolVar(&usageFromHook, "from-hook", false, "read a Claude Code hook payload from stdin")
	f.BoolVar(&usageFinal, "final", false, "mark this as the session's last flush")
	f.StringVar(&usageClientSessionId, "client-session-id", "", "resolve the session by its client id")
	f.StringVar(&usageHosting, "hosting", "", "DIRECT | BEDROCK | VERTEX | AZURE")
	f.BoolVar(&usageDryRun, "dry-run", false, "print the report that would be sent and exit")
}
