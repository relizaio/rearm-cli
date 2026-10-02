package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	rearm "github.com/relizaio/rearm-client-go"
)

// The brief's next-round lines (task RD5-5): "next <TYPE>: <path>" for each type the role produces on the task,
// from the board's template or the server's default, {round} one more than the task's rounds of the type with a
// replaced version counting as its round, and the RD4-7 version case when this hop published the newest round.

func nextDoc(uuid, spec string, round int, version, lifecycle, path string) map[string]any {
	return map[string]any{"uuid": uuid, "version": version, "lifecycle": lifecycle,
		"document": map[string]any{"specification": spec, "round": round, "path": path, "task": "t-1"}}
}

// nextRoundsDocs: DETAILED_DESIGN round 1 in two versions (the newest first, as the server lists them) and a
// cancelled round that is not one; an ARCHITECTURE round the coder does not produce.
func nextRoundsDocs() []any {
	return []any{
		nextDoc("r-a1", "ARCHITECTURE", 1, "3", "ASSEMBLED", "boards/x/design/RD-1/architecture-1.md"),
		nextDoc("r-d1b", "DETAILED_DESIGN", 1, "5", "DRAFT", "boards/x/work/RD-1/notes-1.md"),
		nextDoc("r-d1a", "DETAILED_DESIGN", 1, "4", "ASSEMBLED", "boards/x/work/RD-1/notes-1.md"),
		nextDoc("r-d2x", "DETAILED_DESIGN", 2, "6", "CANCELLED", "boards/x/work/RD-1/notes-2.md"),
	}
}

