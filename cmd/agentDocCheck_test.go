package cmd

import (
	"encoding/json"
	"strings"
	"testing"
)

func release(t *testing.T, js string) map[string]interface{} {
	t.Helper()
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(js), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestTheSummaryCountsAndNamesWhatBlocks(t *testing.T) {
	r := release(t, `{"uuid":"r","document":{"round":2,"checks":{"catalogueVersion":"2026-09.1","results":[
		{"check":"ids.family","result":"PASS","blocking":false},
		{"check":"trace.parent_exists","result":"FAIL","blocking":true,"offences":[{"elementId":"REQ-12","message":"REQ-12 → REQ-4 not found"}]},
		{"check":"tests.no_orphans","result":"FAIL","blocking":false,"offences":[{"elementId":"REQ-30","message":"requirement REQ-30 is verified by nothing"}]},
		{"check":"speculative.inputs_recorded","result":"SKIP","blocking":false,"reason":"no assignment"}]}}}`)
	got := checkSummary(r)
	if got[0] != "checks (2026-09.1): 1 pass, 2 fail, 1 skip (report round 2) — blocking: trace.parent_exists" {
		t.Errorf("head: %q", got[0])
	}
	joined := strings.Join(got, "\n")
	for _, want := range []string{"  FAIL trace.parent_exists [blocking]", "    REQ-12 → REQ-4 not found",
		"  FAIL tests.no_orphans\n", "sign-off will be refused until this passes"} {
		if !strings.Contains(joined+"\n", want) {
			t.Errorf("summary lacks %q:\n%s", want, joined)
		}
	}
}

func TestANonBlockingFailureDoesNotWarnOfTheSignOff(t *testing.T) {
	r := release(t, `{"document":{"round":1,"checks":{"catalogueVersion":"2026-09.1","results":[
		{"check":"tests.no_orphans","result":"FAIL","blocking":false,"offences":[{"message":"x"}]}]}}}`)
	joined := strings.Join(checkSummary(r), "\n")
	if strings.Contains(joined, "blocking") || strings.Contains(joined, "sign-off") {
		t.Errorf("a report-only failure reads as blocking:\n%s", joined)
	}
}

func TestNoReportIsSaid(t *testing.T) {
	if got := checkSummary(nil); len(got) != 1 || !strings.Contains(got[0], "no report") {
		t.Errorf("got %v", got)
	}
}

func TestTheTargetsAreTheNewestElementBearingDocumentOfEachTypeOnTheTask(t *testing.T) {
	task := release(t, `{"uuid":"t1","documents":[
		{"uuid":"a2","document":{"specification":"ARCHITECTURE","round":2,"task":"t1","elements":{"digest":"x"}}},
		{"uuid":"c1","document":{"specification":"CHECK_REPORT","round":1,"task":"t1"}},
		{"uuid":"a1","document":{"specification":"ARCHITECTURE","round":1,"task":"t1","elements":{"digest":"y"}}},
		{"uuid":"d1","document":{"specification":"DETAILED_DESIGN","round":1,"task":"t1"}},
		{"uuid":"o1","document":{"specification":"REQUIREMENTS","round":1,"task":"other","elements":{"digest":"z"}}}]}`)
	got := checkTargets(task)
	if len(got) != 1 || got[0]["uuid"] != "a2" {
		t.Errorf("targets: %v", got)
	}
}

func TestCheckTakesASessionAndOneOfReleaseOrTask(t *testing.T) {
	defer func(s, r, k string) { checkSession, checkRelease, checkTask = s, r, k }(checkSession, checkRelease, checkTask)
	for _, c := range []struct{ s, r, k, want string }{
		{"", "r", "", "--session is required"},
		{"s", "", "", "give --release"},
		{"s", "r", "t", "give --release"},
	} {
		checkSession, checkRelease, checkTask = c.s, c.r, c.k
		if err := runDocCheck(); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%+v: %v", c, err)
		}
	}
	if agentDocCheckCmd.Parent() != agentDocCmd {
		t.Error("check is not under `agent doc`")
	}
}

func TestPublishingACheckReportIsRefusedWithTheWayRound(t *testing.T) {
	defer func(s, ty string) { docSession, docType = s, ty }(docSession, docType)
	docSession, docType = "s", "check-report"
	err := runDocPublish()
	if err == nil || !strings.Contains(err.Error(), "rearm agent doc check") {
		t.Errorf("got %v", err)
	}
}
