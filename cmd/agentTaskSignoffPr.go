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

	rearm "github.com/relizaio/rearm-client-go"
)

// task signoff --pr (task RD5-5): each PR is linked to the task before the sign-off is sent, in one call, so a
// code round can no longer be handed over before its PR is linked. The first refusal stops the command before
// any sign-off. A url the task already links is not an error: the server's link is idempotent.

// taskSignoffPrs is --pr, repeatable, in the order given.
var taskSignoffPrs []string

// linkSignOffPrs links each url to the task, in order, and returns the ones it linked. On a refusal it stops:
// the urls after it are not tried, and the error names the refused url, those linked before it, and that no
// sign-off was sent.
func linkSignOffPrs(taskUuid string, urls []string) ([]string, error) {
	linked := []string{}
	for _, u := range urls {
		if strings.TrimSpace(u) == "" {
			return linked, fmt.Errorf("--pr is empty; nothing was signed off")
		}
		if _, err := sendGraphQLRequest(rearm.AgentTaskLinkPrProgrammatic_Operation,
			map[string]interface{}{"taskUuid": taskUuid, "prUrl": u}); err != nil {
			before := "none"
			if len(linked) > 0 {
				before = strings.Join(linked, ", ")
			}
			return linked, fmt.Errorf("linking %s was refused, so the sign-off was not sent (linked before it: %s): %s",
				u, before, describeError(err))
		}
		linked = append(linked, u)
	}
	return linked, nil
}

// printSignOff prints the sign-off's response; with --pr the compact lines end with the linked PRs, and --json
// carries them as linkedPrs beside the response.
func printSignOff(v interface{}, linked []string) {
	if len(linked) == 0 {
		printCompact(v)
		return
	}
	if compactJson {
		out := map[string]interface{}{}
		if m, ok := v.(map[string]interface{}); ok {
			for k, val := range m {
				out[k] = val
			}
		}
		out["linkedPrs"] = linked
		emitJson(out)
		return
	}
	fmt.Println(compactText(v) + "\nlinked PRs: " + strings.Join(linked, ", "))
}

func init() {
	agentTaskSignoffCmd.PersistentFlags().StringArrayVar(&taskSignoffPrs, "pr", nil,
		"Pull request URL to link to the task before the sign-off is sent; repeatable, linked in order."+
			" The first refused link stops the command before any sign-off")
}
