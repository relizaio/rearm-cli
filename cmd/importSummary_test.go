package cmd

import (
	"strings"
	"testing"
)

// Unit tests of the import outcome and its printed form (task SAFE-16, design SAFE-9 3.6, test list
// 4.3 first paragraph).

func n(v int64) *int64 { return &v }

// One case per row of the outcome table of design 3.6.
func TestClassifyImportFollowsTheOrderedTable(t *testing.T) {
	imported := &importSummaryRead{ProposalsAutoAccepted: n(1)}
	for _, c := range []struct {
		name    string
		typ     string
		summary *importSummaryRead
		state   importState
		want    importOutcome
	}{
		{"row 1 nested, even with a summary", artifactTypeVex, imported, stateNested, outcomeNotImported},
		{"row 2 source code entry, even with a summary", artifactTypeVex, imported, stateSce, outcomeNotReportedSce},
		{"row 3 server not supported", artifactTypeVex, imported, stateUnsupported, outcomeNotReported},
		{"row 4 attribution failed", artifactTypeVex, imported, stateNotFound, outcomeNotFound},
		{"row 5 summary null", artifactTypeVex, nil, stateAttributed, outcomeNoResult},
		{"row 6 imported", artifactTypeVex, imported, stateAttributed, outcomeImported},
		{"row 7 errored", artifactTypeVex, &importSummaryRead{StatementsErrored: n(2), StatementsUnmatched: n(1)}, stateAttributed, outcomeNothingErrored},
		{"row 8 unmatched", artifactTypeVex, &importSummaryRead{StatementsUnmatched: n(1)}, stateAttributed, outcomeNothingUnmatched},
		{"row 9 nothing read", artifactTypeVex, &importSummaryRead{}, stateAttributed, outcomeNothingRead},
	} {
		if got := classifyImport(c.typ, c.summary, c.state); got != c.want {
			t.Errorf("%s: want %q, got %q", c.name, c.want.text(), got.text())
		}
	}
}

// The count sets of test list 4.3 and the imported formula of design 3.6.
func TestClassifyImportCountSets(t *testing.T) {
	msg := "The document declares no safety integrity levels; nothing was imported"
	for _, c := range []struct {
		name    string
		typ     string
		summary *importSummaryRead
		want    importOutcome
	}{
		{"SAFETY_CONTEXT created 3 unmatched 3", artifactTypeSafetyContext, &importSummaryRead{ProposalsCreated: n(3), StatementsUnmatched: n(3)}, outcomeNothingUnmatched},
		{"SAFETY_CONTEXT created 3 unmatched 1", artifactTypeSafetyContext, &importSummaryRead{ProposalsCreated: n(3), StatementsUnmatched: n(1)}, outcomeImported},
		{"VEX created 0 unmatched 2", artifactTypeVex, &importSummaryRead{ProposalsCreated: n(0), StatementsUnmatched: n(2)}, outcomeNothingUnmatched},
		{"VEX created 2 unmatched 2", artifactTypeVex, &importSummaryRead{ProposalsCreated: n(2), StatementsUnmatched: n(2)}, outcomeImported},
		{"unchanged 4 only", artifactTypeSafetyContext, &importSummaryRead{StatementsUnchanged: n(4)}, outcomeImported},
		{"auto-rejected 1 only", artifactTypeVex, &importSummaryRead{ProposalsAutoRejected: n(1)}, outcomeImported},
		{"total 1 errored 1", artifactTypeSafetyContext, &importSummaryRead{StatementsTotal: n(1), StatementsErrored: n(1)}, outcomeNothingErrored},
		{"all zero with a message", artifactTypeSafetyContext, &importSummaryRead{StatementsTotal: n(0), ProposalsCreated: n(0), ErrorMessages: []*string{&msg}}, outcomeNothingRead},
		{"SAFETY_CONTEXT created 2 awaiting 2 unmatched 0", artifactTypeSafetyContext, &importSummaryRead{ProposalsCreated: n(2), StatementsAwaitingReconcile: n(2), StatementsUnmatched: n(0)}, outcomeImported},
		{"SAFETY_CONTEXT created 2 unmatched 2, no awaiting field", artifactTypeSafetyContext, &importSummaryRead{ProposalsCreated: n(2), StatementsUnmatched: n(2)}, outcomeNothingUnmatched},
		{"unmatched greater than created floors at 0", artifactTypeSafetyContext, &importSummaryRead{ProposalsCreated: n(1), StatementsUnmatched: n(5)}, outcomeNothingUnmatched},
	} {
		if got := classifyImport(c.typ, c.summary, stateAttributed); got != c.want {
			t.Errorf("%s: want %q, got %q", c.name, c.want.text(), got.text())
		}
	}
	if got := importedCount(artifactTypeSafetyContext, &importSummaryRead{ProposalsCreated: n(1), StatementsUnmatched: n(5)}); got != 0 {
		t.Errorf("the imported count floors at 0, got %d", got)
	}
}

