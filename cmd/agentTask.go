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
	"os"

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

const agentBoardFragment = `
	uuid org name description status sources
	coordinatorPrompt missingCapabilities
	events { kind message actor eventAt }
	lock { level reason lockedBy lockedAt }
	coordinatorSeat { session agent claimedAt }
	perAgentWipLimit priorityType createdDate
`

const agentTaskFragment = `
	uuid org board externalRef title sourceUrl status role orderIndex
	dependsOn holdReason
	assignment { session agent role assignedAt promptVersion }
	signOffs { role agent session assignedAt signedOffAt outcome note promptVersion }
	returns { role agent session reason description returnedAt }
	parentTask childTasks sessions prUrls registeredBySession createdDate completedAt
	statusHistory { from to at trigger actor }
`

const agentWorkerAssignmentFragment = `
	task { ` + agentTaskFragment + ` }
	role rolePrompt promptVersion
`

const agentRoleConfigFragment = `
	uuid board org name prompt orderIndex wipLimit requireDistinctAgent active requiredCapabilities
`

var (
	taskBoardUuid    string
	taskExternalRef  string
	taskTitle        string
	taskSourceUrl    string
	taskSessionUuid  string
	taskRole         string
	taskOrder        int
	taskOutcome      string
	taskNote         string
	taskReturnReason string
	taskReturnDesc   string
	taskChildrenJson string
	taskPrUrl        string
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
	data, err := sendGraphQLRequest(query, variables, rearmUri+"/graphql")
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
		runGql(`query { agentBoardsProgrammatic {`+agentBoardFragment+`} }`,
			map[string]interface{}{}, "agentBoardsProgrammatic")
	},
}

var agentBoardShowCmd = &cobra.Command{
	Use:   "show <board-uuid>",
	Short: "Show one board incl. sources, lock state, seat and coordinator prompt",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		runGql(`query ($boardUuid: ID!) { agentBoardProgrammatic(boardUuid: $boardUuid) {`+agentBoardFragment+`} }`,
			map[string]interface{}{"boardUuid": args[0]}, "agentBoardProgrammatic")
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
		runGql(`mutation ($boardUuid: ID!, $sessionUuid: ID!) {
			agentBoardCoordinateProgrammatic(boardUuid: $boardUuid, sessionUuid: $sessionUuid) {`+agentBoardFragment+`} }`,
			map[string]interface{}{"boardUuid": args[0], "sessionUuid": taskSessionUuid},
			"agentBoardCoordinateProgrammatic")
	},
}

func boardLockRun(lock bool) func(cmd *cobra.Command, args []string) {
	return func(cmd *cobra.Command, args []string) {
		variables := map[string]interface{}{"boardUuid": args[0], "sessionUuid": taskSessionUuid, "lock": lock}
		if taskLockReason != "" {
			variables["reason"] = taskLockReason
		}
		runGql(`mutation ($boardUuid: ID!, $sessionUuid: ID!, $lock: Boolean!, $reason: String) {
			agentBoardCoordinatorLockProgrammatic(boardUuid: $boardUuid, sessionUuid: $sessionUuid, lock: $lock, reason: $reason) {`+agentBoardFragment+`} }`,
			variables, "agentBoardCoordinatorLockProgrammatic")
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
		runGql(`mutation ($boardUuid: ID!, $sessionUuid: ID!, $kind: AgentBoardEventKind!, $message: String!) {
			agentBoardPostEventProgrammatic(boardUuid: $boardUuid, sessionUuid: $sessionUuid, kind: $kind, message: $message) {`+agentBoardFragment+`} }`,
			map[string]interface{}{"boardUuid": args[0], "sessionUuid": taskSessionUuid, "kind": taskEventKind, "message": taskNote},
			"agentBoardPostEventProgrammatic")
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
		runGql(`mutation ($boardUuid: ID!, $sessionUuid: ID!, $input: AgentTaskRoleConfigInput!) {
			agentTaskRoleConfigSetProgrammatic(boardUuid: $boardUuid, sessionUuid: $sessionUuid, input: $input) {`+agentRoleConfigFragment+`} }`,
			map[string]interface{}{"boardUuid": args[0], "sessionUuid": taskSessionUuid, "input": input},
			"agentTaskRoleConfigSetProgrammatic")
	},
}

