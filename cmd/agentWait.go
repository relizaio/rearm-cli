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
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	rearm "github.com/relizaio/rearm-client-go"
	"github.com/spf13/cobra"
)

// Waiting for work without spending turns (task RD2-32). An idle agent that polls spends a model
// turn per poll, and most polls are empty. `rearm agent wait` runs in the background of the
// agent's harness, costs nothing while it waits, and exits only when there is something to do:
// 0 with the work, 2 on the heartbeat timeout (re-arm), 1 after two failed polls in a row.

const (
	waitExitWork    = 0
	waitExitError   = 1
	waitExitTimeout = 2
	// waitIntervalFloor keeps the loops of several agents on one host inside the ingress limit of
	// about 50 commands per 30 s: a worker makes one request a poll, a coordinator three or four.
	waitIntervalFloor = 30
)

var (
	waitSession     string
	waitBoard       string
	waitRoles       []string
	waitCoordinator bool
	waitAfter       int64
	waitInterval    int
	waitTimeout     time.Duration
	waitState       string
)

// waitOpts is what one run of the loop was asked for.
type waitOpts struct {
	session     string
	board       string
	roles       []string
	coordinator bool
	after       int64
	interval    time.Duration
	timeout     time.Duration
	statePath   string
}

// waitClient is the board as the loop reads it; the real one sends the CLI's operations, a test
// hands in a fake.
type waitClient interface {
	next(session, board string, roles []string) (interface{}, error)
	events(board string, after *int64, since string) (eventPage, error)
	snapshot(board string) ([]map[string]interface{}, error)
	touch(session string) error
	mergeBy(board string) (string, error)
	delivering(board string) ([]map[string]interface{}, error)
}

// waitClock is time as the loop sees it, so a test runs a four-hour timeout in no time.
type waitClock interface {
	now() time.Time
	sleep(d time.Duration)
}

type realClock struct{}

func (realClock) now() time.Time        { return time.Now() }
func (realClock) sleep(d time.Duration) { time.Sleep(d) }

// waitOptsOf checks the flags. The interval has a floor; the board is required for a coordinator.
func waitOptsOf(session, board string, roles []string, coordinator bool, after int64, interval int,
	timeout time.Duration, statePath string) (waitOpts, error) {
	if strings.TrimSpace(session) == "" {
		return waitOpts{}, fmt.Errorf("--session is required: the session whose work to wait for")
	}
	if interval < waitIntervalFloor {
		return waitOpts{}, fmt.Errorf("interval is %d s or more", waitIntervalFloor)
	}
	if timeout <= 0 {
		return waitOpts{}, fmt.Errorf("--timeout must be positive, e.g. 4h")
	}
	if coordinator && strings.TrimSpace(board) == "" {
		return waitOpts{}, fmt.Errorf("--coordinator needs --board: the board whose seat you hold")
	}
	if coordinator && len(roles) > 0 {
		return waitOpts{}, fmt.Errorf("--role is a worker's; a coordinator waits on the board, not on roles")
	}
	if coordinator && statePath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return waitOpts{}, fmt.Errorf("no home directory for the state file; pass --state: %v", err)
		}
		statePath = filepath.Join(home, ".rearm", "wait-"+board+".json")
	}
	return waitOpts{session: session, board: board, roles: roles, coordinator: coordinator, after: after,
		interval: time.Duration(interval) * time.Second, timeout: timeout, statePath: statePath}, nil
}

// traced says whether the shell traces commands: credentials are in the environment, and a traced
// loop would print them into the agent's transcript on every poll.
func traced(shellopts string) bool {
	for _, o := range strings.Split(shellopts, ":") {
		if o == "xtrace" {
			return true
		}
	}
	return false
}

// runWait polls until there is work, the timeout passes, or two polls in a row fail, and returns
// the exit code. What it prints goes to out, one JSON document.
func runWait(o waitOpts, c waitClient, clk waitClock, out io.Writer) int {
	deadline := clk.now().Add(o.timeout)
	var coord *coordinatorWait
	if o.coordinator {
		coord = &coordinatorWait{opts: o, startedAt: clk.now()}
		if o.after > 0 {
			a := o.after
			coord.after = &a
		}
	}
	failures := 0
	for {
		var done bool
		var result interface{}
		var err error
		if coord != nil {
			done, result, err = coord.poll(c)
		} else {
			result, err = c.next(o.session, o.board, o.roles)
			done = err == nil && result != nil
		}
		if err != nil {
			failures++
			if failures >= 2 {
				fmt.Fprintf(os.Stderr, "rearm agent wait: two polls in a row failed: %v\n", err)
				return waitExitError
			}
		} else {
			failures = 0
			if done {
				emitTo(out, result)
				return waitExitWork
			}
		}
		if !clk.now().Add(o.interval).Before(deadline) {
			emitTo(out, map[string]interface{}{"timeout": true})
			return waitExitTimeout
		}
		clk.sleep(o.interval)
	}
}

