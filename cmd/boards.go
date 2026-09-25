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
	"strconv"
	"strings"

	"github.com/relizaio/rearm-client-go"
	"github.com/spf13/cobra"
)

// A person's board verbs: what the board page's buttons do, from a terminal or a script. They
// run as the person -- a browser login (`rearm login`) or a personal key -- with the
// organization's admin permission, and carry no session: people have none. An agent keeps
// using `rearm agent task ...` with its session; an organization key is refused here before
// anything is sent, and the server refuses it again.
//
//   rearm boards list [--org <uuid>]
//   rearm boards tasks <board> [--status S]
//   rearm boards register <board> --title t [--external-ref r] [--source-url u] [--parent p] [--level n]
//   rearm boards authorize <task> --role r [--order n] [--depends-on t]... [--level n]
//   rearm boards order <task> --order n
//   rearm boards complete <task> [--note n] [--skip-required-roles]
//   rearm boards cancel <task> [--note n]
//   rearm boards decide <task> --spec REVIEW_FINDINGS|TEST_REPORT [--dismiss ID=why]... [--accept ID=why]...
//                              [--priority ID=N]... [--file title --priority N] [--about SPEC]
//   rearm boards answer <task> --id q --resolution text [--withdraw] | --all text [--keep-hold]
//   rearm boards review <task> --approve | --reject [--note n] [--file title --priority N --about SPEC]
//   rearm boards signoff <task> --outcome PASSED|REJECTED [--note n]
//   rearm boards hold <task> --reason r | release <task> [--note n]
//   rearm boards require-review <task> [--off]
//   rearm boards strength <task> --required 6.5 | --clear
//   rearm boards budget <task> --usd 2.50 | --clear
//   rearm boards lock <board> --reason r | unlock <board>
//   rearm boards apply -f board.yaml [--dry-run]

var (
	boardsStatus       string
	boardsTitle        string
	boardsExternalRef  string
	boardsSourceUrl    string
	boardsParent       string
	boardsLevel        int
	boardsRole         string
	boardsOrder        int
	boardsDependsOn    []string
	boardsNote         string
	boardsSkipRequired bool
	boardsSpec         string
	boardsDismiss      []string
	boardsAccept       []string
	boardsPriority     []string
	boardsFile         []string
	boardsAbout        string
	boardsAboutRelease string
	boardsAnswerId     string
	boardsResolution   string
	boardsWithdraw     bool
	boardsAnswerAll    string
	boardsKeepHold     bool
	boardsApprove      bool
	boardsReject       bool
	boardsOutcome      string
	boardsReason       string
	boardsOff          bool
	boardsStrength     string
	boardsClear        bool
	boardsBudget       string
)

const personOnly = "rearm boards acts as a person: sign in with `rearm login`, or use a personal key (USER__...). " +
	"An agent uses `rearm agent task ...` with its session"

// personCredentialProblem says why the credential on file cannot act as a person, or "" when it
// can. A browser login can; so can a personal key, whose id starts USER__. An organization key
// or a CI identity acts for an agent.
func personCredentialProblem(mode, keyId string) string {
	switch mode {
	case authSession:
		return ""
	case authGitHubOIDC:
		return personOnly + " (this is a CI identity)"
	}
	if strings.TrimSpace(keyId) == "" {
		return personOnly + " (no credential on file)"
	}
	if !strings.HasPrefix(keyId, "USER__") {
		kind := keyId
		if i := strings.Index(keyId, "__"); i > 0 {
			kind = keyId[:i]
		}
		return personOnly + " (this is a " + kind + " key)"
	}
	return ""
}

