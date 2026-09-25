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
	"os"
	"regexp"
	"strings"

	rearm "github.com/relizaio/rearm-client-go"
	"github.com/spf13/cobra"
)

var (
	deliveredSession   string
	deliveredUnit      string
	deliveredCommit    string
	deliveredAbandoned bool
	deliveredNote      string
)

var deliveredSha = regexp.MustCompile(`^[0-9a-fA-F]{7,40}$`)

// deliveredVars are an attestation's variables (task 18c5c293): the unit, and the commit that
// landed unless the unit is abandoned; a session only on the agent's verb.
func deliveredVars(task, session, unit, commit string, abandoned bool, note string) (map[string]interface{}, error) {
	u := strings.TrimSpace(unit)
	if u == "" {
		return nil, fmt.Errorf("--unit is required: a linked PR's URL, or a branch or release on a board without PRs")
	}
	vars := map[string]interface{}{"taskUuid": task, "unit": u}
	if session != "" {
		vars["sessionUuid"] = session
	}
	c := strings.TrimSpace(commit)
	if abandoned {
		vars["outcome"] = "ABANDONED"
	} else {
		if !deliveredSha.MatchString(c) {
			return nil, fmt.Errorf("--commit is the merged or pushed sha, 7 to 40 hex characters (or --abandoned)")
		}
		vars["outcome"] = "DELIVERED"
	}
	if c != "" {
		vars["commit"] = c
	}
	if n := strings.TrimSpace(note); n != "" {
		vars["note"] = n
	}
	return vars, nil
}

const deliveredLong = `Records that a delivery unit landed, or never will (task 18c5c293): a linked PR merged where this
ReARM cannot see it (its CI reports to another instance, or not at all), or on a board delivering
without PRs a push or release. A DELIVERING task then settles: completed once every unit is
delivered, back to the coordinator when one is --abandoned.`

var agentTaskDeliveredCmd = &cobra.Command{
	Use:   "delivered <task-uuid>",
	Short: "Coordinator or merging role: attest that a PR merged, a push landed, or a unit is abandoned",
	Long:  deliveredLong + "\n\nThe coordinator seat, or a session that worked the task in a role with PR_MERGE.",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		vars, err := deliveredVars(args[0], deliveredSession, deliveredUnit, deliveredCommit, deliveredAbandoned, deliveredNote)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
		runGql(rearm.AgentTaskDeliveredProgrammatic_Operation, vars, "agentTaskDeliveredProgrammatic")
	},
}

var boardsDeliveredCmd = &cobra.Command{
	Use:   "delivered <task-uuid>",
	Short: "Attest that a PR merged, a push landed, or a unit is abandoned (org admin)",
	Long:  deliveredLong,
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		vars, err := deliveredVars(args[0], "", deliveredUnit, deliveredCommit, deliveredAbandoned, deliveredNote)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
		runGql(rearm.AgentTaskDelivered_Operation, vars, "agentTaskDelivered")
	},
}

func init() {
	for _, c := range []*cobra.Command{agentTaskDeliveredCmd, boardsDeliveredCmd} {
		c.Flags().StringVar(&deliveredUnit, "unit", "", "a linked PR's URL, or a branch or release on a board without PRs — required")
		c.Flags().StringVar(&deliveredCommit, "commit", "", "the merged or pushed sha — required unless --abandoned")
		c.Flags().BoolVar(&deliveredAbandoned, "abandoned", false, "the unit will never land; the task goes back to the coordinator")
		c.Flags().StringVar(&deliveredNote, "note", "", "why, or where it landed")
	}
	agentTaskDeliveredCmd.Flags().StringVar(&deliveredSession, "session", "", "Coordinator seat or merging role session uuid — required")
	_ = agentTaskDeliveredCmd.MarkFlagRequired("session")
	agentTaskCmd.AddCommand(agentTaskDeliveredCmd)
	boardsCmd.AddCommand(boardsDeliveredCmd)
}
