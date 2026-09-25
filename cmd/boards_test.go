package cmd

import (
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestOnlyAPersonsCredentialPasses(t *testing.T) {
	for _, c := range []struct {
		mode, key string
		ok        bool
		mentions  string
	}{
		{authSession, "", true, ""},
		{authKey, "USER__7f3c__ord__a1", true, ""},
		{authKey, "FREEFORM__4324b741__ord__2", false, "FREEFORM key"},
		{authKey, "ORGANIZATION_RW__4324b741", false, "ORGANIZATION_RW key"},
		{authKey, "", false, "no credential"},
		{authGitHubOIDC, "", false, "CI identity"},
	} {
		p := personCredentialProblem(c.mode, c.key)
		if c.ok && p != "" {
			t.Errorf("%s %q refused: %s", c.mode, c.key, p)
		}
		if !c.ok {
			if !strings.Contains(p, "rearm login") || !strings.Contains(p, c.mentions) {
				t.Errorf("%s %q: refusal %q should say rearm login and %q", c.mode, c.key, p, c.mentions)
			}
		}
	}
}

func TestNoBoardsVerbTakesASession(t *testing.T) {
	for _, c := range boardsCmd.Commands() {
		if c.Flags().Lookup("session") != nil || c.PersistentFlags().Lookup("session") != nil {
			t.Errorf("boards %s takes --session", c.Name())
		}
	}
	want := []string{"list", "tasks", "register", "authorize", "order", "complete", "cancel", "decide", "answer",
		"review", "signoff", "hold", "release", "require-review", "strength", "lock", "unlock", "apply"}
	for _, name := range want {
		found := false
		for _, c := range boardsCmd.Commands() {
			if c.Name() == name {
				found = true
			}
		}
		if !found {
			t.Errorf("rearm boards has no %s", name)
		}
	}
	if boardsCmd.Parent() != rootCmd {
		t.Error("boards is not a top-level group")
	}
}

func TestRequiredFlags(t *testing.T) {
	for cmd, flags := range map[*cobra.Command][]string{
		boardsRegisterCmd: {"title"}, boardsAuthorizeCmd: {"role"}, boardsOrderCmd: {"order"},
		boardsDecideCmd: {"spec"}, boardsSignoffCmd: {"outcome"}, boardsHoldCmd: {"reason"}, boardsLockCmd: {"reason"},
	} {
		for _, name := range flags {
			f := cmd.Flags().Lookup(name)
			if f == nil {
				t.Fatalf("%s has no --%s", cmd.Name(), name)
			}
			if _, ok := f.Annotations[cobra.BashCompOneRequiredFlag]; !ok {
				t.Errorf("%s --%s is not required", cmd.Name(), name)
			}
		}
	}
}

func TestRegisterSendsOnlyWhatWasGiven(t *testing.T) {
	got := registerVariables("b-1", "fix it", "#12", "", "", 0)
	want := map[string]interface{}{"boardUuid": "b-1", "input": map[string]interface{}{"title": "fix it", "externalRef": "#12"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestAuthorizeSendsTheOrderOnlyWhenSet(t *testing.T) {
	if _, ok := authorizeVariables("t", "coder", 0, false, nil, 0)["orderIndex"]; ok {
		t.Error("orderIndex sent without --order")
	}
	got := authorizeVariables("t", "coder", 0, true, []string{"d-1"}, 2)
	want := map[string]interface{}{"taskUuid": "t", "role": "coder", "orderIndex": 0, "dependsOn": []string{"d-1"}, "level": 2}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestSkippingRequiredRolesNeedsANote(t *testing.T) {
	if _, err := completeVariables("t", "", true); err == nil {
		t.Error("skip without a note accepted")
	}
	got, err := completeVariables("t", "shipped by hand", true)
	if err != nil || got["skipRequiredRoles"] != true || got["note"] != "shipped by hand" {
		t.Errorf("got %v, %v", got, err)
	}
}

func TestDecideBuildsEachKindOfDecision(t *testing.T) {
	got, err := decideVariables("t", "review_findings", []string{"R-3=not reachable"}, []string{"R-4=known, tracked"},
		[]string{"R-5=2", "1"}, []string{"missing null check"}, "architecture", "")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]interface{}{"taskUuid": "t", "specification": "REVIEW_FINDINGS",
		"decisions": []map[string]interface{}{
			{"action": "DISMISS", "findingId": "R-3", "resolution": "not reachable"},
			{"action": "ACCEPT", "findingId": "R-4", "resolution": "known, tracked"},
			{"action": "SET_PRIORITY", "findingId": "R-5", "priority": 2},
			{"action": "FILE", "title": "missing null check", "priority": 1},
		},
		"about": map[string]interface{}{"specification": "ARCHITECTURE"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
}

func TestDecideRefusesWhatTheServerWould(t *testing.T) {
	for name, c := range map[string]struct {
		spec                            string
		dismiss, accept, priority, file []string
	}{
		"spec":            {"ARCHITECTURE", []string{"R-1=x"}, nil, nil, nil},
		"nothing":         {"TEST_REPORT", nil, nil, nil, nil},
		"no reason":       {"TEST_REPORT", []string{"R-1"}, nil, nil, nil},
		"file unranked":   {"TEST_REPORT", nil, nil, nil, []string{"t"}},
		"stray priority":  {"TEST_REPORT", nil, nil, []string{"2"}, nil},
		"priority not N":  {"TEST_REPORT", nil, nil, []string{"R-1=high"}, nil},
		"two bare levels": {"TEST_REPORT", nil, nil, []string{"1", "2"}, []string{"t"}},
	} {
		if _, err := decideVariables("t", c.spec, c.dismiss, c.accept, c.priority, c.file, "", ""); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestAnswerOneOrAll(t *testing.T) {
	got, err := answerVariables("t", "q1", "use the v2 API", true, "", true)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]interface{}{"taskUuid": "t", "releaseHold": false,
		"answers": []map[string]interface{}{{"id": "q1", "status": "WITHDRAWN", "resolution": "use the v2 API"}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if got, _ := answerVariables("t", "", "", false, "yes to all", false); got["answerAll"] != "yes to all" {
		t.Errorf("--all not sent: %v", got)
	}
	for _, bad := range [][]string{{"q1", "", ""}, {"", "", ""}, {"q1", "x", "y"}} {
		if _, err := answerVariables("t", bad[0], bad[1], false, bad[2], false); err == nil {
			t.Errorf("accepted %v", bad)
		}
	}
}

func TestReviewTakesOneVerdictAndFindingsOnlyWithReject(t *testing.T) {
	if _, err := reviewVariables("t", true, true, "", nil, nil, "", ""); err == nil {
		t.Error("both verdicts accepted")
	}
	if _, err := reviewVariables("t", false, false, "", nil, nil, "", ""); err == nil {
		t.Error("no verdict accepted")
	}
	if _, err := reviewVariables("t", true, false, "", []string{"t"}, []string{"1"}, "", ""); err == nil {
		t.Error("findings with --approve accepted")
	}
	got, err := reviewVariables("t", false, true, "not yet", []string{"no rollback plan"}, []string{"1"}, "ARCHITECTURE", "r-9")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]interface{}{"taskUuid": "t", "approve": false, "note": "not yet",
		"findings": []map[string]interface{}{{"action": "FILE", "title": "no rollback plan", "priority": 1}},
		"about":    map[string]interface{}{"specification": "ARCHITECTURE", "release": "r-9"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
}

func TestStrengthSetsOrClears(t *testing.T) {
	got, err := strengthVariables("t", "6.5", false)
	if err != nil || got["requiredStrength"] != 6.5 {
		t.Errorf("got %v, %v", got, err)
	}
	got, err = strengthVariables("t", "", true)
	if v, ok := got["requiredStrength"]; err != nil || !ok || v != nil {
		t.Errorf("--clear should send null: %v, %v", got, err)
	}
	for _, bad := range []struct {
		v     string
		clear bool
	}{{"", false}, {"-1", false}, {"strong", false}, {"6", true}} {
		if _, err := strengthVariables("t", bad.v, bad.clear); err == nil {
			t.Errorf("accepted %v", bad)
		}
	}
}
