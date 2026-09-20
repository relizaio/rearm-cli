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
	"strings"
	"time"

	rearm "github.com/relizaio/rearm-client-go"
	"github.com/spf13/cobra"
)

// Usage reporting: what a session consumed, sent to ReARM.
//
//   rearm agent session usage <session-uuid> --model m --input-tokens N ...   explicit numbers
//
// Transcript and hook reporting live under `rearm agent claude`, because both know the shape of
// one particular agent's output. What stays here is what any agent needs: the report payload, the
// context bands, chunking, and the never-block rule.
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

// usageDelta is one batch of usage ready to report: the generic shape every agent integration
// produces, whatever it parsed to get there.
//
// Defined at agent level on purpose. reportDelta below is shared, and a shared function taking a
// Claude-shaped argument would mean the next integration either reuses a type named after another
// agent or duplicates the reporting path.
type usageDelta struct {
	Lines []usageLine
	// EndOffset is the resume point and the clientSeq this delta is sent under. For a transcript
	// parser it is a byte offset; another integration may use anything monotonic per session.
	EndOffset int64
	// Effort/verbosity setting in force, when the integration can see it.
	ReasoningLevel string
	// The recorded offset was past the end of the source, so this is a re-read from the start. The
	// sequence derived from it is BELOW the session's high-water mark, which the server refuses --
	// so the caller must not keep resending it under the old session.
	Truncated bool
	// Agent-specific facts worth keeping beside the numbers; merged into the report's raw payload.
	Extra map[string]interface{}
}

// usageLine is one (model, band, service tier) group: exactly what the server stores as a row and
// prices under a single pricing entry.
//
// Internal only -- the wire payload is built field by field in reportUsagePayload, because the
// server takes turns and tool calls on the REPORT rather than on the line.
type usageLine struct {
	Model                   string
	ContextBand             int64
	Requests                int
	InputTokens             int64
	OutputTokens            int64
	CacheReadTokens         int64
	CacheWriteTokens        int64
	MaxRequestContextTokens int64
	MinRequestContextTokens int64
	Turns                   int
	ToolCalls               int
	// As reported by the transcript ("standard", "batch", "priority"). Not a wire field: it is
	// folded into the model string below, where the server's normaliser peels it back out into a
	// pricing variant. Grouping on it keeps two tiers of the same model from sharing a row and
	// pricing at whichever tier happened to come first.
	ServiceTier string
}

// wireModel is the model string as sent. A non-standard service tier is appended as the suffix the
// server already peels into a serviceTier variant, which is how a batch-priced request reaches the
// right pricing entry -- the transcript keeps the tier beside the model rather than in it, so
// sending the bare model string would price batch traffic at standard rates.
func (l usageLine) wireModel() string {
	if l.ServiceTier != "" && !strings.EqualFold(l.ServiceTier, "standard") {
		return l.Model + "-" + strings.ToLower(l.ServiceTier)
	}
	return l.Model
}

// contextBands are the thresholds at which pricing changes, by model family. Anthropic's current
// models price differently above a 200k request context.
//
// A static table in the CLI rather than a server lookup: the hook must work with the server
// unreachable, and a band the CLI does not know is not a failure -- every line carries its max and
// min request context, so the server can price it correctly and see that the band label came from
// an older table than its own.
var contextBands = map[string][]int64{
	// Keyed by MODEL family, not by agent: any integration reporting an Anthropic model bands it
	// the same way, whether or not Claude Code was the thing running it.
	"claude": {200000},
}

func bandsForModel(model string) []int64 {
	lower := strings.ToLower(model)
	for family, bands := range contextBands {
		if strings.Contains(lower, family) {
			return bands
		}
	}
	return nil
}

// bandFloor is the floor of the pricing band a request context falls in: 0 below the first
// threshold, then the threshold itself.
//
// A number, not a label. The server keys usage rows on it, and a formatted label put spelling into
// that key -- "200k" and "200000" would have been two bands -- while making every comparison a
// string match on something inherently ordered. A floor sorts, and compares directly against a
// pricing entry's contextAboveTokens.
func bandFloor(model string, contextTokens int64) int64 {
	var floor int64
	for _, b := range bandsForModel(model) {
		if contextTokens >= b {
			floor = b
		}
	}
	return floor
}

