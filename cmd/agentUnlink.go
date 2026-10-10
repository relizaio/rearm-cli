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
	"strings"

	rearm "github.com/relizaio/rearm-client-go"
	"github.com/spf13/cobra"
)

var (
	unlinkSession string
	unlinkPr      string
	unlinkNote    string
)

// unlinkVars are an unlink's variables (task t20261010-033523-18839): the task, the session holding it, the
// PR to take off it, and an optional note.
func unlinkVars(task, session, pr, note string) (map[string]interface{}, error) {
	p := strings.TrimSpace(pr)
	if p == "" {
		return nil, fmt.Errorf("--pr (the linked PR to unlink) is required")
	}
	vars := map[string]interface{}{"taskUuid": task, "sessionUuid": session, "prUrl": p}
	if n := strings.TrimSpace(note); n != "" {
		vars["note"] = n
	}
	return vars, nil
}

var agentTaskUnlinkCmd = &cobra.Command{
	Use:   "unlinkpr <task-key-or-uuid>",
	Short: "Coder or coordinator: unlink a PR that should never have counted (a mistaken link, or a superseded PR)",
	Long: `Unlinks a PR that should never have counted: a mistaken link, or the superseded half of a pair
(task t20261010-033523-18839). Recorded on the task and posted as an INFO. Refused for a merged or
delivered PR, for the replacement of a superseded PR (unlink the superseded one first), on a task
parked for the operator or completed or cancelled, and past the pass when it would leave nothing to
deliver, or take out a PR that is open or unregistered (a person may do the latter from the task
page). Same gate as supersedepr. Linking the PR again stamps the record. The task is a key (RD-42)
or a uuid.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		vars, err := unlinkVars(args[0], unlinkSession, unlinkPr, unlinkNote)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
		runGqlCompact(rearm.AgentTaskUnlinkPrProgrammatic_Operation, vars, "agentTaskUnlinkPrProgrammatic")
	},
}

func init() {
	agentTaskUnlinkCmd.Flags().StringVar(&unlinkSession, "session", "", "the session holding the task (required)")
	agentTaskUnlinkCmd.Flags().StringVar(&unlinkPr, "pr", "", "the linked PR to unlink (required)")
	agentTaskUnlinkCmd.Flags().StringVar(&unlinkNote, "note", "", "why it should never have counted (at most 4000 characters)")
	_ = agentTaskUnlinkCmd.MarkFlagRequired("session")
	_ = agentTaskUnlinkCmd.MarkFlagRequired("pr")
	agentTaskCmd.AddCommand(agentTaskUnlinkCmd)
}