func emitTo(out io.Writer, v interface{}) {
	b, _ := json.MarshalIndent(v, "", "  ")
	fmt.Fprintln(out, string(b))
}

// A trigger is one thing the coordinator should look at: a task in a state that waits on it, or
// an ALERT event.
type waitTrigger struct {
	Task  string `json:"task,omitempty"`
	Kind  string `json:"kind"`
	Alert int64  `json:"alert,omitempty"`
}

func (t waitTrigger) id() string {
	if t.Alert != 0 {
		return fmt.Sprintf("ALERT %d", t.Alert)
	}
	return t.Task + " " + t.Kind
}

// coordinatorWait is the coordinator's loop state: the event cursor, the events and ALERTs seen
// since the last wake, and who merges on the board, read once.
type coordinatorWait struct {
	opts      waitOpts
	startedAt time.Time
	after     *int64
	events    []map[string]interface{}
	alerts    []int64
	merge     *string
}

// poll reads the board once: the events since the cursor, the snapshot, and the seat's touch.
// It says done when the trigger set is non-empty and differs from the one the last wake saw.
func (w *coordinatorWait) poll(c waitClient) (bool, interface{}, error) {
	if err := c.touch(w.opts.session); err != nil {
		return false, nil, err
	}
	if err := w.readEvents(c); err != nil {
		return false, nil, err
	}
	tasks, err := c.snapshot(w.opts.board)
	if err != nil {
		return false, nil, err
	}
	triggers := taskTriggers(tasks)
	if anyDelivering(tasks) {
		if w.merge == nil {
			by, err := c.mergeBy(w.opts.board)
			if err != nil {
				return false, nil, err
			}
			w.merge = &by
		}
		if *w.merge == "COORDINATOR" {
			delivering, err := c.delivering(w.opts.board)
			if err != nil {
				return false, nil, err
			}
			triggers = append(triggers, deliveryTriggers(delivering)...)
		}
	}
	for _, seq := range w.alerts {
		triggers = append(triggers, waitTrigger{Kind: "ALERT", Alert: seq})
	}
	sort.Slice(triggers, func(i, j int) bool { return triggers[i].id() < triggers[j].id() })
	ids := make([]string, 0, len(triggers))
	for _, t := range triggers {
		ids = append(ids, t.id())
	}
	last := readWaitState(w.opts.statePath)
	if len(ids) == 0 {
		// An emptied set clears the state, so the same triggers coming back later wake it again.
		if len(last) > 0 {
			writeWaitState(w.opts.statePath, nil)
		}
		return false, nil, nil
	}
	if sameIds(ids, last) {
		return false, nil, nil
	}
	writeWaitState(w.opts.statePath, ids)
	var nextAfter interface{}
	if w.after != nil {
		nextAfter = *w.after
	}
	events := w.events
	if events == nil {
		events = []map[string]interface{}{}
	}
	result := map[string]interface{}{"triggers": triggers, "events": events, "nextAfter": nextAfter}
	return true, result, nil
}

// readEvents reads on from the cursor to the end, keeping INFO and ALERT events for the wake's
// output and the ALERTs as triggers. The cursor starts at the moment the loop started, so events
// from before never wake it.
func (w *coordinatorWait) readEvents(c waitClient) error {
	since := ""
	if w.after == nil {
		since = w.startedAt.UTC().Format(time.RFC3339)
	}
	for {
		p, err := c.events(w.opts.board, w.after, since)
		if err != nil {
			return err
		}
		for _, e := range p.Events {
			kind, _ := e["kind"].(string)
			if kind != "INFO" && kind != "ALERT" {
				continue
			}
			w.events = append(w.events, e)
			if kind == "ALERT" {
				if s, ok := e["seq"].(float64); ok {
					w.alerts = append(w.alerts, int64(s))
				}
			}
		}
		if p.NextAfter != nil {
			a := *p.NextAfter
			w.after = &a
			since = ""
		}
		if !p.HasMore {
			return nil
		}
	}
}