func nextRoundsWorld(t *testing.T, hopPublished ...string) {
	t.Helper()
	withStateDir(t)
	st := &agentSessionState{SessionUuid: "s-1", ClientSessionId: "c-1"}
	if len(hopPublished) > 0 {
		st.HopOutputs = map[string]*hopOutputs{"t-1": {Outputs: hopPublished}}
	}
	if err := writeAgentState(st); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Query string `json:"query"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		data := map[string]any{}
		switch q := req.Query; {
		case strings.Contains(q, "AgentRoleBriefProgrammatic"):
			data["agentTaskRoleConfigsProgrammatic"] = []any{map[string]any{"name": "coder", "servedPrompt": "P",
				"producesOutputs": []any{
					map[string]any{"specification": "DETAILED_DESIGN", "scope": "TASK", "required": true},
					map[string]any{"specification": "BOARD_QUESTIONS", "scope": "COMPONENT"},
					map[string]any{"specification": "SOFTWARE_REQUIREMENTS", "scope": "COMPONENT"},
				}}}
		case strings.Contains(q, "AgentTaskProgrammatic"):
			data["agentTaskProgrammatic"] = map[string]any{"uuid": "t-1", "key": "RD-1", "board": "b-1", "status": "ASSIGNED",
				"role": "coder", "documents": nextRoundsDocs()}
		case strings.Contains(q, "AgentBoardProgrammatic"):
			data["agentBoardProgrammatic"] = map[string]any{"uuid": "b-1", "documentsRoot": "boards/x/",
				"documentsRepo": map[string]any{"uri": "github.com/acme/docs"},
				"documentPaths": map[string]any{"DETAILED_DESIGN": "work/{task}/notes-{round}.md"}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	t.Cleanup(srv.Close)
	c, err := rearm.New(srv.URL, "id", "secret", rearm.WithoutTokenExchange())
	if err != nil {
		t.Fatal(err)
	}
	apiClient, briefSession = c, "s-1"
	t.Cleanup(func() { apiClient, briefSession, briefJson = nil, "", false })
}

// A board template for one type, the server's default for the other; a COMPONENT-scoped type is not the task's.
func TestTheBriefPrintsTheNextRoundOfEachOutputType(t *testing.T) {
	nextRoundsWorld(t)
	out := runBrief(t)
	docs := out[strings.Index(out, "## 3. Its documents"):strings.Index(out, "## 4. The repository")]
	for _, want := range []string{
		"- next DETAILED_DESIGN: boards/x/work/RD-1/notes-2.md\n",
		"- next BOARD_QUESTIONS: boards/x/questions/RD-1/round-1.md\n",
	} {
		if !strings.Contains(docs, want) {
			t.Errorf("missing %q in\n%s", want, docs)
		}
	}
	if strings.Contains(out, "next SOFTWARE_REQUIREMENTS") || strings.Contains(out, "next ARCHITECTURE") {
		t.Errorf("a type the role does not produce on the task got a line:\n%s", docs)
	}
	if strings.Index(docs, "next DETAILED_DESIGN") > strings.Index(docs, "next BOARD_QUESTIONS") {
		t.Error("the lines follow the role's order")
	}
	if strings.Contains(out, "republish") {
		t.Errorf("nothing was published in this hop:\n%s", docs)
	}
}

// The RD4-7 version case: this hop published the newest round, so a publish at its path is a new version of it.
func TestARoundThisHopPublishedIsRepublishedAsANewVersion(t *testing.T) {
	nextRoundsWorld(t, "r-d1b")
	out := runBrief(t)
	if want := "- next DETAILED_DESIGN: boards/x/work/RD-1/notes-1.md (republish = new version of round 1)\n"; !strings.Contains(out, want) {
		t.Errorf("missing %q in\n%s", want, out)
	}
	if !strings.Contains(out, "- next BOARD_QUESTIONS: boards/x/questions/RD-1/round-1.md\n") {
		t.Errorf("the other type is unaffected:\n%s", out)
	}
}

// Only the newest version counts: a replaced version an earlier publish of this hop recorded is not the round
// a republish would replace, and a release of another hop is not this hop's.
func TestOnlyTheNewestVersionThisHopPublishedMakesAVersion(t *testing.T) {
	for _, published := range [][]string{{"r-d1a"}, {"r-other"}} {
		nextRoundsWorld(t, published...)
		if out := runBrief(t); !strings.Contains(out, "- next DETAILED_DESIGN: boards/x/work/RD-1/notes-2.md\n") {
			t.Errorf("published %v:\n%s", published, out)
		}
	}
}

func TestTheNextRoundsAsJson(t *testing.T) {
	nextRoundsWorld(t, "r-d1b")
	briefJson = true
	var b taskBrief
	if err := json.Unmarshal([]byte(runBrief(t)), &b); err != nil {
		t.Fatal(err)
	}
	want := []briefNextRound{
		{Type: "DETAILED_DESIGN", Path: "boards/x/work/RD-1/notes-1.md", Round: 1, Version: true},
		{Type: "BOARD_QUESTIONS", Path: "boards/x/questions/RD-1/round-1.md", Round: 1},
	}
	if !reflect.DeepEqual(b.NextRounds, want) {
		t.Errorf("nextRounds %+v, want %+v", b.NextRounds, want)
	}
}

func TestNextRoundCounting(t *testing.T) {
	docs := asList(nextRoundsDocs())
	cases := []struct {
		name      string
		outputs   []string
		templates map[string]interface{}
		docs      []map[string]interface{}
		published []string
		want      []briefNextRound
	}{
		{"no round yet is round 1, default well-known path", []string{"DETAILED_DESIGN"}, nil, nil, nil,
			[]briefNextRound{{Type: "DETAILED_DESIGN", Path: "r/impl/RD-1/notes-1.md", Round: 1}}},
		{"a replaced version counts as its round, a cancelled one not at all", []string{"DETAILED_DESIGN"}, nil, docs, nil,
			[]briefNextRound{{Type: "DETAILED_DESIGN", Path: "r/impl/RD-1/notes-2.md", Round: 2}}},
		{"a board template wins over the default", []string{"BOARD_TEST_REPORT"},
			map[string]interface{}{"BOARD_TEST_REPORT": "qa/{key}/r{round}.md"}, nil, nil,
			[]briefNextRound{{Type: "BOARD_TEST_REPORT", Path: "r/qa/RD-1/r1.md", Round: 1}}},
		{"any other type takes one file per round by its lower-case name", []string{"SOFTWARE_REQUIREMENTS"}, nil, nil, nil,
			[]briefNextRound{{Type: "SOFTWARE_REQUIREMENTS", Path: "r/design/RD-1/software_requirements-1.md", Round: 1}}},
		{"another task's round is not this task's", []string{"ARCHITECTURE"}, nil,
			[]map[string]interface{}{{"uuid": "x", "lifecycle": "ASSEMBLED",
				"document": map[string]interface{}{"specification": "ARCHITECTURE", "round": float64(1), "task": "t-9"}}}, nil,
			[]briefNextRound{{Type: "ARCHITECTURE", Path: "r/design/RD-1/architecture-1.md", Round: 1}}},
		{"an advisory round this hop recorded is not the hop's own round", []string{"ARCHITECTURE"}, nil,
			[]map[string]interface{}{{"uuid": "adv", "lifecycle": "ASSEMBLED",
				"document": map[string]interface{}{"specification": "ARCHITECTURE", "round": float64(1), "advisory": true,
					"path": "r/design/RD-1/architecture-1.md"}}}, []string{"adv"},
			[]briefNextRound{{Type: "ARCHITECTURE", Path: "r/design/RD-1/architecture-2.md", Round: 2}}},
	}
	for _, tc := range cases {
		if got := briefNextRounds(tc.outputs, tc.templates, "r/", "RD-1", "t-1", tc.docs, tc.published); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: got %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

func TestTheTaskOutputsAreTaskScopedTypesAndTheIndexTypes(t *testing.T) {
	role := map[string]interface{}{"producesOutputs": []interface{}{
		map[string]interface{}{"specification": "ARCHITECTURE", "scope": "TASK"},
		map[string]interface{}{"specification": "SOFTWARE_REQUIREMENTS", "scope": "COMPONENT"},
		map[string]interface{}{"specification": "BOARD_TEST_REPORT", "scope": "COMPONENT"},
		map[string]interface{}{"specification": "ARCHITECTURE", "scope": "TASK"},
	}}
	if got := briefTaskOutputs(role); !reflect.DeepEqual(got, []string{"ARCHITECTURE", "BOARD_TEST_REPORT"}) {
		t.Errorf("task outputs %v", got)
	}
}

// A release the server does not count as a round leaves the next round where it is (tester run 1, T-1): a
// PENDING release is the reservation while a round is being cut, and a REJECTED one was refused. Each case puts
// that release on round 2 over a settled round 1, so counting it would name round 3; and alone on round 1, so
// counting it would name round 2. Each case runs without and with the hop's recorded outputs naming it, so it
// counts neither as a round nor as the round the hop published.
func TestPendingAndRejectedReleasesAreNotRounds(t *testing.T) {
	settled := nextDoc("r-d1", "DETAILED_DESIGN", 1, "4", "ASSEMBLED", "r/impl/RD-1/notes-1.md")
	for _, lifecycle := range []string{"PENDING", "REJECTED"} {
		unsettled := func(round int) map[string]any {
			return nextDoc("r-u", "DETAILED_DESIGN", round, "5", lifecycle, "r/impl/RD-1/notes-x.md")
		}
		cases := []struct {
			name  string
			docs  []any
			round int
			path  string
		}{
			{"over a settled round 1", []any{unsettled(2), settled}, 2, "r/impl/RD-1/notes-2.md"},
			{"alone on round 1", []any{unsettled(1)}, 1, "r/impl/RD-1/notes-1.md"},
		}
		for _, tc := range cases {
			for _, published := range [][]string{nil, {"r-u"}} {
				got := briefNextRounds([]string{"DETAILED_DESIGN"}, nil, "r/", "RD-1", "t-1", asList(tc.docs), published)
				want := []briefNextRound{{Type: "DETAILED_DESIGN", Path: tc.path, Round: tc.round}}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("%s %s, hop outputs %v: got %+v, want %+v", lifecycle, tc.name, published, got, want)
				}
			}
		}
	}
}
