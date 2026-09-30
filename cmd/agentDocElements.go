package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	rearm "github.com/relizaio/rearm-client-go"
	"github.com/relizaio/rearm/internal/elements"
	"github.com/spf13/cobra"
)

// docNoElements skips the element index on a publish.
var docNoElements bool

// docElementsType is the document type doc elements parses as (grammar 1.2); inferred from the board's
// path templates when left out.
var docElementsType string

// familyEntriesQuery reads the board's defining types per family prefix (grammar 1.2, task RD4-6). A
// CLI-local selection until the client-go pin moves past RD4-6, which adds the field to the shared
// board operations.
const familyEntriesQuery = `query AgentBoardElementFamilyEntries($boardUuid: ID!) {
	agentBoardProgrammatic(boardUuid: $boardUuid) { effectiveElementFamilyEntries { prefix family definedIn } } }`

// readFamilyEntries returns the board's families with their defining types, and whether the server
// has them: a server older than grammar 1.2 refuses the field, and the defaults apply. A variable so
// tests can stand in for the server.
var readFamilyEntries = func(boardUuid string) (map[string]elements.Family, bool) {
	data, err := sendGraphQLRequest(familyEntriesQuery, map[string]interface{}{"boardUuid": boardUuid})
	if err != nil {
		return nil, false
	}
	board, _ := data["agentBoardProgrammatic"].(map[string]interface{})
	entries, ok := board["effectiveElementFamilyEntries"].([]interface{})
	if !ok {
		return nil, false
	}
	return familyEntries(entries), true
}

// familyEntries reads effectiveElementFamilyEntries into families.
func familyEntries(entries []interface{}) map[string]elements.Family {
	out := map[string]elements.Family{}
	for _, e := range entries {
		m, _ := e.(map[string]interface{})
		prefix, _ := m["prefix"].(string)
		name, _ := m["family"].(string)
		if prefix == "" {
			continue
		}
		list := []string{}
		raw, _ := m["definedIn"].([]interface{})
		for _, t := range raw {
			if s, ok := t.(string); ok {
				list = append(list, s)
			}
		}
		out[prefix] = elements.Family{Name: name, DefinedIn: list}
	}
	return out
}

// boardFamilies is the board's families with their defining types, and whether the server serves the
// types (grammar 1.2). Without them each family takes its default list, which is what a server of
// grammar 1.2 would use for a board that sets none.
func boardFamilies(board map[string]interface{}) (elements.Families, bool) {
	uuid, _ := board["uuid"].(string)
	if uuid != "" {
		if fams, ok := readFamilyEntries(uuid); ok && len(fams) > 0 {
			return elements.Families(fams), true
		}
	}
	return elements.FamiliesFrom(familiesOf(board), nil), false
}

// familiesOf reads a board's effective element families. A board read from a server without
// element families has none, and the defaults apply -- the same defaults the server would use.
func familiesOf(board map[string]interface{}) map[string]string {
	raw, _ := board["effectiveElementFamilies"].(map[string]interface{})
	if len(raw) == 0 {
		return elements.DefaultFamilies
	}
	out := map[string]string{}
	for k, v := range raw {
		if s, ok := v.(string); ok {
			out[k] = s
		}
	}
	return out
}

// reservedOf reads a board's task prefixes, the current one and every one held before: a token with
// one of them as its family is a task key, not an element id (task RD2-28). None without a board.
func reservedOf(board map[string]interface{}) map[string]bool {
	out := map[string]bool{}
	if p, _ := board["taskPrefix"].(string); p != "" {
		out[p] = true
	}
	history, _ := board["taskPrefixHistory"].([]interface{})
	for _, h := range history {
		if p, _ := h.(string); p != "" {
			out[p] = true
		}
	}
	return out
}

