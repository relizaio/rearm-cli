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
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/relizaio/rearm-client-go"
	"github.com/spf13/cobra"
)

// Agent task boards: hub-and-spoke task distribution over an external
// tracker. The board's coordinator (singleton, session-bound seat)
// authorizes and orders tasks; workers pull role-lessly and every
// sign-off or return redirects the task back to the coordinator. The
// authoritative design doc is served by the backend team; the runtime
// contract for agents is $REARM_URL/api/agents/orientation.md.
//
//   rearm agent board list | show <board-uuid>
//   rearm agent board coordinate <board-uuid> --session <uuid>
//   rearm agent board lock|unlock <board-uuid> --session <uuid> [--reason r]
//   rearm agent board roleconfig set <board-uuid> --session <uuid> --name <r> [...]
//   rearm agent board roleconfig list <board-uuid>
//   rearm agent task register --board <uuid> --external-ref <ref> --title <t> [--session <uuid>]
//   rearm agent task next --session <uuid> [--board <uuid>]
//   rearm agent task assign <task-uuid> --session <uuid>
//   rearm agent task signoff <task-uuid> --session <uuid> --outcome PASSED|REJECTED [--note n]
//   rearm agent task return <task-uuid> --session <uuid> --reason <enum> [--description d]
//   rearm agent task authorize <task-uuid> --session <uuid> --role <r> [--order N]
//   rearm agent task order <task-uuid> --session <uuid> --order N
//   rearm agent task split <task-uuid> --session <uuid> --children-json '[{"title":"..."}]'
//   rearm agent task complete|cancel <task-uuid> --session <uuid> [--note n]
//   rearm agent task bindref <task-uuid> --external-ref <ref> [--source-url u]
//   rearm agent task linkpr <task-uuid> --pr-url <u>
//   rearm agent task show <task-uuid> | list --board <uuid> [--status S]

var (
	taskBoardUuid    string
	taskExternalRef  string
	taskTitle        string
	taskSourceUrl    string
	taskSessionUuid  string
	taskRole         string
	taskReopenReason string
	taskOrder        int
	taskOutcome      string
	taskNote         string
	taskReturnReason string
	taskReturnDesc   string
	taskChildrenJson string
	taskPrUrl        string
	taskOutputs      []string
	taskRoles        []string
	taskStrength     string
	taskBudget       string
	taskStatusFilter string
	taskLockReason   string
	taskDependsOn    []string
	taskEventKind    string
	roleName         string
	rolePrompt       string
	rolePromptFile   string
	roleOrder        int
	roleWipLimit     int
	roleDistinct     bool
	roleInactive     bool
)

func runGql(query string, variables map[string]interface{}, key string) {
	data, err := sendGraphQLRequest(query, variables)
	if err != nil {
		printGqlError(err)
		os.Exit(1)
	}
	emitJson(data[key])
}

// ---------- board ----------

var agentBoardCmd = &cobra.Command{
	Use:   "board",
	Short: "Agent task boards (list / show / coordinate / lock / roleconfig)",
}

var agentBoardListCmd = &cobra.Command{
	Use:   "list",
	Short: "List the org's boards",
	Run: func(cmd *cobra.Command, args []string) {
		runGql(rearm.AgentBoardsProgrammatic_Operation, map[string]interface{}{}, "agentBoardsProgrammatic")
	},
}

var agentBoardShowCmd = &cobra.Command{
	Use:   "show <board-uuid>",
	Short: "Show one board incl. sources, lock state, seat and coordinator prompt",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		runGql(rearm.AgentBoardProgrammatic_Operation, map[string]interface{}{"boardUuid": args[0]}, "agentBoardProgrammatic")
	},
}

var agentBoardSnapshotCmd = &cobra.Command{
	Use:   "snapshot <board-uuid>",
	Short: "Every task on the board: who holds it, what it waits on, its documents and questions",
	Long: `Returns the whole board in one call: each task with its holder, its
dependencies WITH their statuses, the newest release of each document type,
and the question it is waiting on if any.

Use this instead of listing tasks and then asking about each one. Nothing in
progress appears here -- a hop has no outputs until it signs off -- so what you
see is what has actually been published.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		runGql(rearm.AgentBoardSnapshotProgrammatic_Operation,
			map[string]interface{}{"boardUuid": args[0]}, "agentBoardSnapshotProgrammatic")
	},
}

var agentBoardCoordinateCmd = &cobra.Command{
	Use:   "coordinate <board-uuid>",
	Short: "Claim the board's singleton coordinator seat for the calling session",
	Long: `Claims the coordinator seat. The seat is held until the session closes
and the seat session can take no task assignments. Returns the board
including coordinatorPrompt - assume it.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		runGql(rearm.AgentBoardCoordinateProgrammatic_Operation, map[string]interface{}{"boardUuid": args[0], "sessionUuid": taskSessionUuid}, "agentBoardCoordinateProgrammatic")
		// Recorded so a component-scoped `doc publish`, which names no task, can still tell which
		// board it belongs to. Only the seat gives a session a board without a task.
		rememberBoard(taskSessionUuid, args[0])
	},
}