var boardsCmd = &cobra.Command{
	Use:   "boards",
	Short: "A person's board actions: register, authorize, complete, decide, answer, review, hold, lock",
	Long: `The board actions a person takes, from a terminal: the same operations as the
buttons on the board page, recorded under your name.

For people, not agents. They run on a browser login (` + "`rearm login`" + `) or a
personal key, as you, and need the organization's admin permission. An
organization key is refused; an agent session keeps using ` + "`rearm agent task`" + `.`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		initConfig(cmd)
		if p := personCredentialProblem(resolvedAuthMode(), apiKeyId); p != "" {
			fail(p)
		}
	},
}

func boardsOrg() (string, error) {
	if strings.TrimSpace(orgFlag) != "" {
		return orgFlag, nil
	}
	if inSessionMode() && strings.TrimSpace(sessionOrg) != "" {
		return sessionOrg, nil
	}
	return "", fmt.Errorf("--org is required with a personal key")
}

var boardsListCmd = &cobra.Command{
	Use:   "list",
	Short: "The organization's boards (the login's organization, or --org)",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		org, err := boardsOrg()
		if err != nil {
			fail(err.Error())
		}
		runGql(rearm.AgentBoardsOfOrg_Operation, map[string]interface{}{"orgUuid": org}, "agentBoardsOfOrg")
	},
}

var boardsTasksCmd = &cobra.Command{
	Use:   "tasks <board-uuid>",
	Short: "A board's tasks, optionally by status",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		vars := map[string]interface{}{"boardUuid": args[0]}
		if boardsStatus != "" {
			vars["status"] = strings.ToUpper(boardsStatus)
		}
		runGql(rearm.AgentTasksOfBoard_Operation, vars, "agentTasksOfBoard")
	},
}

func registerVariables(board, title, externalRef, sourceUrl, parent string, level int) map[string]interface{} {
	input := map[string]interface{}{"title": title}
	if externalRef != "" {
		input["externalRef"] = externalRef
	}
	if sourceUrl != "" {
		input["sourceUrl"] = sourceUrl
	}
	if parent != "" {
		input["parentTask"] = parent
	}
	if level > 0 {
		input["level"] = level
	}
	return map[string]interface{}{"boardUuid": board, "input": input}
}

var boardsRegisterCmd = &cobra.Command{
	Use:   "register <board-uuid>",
	Short: "Register a task by hand (PENDING_INTAKE); on a board with sources --external-ref is required",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		runGql(rearm.AgentTaskRegister_Operation, registerVariables(args[0], boardsTitle, boardsExternalRef,
			boardsSourceUrl, boardsParent, boardsLevel), "agentTaskRegister")
	},
}

func authorizeVariables(task, role string, order int, orderSet bool, dependsOn []string, level int) map[string]interface{} {
	vars := map[string]interface{}{"taskUuid": task, "role": role}
	if orderSet {
		vars["orderIndex"] = order
	}
	if len(dependsOn) > 0 {
		vars["dependsOn"] = dependsOn
	}
	if level > 0 {
		vars["level"] = level
	}
	return vars
}

var boardsAuthorizeCmd = &cobra.Command{
	Use:   "authorize <task-uuid>",
	Short: "Authorize a task for a role, as the coordinator does",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		runGql(rearm.AgentTaskAuthorize_Operation, authorizeVariables(args[0], boardsRole, boardsOrder,
			cmd.Flags().Changed("order"), boardsDependsOn, boardsLevel), "agentTaskAuthorize")
	},
}

var boardsOrderCmd = &cobra.Command{
	Use:   "order <task-uuid>",
	Short: "Set a task's order; the coordinator may reorder after you",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		runGql(rearm.AgentTaskOrder_Operation, map[string]interface{}{"taskUuid": args[0], "orderIndex": boardsOrder},
			"agentTaskOrder")
	},
}

func completeVariables(task, note string, skip bool) (map[string]interface{}, error) {
	if skip && strings.TrimSpace(note) == "" {
		return nil, fmt.Errorf("--skip-required-roles needs a --note saying why")
	}
	vars := map[string]interface{}{"taskUuid": task}
	if note != "" {
		vars["note"] = note
	}
	if skip {
		vars["skipRequiredRoles"] = true
	}
	return vars, nil
}

