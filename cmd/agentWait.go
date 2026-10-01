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
	// about 50 commands per 30 s: a worker makes one request a poll (three with --watch), a coordinator
	// three or four.
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
	waitWatch       bool
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
	// watch widens a worker's wake (task RD4-3): see agentWaitWatch.go.
	watch bool
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
		coord = &coordinatorWait{opts: o}
		// Where the event cursor starts (RD2-32 T-1): --after when given; else where the last run
		// stopped, kept in --state, so what was posted while the coordinator worked its turn is read;
		// else from now, the first time on this host.
		st := readWaitState(o.statePath)
		switch {
		case o.after > 0:
			a := o.after
			coord.after = &a
		case st.After != nil:
			coord.after = st.After
		case st.Since != "":
			coord.since = st.Since
		default:
			coord.since = clk.now().UTC().Format(time.RFC3339)
		}
	}
	var watch *workerWatch
	if o.watch {
		watch = newWorkerWatch(o, clk)
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
			if watch != nil && err == nil {
				// A watch reads the board after an offer too (task RD4-16), so the state keeps the changes and
				// questions this exit prints, and it prints the watch object with the offer inside it. An offer
				// still wins: it ends the wait even when that read fails.
				woke, werr := watch.poll(c)
				switch {
				case result != nil:
					if werr != nil {
						fmt.Fprintf(os.Stderr, "rearm agent wait: --watch could not read the board with the offer: %v\n", werr)
					}
					result = watch.object(result)
				case werr != nil:
					err = werr
				default:
					done = woke
					if woke {
						result = watch.object(nil)
					}
				}
			}
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
				if watch != nil {
					watch.persist()
				}
				emitTo(out, result)
				return waitExitWork
			}
		}
		if !clk.now().Add(o.interval).Before(deadline) {
			timeout := map[string]interface{}{"timeout": true}
			if coord != nil {
				// What was read before the timeout, and where the next run reads on from.
				for k, v := range coord.cursorOut() {
					timeout[k] = v
				}
			}
			if watch != nil {
				// The timeout keeps the state and prints the watch object too (task RD4-16).
				watch.persist()
				timeout = watch.object(nil)
				timeout["timeout"] = true
			}
			emitTo(out, timeout)
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
	// New marks a trigger the last poll did not see: what woke the wait (task RD3-15). An ALERT is always new.
	New bool `json:"new,omitempty"`
}

func (t waitTrigger) id() string {
	if t.Alert != 0 {
		return fmt.Sprintf("ALERT %d", t.Alert)
	}
	return t.Task + " " + t.Kind
}

// coordinatorWait is the coordinator's loop state: the event cursor (a seq once one is known, the
// instant to read from before), the events and ALERTs seen in this run, and who merges, read once.
type coordinatorWait struct {
	opts   waitOpts
	after  *int64
	since  string
	events []map[string]interface{}
	alerts []int64
	merge  *string
}

// cursorOut is what every exit prints about the events: those read in this run (INFO and ALERT),
// nextAfter when a seq is known, and else the instant the next run reads from. The same cursor is
// kept in --state, so a run started without --after reads on from here.
func (w *coordinatorWait) cursorOut() map[string]interface{} {
	events := w.events
	if events == nil {
		events = []map[string]interface{}{}
	}
	out := map[string]interface{}{"events": events, "nextAfter": nil}
	if w.after != nil {
		out["nextAfter"] = *w.after
	} else {
		out["since"] = w.since
	}
	return out
}