// chunkLines splits a backfill into reports of at most maxRequests requests each.
//
// A first run can carry an entire session's history; sending it as one report would hit the
// server's oversize guard and be refused in full. Lines are not split internally -- a line is the
// unit the server prices -- so a single line larger than the limit goes on its own, which the
// server sizes by request count and accepts.
func chunkLines(lines []usageLine, maxRequests int) [][]usageLine {
	if maxRequests <= 0 {
		return [][]usageLine{lines}
	}
	var out [][]usageLine
	var cur []usageLine
	count := 0
	for _, l := range lines {
		if len(cur) > 0 && count+l.Requests > maxRequests {
			out = append(out, cur)
			cur = nil
			count = 0
		}
		cur = append(cur, l)
		count += l.Requests
	}
	if len(cur) > 0 {
		out = append(out, cur)
	}
	return out
}

// maxRequestsPerReport chunks a backfill. The server sizes its oversize guard by request count, so
// this is the CLI's half of the same contract.
const maxRequestsPerReport = 500

// The report mutation comes from the generated client, like every other operation the CLI
// sends. It was briefly declared inline here so the CLI could ship alongside the server change
// instead of waiting on a client release; rearm-client-go now carries it.

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

// hookRequestTimeout bounds a report sent from an agent's hook.
//
// The shared client's timeout is ten minutes, which is right for an artifact upload and completely
// wrong here: a hanging server would hold the agent's turn open until Claude Code's own hook
// timeout fired. Usage reporting is the least important thing happening on that turn, so it gets
// the shortest leash. A missed report is resent next turn from the same offset.
const hookRequestTimeout = 5 * time.Second

// sendUsageReport posts one report and returns the ack, or an error.
func sendUsageReport(input map[string]interface{}) (map[string]interface{}, error) {
	send := func() (map[string]interface{}, error) {
		data, err := sendGraphQLRequest(rearm.SessionReportUsageProgrammatic_Operation, map[string]interface{}{"input": input})
		if err != nil {
			return nil, err
		}
		ack, _ := data["sessionReportUsageProgrammatic"].(map[string]interface{})
		return ack, nil
	}
	if !usageFromHook {
		return send()
	}
	// Bounded in the caller rather than by rebuilding the shared client: the client is shared with
	// every other command and carries the auth wiring, so giving this one call its own deadline is
	// the smaller, safer change. The request itself is not cancelled -- it is abandoned -- which is
	// acceptable for a fire-and-forget report and is the difference between the agent waiting five
	// seconds and waiting ten minutes.
	type result struct {
		ack map[string]interface{}
		err error
	}
	ch := make(chan result, 1)
	go func() {
		ack, err := send()
		ch <- result{ack, err}
	}()
	select {
	case r := <-ch:
		return r.ack, r.err
	case <-time.After(hookRequestTimeout):
		return nil, fmt.Errorf("usage report timed out after %s", hookRequestTimeout)
	}
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
func reportDelta(st *agentSessionState, delta *usageDelta, source string, final bool) {
	if delta.Truncated {
		// The transcript is shorter than the offset we recorded, so this is a different session
		// writing to a path we already have history for. Re-reading produced a sequence below the
		// server's high-water mark for the old session, which it refuses -- so stop reporting
		// against the stale mapping rather than being refused on every turn from here on.
		//
		// The warning is printed ONCE and the flag recorded, because this condition holds for the
		// whole life of the session: without the flag the same paragraph lands on the agent's
		// stderr every single turn. The state is kept rather than deleted -- it still holds the
		// mapping an operator needs to work out what happened.
		if !st.TruncationWarned {
			fmt.Fprintf(os.Stderr, "rearm: transcript for session %s is shorter than the offset already "+
				"reported (%d bytes); this looks like a new agent session reusing the path. Not "+
				"reporting to avoid a rejected sequence -- run `rearm agent session init` for the new "+
				"session.\n", st.SessionUuid, st.LastSeq)
			st.TruncationWarned = true
			if err := writeAgentState(st); err != nil {
				// Only costs a repeated warning, so it is not worth failing over.
				fmt.Fprintf(os.Stderr, "rearm: could not record the warning state: %v\n", err)
			}
		}
		return
	}
	if len(delta.Lines) == 0 {
		if usageDryRun {
			fmt.Println("{\"lines\":0}")
		}
		return
	}
	hosting := detectHosting()
	reasoning := delta.ReasoningLevel
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
		// Each chunk needs its own sequence, and they are derived from the ONE end offset of the
		// parsed range because that is all a chunk has: chunking splits lines, not byte ranges, so
		// no chunk corresponds to a byte position of its own. Earlier chunks are offset backwards
		// by their position to stay monotonic and distinct.
		//
		// These are therefore book-keeping values, not resume points -- which is exactly why
		// LastSeq is not advanced here; see below.
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
		// Whatever the integration thought worth recording beside the numbers -- for a transcript
		// parser, how many turns came from subagents, so the total is explainable later rather
		// than looking like the session talked to itself.
		for k, v := range delta.Extra {
			raw[k] = v
		}
		input["raw"] = raw

		if usageDryRun {
			out, _ := json.MarshalIndent(input, "", "  ")
			fmt.Println(string(out))
			continue
		}
		ack, err := sendUsageReport(input)
		if err != nil {
			// Stop here and leave LastSeq where it was, so the whole range is resent next run.
			usageBail("usage report failed (%d of %d), will retry next run: %v", i+1, len(chunks), err)
			return
		}
		if refused, ok := ack["refused"].([]interface{}); ok && len(refused) > 0 {
			for _, r := range refused {
				fmt.Fprintf(os.Stderr, "rearm: usage line refused: %v\n", r)
			}
		}
	}

	// LastSeq advances only once EVERY chunk is in, and only to the real end of the parsed range.
	//
	// Advancing per chunk was a data-loss bug, not merely untidy. All the chunks come from one
	// parsed range, so a per-chunk advance moved LastSeq to a fabricated offset near the END of
	// that range after the FIRST chunk. If chunk two of three then failed, the next run resumed
	// from that near-the-end offset, parsed almost nothing, and chunks two and three were never
	// sent by anyone -- silently, and permanently.
	//
	// Resending the whole range costs one duplicate report of chunk one, which the server answers
	// with a duplicate count and no rows written. That is precisely what the idempotency key was
	// built for, and a cheap duplicate beats a silent hole.
	st.LastSeq = delta.EndOffset
	if err := writeAgentState(st); err != nil {
		fmt.Fprintf(os.Stderr, "rearm: could not record usage offset: %v\n", err)
	}
}

