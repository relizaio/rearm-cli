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
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Spawned contexts (task RD3-12).
//
// A context Claude Code spawns -- a subagent, a fresh context working one board task -- writes its
// turns to a file of its own, <dir of the transcript>/<session id>/subagents/agent-<id>.jsonl, with
// the parent's sessionId and usage on every assistant row, and never into the parent transcript.
// Observed on 2026-09-28. A report built from the parent alone therefore leaves out everything a
// spawned context spent.
//
// So a report sweeps the parent and every subagent file present at that moment, each from its own
// byte offset (agentSessionState.TranscriptOffsets), and sends the sum as one report. The report's
// sequence can no longer be one file's offset: it is the previous sequence plus the bytes newly read
// across all files, which only grows even when a file disappears, and which equals today's value
// (the parent's offset) for a session that never spawned anything.

// claudeSubagentDir is where a session's spawned contexts write: beside the parent transcript, in a
// directory named after the transcript's own name.
func claudeSubagentDir(parent string) string {
	return filepath.Join(filepath.Dir(parent), strings.TrimSuffix(filepath.Base(parent), ".jsonl"), "subagents")
}

// claudeSubagentFiles lists the subagent transcripts present now, in a stable order. None is the
// usual case for a session that has not spawned anything.
func claudeSubagentFiles(parent string) []string {
	dir := claudeSubagentDir(parent)
	files, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil || len(files) == 0 {
		if debug == "true" {
			fmt.Fprintf(os.Stderr, "rearm: no subagent transcripts under %s\n", dir)
		}
		return nil
	}
	sort.Strings(files)
	return files
}

// transcriptOffsets is the state's per-file offsets. A state written before this kept one offset,
// the parent's, in LastSeq; it becomes the parent's entry.
func transcriptOffsets(st *agentSessionState, parent string) map[string]int64 {
	out := map[string]int64{}
	for k, v := range st.TranscriptOffsets {
		out[k] = v
	}
	if st.TranscriptOffsets == nil && parent != "" && st.LastSeq > 0 {
		out[parent] = st.LastSeq
	}
	return out
}

// sweepClaudeTranscripts reads the parent transcript and every subagent file from their offsets and
// returns one delta. also names files to include even if the glob misses them (the transcript a
// SubagentStop payload names). The new offsets ride on the delta and are recorded only once the
// report is in, so a failed report resends the whole range.
func sweepClaudeTranscripts(st *agentSessionState, parent string, also ...string) (*usageDelta, error) {
	known := transcriptOffsets(st, parent)
	files := []string{parent}
	seen := map[string]bool{parent: true}
	for _, f := range append(claudeSubagentFiles(parent), also...) {
		if f != "" && !seen[f] {
			if _, err := os.Stat(f); err == nil {
				seen[f] = true
				files = append(files, f)
			}
		}
	}
	// Only the files present now are kept: a removed file leaves the state.
	next := map[string]int64{}
	var parts []*usageDelta
	var advanced int64
	for _, f := range files {
		d, read, err := sweepOne(f, known[f])
		if err != nil {
			if f == parent {
				return nil, err
			}
			// One unreadable subagent file must not cost the parent's report; it is retried next time.
			fmt.Fprintf(os.Stderr, "rearm: could not read %s: %v\n", f, err)
			if off, ok := known[f]; ok {
				next[f] = off
			}
			continue
		}
		if f == parent && d.Truncated {
			// The parent is shorter than recorded: a different session reusing the mapping. Stop, as
			// reportDelta explains, rather than report into the stale session.
			return d, nil
		}
		next[f] = d.EndOffset
		advanced += read
		parts = append(parts, d)
	}
	merged := mergeUsageDeltas(parts)
	merged.EndOffset = st.LastSeq + advanced
	merged.FileOffsets = next
	return merged, nil
}