func boardLockRun(lock bool) func(cmd *cobra.Command, args []string) {
	return func(cmd *cobra.Command, args []string) {
		variables := map[string]interface{}{"boardUuid": args[0], "sessionUuid": taskSessionUuid, "lock": lock}
		if taskLockReason != "" {
			variables["reason"] = taskLockReason
		}
		runGql(rearm.AgentBoardCoordinatorLockProgrammatic_Operation, variables, "agentBoardCoordinatorLockProgrammatic")
	}
}

var agentBoardLockCmd = &cobra.Command{
	Use:   "lock <board-uuid>",
	Short: "Coordinator lock: stop new assignments (cannot touch an OPERATOR lock)",
	Args:  cobra.ExactArgs(1),
	Run:   boardLockRun(true),
}

var agentBoardUnlockCmd = &cobra.Command{
	Use:   "unlock <board-uuid>",
	Short: "Lift a coordinator lock",
	Args:  cobra.ExactArgs(1),
	Run:   boardLockRun(false),
}

var agentBoardPosteventCmd = &cobra.Command{
	Use:   "postevent <board-uuid>",
	Short: "Coordinator: post an ALERT or INFO notice to the board's event feed",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		runGql(rearm.AgentBoardPostEventProgrammatic_Operation, map[string]interface{}{"boardUuid": args[0], "sessionUuid": taskSessionUuid, "kind": taskEventKind, "message": taskNote}, "agentBoardPostEventProgrammatic")
	},
}

var agentBoardRoleconfigCmd = &cobra.Command{
	Use:   "roleconfig",
	Short: "Board role configuration (the coordinator role is implicit, never listed)",
}

var agentBoardRoleconfigSetCmd = &cobra.Command{
	Use:   "set <board-uuid>",
	Short: "Upsert a role config (coordinator seat required); --prompt-file wins over --prompt",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		prompt := rolePrompt
		if rolePromptFile != "" {
			b, err := os.ReadFile(rolePromptFile)
			if err != nil {
				printGqlError(err)
				os.Exit(1)
			}
			prompt = string(b)
		}
		input := map[string]interface{}{
			"name":                 roleName,
			"orderIndex":           roleOrder,
			"requireDistinctAgent": roleDistinct,
			"active":               !roleInactive,
		}
		if prompt != "" {
			input["prompt"] = prompt
		}
		if roleWipLimit > 0 {
			input["wipLimit"] = roleWipLimit
		}
		runGql(rearm.AgentTaskRoleConfigSetProgrammatic_Operation, map[string]interface{}{"boardUuid": args[0], "sessionUuid": taskSessionUuid, "input": input}, "agentTaskRoleConfigSetProgrammatic")
	},
}

var agentBoardRoleconfigListCmd = &cobra.Command{
	Use:   "list <board-uuid>",
	Short: "List a board's role configs in order",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		runGql(rearm.AgentTaskRoleConfigsProgrammatic_Operation, map[string]interface{}{"boardUuid": args[0]}, "agentTaskRoleConfigsProgrammatic")
	},
}

// ---------- task ----------

var agentTaskCmd = &cobra.Command{
	Use:   "task",
	Short: "Agent tasks on a board (register / next / assign / signoff / return / hub ops)",
	Long: `Hub-and-spoke task lifecycle: the coordinator authorizes and orders
tasks, workers pull role-lessly (the server answers with the role to
assume and its served prompt), and every sign-off or return redirects
the task back to the coordinator.`,
}

var agentTaskRegisterCmd = &cobra.Command{
	Use:   "register",
	Short: "Register a tracker item as a PENDING_INTAKE task (idempotent; source-validated)",
	Run: func(cmd *cobra.Command, args []string) {
		input := map[string]interface{}{"boardUuid": taskBoardUuid, "title": taskTitle}
		if taskExternalRef != "" {
			input["externalRef"] = taskExternalRef
		}
		if taskSourceUrl != "" {
			input["sourceUrl"] = taskSourceUrl
		}
		if taskSessionUuid != "" {
			input["sessionUuid"] = taskSessionUuid
		}
		runGql(rearm.AgentTaskRegisterProgrammatic_Operation, map[string]interface{}{"input": input}, "agentTaskRegisterProgrammatic")
	},
}

