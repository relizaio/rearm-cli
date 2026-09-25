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
	"time"

	rearm "github.com/relizaio/rearm-client-go"
	"github.com/spf13/cobra"
)

var (
	boardEventsAfter  int64
	boardEventsSince  string
	boardEventsLimit  int
	boardEventsJson   bool
	boardEventsFollow bool
)

// boardEventsFollowEvery is how often --follow reads on.
const boardEventsFollowEvery = 30 * time.Second

// eventPage is one page of a board's event log (task 1c5442d2). TruncatedBefore is where the
// board's retention window starts, and Gap says it deleted events this read asked for (task 04dedcc5).
type eventPage struct {
	Events          []map[string]interface{} `json:"events"`
	NextAfter       *int64                   `json:"nextAfter"`
	HasMore         bool                     `json:"hasMore"`
	TruncatedBefore *string                  `json:"truncatedBefore"`
	Gap             bool                     `json:"gap"`
}

// gapWarning is the line a read that lost events to the board's retention prints on stderr, or ""
// when it lost none. The read goes on from the oldest event the board still keeps.
func gapWarning(p eventPage, now time.Time) string {
	if !p.Gap {
		return ""
	}
	const reread = "; re-read the tasks with 'rearm agent task list'"
	if p.TruncatedBefore != nil {
		if at, err := time.Parse(time.RFC3339, *p.TruncatedBefore); err == nil {
			days := int(now.Sub(at).Hours()/24 + 0.5)
			return fmt.Sprintf("gap: events before %s were retained for %d days and are gone%s",
				*p.TruncatedBefore, days, reread)
		}
	}
	return "gap: events this read asked for were deleted by the board's retention" + reread
}

// boardEventsVars are the read's variables: after or since, never both, and the limit when set.
func boardEventsVars(board string, after int64, since string, limit int) (map[string]interface{}, error) {
	if after > 0 && strings.TrimSpace(since) != "" {
		return nil, fmt.Errorf("--after and --since are two ways to say where to start; give one")
	}
	if limit < 0 {
		return nil, fmt.Errorf("--limit must be at least 1")
	}
	vars := map[string]interface{}{"boardUuid": board}
	if after > 0 {
		vars["after"] = after
	}
	if s := strings.TrimSpace(since); s != "" {
		if _, err := time.Parse(time.RFC3339, s); err != nil {
			return nil, fmt.Errorf("--since is an RFC 3339 timestamp, e.g. 2026-09-25T14:00:00Z: %v", err)
		}
		vars["since"] = s
	}
	if limit > 0 {
		vars["limit"] = limit
	}
	return vars, nil
}

// formatEvent is one event on one line: its seq, when, what kind, who, and what was said.
func formatEvent(e map[string]interface{}) string {
	who := ""
	if a, ok := e["actor"].(map[string]interface{}); ok {
		if n, _ := a["name"].(string); n != "" {
			who = n
		} else {
			k, _ := a["kind"].(string)
			u, _ := a["uuid"].(string)
			if len(u) > 8 {
				u = u[:8]
			}
			who = strings.TrimSpace(strings.ToLower(k) + " " + u)
		}
	}
	seq := ""
	if s, ok := e["seq"].(float64); ok {
		seq = fmt.Sprintf("%d", int64(s))
	}
	at, _ := e["eventAt"].(string)
	kind, _ := e["kind"].(string)
	msg, _ := e["message"].(string)
	return fmt.Sprintf("%s  %s  %-8s  %s  %s", seq, at, kind, who, msg)
}

// pageOf reads a page out of the response.
func pageOf(data map[string]interface{}) (eventPage, error) {
	var p eventPage
	raw, err := json.Marshal(data["agentBoardEventsProgrammatic"])
	if err != nil {
		return p, err
	}
	err = json.Unmarshal(raw, &p)
	return p, err
}

// followEvents reads page after page, each from the last one's nextAfter, and waits between reads
// only when it has caught up. A page that lost events to retention is warned about and followed
// on. It stops when stop says so; a failed read ends it with the error.
func followEvents(vars map[string]interface{}, read func(map[string]interface{}) (eventPage, error),
	show func([]map[string]interface{}), warn func(eventPage), wait func(), stop func() bool) error {
	for !stop() {
		p, err := read(vars)
		if err != nil {
			return err
		}
		warn(p)
		show(p.Events)
		if p.NextAfter != nil {
			vars["after"] = *p.NextAfter
			delete(vars, "since")
		}
		if !p.HasMore {
			wait()
		}
	}
	return nil
}

var agentBoardEventsCmd = &cobra.Command{
	Use:   "events <board-uuid>",
	Short: "A board's events since a point, oldest first, with the seq to read on from",
	Long: `Reads the board's event log (task 1c5442d2). board show carries only the newest 50 events;
this is all of them. Start after a seq you already read (--after) or from a time (--since), and
read on from the nextAfter printed at the end. --follow keeps reading every 30 seconds from where
it stopped. Follow the feed this way; never by counting the entries of board show.

A board keeps its events for its eventRetentionDays (15 by default). When events this read asked
for are gone, a "gap:" line on stderr says so and the read goes on from the oldest event kept:
re-read the tasks rather than trusting the feed for the time between.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		vars, err := boardEventsVars(args[0], boardEventsAfter, boardEventsSince, boardEventsLimit)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
		read := func(v map[string]interface{}) (eventPage, error) {
			data, err := sendGraphQLRequest(rearm.AgentBoardEventsProgrammatic_Operation, v)
			if err != nil {
				return eventPage{}, err
			}
			return pageOf(data)
		}
		if !boardEventsFollow {
			p, err := read(vars)
			if err != nil {
				printGqlError(err)
				os.Exit(1)
			}
			warnGap(p)
			if boardEventsJson {
				emitJson(p)
				return
			}
			for _, e := range p.Events {
				fmt.Println(formatEvent(e))
			}
			if p.NextAfter != nil {
				more := ""
				if p.HasMore {
					more = " (more follow)"
				}
				fmt.Printf("nextAfter %d%s\n", *p.NextAfter, more)
			}
			return
		}
		show := func(events []map[string]interface{}) {
			for _, e := range events {
				if boardEventsJson {
					b, _ := json.Marshal(e)
					fmt.Println(string(b))
				} else {
					fmt.Println(formatEvent(e))
				}
			}
		}
		if err := followEvents(vars, read, show, warnGap, func() { time.Sleep(boardEventsFollowEvery) },
			func() bool { return false }); err != nil {
			printGqlError(err)
			os.Exit(1)
		}
	},
}

// warnGap prints a page's gap line on stderr, so it never mixes into the events or the JSON.
func warnGap(p eventPage) {
	if w := gapWarning(p, time.Now()); w != "" {
		fmt.Fprintln(os.Stderr, w)
	}
}

func init() {
	agentBoardEventsCmd.Flags().Int64Var(&boardEventsAfter, "after", 0, "read the events after this seq (exclusive)")
	agentBoardEventsCmd.Flags().StringVar(&boardEventsSince, "since", "", "read the events from this time (RFC 3339)")
	agentBoardEventsCmd.Flags().IntVar(&boardEventsLimit, "limit", 0, "how many per read (server default 200, at most 1000)")
	agentBoardEventsCmd.Flags().BoolVar(&boardEventsJson, "json", false, "print JSON: the page, or with --follow one event per line")
	agentBoardEventsCmd.Flags().BoolVar(&boardEventsFollow, "follow", false, "keep reading every 30 seconds from where it stopped")
	agentBoardCmd.AddCommand(agentBoardEventsCmd)
}
