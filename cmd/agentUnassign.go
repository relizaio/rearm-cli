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
	"os"
	"strings"

	rearm "github.com/relizaio/rearm-client-go"
	"github.com/spf13/cobra"
)

// Releasing a stalled assignment (task RD3-4): the board's staleness sweep only ALERTs, and a person
// -- or the coordinator seat -- puts an ASSIGNED task back to QUEUED for the same role, with a reason.
// The holding session stays open; its later sign-off, publish or question on the task is refused,
// naming who released it and when.

var unassignReason string

var agentTaskUnassignCmd = &cobra.Command{
	Use:   "unassign <task>",
	Short: "Coordinator: release a stalled assignment, putting the task back in the queue for the same role (--reason)",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if strings.TrimSpace(unassignReason) == "" {
			fail("give --reason: it goes on the task's history and the board")
		}
		runGqlCompact(rearm.AgentTaskReleaseAssignmentProgrammatic_Operation, map[string]interface{}{
			"taskUuid": args[0], "sessionUuid": taskSessionUuid, "reason": unassignReason,
		}, "agentTaskReleaseAssignmentProgrammatic")
	},
}

var boardsUnassignCmd = &cobra.Command{
	Use:   "unassign <task>",
	Short: "Release a stalled assignment: the task goes back to the queue for the same role (needs BOARD_WRITE)",
	Long: `Puts an ASSIGNED task back to QUEUED for the same role, with a reason. The session that held it
stays open, and its later sign-off, publish or question on the task is refused, naming who released it
and when. For an agent that is gone or stuck; the board's staleness ALERT says when a hop stalled.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if strings.TrimSpace(unassignReason) == "" {
			fail("give --reason: it goes on the task's history and the board")
		}
		runGql(rearm.AgentTaskReleaseAssignment_Operation, map[string]interface{}{"taskUuid": args[0], "reason": unassignReason},
			"agentTaskReleaseAssignment")
	},
}

// releasedHop reads a refusal that says a person released this session's assignment on the task.
func releasedHop(err error) bool {
	return err != nil && strings.Contains(err.Error(), "was released by") && strings.Contains(err.Error(), "the task is queued again")
}

// runHopCompact is runGqlCompact for a verb that closes a hop (sign-off, return). When the server says
// the assignment was released, the task is no longer this session's: it is forgotten locally, so the
// session's usage stops being reported against it.
func runHopCompact(query string, variables map[string]interface{}, key, sessionUuid, taskUuid string) {
	data, err := sendGraphQLRequest(query, variables)
	if err != nil {
		forgetReleasedHop(err, sessionUuid, taskUuid)
		printRefusal(err)
		os.Exit(1)
	}
	printCompact(data[key])
}

// forgetReleasedHop drops the task from the session's local state when the refusal says its assignment
// was released; any other error leaves the state alone. Reports whether it did.
func forgetReleasedHop(err error, sessionUuid, taskUuid string) bool {
	if !releasedHop(err) {
		return false
	}
	clearCurrentTask(sessionUuid, taskUuid)
	forgetSeen(sessionUuid, taskUuid)
	forgetHopOutputs(sessionUuid, taskUuid)
	return true
}

func init() {
	agentTaskUnassignCmd.PersistentFlags().StringVar(&taskSessionUuid, "session", "", "Calling session uuid (the coordinator seat) — required")
	_ = agentTaskUnassignCmd.MarkPersistentFlagRequired("session")
	for _, c := range []*cobra.Command{agentTaskUnassignCmd, boardsUnassignCmd} {
		c.Flags().StringVar(&unassignReason, "reason", "", "why the assignment is released — required")
		acceptTaskKeys(c, firstTaskArg, nil, nil)
	}
	agentTaskUnassignCmd.Flags().BoolVar(&compactJson, "json", false, "print the full response as JSON instead of the compact lines")
	agentTaskCmd.AddCommand(agentTaskUnassignCmd)
	boardsCmd.AddCommand(boardsUnassignCmd)
}