var agentTaskNextCmd = &cobra.Command{
	Use:   "next",
	Short: "Worker poll: lowest-ordered claimable task with role + served prompt, or null",
	Long: `Asks for the next task this session may take.

--role declares the roles this agent can take -- a role name on the board
(any case) or a role uuid; repeat it or separate with commas. Only tasks
for those roles are offered, and nothing when none match or none is open.
Without --role every role is considered. The declaration is remembered
for this session, so a following 'task assign' passes the same roles.`,
	Run: func(cmd *cobra.Command, args []string) {
		variables := map[string]interface{}{"sessionUuid": taskSessionUuid}
		if taskBoardUuid != "" {
			variables["boardUuid"] = taskBoardUuid
		}
		roles := requireRolesIfGiven(cmd)
		if len(roles) > 0 {
			variables["roles"] = roles
		}
		data, err := sendGraphQLRequest(rearm.AgentTaskNextProgrammatic_Operation, variables)
		if err != nil {
			printGqlError(err)
			os.Exit(1)
		}
		// Remembered only once the server has answered, and cleared by an undeclared poll, so
		// assign never carries a declaration the agent has since dropped.
		setDeclaredRoles(taskSessionUuid, roles)
		offered := data["agentTaskNextProgrammatic"]
		if offered == nil && len(roles) > 0 {
			fmt.Fprintf(os.Stderr, "rearm: no open task for roles %s (names are per board; "+
				"'rearm agent board roleconfig list' shows them)\n", strings.Join(roles, ", "))
		}
		emitJson(offered)
	},
}

// parseRequiredStrength reads --required-strength as a number. The coordinator can only raise a
// requirement, so there is no way to clear one here. Precision and the raise-only rule are the
// server's to enforce, so their refusals arrive with the server's message.
// parseBudgetUSD reads --budget: dollars, as typed, into the USD micros the server stores. Refuses
// anything that is not a non-negative amount with at most six decimals (a micro is the unit).
func parseBudgetUSD(v string) (int64, error) {
	s := strings.TrimPrefix(strings.TrimSpace(v), "$")
	if s == "" {
		return 0, fmt.Errorf("--budget must be an amount in dollars, got %q", v)
	}
	whole, frac, hasFrac := strings.Cut(s, ".")
	if whole == "" {
		whole = "0"
	}
	if hasFrac && (frac == "" || len(frac) > 6) {
		return 0, fmt.Errorf("--budget must have between one and six decimals, got %q", v)
	}
	w, err := strconv.ParseInt(whole, 10, 64)
	if err != nil || w < 0 {
		return 0, fmt.Errorf("--budget must be a non-negative amount in dollars, got %q", v)
	}
	micros := w * 1_000_000
	if hasFrac {
		f, err := strconv.ParseInt((frac + "000000")[:6], 10, 64)
		if err != nil || strings.HasPrefix(frac, "-") || strings.HasPrefix(frac, "+") {
			return 0, fmt.Errorf("--budget must be a non-negative amount in dollars, got %q", v)
		}
		micros += f
	}
	return micros, nil
}

func parseRequiredStrength(v string) (float64, error) {
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil {
		return 0, fmt.Errorf("--required-strength must be a number, got %q", v)
	}
	return f, nil
}

// requireRolesIfGiven returns the declared roles, refusing a --role that was passed but is empty.
// `--role "$ROLE"` with ROLE unset would otherwise read as no declaration at all, and an agent
// that meant to restrict itself would be offered every role.
func requireRolesIfGiven(cmd *cobra.Command) []string {
	roles := cleanRoles(taskRoles)
	if cmd.Flags().Changed("role") && len(roles) == 0 {
		fmt.Fprintln(os.Stderr, "rearm: --role was given but is empty; name a role, or leave --role out to consider every role")
		os.Exit(1)
	}
	return roles
}

// cleanRoles trims the declared roles and drops blanks.
func cleanRoles(in []string) []string {
	var out []string
	for _, r := range in {
		if t := strings.TrimSpace(r); t != "" {
			out = append(out, t)
		}
	}
	return out
}

var agentTaskAssignCmd = &cobra.Command{
	Use:   "assign <task-uuid>",
	Short: "Bind the task to the calling session (QUEUED -> ASSIGNED; constraints re-checked atomically)",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		variables := map[string]interface{}{"taskUuid": args[0], "sessionUuid": taskSessionUuid}
		// An explicit --role wins; otherwise the roles the last 'task next' declared, so a STRICT
		// board ranks this agent only against work it could be offered.
		roles := requireRolesIfGiven(cmd)
		if len(roles) == 0 {
			roles = declaredRoles(taskSessionUuid)
		}
		if len(roles) > 0 {
			variables["roles"] = roles
		}
		runGql(rearm.AgentTaskAssignProgrammatic_Operation, variables, "agentTaskAssignProgrammatic")
		// Record the assignment locally so the usage hooks attribute this session's spend to it
		// without the agent having to pass --task on every turn. Runs only after the server
		// accepted the assignment, so the local file never claims a task the session does not hold.
		setCurrentTask(taskSessionUuid, args[0])
	},
}

var agentTaskSignoffCmd = &cobra.Command{
	Use:   "signoff <task-uuid>",
	Short: "Record the hop's sign-off (PASSED/REJECTED); the task redirects to the coordinator",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		variables := map[string]interface{}{"taskUuid": args[0], "sessionUuid": taskSessionUuid, "outcome": taskOutcome}
		if taskNote != "" {
			variables["note"] = taskNote
		}
		// Documents published during this hop. Sent from local state when the flag is absent, so
		// the ordinary flow is publish then sign off with no uuids copied by hand.
		if outputs := resolveOutputs(taskSessionUuid, args[0]); len(outputs) > 0 {
			variables["outputs"] = outputs
		}
		runGql(rearm.AgentTaskSignOffProgrammatic_Operation, variables, "agentTaskSignOffProgrammatic")
		// The hop is closed; usage after this point is not this task's.
		clearCurrentTask(taskSessionUuid, args[0])
	},
}

