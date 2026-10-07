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

package tests

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/relizaio/rearm/cmd"
	"github.com/relizaio/rearm/internal/bomscore"
)

// Test 15 of SCORE-3: rearm bomutils score.

const bomScoreFixtures = "../internal/bomscore/testdata"

func bomScoreFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(bomScoreFixtures, name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

type bomScoreRun struct {
	code           int
	stdout, stderr string
}

func runBomScore(opts cmd.BomScoreOptions, stdin []byte) bomScoreRun {
	if opts.Format == "" {
		opts.Format = "text"
	}
	var stdout, stderr bytes.Buffer
	code := cmd.RunBomScore(opts, bytes.NewReader(stdin), &stdout, &stderr)
	return bomScoreRun{code, stdout.String(), stderr.String()}
}

func TestBomScoreStdinToStdout(t *testing.T) {
	run := runBomScore(cmd.BomScoreOptions{Format: "json", Profiles: []string{"cisa-2026", "fda"}}, bomScoreFixture(t, "full.cdx.json"))
	if run.code != 0 || run.stderr != "" {
		t.Fatalf("exit %d stderr %q, want 0 and nothing", run.code, run.stderr)
	}
	var report bomscore.Report
	if err := json.Unmarshal([]byte(run.stdout), &report); err != nil {
		t.Fatalf("stdout is not a report: %v", err)
	}
	if report.ReportVersion != 1 || len(report.Profiles) != 2 || report.Profiles[0].Verdict != bomscore.VerdictReady {
		t.Errorf("report %+v, want version 1 with two READY profiles", report)
	}
	if run := runBomScore(cmd.BomScoreOptions{Infile: "-", Outfile: "-", Format: "json"}, bomScoreFixture(t, "full.cdx.json")); run.code != 0 || !strings.Contains(run.stdout, `"key": "cisa-2026"`) {
		t.Errorf("'-' for stdin and stdout: exit %d, stdout %q; want the default cisa-2026 report", run.code, run.stdout)
	}
}

func TestBomScoreFiles(t *testing.T) {
	out := filepath.Join(t.TempDir(), "report.json")
	run := runBomScore(cmd.BomScoreOptions{Infile: filepath.Join(bomScoreFixtures, "full.spdx.yaml"), Outfile: out, Format: "json"}, nil)
	if run.code != 0 || run.stdout != "" {
		t.Fatalf("exit %d stdout %q, want 0 and nothing on stdout", run.code, run.stdout)
	}
	b, err := os.ReadFile(out)
	if err != nil || !bytes.Contains(b, []byte(`"serialization": "yaml"`)) {
		t.Errorf("outfile %q, err %v; want the yaml report", b, err)
	}
	if run := runBomScore(cmd.BomScoreOptions{Infile: filepath.Join(t.TempDir(), "missing.json")}, nil); run.code != 1 || run.stdout != "" || run.stderr == "" {
		t.Errorf("missing infile: exit %d stdout %q stderr %q, want 1 with the error on stderr", run.code, run.stdout, run.stderr)
	}
}

func TestBomScoreText(t *testing.T) {
	run := runBomScore(cmd.BomScoreOptions{Profiles: []string{"cisa-2026", "fda"}}, bomScoreFixture(t, "full.spdx.json"))
	if run.code != 0 {
		t.Fatalf("exit %d", run.code)
	}
	for _, line := range []string{
		"cisa-2026  CISA Minimum Elements for an SBOM (2026)  NOT_READY  score 82\n",
		"  FAIL  SBOM Author Signature  (Table 1, SBOM Author Signature)\n        note: not representable in SPDX 2.3\n",
		"fda  FDA Premarket SBOM (2026)  NOT_READY  score 90\n",
		"  FAIL  Software level of support  0/4  (V.A.4(b))\n" +
			"        missing in: pkg:golang/example.com/delta@v0.4.2, pkg:maven/org.beta/beta@2.0.0, pkg:npm/alpha@1.0.0, pkg:pypi/gamma@3.1.0\n" +
			"        remedy: assess support for the component in ReARM and export with support metadata\n" +
			"        note: not representable in SPDX 2.3\n",
		"  10 required: 9 pass, 1 fail, 0 error; 1 not assessed\n",
	} {
		if !strings.Contains(run.stdout, line) {
			t.Errorf("text output has no %q:\n%s", line, run.stdout)
		}
	}
}

func TestBomScoreUsageErrors(t *testing.T) {
	data := bomScoreFixture(t, "full.cdx.json")
	run := runBomScore(cmd.BomScoreOptions{Profiles: []string{"cisa-2026", "bsi"}}, data)
	if run.code != 2 || run.stdout != "" || !strings.Contains(run.stderr, "cisa-2026, ntia-2021, fda") {
		t.Errorf("unknown profile: exit %d stdout %q stderr %q, want 2 listing the valid keys", run.code, run.stdout, run.stderr)
	}
	run = runBomScore(cmd.BomScoreOptions{Format: "xml"}, data)
	if run.code != 2 || run.stdout != "" || run.stderr == "" {
		t.Errorf("--format xml: exit %d stdout %q stderr %q, want 2", run.code, run.stdout, run.stderr)
	}
}

func TestBomScoreRefusedInputs(t *testing.T) {
	cdx := string(bomScoreFixture(t, "full.cdx.json"))
	cases := map[string]string{
		"":             "unsupported SBOM: empty input\n",
		"not an SBOM":  "unsupported SBOM: not a CycloneDX or SPDX document\n",
		`{"runs": []}`: "unsupported SBOM: not a CycloneDX or SPDX document\n",
		strings.Replace(cdx, `"specVersion": "1.6"`, `"specVersion": "2.0"`, 1):                                                                      "unsupported SBOM: CycloneDX specVersion missing or not 1.0 to 1.7\n",
		strings.Replace(cdx, `"specVersion": "1.6",`, ``, 1):                                                                                         "unsupported SBOM: CycloneDX specVersion missing or not 1.0 to 1.7\n",
		strings.Replace(string(bomScoreFixture(t, "full.cdx.xml")), "http://cyclonedx.org/schema/bom/1.6", "http://cyclonedx.org/schema/bom/9.9", 1): "unsupported SBOM: CycloneDX specVersion missing or not 1.0 to 1.7\n",
		`{"@context": "https://spdx.org/rdf/3.0.1/spdx-context.jsonld"}`:                                                                             "unsupported SBOM: SPDX 3 is not supported\n",
		`<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"/>`:                                                                         "unsupported SBOM: SPDX RDF/XML is not supported\n",
		`{"spdxVersion": "SPDX-2.0", "SPDXID": "SPDXRef-DOCUMENT"}`:                                                                                  "unsupported SBOM: SPDX 2.0 is not supported in json\n",
		cdx[:len(cdx)/2]: "unparsable SBOM: ",
	}
	for input, want := range cases {
		run := runBomScore(cmd.BomScoreOptions{}, []byte(input))
		if run.code != 1 || run.stdout != "" || !strings.HasPrefix(run.stderr, want) || strings.Count(run.stderr, "\n") != 1 {
			t.Errorf("exit %d stdout %q stderr %q, want 1, nothing, one line %q", run.code, run.stdout, run.stderr, want)
		}
	}
}

func TestBomScoreFailOnNotReady(t *testing.T) {
	spdx := bomScoreFixture(t, "full.spdx.json")
	run := runBomScore(cmd.BomScoreOptions{FailOnNotReady: true, Format: "json"}, spdx)
	if run.code != 3 || !strings.Contains(run.stdout, `"verdict": "NOT_READY"`) {
		t.Errorf("NOT_READY with --fail-on-not-ready: exit %d, want 3 and the report", run.code)
	}
	if run := runBomScore(cmd.BomScoreOptions{Format: "json"}, spdx); run.code != 0 {
		t.Errorf("NOT_READY without the flag: exit %d, want 0", run.code)
	}
	if run := runBomScore(cmd.BomScoreOptions{FailOnNotReady: true, Profiles: []string{"ntia-2021"}}, spdx); run.code != 0 {
		t.Errorf("READY with the flag: exit %d, want 0", run.code)
	}
	// SCORE-10: the dependency rule alone makes ntia-2021 NOT_READY when the root declares nothing.
	var m map[string]any
	if err := json.Unmarshal(bomScoreFixture(t, "full.cdx.json"), &m); err != nil {
		t.Fatal(err)
	}
	var kept []any
	for _, e := range m["dependencies"].([]any) {
		if e.(map[string]any)["ref"] != "app" {
			kept = append(kept, e)
		}
	}
	m["dependencies"] = kept
	noRoot, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if run := runBomScore(cmd.BomScoreOptions{FailOnNotReady: true, Profiles: []string{"ntia-2021"}, Format: "json"}, noRoot); run.code != 3 ||
		!strings.Contains(run.stdout, "described component declares no direct dependencies: app") {
		t.Errorf("root without dependencies under ntia-2021: exit %d, want 3 and the dependency note", run.code)
	}
	unknown := bomscore.Report{Profiles: []bomscore.ProfileReport{{Verdict: bomscore.VerdictReady}, {Verdict: bomscore.VerdictUnknown}}}
	if code := cmd.BomScoreExitCode(unknown, true); code != 3 {
		t.Errorf("UNKNOWN with the flag: exit %d, want 3", code)
	}
	if code := cmd.BomScoreExitCode(unknown, false); code != 0 {
		t.Errorf("UNKNOWN without the flag: exit %d, want 0", code)
	}
}

// The command as built: registered under bomutils, reading stdin, with its exit codes.
func TestBomScoreBinary(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not on PATH")
	}
	bin := filepath.Join(t.TempDir(), "rearm")
	build := exec.Command("go", "build", "-o", bin, "..")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	run := func(stdin []byte, args ...string) (int, string, string) {
		c := exec.Command(bin, append([]string{"bomutils", "score"}, args...)...)
		c.Env = append(os.Environ(), "HOME="+t.TempDir())
		c.Stdin = bytes.NewReader(stdin)
		var stdout, stderr bytes.Buffer
		c.Stdout, c.Stderr = &stdout, &stderr
		err := c.Run()
		code := 0
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		return code, stdout.String(), stderr.String()
	}
	if code, out, _ := run(bomScoreFixture(t, "full.cdx.json"), "--profile", "fda", "--format", "json"); code != 0 || !strings.Contains(out, `"key": "fda"`) {
		t.Errorf("exit %d, stdout %.200q; want 0 and the fda report", code, out)
	}
	if code, out, errOut := run(nil); code != 1 || out != "" || errOut != "unsupported SBOM: empty input\n" {
		t.Errorf("empty stdin: exit %d stdout %q stderr %q", code, out, errOut)
	}
	if code, _, _ := run(bomScoreFixture(t, "full.spdx.json"), "--fail-on-not-ready"); code != 3 {
		t.Errorf("--fail-on-not-ready on SPDX: exit %d, want 3", code)
	}
	out := filepath.Join(t.TempDir(), "report.json")
	if code, stdout, _ := run(nil, "-f", filepath.Join(bomScoreFixtures, "full.cdx.xml"), "-o", out,
		"--profile", "fda", "--profile", "cisa-2026", "--profile", "fda", "--format", "json"); code != 0 || stdout != "" {
		t.Errorf("-f/-o: exit %d stdout %q, want 0 and nothing on stdout", code, stdout)
	}
	var report bomscore.Report
	if b, err := os.ReadFile(out); err != nil || json.Unmarshal(b, &report) != nil {
		t.Fatalf("outfile not a report: %v", err)
	}
	if len(report.Profiles) != 2 || report.Profiles[0].Key != bomscore.ProfileFDA || report.Profiles[1].Key != bomscore.ProfileCISA2026 || report.Input.Serialization != bomscore.SerializationXML {
		t.Errorf("profiles %+v from %s, want fda then cisa-2026 once each, from xml", report.Profiles, report.Input.Serialization)
	}
	// cobra refuses a positional argument (the CLI's general behaviour: exit 1), rather than
	// ignoring it and reading stdin.
	if code, _, _ := run(bomScoreFixture(t, "full.cdx.json"), "bom.json"); code != 1 {
		t.Errorf("a positional argument: exit %d, want 1", code)
	}
	if code, _, _ := run(nil, "--profile", "bsi"); code != 2 {
		t.Errorf("unknown profile: exit %d, want 2", code)
	}
	if code, _, _ := run(nil, "-f", filepath.Join(bomScoreFixtures, "full.spdx"), "--format", "xml"); code != 2 {
		t.Errorf("--format xml: exit %d, want 2", code)
	}
	// SCORE-10: the --skip-files flag is registered and reaches the report.
	if code, out, _ := run(bomScoreFixture(t, "full.cdx.json"), "--skip-files", "--format", "json"); code != 0 || !strings.Contains(out, `"skipFiles": true`) {
		t.Errorf("--skip-files: exit %d, stdout %.200q; want 0 and skipFiles true", code, out)
	}
}