var boardsCompleteCmd = &cobra.Command{
	Use:   "complete <task-uuid>",
	Short: "Complete a task; refused while a blocking finding is open or a required role has not passed",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		vars, err := completeVariables(args[0], boardsNote, boardsSkipRequired)
		if err != nil {
			fail(err.Error())
		}
		runGql(rearm.AgentTaskComplete_Operation, vars, "agentTaskComplete")
	},
}

var boardsCancelCmd = &cobra.Command{
	Use:   "cancel <task-uuid>",
	Short: "Cancel a task from any state but COMPLETED",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		vars := map[string]interface{}{"taskUuid": args[0]}
		if boardsNote != "" {
			vars["note"] = boardsNote
		}
		runGql(rearm.AgentTaskCancel_Operation, vars, "agentTaskCancel")
	},
}

// splitPair reads ID=text; the id is everything before the first '='.
func splitPair(flag, v string) (string, string, error) {
	i := strings.Index(v, "=")
	if i <= 0 || i == len(v)-1 {
		return "", "", fmt.Errorf("--%s takes ID=text, got %q", flag, v)
	}
	return strings.TrimSpace(v[:i]), strings.TrimSpace(v[i+1:]), nil
}

// findingDecisions builds the decisions list shared by decide and review. --priority takes ID=N
// to re-prioritise a finding, or a bare N for the findings --file raises.
func findingDecisions(dismiss, accept, priority, file []string) ([]map[string]interface{}, error) {
	var out []map[string]interface{}
	for _, d := range dismiss {
		id, why, err := splitPair("dismiss", d)
		if err != nil {
			return nil, err
		}
		out = append(out, map[string]interface{}{"action": "DISMISS", "findingId": id, "resolution": why})
	}
	for _, a := range accept {
		id, why, err := splitPair("accept", a)
		if err != nil {
			return nil, err
		}
		out = append(out, map[string]interface{}{"action": "ACCEPT", "findingId": id, "resolution": why})
	}
	filePriority := 0
	for _, p := range priority {
		if n, err := strconv.Atoi(strings.TrimSpace(p)); err == nil {
			if filePriority != 0 {
				return nil, fmt.Errorf("only one bare --priority N, for the findings --file raises")
			}
			filePriority = n
			continue
		}
		id, n, err := splitPair("priority", p)
		if err != nil {
			return nil, err
		}
		level, err := strconv.Atoi(n)
		if err != nil {
			return nil, fmt.Errorf("--priority %s: %q is not a number", id, n)
		}
		out = append(out, map[string]interface{}{"action": "SET_PRIORITY", "findingId": id, "priority": level})
	}
	if len(file) > 0 && filePriority == 0 {
		return nil, fmt.Errorf("--file needs --priority N (1 is the highest)")
	}
	if len(file) == 0 && filePriority != 0 {
		return nil, fmt.Errorf("a bare --priority %d applies to --file; to re-prioritise a finding use --priority ID=N", filePriority)
	}
	for _, title := range file {
		if strings.TrimSpace(title) == "" {
			return nil, fmt.Errorf("--file needs a title")
		}
		out = append(out, map[string]interface{}{"action": "FILE", "title": title, "priority": filePriority})
	}
	return out, nil
}

func aboutInput(spec, release string) map[string]interface{} {
	if spec == "" {
		return nil
	}
	about := map[string]interface{}{"specification": strings.ToUpper(spec)}
	if release != "" {
		about["release"] = release
	}
	return about
}

