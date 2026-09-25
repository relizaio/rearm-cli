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

// The element checks (elements.md §7). The board runs them when an element-bearing document is
// published and cuts the result as a CHECK_REPORT round on the task; a failure on a check the board
// blocks on refuses the sign-off that would hand the document over. `doc publish` prints the report
// it produced, and `doc check` re-runs the checks once the inputs have moved.

var (
	checkSession string
	checkRelease string
	checkTask    string
	checkJson    bool
)

// checkSummary renders a report for a terminal: one line of counts, then each failing check with
// its offences, and what a blocking failure means for the sign-off. An empty result -- no report --
// says so rather than printing nothing.
func checkSummary(release map[string]interface{}) []string {
	doc, _ := release["document"].(map[string]interface{})
	report, _ := doc["checks"].(map[string]interface{})
	if report == nil {
		return []string{"checks: no report for this document (component-scoped documents are not checked yet)"}
	}
	results, _ := report["results"].([]interface{})
	pass, fail, skip := 0, 0, 0
	var blocking []string
	var detail []string
	for _, r := range results {
		c, _ := r.(map[string]interface{})
		name, _ := c["check"].(string)
		switch c["result"] {
		case "PASS":
			pass++
		case "SKIP":
			skip++
		case "FAIL":
			fail++
			tag := ""
			if b, _ := c["blocking"].(bool); b {
				blocking = append(blocking, name)
				tag = " [blocking]"
			}
			detail = append(detail, "  FAIL "+name+tag)
			offences, _ := c["offences"].([]interface{})
			for _, o := range offences {
				om, _ := o.(map[string]interface{})
				msg, _ := om["message"].(string)
				detail = append(detail, "    "+msg)
			}
		}
	}
	version, _ := report["catalogueVersion"].(string)
	head := fmt.Sprintf("checks (%s): %d pass, %d fail, %d skip", version, pass, fail, skip)
	if round, ok := doc["round"].(float64); ok {
		head += fmt.Sprintf(" (report round %d)", int(round))
	}
	if len(blocking) > 0 {
		head += " — blocking: " + strings.Join(blocking, ", ")
	}
	out := append([]string{head}, detail...)
	if len(blocking) > 0 {
		out = append(out, "sign-off will be refused until this passes: fix and republish, or run `rearm agent doc check` once the inputs change")
	}
	return out
}

// printCheckReport reads the report the board cut for a document just published, and prints it on
// stderr so the publish's JSON on stdout stays what scripts parse.
func printCheckReport(releaseUuid string) {
	data, err := sendGraphQLRequest(rearm.AgentCheckReportProgrammatic_Operation,
		map[string]interface{}{"releaseUuid": releaseUuid})
	if err != nil {
		fmt.Fprintln(os.Stderr, "checks: could not read the report: "+describeError(err))
		return
	}
	release, _ := data["agentCheckReportProgrammatic"].(map[string]interface{})
	for _, line := range checkSummary(release) {
		fmt.Fprintln(os.Stderr, line)
	}
}

// checkTargets picks, from a task's documents (newest first), the newest release of each
// specification that carries an element index: what the checks are about.
func checkTargets(task map[string]interface{}) []map[string]interface{} {
	docs, _ := task["documents"].([]interface{})
	taskUuid, _ := task["uuid"].(string)
	seen := map[string]bool{}
	var out []map[string]interface{}
	for _, d := range docs {
		rd, _ := d.(map[string]interface{})
		doc, _ := rd["document"].(map[string]interface{})
		spec, _ := doc["specification"].(string)
		if spec == "" || seen[spec] {
			continue
		}
		seen[spec] = true
		if owner, _ := doc["task"].(string); owner != taskUuid || doc["elements"] == nil {
			continue
		}
		out = append(out, rd)
	}
	return out
}

func runDocCheck() error {
	if checkSession == "" {
		return fmt.Errorf("--session is required")
	}
	if (checkRelease == "") == (checkTask == "") {
		return fmt.Errorf("give --release <uuid> or --task <uuid>")
	}
	st := lookupAgentState(checkSession)
	session := sessionUuidOf(st, checkSession)
	type target struct{ release, label string }
	var targets []target
	if checkRelease != "" {
		targets = append(targets, target{checkRelease, checkRelease})
	} else {
		data, err := sendGraphQLRequest(rearm.AgentTaskCheckTargetsProgrammatic_Operation,
			map[string]interface{}{"taskUuid": checkTask})
		if err != nil {
			return fmt.Errorf("could not read the task's documents: %s", describeError(err))
		}
		task, _ := data["agentTaskProgrammatic"].(map[string]interface{})
		for _, rd := range checkTargets(task) {
			doc, _ := rd["document"].(map[string]interface{})
			uuid, _ := rd["uuid"].(string)
			label := fmt.Sprintf("%v", doc["specification"])
			if round, ok := doc["round"].(float64); ok {
				label += fmt.Sprintf(" round %d", int(round))
			}
			targets = append(targets, target{uuid, label + " (" + uuid + ")"})
		}
		if len(targets) == 0 {
			return fmt.Errorf("task %s has no document with elements to check", checkTask)
		}
	}
	var results []interface{}
	for _, t := range targets {
		data, err := sendGraphQLRequest(rearm.AgentCheckRunProgrammatic_Operation,
			map[string]interface{}{"sessionUuid": session, "releaseUuid": t.release})
		if err != nil {
			return fmt.Errorf("checking %s: %s", t.label, describeError(err))
		}
		release, _ := data["agentCheckRunProgrammatic"].(map[string]interface{})
		if checkJson {
			results = append(results, release)
			continue
		}
		if len(targets) > 1 {
			fmt.Println(t.label + ":")
		}
		for _, line := range checkSummary(release) {
			fmt.Println(line)
		}
	}
	if checkJson {
		if checkRelease != "" {
			emitJson(results[0])
		} else {
			emitJson(results)
		}
	}
	return nil
}

var agentDocCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Re-run the element checks of a document (or of every document of a task) in its current scope",
	Long: `Runs the board's element checks over a document again, against the releases it can
see now -- its task's latest documents and the inputs bound to the current assignment -- and
prints what they found (elements.md §7).

The board already ran them when the document was published. Re-run when an input has moved:
an upstream round landed, a draft was baselined. A run whose result is the newest report's
returns that report; otherwise the board cuts a new CHECK_REPORT round.

A failure on a check the board blocks on refuses the sign-off that hands the document over.

  --release <uuid>   one document
  --task <uuid>      the newest release of each of the task's documents that has elements
  --json             the report releases as the server returned them`,
	Run: func(cmd *cobra.Command, args []string) {
		if err := runDocCheck(); err != nil {
			fmt.Fprintf(os.Stderr, "rearm: %v\n", err)
			os.Exit(1)
		}
	},
}

func init() {
	f := agentDocCheckCmd.Flags()
	f.StringVar(&checkSession, "session", "", "session working the task — required")
	f.StringVar(&checkRelease, "release", "", "the document release to check")
	f.StringVar(&checkTask, "task", "", "check every element-bearing document of this task")
	f.BoolVar(&checkJson, "json", false, "print the report releases as JSON")
	agentDocCmd.AddCommand(agentDocCheckCmd)
}
