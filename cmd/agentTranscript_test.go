package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTranscript writes JSONL lines to a temp file and returns the path.
func writeTranscript(t *testing.T, lines ...string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "transcript.jsonl")
	body := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// assistantRow builds one transcript row. Several rows sharing an id is how Claude Code writes a
// single API response: one row per content block, with the usage repeated in full on each.
func assistantRow(id, model, block string, blockIdx int, in, out, cacheRead, cacheWrite int64) string {
	return fmt.Sprintf(`{"type":"assistant","apiBlockIndex":%d,"effort":"high","sessionId":"cs-1","message":{"id":%q,"model":%q,"content":[{"type":%q}],"usage":{"input_tokens":%d,"output_tokens":%d,"cache_read_input_tokens":%d,"cache_creation_input_tokens":%d,"service_tier":"standard"}}}`,
		blockIdx, id, model, block, in, out, cacheRead, cacheWrite)
}

func TestUsageIsCountedOncePerMessageNotOncePerRow(t *testing.T) {
	// One API response written as three rows, as Claude Code actually writes it. Summing per row
	// would treble every token count here; on a real transcript it was a ~2x overcount (10,109
	// rows for 5,708 messages), which is large enough to be believed and wrong enough to matter.
	path := writeTranscript(t,
		assistantRow("msg_1", "claude-opus-5", "thinking", 0, 100, 50, 1000, 200),
		assistantRow("msg_1", "claude-opus-5", "text", 1, 100, 50, 1000, 200),
		assistantRow("msg_1", "claude-opus-5", "tool_use", 2, 100, 50, 1000, 200),
	)
	d, err := parseTranscript(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Lines) != 1 {
		t.Fatalf("expected one line, got %d", len(d.Lines))
	}
	l := d.Lines[0]
	if l.InputTokens != 100 || l.OutputTokens != 50 || l.CacheReadTokens != 1000 || l.CacheWriteTokens != 200 {
		t.Errorf("tokens counted per row instead of per message: %+v", l)
	}
	if l.Requests != 1 || l.Turns != 1 {
		t.Errorf("expected 1 request/turn for one message, got %d/%d", l.Requests, l.Turns)
	}
}

