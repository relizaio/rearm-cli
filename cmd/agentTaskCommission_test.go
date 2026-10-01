package cmd

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// Task RD4-12: task commission sends an investigation's commission, reads its flags before sending, prints the
// investigation compactly, and the brief shows an investigation's block, its pins and the reports returned.

const commissionBoardUuid = "0b3f2a60-9c7e-4a44-8a53-7a0f3e0d9b21"

func TestCommissionVariables(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	files := func(name string) ([]byte, error) {
		if name == "brief.md" {
			return []byte("  How slow is the board page?\n"), nil
		}
		return nil, errors.New("no such file")
	}
	full := commissionOpts{session: "s-1", board: commissionBoardUuid, role: " tester ", title: " measure it ",
		briefFile: "brief.md", fromTask: "t-1", inputs: []string{"r-1", " ", "r-2"}, budget: "2.50", deadline: "48h",
		review: "lead", returnTo: "task", level: 1, levelSet: true, group: "ui", tags: []string{"perf"}}
	for _, c := range []struct {
		name    string
		opts    commissionOpts
		want    map[string]interface{}
		refused string
	}{
		{name: "every flag", opts: full, want: map[string]interface{}{"boardUuid": commissionBoardUuid, "role": "tester",
			"title": "measure it", "sessionUuid": "s-1", "brief": "How slow is the board page?", "fromTask": "t-1",
			"inputs": []string{"r-1", "r-2"}, "budgetMicros": int64(2_500_000), "deadline": "2026-10-02T12:00:00Z",
			"review": "lead", "returnTo": "TASK", "workLevel": 1, "group": "ui",
			"tags": []map[string]interface{}{{"key": "perf"}}}},
		{name: "a person, standalone", opts: commissionOpts{board: commissionBoardUuid, role: "researcher", title: "t",
			brief: "b", deadline: "2026-10-05T09:30:00+02:00"},
			want: map[string]interface{}{"boardUuid": commissionBoardUuid, "role": "researcher", "title": "t", "brief": "b",
				"deadline": "2026-10-05T07:30:00Z"}},
		{name: "days", opts: commissionOpts{board: commissionBoardUuid, role: "r", title: "t", deadline: "3d"},
			want: map[string]interface{}{"boardUuid": commissionBoardUuid, "role": "r", "title": "t",
				"deadline": "2026-10-03T12:00:00Z"}},
		{name: "no board", opts: commissionOpts{role: "r", title: "t"}, refused: "give --board"},
		{name: "no role", opts: commissionOpts{board: commissionBoardUuid, title: "t"}, refused: "give --role"},
		{name: "no title", opts: commissionOpts{board: commissionBoardUuid, role: "r"}, refused: "give --title"},
		{name: "a session without its task", opts: commissionOpts{session: "s", board: commissionBoardUuid, role: "r", title: "t"},
			refused: "give --from-task"},
		{name: "the brief twice", opts: commissionOpts{board: commissionBoardUuid, role: "r", title: "t", brief: "b",
			briefFile: "brief.md"}, refused: "--brief or --brief-file"},
		{name: "a missing brief file", opts: commissionOpts{board: commissionBoardUuid, role: "r", title: "t",
			briefFile: "gone.md"}, refused: "could not read --brief-file"},
		{name: "a bad budget", opts: commissionOpts{board: commissionBoardUuid, role: "r", title: "t", budget: "-1"},
			refused: "budget"},
		{name: "a bad deadline", opts: commissionOpts{board: commissionBoardUuid, role: "r", title: "t", deadline: "soon"},
			refused: "--deadline is an RFC 3339 time"},
		{name: "a bad return", opts: commissionOpts{board: commissionBoardUuid, role: "r", title: "t", returnTo: "home"},
			refused: "--return-to is TASK or NONE"},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, err := commissionVariables(c.opts, now, files)
			if c.refused != "" {
				if err == nil || !strings.Contains(err.Error(), c.refused) {
					t.Fatalf("want a refusal with %q, got %v, %v", c.refused, got, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got["input"], c.want) {
				t.Fatalf("got %#v\nwant %#v", got["input"], c.want)
			}
		})
	}
}

type commissionBoardFake struct {
	mu    sync.Mutex
	query string
	sent  map[string]any
}

