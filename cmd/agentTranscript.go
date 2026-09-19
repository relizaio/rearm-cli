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
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

// Claude Code transcript parsing.
//
// The transcript is JSONL, one object per line, appended as the session runs. Assistant rows carry
// the model and the token usage. Two properties of the format drive everything below, and both were
// verified against real transcripts rather than assumed:
//
//  1. ONE API RESPONSE IS WRITTEN AS SEVERAL ROWS -- one per content block, distinguished by
//     apiBlockIndex and sharing a message.id -- and the usage object is REPEATED IN FULL on each.
//     In a 10,109-row transcript there were 5,708 distinct ids, so summing per row instead of per id
//     nearly doubles every number. Usage was byte-identical across rows sharing an id in every case,
//     so taking the first and discarding the rest is safe.
//
//  2. Tool calls follow the OPPOSITE rule. Each row holds one content block, so a tool_use appears
//     on exactly one row of the group; counting only the first row per id found 2,291 of the 5,802
//     actually present. Tokens are per message, tool calls are per row.
//
// Getting either backwards produces a number that looks plausible and is wrong by ~2x, which is why
// both have tests.

// transcriptRow is the subset of a transcript line this code reads.
type transcriptRow struct {
	Type        string `json:"type"`
	IsSidechain bool   `json:"isSidechain"`
	Effort      string `json:"effort"`
	SessionId   string `json:"sessionId"`
	ApiBlockIdx int    `json:"apiBlockIndex"`
	Message     struct {
		Id      string `json:"id"`
		Model   string `json:"model"`
		Content []struct {
			Type string `json:"type"`
		} `json:"content"`
		Usage struct {
			InputTokens              int64  `json:"input_tokens"`
			OutputTokens             int64  `json:"output_tokens"`
			CacheCreationInputTokens int64  `json:"cache_creation_input_tokens"`
			CacheReadInputTokens     int64  `json:"cache_read_input_tokens"`
			ServiceTier              string `json:"service_tier"`
		} `json:"usage"`
	} `json:"message"`
}

