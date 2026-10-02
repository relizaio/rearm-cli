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
	"strings"
)

// The brief's next-round lines (task RD5-5): for each type the role produces on the task, the path of the
// round the hop is about to write, so no hop resolves {round} by hand. Computed from the reads the brief
// already makes, mirroring the server's rule; the server stays the authority at publish, so a wrong guess
// costs a refusal, not a wrong round.

// briefNextRound is one output type's next round: the path to write, its round, and whether a publish there
// is a new version of a round this hop already published (task RD4-7) rather than a new round.
type briefNextRound struct {
	Type    string `json:"type"`
	Path    string `json:"path"`
	Round   int    `json:"round"`
	Version bool   `json:"version"`
}

// The server's defaults (AgentBoardData.documentPathTemplate), used when the board sets no template for a
// type: the index types always, the well-known per-task names, then one file per round.
var (
	briefIndexTypePaths = map[string]string{
		"BOARD_REVIEW_ITEMS": "review-items/{key}/round-{round}.md",
		"BOARD_TEST_REPORT":  "tests/{key}/run-{round}.md",
		"BOARD_QUESTIONS":    "questions/{key}/round-{round}.md",
	}
	briefWellKnownTaskPaths = map[string]string{
		"ARCHITECTURE":               "design/{key}/architecture-{round}.md",
		"DETAILED_DESIGN":            "impl/{key}/notes-{round}.md",
		"BOARD_INVESTIGATION_REPORT": "investigations/{key}/report-{round}.md",
	}
)

const briefDefaultTaskPath = "design/{key}/{type}-{round}.md"

// briefTaskOutputs is the types a role produces on a task: those it declares at TASK scope, and the index
// types whatever scope it declares, since the server keeps those per task. In the role's order, once each.
func briefTaskOutputs(role map[string]interface{}) []string {
	var out []string
	for _, o := range asList(role["producesOutputs"]) {
		spec := str(o["specification"])
		_, index := briefIndexTypePaths[spec]
		if spec == "" || (str(o["scope"]) != "TASK" && !index) || stringListHas(out, spec) {
			continue
		}
		out = append(out, spec)
	}
	return out
}

// briefPathTemplate is the board's template for a task-scoped type, else the server's default.
func briefPathTemplate(templates map[string]interface{}, spec string) string {
	if t := str(templates[spec]); t != "" {
		// The server rewrites {task} to {key} wherever templates come in (board-documents.md D11).
		return strings.ReplaceAll(t, "{task}", "{key}")
	}
	if t, ok := briefIndexTypePaths[spec]; ok {
		return t
	}
	if t, ok := briefWellKnownTaskPaths[spec]; ok {
		return t
	}
	return briefDefaultTaskPath
}

// briefSettledRound mirrors the server's isSettledRound: a reservation mid-cut and a cancelled or rejected
// round are not rounds.
func briefSettledRound(lifecycle string) bool {
	return lifecycle != "PENDING" && lifecycle != "CANCELLED" && lifecycle != "REJECTED"
}

// briefNextRounds resolves each output type's next round. {round} is one more than the rounds of the type on
// the task, a replaced version counting as its round. When the newest round of the type is one this hop
// published (published lists the releases the session's doc publish recorded since the assignment), the line
// names that round's path instead: a republish there is a new version of it, not a new round (task RD4-7).
func briefNextRounds(outputs []string, templates map[string]interface{}, root, key, taskUuid string,
	docs []map[string]interface{}, published []string) []briefNextRound {
	out := []briefNextRound{}
	for _, spec := range outputs {
		rounds := map[int]bool{}
		var newest map[string]interface{}
		newestRound := 0
		for _, r := range docs {
			d, _ := r["document"].(map[string]interface{})
			if str(d["specification"]) != spec || !briefSettledRound(str(r["lifecycle"])) {
				continue
			}
			if t := str(d["task"]); t != "" && t != taskUuid {
				continue
			}
			n := intOf(d["round"])
			if n <= 0 {
				continue
			}
			rounds[n] = true
			// The server lists the newest version of a round first; that is the one a republish replaces.
			if n > newestRound {
				newest, newestRound = r, n
			}
		}
		if newest != nil && stringListHas(published, str(newest["uuid"])) {
			d, _ := newest["document"].(map[string]interface{})
			advisory, _ := d["advisory"].(bool)
			if p := str(d["path"]); p != "" && !advisory {
				out = append(out, briefNextRound{Type: spec, Path: p, Round: newestRound, Version: true})
				continue
			}
		}
		next := len(rounds) + 1
		path := strings.ReplaceAll(briefPathTemplate(templates, spec), "{type}", strings.ToLower(spec))
		path = strings.ReplaceAll(strings.ReplaceAll(path, "{key}", key), "{round}", fmt.Sprint(next))
		out = append(out, briefNextRound{Type: spec, Path: root + path, Round: next})
	}
	return out
}

// renderNextRound is the line the brief prints under its documents.
func renderNextRound(n briefNextRound) string {
	line := fmt.Sprintf("- next %s: %s", n.Type, n.Path)
	if n.Version {
		line += fmt.Sprintf(" (republish = new version of round %d)", n.Round)
	}
	return line
}
