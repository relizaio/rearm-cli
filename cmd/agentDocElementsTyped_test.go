package cmd

import (
	"strings"
	"testing"

	"github.com/relizaio/rearm/internal/elements"
)

// Grammar 1.2 through the command layer (task RD4-6): the board's lists decide what a document defines,
// read from effectiveElementFamilyEntries when the server serves them.

const reviewItemNote = "# Notes\n\n## T-1: the flaky test is fixed\n\nThe retry waits for the lock.\n"

// withFamilyEntries stands in for the server's effectiveElementFamilyEntries; ok false is an older server.
func withFamilyEntries(t *testing.T, fams map[string]elements.Family, ok bool) {
	t.Helper()
	saved := readFamilyEntries
	readFamilyEntries = func(string) (map[string]elements.Family, bool) { return fams, ok }
	t.Cleanup(func() { readFamilyEntries = saved })
}

var typedBoard = map[string]interface{}{"uuid": "b1"}

func TestANoteNamingAReviewItemDefinesNothingAndSendsTheReference(t *testing.T) {
	withFamilyEntries(t, elements.Defaults(), true)
	extra, ix, err := elementsInput("DETAILED_DESIGN", []byte(reviewItemNote), typedBoard)
	if err != nil {
		t.Fatal(err)
	}
	if extra == nil || len(ix.Elements) != 0 || len(ix.References) != 1 || ix.References[0].ID != "T-1" {
		t.Fatalf("a note's T heading is one reference and no definition: %+v", ix)
	}
	body, _ := extra["elements"].(string)
	if !strings.Contains(body, `"references":[{"id":"T-1","family":"test"`) || !strings.Contains(body, `"grammarVersion":"1.2"`) {
		t.Errorf("wire form: %s", body)
	}
}

func TestATestReportSendsItsTestsOnlyToAServerThatServesTheLists(t *testing.T) {
	withFamilyEntries(t, elements.Defaults(), true)
	extra, ix, _ := elementsInput("BOARD_TEST_REPORT", []byte(reviewItemNote), typedBoard)
	if extra == nil || len(ix.Elements) != 1 || ix.Elements[0].ID != "T-1" {
		t.Fatalf("a test report defines its test ids: %+v", ix)
	}
	withFamilyEntries(t, nil, false)
	if extra, _, _ := elementsInput("BOARD_TEST_REPORT", []byte(reviewItemNote), typedBoard); extra != nil {
		t.Errorf("an older server refuses elements on an index type, so none are sent: %v", extra)
	}
	if extra, ix, _ := elementsInput("DETAILED_DESIGN", []byte(reviewItemNote), typedBoard); extra == nil || len(ix.Elements) != 0 {
		t.Errorf("a prose type still sends, sorted by the default lists: %+v", ix)
	}
}

func TestTheBoardsOwnListsWin(t *testing.T) {
	fams := elements.Defaults()
	fams["T"] = elements.Family{Name: "test", DefinedIn: []string{"DETAILED_DESIGN"}}
	withFamilyEntries(t, fams, true)
	_, ix, _ := elementsInput("DETAILED_DESIGN", []byte(reviewItemNote), typedBoard)
	if len(ix.Elements) != 1 || len(ix.References) != 0 {
		t.Errorf("a board that lists DETAILED_DESIGN for T keeps the note's T-1 a definition: %+v", ix)
	}
	if _, ix, _ := elementsInput("BOARD_TEST_REPORT", []byte(reviewItemNote), typedBoard); len(ix.Elements) != 0 || len(ix.References) != 1 {
		t.Errorf("and a test report then references T-1 (TEST still defines there): %+v", ix)
	}
	fams["TEST"] = elements.Family{Name: "test", DefinedIn: []string{}}
	if extra, _, _ := elementsInput("BOARD_TEST_REPORT", []byte(reviewItemNote), typedBoard); extra != nil {
		t.Errorf("with TEST emptied too a test report defines nothing, so it sends nothing: %v", extra)
	}
}

func TestFamilyEntriesReadTheServersShape(t *testing.T) {
	got := familyEntries([]interface{}{
		map[string]interface{}{"prefix": "T", "family": "test", "definedIn": []interface{}{"TEST_PLAN"}},
		map[string]interface{}{"prefix": "RISK", "family": "risk", "definedIn": []interface{}{}},
		map[string]interface{}{"family": "no prefix"},
	})
	if len(got) != 2 || got["T"].Name != "test" || strings.Join(got["T"].DefinedIn, ",") != "TEST_PLAN" {
		t.Errorf("entries: %+v", got)
	}
	if l := got["RISK"].DefinedIn; l == nil || len(l) != 0 {
		t.Errorf("an empty list stays empty (references only), not the default: %#v", l)
	}
}

func TestTheTypeIsReadOffTheBoardsPathTemplates(t *testing.T) {
	templates := map[string]interface{}{
		"ARCHITECTURE":      "design/{key}/architecture-{round}.md",
		"DETAILED_DESIGN":   "impl/{key}/notes-{round}.md",
		"BOARD_TEST_REPORT": "tests/{key}/run-{round}.md",
	}
	root := "boards/rearm-dogfood-4/"
	if got := typeOfPath("/tmp/docs/boards/rearm-dogfood-4/impl/RD4-6/notes-1.md", templates, root); got != "DETAILED_DESIGN" {
		t.Errorf("notes: %q", got)
	}
	if got := typeOfPath("/tmp/docs/boards/rearm-dogfood-4/tests/RD4-6/run-12.md", templates, root); got != "BOARD_TEST_REPORT" {
		t.Errorf("run: %q", got)
	}
	if got := typeOfPath("/tmp/docs/boards/other-board/impl/RD4-6/notes-1.md", templates, root); got != "" {
		t.Errorf("another board's root matches nothing: %q", got)
	}
	if got := typeOfPath("/tmp/docs/impl/RD4-6/notes-x.md", templates, ""); got != "" {
		t.Errorf("a round that is not a number matches nothing: %q", got)
	}
	both := map[string]interface{}{"ARCHITECTURE": "{key}/doc-{round}.md", "DETAILED_DESIGN": "{key}/doc-{round}.md"}
	if got := typeOfPath("/x/RD4-6/doc-1.md", both, ""); got != "" {
		t.Errorf("two matching templates are ambiguous: %q", got)
	}
}

func TestDefinitionsAndReferencesArePrintedApart(t *testing.T) {
	ix := elements.Parse([]byte("## REQ-1 Lock\n\n## T-1: fixed\n"), "ARCHITECTURE", elements.Defaults(), nil)
	lines := definitionsAndReferences(&ix)
	if len(lines) != 2 || lines[0] != "  defines: REQ-1" || lines[1] != "  references: T-1 (test, line 3)" {
		t.Errorf("listing: %q", lines)
	}
	if s := summarise(&ix); s != "1 element(s), 0 warning(s), 1 reference(s)" {
		t.Errorf("summary: %q", s)
	}
	plain := elements.Parse([]byte("## REQ-1 Lock\n"), "ARCHITECTURE", elements.Defaults(), nil)
	if s := summarise(&plain); s != "1 element(s), 0 warning(s)" {
		t.Errorf("without references the summary reads as before: %q", s)
	}
}
