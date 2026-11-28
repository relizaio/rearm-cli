package cmd

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	rearm "github.com/relizaio/rearm-client-go"
)

// addartifact's import result, summary and exit; the refusal of addrelease, addreleases,
// addodeliverable and getversion; help; the command-line-only --allow-import-failure (task SAFE-16,
// design SAFE-9 3.4a to 3.10, test list 4.3 r1 to r24).

const (
	failedOneOfOne   = "import check failed: 1 of 1 VEX/SAFETY_CONTEXT artifacts imported nothing or have no known result; the artifacts are stored (exit 3). Pass --allow-import-failure to accept this."
	acceptedOneOfOne = "import check: 1 of 1 VEX/SAFETY_CONTEXT artifacts imported nothing or have no known result; accepted by --allow-import-failure"
	passedOneOfOne   = "import check passed: 1 of 1 VEX/SAFETY_CONTEXT artifacts imported"
	staleSbom        = "Cannot import the document: release has no SBOM components yet"
	refusedCallLine  = "no import result is reported: the server refused the call; parts it processed before the refusal may be stored"
	unknownUpload    = "the upload may or may not have been stored; no import result is known"
	sbomHint         = "hint: upload the SBOM first; pass --wait-for-sbom 10m to retry until it is indexed"
	noFAWNote        = "  note: the uploading key does not hold Finding Analysis write at the import scope; these statements wait for review"
)

func counts(total, aa, staged, unchanged, unmatched, errored, ar, deferred, awaiting string) string {
	return fmt.Sprintf("  total=%s auto-accepted=%s staged=%s unchanged=%s unmatched=%s errored=%s auto-rejected=%s attestations-deferred=%s awaiting-sbom=%s",
		total, aa, staged, unchanged, unmatched, errored, ar, deferred, awaiting)
}

// oneEntry runs addartifact with one entry of typ in --artifacts against a server answering the
// probe with all current fields and the upload with summary.
func oneEntry(t *testing.T, typ string, summary any, more ...string) (childResult, *fakeServer) {
	t.Helper()
	dir := t.TempDir()
	f := importServer(t, probeTypes{ImportSummary: currentSummaryFields}, fakeAnswer{summaries: map[string]any{"release/0": summary}})
	args := addArtifactArgs(append([]string{"--artifacts", jsonArg(t, []any{art(artFile(t, dir, "doc.json"), typ, "d")})}, more...)...)
	return runImportChild(t, f.srv.URL, "addartifact", args, childOpts{}), f
}

// r1
func TestNoImportableEntryBehavesAsBefore(t *testing.T) {
	dir := t.TempDir()
	f := newFakeServer(t, func(int, recordedRequest) (int, string) {
		return 200, dataBody(map[string]any{"addArtifactProgrammatic": map[string]any{"uuid": "rel-1", "version": "1.0.0", "lifecycle": "ASSEMBLED", "artifacts": []any{"a-1"}}})
	})
	res := runImportChild(t, f.srv.URL, "addartifact", addArtifactArgs("--artifacts", jsonArg(t, []any{art(artFile(t, dir, "bom.json"), "BOM", "b")})), childOpts{})
	wantExit(t, res, 0)
	reqs, _ := f.recorded()
	if len(reqs) != 1 || reqs[0].OperationName != "AddArtifactProgrammatic" {
		t.Fatalf("want one legacy upload and no probe, got %+v", reqs)
	}
	if res.stdout != lastUploadEnvelope(t, f) || res.stderr != "" {
		t.Errorf("stdout must be the envelope and stderr empty: %s", res)
	}
}

// r2
func TestStagedStatementsImportWithTheNote(t *testing.T) {
	res, f := oneEntry(t, "VEX", sum("statementsTotal", 12, "proposalsCreated", 9, "proposalsAutoAccepted", 0, "proposalsAutoRejected", 0,
		"attestationsDeferred", 0, "statementsUnmatched", 3, "statementsErrored", 0, "statementsUnchanged", nil, "errorMessages", []any{},
		"demotions", []any{map[string]any{"reason": "NO_FINDING_ANALYSIS_WRITE", "count": 9}}, "overrideOutcome", "NONE"))
	wantExit(t, res, 0)
	reqs, _ := f.recorded()
	if len(reqs) != 2 || reqs[0].OperationName != "ImportSummaryProbe" || reqs[1].OperationName != "AddArtifactProgrammaticWithImports" {
		t.Fatalf("want the probe then the upload, got %+v", reqs)
	}
	if probeName(t, reqs[0]) != rearm.ImportSummaryTypeName || strings.Count(reqs[0].Query, "__type") != 1 {
		t.Fatalf("want one probe for ImportSummary with one __type, got %s %s", reqs[0].Variables, reqs[0].Query)
	}
	if strings.Contains(reqs[1].Query, "inboundDeliverableDetails") || strings.Contains(reqs[1].Query, "sourceCodeEntryDetails") || strings.Contains(reqs[1].Query, "statementsAwaitingReconcile") {
		t.Errorf("selection: %s", reqs[1].Query)
	}
	if res.stdout != lastUploadEnvelope(t, f) {
		t.Errorf("stdout must be only the envelope: %s", res)
	}
	wantLines(t, res, []string{
		`import VEX --artifacts[0] "d" artifact ` + detailUuid(1) + ": IMPORTED",
		counts("12", "0", "9", "-", "3", "0", "0", "0", "-"),
		"  staged-by-reason: NO_FINDING_ANALYSIS_WRITE=9",
		noFAWNote,
		passedOneOfOne,
	})
}