// elementsInput is what a publish of spec adds for the document src (gaps §2.A): the element
// index as the JSON text it digested, and the digest. Nothing with --no-elements, or for a document
// with no ids and nothing to warn about -- which publishes exactly as it did before elements existed.
// A type that carries a findings index sends elements only when the server reads grammar 1.2 and the
// board defines a family in it (a test report defines test ids, task RD4-6); an older server refuses
// elements on an index type.
func elementsInput(spec string, src []byte, board map[string]interface{}) (map[string]interface{}, *elements.Index, error) {
	if docNoElements {
		return nil, nil, nil
	}
	fams, typed := boardFamilies(board)
	if taskScopedTypes[spec] && !(typed && fams.DefinesAnything(spec)) {
		return nil, nil, nil
	}
	ix := elements.Parse(src, spec, fams, reservedOf(board))
	if len(ix.Elements) == 0 && len(ix.References) == 0 && len(ix.Warnings) == 0 {
		return nil, &ix, nil
	}
	body, digest, err := elements.Canonical(ix)
	if err != nil {
		return nil, nil, err
	}
	return map[string]interface{}{"elements": string(body), "elementsDigest": digest}, &ix, nil
}

func summarise(ix *elements.Index) string {
	if ix == nil {
		return ""
	}
	out := fmt.Sprintf("%d element(s), %d warning(s)", len(ix.Elements), len(ix.Warnings))
	if len(ix.References) > 0 {
		// Only when there are some, so a document without references reads as it always has.
		out += fmt.Sprintf(", %d reference(s)", len(ix.References))
	}
	return out
}

// definitionsAndReferences is the stderr listing doc elements prints after the index: what the
// document defines and what it only names, apart.
func definitionsAndReferences(ix *elements.Index) []string {
	var out []string
	if len(ix.Elements) > 0 {
		ids := make([]string, 0, len(ix.Elements))
		for _, e := range ix.Elements {
			ids = append(ids, e.ID)
		}
		out = append(out, "  defines: "+strings.Join(ids, ", "))
	}
	if len(ix.References) > 0 {
		refs := make([]string, 0, len(ix.References))
		for _, r := range ix.References {
			family := r.Family
			if family == "" {
				family = "unknown family"
			}
			refs = append(refs, fmt.Sprintf("%s (%s, line %d)", r.ID, family, r.Line))
		}
		out = append(out, "  references: "+strings.Join(refs, ", "))
	}
	return out
}

// boardPathsQuery reads what doc elements needs to tell a file's type from its path.
const boardPathsQuery = `query AgentBoardDocumentPaths($boardUuid: ID!) {
	agentBoardProgrammatic(boardUuid: $boardUuid) { effectiveDocumentPaths documentsRoot } }`

var placeholder = regexp.MustCompile(`\\\{(key|task|component|type|round)\\\}`)

// typeOfPath is the one specification whose path template, under the board's documents root, matches
// the file's path; "" when none or several do.
func typeOfPath(file string, templates map[string]interface{}, root string) string {
	path := filepath.ToSlash(file)
	if abs, err := filepath.Abs(file); err == nil {
		path = filepath.ToSlash(abs)
	}
	root = strings.Trim(filepath.ToSlash(root), "/")
	if root != "" {
		root += "/"
	}
	var found []string
	for spec, t := range templates {
		tmpl, _ := t.(string)
		if tmpl == "" {
			continue
		}
		pattern := placeholder.ReplaceAllStringFunc(regexp.QuoteMeta(strings.TrimLeft(tmpl, "/")), func(m string) string {
			if strings.Contains(m, "round") {
				return `[0-9]+`
			}
			return `[^/]+`
		})
		if regexp.MustCompile(`(^|/)` + regexp.QuoteMeta(root) + pattern + `$`).MatchString(path) {
			found = append(found, spec)
		}
	}
	sort.Strings(found)
	if len(found) != 1 {
		return ""
	}
	return found[0]
}