var agentBoardRoleconfigListCmd = &cobra.Command{
	Use:   "list <board-uuid>",
	Short: "List a board's role configs in order",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		runGql(`query ($boardUuid: ID!) { agentTaskRoleConfigsProgrammatic(boardUuid: $boardUuid) {`+agentRoleConfigFragment+`} }`,
			map[string]interface{}{"boardUuid": args[0]}, "agentTaskRoleConfigsProgrammatic")
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
		runGql(`mutation ($input: AgentTaskRegisterInput!) {
			agentTaskRegisterProgrammatic(input: $input) {`+agentTaskFragment+`} }`,
			map[string]interface{}{"input": input}, "agentTaskRegisterProgrammatic")
	},
}

var agentTaskNextCmd = &cobra.Command{
	Use:   "next",
	Short: "Role-less worker poll: lowest-ordered claimable task with role + served prompt, or null",
	Run: func(cmd *cobra.Command, args []string) {
		variables := map[string]interface{}{"sessionUuid": taskSessionUuid}
		if taskBoardUuid != "" {
			variables["boardUuid"] = taskBoardUuid
		}
		runGql(`query ($sessionUuid: ID!, $boardUuid: ID) {
			agentTaskNextProgrammatic(sessionUuid: $sessionUuid, boardUuid: $boardUuid) {`+agentWorkerAssignmentFragment+`} }`,
			variables, "agentTaskNextProgrammatic")
	},
}

var agentTaskAssignCmd = &cobra.Command{
	Use:   "assign <task-uuid>",
	Short: "Bind the task to the calling session (QUEUED -> ASSIGNED; constraints re-checked atomically)",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		runGql(`mutation ($taskUuid: ID!, $sessionUuid: ID!) {
			agentTaskAssignProgrammatic(taskUuid: $taskUuid, sessionUuid: $sessionUuid) {`+agentWorkerAssignmentFragment+`} }`,
			map[string]interface{}{"taskUuid": args[0], "sessionUuid": taskSessionUuid},
			"agentTaskAssignProgrammatic")
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
		runGql(`mutation ($taskUuid: ID!, $sessionUuid: ID!, $outcome: AgentSignOffOutcome!, $note: String) {
			agentTaskSignOffProgrammatic(taskUuid: $taskUuid, sessionUuid: $sessionUuid, outcome: $outcome, note: $note) {`+agentTaskFragment+`} }`,
			variables, "agentTaskSignOffProgrammatic")
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
		runGql(`mutation ($taskUuid: ID!, $sessionUuid: ID!, $reason: AgentTaskReturnReason!, $description: String) {
			agentTaskReturnProgrammatic(taskUuid: $taskUuid, sessionUuid: $sessionUuid, reason: $reason, description: $description) {`+agentTaskFragment+`} }`,
			variables, "agentTaskReturnProgrammatic")
	},
}

