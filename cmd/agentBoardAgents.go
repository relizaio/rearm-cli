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
	"strconv"
	"strings"
	"time"

	rearm "github.com/relizaio/rearm-client-go"
	"github.com/spf13/cobra"
)

var (
	boardAgentsWindow string
	boardAgentsJson   bool
)

// agentsWindow turns --window into the read's from and to (task RD3-5): 24h, 7d, 30d or 90d back from now, as the
// Agents tab's selector offers, any Nh or Nd, or "all" for the board's life (no bounds).
func agentsWindow(window string, now time.Time) (map[string]interface{}, error) {
	w := strings.ToLower(strings.TrimSpace(window))
	if w == "" || w == "all" {
		return map[string]interface{}{}, nil
	}
	if len(w) < 2 {
		return nil, fmt.Errorf("--window is Nh, Nd or all, not %q", window)
	}
	n, err := strconv.Atoi(w[:len(w)-1])
	if err != nil || n <= 0 {
		return nil, fmt.Errorf("--window is Nh, Nd or all, not %q", window)
	}
	var span time.Duration
	switch w[len(w)-1] {
	case 'h':
		span = time.Duration(n) * time.Hour
	case 'd':
		span = time.Duration(n) * 24 * time.Hour
	default:
		return nil, fmt.Errorf("--window is Nh, Nd or all, not %q", window)
	}
	return map[string]interface{}{"from": now.Add(-span).UTC().Format(time.RFC3339), "to": now.UTC().Format(time.RFC3339)}, nil
}

func agentField(m map[string]interface{}, k string) string {
	if v, ok := m[k].(string); ok {
		return v
	}
	return ""
}

// agentLine is one row of the Agents tab as a line: state, agent and session, roles, last poll, completed tasks,
// spend, cache share, and the stale rules that name it.
func agentLine(row map[string]interface{}) string {
	st, _ := row["state"].(map[string]interface{})
	state := agentField(st, "kind")
	switch state {
	case "WORKING":
		state += " " + agentField(st, "taskKey")
	case "CLOSED":
		if by, ok := st["closedBy"].(map[string]interface{}); ok && agentField(by, "name") != "" {
			state += " by " + agentField(by, "name")
		}
	}
	if since := agentField(st, "since"); since != "" {
		state += " since " + since
	}
	session := agentField(row, "session")
	if len(session) > 8 {
		session = session[:8]
	}
	parts := []string{state, fmt.Sprintf("%s (%s)", agentField(row, "agentName"), session)}
	if roles, ok := row["roles"].([]interface{}); ok && len(roles) > 0 {
		names := make([]string, 0, len(roles))
		for _, r := range roles {
			names = append(names, fmt.Sprint(r))
		}
		parts = append(parts, strings.Join(names, ","))
	}
	if p := agentField(row, "lastPollAt"); p != "" {
		parts = append(parts, "last poll "+p)
	}
	if n, ok := row["tasksCompleted"].(float64); ok && n > 0 {
		parts = append(parts, fmt.Sprintf("%d done", int(n)))
	}
	if c, ok := row["costMicros"].(float64); ok && c > 0 {
		parts = append(parts, fmt.Sprintf("$%.2f", c/1e6))
	}
	if s, ok := row["cacheShare"].(float64); ok {
		parts = append(parts, fmt.Sprintf("cache %d%%", int(s*100+0.5)))
	}
	if stale, ok := row["stale"].([]interface{}); ok && len(stale) > 0 {
		rules := make([]string, 0, len(stale))
		for _, m := range stale {
			if mm, ok := m.(map[string]interface{}); ok {
				rules = append(rules, agentField(mm, "rule"))
			}
		}
		parts = append(parts, "STALE "+strings.Join(rules, ","))
	}
	return strings.Join(parts, " · ")
}

var agentBoardAgentsCmd = &cobra.Command{
	Use:   "agents <board>",
	Short: "Who works the board: each session's state, last poll, spend and stale marks (the Agents tab)",
	Long: `Lists every session that worked or polled the board (task RD3-5), open ones first, then closed, each by
last activity: its state (WORKING a task, WAITING for work, IDLE, CLOSED), the roles it worked, its last poll,
the tasks it completed, its spend and cache share in the window, and any staleness rule that names it.
--window is 24h, 7d (default), 30d, 90d, any Nh or Nd, or all. --json prints the rows as the server returns them.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		vars, err := agentsWindow(boardAgentsWindow, time.Now())
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
		vars["boardUuid"] = boardArg(args[0])
		data, err := sendGraphQLRequest(rearm.AgentBoardAgentsProgrammatic_Operation, vars)
		if err != nil {
			printRefusal(err)
			os.Exit(1)
		}
		board, _ := data["agentBoardProgrammatic"].(map[string]interface{})
		rows, _ := board["agents"].([]interface{})
		if boardAgentsJson {
			emitJson(rows)
			return
		}
		if len(rows) == 0 {
			fmt.Println("No session has worked or polled this board.")
			return
		}
		for _, r := range rows {
			if m, ok := r.(map[string]interface{}); ok {
				fmt.Println(agentLine(m))
			}
		}
	},
}

func init() {
	agentBoardAgentsCmd.Flags().StringVar(&boardAgentsWindow, "window", "7d", "the spend window: 24h, 7d, 30d, 90d, Nh, Nd or all")
	agentBoardAgentsCmd.Flags().BoolVar(&boardAgentsJson, "json", false, "print the rows as JSON")
	agentBoardCmd.AddCommand(agentBoardAgentsCmd)
}
