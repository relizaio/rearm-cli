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
	"io"
	"os"
)

// One JSON value per command with --json (task RD5-8). A script captures both streams (2>&1) to keep a refusal's
// text, so the element-check report a publish used to print on stderr after its JSON made the capture unparseable.
// With --json the report is folded into the object as checks, and the publish's own lines (which file it took, the
// element count) as notices; nothing goes to stderr on success. Refusals and local errors still go to stderr with
// exit 1 and an empty stdout. Without --json nothing changes.

// publishNotices are the lines a publish says beside its result, kept for the JSON object when --json is set.
var publishNotices []string

// sayPublishNote prints a publish's own line on w, or keeps it for the object under --json.
func sayPublishNote(w io.Writer, line string) {
	if compactJson {
		publishNotices = append(publishNotices, line)
		return
	}
	fmt.Fprintln(w, line)
}

// flushPublishNotices prints the kept lines on stderr: a dry run prints its input, not the object, so the lines go
// where they went before.
func flushPublishNotices() {
	for _, line := range publishNotices {
		fmt.Fprintln(os.Stderr, line)
	}
	publishNotices = nil
}

// withNotices adds the kept lines to an object, when there are any.
func withNotices(out map[string]interface{}) map[string]interface{} {
	if len(publishNotices) > 0 {
		out["notices"] = append([]string(nil), publishNotices...)
	}
	return out
}

// elementCheckReportOf is the report on a release the board returned: document.elementChecks, nil when none.
func elementCheckReportOf(release map[string]interface{}) map[string]interface{} {
	doc, _ := release["document"].(map[string]interface{})
	report, _ := doc["elementChecks"].(map[string]interface{})
	return report
}

// elementCheckVerdict is the task page's rule (elementCheckVerdictOf in the UI): a failure the board blocks on is
// FAIL, any other failure WARN, else PASS.
func elementCheckVerdict(fail, blockingFailed int) string {
	if blockingFailed > 0 {
		return "FAIL"
	}
	if fail > 0 {
		return "WARN"
	}
	return "PASS"
}

// checksObject is the report as the JSON object carries it: the verdict, the counts, each offence of a failed
// check the board blocks on, and lines, the report as the terminal prints it. nil (JSON null) when there is no
// report, as for a document without elements.
func checksObject(report map[string]interface{}, lines []string) map[string]interface{} {
	if report == nil {
		return nil
	}
	results, _ := report["results"].([]interface{})
	pass, fail, skip, blockingFailed := 0, 0, 0, 0
	blocking := []interface{}{}
	for _, r := range results {
		c, _ := r.(map[string]interface{})
		switch c["result"] {
		case "PASS":
			pass++
		case "SKIP":
			skip++
		case "FAIL":
			fail++
			if b, _ := c["blocking"].(bool); !b {
				continue
			}
			blockingFailed++
			offences, _ := c["offences"].([]interface{})
			if len(offences) == 0 {
				blocking = append(blocking, map[string]interface{}{"check": c["check"], "offence": nil})
			}
			for _, o := range offences {
				om, _ := o.(map[string]interface{})
				blocking = append(blocking, map[string]interface{}{"check": c["check"], "offence": om["message"]})
			}
		}
	}
	if lines == nil {
		lines = []string{}
	}
	return map[string]interface{}{
		"verdict":  elementCheckVerdict(fail, blockingFailed),
		"counts":   map[string]interface{}{"pass": pass, "fail": fail, "skip": skip},
		"blocking": blocking,
		"lines":    lines,
	}
}

// releaseChecks is checksObject for a published release, with the lines doc publish prints for it.
func releaseChecks(release map[string]interface{}) map[string]interface{} {
	return checksObject(elementCheckReportOf(release), elementCheckSummary(release))
}

// publishObject is what doc publish --json prints: the release as the server returned it, plus checks and any
// notices.
func publishObject(release map[string]interface{}, checks interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	for k, v := range release {
		out[k] = v
	}
	out["checks"] = checks
	return withNotices(out)
}