func decideVariables(task, spec string, dismiss, accept, priority, file []string, about, aboutRelease string) (map[string]interface{}, error) {
	switch strings.ToUpper(spec) {
	case "REVIEW_FINDINGS", "TEST_REPORT":
	default:
		return nil, fmt.Errorf("--spec must be REVIEW_FINDINGS or TEST_REPORT")
	}
	decisions, err := findingDecisions(dismiss, accept, priority, file)
	if err != nil {
		return nil, err
	}
	if len(decisions) == 0 {
		return nil, fmt.Errorf("nothing to decide: give --dismiss, --accept, --priority or --file")
	}
	vars := map[string]interface{}{"taskUuid": task, "specification": strings.ToUpper(spec), "decisions": decisions}
	if a := aboutInput(about, aboutRelease); a != nil {
		vars["about"] = a
	}
	return vars, nil
}

var boardsDecideCmd = &cobra.Command{
	Use:   "decide <task-uuid>",
	Short: "Decide findings as one round of the task's REVIEW_FINDINGS or TEST_REPORT",
	Long: `Records one round of decisions on a task's findings. Whatever the round leaves blocking
sends the task back to the role that produces what it is about (--about when the index does
not say yet). Refused while an agent is working the task.

  --dismiss R-3='does not apply because ...'   WITHDRAWN, with the reason
  --accept R-4='risk taken because ...'        ACCEPTED, with the reason
  --priority R-5=2                             a new priority
  --file 'title' --priority 1                  a new finding, numbered P-1, P-2 ... by the board`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		vars, err := decideVariables(args[0], boardsSpec, boardsDismiss, boardsAccept, boardsPriority, boardsFile,
			boardsAbout, boardsAboutRelease)
		if err != nil {
			fail(err.Error())
		}
		runGql(rearm.AgentTaskDecideFindings_Operation, vars, "agentTaskDecideFindings")
	},
}

func answerVariables(task, id, resolution string, withdraw bool, all string, keepHold bool) (map[string]interface{}, error) {
	vars := map[string]interface{}{"taskUuid": task}
	switch {
	case all != "" && id != "":
		return nil, fmt.Errorf("give --id with --resolution, or --all, not both")
	case all != "":
		vars["answerAll"] = all
	case id != "":
		if strings.TrimSpace(resolution) == "" {
			return nil, fmt.Errorf("--id needs --resolution")
		}
		status := "RESOLVED"
		if withdraw {
			status = "WITHDRAWN"
		}
		vars["answers"] = []map[string]interface{}{{"id": id, "status": status, "resolution": resolution}}
	default:
		return nil, fmt.Errorf("give --id with --resolution, or --all")
	}
	if keepHold {
		vars["releaseHold"] = false
	}
	return vars, nil
}

var boardsAnswerCmd = &cobra.Command{
	Use:   "answer <task-uuid>",
	Short: "Answer the questions a task waits on; the answer routes back to whoever asked",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		vars, err := answerVariables(args[0], boardsAnswerId, boardsResolution, boardsWithdraw, boardsAnswerAll, boardsKeepHold)
		if err != nil {
			fail(err.Error())
		}
		runGql(rearm.AgentTaskAnswer_Operation, vars, "agentTaskAnswer")
	},
}

func reviewVariables(task string, approve, reject bool, note string, file, priority []string, about, aboutRelease string) (map[string]interface{}, error) {
	if approve == reject {
		return nil, fmt.Errorf("give exactly one of --approve or --reject")
	}
	vars := map[string]interface{}{"taskUuid": task, "approve": approve}
	if note != "" {
		vars["note"] = note
	}
	if len(file) > 0 || len(priority) > 0 {
		if approve {
			return nil, fmt.Errorf("findings go with --reject")
		}
		findings, err := findingDecisions(nil, nil, priority, file)
		if err != nil {
			return nil, err
		}
		vars["findings"] = findings
	}
	if a := aboutInput(about, aboutRelease); a != nil {
		vars["about"] = a
	}
	return vars, nil
}

var boardsReviewCmd = &cobra.Command{
	Use:   "review <task-uuid>",
	Short: "Your verdict on a human gate: --approve, or --reject with the findings that say why",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		vars, err := reviewVariables(args[0], boardsApprove, boardsReject, boardsNote, boardsFile, boardsPriority,
			boardsAbout, boardsAboutRelease)
		if err != nil {
			fail(err.Error())
		}
		runGql(rearm.AgentTaskHumanReview_Operation, vars, "agentTaskHumanReview")
	},
}

