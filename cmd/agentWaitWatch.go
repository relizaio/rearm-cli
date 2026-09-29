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
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// A worker's wait with --watch (task RD4-3). Without it a worker wakes only on an offer, so an
// architect answering tester rejections, or a reviewer following its findings, ran a polling loop of
// its own beside the wait. With it, a poll that finds no offer also reads the board's snapshot and
// its events, and wakes on:
//   (a) a task the session signed off on that a later hop rejected, returned, or that was reopened,
//       or that a later hop passed after the session's own rejection (the producer's next round);
//   (b) an open question on the board addressed to the session's role, while it is still the
//       session's to answer (task RD4-14): its task is open and the session published no round of
//       the answering role's specification on it since it was asked;
//   (c) an ALERT, LOCKED or UNLOCKED event since the cursor.
// It wakes only on what it has not reported (as the coordinator's wait, task RD3-15): the changes
// and the questions each poll saw, and the event cursor, are kept in --state.
// Whatever ends the wait -- an offer, a change, a question, an event or the timeout -- the watch has read
// the board and kept what it saw before the process exits, and it prints one shape, the watch object,
// with the offer inside it or null (task RD4-16).

// What a watch reads beyond the worker's next, and only what it uses: the session's tasks and the
// board's role names once a run, and a task's hops and documents only when the snapshot shows the
// task moved.
const (
	watchScopeOp = `query AgentWaitWatchScope($sessionUuid: ID!, $boardUuid: ID!) {
	sessionProgrammatic(sessionUuid: $sessionUuid) { tasksWorked { uuid key role board } }
	agentTaskRoleConfigsProgrammatic(boardUuid: $boardUuid) { uuid name producesOutputs { specification } }
}`
	watchTasksOp = `query AgentWaitWatchTasks($taskUuids: [ID!]!) { agentTasksByUuidProgrammatic(taskUuids: $taskUuids) {
	key uuid status role
	signOffs { role session signedOffAt outcome }
	returns { role session reason returnedAt }
	reopens { role at reason by { kind name } }
	statusHistory { from to at trigger }
	documents { uuid createdDate document { specification path round advisory publishedByRole session findings { verdict } } }
} }`
	// watchTasksPage is the most tasks one AgentTasksByUuid read takes.
	watchTasksPage = 100
)

// watchReader is what a watch reads besides the board a worker's and a coordinator's wait read.
type watchReader interface {
	// scope is the tasks the session worked (Session.tasksWorked) and the board's roles: uuid, name and
	// the specifications each produces.
	scope(session, board string) (worked, roles []map[string]interface{}, err error)
	// tasks is the named tasks with their hops, status history and documents.
	tasks(uuids []string) ([]map[string]interface{}, error)
}

// watching checks and completes the options of a worker's --watch.
func (o waitOpts) watching() (waitOpts, error) {
	if o.coordinator {
		return o, fmt.Errorf("--watch is a worker's; a coordinator's wait already wakes on the board")
	}
	if strings.TrimSpace(o.board) == "" {
		return o, fmt.Errorf("--watch needs --board: the board whose tasks and events to watch")
	}
	if o.statePath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return o, fmt.Errorf("no home directory for the state file; pass --state: %v", err)
		}
		o.statePath = filepath.Join(home, ".rearm", "wait-"+o.board+"-watch-"+o.session+".json")
	}
	o.watch = true
	return o, nil
}

// watchChange is what happened to a task the session signed off on, as a wake prints it.
type watchChange struct {
	Task    string `json:"task"`
	Uuid    string `json:"uuid"`
	From    string `json:"from,omitempty"`
	To      string `json:"to"`
	Role    string `json:"role,omitempty"`
	Trigger string `json:"trigger"`
	// By is the role whose hop rejected, returned or passed it, or who reopened it.
	By     string `json:"by,omitempty"`
	At     string `json:"at"`
	Reason string `json:"reason,omitempty"`
	// Documents are the rounds published on the task since the session's last sign-off on it, oldest first.
	Documents []watchDocument `json:"documents"`
	New       bool            `json:"new,omitempty"`
}

// id names the change, not the task's state: the same rejection read again is not new, a second one is.
func (c watchChange) id() string { return c.Uuid + " " + c.Trigger + " " + c.At }