var agentTaskReturnCmd = &cobra.Command{
	Use:   "return <task-uuid>",
	Short: "Hand the task back to the coordinator with a reason",
	Long:  `Reasons: TASK_UNCLEAR | ROLE_MISMATCH | MISSING_CAPABILITY | BLOCKED_ON_DEPENDENCY | NEEDS_HUMAN | OTHER (OTHER requires --description).`,
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		variables := map[string]interface{}{"taskUuid": args[0], "sessionUuid": taskSessionUuid, "reason": taskReturnReason}
		if taskReturnDesc != "" {
			variables["description"] = taskReturnDesc
		}
		// Nothing is required of a return, but partial findings from an aborted hop are worth far
		// more to the next agent than a free-text reason.
		if outputs := resolveOutputs(taskSessionUuid, args[0]); len(outputs) > 0 {
			variables["outputs"] = outputs
		}
		runGql(rearm.AgentTaskReturnProgrammatic_Operation, variables, "agentTaskReturnProgrammatic")
		clearCurrentTask(taskSessionUuid, args[0])
	},
}

var agentTaskAuthorizeCmd = &cobra.Command{
	Use:   "authorize <task-uuid>",
	Short: "Coordinator: authorize the task for a role with priority order and optional dependencies (-> QUEUED)",
	Long: `Queues the task for a role. --depends-on (comma-separated task uuids)
replaces the dependency list: the task stays queued but ineligible for
assignment until every dependency is COMPLETED - lay out the whole
plan up front and the server releases work as dependencies land.

--required-strength raises the model strength this task needs above what
its role usually asks, for work harder than the role usually is: a number
with at most two decimals. Raise only -- the server refuses a value below
the task's current requirement (its own, else the role's floor). To lower
or clear one, ask the operator. Left out, the requirement is unchanged.

--budget seeds what the task may spend, in dollars (for example 2.50). Seed
only: the server refuses it when the task already has a budget -- changing
one is the operator's decision. Left out, the budget is unchanged.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		variables := map[string]interface{}{"taskUuid": args[0], "sessionUuid": taskSessionUuid, "role": taskRole}
		if taskOrder != 0 {
			variables["orderIndex"] = taskOrder
		}
		if len(taskDependsOn) > 0 {
			variables["dependsOn"] = taskDependsOn
		}
		if cmd.Flags().Changed("required-strength") {
			strength, err := parseRequiredStrength(taskStrength)
			if err != nil {
				fmt.Fprintln(os.Stderr, "rearm:", err)
				os.Exit(1)
			}
			variables["requiredStrength"] = strength
		}
		if cmd.Flags().Changed("budget") {
			micros, err := parseBudgetUSD(taskBudget)
			if err != nil {
				fmt.Fprintln(os.Stderr, "rearm:", err)
				os.Exit(1)
			}
			variables["budgetMicros"] = micros
		}
		runGql(rearm.AgentTaskAuthorizeProgrammatic_Operation, variables, "agentTaskAuthorizeProgrammatic")
	},
}

var agentTaskHoldCmd = &cobra.Command{
	Use:   "hold <task-uuid>",
	Short: "Coordinator: put the task ON_HOLD pending human input (excluded from polls)",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		runGql(rearm.AgentTaskHoldProgrammatic_Operation, map[string]interface{}{"taskUuid": args[0], "sessionUuid": taskSessionUuid, "reason": taskNote}, "agentTaskHoldProgrammatic")
	},
}

var (
	taskReleaseRole    string
	taskReleaseNote    string
	taskEscalateReason string
)

// releaseHoldVars are the variables of a coordinator's release: the role only when one is named,
// so a release without --role lets routing pick, as before (task 4c566d0d); the note only when
// given (task c0a2134c).
func releaseHoldVars(task, session, role, note string) map[string]interface{} {
	vars := map[string]interface{}{"taskUuid": task, "sessionUuid": session}
	if r := strings.TrimSpace(role); r != "" {
		vars["role"] = r
	}
	if n := strings.TrimSpace(note); n != "" {
		vars["note"] = n
	}
	return vars
}

// escalateHoldVars are the variables of a coordinator's escalation (task c0a2134c).
func escalateHoldVars(task, session, reason string) (map[string]interface{}, error) {
	r := strings.TrimSpace(reason)
	if r == "" {
		return nil, fmt.Errorf("--reason is required: say what the operator is to decide")
	}
	return map[string]interface{}{"taskUuid": task, "sessionUuid": session, "reason": r}, nil
}

var agentTaskReleaseholdCmd = &cobra.Command{
	Use:   "releasehold <task-uuid>",
	Short: "Coordinator: release a hold; the task routes on from its last hop, or to --role",
	Long: `Releases a COORDINATOR-level hold. The task routes on from its last hop, as routing would
have routed it; --role names an active role on the board to send it to instead. --note says why,
on the board feed.

A no-progress or cycle-cap stop parks at COORDINATOR level first when the board allows it (the
default): releasing it routes past the stop once. One release per stop kind per task; the next
identical stop is the operator's. If it is a judgement call, escalate instead.

An OPERATOR hold (a second loop stop, a budget stop, a person's hold) is the operator's to release.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		runGql(rearm.AgentTaskReleaseHoldProgrammatic_Operation, releaseHoldVars(args[0], taskSessionUuid, taskReleaseRole, taskReleaseNote), "agentTaskReleaseHoldProgrammatic")
	},
}

