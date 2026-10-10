package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"regexp"

	rearm "github.com/relizaio/rearm-client-go"
	"github.com/spf13/cobra"
)

// Task keys (task 3d1f9dd7): a task is named RD-42 on every human surface, so every command that
// takes a task uuid takes its key too. The key is resolved to the uuid through one read before the
// call; the server's task mutations keep their uuid arguments.

var taskUUIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// taskKeyLookup resolves a key to a task uuid, or "" when no task has it. A variable so tests can
// stand in for the server.
var taskKeyLookup = lookupTaskKey

// resolveTaskRef returns the uuid a task argument names: itself when it is a uuid, else the task
// with that key.
func resolveTaskRef(ref string) (string, error) {
	if ref == "" || taskUUIDPattern.MatchString(ref) {
		return ref, nil
	}
	uuid, err := taskKeyLookup(ref)
	if err != nil {
		return "", err
	}
	if uuid == "" {
		return "", fmt.Errorf("no task has the key %s", ref)
	}
	return uuid, nil
}

// lookupTaskKey reads the task by key: the agent read first, then, for a login whose key carries no
// AGENT function, a person's read in the login's organization.
func lookupTaskKey(key string) (string, error) {
	data, err := sendGraphQLRequest(rearm.AgentTaskByKeyProgrammatic_Operation, map[string]interface{}{"key": key})
	if err == nil {
		return uuidOf(data, "agentTaskByKeyProgrammatic"), nil
	}
	org, orgErr := boardsOrg()
	if orgErr != nil {
		return "", fmt.Errorf("resolving task key %s: %w", key, err)
	}
	data, personErr := sendGraphQLRequest(rearm.AgentTaskByKey_Operation,
		map[string]interface{}{"orgUuid": org, "key": key})
	if personErr != nil {
		return "", fmt.Errorf("resolving task key %s: %w", key, personErr)
	}
	return uuidOf(data, "agentTaskByKey"), nil
}

func uuidOf(data map[string]interface{}, field string) string {
	task, _ := data[field].(map[string]interface{})
	uuid, _ := task["uuid"].(string)
	return uuid
}

// Which positional arguments of a command name tasks.
const (
	noTaskArgs   = 0
	firstTaskArg = 1
	everyTaskArg = -1
)

// acceptTaskKeys lets a command's task arguments be keys: the positional ones it names and the given
// flags. Resolved in PreRunE, before the command's own run sees them.
func acceptTaskKeys(cmd *cobra.Command, positional int, flags []*string, sliceFlags []*[]string) {
	prevE, prev := cmd.PreRunE, cmd.PreRun
	cmd.PreRun = nil
	cmd.PreRunE = func(c *cobra.Command, args []string) error {
		n := positional
		if n == everyTaskArg || n > len(args) {
			n = len(args)
		}
		for i := 0; i < n; i++ {
			u, err := resolveTaskRef(args[i])
			if err != nil {
				return err
			}
			args[i] = u
		}
		for _, f := range flags {
			u, err := resolveTaskRef(*f)
			if err != nil {
				return err
			}
			*f = u
		}
		for _, f := range sliceFlags {
			for i, ref := range *f {
				u, err := resolveTaskRef(ref)
				if err != nil {
					return err
				}
				(*f)[i] = u
			}
		}
		if prevE != nil {
			return prevE(c, args)
		}
		if prev != nil {
			prev(c, args)
		}
		return nil
	}
}

func init() {
	for _, c := range []*cobra.Command{
		agentTaskAssignCmd, agentTaskSignoffCmd, agentTaskReturnCmd, agentTaskHoldCmd, agentTaskLiftholdCmd,
		agentTaskEscalateCmd, agentTaskRequireReviewCmd, agentTaskOrderCmd, agentTaskSplitCmd, agentTaskCompleteCmd,
		agentTaskCancelCmd, agentTaskReopenCmd, agentTaskBindrefCmd, agentTaskLinkprCmd, agentTaskMergeplanCmd,
		agentTaskDeclareDeliveryCmd, agentTaskSupersedeCmd, agentTaskUnlinkCmd, boardsDeclareDeliveryCmd, boardsOrderCmd, boardsCompleteCmd, boardsCancelCmd,
		boardsDecideCmd, boardsAnswerCmd, boardsReviewCmd, boardsSignoffCmd, boardsHoldCmd, boardsLiftholdCmd,
		boardsRequireReviewCmd, boardsBudgetCmd, boardsStrengthCmd,
	} {
		acceptTaskKeys(c, firstTaskArg, nil, nil)
	}
	acceptTaskKeys(agentTaskShowCmd, everyTaskArg, nil, nil)
	acceptTaskKeys(agentTaskAuthorizeCmd, firstTaskArg, nil, []*[]string{&taskDependsOn})
	acceptTaskKeys(boardsAuthorizeCmd, firstTaskArg, nil, []*[]string{&boardsDependsOn})
	acceptTaskKeys(boardsRegisterCmd, noTaskArgs, []*string{&boardsParent}, nil)
	acceptTaskKeys(agentDocPublishCmd, noTaskArgs, []*string{&docTask}, nil)
	acceptTaskKeys(agentDocElementCheckCmd, noTaskArgs, []*string{&checkTask}, nil)
	acceptTaskKeys(agentSessionUsageCmd, noTaskArgs, []*string{&usageTask}, nil)
}

// runGqlTasks is runGql for replies that are a task or a list of tasks: each task is printed with its
// key, title and description first (board-documents.md §3.6, §4.5), the rest in the usual sorted
// order. emitJson writes through a map, whose keys come out sorted, so the selection order alone does
// not lead with them.
func runGqlTasks(query string, variables map[string]interface{}, key string) {
	runGqlTasksRead(query, variables, key)
}

// runGqlTasksRead is runGqlTasks that also hands back what it printed.
func runGqlTasksRead(query string, variables map[string]interface{}, key string) interface{} {
	data, err := sendGraphQLRequest(query, variables)
	if err != nil {
		printRefusal(err)
		os.Exit(1)
	}
	fmt.Println(string(keyFirstJSON(data[key])))
	return data[key]
}

// taskLeadFields lead a task object, in this order, when it has them: what a reader looks for first.
var taskLeadFields = []string{"key", "title", "description"}

// keyFirstJSON is json.Marshal with the lead fields first in a task object, or in each object of a
// list. An object without a key is not a task and keeps the sorted order.
func keyFirstJSON(v interface{}) []byte {
	switch t := v.(type) {
	case []interface{}:
		parts := make([][]byte, len(t))
		for i, e := range t {
			parts[i] = keyFirstJSON(e)
		}
		return append(append([]byte("["), bytes.Join(parts, []byte(","))...), ']')
	case map[string]interface{}:
		if _, has := t["key"]; !has {
			out, _ := json.Marshal(t)
			return out
		}
		rest := make(map[string]interface{}, len(t))
		for name, val := range t {
			rest[name] = val
		}
		out := []byte("{")
		for _, name := range taskLeadFields {
			val, ok := rest[name]
			if !ok {
				continue
			}
			delete(rest, name)
			k, _ := json.Marshal(name)
			vb, _ := json.Marshal(val)
			if len(out) > 1 {
				out = append(out, ',')
			}
			out = append(append(append(out, k...), ':'), vb...)
		}
		body, _ := json.Marshal(rest)
		if len(body) > 2 {
			out = append(append(out, ','), body[1:]...)
			return out
		}
		return append(out, '}')
	default:
		out, _ := json.Marshal(v)
		return out
	}
}