// r3
func TestARefusedSafetyContextExitsThree(t *testing.T) {
	summary := sum("statementsTotal", 1, "proposalsCreated", 0, "proposalsAutoAccepted", 0, "proposalsAutoRejected", 0, "attestationsDeferred", 0,
		"statementsUnmatched", 0, "statementsErrored", 1, "statementsUnchanged", 0, "errorMessages", []any{"the document is not CycloneDX 2.x"},
		"demotions", []any{}, "overrideOutcome", "NONE")
	block := []string{
		`import SAFETY_CONTEXT --artifacts[0] "d" artifact ` + detailUuid(1) + ": NOTHING IMPORTED: statements errored",
		counts("1", "0", "0", "0", "0", "1", "0", "0", "-"),
		`  message: "the document is not CycloneDX 2.x"`,
	}
	res, _ := oneEntry(t, "SAFETY_CONTEXT", summary)
	wantExit(t, res, 3)
	wantLines(t, res, append(append([]string{}, block...), failedOneOfOne))
	res, _ = oneEntry(t, "SAFETY_CONTEXT", summary, "--allow-import-failure")
	wantExit(t, res, 0)
	wantLines(t, res, append(append([]string{}, block...), acceptedOneOfOne))
}

// r4, r5, r6
func TestOutcomeByCounts(t *testing.T) {
	for _, c := range []struct {
		typ     string
		summary any
		outcome string
		exit    int
	}{
		{"VEX", sum("statementsTotal", 0, "statementsErrored", 0, "errorMessages", []any{"unreadable"}), "NOTHING IMPORTED: the server read no statement from the document", 3},
		{"SAFETY_CONTEXT", sum("statementsUnchanged", 5), "IMPORTED", 0},
		{"VEX", sum("statementsUnmatched", 4, "proposalsCreated", 0), "NOTHING IMPORTED: no statement matched a component in the release's SBOM", 3},
	} {
		res, _ := oneEntry(t, c.typ, c.summary)
		wantExit(t, res, c.exit)
		if lines := res.stderrLines(); len(lines) == 0 || !strings.HasSuffix(lines[0], ": "+c.outcome) {
			t.Errorf("%v: want %q, got %s", c.summary, c.outcome, res)
		}
	}
}

// r7
func TestReleaseAndDeliverableEntriesAreAttributed(t *testing.T) {
	dir := t.TempDir()
	f := importServer(t, probeTypes{ImportSummary: currentSummaryFields}, fakeAnswer{
		summaries: map[string]any{
			"release/0":       sum("proposalsAutoAccepted", 2),
			"deliverable/0/0": sum("statementsTotal", 1, "statementsErrored", 1),
		},
		outbound: map[int]bool{1: true},
	})
	release := jsonArg(t, []any{art(artFile(t, dir, "vex.json"), "VEX", "rv")})
	deliverables := jsonArg(t, []any{
		map[string]any{"deliverable": "D-IN", "artifacts": []any{art(artFile(t, dir, "sc.json"), "SAFETY_CONTEXT", "ds")}},
		map[string]any{"deliverable": "d-out", "artifacts": []any{art(artFile(t, dir, "dvex.json"), "VEX", "dv")}},
	})
	res := runImportChild(t, f.srv.URL, "addartifact", addArtifactArgs("--releasearts", release, "--deliverablearts", deliverables), childOpts{})
	wantExit(t, res, 3)
	reqs, _ := f.recorded()
	q := uploads(reqs)[0].Query
	if !strings.Contains(q, "inboundDeliverableDetails") || !strings.Contains(q, "outboundDeliverableDetails") || strings.Contains(q, "sourceCodeEntryDetails") {
		t.Errorf("selection: %s", q)
	}
	lines := res.stderrLines()
	want := map[string]string{
		`import VEX --releasearts[0] "rv" artifact ` + detailUuid(1) + ": IMPORTED":                                                                                 "",
		`import SAFETY_CONTEXT --deliverablearts[0].artifacts[0] "ds" artifact ` + detailUuid(2) + ": NOTHING IMPORTED: statements errored":                         "",
		`import VEX --deliverablearts[1].artifacts[0] "dv" artifact ` + detailUuid(3) + ": NO RESULT: the server stored the artifact but recorded no import result": "",
	}
	var headers []string
	for _, l := range lines {
		if strings.HasPrefix(l, "import ") && !strings.HasPrefix(l, "import check") {
			headers = append(headers, l)
		}
	}
	if len(headers) != 3 {
		t.Fatalf("want three blocks, got %s", res)
	}
	for _, h := range headers {
		if _, ok := want[h]; !ok {
			t.Errorf("unexpected block header %q\n%s", h, res)
		}
	}
	if !strings.Contains(headers[0], "--releasearts[0]") || !strings.Contains(headers[1], "--deliverablearts[0]") || !strings.Contains(headers[2], "--deliverablearts[1]") {
		t.Errorf("blocks out of input order: %q", headers)
	}
	if lines[len(lines)-1] != "import check failed: 2 of 3 VEX/SAFETY_CONTEXT artifacts imported nothing or have no known result; the artifacts are stored (exit 3). Pass --allow-import-failure to accept this." {
		t.Errorf("final line: %s", res)
	}
}