// poll reads the board once: the events since the cursor, the snapshot, and the seat's touch.
// It says done when a task trigger appears that the last poll did not see, or on an ALERT (task RD3-15): a set
// that only shrinks, or stays the same, is quiet.
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
	// The dedupe compares the task triggers only: an ALERT is new each time and wakes on its own, and
	// keeping it in the set would make the next run wake again on the tasks alone once it drops out.
	sort.Slice(triggers, func(i, j int) bool { return triggers[i].id() < triggers[j].id() })
	ids := make([]string, 0, len(triggers))
	for _, t := range triggers {
		ids = append(ids, t.id())
	}
	st := readWaitState(w.opts.statePath)
	fresh := newSince(ids, st.Triggers)
	for i := range triggers {
		triggers[i].New = fresh[triggers[i].id()]
	}
	for _, seq := range w.alerts {
		triggers = append(triggers, waitTrigger{Kind: "ALERT", Alert: seq, New: true})
	}
	// What woke it first, then what still waits.
	sort.Slice(triggers, func(i, j int) bool {
		if triggers[i].New != triggers[j].New {
			return triggers[i].New
		}
		return triggers[i].id() < triggers[j].id()
	})
	// The cursor is kept every poll, so a run that ends -- woken, timed out or killed -- hands the
	// next one the place it stopped reading.
	st.After, st.Since = w.after, ""
	if w.after == nil {
		st.Since = w.since
	}
	if len(w.alerts) == 0 && len(ids) == 0 {
		// An emptied set clears the triggers, so the same ones coming back later wake it again.
		st.Triggers = nil
		writeWaitState(w.opts.statePath, st)
		return false, nil, nil
	}
	// The set kept is the current one, every poll, woken or not: a trigger that leaves and comes back is
	// new again when it returns, and one already reported stays reported while it waits.
	st.Triggers = ids
	writeWaitState(w.opts.statePath, st)
	if len(w.alerts) == 0 && len(fresh) == 0 {
		return false, nil, nil
	}
	result := w.cursorOut()
	result["triggers"] = triggers
	return true, result, nil
}

// readEvents reads on from the cursor to the end, keeping INFO and ALERT events for the output and
// the ALERTs as triggers.
func (w *coordinatorWait) readEvents(c waitClient) error {
	kept, err := readBoardEvents(c, w.opts.board, &w.after, &w.since, func(kind string) bool {
		return kind == "INFO" || kind == "ALERT"
	})
	for _, e := range kept {
		w.events = append(w.events, e)
		if kind, _ := e["kind"].(string); kind == "ALERT" {
			if s, ok := e["seq"].(float64); ok {
				w.alerts = append(w.alerts, int64(s))
			}
		}
	}
	return err
}

