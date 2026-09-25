package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	rearm "github.com/relizaio/rearm-client-go"
	"github.com/relizaio/rearm/internal/elements"
	"github.com/spf13/cobra"
)

// docNoElements skips the element index on a publish.
var docNoElements bool

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

// elementsInput is what a publish of spec adds for the document src (gaps §2.A): the element
// index as the JSON text it digested, and the digest. Nothing for a type that carries a findings
// index, with --no-elements, or for a document with no elements and nothing to warn about -- which
// publishes exactly as it did before elements existed.
func elementsInput(spec string, src []byte, board map[string]interface{}) (map[string]interface{}, *elements.Index, error) {
	if docNoElements || taskScopedTypes[spec] {
		return nil, nil, nil
	}
	ix := elements.Parse(src, familiesOf(board))
	if len(ix.Elements) == 0 && len(ix.Warnings) == 0 {
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
	return fmt.Sprintf("%d element(s), %d warning(s)", len(ix.Elements), len(ix.Warnings))
}

var agentDocElementsCmd = &cobra.Command{
	Use:   "elements <path>",
	Short: "Print the element index a document would publish, and its warnings, without publishing",
	Long: `Parses a markdown file under the element grammar (elements.md, grammar version 1.1) and prints
the index doc publish would send: each element's id, family, title, parent, links, glossary terms
(**term** in its content) and content digest, and every warning. Nothing is sent.

Families come from --board (its effective element families), else the defaults.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		src, err := os.ReadFile(args[0])
		if err != nil {
			return err
		}
		families := elements.DefaultFamilies
		if docBoard != "" {
			data, err := sendGraphQLRequest(rearm.AgentBoardProgrammatic_Operation,
				map[string]interface{}{"boardUuid": docBoard})
			if err != nil {
				return fmt.Errorf("could not read board %s: %w", docBoard, err)
			}
			if board, _ := data["agentBoardProgrammatic"].(map[string]interface{}); board != nil {
				families = familiesOf(board)
			}
		}
		ix := elements.Parse(src, families)
		out, err := json.MarshalIndent(ix, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(out))
		fmt.Fprintln(os.Stderr, summarise(&ix))
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
	agentDocCmd.AddCommand(agentDocElementsCmd)
}