// r8
func TestAttributionIsByTheCheckTagOnly(t *testing.T) {
	dir := t.TempDir()
	vex := artFile(t, dir, "vex.json")
	release := jsonArg(t, []any{art(vex, "VEX", "same")})
	deliverable := jsonArg(t, []any{map[string]any{"deliverable": "d-1", "artifacts": []any{art(vex, "VEX", "same")}}})
	imported, errored := sum("proposalsAutoAccepted", 3), sum("statementsTotal", 2, "statementsErrored", 2)
	release0 := func(a map[string]any) map[string]any { return a["artifactDetails"].([]any)[0].(map[string]any) }
	cases := []struct {
		name   string
		flag   string
		input  string
		mutate func(a map[string]any)
		exit   int
		want   string
	}{
		{"another upload of the same document after this one", "--releasearts", release, func(a map[string]any) {
			a["artifactDetails"] = append(a["artifactDetails"].([]any), map[string]any{"uuid": detailUuid(9), "type": "VEX", "displayIdentifier": "same",
				"tags": []any{map[string]any{"key": "fileName", "value": "vex.json"}}, "importSummary": errored})
		}, 0, `import VEX --releasearts[0] "same" artifact ` + detailUuid(1) + ": IMPORTED"},
		{"lost update: an earlier run's upload, another check value", "--deliverablearts", deliverable, func(a map[string]any) {
			d := a["inboundDeliverableDetails"].([]any)[0].(map[string]any)
			d["artifactDetails"] = []any{map[string]any{"uuid": detailUuid(8), "type": "VEX", "displayIdentifier": "same",
				"tags": []any{map[string]any{"key": importCheckTagKey, "value": strings.Repeat("0", 32)}}, "importSummary": imported}}
		}, 3, `import VEX --deliverablearts[0].artifacts[0] "same": NOT FOUND IN ANSWER: the artifact could not be identified in the server's answer`},
		{"two details carry the value", "--releasearts", release, func(a map[string]any) {
			dup := map[string]any{}
			for k, v := range release0(a) {
				dup[k] = v
			}
			dup["uuid"] = detailUuid(7)
			a["artifactDetails"] = append(a["artifactDetails"].([]any), dup)
		}, 3, `import VEX --releasearts[0] "same": NOT FOUND IN ANSWER: the artifact could not be identified in the server's answer`},
		{"the detail carrying the value is a BOM", "--releasearts", release, func(a map[string]any) {
			release0(a)["type"] = "BOM"
		}, 3, `import VEX --releasearts[0] "same": NOT FOUND IN ANSWER: the artifact could not be identified in the server's answer`},
		{"the deliverable is in neither list", "--deliverablearts", deliverable, func(a map[string]any) {
			a["inboundDeliverableDetails"] = []any{}
		}, 3, `import VEX --deliverablearts[0].artifacts[0] "same": NOT FOUND IN ANSWER: the artifact could not be identified in the server's answer`},
	}
	for _, c := range cases {
		f := importServer(t, probeTypes{ImportSummary: currentSummaryFields}, fakeAnswer{
			summaries: map[string]any{"release/0": imported, "deliverable/0/0": imported}, mutate: c.mutate})
		res := runImportChild(t, f.srv.URL, "addartifact", addArtifactArgs(c.flag, c.input), childOpts{})
		if res.exit != c.exit {
			t.Errorf("%s: want exit %d, got %s", c.name, c.exit, res)
			continue
		}
		if lines := res.stderrLines(); len(lines) == 0 || lines[0] != c.want {
			t.Errorf("%s: want %q first, got %s", c.name, c.want, res)
		}
	}
}

// r9
func TestAnAnswerWithDataAndErrorsIsGated(t *testing.T) {
	dir := t.TempDir()
	var dataJSON string
	f := newFakeServer(t, func(n int, r recordedRequest) (int, string) {
		if r.OperationName == rearm.ImportSummaryProbeOperationName {
			return answerProbe(t, r, probeTypes{ImportSummary: currentSummaryFields})
		}
		a := fakeAnswer{summaries: map[string]any{"release/0": sum("proposalsAutoAccepted", 1)}}.build(t, r)
		a["inboundDeliverableDetails"] = []any{}
		data := map[string]any{"addArtifactProgrammatic": a}
		b, _ := json.Marshal(data)
		dataJSON = string(b)
		return 200, errorsBody(data, "Artifact x of deliverable d-1 was not found")
	})
	release := jsonArg(t, []any{art(artFile(t, dir, "vex.json"), "VEX", "rv")})
	deliverable := jsonArg(t, []any{map[string]any{"deliverable": "d-1", "artifacts": []any{art(artFile(t, dir, "dvex.json"), "VEX", "dv")}}})
	res := runImportChild(t, f.srv.URL, "addartifact", addArtifactArgs("--releasearts", release, "--deliverablearts", deliverable), childOpts{})
	wantExit(t, res, 3)
	if res.stdout != `{"data":`+dataJSON+"}\n" {
		t.Errorf("stdout must be the envelope: %s", res)
	}
	wantLines(t, res, []string{
		`warning: the server answered with an error after storing the upload: "Artifact x of deliverable d-1 was not found"`,
		`import VEX --releasearts[0] "rv" artifact ` + detailUuid(1) + ": IMPORTED",
		counts("-", "1", "-", "-", "-", "-", "-", "-", "-"),
		`import VEX --deliverablearts[0].artifacts[0] "dv": NOT FOUND IN ANSWER: the artifact could not be identified in the server's answer`,
		"import check failed: 1 of 2 VEX/SAFETY_CONTEXT artifacts imported nothing or have no known result; the artifacts are stored (exit 3). Pass --allow-import-failure to accept this.",
	})
}

