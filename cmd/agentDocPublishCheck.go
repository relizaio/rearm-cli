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
	"path/filepath"
)

// `rearm agent doc publish --check` (task RD4-6): the element index a publish would send, checked by
// the board in the task's current scope, with nothing created. Run it before publishing: a failure it
// prints would otherwise land as a DRAFT round with a failed report.

// checkPreviewQuery asks the board for the report. A CLI-local operation until the client-go pin moves
// past RD4-6, which adds it to the shared operations.
const checkPreviewQuery = `query AgentElementCheckPreviewProgrammatic($sessionUuid: ID!, $taskUuid: ID!,
	$specification: SpecificationType!, $elements: String!, $elementsDigest: String) {
	agentElementCheckPreviewProgrammatic(sessionUuid: $sessionUuid, taskUuid: $taskUuid, specification: $specification,
		elements: $elements, elementsDigest: $elementsDigest) {
		catalogueVersion grammarVersion
		results { check result blocking reason offences { elementId release message } }
	} }`

// previewChecks sends the preview; a variable so tests can stand in for the server.
var previewChecks = func(vars map[string]interface{}) (map[string]interface{}, error) {
	data, err := sendGraphQLRequest(checkPreviewQuery, vars)
	if err != nil {
		return nil, err
	}
	report, _ := data["agentElementCheckPreviewProgrammatic"].(map[string]interface{})
	return report, nil
}

// previewSummary renders a preview report the way doc publish renders a published one, and whether a
// check the board blocks on failed.
func previewSummary(report map[string]interface{}) ([]string, bool) {
	lines := elementCheckSummary(map[string]interface{}{"document": map[string]interface{}{"elementChecks": report}})
	blocking := false
	results, _ := report["results"].([]interface{})
	for _, r := range results {
		c, _ := r.(map[string]interface{})
		if b, _ := c["blocking"].(bool); b && c["result"] == "FAIL" {
			blocking = true
		}
	}
	if blocking && len(lines) > 1 {
		lines[len(lines)-1] = "a publish now would be refused at sign-off: fix the document, commit, and check again"
	}
	return append([]string{"check only: nothing was published"}, lines...), blocking
}

// runPublishCheck parses the committed file as the publish would, asks the board what its checks make
// of it, prints the report, and exits 1 on a blocking failure.
func runPublishCheck(st *agentSessionState, repoPath, file, spec string, board map[string]interface{}) error {
	source, err := os.ReadFile(filepath.Join(repoPath, file))
	if err != nil {
		return fmt.Errorf("could not read %s: %w", file, err)
	}
	extra, ix, err := elementsInput(spec, source, board)
	if err != nil {
		return err
	}
	if extra == nil {
		sayPublishNote(os.Stderr, "check only: "+file+" has no element ids to check ("+summarise(ix)+"); a publish sends no element index")
		if compactJson {
			emitJson(checkOnlyObject(nil))
		}
		return nil
	}
	sayPublishNote(os.Stderr, "elements: "+summarise(ix))
	for _, line := range definitionsAndReferences(ix) {
		sayPublishNote(os.Stderr, line)
	}
	report, err := previewChecks(map[string]interface{}{
		"sessionUuid":    sessionUuidOf(st, docSession),
		"taskUuid":       docTask,
		"specification":  spec,
		"elements":       extra["elements"],
		"elementsDigest": extra["elementsDigest"],
	})
	if err != nil {
		printRefusal(err)
		os.Exit(1)
	}
	lines, blocking := previewSummary(report)
	// --json: one object, {check: true, checks}, and nothing on stderr (task RD5-8); the exit code still says
	// whether a check the board blocks on failed.
	if compactJson {
		emitJson(checkOnlyObject(checksObject(report, lines)))
	} else {
		for _, line := range lines {
			fmt.Fprintln(os.Stderr, line)
		}
	}
	if blocking {
		os.Exit(1)
	}
	return nil
}

// checkOnlyObject is what doc publish --check --json prints: nothing was published, here is what the checks found
// (null for a document without element ids), and what the check said beside it.
func checkOnlyObject(checks map[string]interface{}) map[string]interface{} {
	return withNotices(map[string]interface{}{"check": true, "checks": checks})
}