func TestRenderedValuesStayOnOneLine(t *testing.T) {
	if got := renderText("a\nb\x1b[2Jc"); got != `"a\nb\x1b[2Jc"` {
		t.Errorf("control characters: %s", got)
	}
	if got := renderText("x\u202ey\r"); got != `"x\u202ey\r"` {
		t.Errorf("format characters: %s", got)
	}
	long := strings.Repeat("m", 1000)
	if got := renderText(long); got != `"`+strings.Repeat("m", 300)+`" (cut, 1000 characters)` {
		t.Errorf("cut: %s", got)
	}
	if got := renderToken("NO_FINDING_ANALYSIS_WRITE"); got != "NO_FINDING_ANALYSIS_WRITE" {
		t.Errorf("token bare: %s", got)
	}
	if got := renderToken("bad reason"); got != `"bad reason"` {
		t.Errorf("token quoted: %s", got)
	}
	if got := renderUuid("x"); got != `"x"` {
		t.Errorf("uuid quoted: %s", got)
	}
	if got := renderUuid(detailUuid(1)); got != detailUuid(1) {
		t.Errorf("uuid bare: %s", got)
	}
}

func TestFinalImportLine(t *testing.T) {
	for _, c := range []struct {
		k, f  int
		allow bool
		want  string
	}{
		{2, 0, false, "import check passed: 2 of 2 VEX/SAFETY_CONTEXT artifacts imported"},
		{3, 2, false, "import check failed: 2 of 3 VEX/SAFETY_CONTEXT artifacts imported nothing or have no known result; the artifacts are stored (exit 3). Pass --allow-import-failure to accept this."},
		{3, 2, true, "import check: 2 of 3 VEX/SAFETY_CONTEXT artifacts imported nothing or have no known result; accepted by --allow-import-failure"},
	} {
		if got := finalImportLine(c.k, c.f, c.allow); got != c.want {
			t.Errorf("want %q, got %q", c.want, got)
		}
	}
}

// The refusal of the commands whose mutation runs no import (design 3.4a), on its own.
func TestNonImportingEntriesCheck(t *testing.T) {
	entries := collectRefEntries("--releasearts", []Artifact{
		{Type: "BOM", Artifacts: []Artifact{{Type: "SAFETY_CONTEXT"}}},
		{Type: "VEX"},
	})
	if len(entries) != 3 || entries[1].ref != "--releasearts[0].artifacts[0]" || entries[2].ref != "--releasearts[1]" {
		t.Fatalf("refs: %+v", entries)
	}
	_, err := nonImportingEntriesCheck("addrelease", entries, false)
	want := "--releasearts[0].artifacts[0]: addrelease stores a SAFETY_CONTEXT artifact but never imports it, so nothing would take effect; upload it with addartifact once the release's SBOM is indexed, or pass --allow-import-failure to store it without an import"
	if err == nil || err.Error() != want {
		t.Fatalf("want %q, got %v", want, err)
	}
	notices, err := nonImportingEntriesCheck("addrelease", entries, true)
	if err != nil || len(notices) != 2 ||
		notices[1] != "notice: --releasearts[1]: addrelease stores this VEX artifact but does not import it (accepted by --allow-import-failure)" {
		t.Fatalf("notices: %q %v", notices, err)
	}
	if notices, err := nonImportingEntriesCheck("addrelease", collectRefEntries("--releasearts", []Artifact{{Type: "BOM"}}), false); err != nil || notices != nil {
		t.Errorf("BOM only: %q %v", notices, err)
	}
	// the type is compared exactly, as the server's enum is
	if _, err := nonImportingEntriesCheck("addrelease", collectRefEntries("--releasearts", []Artifact{{Type: "vex"}}), false); err != nil {
		t.Errorf("a lower-case type is not VEX: %v", err)
	}
}
