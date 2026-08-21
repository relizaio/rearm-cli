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

// Agent task pipeline commands. The external tracker (GitHub issues
// in the base case) stays the human interface and the plan store;
// ReARM is the coordination plane: atomic role-aware claiming with
// leases, pipeline-order enforcement, split lineage, and the
// role-passage audit log. Agents do tracker I/O themselves (gh) and
// report every state transition here.
//
//   rearm agent task register --external-ref <ref> --title <t> [--source-url <u>]
//   rearm agent task next [--session <uuid>]
//   rearm agent task claim <task-uuid> --session <uuid> [--lease-minutes N]
//   rearm agent task done <task-uuid> --outcome PASSED|REJECTED|SKIPPED [--note <n>]
//   rearm agent task split <task-uuid> --children-json '[{"title":"..."}]'
//   rearm agent task bindref <task-uuid> --external-ref <ref> [--source-url <u>]
//   rearm agent task linkpr <task-uuid> --pr-url <u>
//   rearm agent task abandon <task-uuid>
//   rearm agent task cancel <task-uuid> [--note <n>]
//   rearm agent task show <task-uuid> | list [--status S]
//   rearm agent task roleconfig set --name <r> [--prompt-file <f>] [--order N] [--routing] [--require-distinct-agent]
//   rearm agent task roleconfig list

const agentTaskFragment = `
	uuid org externalRef title sourceUrl status
	pipeline currentStageIndex
	rolePassages { role agent session claimedAt completedAt outcome note promptVersion }
	activeClaim { role agent session claimedAt leaseExpiresAt promptVersion }
	parentTask childTasks sessions prUrls createdDate completedAt
`

const agentTaskAssignmentFragment = `
	task { ` + agentTaskFragment + ` }
	role rolePrompt promptVersion
`

var agentTaskCmd = &cobra.Command{
	Use:   "task",
	Short: "Agent task pipeline (register / poll / claim / transition / split)",
	Long: `Coordination plane over an external tracker: tasks registered from
tracker items move through the org's configured role pipeline
(architect, coder, qa, ...). Poll is role-less — the server answers
with the role to assume and its served prompt.`,
}

// shared flags
var (
	taskExternalRef   string
	taskTitle         string
	taskSourceUrl     string
	taskPipeline      []string
	taskParent        string
	taskSessionUuid   string
	taskLeaseMinutes  int
	taskOutcome       string
	taskNote          string
	taskChildrenJson  string
	taskPrUrl         string
	taskStatusFilter  string
	roleName          string
	rolePrompt        string
	rolePromptFile    string
	roleOrder         int
	roleRouting       bool
	roleDistinctAgent bool
	roleInactive      bool
)

var agentTaskRegisterCmd = &cobra.Command{
	Use:   "register",
	Short: "Register a task for an external tracker item (idempotent on external ref)",
	Run: func(cmd *cobra.Command, args []string) {
		query := `
			mutation ($input: AgentTaskRegisterInput!) {
				agentTaskRegisterProgrammatic(input: $input) {` + agentTaskFragment + `}
			}
		`
		input := map[string]interface{}{"title": taskTitle}
		if taskExternalRef != "" {
			input["externalRef"] = taskExternalRef
		}
		if taskSourceUrl != "" {
			input["sourceUrl"] = taskSourceUrl
		}
		if len(taskPipeline) > 0 {
			input["pipeline"] = taskPipeline
		}
		if taskParent != "" {
			input["parentTask"] = taskParent
		}
		data, err := sendGraphQLRequest(query, map[string]interface{}{"input": input}, rearmUri+"/graphql")
		if err != nil {
			printGqlError(err)
			os.Exit(1)
		}
		emitJson(data["agentTaskRegisterProgrammatic"])
	},
}

var agentTaskNextCmd = &cobra.Command{
	Use:   "next",
	Short: "Role-less poll: the next claimable assignment (role + served prompt), or null",
	Run: func(cmd *cobra.Command, args []string) {
		query := `
			query ($sessionUuid: ID) {
				agentTaskNextProgrammatic(sessionUuid: $sessionUuid) {` + agentTaskAssignmentFragment + `}
			}
		`
		variables := map[string]interface{}{}
		if taskSessionUuid != "" {
			variables["sessionUuid"] = taskSessionUuid
		}
		data, err := sendGraphQLRequest(query, variables, rearmUri+"/graphql")
		if err != nil {
			printGqlError(err)
			os.Exit(1)
		}
		emitJson(data["agentTaskNextProgrammatic"])
	},
}