// r10
func TestAServerWithoutSummariesIsNotReported(t *testing.T) {
	dir := t.TempDir()
	for _, allow := range []bool{false, true} {
		f := newFakeServer(t, func(n int, r recordedRequest) (int, string) {
			if r.OperationName == rearm.ImportSummaryProbeOperationName {
				return answerProbe(t, r, probeTypes{})
			}
			return 200, dataBody(map[string]any{"addArtifactProgrammatic": map[string]any{"uuid": "rel-1"}})
		})
		args := addArtifactArgs("--artifacts", jsonArg(t, []any{art(artFile(t, dir, "vex.json"), "VEX", "v")}))
		want, exit := failedOneOfOne, 3
		if allow {
			args, want, exit = append(args, "--allow-import-failure"), acceptedOneOfOne, 0
		}
		res := runImportChild(t, f.srv.URL, "addartifact", args, childOpts{})
		wantExit(t, res, exit)
		wantLines(t, res, []string{`import VEX --artifacts[0] "v": NOT REPORTED: this server does not report import results`, want})
		reqs, _ := f.recorded()
		if len(reqs) != 3 || probeName(t, reqs[0]) != rearm.ImportSummaryTypeName || probeName(t, reqs[1]) != rearm.VexImportSummaryTypeName ||
			reqs[2].OperationName != "AddArtifactProgrammatic" {
			t.Errorf("want the probes for ImportSummary and VexImportSummary, then a legacy upload, got %+v", reqs)
		}
	}
}

// r11
func TestAServerWithOnlyVexImportSummary(t *testing.T) {
	dir := t.TempDir()
	f := importServer(t, probeTypes{VexImportSummary: vexSummaryFields}, fakeAnswer{field: rearm.VexImportSummaryField, summaries: map[string]any{
		"release/0": sum("statementsTotal", 2, "proposalsAutoAccepted", 2, "proposalsCreated", 0, "proposalsAutoRejected", 0,
			"attestationsDeferred", 0, "statementsUnmatched", 0, "statementsErrored", 0, "errorMessages", []any{})}})
	res := runImportChild(t, f.srv.URL, "addartifact", addArtifactArgs("--artifacts", jsonArg(t, []any{art(artFile(t, dir, "vex.json"), "VEX", "v")})), childOpts{})
	wantExit(t, res, 0)
	reqs, _ := f.recorded()
	if len(reqs) != 3 || reqs[0].OperationName != rearm.ImportSummaryProbeOperationName || probeName(t, reqs[0]) != rearm.ImportSummaryTypeName ||
		reqs[1].OperationName != rearm.ImportSummaryProbeOperationName || probeName(t, reqs[1]) != rearm.VexImportSummaryTypeName {
		t.Fatalf("want the probes for ImportSummary and VexImportSummary, then the upload, got %+v", reqs)
	}
	if q := uploads(reqs)[0].Query; !strings.Contains(q, " vexImportSummary {") || strings.Contains(q, " importSummary {") {
		t.Errorf("selection: %s", q)
	}
	wantLines(t, res, []string{
		`import VEX --artifacts[0] "v" artifact ` + detailUuid(1) + ": IMPORTED",
		counts("2", "2", "0", "-", "0", "0", "0", "0", "-"),
		passedOneOfOne,
	})
}

// r12
func TestAProbeTransportErrorUploadsNothing(t *testing.T) {
	dir := t.TempDir()
	closed := httptest.NewServer(nil)
	url := closed.URL
	closed.Close()
	res := runImportChild(t, url, "addartifact", addArtifactArgs("--artifacts", jsonArg(t, []any{art(artFile(t, dir, "vex.json"), "VEX", "v")})), childOpts{})
	wantExit(t, res, 1)
	lines := res.stderrLines()
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "Error: could not read which import result fields this server reports: ") || !strings.HasSuffix(lines[0], "; nothing was uploaded") {
		t.Errorf("probe error line: %s", res)
	}
}

// r13, r14
func TestARefusedOrFailedUploadAddsItsLine(t *testing.T) {
	dir := t.TempDir()
	input := jsonArg(t, []any{art(artFile(t, dir, "vex.json"), "VEX", "v")})
	f := newFakeServer(t, func(n int, r recordedRequest) (int, string) {
		if r.OperationName == rearm.ImportSummaryProbeOperationName {
			return answerProbe(t, r, probeTypes{ImportSummary: currentSummaryFields})
		}
		return 200, errorsBody(map[string]any{"addArtifactProgrammatic": nil}, "Not authorized")
	})
	res := runImportChild(t, f.srv.URL, "addartifact", addArtifactArgs("--artifacts", input), childOpts{})
	wantExit(t, res, 1)
	if res.stdout != "{\"data\":{\"addArtifactProgrammatic\":null}}\nGraphQL returned errors:\n- Not authorized\n" {
		t.Errorf("stdout must be as before: %s", res)
	}
	wantLines(t, res, []string{refusedCallLine})

	f = newFakeServer(t, func(n int, r recordedRequest) (int, string) {
		if r.OperationName == rearm.ImportSummaryProbeOperationName {
			return answerProbe(t, r, probeTypes{ImportSummary: currentSummaryFields})
		}
		return 502, "bad gateway"
	})
	res = runImportChild(t, f.srv.URL, "addartifact", addArtifactArgs("--artifacts", input), childOpts{})
	wantExit(t, res, 1)
	if res.stdout != "Error: rearm: request failed with status 502: bad gateway\n" {
		t.Errorf("stdout must be as before: %s", res)
	}
	wantLines(t, res, []string{unknownUpload})
}

