package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/relizaio/rearm-client-go"
)

// The split request (task SPLIT-12, parent SPLIT-9 D4, D10, D11): a hop returns the task with --reason SPLIT and
// may propose children; the coordinator splits it with task split --from-request or declines it with
// task authorize --note.

var (
	taskReturnChildrenJson string
	taskSplitFromRequest   bool
)

// errSplitNeedsChildren: task split was given neither list.
var errSplitNeedsChildren = errors.New("task split needs --children-json or --from-request")

// parseChildrenJson reads a --children-json value: the CLI checks only that it is a JSON array of objects, the
// server validates the children.
func parseChildrenJson(raw string) ([]map[string]interface{}, error) {
	var children []map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &children); err != nil {
		return nil, err
	}
	return children, nil
}

// addProposedChildren puts return's --children-json into its variables, only when the flag was given: the server
// refuses a proposal, an empty one included, with any reason but SPLIT.
func addProposedChildren(vars map[string]interface{}, raw string, given bool) error {
	if !given {
		return nil
	}
	children, err := parseChildrenJson(raw)
	if err != nil {
		return err
	}
	vars["proposedChildren"] = children
	return nil
}

// addAuthorizeNote puts authorize's --note into its variables when one was given: the coordinator's reason for
// declining a split request, recorded on the AUTHORIZE row.
func addAuthorizeNote(vars map[string]interface{}, note string) {
	if note != "" {
		vars["note"] = note
	}
}

// splitChildren picks the list task split sends: --children-json when given (it wins), else the children the
// task's split request proposes, read with readTask. Neither flag is refused before any request.
func splitChildren(taskUuid, raw string, jsonGiven, fromRequest bool,
	readTask func(string) (map[string]interface{}, error)) ([]map[string]interface{}, error) {
	if jsonGiven {
		return parseChildrenJson(raw)
	}
	if !fromRequest {
		return nil, errSplitNeedsChildren
	}
	task, err := readTask(taskUuid)
	if err != nil {
		return nil, err
	}
	return proposalChildren(task, taskUuid)
}

// readTaskForSplit is --from-request's one read, the brief's: the key is accepted as the uuid is.
func readTaskForSplit(taskUuid string) (map[string]interface{}, error) {
	data, err := sendGraphQLRequest(rearm.AgentTaskProgrammatic_Operation, map[string]interface{}{"taskUuid": taskUuid})
	if err != nil {
		return nil, err
	}
	task, _ := data["agentTaskProgrammatic"].(map[string]interface{})
	if task == nil {
		return nil, fmt.Errorf("no task %s", taskUuid)
	}
	return task, nil
}

// proposalChildren maps the proposed children of a REQUESTED split request to the split's input (D10): the fields
// copied when not null, the sibling indexes as read, a null group or tags left out so the child inherits the
// parent's, each tag as {key, value}; designFromParent only when the proposal set it (task TPF-163), so an
// unflagged child is sent without it. ref names the task in the refusal when the read carries no key.
func proposalChildren(task map[string]interface{}, ref string) ([]map[string]interface{}, error) {
	r, _ := task["splitRequest"].(map[string]interface{})
	var proposed []map[string]interface{}
	if r != nil && str(r["state"]) == "REQUESTED" {
		proposed = asList(r["proposedChildren"])
	}
	if len(proposed) == 0 {
		return nil, fmt.Errorf("Task %s has no split request with proposed children; pass --children-json",
			orElse(str(task["key"]), ref))
	}
	children := make([]map[string]interface{}, 0, len(proposed))
	for _, p := range proposed {
		child := map[string]interface{}{}
		for _, field := range []string{"title", "description", "externalRef", "sourceUrl", "producesComponent",
			"workLevel", "group", "dependsOnSiblingIndexes", "designFromParent"} {
			if v, ok := p[field]; ok && v != nil {
				child[field] = v
			}
		}
		if tags, ok := p["tags"].([]interface{}); ok {
			sent := make([]map[string]interface{}, 0, len(tags))
			for _, tag := range asList(tags) {
				sent = append(sent, map[string]interface{}{"key": tag["key"], "value": tag["value"]})
			}
			child["tags"] = sent
		}
		children = append(children, child)
	}
	return children, nil
}

// proposedTitle is a proposed child's title as the split request line prints it: a child flagged to build from
// the parent's reviewed design is marked, so the seat sees what --from-request would carry (task TPF-163).
func proposedTitle(c map[string]interface{}) string {
	if c["designFromParent"] == true {
		return str(c["title"]) + " [design from parent]"
	}
	return str(c["title"])
}

// splitRequestWaitLine is the compact line for a task AWAITING_COORDINATOR on a REQUESTED split request (D11);
// "" otherwise. The caller prints it only in that status.
func splitRequestWaitLine(t map[string]interface{}) string {
	r, _ := t["splitRequest"].(map[string]interface{})
	if r == nil || str(r["state"]) != "REQUESTED" {
		return ""
	}
	n := 0
	if l, ok := r["proposedChildren"].([]interface{}); ok {
		n = len(l)
	}
	return fmt.Sprintf("waits for the coordinator: split requested by %s (%d children proposed)", str(r["role"]), n)
}

// splitRequestLine is the brief's line for a task's split request (D11): its state, never "waits", since the
// record outlives the wait. "" when the task never had one. A blind read carries no description and no children.
func splitRequestLine(r map[string]interface{}) string {
	if r == nil {
		return ""
	}
	head := "split request by " + str(r["role"])
	switch str(r["state"]) {
	case "REQUESTED":
		line := head + " " + str(r["requestedAt"])
		if r["description"] == nil {
			return line
		}
		line += ": " + str(r["description"])
		var titles []string
		for _, c := range asList(r["proposedChildren"]) {
			titles = append(titles, proposedTitle(c))
		}
		if len(titles) == 0 {
			return line + "; proposes no children"
		}
		return line + "; proposes: " + strings.Join(titles, "; ")
	case "DECLINED":
		return head + ": " + settledLine("declined", r)
	case "SPLIT":
		return head + ": " + settledLine("split", r)
	case "LAPSED":
		// The lapse names the verb, not who ran it.
		return head + ": " + settledLine("lapsed", map[string]interface{}{"settledAt": r["settledAt"], "note": r["note"]})
	}
	return head + ": " + strings.ToLower(str(r["state"]))
}