// usageLine is one (model, band, service tier) group: exactly what the server stores as a row and
// prices under a single pricing entry.
//
// Internal only -- the wire payload is built field by field in reportUsagePayload, because the
// server takes turns and tool calls on the REPORT rather than on the line.
type usageLine struct {
	Model                   string
	ContextBand             string
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

// transcriptDelta is everything parsed out of one pass over the new part of a transcript.
type transcriptDelta struct {
	Lines []usageLine
	// EndOffset is the byte offset after the last line parsed: the next run's starting point and
	// the clientSeq this delta is sent under.
	EndOffset       int64
	ReasoningLevel  string
	SidechainRows   int
	ClaudeSessionId string
	// The recorded offset was past the end of the file, so this is a re-read from the start. The
	// sequence derived from it will be LOWER than the session's high-water mark, which the server
	// refuses by design -- so the caller must not keep resending it under the old session.
	Truncated bool
}

// contextBands are the thresholds at which pricing changes, by model family. Anthropic's current
// models price differently above a 200k request context.
//
// A static table in the CLI rather than a server lookup: the hook must work with the server
// unreachable, and a band the CLI does not know is not a failure -- every line carries its max and
// min request context, so the server can price it correctly and see that the band label came from
// an older table than its own.
var contextBands = map[string][]int64{
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

// bandLabel names the band a request context falls in: "0" below the first threshold, then the
// threshold itself in compact form ("200k"). The label is only an identifier the server groups on;
// the authoritative numbers travel as max/min request context on the line.
func bandLabel(model string, contextTokens int64) string {
	bands := bandsForModel(model)
	label := "0"
	for _, b := range bands {
		if contextTokens >= b {
			label = compactTokens(b)
		}
	}
	return label
}

func compactTokens(n int64) string {
	switch {
	case n >= 1_000_000 && n%1_000_000 == 0:
		return fmt.Sprintf("%dm", n/1_000_000)
	case n >= 1000 && n%1000 == 0:
		return fmt.Sprintf("%dk", n/1000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

// parseTranscript reads the transcript from sinceOffset to EOF and returns the delta.
//
// sinceOffset is a byte offset, not a line count: the file is appended to while the session runs,
// so the only stable resume point is where the last parse stopped.
func parseTranscript(path string, sinceOffset int64) (*transcriptDelta, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	size, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return nil, err
	}
	truncated := false
	if sinceOffset > size {
		// The transcript is shorter than where we left off: a different session reusing the path,
		// or a truncated file. Re-read from the start rather than trusting a stale offset that
		// would skip real usage forever -- but flag it, because the sequence this produces is
		// below the server's high-water mark for the old session and will be refused.
		sinceOffset = 0
		truncated = true
	}
	if _, err := f.Seek(sinceOffset, io.SeekStart); err != nil {
		return nil, err
	}

	delta := &transcriptDelta{EndOffset: sinceOffset, Truncated: truncated}
	// Grouped by (model, band). Two parallel maps rather than one struct map because tokens are
	// accumulated per distinct message id while tool calls are accumulated per row.
	type groupKey struct{ model, band, tier string }
	groups := map[groupKey]*usageLine{}
	seenIds := map[string]bool{}
	// A message id's group, so later rows of the same message add their tool calls to the same
	// line the tokens went to.
	idGroup := map[string]groupKey{}

	sc := bufio.NewScanner(f)
	// Transcript lines carry whole assistant messages and run well past the 64 KiB default.
	sc.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
	offset := sinceOffset
	for sc.Scan() {
		lineBytes := sc.Bytes()
		// +1 for the newline the scanner strips. The offset must land after the line so a resume
		// does not re-read it.
		offset += int64(len(lineBytes)) + 1

		// Decoded in two steps, and the reason is not style. A transcript holds rows of many
		// shapes, and they disagree about types: a user row's message.content is a STRING where an
		// assistant row's is an ARRAY. Unmarshalling every line into the assistant struct fails on
		// the fourth line of a real transcript, and failing there used to stop the whole parse --
		// so the parser returned nothing at all for any genuine session while every unit test
		// passed. Only assistant rows are decoded into the typed struct.
		var probe struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(lineBytes, &probe); err != nil {
			// Not valid JSON. Either a partially written final line -- normal, the hook can fire
			// while Claude Code is still appending -- or a corrupt one mid-file. EndOffset is NOT
			// advanced past it, so if it was the last line the next run re-reads it whole; if more
			// lines follow, a later good line advances the offset past it and it is skipped.
			continue
		}
		if probe.Type != "assistant" {
			delta.EndOffset = offset
			continue
		}
		var row transcriptRow
		if err := json.Unmarshal(lineBytes, &row); err != nil {
			// An assistant row this version cannot map. Skipped rather than fatal: losing one
			// row's tokens beats losing the session's.
			delta.EndOffset = offset
			continue
		}
		if row.Message.Id == "" {
			delta.EndOffset = offset
			continue
		}
		if row.Effort != "" {
			delta.ReasoningLevel = row.Effort
		}
		if row.SessionId != "" {
			delta.ClaudeSessionId = row.SessionId
		}
		if row.IsSidechain {
			// Subagent turns are written into the PARENT's transcript with this flag. They are
			// real spend on this session, so they are counted here rather than dropped; the count
			// travels with the report so the server can see the session had subagent activity.
			delta.SidechainRows++
		}

		model := row.Message.Model
		if model == "" {
			model = "unknown"
		}
		u := row.Message.Usage
		first := !seenIds[row.Message.Id]

		var key groupKey
		if first {
			ctx := u.InputTokens + u.CacheReadInputTokens + u.CacheCreationInputTokens
			key = groupKey{model: model, band: bandLabel(model, ctx), tier: u.ServiceTier}
			idGroup[row.Message.Id] = key
			seenIds[row.Message.Id] = true

			line := groups[key]
			if line == nil {
				line = &usageLine{
					Model: model, ContextBand: key.band,
					MinRequestContextTokens: ctx,
					ServiceTier:             u.ServiceTier,
				}
				groups[key] = line
			}
			line.Requests++
			// Turns and requests are the same count here -- one API request is one turn in a
			// transcript. Kept as separate fields because a non-transcript source can report
			// turns without knowing its request count.
			line.Turns++
			line.InputTokens += u.InputTokens
			line.OutputTokens += u.OutputTokens
			line.CacheReadTokens += u.CacheReadInputTokens
			line.CacheWriteTokens += u.CacheCreationInputTokens
			if ctx > line.MaxRequestContextTokens {
				line.MaxRequestContextTokens = ctx
			}
			if ctx < line.MinRequestContextTokens {
				line.MinRequestContextTokens = ctx
			}
		} else {
			key = idGroup[row.Message.Id]
		}

		// Every row, first or not: one content block per row, so a tool_use is only ever on one of
		// them. Counting these per message id instead would lose most of them.
		if line := groups[key]; line != nil {
			for _, c := range row.Message.Content {
				if c.Type == "tool_use" {
					line.ToolCalls++
				}
			}
		}
		delta.EndOffset = offset
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("reading transcript %s: %w", path, err)
	}

	// Stable order so a retried report produces byte-identical lines.
	keys := make([]groupKey, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].model != keys[j].model {
			return keys[i].model < keys[j].model
		}
		if keys[i].band != keys[j].band {
			return keys[i].band < keys[j].band
		}
		return keys[i].tier < keys[j].tier
	})
	for _, k := range keys {
		delta.Lines = append(delta.Lines, *groups[k])
	}
	return delta, nil
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