var boardsSignoffCmd = &cobra.Command{
	Use:   "signoff <task-uuid>",
	Short: "Sign off a task queued in a human role",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		outcome := strings.ToUpper(boardsOutcome)
		if outcome != "PASSED" && outcome != "REJECTED" {
			fail("--outcome must be PASSED or REJECTED")
		}
		vars := map[string]interface{}{"taskUuid": args[0], "outcome": outcome}
		if boardsNote != "" {
			vars["note"] = boardsNote
		}
		runGql(rearm.AgentTaskHumanSignOff_Operation, vars, "agentTaskHumanSignOff")
	},
}

var boardsHoldCmd = &cobra.Command{
	Use:   "hold <task-uuid>",
	Short: "Put an operator hold on a task; the coordinator cannot lift it",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		runGql(rearm.AgentTaskOperatorHold_Operation,
			map[string]interface{}{"taskUuid": args[0], "hold": true, "reason": boardsReason}, "agentTaskOperatorHold")
	},
}

var boardsReleaseCmd = &cobra.Command{
	Use:   "release <task-uuid>",
	Short: "Release a hold; on a question hold, --note is the answer",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		vars := map[string]interface{}{"taskUuid": args[0], "hold": false}
		if boardsNote != "" {
			vars["reason"] = boardsNote
		}
		runGql(rearm.AgentTaskOperatorHold_Operation, vars, "agentTaskOperatorHold")
	},
}

var boardsRequireReviewCmd = &cobra.Command{
	Use:   "require-review <task-uuid>",
	Short: "Require a human review on a task (--off to clear)",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		runGql(rearm.AgentTaskRequireHumanReview_Operation,
			map[string]interface{}{"taskUuid": args[0], "value": !boardsOff}, "agentTaskRequireHumanReview")
	},
}

func strengthVariables(task, required string, clear bool) (map[string]interface{}, error) {
	vars := map[string]interface{}{"taskUuid": task}
	switch {
	case clear && required != "":
		return nil, fmt.Errorf("give --required or --clear, not both")
	case clear:
		vars["requiredStrength"] = nil
	case required != "":
		v, err := strconv.ParseFloat(strings.TrimSpace(required), 64)
		if err != nil || v < 0 {
			return nil, fmt.Errorf("--required must be a non-negative number, got %q", required)
		}
		vars["requiredStrength"] = v
	default:
		return nil, fmt.Errorf("give --required N or --clear")
	}
	return vars, nil
}

// budgetVariables are a task budget's variables: dollars as the CLI takes them everywhere, sent in
// micros; --clear sends null, which removes it (task 6f1b348d's setter, a person verb since
// b6d7c308 round 3).
func budgetVariables(task, usd string, clear bool) (map[string]interface{}, error) {
	vars := map[string]interface{}{"taskUuid": task}
	switch {
	case clear && usd != "":
		return nil, fmt.Errorf("give --usd or --clear, not both")
	case clear:
		vars["budgetMicros"] = nil
	case usd != "":
		micros, err := parseBudgetUSD(usd)
		if err != nil {
			return nil, fmt.Errorf("%s", strings.Replace(err.Error(), "--budget", "--usd", 1))
		}
		vars["budgetMicros"] = micros
	default:
		return nil, fmt.Errorf("give --usd N or --clear")
	}
	return vars, nil
}

var boardsBudgetCmd = &cobra.Command{
	Use:   "budget <task-uuid>",
	Short: "Set what one task may spend, in dollars, or --clear; a raise does not release a budget hold",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		vars, err := budgetVariables(args[0], boardsBudget, boardsClear)
		if err != nil {
			fail(err.Error())
		}
		runGql(rearm.AgentTaskSetBudget_Operation, vars, "agentTaskSetBudget")
	},
}

