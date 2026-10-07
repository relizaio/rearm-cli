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

// SCORE-10: rearm bomutils score --skip-files.

// twoFilesBOM is full.cdx.json with two type file components, one of them nesting a library.
func twoFilesBOM(t *testing.T) []byte {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(bomScoreFixture(t, "full.cdx.json"), &m); err != nil {
		t.Fatal(err)
	}
	m["components"] = append(m["components"].([]any),
		map[string]any{"type": "file", "bom-ref": "file-readme", "name": "README.md"},
		map[string]any{"type": "file", "bom-ref": "file-app-bin", "name": "bin/app", "components": []any{
			map[string]any{"type": "library", "bom-ref": "epsilon", "name": "epsilon", "version": "1.0.0"},
		}},
	)
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestBomScoreSkipFiles(t *testing.T) {
	data := twoFilesBOM(t)
	type report struct {
		Input struct {
			Components        int `json:"components"`
			ComponentsSkipped int `json:"componentsSkipped"`
		} `json:"input"`
		Options struct {
			SkipFiles bool `json:"skipFiles"`
		} `json:"options"`
	}
	parse := func(t *testing.T, out string) report {
		t.Helper()
		var r report
		if err := json.Unmarshal([]byte(out), &r); err != nil {
			t.Fatalf("%v:\n%s", err, out)
		}
		return r
	}

	run := runBomScore(cmd.BomScoreOptions{SkipFiles: true, Format: "json"}, data)
	if run.code != 0 {
		t.Fatalf("exit %d, stderr %q", run.code, run.stderr)
	}
	if r := parse(t, run.stdout); !r.Options.SkipFiles || r.Input.ComponentsSkipped != 2 || r.Input.Components != 5 {
		t.Errorf("--skip-files: %+v, want skipFiles true, 5 scored, 2 skipped", r)
	}

	run = runBomScore(cmd.BomScoreOptions{Format: "json"}, data)
	if r := parse(t, run.stdout); run.code != 0 || r.Options.SkipFiles || r.Input.ComponentsSkipped != 0 || r.Input.Components != 7 {
		t.Errorf("without the flag: exit %d %+v, want skipFiles false, 7 scored, 0 skipped", run.code, r)
	}

	run = runBomScore(cmd.BomScoreOptions{SkipFiles: true}, data)
	if run.code != 0 || !strings.HasPrefix(run.stdout, "options: --skip-files, 2 file components left out\ncisa-2026  ") {
		t.Errorf("text with --skip-files: exit %d\n%s", run.code, run.stdout)
	}
	run = runBomScore(cmd.BomScoreOptions{}, data)
	if run.code != 0 || strings.Contains(run.stdout, "options:") {
		t.Errorf("text without the flag: exit %d\n%s", run.code, run.stdout)
	}
}