// r15
func TestHostileValuesPrintOnOneLine(t *testing.T) {
	dir := t.TempDir()
	messages := []any{strings.Repeat("m", 1000), "p\rq\u202er"}
	for i := 0; i < 23; i++ {
		messages = append(messages, fmt.Sprintf("m%d", i))
	}
	f := importServer(t, probeTypes{ImportSummary: currentSummaryFields}, fakeAnswer{
		summaries: map[string]any{"release/0": sum("proposalsAutoAccepted", 1, "errorMessages", messages,
			"demotions", []any{map[string]any{"reason": "bad reason", "count": 1}})},
		mutate: func(a map[string]any) { a["artifactDetails"].([]any)[0].(map[string]any)["uuid"] = "x" },
	})
	input := jsonArg(t, []any{art(artFile(t, dir, "vex.json"), "VEX", "a\nb\u001b[2Jc")})
	res := runImportChild(t, f.srv.URL, "addartifact", addArtifactArgs("--artifacts", input), childOpts{})
	wantExit(t, res, 0)
	lines := res.stderrLines()
	if lines[0] != `import VEX --artifacts[0] "a\nb\x1b[2Jc" artifact "x": IMPORTED` {
		t.Errorf("header: %q", lines[0])
	}
	if lines[2] != `  staged-by-reason: "bad reason"=1` {
		t.Errorf("reason: %q", lines[2])
	}
	if lines[3] != `  message: "`+strings.Repeat("m", 300)+`" (cut, 1000 characters)` || lines[4] != `  message: "p\rq\u202er"` {
		t.Errorf("messages: %q %q", lines[3], lines[4])
	}
	if got := strings.Count(res.stderr, "  message: "); got != 20 {
		t.Errorf("want 20 message lines, got %d", got)
	}
	if lines[len(lines)-2] != "  (+5 more messages)" {
		t.Errorf("more line: %q", lines[len(lines)-2])
	}
	for _, bad := range []string{"\x1b", "\r", "\u202e"} {
		if strings.Contains(res.stderr, bad) {
			t.Errorf("a raw %q reached stderr", bad)
		}
	}
}

// preconditionServer answers the probe, then the upload with the SBOM precondition for the first
// `refusals` uploads (-1: always), then with an imported VEX.
func preconditionServer(t *testing.T, refusals int, other string) *fakeServer {
	return newFakeServer(t, func(n int, r recordedRequest) (int, string) {
		if r.OperationName == rearm.ImportSummaryProbeOperationName {
			return answerProbe(t, r, probeTypes{ImportSummary: currentSummaryFields})
		}
		if other != "" {
			return 200, errorsBody(map[string]any{"addArtifactProgrammatic": nil}, other)
		}
		if refusals < 0 || n <= refusals { // request 0 is the probe
			return 200, errorsBody(map[string]any{"addArtifactProgrammatic": nil}, staleSbom)
		}
		return 200, dataBody(map[string]any{"addArtifactProgrammatic": fakeAnswer{summaries: map[string]any{"release/0": sum("proposalsAutoAccepted", 1)}}.build(t, r)})
	})
}

// r16
func TestWaitForSbomRetriesTheIdenticalUpload(t *testing.T) {
	dir := t.TempDir()
	f := preconditionServer(t, 3, "")
	res := runImportChild(t, f.srv.URL, "addartifact", addArtifactArgs("--artifacts", jsonArg(t, []any{art(artFile(t, dir, "vex.json"), "VEX", "v")}), "--wait-for-sbom", "1m"), childOpts{fakeClock: true})
	wantExit(t, res, 0)
	reqs, _ := f.recorded()
	ups := uploads(reqs)
	if len(reqs)-len(ups) != 1 || len(ups) != 4 {
		t.Fatalf("want one probe and four uploads, got %d requests", len(reqs))
	}
	for i := 1; i < 4; i++ {
		if string(ups[i].Variables) != string(ups[0].Variables) || !reflect.DeepEqual(ups[i].FileContents, ups[0].FileContents) || !reflect.DeepEqual(ups[i].Files, ups[0].Files) {
			t.Errorf("upload %d differs from the first", i)
		}
	}
	lines := res.stderrLines()
	want := []string{
		"waiting for the release's SBOM to be indexed; retry 1 in 5s",
		"waiting for the release's SBOM to be indexed; retry 2 in 10s",
		"waiting for the release's SBOM to be indexed; retry 3 in 20s",
	}
	if len(lines) < 3 || strings.Join(lines[:3], "\n") != strings.Join(want, "\n") || lines[len(lines)-1] != passedOneOfOne {
		t.Errorf("stderr: %s", res)
	}
}

// r17
func TestWaitForSbomGivesUp(t *testing.T) {
	dir := t.TempDir()
	input := jsonArg(t, []any{art(artFile(t, dir, "vex.json"), "VEX", "v")})
	f := preconditionServer(t, -1, "")
	res := runImportChild(t, f.srv.URL, "addartifact", addArtifactArgs("--artifacts", input, "--wait-for-sbom", "30s"), childOpts{fakeClock: true})
	wantExit(t, res, 1)
	wantLines(t, res, []string{
		"waiting for the release's SBOM to be indexed; retry 1 in 5s",
		"waiting for the release's SBOM to be indexed; retry 2 in 10s",
		"waiting for the release's SBOM to be indexed; retry 3 in 15s",
		`Error: the release's SBOM was not indexed within 30s; nothing was uploaded. Server: "` + staleSbom + `"`,
	})
	reqs, _ := f.recorded()
	if len(uploads(reqs)) != 4 {
		t.Errorf("want four uploads, got %d", len(uploads(reqs)))
	}

	f = preconditionServer(t, -1, "")
	res = runImportChild(t, f.srv.URL, "addartifact", addArtifactArgs("--artifacts", input), childOpts{fakeClock: true})
	wantExit(t, res, 1)
	if res.stdout != "{\"data\":{\"addArtifactProgrammatic\":null}}\nGraphQL returned errors:\n- "+staleSbom+"\n" {
		t.Errorf("stdout must be as before: %s", res)
	}
	wantLines(t, res, []string{sbomHint})
}

