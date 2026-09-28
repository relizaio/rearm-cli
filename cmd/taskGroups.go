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

// Task groups and tags (task-groups-and-tags.md §2-§3, task RD2-29). A group is a board's batch with
// an order, the groups it waits on and a default level; a tag is a free label. Keys are resolved by
// the server: a group key is the board's handle, which the CLI cannot resolve without a read.

var (
	taskGroupKey   string
	taskTagKeys    []string
	taskListGroup  string
	taskListTags   []string
	taskGroupNone  bool
	taskTagAdd     []string
	taskTagRemove  []string
	taskTagsClear  bool
	groupSetKey    string
	groupSetName   string
	groupSetDesc   string
	groupSetOrder  int
	groupSetDeps   []string
	groupSetLevel  int
	groupSetStatus string
	groupSetNoDeps bool
	groupSetNoLvl  bool
)

// withGroupAndTags puts --group and --tag on a register input, when they were given.
func withGroupAndTags(input map[string]interface{}, group string, tags []string) map[string]interface{} {
	if g := strings.TrimSpace(group); g != "" {
		input["group"] = g
	}
	if t := tagInputs(tags); len(t) > 0 {
		input["tags"] = t
	}
	return input
}

// tagInputs are the tags as the API takes them: a key each, blanks dropped.
func tagInputs(keys []string) []map[string]interface{} {
	var out []map[string]interface{}
	for _, k := range keys {
		if t := strings.TrimSpace(k); t != "" {
			out = append(out, map[string]interface{}{"key": t})
		}
	}
	return out
}

// withListFilters narrows a task list to a group and to any of the tags, when given.
func withListFilters(vars map[string]interface{}, group string, tags []string) map[string]interface{} {
	if g := strings.TrimSpace(group); g != "" {
		vars["group"] = g
	}
	var t []string
	for _, k := range tags {
		if s := strings.TrimSpace(k); s != "" {
			t = append(t, s)
		}
	}
	if len(t) > 0 {
		vars["tag"] = t
	}
	return vars
}

// setGroupVariables are a move's variables: a group by key, or null with --none. One of the two.
func setGroupVariables(task, group string, none bool) (map[string]interface{}, error) {
	g := strings.TrimSpace(group)
	if none == (g != "") {
		return nil, fmt.Errorf("give --group <key> to move the task into a group, or --none to take it out of its group")
	}
	vars := map[string]interface{}{"taskUuid": task}
	if none {
		vars["group"] = nil
	} else {
		vars["group"] = g
	}
	return vars, nil
}

// tagsAfter is the task's tag list after adding and removing keys: removed keys go (any case), added
// ones are appended once, and the tags kept keep their values.
func tagsAfter(current []interface{}, add, remove []string) []map[string]interface{} {
	gone := map[string]bool{}
	for _, r := range remove {
		gone[strings.ToLower(strings.TrimSpace(r))] = true
	}
	out := []map[string]interface{}{}
	have := map[string]bool{}
	for _, c := range current {
		t, _ := c.(map[string]interface{})
		k, _ := t["key"].(string)
		if k == "" || gone[strings.ToLower(k)] {
			continue
		}
		tag := map[string]interface{}{"key": k}
		if v, ok := t["value"].(string); ok && v != "" {
			tag["value"] = v
		}
		out = append(out, tag)
		have[strings.ToLower(k)] = true
	}
	for _, a := range add {
		k := strings.TrimSpace(a)
		if k == "" || have[strings.ToLower(k)] || gone[strings.ToLower(k)] {
			continue
		}
		out = append(out, map[string]interface{}{"key": k})
		have[strings.ToLower(k)] = true
	}
	return out
}