// taskTriggers are the snapshot's tasks that wait on the coordinator: new intake, a hop handed
// back, and a hold at the coordinator's level. A hold at the operator's level or a human gate
// waits on a person and is not one.
func taskTriggers(tasks []map[string]interface{}) []waitTrigger {
	var out []waitTrigger
	for _, entry := range tasks {
		t, _ := entry["task"].(map[string]interface{})
		if t == nil {
			continue
		}
		status, _ := t["status"].(string)
		switch status {
		case "PENDING_INTAKE", "AWAITING_COORDINATOR":
			out = append(out, waitTrigger{Task: taskName(t), Kind: status})
		case "ON_HOLD":
			h, _ := t["hold"].(map[string]interface{})
			if level, _ := h["level"].(string); level == "COORDINATOR" {
				out = append(out, waitTrigger{Task: taskName(t), Kind: "COORDINATOR_HOLD"})
			}
		}
	}
	return out
}

func anyDelivering(tasks []map[string]interface{}) bool {
	for _, entry := range tasks {
		if t, _ := entry["task"].(map[string]interface{}); t != nil {
			if s, _ := t["status"].(string); s == "DELIVERING" {
				return true
			}
		}
	}
	return false
}

// deliveryTriggers are the DELIVERING tasks with a PR not yet merged, when the merge is the
// coordinator's.
func deliveryTriggers(tasks []map[string]interface{}) []waitTrigger {
	var out []waitTrigger
	for _, t := range tasks {
		prs, _ := t["pullRequests"].([]interface{})
		for _, p := range prs {
			pr, _ := p.(map[string]interface{})
			if state, _ := pr["state"].(string); state != "MERGED" {
				out = append(out, waitTrigger{Task: taskName(t), Kind: "DELIVERING"})
				break
			}
		}
	}
	return out
}

// taskName is how a trigger names a task: its key, else its uuid.
func taskName(t map[string]interface{}) string {
	if k, _ := t["key"].(string); k != "" {
		return k
	}
	u, _ := t["uuid"].(string)
	return u
}