// r18
func TestWaitForSbomRetriesNothingElse(t *testing.T) {
	dir := t.TempDir()
	f := preconditionServer(t, 0, "Release is locked")
	res := runImportChild(t, f.srv.URL, "addartifact", addArtifactArgs("--artifacts", jsonArg(t, []any{art(artFile(t, dir, "vex.json"), "VEX", "v")}), "--wait-for-sbom", "1m"), childOpts{fakeClock: true})
	wantExit(t, res, 1)
	wantLines(t, res, []string{refusedCallLine})
	reqs, _ := f.recorded()
	if len(uploads(reqs)) != 1 {
		t.Errorf("want one upload, got %d", len(uploads(reqs)))
	}
}

// r19
func TestANegativeWaitIsAnInputError(t *testing.T) {
	dir := t.TempDir()
	f := newFakeServer(t, func(int, recordedRequest) (int, string) { return 200, "{}" })
	res := runImportChild(t, f.srv.URL, "addartifact", addArtifactArgs("--artifacts", jsonArg(t, []any{art(artFile(t, dir, "vex.json"), "VEX", "v")}), "--wait-for-sbom", "-5s"), childOpts{})
	wantExit(t, res, 1)
	wantLines(t, res, []string{"Error: --wait-for-sbom must not be negative, got -5s"})
	wantNoRequests(t, f)
}

// legacyArtifactKeys are the keys the CLI sent for an artifact before this change (no new field is
// sent when the input does not set it).
var legacyArtifactKeys = []string{"displayIdentifier", "downloadLinks", "file", "inventoryTypes", "storedIn", "stripBom", "tags", "type", "version"}

// sentArtifacts walks the request variables for every object that carries a "file" key: the
// artifacts the command sent, in no particular order.
func sentArtifacts(v any, out *[]map[string]any) {
	switch x := v.(type) {
	case map[string]any:
		if _, ok := x["file"]; ok {
			*out = append(*out, x)
		}
		for _, c := range x {
			sentArtifacts(c, out)
		}
	case []any:
		for _, c := range x {
			sentArtifacts(c, out)
		}
	}
}