// sweepOneTranscript reads one file -- the parent or a subagent's -- for --from-transcript. The
// file's offset is kept under its own path; since overrides it when non-negative.
func sweepOneTranscript(st *agentSessionState, file string, since int64) (*usageDelta, error) {
	known := transcriptOffsets(st, st.TranscriptPath)
	from := known[file]
	if since >= 0 {
		from = since
	}
	d, read, err := sweepOne(file, from)
	if err != nil {
		return nil, err
	}
	if d.Truncated && file == st.TranscriptPath {
		return d, nil
	}
	known[file] = d.EndOffset
	d.Truncated = false
	d.EndOffset = st.LastSeq + read
	d.FileOffsets = known
	return d, nil
}

// sweepOne parses one file from its offset and says how many bytes that consumed. A subagent file
// shorter than its offset is re-read from the start by the parser's rule; the bytes read then count
// from zero.
func sweepOne(file string, from int64) (*usageDelta, int64, error) {
	d, err := parseClaudeTranscript(file, from)
	if err != nil {
		return nil, 0, err
	}
	start := from
	if d.Truncated {
		start = 0
	}
	return d, d.EndOffset - start, nil
}

// mergeUsageDeltas sums the per-file deltas line by line on (model, band, service tier), the key a
// line is stored and priced under.
func mergeUsageDeltas(parts []*usageDelta) *usageDelta {
	out := &usageDelta{Extra: map[string]interface{}{}}
	type key struct {
		model, tier string
		band        int64
	}
	byKey := map[key]*usageLine{}
	var order []key
	sidechain, contexts := 0, 0
	for i, p := range parts {
		if p.ReasoningLevel != "" && (out.ReasoningLevel == "" || i == 0) {
			out.ReasoningLevel = p.ReasoningLevel
		}
		if len(p.Lines) > 0 && i > 0 {
			contexts++
		}
		for k, v := range p.Extra {
			switch k {
			case "sidechainRows":
				if n, ok := v.(int); ok {
					sidechain += n
				}
			case "claudeSessionId":
				if _, set := out.Extra[k]; !set {
					out.Extra[k] = v
				}
			default:
				out.Extra[k] = v
			}
		}
		for _, l := range p.Lines {
			k := key{model: l.Model, tier: l.ServiceTier, band: l.ContextBand}
			m := byKey[k]
			if m == nil {
				c := l
				byKey[k] = &c
				order = append(order, k)
				continue
			}
			m.Requests += l.Requests
			m.Turns += l.Turns
			m.ToolCalls += l.ToolCalls
			m.InputTokens += l.InputTokens
			m.OutputTokens += l.OutputTokens
			m.CacheReadTokens += l.CacheReadTokens
			m.CacheWriteTokens += l.CacheWriteTokens
			if l.MaxRequestContextTokens > m.MaxRequestContextTokens {
				m.MaxRequestContextTokens = l.MaxRequestContextTokens
			}
			if l.MinRequestContextTokens < m.MinRequestContextTokens {
				m.MinRequestContextTokens = l.MinRequestContextTokens
			}
		}
	}
	// the same stable order as one file's lines, so a retried report is byte-identical
	sort.Slice(order, func(i, j int) bool {
		if order[i].model != order[j].model {
			return order[i].model < order[j].model
		}
		if order[i].band != order[j].band {
			return order[i].band < order[j].band
		}
		return order[i].tier < order[j].tier
	})
	for _, k := range order {
		out.Lines = append(out.Lines, *byKey[k])
	}
	if sidechain > 0 {
		out.Extra["sidechainRows"] = sidechain
	}
	if contexts > 0 {
		// how many spawned contexts contributed, so the total is explainable later
		out.Extra["spawnedContexts"] = contexts
	}
	return out
}

// claudeSessionIdOf reads the Claude session id a transcript's rows carry -- a subagent file's is
// its parent's -- so a file named by hand maps to the ReARM session the same way a hook does.
func claudeSessionIdOf(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
	for i := 0; i < 200 && sc.Scan(); i++ {
		var row struct {
			SessionId string `json:"sessionId"`
		}
		if json.Unmarshal(sc.Bytes(), &row) == nil && row.SessionId != "" {
			return row.SessionId
		}
	}
	return ""
}