var agentTaskAuthorizeCmd = &cobra.Command{
	Use:   "authorize <task-uuid>",
	Short: "Coordinator: authorize the task for a role with priority order and optional dependencies (-> QUEUED)",
	Long: `Queues the task for a role. --depends-on (comma-separated task uuids)
replaces the dependency list: the task stays queued but ineligible for
assignment until every dependency is COMPLETED - lay out the whole
plan up front and the server releases work as dependencies land.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		variables := map[string]interface{}{"taskUuid": args[0], "sessionUuid": taskSessionUuid, "role": taskRole}
		if taskOrder != 0 {
			variables["orderIndex"] = taskOrder
		}
		if len(taskDependsOn) > 0 {
			variables["dependsOn"] = taskDependsOn
		}
		runGql(`mutation ($taskUuid: ID!, $sessionUuid: ID!, $role: String!, $orderIndex: Int, $dependsOn: [ID!]) {
			agentTaskAuthorizeProgrammatic(taskUuid: $taskUuid, sessionUuid: $sessionUuid, role: $role, orderIndex: $orderIndex, dependsOn: $dependsOn) {`+agentTaskFragment+`} }`,
			variables, "agentTaskAuthorizeProgrammatic")
	},
}

var agentTaskHoldCmd = &cobra.Command{
	Use:   "hold <task-uuid>",
	Short: "Coordinator: put the task ON_HOLD pending human input (excluded from polls)",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		runGql(`mutation ($taskUuid: ID!, $sessionUuid: ID!, $reason: String!) {
			agentTaskHoldProgrammatic(taskUuid: $taskUuid, sessionUuid: $sessionUuid, reason: $reason) {`+agentTaskFragment+`} }`,
			map[string]interface{}{"taskUuid": args[0], "sessionUuid": taskSessionUuid, "reason": taskNote},
			"agentTaskHoldProgrammatic")
	},
}

var agentTaskReleaseholdCmd = &cobra.Command{
	Use:   "releasehold <task-uuid>",
	Short: "Coordinator: release a hold back to AWAITING_COORDINATOR",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		runGql(`mutation ($taskUuid: ID!, $sessionUuid: ID!) {
			agentTaskReleaseHoldProgrammatic(taskUuid: $taskUuid, sessionUuid: $sessionUuid) {`+agentTaskFragment+`} }`,
			map[string]interface{}{"taskUuid": args[0], "sessionUuid": taskSessionUuid},
			"agentTaskReleaseHoldProgrammatic")
	},
}

var agentTaskOrderCmd = &cobra.Command{
	Use:   "order <task-uuid>",
	Short: "Coordinator: re-prioritize a task",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		runGql(`mutation ($taskUuid: ID!, $sessionUuid: ID!, $orderIndex: Int!) {
			agentTaskOrderProgrammatic(taskUuid: $taskUuid, sessionUuid: $sessionUuid, orderIndex: $orderIndex) {`+agentTaskFragment+`} }`,
			map[string]interface{}{"taskUuid": args[0], "sessionUuid": taskSessionUuid, "orderIndex": taskOrder},
			"agentTaskOrderProgrammatic")
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
		runGql(`mutation ($taskUuid: ID!, $sessionUuid: ID!, $children: [AgentTaskSplitChildInput!]!) {
			agentTaskSplitProgrammatic(taskUuid: $taskUuid, sessionUuid: $sessionUuid, children: $children) {`+agentTaskFragment+`} }`,
			map[string]interface{}{"taskUuid": args[0], "sessionUuid": taskSessionUuid, "children": children},
			"agentTaskSplitProgrammatic")
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
		runGql(`mutation ($taskUuid: ID!, $sessionUuid: ID!, $note: String) {
			agentTaskCompleteProgrammatic(taskUuid: $taskUuid, sessionUuid: $sessionUuid, note: $note) {`+agentTaskFragment+`} }`,
			variables, "agentTaskCompleteProgrammatic")
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
		runGql(`mutation ($taskUuid: ID!, $sessionUuid: ID!, $note: String) {
			agentTaskCancelProgrammatic(taskUuid: $taskUuid, sessionUuid: $sessionUuid, note: $note) {`+agentTaskFragment+`} }`,
			variables, "agentTaskCancelProgrammatic")
	},
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
		runGql(`mutation ($taskUuid: ID!, $externalRef: String!, $sourceUrl: String) {
			agentTaskBindExternalRefProgrammatic(taskUuid: $taskUuid, externalRef: $externalRef, sourceUrl: $sourceUrl) {`+agentTaskFragment+`} }`,
			variables, "agentTaskBindExternalRefProgrammatic")
	},
}

var agentTaskLinkprCmd = &cobra.Command{
	Use:   "linkpr <task-uuid>",
	Short: "Attach a delivering pull-request URL to the task",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		runGql(`mutation ($taskUuid: ID!, $prUrl: String!) {
			agentTaskLinkPrProgrammatic(taskUuid: $taskUuid, prUrl: $prUrl) {`+agentTaskFragment+`} }`,
			map[string]interface{}{"taskUuid": args[0], "prUrl": taskPrUrl}, "agentTaskLinkPrProgrammatic")
	},
}

var agentTaskShowCmd = &cobra.Command{
	Use:   "show <task-uuid>",
	Short: "Show one task with assignment, sign-offs and returns",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		runGql(`query ($taskUuid: ID!) { agentTaskProgrammatic(taskUuid: $taskUuid) {`+agentTaskFragment+`} }`,
			map[string]interface{}{"taskUuid": args[0]}, "agentTaskProgrammatic")
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
		runGql(`query ($boardUuid: ID!, $status: AgentTaskStatus) {
			agentTasksProgrammatic(boardUuid: $boardUuid, status: $status) {`+agentTaskFragment+`} }`,
			variables, "agentTasksProgrammatic")
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
	_ = agentTaskNextCmd.MarkPersistentFlagRequired("session")

	for _, c := range []*cobra.Command{agentTaskAssignCmd, agentTaskSignoffCmd, agentTaskReturnCmd,
		agentTaskAuthorizeCmd, agentTaskOrderCmd, agentTaskSplitCmd, agentTaskCompleteCmd, agentTaskCancelCmd} {
		c.PersistentFlags().StringVar(&taskSessionUuid, "session", "", "Calling session uuid — required")
		_ = c.MarkPersistentFlagRequired("session")
	}
	agentTaskSignoffCmd.PersistentFlags().StringVar(&taskOutcome, "outcome", "", "PASSED | REJECTED — required")
	agentTaskSignoffCmd.PersistentFlags().StringVar(&taskNote, "note", "", "Sign-off note")
	_ = agentTaskSignoffCmd.MarkPersistentFlagRequired("outcome")
	agentTaskReturnCmd.PersistentFlags().StringVar(&taskReturnReason, "reason", "", "Return reason enum — required")
	agentTaskReturnCmd.PersistentFlags().StringVar(&taskReturnDesc, "description", "", "Free-text detail (required for OTHER)")
	_ = agentTaskReturnCmd.MarkPersistentFlagRequired("reason")
	agentTaskAuthorizeCmd.PersistentFlags().StringVar(&taskRole, "role", "", "Role to queue the task for — required")
	agentTaskAuthorizeCmd.PersistentFlags().IntVar(&taskOrder, "order", 0, "Priority order (lowest served first)")
	agentTaskAuthorizeCmd.PersistentFlags().StringSliceVar(&taskDependsOn, "depends-on", nil, "Task uuids that must be COMPLETED before this one is assignable (replaces the list)")
	_ = agentTaskAuthorizeCmd.MarkPersistentFlagRequired("role")
	agentTaskHoldCmd.PersistentFlags().StringVar(&taskSessionUuid, "session", "", "Coordinator seat session uuid — required")
	agentTaskHoldCmd.PersistentFlags().StringVar(&taskNote, "reason", "", "Why the task waits for a human — required")
	_ = agentTaskHoldCmd.MarkPersistentFlagRequired("session")
	_ = agentTaskHoldCmd.MarkPersistentFlagRequired("reason")
	agentTaskReleaseholdCmd.PersistentFlags().StringVar(&taskSessionUuid, "session", "", "Coordinator seat session uuid — required")
	_ = agentTaskReleaseholdCmd.MarkPersistentFlagRequired("session")
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
	agentTaskBindrefCmd.PersistentFlags().StringVar(&taskExternalRef, "external-ref", "", "Tracker ref — required")
	agentTaskBindrefCmd.PersistentFlags().StringVar(&taskSourceUrl, "source-url", "", "Human-clickable tracker URL")
	_ = agentTaskBindrefCmd.MarkPersistentFlagRequired("external-ref")
	agentTaskLinkprCmd.PersistentFlags().StringVar(&taskPrUrl, "pr-url", "", "Pull request URL — required")
	_ = agentTaskLinkprCmd.MarkPersistentFlagRequired("pr-url")
	agentTaskListCmd.PersistentFlags().StringVar(&taskBoardUuid, "board", "", "Board uuid — required")
	agentTaskListCmd.PersistentFlags().StringVar(&taskStatusFilter, "status", "", "PENDING_INTAKE | QUEUED | ASSIGNED | AWAITING_COORDINATOR | COMPLETED | CANCELLED")
	_ = agentTaskListCmd.MarkPersistentFlagRequired("board")

	agentBoardRoleconfigCmd.AddCommand(agentBoardRoleconfigSetCmd)
	agentBoardRoleconfigCmd.AddCommand(agentBoardRoleconfigListCmd)
	agentBoardCmd.AddCommand(agentBoardListCmd)
	agentBoardCmd.AddCommand(agentBoardShowCmd)
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
	agentTaskCmd.AddCommand(agentTaskSplitCmd)
	agentTaskCmd.AddCommand(agentTaskCompleteCmd)
	agentTaskCmd.AddCommand(agentTaskCancelCmd)
	agentTaskCmd.AddCommand(agentTaskBindrefCmd)
	agentTaskCmd.AddCommand(agentTaskLinkprCmd)
	agentTaskCmd.AddCommand(agentTaskShowCmd)
	agentTaskCmd.AddCommand(agentTaskListCmd)

	agentCmd.AddCommand(agentBoardCmd)
	agentCmd.AddCommand(agentTaskCmd)
}