// readBoardEvents reads a board's events on from the cursor (a seq once one is known, the instant
// before) to the end, moves the cursor, and returns the events of the kinds keep takes.
func readBoardEvents(c waitClient, board string, after **int64, since *string,
	keep func(kind string) bool) ([]map[string]interface{}, error) {
	var kept []map[string]interface{}
	for {
		from := ""
		if *after == nil {
			from = *since
		}
		p, err := c.events(board, *after, from)
		if err != nil {
			return kept, err
		}
		for _, e := range p.Events {
			if kind, _ := e["kind"].(string); keep(kind) {
				kept = append(kept, e)
			}
		}
		if p.NextAfter != nil {
			a := *p.NextAfter
			*after = &a
			*since = ""
		}
		if !p.HasMore {
			return kept, nil
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

// newSince is the ids in the current set that the last poll's set did not hold.
func newSince(ids, last []string) map[string]bool {
	seen := map[string]bool{}
	for _, id := range last {
		seen[id] = true
	}
	out := map[string]bool{}
	for _, id := range ids {
		if !seen[id] {
			out[id] = true
		}
	}
	return out
}

// waitKept is what a coordinator's wait keeps between runs: the trigger set the last poll saw, and
// the event cursor -- a seq once one is known, else the instant to read from.
type waitKept struct {
	Triggers []string `json:"triggers,omitempty"`
	After    *int64   `json:"after,omitempty"`
	Since    string   `json:"since,omitempty"`
}

// readWaitState is the kept state; empty when the file is absent or unreadable.
func readWaitState(path string) waitKept {
	var s waitKept
	if path == "" {
		return s
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return s
	}
	if json.Unmarshal(b, &s) != nil {
		return waitKept{}
	}
	return s
}

func writeWaitState(path string, s waitKept) {
	if path == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	b, _ := json.Marshal(s)
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
INFO and ALERT events it read, and nextAfter. It wakes when a trigger appears that it has not
reported (a new task, or a task in a new state), or on an ALERT; a set that only shrinks stays
quiet. The set each poll saw is kept in --state, so a trigger that leaves and comes back wakes it
again. The triggers print with the new ones first, marked "new": true. A task waiting on a person
(an operator-level hold, a human gate) does not wake it.

The event cursor is kept in --state too: a wait started without --after reads on from where the
last one stopped, so what was posted while you worked your turn is read. Pass --after <nextAfter>
when the last exit printed one; nothing is lost when it printed null. The timeout prints the events
it read and the cursor as well.

--interval is 30 s or more (default 60): every agent on a host shares a limit of about 50 commands
per 30 s. --timeout (default 4h) exits 2 with {"timeout": true}: re-arm. One failed poll is
retried; two in a row exit 1 with the error. Never run it with shell tracing: credentials are in
the environment.

A worker with --watch (and --board) also wakes, when no offer is there, on what happens to its
work (task RD4-3): a task the session signed off on that a later hop REJECTED, returned, or that
was reopened, or that a later hop passed after the session's own rejection (the producer's next
round landed); an open question on the board addressed to the session's role (--role, else the
roles it worked) while it is still the session's to answer: its task is not COMPLETED or CANCELLED,
and the session has published no round of the answering role's specification on it since the
question was asked (task RD4-14); an ALERT, PAUSED or RESUMED event. It prints {"offer": null, "changes": [...],
"questions": [...], "events": [...], "nextAfter": N}: each change names the task, the transition
(from, to, trigger, by, at) and the document rounds published since the session's last sign-off
on it, so the rejection can be read without a 'task show'. It wakes only on what it has not
reported: the changes and questions each poll saw, and the event cursor, are kept in --state
(default ~/.rearm/wait-<board>-watch-<session>.json); a first run, with no state file, lists a
question asked before the session's last sign-off on its task without waking on it. A question
names its roles (askingRole, answeringRole), with the role config uuids beside them
(askingRoleUuid, answeringRoleUuid). An offer still wins, and with --watch it prints inside the
same object, as "offer", with the changes and questions read with it (task RD4-16): whatever ends
the wait (an offer, a change, a question, an event or the timeout), the output is that object, and
what it reported is kept in --state first, so the next run does not report it again. The timeout
prints the object with "timeout": true. A watch makes two more reads a poll (the snapshot and the
events), an offer's poll included.`,
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
		if strings.TrimSpace(waitBoard) != "" {
			waitBoard = boardArg(waitBoard)
		}
		o, err := waitOptsOf(waitSession, waitBoard, roles, waitCoordinator, waitAfter, waitInterval, waitTimeout, waitState)
		if err == nil && waitWatch {
			o, err = o.watching()
		}
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
	agentWaitCmd.Flags().Int64Var(&waitAfter, "after", 0, "coordinator or --watch: the event seq to read on from (default: where the last wait stopped, else now)")
	agentWaitCmd.Flags().IntVar(&waitInterval, "interval", 60, "seconds between polls, 30 or more")
	agentWaitCmd.Flags().DurationVar(&waitTimeout, "timeout", 4*time.Hour, "exit 2 after this long with nothing to do")
	agentWaitCmd.Flags().StringVar(&waitState, "state", "", "coordinator or --watch: where the last poll's triggers are kept (default ~/.rearm/wait-<board>.json; with --watch wait-<board>-watch-<session>.json)")
	agentWaitCmd.Flags().BoolVar(&waitWatch, "watch", false, "worker: also wake on a rejection, return or reopen of a task the session signed off on, the producer's next pass after its own rejection, an open question to its role, or an ALERT/PAUSED/RESUMED event; needs --board")
	agentCmd.AddCommand(agentWaitCmd)
}
