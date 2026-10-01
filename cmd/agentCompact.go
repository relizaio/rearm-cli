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
	"strings"

	"github.com/spf13/cobra"
)

// Compact output for the agent's mutations (task RD3-9): the key, where the task stands, who holds it and
// the next thing to do, in one to three lines, instead of the whole task after every call. --json prints
// the full response as before. task next, task show, task list, board show and board snapshot keep their
// shapes: they are the reads scripts parse, and wait prints what next prints.
//
// One rule for --json (task RD4-9): a read's payload is its output whatever the flag, and a mutation's
// payload is compact unless the flag is set. So every read takes --json too, and prints the same JSON with
// it as without it, rather than refusing the flag: a script can pass --json to every verb.

// compactJson is the mutations' --json: the full response instead of the compact lines.
var compactJson bool

// readJson is the reads' --json: accepted and ignored, since a read prints its JSON payload either way.
var readJson bool

// runGqlCompact is runGql for a mutation: compact lines by default, the full response with --json.
func runGqlCompact(query string, variables map[string]interface{}, key string) {
	runGqlReadCompact(query, variables, key)
}

// runGqlReadCompact is runGqlCompact that also hands back the response, for a command that records part of it.
func runGqlReadCompact(query string, variables map[string]interface{}, key string) interface{} {
	data, err := sendGraphQLRequest(query, variables)
	if err != nil {
		printRefusal(err)
		os.Exit(1)
	}
	printCompact(data[key])
	return data[key]
}

// printRefusal is how every compact verb reports an error (task RD3-16): on stderr, so a caller reading the
// result from stdout never takes the refusal for one, with exit 1 after it. The server's message says why.
func printRefusal(err error) {
	fmt.Fprintln(os.Stderr, "Error:", describeError(err))
}

// printCompact prints a response as the compact lines, or whole with --json.
func printCompact(v interface{}) {
	if compactJson {
		emitJson(v)
		return
	}
	fmt.Println(compactText(v))
}

// compactText is a task, an assignment, or a list of tasks, in one to three lines each.
func compactText(v interface{}) string {
	switch x := v.(type) {
	case []interface{}:
		var lines []string
		for _, e := range x {
			lines = append(lines, compactText(e))
		}
		return strings.Join(lines, "\n")
	case map[string]interface{}:
		if t, ok := x["task"].(map[string]interface{}); ok {
			return compactTask(t, x)
		}
		if _, ok := x["status"]; ok {
			return compactTask(x, nil)
		}
	}
	// Nothing the printer knows: the response as it came, never nothing.
	return jsonLine(v)
}

// compactTask: "RD3-9 assigned, role coder" then what to do next. For an assignment, the prompt version too.
func compactTask(t map[string]interface{}, assignment map[string]interface{}) string {
	key := orElse(str(t["key"]), str(t["uuid"]))
	status := str(t["status"])
	role := str(t["role"])
	first := fmt.Sprintf("%s %s, role %s", key, words(status), orElse(role, "none"))
	if assignment != nil && str(assignment["promptVersion"]) != "" {
		first += ", prompt " + str(assignment["promptVersion"])
	}
	lines := []string{first}
	if inv := investigationLine(t); inv != "" {
		lines = append(lines, inv)
	}
	held, _ := t["assignment"].(map[string]interface{})
	switch status {
	case "QUEUED":
		lines = append(lines, "queued for "+orElse(role, "the next role"))
	case "ASSIGNED":
		session := str(held["session"])
		if session != "" {
			lines = append(lines, "held by session "+short(session)+"; read it with: rearm agent task brief "+key+" --session "+session)
		}
	case "AWAITING_COORDINATOR":
		lines = append(lines, "waits for the coordinator")
	case "ON_HOLD":
		if h, _ := t["hold"].(map[string]interface{}); h != nil {
			lines = append(lines, fmt.Sprintf("on hold (%s): %s", words(str(h["kind"])), str(h["reason"])))
		}
	case "PENDING_INTAKE":
		lines = append(lines, "in intake: the coordinator authorizes it")
	case "DELIVERING":
		lines = append(lines, "delivering: it completes when its PRs merge")
	}
	lines = append(lines, baseMovedLines(t)...)
	return strings.Join(lines, "\n")
}

// compactDocument: "published ARCHITECTURE v113 (assembled), advisory; release <uuid>".
func compactDocument(release map[string]interface{}, spec string, advisory bool) string {
	line := fmt.Sprintf("published %s v%s (%s)", orElse(spec, "a document"), str(release["version"]), words(str(release["lifecycle"])))
	if advisory {
		line += ", advisory"
	}
	return line + "; release " + str(release["uuid"])
}

func words(enum string) string {
	return strings.ToLower(strings.ReplaceAll(enum, "_", " "))
}

func short(uuid string) string {
	if len(uuid) > 8 {
		return uuid[:8]
	}
	return uuid
}

func jsonLine(v interface{}) string {
	out, _ := json.Marshal(v)
	return string(out)
}

// compactCommands are the mutations that print compactly; each takes --json for the full response.
func compactCommands() []*cobra.Command {
	return []*cobra.Command{
		agentTaskRegisterCmd, agentTaskAssignCmd, agentTaskSignoffCmd, agentTaskReturnCmd, agentTaskAuthorizeCmd,
		agentTaskHoldCmd, agentTaskLiftholdCmd, agentTaskEscalateCmd, agentTaskRequireReviewCmd, agentTaskOrderCmd,
		agentTaskWorkLevelCmd, agentTaskSplitCmd, agentTaskCompleteCmd, agentTaskCancelCmd, agentTaskReopenCmd,
		agentTaskBindrefCmd, agentTaskLinkprCmd, agentTaskSetGroupCmd, agentTaskTagCmd, agentDocPublishCmd,
		agentTaskSupersedeCmd, agentTaskWithdrawCmd, agentTaskCommissionCmd,
	}
}

// jsonReadCommands are the reads whose output is already JSON; each takes --json and prints the same.
func jsonReadCommands() []*cobra.Command {
	return []*cobra.Command{
		agentTaskShowCmd, agentTaskListCmd, agentTaskNextCmd, agentWaitCmd,
		agentBoardListCmd, agentBoardShowCmd, agentBoardSnapshotCmd, agentBoardRoleconfigListCmd, agentBoardGroupListCmd,
		agentSessionShowCmd, agentSessionInboxCmd, agentReleaseShowCmd, agentDocElementsCmd,
	}
}

func init() {
	for _, c := range compactCommands() {
		c.Flags().BoolVar(&compactJson, "json", false, "print the full response as JSON instead of the compact lines")
	}
	for _, c := range jsonReadCommands() {
		c.Flags().BoolVar(&readJson, "json", false, "accepted for scripts that pass --json to every verb: this read prints JSON with or without it")
	}
}