var agentTaskClaimCmd = &cobra.Command{
	Use:   "claim <task-uuid>",
	Short: "Claim the task's current stage (lease-based; re-claim renews the lease)",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		query := `
			mutation ($taskUuid: ID!, $sessionUuid: ID, $leaseMinutes: Int) {
				agentTaskClaimProgrammatic(taskUuid: $taskUuid, sessionUuid: $sessionUuid, leaseMinutes: $leaseMinutes) {` +
			agentTaskAssignmentFragment + `}
			}
		`
		variables := map[string]interface{}{"taskUuid": args[0], "sessionUuid": taskSessionUuid}
		if taskLeaseMinutes > 0 {
			variables["leaseMinutes"] = taskLeaseMinutes
		}
		data, err := sendGraphQLRequest(query, variables, rearmUri+"/graphql")
		if err != nil {
			printGqlError(err)
			os.Exit(1)
		}
		emitJson(data["agentTaskClaimProgrammatic"])
	},
}

var agentTaskDoneCmd = &cobra.Command{
	Use:   "done <task-uuid>",
	Short: "Complete the claimed stage: PASSED/SKIPPED advance, REJECTED bounces back",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		query := `
			mutation ($taskUuid: ID!, $outcome: AgentRolePassageOutcome!, $note: String) {
				agentTaskTransitionProgrammatic(taskUuid: $taskUuid, outcome: $outcome, note: $note) {` +
			agentTaskFragment + `}
			}
		`
		variables := map[string]interface{}{"taskUuid": args[0], "outcome": taskOutcome}
		if taskNote != "" {
			variables["note"] = taskNote
		}
		data, err := sendGraphQLRequest(query, variables, rearmUri+"/graphql")
		if err != nil {
			printGqlError(err)
			os.Exit(1)
		}
		emitJson(data["agentTaskTransitionProgrammatic"])
	},
}

var agentTaskSplitCmd = &cobra.Command{
	Use:   "split <task-uuid>",
	Short: "Split the claimed task into children inheriting the remaining pipeline",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		var children []map[string]interface{}
		if err := json.Unmarshal([]byte(taskChildrenJson), &children); err != nil {
			printGqlError(err)
			os.Exit(1)
		}
		query := `
			mutation ($taskUuid: ID!, $children: [AgentTaskSplitChildInput!]!) {
				agentTaskSplitProgrammatic(taskUuid: $taskUuid, children: $children) {` + agentTaskFragment + `}
			}
		`
		variables := map[string]interface{}{"taskUuid": args[0], "children": children}
		data, err := sendGraphQLRequest(query, variables, rearmUri+"/graphql")
		if err != nil {
			printGqlError(err)
			os.Exit(1)
		}
		emitJson(data["agentTaskSplitProgrammatic"])
	},
}

var agentTaskBindrefCmd = &cobra.Command{
	Use:   "bindref <task-uuid>",
	Short: "Bind the tracker ref of a draft split child once its issue exists",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		query := `
			mutation ($taskUuid: ID!, $externalRef: String!, $sourceUrl: String) {
				agentTaskBindExternalRefProgrammatic(taskUuid: $taskUuid, externalRef: $externalRef, sourceUrl: $sourceUrl) {` +
			agentTaskFragment + `}
			}
		`
		variables := map[string]interface{}{"taskUuid": args[0], "externalRef": taskExternalRef}
		if taskSourceUrl != "" {
			variables["sourceUrl"] = taskSourceUrl
		}
		data, err := sendGraphQLRequest(query, variables, rearmUri+"/graphql")
		if err != nil {
			printGqlError(err)
			os.Exit(1)
		}
		emitJson(data["agentTaskBindExternalRefProgrammatic"])
	},
}

var agentTaskLinkprCmd = &cobra.Command{
	Use:   "linkpr <task-uuid>",
	Short: "Attach a delivering pull-request URL to the task",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		query := `
			mutation ($taskUuid: ID!, $prUrl: String!) {
				agentTaskLinkPrProgrammatic(taskUuid: $taskUuid, prUrl: $prUrl) {` + agentTaskFragment + `}
			}
		`
		variables := map[string]interface{}{"taskUuid": args[0], "prUrl": taskPrUrl}
		data, err := sendGraphQLRequest(query, variables, rearmUri+"/graphql")
		if err != nil {
			printGqlError(err)
			os.Exit(1)
		}
		emitJson(data["agentTaskLinkPrProgrammatic"])
	},
}

var agentTaskAbandonCmd = &cobra.Command{
	Use:   "abandon <task-uuid>",
	Short: "Voluntarily release the live claim (task returns to the pool)",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		query := `
			mutation ($taskUuid: ID!) {
				agentTaskReleaseClaimProgrammatic(taskUuid: $taskUuid) {` + agentTaskFragment + `}
			}
		`
		data, err := sendGraphQLRequest(query, map[string]interface{}{"taskUuid": args[0]}, rearmUri+"/graphql")
		if err != nil {
			printGqlError(err)
			os.Exit(1)
		}
		emitJson(data["agentTaskReleaseClaimProgrammatic"])
	},
}

