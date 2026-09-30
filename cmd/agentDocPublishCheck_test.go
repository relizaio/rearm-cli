package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/relizaio/rearm/internal/elements"
)

// doc publish --check (task RD4-6): the index a publish would send goes to the preview, nothing else
// is sent, and a blocking failure is the exit status.

func report(blocking bool, result string) map[string]interface{} {
	return map[string]interface{}{"catalogueVersion": "2026-09.3", "results": []interface{}{
		map[string]interface{}{"check": "ids.family", "result": "PASS", "blocking": false},
		map[string]interface{}{"check": "trace.parent_exists", "result": result, "blocking": blocking,
			"offences": []interface{}{map[string]interface{}{"message": "REQ-12 → REQ-4 not found"}}},
	}}
}

func TestAPreviewSaysNothingWasPublishedAndBlocksOnlyOnABlockingFailure(t *testing.T) {
	lines, blocking := previewSummary(report(true, "FAIL"))
	text := strings.Join(lines, "\n")
	if !blocking || !strings.HasPrefix(text, "check only: nothing was published") ||
		!strings.Contains(text, "FAIL trace.parent_exists [blocking]") ||
		!strings.Contains(text, "a publish now would be refused at sign-off") {
		t.Errorf("blocking=%v\n%s", blocking, text)
	}
	if _, blocking := previewSummary(report(false, "FAIL")); blocking {
		t.Error("a failure the board does not block on is not an exit")
	}
	if _, blocking := previewSummary(report(true, "PASS")); blocking {
		t.Error("a passing blocking check is not an exit")
	}
}

func TestTheCheckSendsThePublishIndexAndOnlyThat(t *testing.T) {
	withFamilyEntries(t, elements.Defaults(), true)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "design.md"), []byte(designDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	var sent map[string]interface{}
	saved := previewChecks
	previewChecks = func(vars map[string]interface{}) (map[string]interface{}, error) {
		sent = vars
		return report(false, "PASS"), nil
	}
	t.Cleanup(func() { previewChecks = saved })
	docTask, docSession = "task-1", "session-1"
	t.Cleanup(func() { docTask, docSession = "", "" })

	if err := runPublishCheck(nil, dir, "design.md", "ARCHITECTURE", typedBoard); err != nil {
		t.Fatal(err)
	}
	want, _, _ := elementsInput("ARCHITECTURE", []byte(designDoc), typedBoard)
	if sent["elements"] != want["elements"] || sent["elementsDigest"] != want["elementsDigest"] ||
		sent["taskUuid"] != "task-1" || sent["sessionUuid"] != "session-1" || sent["specification"] != "ARCHITECTURE" {
		t.Errorf("sent %v", sent)
	}

	sent = nil
	if err := os.WriteFile(filepath.Join(dir, "plain.md"), []byte("# Plain\n\nNo ids.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runPublishCheck(nil, dir, "plain.md", "ARCHITECTURE", typedBoard); err != nil || sent != nil {
		t.Errorf("a document without ids has nothing to check: err %v, sent %v", err, sent)
	}
}

func TestTheCheckNeedsATaskAndAFile(t *testing.T) {
	docSession, docType, docCheck = "session-1", "DETAILED_DESIGN", true
	t.Cleanup(func() {
		docSession, docType, docCheck, docTask, docIndexOnlyFlag, docIndexFile = "", "", false, "", false, ""
	})
	if err := runDocPublish(); err == nil || !strings.Contains(err.Error(), "--check previews") {
		t.Errorf("no --task: %v", err)
	}
	docTask, docType, docIndexOnlyFlag, docIndexFile = "task-1", "QUESTIONS", true, "q.json"
	if err := runDocPublish(); err == nil || !strings.Contains(err.Error(), "--check previews") {
		t.Errorf("--index-only: %v", err)
	}
}
