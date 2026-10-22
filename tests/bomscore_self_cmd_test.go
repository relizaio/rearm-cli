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
)

// SCORE-59 T-7: the subject's own components (reliza:support:selfComponent) through RunBomScore.
func TestBomScoreSelfComponents(t *testing.T) {
	self := bomScoreFixture(t, "full.self.cdx.json")
	run := runBomScore(cmd.BomScoreOptions{Profiles: []string{"fda"}, Format: "json"}, self)
	if run.code != 0 || !strings.Contains(run.stdout, `"selfComponentsSkipped": 1`) {
		t.Errorf("json: exit %d stdout %.300q, want 0 and selfComponentsSkipped 1", run.code, run.stdout)
	}

	// gamma (beta's first nested component) without properties fails both support checks.
	var m map[string]any
	if err := json.Unmarshal(self, &m); err != nil {
		t.Fatal(err)
	}
	beta := m["components"].([]any)[1].(map[string]any)
	gamma := beta["components"].([]any)[0].(map[string]any)
	if gamma["bom-ref"] != "gamma" {
		t.Fatal("fixture changed: gamma is expected under beta")
	}
	delete(gamma, "properties")
	failing, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	run = runBomScore(cmd.BomScoreOptions{Profiles: []string{"fda"}, Format: "text"}, failing)
	if want := "skipped 1 of the subject's own components (reliza:support:selfComponent)"; !strings.Contains(run.stdout, want) {
		t.Errorf("text: stdout %q, want %q", run.stdout, want)
	}

	// --skip-files removes the file component; the self component is not a file.
	run = runBomScore(cmd.BomScoreOptions{Profiles: []string{"fda"}, Format: "json", SkipFiles: true}, self)
	if run.code != 0 || !strings.Contains(run.stdout, `"selfComponentsSkipped": 1`) {
		t.Errorf("--skip-files: exit %d stdout %.300q, want 0 and selfComponentsSkipped 1", run.code, run.stdout)
	}
	var report struct {
		Input struct {
			ComponentsSkipped int `json:"componentsSkipped"`
		} `json:"input"`
	}
	if err := json.Unmarshal([]byte(run.stdout), &report); err != nil || report.Input.ComponentsSkipped != 1 {
		t.Errorf("--skip-files: input.componentsSkipped %d (%v), want 1", report.Input.ComponentsSkipped, err)
	}
}

// SCORE-59 T-7: the built binary reports the subject's own components.
func TestBomScoreSelfComponentsBinary(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not on PATH")
	}
	bin := filepath.Join(t.TempDir(), "rearm")
	if out, err := exec.Command("go", "build", "-o", bin, "..").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	c := exec.Command(bin, "bomutils", "score", "--profile", "fda", "--format", "json")
	c.Env = append(os.Environ(), "HOME="+t.TempDir())
	c.Stdin = bytes.NewReader(bomScoreFixture(t, "full.self.cdx.json"))
	out, err := c.Output()
	if err != nil || !strings.Contains(string(out), `"selfComponentsSkipped": 1`) {
		t.Errorf("full.self.cdx.json: %v, stdout %.200q; want exit 0 and selfComponentsSkipped 1", err, out)
	}
}
