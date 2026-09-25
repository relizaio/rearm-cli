package elements

import (
	"os"
	"strings"
	"testing"
)

func parseFile(t *testing.T, name string) Index {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return Parse(b, DefaultFamilies)
}

func byID(ix Index) map[string]Element {
	m := map[string]Element{}
	for _, e := range ix.Elements {
		if _, ok := m[e.ID]; !ok {
			m[e.ID] = e
		}
	}
	return m
}

func TestHeadingsNestAndAnExplicitParentWins(t *testing.T) {
	ix := parseFile(t, "nested.md")
	got := byID(ix)
	if len(ix.Elements) != 4 {
		t.Fatalf("want 4 elements, got %d: %+v", len(ix.Elements), ix.Elements)
	}
	if got["REQ-1"].Parent != "" || got["REQ-1"].Title != "The board refuses a dependency cycle" || got["REQ-1"].Family != "requirement" {
		t.Errorf("REQ-1: %+v", got["REQ-1"])
	}
	if got["REQ-1.1"].Parent != "REQ-1" {
		t.Errorf("REQ-1.1 nests under REQ-1: %+v", got["REQ-1.1"])
	}
	if got["REQ-1.1.a"].Parent != "REQ-1" {
		t.Errorf("an explicit parent wins over nesting under REQ-1.1: %+v", got["REQ-1.1.a"])
	}
	if got["FN-1"].Parent != "" || got["FN-1"].Family != "function" {
		t.Errorf("FN-1 sits under a non-element heading: %+v", got["FN-1"])
	}
	if got["REQ-1"].Line != 5 {
		t.Errorf("REQ-1 is on line 5, got %d", got["REQ-1"].Line)
	}
	if len(ix.Warnings) != 0 {
		t.Errorf("no warnings: %+v", ix.Warnings)
	}
}

func TestAttributes(t *testing.T) {
	got := byID(parseFile(t, "nested.md"))
	r := got["REQ-1.1"]
	want := []Link{{"derives_from", "CONOPS-3"}, {"is_verified_by", "TEST-7"}, {"is_verified_by", "TEST-8"}}
	if len(r.Traces) != len(want) {
		t.Fatalf("traces: %+v", r.Traces)
	}
	for i := range want {
		if r.Traces[i] != want[i] {
			t.Errorf("trace %d: got %+v want %+v", i, r.Traces[i], want[i])
		}
	}
	if len(r.Assumes) != 1 || r.Assumes[0] != "ADR-2" {
		t.Errorf("assumes: %+v", r.Assumes)
	}
	if got["REQ-1"].Level == nil || *got["REQ-1"].Level != 1 {
		t.Errorf("level: %+v", got["REQ-1"].Level)
	}
}

func TestANonElementSubheadingIsContent(t *testing.T) {
	a := parseFile(t, "nested.md")
	b, _ := os.ReadFile("testdata/nested.md")
	changed := Parse([]byte(strings.Replace(string(b), "Still REQ-1.1's content.", "Changed.", 1)), DefaultFamilies)
	if byID(a)["REQ-1.1"].ContentDigest == byID(changed)["REQ-1.1"].ContentDigest {
		t.Error("text under a non-element subheading is the element's content")
	}
	if byID(a)["REQ-1"].ContentDigest != byID(changed)["REQ-1"].ContentDigest {
		t.Error("and not its parent's")
	}
}

func TestTableRowsWithAttributeColumns(t *testing.T) {
	got := byID(parseFile(t, "table.md"))
	if _, ok := got["not-an-id"]; ok {
		t.Error("a row without an id token is not an element")
	}
	if got["IF-1"].Title != "Board API" || got["IF-1"].Parent != "REQ-10" || got["IF-1"].Family != "interface" {
		t.Errorf("IF-1: %+v", got["IF-1"])
	}
	if len(got["IF-1"].Traces) != 1 || got["IF-1"].Traces[0] != (Link{"satisfies", "REQ-10"}) {
		t.Errorf("IF-1 traces: %+v", got["IF-1"].Traces)
	}
	if got["IF-2"].Parent != "REQ-10" {
		t.Errorf("a row without a parent takes the heading above the table: %+v", got["IF-2"])
	}
	if got["IF-1"].ContentDigest == got["IF-2"].ContentDigest {
		t.Error("each row's content is its own cells")
	}
}

func TestProblemsAreWarningsAndTheParseGoesOn(t *testing.T) {
	ix := parseFile(t, "warnings.md")
	codes := map[string]int{}
	for _, w := range ix.Warnings {
		codes[w.Code]++
	}
	if codes[MalformedAttribute] != 3 || codes[UnknownFamily] != 1 || codes[DuplicateID] != 1 {
		t.Errorf("warnings: %+v", ix.Warnings)
	}
	if len(ix.Elements) != 3 {
		t.Errorf("every element is still indexed: %+v", ix.Elements)
	}
	if got := byID(ix)["REQ-1"]; len(got.Assumes) != 1 || got.Assumes[0] != "ADR-1" {
		t.Errorf("the good part of a malformed line is kept: %+v", got.Assumes)
	}
}

func TestNoIdTokensNoElements(t *testing.T) {
	ix := parseFile(t, "plain.md")
	if len(ix.Elements) != 0 || len(ix.Warnings) != 0 {
		t.Errorf("a plain document has no elements (and a heading in a fence is not one): %+v", ix)
	}
}

func TestDeterministicAndLineEndingBlind(t *testing.T) {
	b, _ := os.ReadFile("testdata/nested.md")
	one, d1, _ := Canonical(Parse(b, DefaultFamilies))
	two, d2, _ := Canonical(Parse(b, DefaultFamilies))
	crlf, d3, _ := Canonical(Parse([]byte(strings.ReplaceAll(string(b), "\n", "\r\n")), DefaultFamilies))
	if string(one) != string(two) || d1 != d2 {
		t.Error("the same bytes give the same canonical form")
	}
	if string(one) != string(crlf) || d1 != d3 {
		t.Error("CRLF and LF give the same index and digest")
	}
	if strings.HasSuffix(string(one), "\n") || !strings.HasPrefix(string(one), `{"grammarVersion":"1","elements":[`) {
		t.Errorf("wire form: %s", one)
	}
}