func TestToolCallsAreCountedPerRowNotPerMessage(t *testing.T) {
	// The opposite rule, and the reason it needs its own test: each row carries ONE content block,
	// so a tool_use sits on exactly one row of the group. Applying the token rule here (first row
	// per message) found 2,291 of the 5,802 tool calls in a real transcript.
	path := writeTranscript(t,
		assistantRow("msg_1", "claude-opus-5", "thinking", 0, 10, 5, 0, 0),
		assistantRow("msg_1", "claude-opus-5", "tool_use", 1, 10, 5, 0, 0),
		assistantRow("msg_1", "claude-opus-5", "tool_use", 2, 10, 5, 0, 0),
	)
	d, err := parseTranscript(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := d.Lines[0].ToolCalls; got != 2 {
		t.Errorf("expected 2 tool calls across the rows of one message, got %d", got)
	}
}

func TestModelsAndBandsSplitIntoSeparateLines(t *testing.T) {
	path := writeTranscript(t,
		assistantRow("m1", "claude-opus-5", "text", 0, 10, 5, 0, 0),
		assistantRow("m2", "claude-fable-5-1", "text", 0, 20, 5, 0, 0),
		// Over the 200k threshold: same model as m1, different pricing band.
		assistantRow("m3", "claude-opus-5", "text", 0, 1000, 5, 250000, 0),
	)
	d, err := parseTranscript(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Lines) != 3 {
		t.Fatalf("expected 3 lines (2 models, one crossing a band), got %d: %+v", len(d.Lines), d.Lines)
	}
	var banded *usageLine
	for i := range d.Lines {
		if d.Lines[i].ContextBand == "200k" {
			banded = &d.Lines[i]
		}
	}
	if banded == nil {
		t.Fatalf("no line landed in the 200k band: %+v", d.Lines)
	}
	// The band is chosen on the FULL request context, not on input_tokens: a cached session has a
	// tiny input_tokens and a huge context, and pricing follows the context.
	if banded.MaxRequestContextTokens != 251000 {
		t.Errorf("band should follow input+cacheRead+cacheWrite, got max context %d", banded.MaxRequestContextTokens)
	}
}

func TestANonStandardServiceTierRidesOnTheModelString(t *testing.T) {
	// The transcript keeps the tier beside the model rather than in it, but tier is a pricing
	// dimension. Folding it into the model string as the suffix the server peels is what gets a
	// batch request priced at batch rates instead of standard.
	row := strings.Replace(
		assistantRow("m1", "claude-opus-5", "text", 0, 10, 5, 0, 0),
		`"service_tier":"standard"`, `"service_tier":"batch"`, 1)
	path := writeTranscript(t, row)
	d, err := parseTranscript(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := d.Lines[0].wireModel(); got != "claude-opus-5-batch" {
		t.Errorf("expected the tier folded into the model string, got %q", got)
	}
	// Standard is the default and must NOT be appended, or every ordinary request would resolve
	// to a model string no catalogue entry matches.
	std := writeTranscript(t, assistantRow("m1", "claude-opus-5", "text", 0, 10, 5, 0, 0))
	d2, _ := parseTranscript(std, 0)
	if got := d2.Lines[0].wireModel(); got != "claude-opus-5" {
		t.Errorf("standard tier must not be appended, got %q", got)
	}
}

func TestResumingFromAnOffsetSkipsWhatWasAlreadyReported(t *testing.T) {
	path := writeTranscript(t,
		assistantRow("m1", "claude-opus-5", "text", 0, 10, 5, 0, 0),
		assistantRow("m2", "claude-opus-5", "text", 0, 20, 5, 0, 0),
	)
	first, err := parseTranscript(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if first.Lines[0].InputTokens != 30 {
		t.Fatalf("first pass should see both messages, got %d", first.Lines[0].InputTokens)
	}
	// A second pass from the recorded offset has nothing new to send. This is what stops every
	// turn from re-reporting the whole session.
	second, err := parseTranscript(path, first.EndOffset)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Lines) != 0 {
		t.Errorf("resuming at the end offset should yield nothing, got %+v", second.Lines)
	}
}

func TestAnOffsetPastTheEndRereadsFromTheStart(t *testing.T) {
	// A shorter file than the recorded offset means the path was reused by a different session or
	// truncated. Re-reading risks a duplicate, which the server drops; trusting the offset would
	// skip real usage permanently. The cheap mistake is the right one.
	path := writeTranscript(t, assistantRow("m1", "claude-opus-5", "text", 0, 10, 5, 0, 0))
	d, err := parseTranscript(path, 999999)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Lines) != 1 {
		t.Errorf("expected a re-read from the start, got %d lines", len(d.Lines))
	}
}

func TestAHalfWrittenFinalLineIsLeftForNextTime(t *testing.T) {
	// The Stop hook can fire while Claude Code is still appending. The truncated line must not be
	// skipped: the offset stops before it so the next run reads it whole.
	good := assistantRow("m1", "claude-opus-5", "text", 0, 10, 5, 0, 0)
	path := writeTranscript(t, good, `{"type":"assistant","message":{"id":"m2","mod`)
	d, err := parseTranscript(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Lines) != 1 || d.Lines[0].InputTokens != 10 {
		t.Errorf("only the complete line should be counted, got %+v", d.Lines)
	}
	if d.EndOffset != int64(len(good))+1 {
		t.Errorf("offset should stop before the partial line, got %d want %d", d.EndOffset, len(good)+1)
	}
}

func TestSubagentTurnsAreCountedAndFlagged(t *testing.T) {
	// Subagent turns are written into the PARENT's transcript with isSidechain. They are real spend
	// on this session, so they count; the flag travels so the number is explainable.
	side := strings.Replace(assistantRow("m2", "claude-opus-5", "text", 0, 70, 5, 0, 0),
		`"type":"assistant"`, `"type":"assistant","isSidechain":true`, 1)
	path := writeTranscript(t, assistantRow("m1", "claude-opus-5", "text", 0, 30, 5, 0, 0), side)
	d, err := parseTranscript(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if d.Lines[0].InputTokens != 100 {
		t.Errorf("subagent turns should be counted into the session, got %d", d.Lines[0].InputTokens)
	}
	if d.SidechainRows != 1 {
		t.Errorf("expected the sidechain row to be flagged, got %d", d.SidechainRows)
	}
}

func TestReasoningLevelComesOffTheTranscript(t *testing.T) {
	path := writeTranscript(t, assistantRow("m1", "claude-opus-5", "text", 0, 10, 5, 0, 0))
	d, _ := parseTranscript(path, 0)
	if d.ReasoningLevel != "high" {
		t.Errorf("expected effort read from the row, got %q", d.ReasoningLevel)
	}
}

func TestChunkingSplitsABackfillByRequestCount(t *testing.T) {
	lines := []usageLine{
		{Model: "a", Requests: 300},
		{Model: "b", Requests: 300},
		{Model: "c", Requests: 300},
	}
	chunks := chunkLines(lines, 500)
	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks under a 500-request limit, got %d", len(chunks))
	}
	// A single line over the limit is not split: a line is the unit the server prices, and the
	// server's guard scales with the request count, so it is accepted whole.
	big := chunkLines([]usageLine{{Model: "a", Requests: 900}}, 500)
	if len(big) != 1 {
		t.Errorf("an oversized single line should go on its own, got %d chunks", len(big))
	}
}

func TestLineOrderIsStableSoARetryIsIdentical(t *testing.T) {
	// Map iteration in Go is randomised, so without the sort a retried report would send the same
	// lines in a different order each time. The server keys rows on (session, seq, model, hosting,
	// band) so it would still dedupe, but a report that is not byte-stable is a nuisance to
	// diff and to trust.
	path := writeTranscript(t,
		assistantRow("m1", "claude-opus-5", "text", 0, 10, 5, 0, 0),
		assistantRow("m2", "claude-fable-5-1", "text", 0, 20, 5, 0, 0),
		assistantRow("m3", "claude-haiku-4-5", "text", 0, 30, 5, 0, 0),
	)
	var first []string
	for i := 0; i < 8; i++ {
		d, err := parseTranscript(path, 0)
		if err != nil {
			t.Fatal(err)
		}
		var order []string
		for _, l := range d.Lines {
			order = append(order, l.Model+"/"+l.ContextBand)
		}
		if i == 0 {
			first = order
			continue
		}
		if strings.Join(order, ",") != strings.Join(first, ",") {
			t.Fatalf("line order is not stable: %v vs %v", order, first)
		}
	}
}

func TestRowsOfOtherShapesDoNotHaltTheParse(t *testing.T) {
	// A transcript is not homogeneous, and its rows disagree about types: a user row's
	// message.content is a STRING where an assistant row's is an ARRAY. This appears on line 4 of
	// a real transcript. Decoding every line into the assistant struct fails there, and when that
	// failure stopped the parse the CLI reported zero usage for every genuine session -- while all
	// the hand-built fixtures above passed, because they only ever contained assistant rows.
	path := writeTranscript(t,
		`{"type":"ai-title","aiTitle":"something","sessionId":"cs-1"}`,
		`{"type":"user","message":{"role":"user","content":"a plain string, not an array"}}`,
		`{"type":"system","subtype":"info","content":"whatever"}`,
		assistantRow("m1", "claude-opus-5", "text", 0, 10, 5, 0, 0),
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result"}]}}`,
		assistantRow("m2", "claude-opus-5", "text", 0, 20, 5, 0, 0),
	)
	d, err := parseTranscript(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Lines) != 1 {
		t.Fatalf("expected one line, got %d: %+v", len(d.Lines), d.Lines)
	}
	if d.Lines[0].InputTokens != 30 || d.Lines[0].Requests != 2 {
		t.Errorf("both assistant rows should be counted past the other shapes, got %+v", d.Lines[0])
	}
}

func TestACorruptLineMidFileIsSkippedNotFatal(t *testing.T) {
	// Distinct from a truncated FINAL line, which must be left for the next run. A broken line with
	// good lines after it must not cost us everything that follows.
	path := writeTranscript(t,
		assistantRow("m1", "claude-opus-5", "text", 0, 10, 5, 0, 0),
		`{"type":"assistant","message":{"id":"broken`,
		assistantRow("m2", "claude-opus-5", "text", 0, 20, 5, 0, 0),
	)
	d, err := parseTranscript(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if d.Lines[0].Requests != 2 {
		t.Errorf("the line after the corrupt one should still be counted, got %+v", d.Lines[0])
	}
}