type watchDocument struct {
	Release       string      `json:"release"`
	Specification string      `json:"specification,omitempty"`
	Round         interface{} `json:"round,omitempty"`
	Path          string      `json:"path,omitempty"`
	Advisory      bool        `json:"advisory,omitempty"`
	Role          string      `json:"publishedByRole,omitempty"`
	Verdict       string      `json:"verdict,omitempty"`
	PublishedAt   string      `json:"publishedAt,omitempty"`
}

// watchQuestion is an open question on the board addressed to the session's role. The roles print by
// name, with the role config uuids beside them for scripts (task RD4-14).
type watchQuestion struct {
	Task              string `json:"task"`
	AskingRole        string `json:"askingRole,omitempty"`
	AskingRoleUuid    string `json:"askingRoleUuid,omitempty"`
	AnsweringRole     string `json:"answeringRole"`
	AnsweringRoleUuid string `json:"answeringRoleUuid"`
	QuestionsRelease  string `json:"questionsRelease"`
	AskedAt           string `json:"askedAt,omitempty"`
	New               bool   `json:"new,omitempty"`
	taskUuid          string
}

func (q watchQuestion) id() string { return q.Task + " " + q.QuestionsRelease }

// watchKept is what a watch keeps between polls and runs: per task the snapshot's fingerprint and the
// change it stands at, the questions the last poll saw, and the event cursor.
type watchKept struct {
	Tasks     map[string]watchTaskKept `json:"tasks,omitempty"`
	Questions []string                 `json:"questions,omitempty"`
	After     *int64                   `json:"after,omitempty"`
	Since     string                   `json:"since,omitempty"`
}

type watchTaskKept struct {
	// Seen is the task in the last poll's snapshot: its status, role and latest document releases. A
	// task that did not move is not read again.
	Seen   string       `json:"seen"`
	Change *watchChange `json:"change,omitempty"`
	// Mine is what the session itself did on the task, read with the change. A state written before it
	// existed has none, so the task is read again once.
	Mine *watchMine `json:"mine,omitempty"`
}

// watchMine is the session's own record on a task: its last sign-off, and per specification the newest
// round it published. A question is the session's while neither answers it (task RD4-14).
type watchMine struct {
	SignedOff string            `json:"signedOff,omitempty"`
	Rounds    map[string]string `json:"rounds,omitempty"`
}

// workerWatch is a watch's loop state: the event cursor, the watched events read in this run, and the
// session's tasks and roles, read once a run (they change only when the session takes a task, after
// the wait has exited).
type workerWatch struct {
	opts   waitOpts
	after  *int64
	since  string
	events []map[string]interface{}
	worked []map[string]interface{}
	roles  map[string]bool
	// names and answers are the board's roles by uuid: the name, and the specifications it produces.
	names   map[string]string
	answers map[string][]string
	scoped  bool
	// changes and questions are what the last poll that read the whole board saw, new ones first; polled
	// says one has, so the state holds the sets this run printed (task RD4-16).
	changes   []watchChange
	questions []watchQuestion
	polled    bool
}

func newWorkerWatch(o waitOpts, clk waitClock) *workerWatch {
	w := &workerWatch{opts: o}
	// The cursor starts as the coordinator's does: --after, else where the last run stopped, else now.
	st := readWatchState(o.statePath)
	switch {
	case o.after > 0:
		a := o.after
		w.after = &a
	case st.After != nil:
		w.after = st.After
	case st.Since != "":
		w.since = st.Since
	default:
		w.since = clk.now().UTC().Format(time.RFC3339)
	}
	return w
}

// cursorOut is where the next run reads the events on from: nextAfter once a seq is known, else the
// instant.
func (w *workerWatch) cursorOut() map[string]interface{} {
	out := map[string]interface{}{"nextAfter": nil}
	if w.after != nil {
		out["nextAfter"] = *w.after
	} else {
		out["since"] = w.since
	}
	return out
}

func watchedEventKind(kind string) bool {
	return kind == "ALERT" || kind == "LOCKED" || kind == "UNLOCKED"
}