var agentTaskCancelCmd = &cobra.Command{
	Use:   "cancel <task-uuid>",
	Short: "Cancel a task (coordinator/operator action; terminal)",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		query := `
			mutation ($taskUuid: ID!, $note: String) {
				agentTaskCancelProgrammatic(taskUuid: $taskUuid, note: $note) {` + agentTaskFragment + `}
			}
		`
		variables := map[string]interface{}{"taskUuid": args[0]}
		if taskNote != "" {
			variables["note"] = taskNote
		}
		data, err := sendGraphQLRequest(query, variables, rearmUri+"/graphql")
		if err != nil {
			printGqlError(err)
			os.Exit(1)
		}
		emitJson(data["agentTaskCancelProgrammatic"])
	},
}

var agentTaskShowCmd = &cobra.Command{
	Use:   "show <task-uuid>",
	Short: "Show one task with its full pipeline state and passage log",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		query := `
			query ($taskUuid: ID!) {
				agentTaskProgrammatic(taskUuid: $taskUuid) {` + agentTaskFragment + `}
			}
		`
		data, err := sendGraphQLRequest(query, map[string]interface{}{"taskUuid": args[0]}, rearmUri+"/graphql")
		if err != nil {
			printGqlError(err)
			os.Exit(1)
		}
		emitJson(data["agentTaskProgrammatic"])
	},
}

var agentTaskListCmd = &cobra.Command{
	Use:   "list",
	Short: "List the org's tasks, optionally by status (OPEN / COMPLETED / CANCELLED)",
	Run: func(cmd *cobra.Command, args []string) {
		query := `
			query ($status: AgentTaskStatus) {
				agentTasksProgrammatic(status: $status) {` + agentTaskFragment + `}
			}
		`
		variables := map[string]interface{}{}
		if taskStatusFilter != "" {
			variables["status"] = taskStatusFilter
		}
		data, err := sendGraphQLRequest(query, variables, rearmUri+"/graphql")
		if err != nil {
			printGqlError(err)
			os.Exit(1)
		}
		emitJson(data["agentTasksProgrammatic"])
	},
}

var agentTaskRoleconfigCmd = &cobra.Command{
	Use:   "roleconfig",
	Short: "Org role configuration (name, served prompt, pipeline order, flags)",
}

var agentTaskRoleconfigSetCmd = &cobra.Command{
	Use:   "set",
	Short: "Upsert a role config; --prompt-file wins over --prompt",
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
		query := `
			mutation ($input: AgentTaskRoleConfigInput!) {
				agentTaskRoleConfigSetProgrammatic(input: $input) {
					uuid org name prompt orderIndex routing requireDistinctAgent active
				}
			}
		`
		input := map[string]interface{}{
			"name":                 roleName,
			"orderIndex":           roleOrder,
			"routing":              roleRouting,
			"requireDistinctAgent": roleDistinctAgent,
			"active":               !roleInactive,
		}
		if prompt != "" {
			input["prompt"] = prompt
		}
		data, err := sendGraphQLRequest(query, map[string]interface{}{"input": input}, rearmUri+"/graphql")
		if err != nil {
			printGqlError(err)
			os.Exit(1)
		}
		emitJson(data["agentTaskRoleConfigSetProgrammatic"])
	},
}

var agentTaskRoleconfigListCmd = &cobra.Command{
	Use:   "list",
	Short: "List the org's role configs in pipeline order",
	Run: func(cmd *cobra.Command, args []string) {
		query := `
			query {
				agentTaskRoleConfigsProgrammatic {
					uuid org name prompt orderIndex routing requireDistinctAgent active
				}
			}
		`
		data, err := sendGraphQLRequest(query, map[string]interface{}{}, rearmUri+"/graphql")
		if err != nil {
			printGqlError(err)
			os.Exit(1)
		}
		emitJson(data["agentTaskRoleConfigsProgrammatic"])
	},
}

