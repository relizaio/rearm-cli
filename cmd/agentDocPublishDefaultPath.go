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

	rearm "github.com/relizaio/rearm-client-go"
)

// The default file of a publish without --file in the version case (task RD5-6). The board's documentPath fills
// {round} with the next round, which is right for a new round and wrong when this hop republishes the round it
// already published (task RD4-7 makes that a new version of the round): the next round's file does not exist and
// the publish stops. The CLI knows the one fact the server's path resolver does not, what this hop published, so
// it decides here, with the brief's next-round computation (briefNextRounds, task RD5-5), and the brief's line and
// the publish's default cannot disagree.

// hopVersionRound is the round a publish of spec on --task would be a new version of: the newest round of the type
// on the task, when this session published it in this hop and it is not advisory. ok is false otherwise, and then
// the board's documentPath names the file. The task is read only when the hop has published something on it:
// with nothing published there is no version case.
func hopVersionRound(board map[string]interface{}, spec string) (briefNextRound, bool, error) {
	if docTask == "" {
		return briefNextRound{}, false, nil
	}
	published := hopOutputsFor(docSession, docTask)
	if len(published) == 0 {
		return briefNextRound{}, false, nil
	}
	data, err := sendGraphQLRequest(rearm.AgentTaskProgrammatic_Operation, map[string]interface{}{"taskUuid": docTask})
	if err != nil {
		return briefNextRound{}, false, fmt.Errorf("could not read task %s for its rounds of %s: %w; pass --file", docTask, spec, err)
	}
	task, _ := data["agentTaskProgrammatic"].(map[string]interface{})
	if task == nil {
		return briefNextRound{}, false, fmt.Errorf("task %s not found; pass --file", docTask)
	}
	templates, _ := board["documentPaths"].(map[string]interface{})
	next := briefNextRounds([]string{spec}, templates, str(board["documentsRoot"]), str(task["key"]), docTask,
		asList(task["documents"]), published)
	if len(next) == 1 && next[0].Version {
		return next[0], true, nil
	}
	return briefNextRound{}, false, nil
}

// versionPathLine is what the publish says when it took the hop's own path: which file, and why.
func versionPathLine(n briefNextRound) string {
	return fmt.Sprintf("republishing %s as a new version of round %d, published in this hop", n.Path, n.Round)
}

// publishNoteOut is where a publish's own lines go without --json: stdout beside the compact line, stderr under
// --dry-run, whose stdout is the input a script parses. Under --json sayPublishNote keeps them as notices.
func publishNoteOut() io.Writer {
	if docDryRun {
		return os.Stderr
	}
	return os.Stdout
}