var agentTaskEscalateCmd = &cobra.Command{
	Use:   "escalate <task-uuid>",
	Short: "Coordinator: hand a COORDINATOR-level hold to the operator, with your recommendation",
	Long: `Moves a COORDINATOR-level hold to OPERATOR level: the hold keeps what it said, with --reason
after it, and the board posts an ALERT naming both. For a judgement call -- two roles disagree, an
item needs accepting. Refused on any other hold.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		vars, err := escalateHoldVars(args[0], taskSessionUuid, taskEscalateReason)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
		runGql(rearm.AgentTaskEscalateHoldProgrammatic_Operation, vars, "agentTaskEscalateHoldProgrammatic")
	},
}

var agentTaskRequireReviewCmd = &cobra.Command{
	Use:   "requirereview <task-uuid>",
	Short: "Coordinator: require human review of the task's next sign-off (add-only; clearing is operator-only)",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		runGql(rearm.AgentTaskRequireHumanReviewProgrammatic_Operation, map[string]interface{}{"taskUuid": args[0], "sessionUuid": taskSessionUuid}, "agentTaskRequireHumanReviewProgrammatic")
	},
}

var agentTaskOrderCmd = &cobra.Command{
	Use:   "order <task-uuid>",
	Short: "Coordinator: re-prioritize a task",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		runGql(rearm.AgentTaskOrderProgrammatic_Operation, map[string]interface{}{"taskUuid": args[0], "sessionUuid": taskSessionUuid, "orderIndex": taskOrder}, "agentTaskOrderProgrammatic")
	},
}

var agentTaskSplitCmd = &cobra.Command{
	Use:   "split <task-uuid>",
	Short: "Coordinator: split into PENDING_INTAKE children (authorize each separately)",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		var children []map[string]interface{}
		if err := json.Unmarshal([]byte(taskChildrenJson), &children); err != nil {
			printGqlError(err)
			os.Exit(1)
		}
		runGql(rearm.AgentTaskSplitProgrammatic_Operation, map[string]interface{}{"taskUuid": args[0], "sessionUuid": taskSessionUuid, "children": children}, "agentTaskSplitProgrammatic")
	},
}

var agentTaskCompleteCmd = &cobra.Command{
	Use:   "complete <task-uuid>",
	Short: "Coordinator: complete the task (requires children complete)",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		variables := map[string]interface{}{"taskUuid": args[0], "sessionUuid": taskSessionUuid}
		if taskNote != "" {
			variables["note"] = taskNote
		}
		runGql(rearm.AgentTaskCompleteProgrammatic_Operation, variables, "agentTaskCompleteProgrammatic")
	},
}

var agentTaskCancelCmd = &cobra.Command{
	Use:   "cancel <task-uuid>",
	Short: "Coordinator: cancel the task (terminal)",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		variables := map[string]interface{}{"taskUuid": args[0], "sessionUuid": taskSessionUuid}
		if taskNote != "" {
			variables["note"] = taskNote
		}
		runGql(rearm.AgentTaskCancelProgrammatic_Operation, variables, "agentTaskCancelProgrammatic")
	},
}

var agentTaskReopenCmd = &cobra.Command{
	Use:   "reopen <task-uuid>",
	Short: "Coordinator: send a COMPLETED task back to a role, with a reason (e.g. its PR no longer merges)",
	Long: `Sends a COMPLETED task back to an active role. The role's earlier passes stop counting,
and whoever read its part re-runs once the redone hop republishes. A cancelled task is not
reopened; register a new one. A round the budget does not cover holds for an operator.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		runGql(rearm.AgentTaskReopenProgrammatic_Operation,
			reopenVariables(args[0], taskSessionUuid, taskRole, taskReopenReason), "agentTaskReopenProgrammatic")
	},
}

// taskStatusHelp lists every task status a list can filter on. DELIVERING is a task whose roles
// have all passed and whose linked PR has not merged yet.
const taskStatusHelp = "PENDING_INTAKE | QUEUED | ASSIGNED | AWAITING_COORDINATOR | ON_HOLD | DELIVERING | COMPLETED | CANCELLED"

// reopenVariables is the reopen mutation's input: every field is required by the server.
func reopenVariables(task, session, role, reason string) map[string]interface{} {
	return map[string]interface{}{"taskUuid": task, "sessionUuid": session, "role": role, "reason": reason}
}