// groupInput is a group to create or edit: the key, and only what the command was given, so an edit
// leaves the rest as it is. --no-depends-on clears the dependencies; --no-default-level the level.
func groupInput(cmd *cobra.Command, key string) (map[string]interface{}, error) {
	k := strings.TrimSpace(key)
	if k == "" {
		return nil, fmt.Errorf("--key is required: the group's handle, e.g. basic-board")
	}
	in := map[string]interface{}{"key": k}
	if cmd.Flags().Changed("name") {
		in["name"] = groupSetName
	}
	if cmd.Flags().Changed("description") {
		in["description"] = groupSetDesc
	}
	if cmd.Flags().Changed("order") {
		in["order"] = groupSetOrder
	}
	if cmd.Flags().Changed("depends-on") && groupSetNoDeps {
		return nil, fmt.Errorf("--depends-on and --no-depends-on say opposite things; give one")
	}
	if cmd.Flags().Changed("depends-on") {
		in["dependsOn"] = cleanRoles(groupSetDeps)
	} else if groupSetNoDeps {
		in["dependsOn"] = []string{}
	}
	if cmd.Flags().Changed("default-level") && groupSetNoLvl {
		return nil, fmt.Errorf("--default-level and --no-default-level say opposite things; give one")
	}
	if cmd.Flags().Changed("default-level") {
		if groupSetLevel < 0 || groupSetLevel > 9 {
			return nil, fmt.Errorf("--default-level is 0 to 9")
		}
		in["defaultLevel"] = groupSetLevel
	} else if groupSetNoLvl {
		in["defaultLevel"] = nil
	}
	if cmd.Flags().Changed("status") {
		s := strings.ToUpper(strings.TrimSpace(groupSetStatus))
		if s != "OPEN" && s != "CLOSED" {
			return nil, fmt.Errorf("--status is OPEN or CLOSED")
		}
		in["status"] = s
	}
	return in, nil
}

func statusGroupInput(key, status string) map[string]interface{} {
	return map[string]interface{}{"key": strings.TrimSpace(key), "status": status}
}

// ---------- the agent's verbs (the coordinator seat) ----------

var agentTaskSetGroupCmd = &cobra.Command{
	Use:   "setgroup <task-uuid>",
	Short: "Coordinator: move a task into a group by key (--group), or out of its group (--none)",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		vars, err := setGroupVariables(args[0], taskGroupKey, taskGroupNone)
		if err != nil {
			fail(err.Error())
		}
		vars["sessionUuid"] = taskSessionUuid
		runGql(rearm.AgentTaskSetGroupProgrammatic_Operation, vars, "agentTaskSetGroupProgrammatic")
	},
}

var agentTaskTagCmd = &cobra.Command{
	Use:   "tag <task-uuid>",
	Short: "Coordinator: add and remove a task's tags (--add k, --remove k; repeatable), or --clear them",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		var tags []map[string]interface{}
		if taskTagsClear {
			if len(taskTagAdd) > 0 || len(taskTagRemove) > 0 {
				fail("--clear removes every tag; give it alone")
			}
			tags = []map[string]interface{}{}
		} else {
			if len(taskTagAdd) == 0 && len(taskTagRemove) == 0 {
				fail("give --add <key> or --remove <key> (repeatable), or --clear")
			}
			data, err := sendGraphQLRequest(rearm.AgentTaskProgrammatic_Operation, map[string]interface{}{"taskUuid": args[0]})
			if err != nil {
				printGqlError(err)
				os.Exit(1)
			}
			t, _ := data["agentTaskProgrammatic"].(map[string]interface{})
			current, _ := t["tags"].([]interface{})
			tags = tagsAfter(current, taskTagAdd, taskTagRemove)
		}
		runGql(rearm.AgentTaskSetTagsProgrammatic_Operation, map[string]interface{}{
			"taskUuid": args[0], "sessionUuid": taskSessionUuid, "tags": tags}, "agentTaskSetTagsProgrammatic")
	},
}

var agentBoardGroupCmd = &cobra.Command{
	Use:   "group",
	Short: "A board's task groups: set, list, close, reopen",
}