var agentDocElementsCmd = &cobra.Command{
	Use:   "elements <path>",
	Short: "Print the element index a document would publish, and its warnings, without publishing",
	Long: `Parses a markdown file under the element grammar (elements.md, grammar version 1.2) and prints
the index doc publish would send: each element's id, family, title, parent, links, glossary terms
(**term** in its content) and content digest, the references, and every warning. Nothing is sent.

Grammar 1.2 reads a heading by the document's type: an id defines only in a type its family lists
(test ids in a test plan or report, a finding id in review findings), and is a reference elsewhere,
defining nothing. So give the type with --type; with --board and no --type it is read off the board's
path templates. A bold span leading a line and ending with a period or colon ("**Fix.**", "- **Why:**")
is emphasis, not a glossary term.

Families come from --board (its effective element families and their defining types), else the
defaults. With --board, a token whose family is one of the board's task prefixes (the current one or
one held before) is a task key, not an id: "# RD2-1 — Title" is a prose heading. Without --board no
prefix is reserved, and a task key in a heading reads as an id of an unknown family.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		src, err := os.ReadFile(args[0])
		if err != nil {
			return err
		}
		families := elements.Defaults()
		var reserved map[string]bool
		spec := strings.ToUpper(strings.ReplaceAll(docElementsType, "-", "_"))
		if docBoard != "" {
			boardUuid := boardArg(docBoard)
			data, err := sendGraphQLRequest(rearm.AgentBoardProgrammatic_Operation,
				map[string]interface{}{"boardUuid": boardUuid})
			if err != nil {
				return fmt.Errorf("could not read board %s: %w", docBoard, err)
			}
			if board, _ := data["agentBoardProgrammatic"].(map[string]interface{}); board != nil {
				families, _ = boardFamilies(board)
				reserved = reservedOf(board)
			}
			if spec == "" {
				if paths, err := sendGraphQLRequest(boardPathsQuery, map[string]interface{}{"boardUuid": boardUuid}); err == nil {
					b, _ := paths["agentBoardProgrammatic"].(map[string]interface{})
					templates, _ := b["effectiveDocumentPaths"].(map[string]interface{})
					root, _ := b["documentsRoot"].(string)
					spec = typeOfPath(args[0], templates, root)
				}
				if spec != "" {
					fmt.Fprintln(os.Stderr, "type "+spec+", from the board's path templates")
				}
			}
		} else {
			fmt.Fprintln(os.Stderr, "no --board: no task prefix is reserved, so a task key in a heading reads as an element")
		}
		if spec == "" {
			fmt.Fprintln(os.Stderr, "no type: pass --type (or --board with a file at one of its paths); every id heading reads as a definition")
		}
		ix := elements.Parse(src, spec, families, reserved)
		out, err := json.MarshalIndent(ix, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(out))
		fmt.Fprintln(os.Stderr, summarise(&ix))
		for _, line := range definitionsAndReferences(&ix) {
			fmt.Fprintln(os.Stderr, line)
		}
		for _, w := range ix.Warnings {
			fmt.Fprintf(os.Stderr, "  %s %s: %s\n", w.Code, w.ElementID, strings.TrimSpace(w.Message))
		}
		for _, e := range ix.Elements {
			if len(e.Terms) > 0 {
				fmt.Fprintf(os.Stderr, "  %s terms: %s\n", e.ID, strings.Join(e.Terms, ", "))
			}
		}
		return nil
	},
}

func init() {
	agentDocPublishCmd.Flags().BoolVar(&docNoElements, "no-elements", false,
		"publish without the element index this prose document's ids would otherwise send")
	agentDocElementsCmd.Flags().StringVar(&docBoard, "board", "", "board whose element families to parse against")
	agentDocElementsCmd.Flags().StringVar(&docElementsType, "type", "",
		"the document's specification type, e.g. DETAILED_DESIGN; read off the board's path templates when left out")
	agentDocCmd.AddCommand(agentDocElementsCmd)
}
