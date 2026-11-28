package cmd

import (
	"strings"
	"testing"

	rearm "github.com/relizaio/rearm-client-go"
)

// addartifact when the server refuses or cannot answer the import summary probe, against a fake
// that keeps the good-faith introspection limit (task SAFE-30, tests c2 to c5).

const probeErrorPrefix = "Error: could not read which import result fields this server reports: "

// vexArgs is addartifact with one VEX in --artifacts, plus more.
func vexArgs(t *testing.T, more ...string) []string {
	t.Helper()
	dir := t.TempDir()
	return addArtifactArgs(append([]string{"--artifacts", jsonArg(t, []any{art(artFile(t, dir, "vex.json"), "VEX", "v")})}, more...)...)
}

// probeRefusal runs addartifact without and with --allow-import-failure against a server that
// answers every probe request with body, and checks exit 1, the one probe error line and that
// only the probe was sent.
func probeRefusal(t *testing.T, body, wantLine string) {
	t.Helper()
	for _, more := range [][]string{nil, {"--allow-import-failure"}} {
		f := newFakeServer(t, func(n int, r recordedRequest) (int, string) {
			if r.OperationName == rearm.ImportSummaryProbeOperationName {
				return 200, body
			}
			t.Errorf("nothing is uploaded after a refused probe, got %s", r.OperationName)
			return 500, ""
		})
		res := runImportChild(t, f.srv.URL, "addartifact", vexArgs(t, more...), childOpts{})
		wantExit(t, res, 1)
		if lines := res.stderrLines(); len(lines) != 1 || lines[0] != wantLine {
			t.Errorf("%v: want only %q on stderr, got %s", more, wantLine, res)
		}
		if res.stdout != "" {
			t.Errorf("%v: stdout must be empty: %s", more, res)
		}
		reqs, _ := f.recorded()
		if len(reqs) != 1 || reqs[0].OperationName != rearm.ImportSummaryProbeOperationName {
			t.Errorf("%v: want the probe only, got %+v", more, reqs)
		}
	}
}

// c2
func TestABadFaithProbeRefusalUploadsNothing(t *testing.T) {
	probeRefusal(t, badFaithBody,
		probeErrorPrefix+"This request is not asking for introspection in good faith - __Type.fields is present too often!; nothing was uploaded")
}

// c3
func TestARefusedProbeUploadsNothing(t *testing.T) {
	probeRefusal(t, `{"errors":[{"message":"Not authorized"}]}`, probeErrorPrefix+"Not authorized; nothing was uploaded")
}

// c4
func TestAFailedSecondProbeUploadsNothing(t *testing.T) {
	f := newFakeServer(t, func(n int, r recordedRequest) (int, string) {
		if r.OperationName != rearm.ImportSummaryProbeOperationName {
			t.Errorf("nothing is uploaded after a failed probe, got %s", r.OperationName)
			return 500, ""
		}
		if n == 0 {
			return answerProbe(t, r, probeTypes{VexImportSummary: vexSummaryFields})
		}
		return 500, "internal error"
	})
	res := runImportChild(t, f.srv.URL, "addartifact", vexArgs(t), childOpts{})
	wantExit(t, res, 1)
	lines := res.stderrLines()
	if len(lines) != 1 || !strings.HasPrefix(lines[0], probeErrorPrefix) || !strings.HasSuffix(lines[0], "; nothing was uploaded") {
		t.Errorf("want the probe error line, got %s", res)
	}
	reqs, _ := f.recorded()
	if len(reqs) != 2 || probeName(t, reqs[0]) != rearm.ImportSummaryTypeName || probeName(t, reqs[1]) != rearm.VexImportSummaryTypeName {
		t.Errorf("want the two probes only, got %+v", reqs)
	}
}

// c5
func TestAServerWithIntrospectionDisabledIsNotReported(t *testing.T) {
	const disabled = `{"errors":[{"message":"Introspection has been disabled for this request","extensions":{"classification":"IntrospectionDisabled"}}]}`
	for _, allow := range []bool{false, true} {
		f := newFakeServer(t, func(n int, r recordedRequest) (int, string) {
			if r.OperationName == rearm.ImportSummaryProbeOperationName {
				return 200, disabled
			}
			return 200, dataBody(map[string]any{"addArtifactProgrammatic": map[string]any{"uuid": "rel-1"}})
		})
		args, want, exit := vexArgs(t), failedOneOfOne, 3
		if allow {
			args, want, exit = vexArgs(t, "--allow-import-failure"), acceptedOneOfOne, 0
		}
		res := runImportChild(t, f.srv.URL, "addartifact", args, childOpts{})
		wantExit(t, res, exit)
		wantLines(t, res, []string{`import VEX --artifacts[0] "v": NOT REPORTED: this server does not report import results`, want})
		reqs, _ := f.recorded()
		if len(reqs) != 2 || reqs[0].OperationName != rearm.ImportSummaryProbeOperationName || reqs[1].OperationName != "AddArtifactProgrammatic" {
			t.Errorf("want one probe and a legacy upload, got %+v", reqs)
		}
	}
}