var boardsStrengthCmd = &cobra.Command{
	Use:   "strength <task-uuid>",
	Short: "Require a model of at least this strength for one task, or --clear",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		vars, err := strengthVariables(args[0], boardsStrength, boardsClear)
		if err != nil {
			fail(err.Error())
		}
		runGql(rearm.AgentTaskSetStrength_Operation, vars, "agentTaskSetStrength")
	},
}

var boardsLockCmd = &cobra.Command{
	Use:   "lock <board-uuid>",
	Short: "Operator lock: no new assignments until you unlock; the coordinator cannot lift it",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		runGql(rearm.AgentBoardOperatorLock_Operation,
			map[string]interface{}{"boardUuid": args[0], "lock": true, "reason": boardsReason}, "agentBoardOperatorLock")
	},
}

var boardsUnlockCmd = &cobra.Command{
	Use:   "unlock <board-uuid>",
	Short: "Lift your operator lock",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		runGql(rearm.AgentBoardOperatorLock_Operation,
			map[string]interface{}{"boardUuid": args[0], "lock": false}, "agentBoardOperatorLock")
	},
}

var boardsApplyCmd = &cobra.Command{
	Use:   "apply",
	Short: "Apply a board file (kind: BOARD); the same as `rearm agent board apply`",
	Run: func(cmd *cobra.Command, args []string) {
		runSpecApply("BOARD")
	},
}