var agentTaskBindrefCmd = &cobra.Command{
	Use:   "bindref <task-uuid>",
	Short: "Bind a draft split child's tracker ref once its issue exists",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		variables := map[string]interface{}{"taskUuid": args[0], "externalRef": taskExternalRef}
		if taskSourceUrl != "" {
			variables["sourceUrl"] = taskSourceUrl
		}
		runGql(rearm.AgentTaskBindExternalRefProgrammatic_Operation, variables, "agentTaskBindExternalRefProgrammatic")
	},
}

var agentTaskLinkprCmd = &cobra.Command{
	Use:   "linkpr <task-uuid>",
	Short: "Attach a delivering pull-request URL to the task",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		runGql(rearm.AgentTaskLinkPrProgrammatic_Operation, map[string]interface{}{"taskUuid": args[0], "prUrl": taskPrUrl}, "agentTaskLinkPrProgrammatic")
	},
}

var agentTaskShowCmd = &cobra.Command{
	Use:   "show <task-uuid>",
	Short: "Show one task with assignment, sign-offs and returns",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		runGql(rearm.AgentTaskProgrammatic_Operation, map[string]interface{}{"taskUuid": args[0]}, "agentTaskProgrammatic")
	},
}

var agentTaskListCmd = &cobra.Command{
	Use:   "list",
	Short: "List a board's tasks, optionally by status",
	Run: func(cmd *cobra.Command, args []string) {
		variables := map[string]interface{}{"boardUuid": taskBoardUuid}
		if taskStatusFilter != "" {
			variables["status"] = taskStatusFilter
		}
		runGql(rearm.AgentTasksProgrammatic_Operation, variables, "agentTasksProgrammatic")
	},
}

