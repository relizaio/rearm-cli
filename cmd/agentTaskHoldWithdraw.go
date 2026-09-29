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
	"errors"
	"strings"

	rearm "github.com/relizaio/rearm-client-go"
	"github.com/spf13/cobra"
)

// Task RD4-5: a session parks its own hop for an operator decision, and withdraws its own registration in
// intake. Both reuse the verbs the coordinator seat has (hold, cancel): the server decides who may call them,
// and these are the flags that say which caller this is.

var (
	holdOperator   bool
	holdQuestion   string
	withdrawReason string
)

// holdVariables are the hold's variables: the seat's COORDINATOR hold with --reason, as before; or, with
// --operator, the holder's OPERATOR hold with the question as the reason. A question without --operator, or a
// reason beside it, would say two things, so each is refused before anything is sent.
func holdVariables(task, session, reason, question string, operator bool) (map[string]interface{}, error) {
	reason, question = strings.TrimSpace(reason), strings.TrimSpace(question)
	v := map[string]interface{}{"taskUuid": task, "sessionUuid": session}
	if !operator {
		if question != "" {
			return nil, errors.New("--question goes with --operator: it is what the session holding the task asks the operator")
		}
		if reason == "" {
			return nil, errors.New("give --reason: why the task waits for a human")
		}
		v["reason"] = reason
		return v, nil
	}
	if reason != "" {
		return nil, errors.New("with --operator the reason is the question: give --question, not --reason")
	}
	if question == "" {
		return nil, errors.New("give --question: what the operator is to decide, shown on the task until a person answers")
	}
	v["reason"] = question
	v["level"] = "OPERATOR"
	return v, nil
}

// holdOperation is the hold operation with the level it takes since task RD4-5. The pinned client-go
// operation predates the argument; once the pin carries it, this is that operation unchanged.
func holdOperation() string {
	op := rearm.AgentTaskHoldProgrammatic_Operation
	if strings.Contains(op, "$level") {
		return op
	}
	op = strings.Replace(op, "$reason: String!)", "$reason: String!, $level: AgentTaskHoldLevel)", 1)
	return strings.Replace(op, "reason: $reason)", "reason: $reason, level: $level)", 1)
}

func runHold(task string) {
	v, err := holdVariables(task, taskSessionUuid, taskNote, holdQuestion, holdOperator)
	if err != nil {
		fail(err.Error())
	}
	runGqlCompact(holdOperation(), v, "agentTaskHoldProgrammatic")
}

// withdrawVariables are a withdrawal's: the cancel operation, with the reason as its note (required).
func withdrawVariables(task, session, reason string) (map[string]interface{}, error) {
	if strings.TrimSpace(reason) == "" {
		return nil, errors.New("give --reason: the task's history reads withdrawn by its registrant: <reason>")
	}
	return map[string]interface{}{"taskUuid": task, "sessionUuid": session, "note": strings.TrimSpace(reason)}, nil
}

var agentTaskWithdrawCmd = &cobra.Command{
	Use:   "withdraw <task>",
	Short: "Withdraw a task you registered while it waits on intake (--reason); refused once the coordinator authorised it",
	Long: `The session that registered a task withdraws it while it is PENDING_INTAKE: a duplicate, or a
registration it no longer stands behind. The task is cancelled with the row "withdrawn by its
registrant: <reason>". Once the coordinator has authorised it the task is the coordinator's, and
the withdrawal is refused: return it or ask the seat.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		v, err := withdrawVariables(args[0], taskSessionUuid, withdrawReason)
		if err != nil {
			fail(err.Error())
		}
		runGqlCompact(rearm.AgentTaskCancelProgrammatic_Operation, v, "agentTaskCancelProgrammatic")
	},
}

func init() {
	agentTaskHoldCmd.Flags().BoolVar(&holdOperator, "operator", false,
		"Park the hop you hold for the operator: an OPERATOR hold with --question; a person's release answers it")
	agentTaskHoldCmd.Flags().StringVar(&holdQuestion, "question", "",
		"With --operator: what the operator is to decide, shown on the task as awaiting the operator")
	agentTaskWithdrawCmd.PersistentFlags().StringVar(&taskSessionUuid, "session", "", "The session that registered the task — required")
	_ = agentTaskWithdrawCmd.MarkPersistentFlagRequired("session")
	agentTaskWithdrawCmd.Flags().StringVar(&withdrawReason, "reason", "", "Why the registration is withdrawn — required")
	acceptTaskKeys(agentTaskWithdrawCmd, firstTaskArg, nil, nil)
	agentTaskCmd.AddCommand(agentTaskWithdrawCmd)
}
