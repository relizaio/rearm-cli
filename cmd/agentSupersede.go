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
	supersedeSession string
	supersedeOld     string
	supersedeBy      string
	supersedeNote    string
)

// supersedeVars are a supersede declaration's variables (task RD3-13): the task, the session holding it, the
// old PR and the one replacing it, and an optional note.
func supersedeVars(task, session, old, by, note string) (map[string]interface{}, error) {
	o, b := strings.TrimSpace(old), strings.TrimSpace(by)
	if o == "" || b == "" {
		return nil, fmt.Errorf("--old (the PR closed without merging) and --by (the linked PR that replaces it) are required")
	}
	vars := map[string]interface{}{"taskUuid": task, "sessionUuid": session, "oldUrl": o, "byUrl": b}
	if n := strings.TrimSpace(note); n != "" {
		vars["note"] = n
	}
	return vars, nil
}

var agentTaskSupersedeCmd = &cobra.Command{
	Use:   "supersedepr <task-uuid>",
	Short: "Coder: declare a linked PR superseded by the linked PR that replaces it",
	Long: `Declares a linked PR superseded by its replacement (task RD3-13), so the task's delivery counts the
replacement and no longer waits on, or is blocked by, the old one. For a PR replaced rather than
force-pushed -- for example to drop commits without their ReARM trailers.

The old PR must be closed without merging, as its CI reported it; the replacement must be linked to the
task first (task linkpr) and be on the same repository. A merged PR is never superseded. The session
holding the task, in a role with CODE_PUSH; a person declares it on the task page.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		vars, err := supersedeVars(args[0], supersedeSession, supersedeOld, supersedeBy, supersedeNote)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
		runGqlCompact(rearm.AgentTaskSupersedePullRequestProgrammatic_Operation, vars, "agentTaskSupersedePullRequestProgrammatic")
	},
}

func init() {
	agentTaskSupersedeCmd.Flags().StringVar(&supersedeSession, "session", "", "the session holding the task — required")
	agentTaskSupersedeCmd.Flags().StringVar(&supersedeOld, "old", "", "the linked PR closed without merging — required")
	agentTaskSupersedeCmd.Flags().StringVar(&supersedeBy, "by", "", "the linked PR that replaces it, on the same repository — required")
	agentTaskSupersedeCmd.Flags().StringVar(&supersedeNote, "note", "", "why it was replaced")
	_ = agentTaskSupersedeCmd.MarkFlagRequired("session")
	agentTaskCmd.AddCommand(agentTaskSupersedeCmd)
}