func (f *commissionBoardFake) serve() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		data := map[string]any{}
		if strings.Contains(req.Query, "agentTaskCommissionProgrammatic(") {
			f.mu.Lock()
			f.query, f.sent = req.Query, req.Variables
			f.mu.Unlock()
			data["agentTaskCommissionProgrammatic"] = map[string]any{"uuid": "i-1", "key": "RD-13", "status": "QUEUED",
				"role": "tester", "kind": "INVESTIGATION", "investigation": map[string]any{"role": "tester", "review": nil,
					"returnTo": "TASK", "commissionedBy": map[string]any{"role": "architect", "task": "t-1"}}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
}

func TestTheCommissionIsSentAndPrintedCompactly(t *testing.T) {
	f := &commissionBoardFake{}
	srv := f.serve()
	defer srv.Close()
	useFake(t, srv)
	t.Cleanup(func() {
		commissionSession, commissionBoard, commissionRole, commissionTitle, commissionFromTask = "", "", "", "", ""
	})
	commissionSession, commissionBoard, commissionRole, commissionTitle, commissionFromTask =
		"s-arch", commissionBoardUuid, "tester", "measure it", "t-1"
	out := captureStdout(t, func() { agentTaskCommissionCmd.Run(agentTaskCommissionCmd, nil) })
	in, _ := f.sent["input"].(map[string]any)
	if in["sessionUuid"] != "s-arch" || in["role"] != "tester" || in["fromTask"] != "t-1" || in["boardUuid"] != commissionBoardUuid {
		t.Fatalf("the commission sends its input, got %v", f.sent)
	}
	if !strings.Contains(normalisedOp(f.query), "mutation AgentTaskCommissionProgrammatic ($input: AgentTaskCommissionInput!)") {
		t.Error("the operation is not the commission mutation")
	}
	for _, want := range []string{"RD-13 queued, role tester", "investigation: a report by tester",
		"its report comes back pinned on task t-1"} {
		if !strings.Contains(out, want) {
			t.Errorf("the compact output lacks %q: %q", want, out)
		}
	}
}

func TestTheCommissionVerbTakesItsFlagsAndATaskKey(t *testing.T) {
	for _, flag := range []string{"session", "board", "role", "title", "brief", "brief-file", "from-task", "input",
		"budget", "deadline", "review", "return-to", "work-level", "group", "tag", "json"} {
		if agentTaskCommissionCmd.Flags().Lookup(flag) == nil {
			t.Errorf("task commission has no --%s", flag)
		}
	}
	if agentTaskCommissionCmd.PreRunE == nil {
		t.Error("--from-task does not take a task key")
	}
}

func TestTheBriefShowsAnInvestigation(t *testing.T) {
	task := map[string]interface{}{"key": "RD-13", "title": "measure it", "status": "ASSIGNED", "role": "tester",
		"kind": "INVESTIGATION", "investigation": map[string]interface{}{"role": "tester", "review": "lead",
			"deadline": "2026-10-02T12:00:00Z", "returnTo": "TASK",
			"commissionedBy": map[string]interface{}{"role": "architect", "task": "t-1"}},
		"requiredInputs": []interface{}{map[string]interface{}{"kind": "DOCUMENT", "specification": "ARCHITECTURE", "release": "r-1"},
			map[string]interface{}{"kind": "DOCUMENT", "specification": "TEST_PLAN"}},
	}
	fields := briefTaskFields(task)
	if !reflect.DeepEqual(fields["pinnedInputs"], []string{"ARCHITECTURE r-1"}) {
		t.Errorf("the brief lists only the pinned releases, got %v", fields["pinnedInputs"])
	}
	out := renderTaskBrief(&taskBrief{Key: "RD-13", Role: "tester", Task: fields})
	for _, want := range []string{"investigation: a report by tester, reviewed by lead, due 2026-10-02T12:00:00Z",
		"Deliver a BOARD_INVESTIGATION_REPORT and no code", "- pinned inputs: ARCHITECTURE r-1"} {
		if !strings.Contains(out, want) {
			t.Errorf("the brief lacks %q", want)
		}
	}
	back := briefTaskFields(map[string]interface{}{"key": "RD-12", "reportsReturned": []interface{}{
		map[string]interface{}{"investigation": "i-1", "investigationKey": "RD-13", "report": "rep-1"}}})
	if !reflect.DeepEqual(back["reportsReturned"], []string{"RD-13: report rep-1 pinned"}) {
		t.Errorf("a commissioning task lists the reports returned, got %v", back["reportsReturned"])
	}
	cancelled := briefTaskFields(map[string]interface{}{"key": "RD-12", "reportsReturned": []interface{}{
		map[string]interface{}{"investigation": "i-1", "investigationKey": "RD-13", "report": "rep-1", "cancelled": false},
		map[string]interface{}{"investigation": "i-2", "investigationKey": "RD-14", "report": nil, "cancelled": true,
			"note": "not needed after all"}}})
	if !reflect.DeepEqual(cancelled["reportsReturned"], []string{"RD-13: report rep-1 pinned",
		"RD-14: cancelled, no report; this task no longer waits on it (not needed after all)"}) {
		t.Errorf("a cancelled investigation is listed among the reports returned, with its note, got %v", cancelled["reportsReturned"])
	}
	if out := renderTaskBrief(&taskBrief{Key: "RD-12", Role: "architect", Task: cancelled}); !strings.Contains(out,
		"- reports returned: RD-13: report rep-1 pinned; RD-14: cancelled, no report; this task no longer waits on it (not needed after all)") {
		t.Errorf("the brief lacks the cancelled investigation:\n%s", out)
	}
	if _, ok := briefTaskFields(map[string]interface{}{"key": "RD-1", "kind": "WORK"})["investigation"]; ok {
		t.Error("a work task has no investigation line")
	}
}