var agentBoardGroupSetCmd = &cobra.Command{
	Use:   "set <board>",
	Short: "Coordinator: create a group, or edit the one --key names (only what is given changes)",
	Long: `Creates a group of the board, or edits the one --key names. On an edit only the flags given
change. --depends-on names the groups it waits on, by key (repeat or comma-separate): a task in the
group is not offered while one of them has an open task. --default-level is the level its tasks
read when they set none. --status CLOSED stops new tasks joining it; OPEN reopens it.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		in, err := groupInput(cmd, groupSetKey)
		if err != nil {
			fail(err.Error())
		}
		runGql(rearm.AgentBoardGroupSetProgrammatic_Operation, map[string]interface{}{
			"boardUuid": boardArg(args[0]), "sessionUuid": taskSessionUuid, "group": in}, "agentBoardGroupSetProgrammatic")
	},
}

var agentBoardGroupListCmd = &cobra.Command{
	Use:   "list <board>",
	Short: "The board's groups in order, with progress and what their tasks spent",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		data, err := sendGraphQLRequest(rearm.AgentBoardGroupsProgrammatic_Operation, map[string]interface{}{"boardUuid": boardArg(args[0])})
		if err != nil {
			printGqlError(err)
			os.Exit(1)
		}
		b, _ := data["agentBoardProgrammatic"].(map[string]interface{})
		emitJson(b["groups"])
	},
}

func agentGroupStatusCmd(use, status, short string) *cobra.Command {
	return &cobra.Command{
		Use:   use + " <board> <group-key>",
		Short: short,
		Args:  cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			runGql(rearm.AgentBoardGroupSetProgrammatic_Operation, map[string]interface{}{
				"boardUuid": boardArg(args[0]), "sessionUuid": taskSessionUuid, "group": statusGroupInput(args[1], status)},
				"agentBoardGroupSetProgrammatic")
		},
	}
}

var agentBoardGroupCloseCmd = agentGroupStatusCmd("close", "CLOSED", "Coordinator: close a group: no new tasks join it; nothing else changes")
var agentBoardGroupReopenCmd = agentGroupStatusCmd("reopen", "OPEN", "Coordinator: reopen a closed group")

// ---------- the people's verbs (rearm boards) ----------

var boardsSetGroupCmd = &cobra.Command{
	Use:   "setgroup <task-uuid>",
	Short: "Move a task into a group by key (--group), or out of its group (--none)",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		vars, err := setGroupVariables(args[0], taskGroupKey, taskGroupNone)
		if err != nil {
			fail(err.Error())
		}
		runGql(rearm.AgentTaskSetGroup_Operation, vars, "agentTaskSetGroup")
	},
}

var boardsTagCmd = &cobra.Command{
	Use:   "tag <task-uuid>",
	Short: "Set a task's tags to exactly the --tag keys given (repeatable), or --clear them",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		tags := tagInputs(taskTagKeys)
		if taskTagsClear == (len(tags) > 0) {
			fail("give --tag <key> (repeatable) for the task's whole list, or --clear")
		}
		if tags == nil {
			tags = []map[string]interface{}{}
		}
		runGql(rearm.AgentTaskSetTags_Operation, map[string]interface{}{"taskUuid": args[0], "tags": tags}, "agentTaskSetTags")
	},
}

var boardsGroupCmd = &cobra.Command{
	Use:   "group",
	Short: "A board's task groups: set, close, reopen, delete",
}

var boardsGroupSetCmd = &cobra.Command{
	Use:   "set <board>",
	Short: "Create a group, or edit the one --key names (board configuration)",
	Long:  agentBoardGroupSetCmd.Long,
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		in, err := groupInput(cmd, groupSetKey)
		if err != nil {
			fail(err.Error())
		}
		runGql(rearm.AgentBoardGroupSet_Operation, map[string]interface{}{"boardUuid": personBoardArg(args[0]), "group": in}, "agentBoardGroupSet")
	},
}

func boardsGroupStatusCmd(use, status, short string) *cobra.Command {
	return &cobra.Command{
		Use:   use + " <board> <group-key>",
		Short: short,
		Args:  cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			runGql(rearm.AgentBoardGroupSet_Operation, map[string]interface{}{
				"boardUuid": personBoardArg(args[0]), "group": statusGroupInput(args[1], status)}, "agentBoardGroupSet")
		},
	}
}

var boardsGroupCloseCmd = boardsGroupStatusCmd("close", "CLOSED", "Close a group: no new tasks join it; nothing else changes")
var boardsGroupReopenCmd = boardsGroupStatusCmd("reopen", "OPEN", "Reopen a closed group")

var boardsGroupDeleteCmd = &cobra.Command{
	Use:   "delete <board> <group-key>",
	Short: "Delete an empty group; refused while a task is in it",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		runGql(rearm.AgentBoardGroupDelete_Operation, map[string]interface{}{"boardUuid": personBoardArg(args[0]), "key": args[1]},
			"agentBoardGroupDelete")
	},
}

func addGroupSetFlags(c *cobra.Command) {
	c.Flags().StringVar(&groupSetKey, "key", "", "the group's key: 2 to 24 lower-case letters, digits and hyphens — required")
	c.Flags().StringVar(&groupSetName, "name", "", "one-line name")
	c.Flags().StringVar(&groupSetDesc, "description", "", "what the batch is")
	c.Flags().IntVar(&groupSetOrder, "order", 0, "display order among the board's groups (new groups append)")
	c.Flags().StringSliceVar(&groupSetDeps, "depends-on", nil, "keys of the groups this one waits on (repeat or comma-separate)")
	c.Flags().BoolVar(&groupSetNoDeps, "no-depends-on", false, "clear the groups it waits on")
	c.Flags().IntVar(&groupSetLevel, "default-level", 0, "the level its tasks read when they set none, 0 to 9")
	c.Flags().BoolVar(&groupSetNoLvl, "no-default-level", false, "clear the default level")
	c.Flags().StringVar(&groupSetStatus, "status", "", "OPEN or CLOSED")
}

func init() {
	for _, c := range []*cobra.Command{agentTaskRegisterCmd, boardsRegisterCmd} {
		c.Flags().StringVar(&taskGroupKey, "group", "", "register into this group of the board, by key")
		c.Flags().StringSliceVar(&taskTagKeys, "tag", nil, "a free label (repeat or comma-separate)")
	}
	for _, c := range []*cobra.Command{agentTaskListCmd, boardsTasksCmd} {
		c.Flags().StringVar(&taskListGroup, "group", "", "only the tasks in this group, by key")
		c.Flags().StringSliceVar(&taskListTags, "tag", nil, "only the tasks carrying any of these tags")
	}
	for _, c := range []*cobra.Command{agentTaskSetGroupCmd, boardsSetGroupCmd} {
		c.Flags().StringVar(&taskGroupKey, "group", "", "the group to move the task into, by key")
		c.Flags().BoolVar(&taskGroupNone, "none", false, "take the task out of its group")
	}
	agentTaskTagCmd.Flags().StringSliceVar(&taskTagAdd, "add", nil, "tags to add (repeatable)")
	agentTaskTagCmd.Flags().StringSliceVar(&taskTagRemove, "remove", nil, "tags to remove (repeatable)")
	agentTaskTagCmd.Flags().BoolVar(&taskTagsClear, "clear", false, "remove every tag")
	boardsTagCmd.Flags().StringSliceVar(&taskTagKeys, "tag", nil, "the task's tags, the whole list (repeatable)")
	boardsTagCmd.Flags().BoolVar(&taskTagsClear, "clear", false, "remove every tag")
	for _, c := range []*cobra.Command{agentTaskSetGroupCmd, agentTaskTagCmd, agentBoardGroupSetCmd,
		agentBoardGroupCloseCmd, agentBoardGroupReopenCmd} {
		c.Flags().StringVar(&taskSessionUuid, "session", "", "Coordinator seat session uuid — required")
		_ = c.MarkFlagRequired("session")
	}
	addGroupSetFlags(agentBoardGroupSetCmd)
	addGroupSetFlags(boardsGroupSetCmd)

	agentTaskCmd.AddCommand(agentTaskSetGroupCmd, agentTaskTagCmd)
	agentBoardGroupCmd.AddCommand(agentBoardGroupSetCmd, agentBoardGroupListCmd, agentBoardGroupCloseCmd, agentBoardGroupReopenCmd)
	agentBoardCmd.AddCommand(agentBoardGroupCmd)
	boardsGroupCmd.AddCommand(boardsGroupSetCmd, boardsGroupCloseCmd, boardsGroupReopenCmd, boardsGroupDeleteCmd)
	boardsCmd.AddCommand(boardsSetGroupCmd, boardsTagCmd, boardsGroupCmd)
}
