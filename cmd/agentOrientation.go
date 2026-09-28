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
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// The agent orientation, split by action (task RD3-10): a core every agent reads and one section per
// action, each served alone at $REARM_URL/api/agents/orientation/<section>.md. The URLs are public, like
// the whole document at /api/agents/orientation.md, so no credentials are sent.

var orientationSection string

// orientationError is a section the server did not serve. Its words are the CLI's own, never the response
// body: in a deployment a proxy in front of /api/agents replaces error bodies with its own page (RD3-10 T-1),
// so the names come from the core's table instead.
type orientationError struct {
	name   string
	status int
	names  []string
}

func (e *orientationError) Error() string {
	if len(e.names) > 0 {
		return fmt.Sprintf("No orientation section named '%s'. The sections are: core, %s.", e.name, strings.Join(e.names, ", "))
	}
	return fmt.Sprintf("orientation section '%s' not served (status %d)", e.name, e.status)
}

// orientationRow is a row of the core's "Where to read what" table: | when you need to | `section` |
var orientationRow = regexp.MustCompile("(?m)^\\| .+? \\| `([a-z0-9-]+)` \\|$")

// sectionNames reads the section names from the core's "Where to read what" table.
func sectionNames(core string) []string {
	at := strings.Index(core, "## Where to read what")
	if at < 0 {
		return nil
	}
	var names []string
	for _, m := range orientationRow.FindAllStringSubmatch(core[at:], -1) {
		names = append(names, m[1])
	}
	return names
}

// fetchOrientation reads the core ("core") or one section by name from the server.
func fetchOrientation(name string) (string, error) {
	url := strings.TrimRight(rearmUri, "/") + "/api/agents/orientation/" + name + ".md"
	hc := &http.Client{Timeout: 30 * time.Second}
	resp, err := hc.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode != http.StatusOK {
		oe := &orientationError{name: name, status: resp.StatusCode}
		if name != "core" {
			if core, err := fetchOrientation("core"); err == nil {
				oe.names = sectionNames(core)
			}
		}
		return "", oe
	}
	return string(body), nil
}

var agentOrientationCmd = &cobra.Command{
	Use:   "orientation",
	Short: "Print the agent orientation's core, or one section of it (--section)",
	Long: `Prints the core of the agent orientation: prerequisites, opening and closing a session, the usage
hooks, the final report, and the table of which section to read for which action. --section <name>
prints that section instead, e.g. taking-a-task, publishing, asking, waiting, commit-trailers. An
unknown name lists the sections there are. The whole document stays at $REARM_URL/api/agents/orientation.md.`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		name := strings.TrimSpace(orientationSection)
		if name == "" {
			name = "core"
		}
		text, err := fetchOrientation(name)
		if err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			os.Exit(1)
		}
		fmt.Print(text)
	},
}

func init() {
	agentOrientationCmd.Flags().StringVar(&orientationSection, "section", "", "print this section instead of the core")
	agentCmd.AddCommand(agentOrientationCmd)
}