// object is what a watch prints on every exit (task RD4-16): the offer or null, the changes and the
// questions the last poll saw (new ones first, marked new), the watched events read in this run, and
// the cursor. A caller parses one shape whatever ended the wait.
func (w *workerWatch) object(offer interface{}) map[string]interface{} {
	out := w.cursorOut()
	out["offer"] = offer
	changes, questions, events := w.changes, w.questions, w.events
	if changes == nil {
		changes = []watchChange{}
	}
	if questions == nil {
		questions = []watchQuestion{}
	}
	if events == nil {
		events = []map[string]interface{}{}
	}
	out["changes"] = changes
	out["questions"] = questions
	out["events"] = events
	return out
}

// persist writes the event cursor to the state before the process exits (task RD4-16). The sets are
// written by every poll that reads the whole board; this keeps the cursor of the events printed on an
// exit whose last read stopped short. With no state file and no whole poll in this run it writes
// nothing, so the next run keeps the first-run baseline (task RD4-14).
func (w *workerWatch) persist() {
	if !w.polled && !watchStateExists(w.opts.statePath) {
		return
	}
	st := readWatchState(w.opts.statePath)
	st.After, st.Since = w.after, ""
	if w.after == nil {
		st.Since = w.since
	}
	writeWatchState(w.opts.statePath, st)
}

// poll reads the board once and says woke when something it has not reported appeared: a change on a
// task the session signed off on, a question to its role, or a watched event. It runs after every next
// that did not fail, an offer included, so the state holds what the exit prints (task RD4-16).
func (w *workerWatch) poll(c waitClient) (bool, error) {
	r, ok := c.(watchReader)
	if !ok {
		return false, fmt.Errorf("--watch: this client cannot read the session's tasks")
	}
	if !w.scoped {
		worked, roles, err := r.scope(w.opts.session, w.opts.board)
		if err != nil {
			return false, err
		}
		w.worked, w.roles = watchScope(w.opts, worked, roles)
		w.names, w.answers = roleFacts(roles)
		w.scoped = true
	}
	events, err := readBoardEvents(c, w.opts.board, &w.after, &w.since, watchedEventKind)
	w.events = append(w.events, events...)
	if err != nil {
		return false, err
	}
	snap, err := c.snapshot(w.opts.board)
	if err != nil {
		return false, err
	}
	byUuid := map[string]map[string]interface{}{}
	for _, e := range snap {
		if t, _ := e["task"].(map[string]interface{}); t != nil {
			u, _ := t["uuid"].(string)
			byUuid[u] = e
		}
	}

	// A first run has no state file: its baseline is the session's own sign-offs (task RD4-14).
	first := !watchStateExists(w.opts.statePath)
	st := readWatchState(w.opts.statePath)
	kept := map[string]watchTaskKept{}
	var moved []string
	for _, t := range w.worked {
		u, _ := t["uuid"].(string)
		seen := seenOf(byUuid[u])
		last, had := st.Tasks[u]
		if !had || last.Seen != seen || last.Mine == nil {
			moved = append(moved, u)
		}
		kept[u] = watchTaskKept{Seen: seen, Change: last.Change, Mine: last.Mine}
	}
	if len(moved) > 0 {
		for _, u := range moved {
			if k := kept[u]; k.Mine == nil {
				k.Mine = &watchMine{}
				kept[u] = k
			}
		}
		for i := 0; i < len(moved); i += watchTasksPage {
			end := i + watchTasksPage
			if end > len(moved) {
				end = len(moved)
			}
			details, err := r.tasks(moved[i:end])
			if err != nil {
				return false, err
			}
			for _, d := range details {
				u, _ := d["uuid"].(string)
				k := kept[u]
				k.Change = changeOf(d, w.opts.session)
				k.Mine = mineOf(d, w.opts.session)
				kept[u] = k
			}
		}
	}

	changes := []watchChange{}
	woke := len(w.events) > 0
	for _, t := range w.worked {
		u, _ := t["uuid"].(string)
		k := kept[u]
		if k.Change == nil {
			continue
		}
		ch := *k.Change
		last := st.Tasks[u].Change
		ch.New = last == nil || last.id() != ch.id()
		woke = woke || ch.New
		changes = append(changes, ch)
	}
	questions := questionsTo(snap, w.roles, w.names, w.answers, kept)
	qids := make([]string, 0, len(questions))
	for _, q := range questions {
		qids = append(qids, q.id())
	}
	fresh := newSince(qids, st.Questions)
	for i := range questions {
		q := &questions[i]
		q.New = fresh[q.id()]
		// With no state, a question asked before the session's last sign-off on its task is one the
		// session has already acted on: listed, not new.
		if first && q.New {
			if m := kept[q.taskUuid].Mine; m != nil && m.SignedOff != "" && timeOf(q.AskedAt).Before(timeOf(m.SignedOff)) {
				q.New = false
			}
		}
		woke = woke || q.New
	}
	sort.SliceStable(changes, func(i, j int) bool { return changes[i].New && !changes[j].New })
	sort.SliceStable(questions, func(i, j int) bool { return questions[i].New && !questions[j].New })

	// Kept every poll, woken or not: what was reported stays reported while it stands, and an emptied
	// set clears, so the same question asked again later wakes it again.
	st.Tasks, st.Questions = kept, qids
	if len(qids) == 0 {
		st.Questions = nil
	}
	st.After, st.Since = w.after, ""
	if w.after == nil {
		st.Since = w.since
	}
	writeWatchState(w.opts.statePath, st)
	w.changes, w.questions, w.polled = changes, questions, true
	return woke, nil
}

