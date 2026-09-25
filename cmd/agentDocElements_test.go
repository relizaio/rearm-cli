package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

const designDoc = "## REQ-1 Refuse a cycle\n\nA cycle stalls every task in it.\n\n### FN-1 Detect it\ntraces: satisfies REQ-1\n"

func TestAProseDocumentSendsItsElementIndexWithTheDigest(t *testing.T) {
	board := map[string]interface{}{"effectiveElementFamilies": map[string]interface{}{"REQ": "requirement", "FN": "function"}}
	extra, ix, err := elementsInput("ARCHITECTURE", []byte(designDoc), board)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := extra["elements"].(string)
	digest, _ := extra["elementsDigest"].(string)
	sum := sha256.Sum256([]byte(body))
	if digest != hex.EncodeToString(sum[:]) {
		t.Errorf("the digest is of the exact text sent")
	}
	if !strings.Contains(body, `"id":"REQ-1"`) || !strings.Contains(body, `"family":"function"`) || len(ix.Elements) != 2 {
		t.Errorf("the index: %s", body)
	}
}

func TestAnIndexTypeAndNoElementsSendNone(t *testing.T) {
	for _, spec := range []string{"TEST_REPORT", "REVIEW_FINDINGS", "QUESTIONS"} {
		if extra, _, _ := elementsInput(spec, []byte(designDoc), nil); extra != nil {
			t.Errorf("%s carries a findings index, not elements: %v", spec, extra)
		}
	}
	docNoElements = true
	defer func() { docNoElements = false }()
	if extra, _, _ := elementsInput("ARCHITECTURE", []byte(designDoc), nil); extra != nil {
		t.Errorf("--no-elements sends none: %v", extra)
	}
}

func TestADocumentWithoutIdsPublishesAsBefore(t *testing.T) {
	if extra, _, _ := elementsInput("ARCHITECTURE", []byte("# Plain\n\nNo ids.\n"), nil); extra != nil {
		t.Errorf("no ids, nothing added: %v", extra)
	}
}

func TestFamiliesFallBackToTheDefaults(t *testing.T) {
	if familiesOf(map[string]interface{}{})["REQ"] != "requirement" {
		t.Error("a board without families parses against the defaults")
	}
	if familiesOf(map[string]interface{}{"effectiveElementFamilies": map[string]interface{}{"SAF": "safety"}})["SAF"] != "safety" {
		t.Error("the board's effective families are used")
	}
}
