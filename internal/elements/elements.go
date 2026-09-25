// Package elements parses a markdown document's element index (gaps §2.A, task e200cb32).
//
// The grammar's authority is rearm-saas ai-plans/agentic/elements.md, grammar version 1. An element
// is an ATX heading, or a table row, that starts with an id token (FAMILY-LOCAL). Attribute lines
// under it (parent, traces, assumes, level, speculative) give its parent and typed links, and
// everything up to the next element heading is its content, which is digested so a later diff can
// be per element.
//
// Parsing never fails: unknown families, duplicate ids and malformed attribute lines are warnings
// on the index. The same bytes always give the same index; CRLF and LF input agree.
package elements

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// GrammarVersion is the grammar this parser implements. 1.1 adds terms: the glossary terms an
// element's content marks as **term** (elements.md §3).
const GrammarVersion = "1.1"

// Warning codes, as the server uses them.
const (
	DuplicateID        = "DUPLICATE_ID"
	UnknownFamily      = "UNKNOWN_FAMILY"
	MalformedAttribute = "MALFORMED_ATTRIBUTE"
)

// DefaultFamilies mirrors the server's defaults, for parsing without a board.
var DefaultFamilies = map[string]string{
	"REQ": "requirement", "FN": "function", "PBS": "product", "IBS": "interface", "IF": "interface",
	"DS": "data", "TEST": "test", "T": "test", "GLOSS": "glossary", "ADR": "decision", "UC": "use-case",
	"CONOPS": "concept",
}

// Link is a typed trace: traces: derives_from REQ-1.
type Link struct {
	Verb   string `json:"verb"`
	Target string `json:"target"`
}

// Element is one element of the document. Field order is the wire order.
type Element struct {
	ID            string   `json:"id"`
	Family        string   `json:"family,omitempty"`
	Title         string   `json:"title"`
	Parent        string   `json:"parent,omitempty"`
	Level         *int     `json:"level,omitempty"`
	Traces        []Link   `json:"traces"`
	Assumes       []string `json:"assumes"`
	Speculative   []string `json:"speculative"`
	Terms         []string `json:"terms"`
	ContentDigest string   `json:"contentDigest"`
	Line          int      `json:"line"`
}

// Warning is something the parse found wrong; it never stops the parse.
type Warning struct {
	Code      string `json:"code"`
	ElementID string `json:"elementId,omitempty"`
	Message   string `json:"message"`
}

// Index is what the CLI sends with a publish.
type Index struct {
	GrammarVersion string    `json:"grammarVersion"`
	Elements       []Element `json:"elements"`
	Warnings       []Warning `json:"warnings"`
}

var (
	idToken    = regexp.MustCompile(`^([A-Z][A-Z0-9]*)-[A-Za-z0-9][A-Za-z0-9._-]*`)
	atxHeading = regexp.MustCompile(`^(#{1,6})[ \t]+(.*?)[ \t]*#*[ \t]*$`)
	attrLine   = regexp.MustCompile(`^(parent|traces|assumes|level|speculative):[ \t]*(.*)$`)
	fence      = regexp.MustCompile("^[ \t]{0,3}(```|~~~)")
	// A bold span as markdown reads one: no whitespace just inside either pair of asterisks, so a
	// stray ** left by a span that ran across lines does not pair up with the next one.
	boldSpan = regexp.MustCompile(`\*\*([^*\s](?:[^*\n]*?[^*\s])?)\*\*`)
)