// watchScope is the session's tasks on the board, and the uuids of the roles it waits as: --role
// when given (a name, in any case, or a uuid), else every role it worked a task of the board as.
func watchScope(o waitOpts, worked, roles []map[string]interface{}) ([]map[string]interface{}, map[string]bool) {
	var mine []map[string]interface{}
	names := map[string]bool{}
	for _, t := range worked {
		if b, _ := t["board"].(string); b != o.board {
			continue
		}
		mine = append(mine, t)
		if r, _ := t["role"].(string); r != "" && len(o.roles) == 0 {
			names[strings.ToLower(r)] = true
		}
	}
	for _, r := range o.roles {
		names[strings.ToLower(r)] = true
	}
	uuids := map[string]bool{}
	for _, r := range roles {
		u, _ := r["uuid"].(string)
		n, _ := r["name"].(string)
		if names[strings.ToLower(n)] || names[strings.ToLower(u)] {
			uuids[u] = true
		}
	}
	return mine, uuids
}

// seenOf is a snapshot entry as a watch compares it between polls: the task's status and role and its
// latest document releases. A hop ending moves one of them.
func seenOf(e map[string]interface{}) string {
	if e == nil {
		return "absent"
	}
	t, _ := e["task"].(map[string]interface{})
	status, _ := t["status"].(string)
	role, _ := t["role"].(string)
	var docs []string
	for _, d := range mapsOf(e["latestDocuments"]) {
		u, _ := d["uuid"].(string)
		l, _ := d["lifecycle"].(string)
		docs = append(docs, u+"@"+l)
	}
	sort.Strings(docs)
	return status + "|" + role + "|" + strings.Join(docs, ",")
}

// changeOf is where a task stands for the session: the newest hop end or reopen after the session's
// own last hop end on it, when that is a rejection, a return or a reopen, or a pass after the session's
// own rejection (the producer's next round landed); nil when it is any other pass, when nothing happened
// since, or when the session never signed off on the task.
func changeOf(t map[string]interface{}, session string) *watchChange {
	var mine time.Time
	signed, rejected := false, false
	for _, s := range mapsOf(t["signOffs"]) {
		if who, _ := s["session"].(string); who == session {
			if at := timeOf(s["signedOffAt"]); !signed || !at.Before(mine) {
				mine = at
				rejected = str(s["outcome"]) == "REJECTED"
			}
			signed = true
		}
	}
	if !signed {
		return nil
	}
	for _, r := range mapsOf(t["returns"]) {
		if who, _ := r["session"].(string); who == session {
			if at := timeOf(r["returnedAt"]); at.After(mine) {
				mine, rejected = at, false
			}
		}
	}
	type hopEnd struct {
		at      time.Time
		raw     string
		trigger string
		by      string
		reason  string
	}
	var newest *hopEnd
	consider := func(h hopEnd) {
		if !h.at.After(mine) {
			return
		}
		if newest == nil || h.at.After(newest.at) {
			newest = &h
		}
	}
	for _, s := range mapsOf(t["signOffs"]) {
		raw, _ := s["signedOffAt"].(string)
		outcome, _ := s["outcome"].(string)
		role, _ := s["role"].(string)
		trigger := "PASSED"
		if outcome == "REJECTED" {
			trigger = "REJECTED"
		}
		consider(hopEnd{at: timeOf(raw), raw: raw, trigger: trigger, by: role})
	}
	for _, r := range mapsOf(t["returns"]) {
		raw, _ := r["returnedAt"].(string)
		role, _ := r["role"].(string)
		reason, _ := r["reason"].(string)
		consider(hopEnd{at: timeOf(raw), raw: raw, trigger: "RETURNED", by: role, reason: reason})
	}
	for _, r := range mapsOf(t["reopens"]) {
		raw, _ := r["at"].(string)
		by := ""
		if a, _ := r["by"].(map[string]interface{}); a != nil {
			by, _ = a["name"].(string)
		}
		reason, _ := r["reason"].(string)
		consider(hopEnd{at: timeOf(raw), raw: raw, trigger: "REOPENED", by: by, reason: reason})
	}
	if newest == nil || (newest.trigger == "PASSED" && !rejected) {
		return nil
	}
	status, _ := t["status"].(string)
	role, _ := t["role"].(string)
	return &watchChange{Task: taskName(t), Uuid: str(t["uuid"]), From: fromOf(t, newest.trigger, newest.at),
		To: status, Role: role, Trigger: newest.trigger, By: newest.by, At: newest.raw, Reason: newest.reason,
		Documents: documentsSince(t, mine)}
}