func init() {
	// board flags
	agentBoardCoordinateCmd.PersistentFlags().StringVar(&taskSessionUuid, "session", "", "Calling session uuid — required")
	_ = agentBoardCoordinateCmd.MarkPersistentFlagRequired("session")
	for _, c := range []*cobra.Command{agentBoardLockCmd, agentBoardUnlockCmd} {
		c.PersistentFlags().StringVar(&taskSessionUuid, "session", "", "Coordinator seat session uuid — required")
		c.PersistentFlags().StringVar(&taskLockReason, "reason", "", "Lock reason")
		_ = c.MarkPersistentFlagRequired("session")
	}
	agentBoardRoleconfigSetCmd.PersistentFlags().StringVar(&taskSessionUuid, "session", "", "Coordinator seat session uuid — required")
	agentBoardRoleconfigSetCmd.PersistentFlags().StringVar(&roleName, "name", "", "Role name — required")
	agentBoardRoleconfigSetCmd.PersistentFlags().StringVar(&rolePrompt, "prompt", "", "Role prompt text")
	agentBoardRoleconfigSetCmd.PersistentFlags().StringVar(&rolePromptFile, "prompt-file", "", "File containing the role prompt (wins over --prompt)")
	agentBoardRoleconfigSetCmd.PersistentFlags().IntVar(&roleOrder, "order", 0, "Advisory routing order")
	agentBoardRoleconfigSetCmd.PersistentFlags().IntVar(&roleWipLimit, "wip-limit", 0, "Per-role concurrent assignment cap (0 = uncapped)")
	agentBoardRoleconfigSetCmd.PersistentFlags().BoolVar(&roleDistinct, "require-distinct-agent", false, "Assigned agent must differ from the last sign-off's agent")
	agentBoardRoleconfigSetCmd.PersistentFlags().BoolVar(&roleInactive, "inactive", false, "Deactivate the role")
	_ = agentBoardRoleconfigSetCmd.MarkPersistentFlagRequired("session")
	_ = agentBoardRoleconfigSetCmd.MarkPersistentFlagRequired("name")

	// task flags
	agentTaskRegisterCmd.PersistentFlags().StringVar(&taskBoardUuid, "board", "", "Board uuid — required")
	agentTaskRegisterCmd.PersistentFlags().StringVar(&taskExternalRef, "external-ref", "", "Tracker ref, e.g. github:owner/repo#123")
	agentTaskRegisterCmd.PersistentFlags().StringVar(&taskTitle, "title", "", "Task title — required")
	agentTaskRegisterCmd.PersistentFlags().StringVar(&taskSourceUrl, "source-url", "", "Human-clickable tracker URL")
	agentTaskRegisterCmd.PersistentFlags().StringVar(&taskSessionUuid, "session", "", "Registering session (intake provenance)")
	_ = agentTaskRegisterCmd.MarkPersistentFlagRequired("board")
	_ = agentTaskRegisterCmd.MarkPersistentFlagRequired("title")

	agentTaskNextCmd.PersistentFlags().StringVar(&taskSessionUuid, "session", "", "Calling session uuid — required")
	agentTaskNextCmd.PersistentFlags().StringVar(&taskBoardUuid, "board", "", "Restrict the poll to one board")
	agentTaskNextCmd.PersistentFlags().StringSliceVar(&taskRoles, "role", nil,
		"Role this agent can take (name or uuid); repeatable. Only tasks for these roles are offered")
	agentTaskAssignCmd.PersistentFlags().StringSliceVar(&taskRoles, "role", nil,
		"Roles this agent declared; defaults to what the last `task next` declared for this session")
	_ = agentTaskNextCmd.MarkPersistentFlagRequired("session")

	for _, c := range []*cobra.Command{agentTaskAssignCmd, agentTaskSignoffCmd, agentTaskReturnCmd,
		agentTaskAuthorizeCmd, agentTaskOrderCmd, agentTaskSplitCmd, agentTaskCompleteCmd, agentTaskCancelCmd,
		agentTaskReopenCmd} {
		c.PersistentFlags().StringVar(&taskSessionUuid, "session", "", "Calling session uuid — required")
		_ = c.MarkPersistentFlagRequired("session")
	}
	agentTaskSignoffCmd.PersistentFlags().StringVar(&taskOutcome, "outcome", "", "PASSED | REJECTED — required")
	agentTaskSignoffCmd.PersistentFlags().StringVar(&taskNote, "note", "", "Sign-off note")
	agentTaskSignoffCmd.PersistentFlags().StringSliceVar(&taskOutputs, "outputs", nil,
		"Document releases produced by this hop; defaults to what `doc publish` recorded")
	_ = agentTaskSignoffCmd.MarkPersistentFlagRequired("outcome")
	agentTaskReturnCmd.PersistentFlags().StringSliceVar(&taskOutputs, "outputs", nil,
		"Document releases produced before returning; defaults to what `doc publish` recorded")
	agentTaskReturnCmd.PersistentFlags().StringVar(&taskReturnReason, "reason", "", "Return reason enum — required")
	agentTaskReturnCmd.PersistentFlags().StringVar(&taskReturnDesc, "description", "", "Free-text detail (required for OTHER)")
	_ = agentTaskReturnCmd.MarkPersistentFlagRequired("reason")
	agentTaskAuthorizeCmd.PersistentFlags().StringVar(&taskRole, "role", "", "Role to queue the task for — required")
	agentTaskAuthorizeCmd.PersistentFlags().IntVar(&taskOrder, "order", 0, "Priority order (lowest served first)")
	agentTaskAuthorizeCmd.PersistentFlags().StringSliceVar(&taskDependsOn, "depends-on", nil, "Task uuids that must be COMPLETED before this one is assignable (replaces the list)")
	agentTaskAuthorizeCmd.PersistentFlags().StringVar(&taskStrength, "required-strength", "",
		"Raise the model strength this task needs above its role's floor (raise only)")
	agentTaskAuthorizeCmd.PersistentFlags().StringVar(&taskBudget, "budget", "",
		"Seed what the task may spend, in dollars (only when it has no budget yet)")
	_ = agentTaskAuthorizeCmd.MarkPersistentFlagRequired("role")
	agentTaskHoldCmd.PersistentFlags().StringVar(&taskSessionUuid, "session", "", "Coordinator seat session uuid — required")
	agentTaskRequireReviewCmd.PersistentFlags().StringVar(&taskSessionUuid, "session", "", "Coordinator seat session uuid — required")
	agentTaskHoldCmd.PersistentFlags().StringVar(&taskNote, "reason", "", "Why the task waits for a human — required")
	_ = agentTaskHoldCmd.MarkPersistentFlagRequired("session")
	_ = agentTaskHoldCmd.MarkPersistentFlagRequired("reason")
	agentTaskReleaseholdCmd.PersistentFlags().StringVar(&taskSessionUuid, "session", "", "Coordinator seat session uuid — required")
	_ = agentTaskReleaseholdCmd.MarkPersistentFlagRequired("session")
	agentTaskReleaseholdCmd.Flags().StringVar(&taskReleaseRole, "role", "", "Route the released task to this role instead of the one routing would pick")
	agentTaskReleaseholdCmd.Flags().StringVar(&taskReleaseNote, "note", "", "Why, posted to the board feed with the release")
	agentTaskEscalateCmd.PersistentFlags().StringVar(&taskSessionUuid, "session", "", "Coordinator seat session uuid — required")
	_ = agentTaskEscalateCmd.MarkPersistentFlagRequired("session")
	agentTaskEscalateCmd.Flags().StringVar(&taskEscalateReason, "reason", "", "Your recommendation: what the operator is to decide — required")
	_ = agentTaskEscalateCmd.MarkFlagRequired("reason")
	agentBoardPosteventCmd.PersistentFlags().StringVar(&taskSessionUuid, "session", "", "Coordinator seat session uuid — required")
	agentBoardPosteventCmd.PersistentFlags().StringVar(&taskEventKind, "kind", "INFO", "ALERT | INFO")
	agentBoardPosteventCmd.PersistentFlags().StringVar(&taskNote, "message", "", "Notice text — required")
	_ = agentBoardPosteventCmd.MarkPersistentFlagRequired("session")
	_ = agentBoardPosteventCmd.MarkPersistentFlagRequired("message")
	agentTaskOrderCmd.PersistentFlags().IntVar(&taskOrder, "order", 0, "Priority order — required")
	_ = agentTaskOrderCmd.MarkPersistentFlagRequired("order")
	agentTaskSplitCmd.PersistentFlags().StringVar(&taskChildrenJson, "children-json", "", `JSON array of children, e.g. '[{"title":"part 1"}]' — required`)
	_ = agentTaskSplitCmd.MarkPersistentFlagRequired("children-json")
	agentTaskCompleteCmd.PersistentFlags().StringVar(&taskNote, "note", "", "Completion note")
	agentTaskCancelCmd.PersistentFlags().StringVar(&taskNote, "note", "", "Cancellation reason")
	agentTaskReopenCmd.PersistentFlags().StringVar(&taskRole, "role", "", "Role that must redo its part — required")
	agentTaskReopenCmd.PersistentFlags().StringVar(&taskReopenReason, "reason", "", "Why the delivery cannot land — required")
	_ = agentTaskReopenCmd.MarkPersistentFlagRequired("role")
	_ = agentTaskReopenCmd.MarkPersistentFlagRequired("reason")
	agentTaskBindrefCmd.PersistentFlags().StringVar(&taskExternalRef, "external-ref", "", "Tracker ref — required")
	agentTaskBindrefCmd.PersistentFlags().StringVar(&taskSourceUrl, "source-url", "", "Human-clickable tracker URL")
	_ = agentTaskBindrefCmd.MarkPersistentFlagRequired("external-ref")
	agentTaskLinkprCmd.PersistentFlags().StringVar(&taskPrUrl, "pr-url", "", "Pull request URL — required")
	_ = agentTaskLinkprCmd.MarkPersistentFlagRequired("pr-url")
	agentTaskListCmd.PersistentFlags().StringVar(&taskBoardUuid, "board", "", "Board uuid — required")
	agentTaskListCmd.PersistentFlags().StringVar(&taskStatusFilter, "status", "", taskStatusHelp)
	_ = agentTaskListCmd.MarkPersistentFlagRequired("board")

	agentBoardRoleconfigCmd.AddCommand(agentBoardRoleconfigSetCmd)
	agentBoardRoleconfigCmd.AddCommand(agentBoardRoleconfigListCmd)
	agentBoardCmd.AddCommand(agentBoardListCmd)
	agentBoardCmd.AddCommand(agentBoardShowCmd)
	agentBoardCmd.AddCommand(agentBoardSnapshotCmd)
	agentBoardCmd.AddCommand(agentBoardCoordinateCmd)
	agentBoardCmd.AddCommand(agentBoardLockCmd)
	agentBoardCmd.AddCommand(agentBoardUnlockCmd)
	agentBoardCmd.AddCommand(agentBoardPosteventCmd)
	agentBoardCmd.AddCommand(agentBoardRoleconfigCmd)

	agentTaskCmd.AddCommand(agentTaskRegisterCmd)
	agentTaskCmd.AddCommand(agentTaskNextCmd)
	agentTaskCmd.AddCommand(agentTaskAssignCmd)
	agentTaskCmd.AddCommand(agentTaskSignoffCmd)
	agentTaskCmd.AddCommand(agentTaskReturnCmd)
	agentTaskCmd.AddCommand(agentTaskAuthorizeCmd)
	agentTaskCmd.AddCommand(agentTaskOrderCmd)
	agentTaskCmd.AddCommand(agentTaskHoldCmd)
	agentTaskCmd.AddCommand(agentTaskReleaseholdCmd)
	agentTaskCmd.AddCommand(agentTaskEscalateCmd)
	agentTaskCmd.AddCommand(agentTaskRequireReviewCmd)
	agentTaskCmd.AddCommand(agentTaskSplitCmd)
	agentTaskCmd.AddCommand(agentTaskCompleteCmd)
	agentTaskCmd.AddCommand(agentTaskCancelCmd)
	agentTaskCmd.AddCommand(agentTaskReopenCmd)
	agentTaskCmd.AddCommand(agentTaskBindrefCmd)
	agentTaskCmd.AddCommand(agentTaskLinkprCmd)
	agentTaskCmd.AddCommand(agentTaskShowCmd)
	agentTaskCmd.AddCommand(agentTaskListCmd)

	agentCmd.AddCommand(agentBoardCmd)
	agentCmd.AddCommand(agentTaskCmd)
}

// resolveOutputs picks the document releases to send with a sign-off or return.
//
// An explicit --outputs wins; otherwise the ones `doc publish` recorded for this task are taken and
// forgotten. Taking rather than reading matters: the hop is closing, and leaving them would offer
// the same documents at the next hop on the same task, where the server refuses them for falling
// outside the assignment window.
func resolveOutputs(sessionUuid, taskUuid string) []string {
	if len(taskOutputs) > 0 {
		return taskOutputs
	}
	return takePendingOutputs(sessionUuid, taskUuid)
}