func sameIds(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// readWaitState is the trigger set the last wake saw; none when the file is absent or unreadable.
func readWaitState(path string) []string {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var s struct {
		Triggers []string `json:"triggers"`
	}
	if json.Unmarshal(b, &s) != nil {
		return nil
	}
	return s.Triggers
}

func writeWaitState(path string, ids []string) {
	if path == "" {
		return
	}
	if len(ids) == 0 {
		_ = os.Remove(path)
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	b, _ := json.Marshal(map[string]interface{}{"triggers": ids})
	_ = os.WriteFile(path, b, 0o600)
}

// cliWaitClient reads the board with the CLI's operations and credentials.
type cliWaitClient struct{}

func (cliWaitClient) next(session, board string, roles []string) (interface{}, error) {
	vars := map[string]interface{}{"sessionUuid": session}
	if board != "" {
		vars["boardUuid"] = board
	}
	if len(roles) > 0 {
		vars["roles"] = roles
	}
	data, err := sendGraphQLRequest(rearm.AgentTaskNextProgrammatic_Operation, vars)
	if err != nil {
		return nil, err
	}
	return data["agentTaskNextProgrammatic"], nil
}

func (cliWaitClient) events(board string, after *int64, since string) (eventPage, error) {
	vars := map[string]interface{}{"boardUuid": board}
	if after != nil {
		vars["after"] = *after
	} else if since != "" {
		vars["since"] = since
	}
	data, err := sendGraphQLRequest(rearm.AgentBoardEventsProgrammatic_Operation, vars)
	if err != nil {
		return eventPage{}, err
	}
	return pageOf(data)
}

func (cliWaitClient) snapshot(board string) ([]map[string]interface{}, error) {
	data, err := sendGraphQLRequest(rearm.AgentBoardSnapshotProgrammatic_Operation,
		map[string]interface{}{"boardUuid": board})
	if err != nil {
		return nil, err
	}
	snap, _ := data["agentBoardSnapshotProgrammatic"].(map[string]interface{})
	return mapsOf(snap["tasks"]), nil
}

func (cliWaitClient) touch(session string) error {
	_, err := sendGraphQLRequest(rearm.SessionTouchProgrammatic_Operation, map[string]interface{}{"sessionUuid": session})
	return err
}

func (cliWaitClient) mergeBy(board string) (string, error) {
	data, err := sendGraphQLRequest(rearm.AgentBoardProgrammatic_Operation, map[string]interface{}{"boardUuid": board})
	if err != nil {
		return "", err
	}
	b, _ := data["agentBoardProgrammatic"].(map[string]interface{})
	p, _ := b["effectiveDeliveryPolicy"].(map[string]interface{})
	m, _ := p["merge"].(map[string]interface{})
	by, _ := m["by"].(string)
	return by, nil
}

func (cliWaitClient) delivering(board string) ([]map[string]interface{}, error) {
	data, err := sendGraphQLRequest(rearm.AgentWaitDeliveringProgrammatic_Operation,
		map[string]interface{}{"boardUuid": board})
	if err != nil {
		return nil, err
	}
	return mapsOf(data["agentTasksProgrammatic"]), nil
}

func mapsOf(v interface{}) []map[string]interface{} {
	list, _ := v.([]interface{})
	out := make([]map[string]interface{}, 0, len(list))
	for _, e := range list {
		if m, ok := e.(map[string]interface{}); ok {
			out = append(out, m)
		}
	}
	return out
}

var agentWaitCmd = &cobra.Command{
	Use:   "wait",
	Short: "Wait for work in the background: exits 0 with the work, 2 on the timeout, 1 on repeated errors",
	Long: `Waits for work without spending an agent's turns: run it in the background and take a
turn when it exits.

A worker (the default) polls 'task next' for the session every --interval seconds and exits 0
printing the offer, the same JSON 'task next' prints. It does not claim: assign the task as usual.

A coordinator (--coordinator, with the seat's session and --board) reads the board's events, its
snapshot, and touches the seat, every interval. It exits 0 when something waits on it: a task in
PENDING_INTAKE or AWAITING_COORDINATOR, a hold at the coordinator's level, a DELIVERING task with a
PR not merged when the board's merge is the coordinator's, or an ALERT. It prints the triggers, the
INFO and ALERT events since the cursor, and nextAfter. The set it woke on is kept in --state, and
the same set does not wake it twice; a set that empties and comes back does. A task waiting on a
person (an operator-level hold, a human gate) does not wake it.

--interval is 30 s or more (default 60): every agent on a host shares a limit of about 50 commands
per 30 s. --timeout (default 4h) exits 2 with {"timeout": true}: re-arm. One failed poll is
retried; two in a row exit 1 with the error. Never run it with shell tracing: credentials are in
the environment.`,
	Run: func(cmd *cobra.Command, args []string) {
		if traced(os.Getenv("SHELLOPTS")) {
			fmt.Fprintln(os.Stderr, "do not trace this command: credentials are in the environment")
			os.Exit(waitExitError)
		}
		roles := cleanRoles(waitRoles)
		if cmd.Flags().Changed("role") && len(roles) == 0 {
			fmt.Fprintln(os.Stderr, "rearm: --role was given but is empty; name a role, or leave --role out to consider every role")
			os.Exit(waitExitError)
		}
		o, err := waitOptsOf(waitSession, waitBoard, roles, waitCoordinator, waitAfter, waitInterval, waitTimeout, waitState)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(waitExitError)
		}
		os.Exit(runWait(o, cliWaitClient{}, realClock{}, os.Stdout))
	},
}

func init() {
	agentWaitCmd.Flags().StringVar(&waitSession, "session", "", "the session waiting: a worker's, or the coordinator seat's — required")
	agentWaitCmd.Flags().StringVar(&waitBoard, "board", "", "the board; required with --coordinator")
	agentWaitCmd.Flags().StringSliceVar(&waitRoles, "role", nil, "a worker's roles, as for 'task next' (repeat or comma-separate)")
	agentWaitCmd.Flags().BoolVar(&waitCoordinator, "coordinator", false, "wait as the board's coordinator")
	agentWaitCmd.Flags().Int64Var(&waitAfter, "after", 0, "coordinator: the event seq to read on from (default: from now)")
	agentWaitCmd.Flags().IntVar(&waitInterval, "interval", 60, "seconds between polls, 30 or more")
	agentWaitCmd.Flags().DurationVar(&waitTimeout, "timeout", 4*time.Hour, "exit 2 after this long with nothing to do")
	agentWaitCmd.Flags().StringVar(&waitState, "state", "", "coordinator: where the last wake's triggers are kept (default ~/.rearm/wait-<board>.json)")
	agentCmd.AddCommand(agentWaitCmd)
}