// r20
func TestCommandsThatNeverImportRefuseImportableEntries(t *testing.T) {
	dir := t.TempDir()
	vex, sc, bom := artFile(t, dir, "vex.json"), artFile(t, dir, "sc.json"), artFile(t, dir, "bom.json")
	batch := func(entry map[string]any) string {
		p := filepath.Join(t.TempDir(), "batch.json")
		b, _ := json.Marshal([]any{map[string]any{"component": "c", "branch": "main", "version": "1.0.0",
			"commits": []any{map[string]any{"commit": "abc", "commitMessage": "m", "artifacts": []any{entry}}}}})
		if err := os.WriteFile(p, b, 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	type form struct {
		command string
		args    func(entry map[string]any) []string
		entry   func(importable bool) map[string]any
		ref     string
		typ     string
	}
	top := func(path, typ string) func(bool) map[string]any {
		return func(importable bool) map[string]any {
			if importable {
				return art(path, typ, "x")
			}
			return art(bom, "BOM", "x")
		}
	}
	nestedSC := func(importable bool) map[string]any {
		inner := art(bom, "BOM", "y")
		if importable {
			inner = art(sc, "SAFETY_CONTEXT", "y")
		}
		return art(bom, "BOM", "x", "artifacts", []any{inner})
	}
	for _, c := range []form{
		{"addrelease", func(e map[string]any) []string {
			return []string{"--branch", "main", "--version", "1.0.0", "--component", "c", "--releasearts", jsonArg(t, []any{e})}
		}, top(vex, "VEX"), "--releasearts[0]", "VEX"},
		{"addrelease", func(e map[string]any) []string {
			return []string{"--branch", "main", "--version", "1.0.0", "--component", "c", "--odelid", "od-1", "--odelartsjson", jsonArg(t, []any{e})}
		}, nestedSC, "--odelartsjson[0][0].artifacts[0]", "SAFETY_CONTEXT"},
		{"addreleases", func(e map[string]any) []string { return []string{"--infile", batch(e)} }, top(vex, "VEX"), "--infile[0].commits[0].artifacts[0]", "VEX"},
		{"addodeliverable", func(e map[string]any) []string {
			return []string{"--releaseid", "r-1", "--odelid", "od-1", "--odelartsjson", jsonArg(t, []any{e})}
		}, top(vex, "VEX"), "--odelartsjson[0][0]", "VEX"},
		{"getversion", func(e map[string]any) []string {
			return []string{"--branch", "main", "--component", "c", "--scearts", jsonArg(t, []any{e})}
		}, top(vex, "VEX"), "--scearts[0]", "VEX"},
	} {
		answer := func(n int, r recordedRequest) (int, string) {
			return 200, dataBody(map[string]any{rootFieldOf(r.Query): map[string]any{"uuid": "x-1"}})
		}
		todayStdout := func(f *fakeServer) string {
			if c.command == "getversion" {
				return `{"uuid":"x-1"}` + "\n"
			}
			_, bodies := f.recorded()
			return bodies[0] + "\n"
		}
		// refused
		f := newFakeServer(t, answer)
		res := runImportChild(t, f.srv.URL, c.command, c.args(c.entry(true)), childOpts{})
		wantExit(t, res, 1)
		wantLines(t, res, []string{fmt.Sprintf("Error: %s: %s stores a %s artifact but never imports it, so nothing would take effect; upload it with addartifact once the release's SBOM is indexed, or pass --allow-import-failure to store it without an import", c.ref, c.command, c.typ)})
		wantNoRequests(t, f)

		// accepted by the flag: one notice, the request as before
		f = newFakeServer(t, answer)
		res = runImportChild(t, f.srv.URL, c.command, append(c.args(c.entry(true)), "--allow-import-failure"), childOpts{})
		wantExit(t, res, 0)
		wantLines(t, res, []string{fmt.Sprintf("notice: %s: %s stores this %s artifact but does not import it (accepted by --allow-import-failure)", c.ref, c.command, c.typ)})
		if res.stdout != todayStdout(f) {
			t.Errorf("%s: stdout must be as before: %s", c.command, res)
		}
		reqs, _ := f.recorded()
		if len(reqs) != 1 {
			t.Fatalf("%s: want one request, got %d", c.command, len(reqs))
		}
		var vars any
		_ = json.Unmarshal(reqs[0].Variables, &vars)
		var arts []map[string]any
		sentArtifacts(vars, &arts)
		if len(arts) == 0 {
			t.Fatalf("%s: no artifact sent: %s", c.command, reqs[0].Variables)
		}
		for _, a := range arts {
			var keys []string
			for k := range a {
				if k != "artifacts" {
					keys = append(keys, k)
				}
			}
			sort.Strings(keys)
			if !reflect.DeepEqual(keys, legacyArtifactKeys) || a["tags"] != nil {
				t.Errorf("%s: the artifact is not sent as before: %v", c.command, a)
			}
		}

		// BOM only: nothing changes
		f = newFakeServer(t, answer)
		res = runImportChild(t, f.srv.URL, c.command, c.args(c.entry(false)), childOpts{})
		wantExit(t, res, 0)
		if res.stderr != "" {
			t.Errorf("%s: BOM only prints nothing on stderr: %s", c.command, res)
		}
		if reqs, _ := f.recorded(); len(reqs) != 1 {
			t.Errorf("%s: BOM only sends its request", c.command)
		}
	}
}

// r21, r22
func TestHelpStatesTheRules(t *testing.T) {
	f := newFakeServer(t, func(int, recordedRequest) (int, string) { return 200, "{}" })
	help := func(args ...string) string {
		res := runImportChild(t, f.srv.URL, "", append(args, "--help"), childOpts{execute: true})
		wantExit(t, res, 0)
		return res.stdout
	}
	out := help("addartifact")
	for _, want := range []string{
		`--artifacts '[{"filePath": "safety-context.cdx.json", "type": "SAFETY_CONTEXT", "displayIdentifier": "iec62304-classes", "importScope": "COMPONENT"}]'`,
		`--artifacts '[{"filePath": "vex.cdx.json", "type": "VEX", "displayIdentifier": "vendor-vex", "importScope": "COMPONENT", "importMode": "STAGE"}]'`,
		"takes effect automatically only when the uploading key itself holds Finding Analysis write at the import scope",
		"Component keys and organization-wide read-write keys never hold Finding Analysis write",
		"The release's SBOM must be indexed first",
		"--wait-for-sbom 10m",
		"--allow-import-failure",
		"cannot be combined with the advanced flags",
		"on a source code entry (`--scearts`) or nested under another artifact is refused",
		allowImportFailureReadOnlyCommandLine,
		"retry the upload while the release has no indexed SBOM, for at most this long (e.g. 10m); 0 does not wait",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("addartifact --help lacks %q", want)
		}
	}
	if strings.Contains(out, `"vexImportMode": "STAGE"`) {
		t.Errorf("addartifact --help still shows the deprecated field in an example")
	}
	for _, c := range []string{"addrelease", "addreleases", "addodeliverable", "getversion"} {
		out := help(c)
		for _, want := range []string{"this command stores it but never imports it", "--allow-import-failure", allowImportFailureReadOnlyCommandLine} {
			if !strings.Contains(out, want) {
				t.Errorf("%s --help lacks %q", c, want)
			}
		}
	}
	out = help("agent", "session", "add-artifact")
	if strings.Contains(out, "OCI_STORAGE and REARM may also be declared") || !strings.Contains(out, "AS_UPLOADED, RAW_OCI_STORAGE and REARM are computed by ReARM and refused") {
		t.Errorf("agent session add-artifact --help: REARM must be named as computed by ReARM:\n%s", out)
	}
	wantNoRequests(t, f)
}

// r23
func TestOnlyUncheckedEntriesSendNoProbe(t *testing.T) {
	dir := t.TempDir()
	f := newFakeServer(t, func(n int, r recordedRequest) (int, string) {
		return 200, dataBody(map[string]any{"addArtifactProgrammatic": map[string]any{"uuid": "rel-1"}})
	})
	release := jsonArg(t, []any{art(artFile(t, dir, "bom.json"), "BOM", "b", "artifacts", []any{art(artFile(t, dir, "sc.json"), "SAFETY_CONTEXT", "n")})})
	sce := jsonArg(t, []any{map[string]any{"sce": "s-1", "artifacts": []any{art(artFile(t, dir, "vex.json"), "VEX", "s")}}})
	res := runImportChild(t, f.srv.URL, "addartifact", addArtifactArgs("--releasearts", release, "--scearts", sce, "--allow-import-failure"), childOpts{})
	wantExit(t, res, 0)
	wantLines(t, res, []string{
		`import SAFETY_CONTEXT --releasearts[0].artifacts[0] "n": NOT IMPORTED: a nested artifact runs no import`,
		`import VEX --scearts[0].artifacts[0] "s": NOT REPORTED: the server does not report import results for source code entry uploads`,
		"import check: 2 of 2 VEX/SAFETY_CONTEXT artifacts imported nothing or have no known result; accepted by --allow-import-failure",
	})
	reqs, _ := f.recorded()
	if len(reqs) != 1 || reqs[0].OperationName != "AddArtifactProgrammatic" {
		t.Errorf("want one legacy upload and no probe, got %+v", reqs)
	}
}

// r24: through the real Execute, so PersistentPreRun, initConfig and bindFlags run.
func TestAllowImportFailureIsReadOnlyFromTheCommandLine(t *testing.T) {
	dir := t.TempDir()
	errored := sum("statementsTotal", 1, "proposalsCreated", 0, "proposalsAutoAccepted", 0, "proposalsAutoRejected", 0, "attestationsDeferred", 0,
		"statementsUnmatched", 0, "statementsErrored", 1, "statementsUnchanged", 0, "errorMessages", []any{"refused"}, "demotions", []any{}, "overrideOutcome", "NONE")
	scArgs := []string{"addartifact", "--component", "comp", "--version", "1.0.0", "--artifacts", jsonArg(t, []any{art(artFile(t, dir, "sc.json"), "SAFETY_CONTEXT", "d")})}
	releaseArgs := []string{"addrelease", "--branch", "main", "--version", "1.0.0", "--component", "c", "--releasearts", jsonArg(t, []any{art(artFile(t, dir, "vex.json"), "VEX", "x")})}
	config := func(line string) []string {
		p := filepath.Join(t.TempDir(), "rearm.yaml")
		if err := os.WriteFile(p, []byte(line+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		return []string{"--config", p, "--debug", "true"}
	}
	importSrv := func() *fakeServer {
		return importServer(t, probeTypes{ImportSummary: currentSummaryFields}, fakeAnswer{summaries: map[string]any{"release/0": errored}})
	}
	releaseSrv := func() *fakeServer {
		return newFakeServer(t, func(n int, r recordedRequest) (int, string) {
			return 200, dataBody(map[string]any{rootFieldOf(r.Query): map[string]any{"uuid": "x-1"}})
		})
	}
	const refusal = "Error: --releasearts[0]: addrelease stores a VEX artifact but never imports it, so nothing would take effect; upload it with addartifact once the release's SBOM is indexed, or pass --allow-import-failure to store it without an import"
	// bindFlags binds a dashed flag to envPrefix + "_" + NAME, which is rearm_ALLOW_IMPORT_FAILURE (viper's
	// BindEnv takes the name as given); both that spelling and the documented REARM_ one are set, and
	// case (f) shows the rearm_ spelling is the one the binding reads
	envOn := childOpts{execute: true, env: []string{"REARM_ALLOW_IMPORT_FAILURE=true", envPrefix + "_ALLOW_IMPORT_FAILURE=true"}}

	// (a) the variable, (b) the config file: a default run
	for i, c := range []struct {
		args []string
		opts childOpts
	}{{scArgs, envOn}, {append(config("allow-import-failure: true"), scArgs...), childOpts{execute: true}}} {
		f := importSrv()
		res := runImportChild(t, f.srv.URL, "", c.args, c.opts)
		wantExit(t, res, 3)
		if reqs, _ := f.recorded(); len(reqs) != 2 {
			t.Errorf("case %d: want the probe and the upload, got %d requests", i, len(reqs))
		}
		if i == 0 && res.stdout != lastUploadEnvelope(t, f) {
			t.Errorf("case a: stdout must be the envelope: %s", res)
		}
		lines := res.stderrLines()
		if len(lines) == 0 || lines[len(lines)-1] != failedOneOfOne || !strings.Contains(res.stderr, ": NOTHING IMPORTED: statements errored") ||
			strings.Contains(res.stderr, "accepted by --allow-import-failure") {
			t.Errorf("case %d: want the default failed line: %s", i, res)
		}
		if i == 1 && !strings.Contains(res.stderr, "Using config file: "+c.args[1]) {
			t.Errorf("case b: the config file was not read: %s", res)
		}
	}

	// (c) the variable, (d) the config file: addrelease still refuses
	for i, c := range []struct {
		args []string
		opts childOpts
	}{{releaseArgs, envOn}, {append(config("allow-import-failure: true"), releaseArgs...), childOpts{execute: true}}} {
		f := releaseSrv()
		res := runImportChild(t, f.srv.URL, "", c.args, c.opts)
		wantExit(t, res, 1)
		want := []string{refusal}
		if i == 1 {
			want = []string{"Using config file: " + c.args[1], refusal}
		}
		wantLines(t, res, want)
		wantNoRequests(t, f)
	}

	// (e) the command-line flag works through bindFlags
	f := importSrv()
	res := runImportChild(t, f.srv.URL, "", append(scArgs, "--allow-import-failure"), childOpts{execute: true})
	wantExit(t, res, 0)
	if lines := res.stderrLines(); len(lines) == 0 || lines[len(lines)-1] != acceptedOneOfOne {
		t.Errorf("case e (addartifact): %s", res)
	}
	f = releaseSrv()
	res = runImportChild(t, f.srv.URL, "", append(releaseArgs, "--allow-import-failure"), childOpts{execute: true})
	wantExit(t, res, 0)
	wantLines(t, res, []string{"notice: --releasearts[0]: addrelease stores this VEX artifact but does not import it (accepted by --allow-import-failure)"})
	if reqs, _ := f.recorded(); len(reqs) != 1 {
		t.Errorf("case e (addrelease): want the request sent")
	}

	// (f) the skip is by name only: --wait-for-sbom still binds
	for i, c := range []struct {
		args []string
		opts childOpts
	}{{scArgs, childOpts{execute: true, env: []string{envPrefix + "_WAIT_FOR_SBOM=-5s"}}}, {append(config("wait-for-sbom: -5s"), scArgs...), childOpts{execute: true}}} {
		f := importSrv()
		res := runImportChild(t, f.srv.URL, "", c.args, c.opts)
		wantExit(t, res, 1)
		if !strings.Contains(res.stderr, "Error: --wait-for-sbom must not be negative, got -5s") {
			t.Errorf("case f%d: %s", i, res)
		}
		wantNoRequests(t, f)
	}
}