func init() {
	agentTaskRegisterCmd.PersistentFlags().StringVar(&taskExternalRef, "external-ref", "", "Canonical tracker ref, e.g. github:owner/repo#123")
	agentTaskRegisterCmd.PersistentFlags().StringVar(&taskTitle, "title", "", "Task title — required")
	agentTaskRegisterCmd.PersistentFlags().StringVar(&taskSourceUrl, "source-url", "", "Human-clickable tracker URL")
	agentTaskRegisterCmd.PersistentFlags().StringSliceVar(&taskPipeline, "pipeline", nil, "Explicit pipeline override (role names in order)")
	agentTaskRegisterCmd.PersistentFlags().StringVar(&taskParent, "parent", "", "Parent task uuid")
	_ = agentTaskRegisterCmd.MarkPersistentFlagRequired("title")

	agentTaskNextCmd.PersistentFlags().StringVar(&taskSessionUuid, "session", "", "Session uuid identifying the polling agent (enables separation-of-duties filtering)")

	agentTaskClaimCmd.PersistentFlags().StringVar(&taskSessionUuid, "session", "", "Session uuid identifying the claiming agent — required")
	agentTaskClaimCmd.PersistentFlags().IntVar(&taskLeaseMinutes, "lease-minutes", 0, "Claim lease duration (default 60)")
	_ = agentTaskClaimCmd.MarkPersistentFlagRequired("session")

	agentTaskDoneCmd.PersistentFlags().StringVar(&taskOutcome, "outcome", "", "PASSED | REJECTED | SKIPPED — required")
	agentTaskDoneCmd.PersistentFlags().StringVar(&taskNote, "note", "", "Free-text note recorded on the passage")
	_ = agentTaskDoneCmd.MarkPersistentFlagRequired("outcome")

	agentTaskSplitCmd.PersistentFlags().StringVar(&taskChildrenJson, "children-json", "", `JSON array of children, e.g. '[{"title":"part 1"},{"title":"part 2"}]' — required`)
	_ = agentTaskSplitCmd.MarkPersistentFlagRequired("children-json")

	agentTaskBindrefCmd.PersistentFlags().StringVar(&taskExternalRef, "external-ref", "", "Canonical tracker ref — required")
	agentTaskBindrefCmd.PersistentFlags().StringVar(&taskSourceUrl, "source-url", "", "Human-clickable tracker URL")
	_ = agentTaskBindrefCmd.MarkPersistentFlagRequired("external-ref")

	agentTaskLinkprCmd.PersistentFlags().StringVar(&taskPrUrl, "pr-url", "", "Pull request URL — required")
	_ = agentTaskLinkprCmd.MarkPersistentFlagRequired("pr-url")

	agentTaskCancelCmd.PersistentFlags().StringVar(&taskNote, "note", "", "Cancellation reason")

	agentTaskListCmd.PersistentFlags().StringVar(&taskStatusFilter, "status", "", "OPEN | COMPLETED | CANCELLED")

	agentTaskRoleconfigSetCmd.PersistentFlags().StringVar(&roleName, "name", "", "Role name — required")
	agentTaskRoleconfigSetCmd.PersistentFlags().StringVar(&rolePrompt, "prompt", "", "Role prompt text")
	agentTaskRoleconfigSetCmd.PersistentFlags().StringVar(&rolePromptFile, "prompt-file", "", "File containing the role prompt (wins over --prompt)")
	agentTaskRoleconfigSetCmd.PersistentFlags().IntVar(&roleOrder, "order", 0, "Pipeline order index (ascending)")
	agentTaskRoleconfigSetCmd.PersistentFlags().BoolVar(&roleRouting, "routing", false, "Routing role (coordinator) — excluded from task pipelines")
	agentTaskRoleconfigSetCmd.PersistentFlags().BoolVar(&roleDistinctAgent, "require-distinct-agent", false, "Claiming agent must differ from the previous stage's agent")
	agentTaskRoleconfigSetCmd.PersistentFlags().BoolVar(&roleInactive, "inactive", false, "Deactivate the role (excluded from default pipelines)")
	_ = agentTaskRoleconfigSetCmd.MarkPersistentFlagRequired("name")

	agentTaskRoleconfigCmd.AddCommand(agentTaskRoleconfigSetCmd)
	agentTaskRoleconfigCmd.AddCommand(agentTaskRoleconfigListCmd)

	agentTaskCmd.AddCommand(agentTaskRegisterCmd)
	agentTaskCmd.AddCommand(agentTaskNextCmd)
	agentTaskCmd.AddCommand(agentTaskClaimCmd)
	agentTaskCmd.AddCommand(agentTaskDoneCmd)
	agentTaskCmd.AddCommand(agentTaskSplitCmd)
	agentTaskCmd.AddCommand(agentTaskBindrefCmd)
	agentTaskCmd.AddCommand(agentTaskLinkprCmd)
	agentTaskCmd.AddCommand(agentTaskAbandonCmd)
	agentTaskCmd.AddCommand(agentTaskCancelCmd)
	agentTaskCmd.AddCommand(agentTaskShowCmd)
	agentTaskCmd.AddCommand(agentTaskListCmd)
	agentTaskCmd.AddCommand(agentTaskRoleconfigCmd)
	agentCmd.AddCommand(agentTaskCmd)
}