// fromOf is the status the task left at the change, from its status history: the transition the hop
// end or reopen wrote, the newest of its kind at or before the change.
func fromOf(t map[string]interface{}, trigger string, at time.Time) string {
	kinds := map[string][]string{
		"REJECTED": {"SIGNOFF", "HUMAN_REJECT", "HUMAN_SIGNOFF"},
		"PASSED":   {"SIGNOFF", "HUMAN_APPROVE", "HUMAN_SIGNOFF"},
		"RETURNED": {"RETURN"},
		"REOPENED": {"REOPEN"},
	}[trigger]
	from := ""
	var best time.Time
	for _, h := range mapsOf(t["statusHistory"]) {
		tr, _ := h["trigger"].(string)
		match := false
		for _, k := range kinds {
			match = match || tr == k
		}
		ht := timeOf(h["at"])
		// A second's slack: the change and its transition are written in one transaction.
		if !match || ht.After(at.Add(time.Second)) || ht.Before(best) {
			continue
		}
		best = ht
		from, _ = h["from"].(string)
	}
	return from
}

// documentsSince is the task's document rounds published after the instant, oldest first.
func documentsSince(t map[string]interface{}, since time.Time) []watchDocument {
	out := []watchDocument{}
	type dated struct {
		at  time.Time
		doc watchDocument
	}
	var list []dated
	for _, d := range mapsOf(t["documents"]) {
		at := timeOf(d["createdDate"])
		if !at.After(since) {
			continue
		}
		ref, _ := d["document"].(map[string]interface{})
		doc := watchDocument{Release: str(d["uuid"]), PublishedAt: str(d["createdDate"])}
		if ref != nil {
			doc.Specification = str(ref["specification"])
			doc.Round = ref["round"]
			doc.Path = str(ref["path"])
			doc.Advisory, _ = ref["advisory"].(bool)
			doc.Role = str(ref["publishedByRole"])
			if f, _ := ref["findings"].(map[string]interface{}); f != nil {
				doc.Verdict = str(f["verdict"])
			}
		}
		list = append(list, dated{at, doc})
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].at.Before(list[j].at) })
	for _, d := range list {
		out = append(out, d.doc)
	}
	return out
}

// questionsTo is the snapshot's open questions that are still the session's to answer (task RD4-14):
// the answering role is one of roles, the task is not COMPLETED or CANCELLED, and the session has
// published no round of a specification the answering role produces on the task since askedAt. A
// question that fails any of them is not listed. The roles print by name, the uuids beside them.
func questionsTo(snap []map[string]interface{}, roles map[string]bool, names map[string]string,
	answers map[string][]string, kept map[string]watchTaskKept) []watchQuestion {
	out := []watchQuestion{}
	for _, e := range snap {
		q, _ := e["waitingOn"].(map[string]interface{})
		if q == nil {
			continue
		}
		answering := str(q["answeringRole"])
		if answering == "" || !roles[answering] {
			continue
		}
		t, _ := e["task"].(map[string]interface{})
		if s := str(t["status"]); s == "COMPLETED" || s == "CANCELLED" {
			continue
		}
		u := str(t["uuid"])
		if answeredBy(kept[u].Mine, answers[answering], timeOf(q["askedAt"])) {
			continue
		}
		asking := str(q["askingRole"])
		out = append(out, watchQuestion{Task: taskName(t), AskingRole: watchRoleName(names, asking), AskingRoleUuid: asking,
			AnsweringRole: watchRoleName(names, answering), AnsweringRoleUuid: answering,
			QuestionsRelease: str(q["questionsRelease"]), AskedAt: str(q["askedAt"]), taskUuid: u})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].id() < out[j].id() })
	return out
}

