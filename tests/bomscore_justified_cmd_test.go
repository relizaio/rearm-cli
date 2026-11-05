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
	"encoding/json"
	"strings"
	"testing"

	"github.com/relizaio/rearm/cmd"
)

// FDAW-13 T-8: the fda justified outcome (reliza:support:justifiedUnknown:*) through RunBomScore.
func TestBomScoreJustified(t *testing.T) {
	justified := bomScoreFixture(t, "full.justified.cdx.json")

	run := runBomScore(cmd.BomScoreOptions{Profiles: []string{"fda"}, Format: "json"}, justified)
	if n := strings.Count(run.stdout, `"justified": 1,`); run.code != 0 || n != 2 {
		t.Errorf("json: exit %d, %d justified keys; want 0 and 2", run.code, n)
	}

	run = runBomScore(cmd.BomScoreOptions{Profiles: []string{"fda"}, Format: "text"}, justified)
	for _, line := range []string{
		"  PASS (1 justified)  Component Hash  4/5  (V.A.4(b); NTIA Framing 2nd ed. 2.2, Component Hash)\n",
		"  FAIL (1 justified)  End-of-support date  3/5  (V.A.4(b))\n        missing in: pkg:golang/example.com/delta@v0.4.2\n",
	} {
		if !strings.Contains(run.stdout, line) {
			t.Errorf("text: stdout %q, want %q", run.stdout, line)
		}
	}

	run = runBomScore(cmd.BomScoreOptions{Profiles: []string{"cisa-2026"}, Format: "json"}, justified)
	if run.code != 0 || strings.Contains(run.stdout, `"justified"`) {
		t.Errorf("cisa-2026: exit %d, justified in stdout %v; want 0 and none", run.code, strings.Contains(run.stdout, `"justified"`))
	}

	run = runBomScore(cmd.BomScoreOptions{Profiles: []string{"fda"}, Format: "json", FailOnNotReady: true}, justified)
	if run.code != 3 {
		t.Errorf("--fail-on-not-ready on the fixture: exit %d, want 3 (end-of-support fails)", run.code)
	}

	// T-2(d): gamma without hash and end-of-support date, both justified: READY.
	var m map[string]any
	if err := json.Unmarshal(bomScoreFixture(t, "full.cdx.json"), &m); err != nil {
		t.Fatal(err)
	}
	beta := m["components"].([]any)[1].(map[string]any)
	gamma := beta["components"].([]any)[0].(map[string]any)
	if gamma["bom-ref"] != "gamma" {
		t.Fatal("fixture changed: gamma is expected under beta")
	}
	delete(gamma, "hashes")
	gamma["properties"] = []any{
		map[string]any{"name": "reliza:support:levelOfSupport", "value": "abandoned"},
		map[string]any{"name": "reliza:support:justifiedUnknown:hash", "value": "DISTRIBUTION_PUBLISHES_NO_HASH"},
		map[string]any{"name": "reliza:support:justifiedUnknown:endOfSupport", "value": "SUPPLIER_PUBLISHES_NO_DATE"},
	}
	ready, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	run = runBomScore(cmd.BomScoreOptions{Profiles: []string{"fda"}, Format: "json", FailOnNotReady: true}, ready)
	if run.code != 0 || !strings.Contains(run.stdout, `"verdict": "READY"`) {
		t.Errorf("--fail-on-not-ready, every unknown justified: exit %d, want 0 and READY (stderr %q)", run.code, run.stderr)
	}
}
