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
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// The agent orientation, split by action (task RD3-10): a core every agent reads and one section per
// action, each served alone at $REARM_URL/api/agents/orientation/<section>.md. The URLs are public, like
// the whole document at /api/agents/orientation.md, so no credentials are sent.

var orientationSection string

// orientationError is a section the server does not have; its body names the sections there are.
type orientationError struct {
	status int
	body   string
}

func (e *orientationError) Error() string {
	if strings.TrimSpace(e.body) != "" {
		return strings.TrimSpace(e.body)
	}
	return fmt.Sprintf("orientation request failed with status %d", e.status)
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
		return "", &orientationError{status: resp.StatusCode, body: string(body)}
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