func init() {
	boardsTasksCmd.Flags().StringVar(&boardsStatus, "status", "", "only tasks in this status (e.g. QUEUED, ON_HOLD, DELIVERING, COMPLETED)")

	boardsRegisterCmd.Flags().StringVar(&boardsTitle, "title", "", "task title — required")
	boardsRegisterCmd.Flags().StringVar(&boardsExternalRef, "external-ref", "", "tracker reference; required on a board with sources")
	boardsRegisterCmd.Flags().StringVar(&boardsSourceUrl, "source-url", "", "link to the issue")
	boardsRegisterCmd.Flags().StringVar(&boardsParent, "parent", "", "parent task uuid")
	boardsRegisterCmd.Flags().IntVar(&boardsLevel, "level", 0, "task level on the board's target")
	_ = boardsRegisterCmd.MarkFlagRequired("title")

	boardsAuthorizeCmd.Flags().StringVar(&boardsRole, "role", "", "role to authorize the task for — required")
	boardsAuthorizeCmd.Flags().IntVar(&boardsOrder, "order", 0, "order index")
	boardsAuthorizeCmd.Flags().StringSliceVar(&boardsDependsOn, "depends-on", nil, "task uuids this one waits on")
	boardsAuthorizeCmd.Flags().IntVar(&boardsLevel, "level", 0, "task level on the board's target")
	_ = boardsAuthorizeCmd.MarkFlagRequired("role")

	boardsOrderCmd.Flags().IntVar(&boardsOrder, "order", 0, "order index — required")
	_ = boardsOrderCmd.MarkFlagRequired("order")

	boardsCompleteCmd.Flags().StringVar(&boardsNote, "note", "", "why, when you complete someone else's work")
	boardsCompleteCmd.Flags().BoolVar(&boardsSkipRequired, "skip-required-roles", false, "complete without the required roles; needs --note")
	boardsCancelCmd.Flags().StringVar(&boardsNote, "note", "", "why")

	boardsDecideCmd.Flags().StringVar(&boardsSpec, "spec", "", "REVIEW_FINDINGS or TEST_REPORT — required")
	boardsDecideCmd.Flags().StringArrayVar(&boardsDismiss, "dismiss", nil, "ID=why: the finding does not apply")
	boardsDecideCmd.Flags().StringArrayVar(&boardsAccept, "accept", nil, "ID=why: the risk is taken knowingly")
	_ = boardsDecideCmd.MarkFlagRequired("spec")
	for _, c := range []*cobra.Command{boardsDecideCmd, boardsReviewCmd} {
		c.Flags().StringArrayVar(&boardsPriority, "priority", nil, "ID=N to re-prioritise a finding, or N for the findings --file raises")
		c.Flags().StringArrayVar(&boardsFile, "file", nil, "raise a new finding with this title")
		c.Flags().StringVar(&boardsAbout, "about", "", "the specification the findings are about, when the index does not say yet")
		c.Flags().StringVar(&boardsAboutRelease, "about-release", "", "the release the findings are about")
	}

	boardsAnswerCmd.Flags().StringVar(&boardsAnswerId, "id", "", "the question's id")
	boardsAnswerCmd.Flags().StringVar(&boardsResolution, "resolution", "", "the answer")
	boardsAnswerCmd.Flags().BoolVar(&boardsWithdraw, "withdraw", false, "the question does not apply after all")
	boardsAnswerCmd.Flags().StringVar(&boardsAnswerAll, "all", "", "one answer for every open question")
	boardsAnswerCmd.Flags().BoolVar(&boardsKeepHold, "keep-hold", false, "answer without lifting the question hold")

	boardsReviewCmd.Flags().BoolVar(&boardsApprove, "approve", false, "approve the gated hop")
	boardsReviewCmd.Flags().BoolVar(&boardsReject, "reject", false, "reject it; the findings say why")
	boardsReviewCmd.Flags().StringVar(&boardsNote, "note", "", "your note")

	boardsSignoffCmd.Flags().StringVar(&boardsOutcome, "outcome", "", "PASSED or REJECTED — required")
	boardsSignoffCmd.Flags().StringVar(&boardsNote, "note", "", "your note")
	_ = boardsSignoffCmd.MarkFlagRequired("outcome")

	boardsHoldCmd.Flags().StringVar(&boardsReason, "reason", "", "why — required")
	_ = boardsHoldCmd.MarkFlagRequired("reason")
	boardsReleaseCmd.Flags().StringVar(&boardsNote, "note", "", "on a question hold, the answer")

	boardsRequireReviewCmd.Flags().BoolVar(&boardsOff, "off", false, "clear the requirement")

	boardsStrengthCmd.Flags().StringVar(&boardsStrength, "required", "", "the least model strength, e.g. 6.5")
	boardsStrengthCmd.Flags().BoolVar(&boardsClear, "clear", false, "back to what the role asks for")

	boardsBudgetCmd.Flags().StringVar(&boardsBudget, "usd", "", "what the task may spend, in dollars, e.g. 2.50")
	boardsBudgetCmd.Flags().BoolVar(&boardsClear, "clear", false, "remove the task's budget")

	boardsLockCmd.Flags().StringVar(&boardsReason, "reason", "", "why — required")
	_ = boardsLockCmd.MarkFlagRequired("reason")

	boardsApplyCmd.Flags().StringVarP(&specFile, "file", "f", "", "the board file (YAML or JSON)")
	boardsApplyCmd.Flags().BoolVar(&specDryRun, "dry-run", false, "show the change set without applying it")
	boardsApplyCmd.Flags().BoolVar(&specNoSource, "no-source", false, "do not record the file's repository, commit and path")

	for _, c := range []*cobra.Command{boardsListCmd, boardsTasksCmd, boardsRegisterCmd, boardsAuthorizeCmd,
		boardsOrderCmd, boardsCompleteCmd, boardsCancelCmd, boardsDecideCmd, boardsAnswerCmd, boardsReviewCmd,
		boardsSignoffCmd, boardsHoldCmd, boardsReleaseCmd, boardsRequireReviewCmd, boardsStrengthCmd, boardsBudgetCmd,
		boardsLockCmd, boardsUnlockCmd, boardsApplyCmd} {
		boardsCmd.AddCommand(c)
	}
	rootCmd.AddCommand(boardsCmd)
}