// answeredBy says whether the session published, after the instant, a round of one of the answering
// specifications; of any specification but QUESTIONS when the answering role declares none.
func answeredBy(m *watchMine, specs []string, asked time.Time) bool {
	if m == nil {
		return false
	}
	for spec, at := range m.Rounds {
		if len(specs) == 0 && spec == "QUESTIONS" {
			continue
		}
		if len(specs) > 0 && !contains(specs, spec) {
			continue
		}
		if timeOf(at).After(asked) {
			return true
		}
	}
	return false
}

// watchRoleName is a role config's name, or its uuid when the board's role list does not have it.
func watchRoleName(names map[string]string, uuid string) string {
	if n := names[uuid]; n != "" {
		return n
	}
	return uuid
}

// roleFacts is the board's roles by uuid: each one's name and the specifications it produces.
func roleFacts(roles []map[string]interface{}) (map[string]string, map[string][]string) {
	names, answers := map[string]string{}, map[string][]string{}
	for _, r := range roles {
		u := str(r["uuid"])
		names[u] = str(r["name"])
		for _, o := range mapsOf(r["producesOutputs"]) {
			if s := str(o["specification"]); s != "" {
				answers[u] = append(answers[u], s)
			}
		}
	}
	return names, answers
}

// mineOf is the session's own record on a task as the watch reads it: its last sign-off, and the newest
// round of each specification it published.
func mineOf(t map[string]interface{}, session string) *watchMine {
	m := &watchMine{Rounds: map[string]string{}}
	for _, s := range mapsOf(t["signOffs"]) {
		if str(s["session"]) != session {
			continue
		}
		if at := str(s["signedOffAt"]); m.SignedOff == "" || timeOf(at).After(timeOf(m.SignedOff)) {
			m.SignedOff = at
		}
	}
	for _, d := range mapsOf(t["documents"]) {
		ref, _ := d["document"].(map[string]interface{})
		if ref == nil || str(ref["session"]) != session {
			continue
		}
		spec, at := str(ref["specification"]), str(d["createdDate"])
		if cur, ok := m.Rounds[spec]; !ok || timeOf(at).After(timeOf(cur)) {
			m.Rounds[spec] = at
		}
	}
	return m
}

// timeOf reads a server instant; the zero time when absent or unreadable.
func timeOf(v interface{}) time.Time {
	s, _ := v.(string)
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// watchStateExists says whether a watch's state file is there to read: a first run has none.
func watchStateExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func readWatchState(path string) watchKept {
	var s watchKept
	if path == "" {
		return s
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return s
	}
	if json.Unmarshal(b, &s) != nil {
		return watchKept{}
	}
	return s
}

func writeWatchState(path string, s watchKept) {
	if path == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	b, _ := json.Marshal(s)
	_ = os.WriteFile(path, b, 0o600)
}

func (cliWaitClient) scope(session, board string) ([]map[string]interface{}, []map[string]interface{}, error) {
	data, err := sendGraphQLRequest(watchScopeOp, map[string]interface{}{"sessionUuid": session, "boardUuid": board})
	if err != nil {
		return nil, nil, err
	}
	s, _ := data["sessionProgrammatic"].(map[string]interface{})
	if s == nil {
		return nil, nil, fmt.Errorf("session %s not found", session)
	}
	return mapsOf(s["tasksWorked"]), mapsOf(data["agentTaskRoleConfigsProgrammatic"]), nil
}

func (cliWaitClient) tasks(uuids []string) ([]map[string]interface{}, error) {
	data, err := sendGraphQLRequest(watchTasksOp, map[string]interface{}{"taskUuids": uuids})
	if err != nil {
		return nil, err
	}
	return mapsOf(data["agentTasksByUuidProgrammatic"]), nil
}