// termsOf reads the glossary terms a piece of content uses: every bold span on one line, trimmed,
// trailing punctuation dropped, unique in first-seen order, case kept. Fenced code is not content
// that uses terms, so spans inside it are skipped.
func termsOf(lines []string) []string {
	out := []string{}
	seen := map[string]bool{}
	inFence := false
	for _, line := range lines {
		if fence.MatchString(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		for _, m := range boldSpan.FindAllStringSubmatch(line, -1) {
			term := strings.TrimRight(strings.TrimSpace(m[1]), ".,;:!?")
			term = strings.TrimSpace(term)
			if term == "" || seen[term] {
				continue
			}
			seen[term] = true
			out = append(out, term)
		}
	}
	return out
}

var attributeNames = map[string]bool{"parent": true, "traces": true, "assumes": true, "level": true, "speculative": true}

// token returns the id token at the start of s and the rest after it, or "" when s does not start
// with one. The token must end at whitespace, a colon, or the end of s.
func token(s string) (string, string) {
	m := idToken.FindString(s)
	if m == "" {
		return "", s
	}
	rest := s[len(m):]
	if rest != "" && rest[0] != ' ' && rest[0] != '\t' && rest[0] != ':' {
		return "", s
	}
	rest = strings.TrimLeft(rest, ":")
	return m, strings.TrimSpace(rest)
}

func familyOf(id string) string {
	m := idToken.FindStringSubmatch(id)
	if m == nil {
		return ""
	}
	return m[1]
}

type attrs struct {
	parent      string
	level       *int
	traces      []Link
	assumes     []string
	speculative []string
}

// apply parses one attribute value into a, returning a warning message when it does not parse.
func (a *attrs) apply(name, value string) string {
	value = strings.TrimSpace(value)
	switch name {
	case "parent":
		if t, rest := token(value); t == "" || rest != "" {
			return fmt.Sprintf("parent: %q is not an id", value)
		} else {
			a.parent = t
		}
	case "level":
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Sprintf("level: %q is not an integer", value)
		}
		a.level = &n
	case "assumes", "speculative":
		ids, bad := idList(value)
		if name == "assumes" {
			a.assumes = append(a.assumes, ids...)
		} else {
			a.speculative = append(a.speculative, ids...)
		}
		if bad != "" {
			return fmt.Sprintf("%s: %q is not an id", name, bad)
		}
	case "traces":
		for _, clause := range strings.Split(value, ";") {
			clause = strings.TrimSpace(clause)
			if clause == "" {
				continue
			}
			parts := strings.Fields(clause)
			if len(parts) < 2 {
				return fmt.Sprintf("traces: %q needs a verb and at least one id", clause)
			}
			verb := parts[0]
			ids, bad := idList(strings.TrimSpace(clause[len(verb):]))
			for _, id := range ids {
				a.traces = append(a.traces, Link{Verb: verb, Target: id})
			}
			if bad != "" {
				return fmt.Sprintf("traces: %q is not an id", bad)
			}
		}
	}
	return ""
}

// idList splits "A-1, B-2" into ids; bad is the first entry that is not one.
func idList(value string) ([]string, string) {
	var ids []string
	bad := ""
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if t, rest := token(part); t != "" && rest == "" {
			ids = append(ids, t)
		} else if bad == "" {
			bad = part
		}
	}
	return ids, bad
}

func digestLines(lines []string) string {
	start, end := 0, len(lines)
	for start < end && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	for end > start && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	sum := sha256.Sum256([]byte(strings.Join(lines[start:end], "\n")))
	return hex.EncodeToString(sum[:])
}

func splitRow(line string) []string {
	s := strings.TrimSpace(line)
	s = strings.TrimPrefix(s, "|")
	s = strings.TrimSuffix(s, "|")
	cells := strings.Split(s, "|")
	for i := range cells {
		cells[i] = strings.TrimSpace(cells[i])
	}
	return cells
}

var separatorCell = regexp.MustCompile(`^:?-{1,}:?$`)

func isSeparator(line string) bool {
	if !strings.HasPrefix(strings.TrimSpace(line), "|") {
		return false
	}
	for _, c := range splitRow(line) {
		if !separatorCell.MatchString(c) {
			return false
		}
	}
	return true
}

type heading struct {
	depth int
	id    string // "" for a heading that is not an element
}