var agentSessionUsageCmd = &cobra.Command{
	Use:   "usage [session-uuid]",
	Short: "Report what this session consumed (tokens, turns, tool calls)",
	Long: `Reports usage against an agent session from explicit numbers, for agents
that report their own consumption:

  rearm agent session usage <session-uuid> --source self-reported \
    --model '<model identifier>' --input-tokens N --output-tokens N \
    --turns N --wall-seconds N

Claude Code does not need this: it writes a transcript, so 'rearm agent claude
usage' reads the numbers rather than being told them, and 'rearm agent claude
hooks install' runs that automatically.

Reports are idempotent on their sequence, so a resend files nothing twice.`,
	Args: cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		runUsageExplicit(args)
	},
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
	// The window's total context across however many turns it covers -- which is a per-REQUEST
	// figure only when the window is a single request.
	windowContext := usageInputTokens + usageCacheReadTokens + usageCacheWriteTokens
	requests := maxInt(usageTurns, 1)
	line := map[string]interface{}{
		"model":            usageModel,
		"requests":         requests,
		"inputTokens":      usageInputTokens,
		"outputTokens":     usageOutputTokens,
		"cacheReadTokens":  usageCacheReadTokens,
		"cacheWriteTokens": usageCacheWriteTokens,
	}
	if requests == 1 {
		// One request, so the window's totals ARE that request's context and the band is real.
		line["contextBand"] = bandFloor(usageModel, windowContext)
		line["maxRequestContextTokens"] = windowContext
		line["minRequestContextTokens"] = windowContext
	} else {
		// Several requests summed into one line: the sum is not any request's context, and
		// reporting it as the max would trip long-context pricing on a window of many small
		// requests that never individually came near the threshold. The base band is honest --
		// the server prices under the base entry and knows the band was not measured.
		line["contextBand"] = int64(0)
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
	f.StringVar(&usageSource, "source", "", "self-reported | otel | provider")
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
	f.Int64Var(&usageSeq, "seq", 0, "monotonic sequence; defaults to the clock")
	f.StringVar(&usageHosting, "hosting", "", "DIRECT | BEDROCK | VERTEX | AZURE")
	f.BoolVar(&usageDryRun, "dry-run", false, "print the report that would be sent and exit")
	// --client-session-id is shared with the claude subcommand, which registers its own copy;
	// both resolve the same state file.
	f.StringVar(&usageClientSessionId, "client-session-id", "", "resolve the session by its client id")
}