// Parse reads src under the families (prefix to family name).
func Parse(src []byte, families map[string]string) Index {
	text := strings.ReplaceAll(strings.ReplaceAll(string(src), "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(text, "\n")
	ix := Index{GrammarVersion: GrammarVersion, Elements: []Element{}, Warnings: []Warning{}}

	// open is the element whose content is being collected: its index in ix.Elements and where its
	// content starts. A table row's content is its own cells, so it closes nothing and opens nothing.
	open, contentFrom := -1, 0
	closeOpen := func(end int) {
		if open >= 0 {
			ix.Elements[open].ContentDigest = digestLines(lines[contentFrom:end])
			ix.Elements[open].Terms = termsOf(lines[contentFrom:end])
			open = -1
		}
	}
	var stack []heading
	nearestElement := func(depth int) string {
		for i := len(stack) - 1; i >= 0; i-- {
			if stack[i].depth < depth && stack[i].id != "" {
				return stack[i].id
			}
		}
		return ""
	}

	inFence := false
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if fence.MatchString(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}

		if m := atxHeading.FindStringSubmatch(line); m != nil {
			depth := len(m[1])
			for len(stack) > 0 && stack[len(stack)-1].depth >= depth {
				stack = stack[:len(stack)-1]
			}
			id, title := token(m[2])
			if id == "" {
				stack = append(stack, heading{depth: depth})
				continue
			}
			closeOpen(i)
			e := Element{ID: id, Title: title, Line: i + 1, Traces: []Link{}, Assumes: []string{}, Speculative: []string{}, Terms: []string{}}
			var a attrs
			j := i + 1
			for ; j < len(lines); j++ {
				if strings.TrimSpace(lines[j]) == "" {
					continue
				}
				am := attrLine.FindStringSubmatch(lines[j])
				if am == nil {
					break
				}
				if msg := a.apply(am[1], am[2]); msg != "" {
					ix.Warnings = append(ix.Warnings, Warning{Code: MalformedAttribute, ElementID: id,
						Message: fmt.Sprintf("line %d: %s", j+1, msg)})
				}
			}
			// Blank lines skipped while looking for more attributes belong to the content.
			k := j
			for k > i+1 && strings.TrimSpace(lines[k-1]) == "" {
				k--
			}
			e.Parent = a.parent
			if e.Parent == "" {
				e.Parent = nearestElement(depth)
			}
			e.Level = a.level
			e.Traces, e.Assumes, e.Speculative = orEmpty(a.traces), orEmptyS(a.assumes), orEmptyS(a.speculative)
			ix.Elements = append(ix.Elements, e)
			open, contentFrom = len(ix.Elements)-1, k
			stack = append(stack, heading{depth: depth, id: id})
			i = k - 1
			continue
		}

		// A table: a header row, a separator, then rows.
		if strings.HasPrefix(strings.TrimSpace(line), "|") && i+1 < len(lines) && isSeparator(lines[i+1]) {
			header := splitRow(line)
			j := i + 2
			for ; j < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[j]), "|"); j++ {
				cells := splitRow(lines[j])
				if len(cells) == 0 {
					continue
				}
				id, rest := token(cells[0])
				if id == "" || rest != "" {
					continue
				}
				e := Element{ID: id, Line: j + 1, Traces: []Link{}, Assumes: []string{}, Speculative: []string{}, Terms: []string{}}
				var a attrs
				titled := false
				var content []string
				for c := 1; c < len(cells); c++ {
					name := ""
					if c < len(header) {
						name = strings.ToLower(header[c])
					}
					if attributeNames[name] {
						if cells[c] != "" {
							if msg := a.apply(name, cells[c]); msg != "" {
								ix.Warnings = append(ix.Warnings, Warning{Code: MalformedAttribute, ElementID: id,
									Message: fmt.Sprintf("line %d: %s", j+1, msg)})
							}
						}
						continue
					}
					if !titled {
						e.Title, titled = cells[c], true
						continue
					}
					content = append(content, cells[c])
				}
				e.Parent = a.parent
				if e.Parent == "" {
					e.Parent = nearestElement(7)
				}
				e.Level = a.level
				e.Traces, e.Assumes, e.Speculative = orEmpty(a.traces), orEmptyS(a.assumes), orEmptyS(a.speculative)
				sum := sha256.Sum256([]byte(strings.Join(content, "\n")))
				e.ContentDigest = hex.EncodeToString(sum[:])
				e.Terms = termsOf(content)
				ix.Elements = append(ix.Elements, e)
			}
			i = j - 1
			continue
		}
	}
	closeOpen(len(lines))

	seen := map[string]bool{}
	for i := range ix.Elements {
		e := &ix.Elements[i]
		prefix := familyOf(e.ID)
		if name, ok := families[prefix]; ok {
			e.Family = name
		} else {
			ix.Warnings = append(ix.Warnings, Warning{Code: UnknownFamily, ElementID: e.ID,
				Message: fmt.Sprintf("no element family %s on this board", prefix)})
		}
		if seen[e.ID] {
			ix.Warnings = append(ix.Warnings, Warning{Code: DuplicateID, ElementID: e.ID,
				Message: fmt.Sprintf("%s is defined more than once (line %d)", e.ID, e.Line)})
		}
		seen[e.ID] = true
	}
	return ix
}

func orEmpty(l []Link) []Link {
	if l == nil {
		return []Link{}
	}
	return l
}

func orEmptyS(l []string) []string {
	if l == nil {
		return []string{}
	}
	return l
}

// Canonical is the index's wire form and its sha256: the JSON text the publish sends, byte for
// byte what the digest covers. No HTML escaping and no trailing newline, so the bytes depend only
// on the index.
func Canonical(ix Index) ([]byte, string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(ix); err != nil {
		return nil, "", err
	}
	out := bytes.TrimRight(buf.Bytes(), "\n")
	sum := sha256.Sum256(out)
	return out, hex.EncodeToString(sum[:]), nil
}
