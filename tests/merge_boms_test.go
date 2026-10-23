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
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/relizaio/rearm/cmd"
)

// SCORE-67: rearm bomutils merge-boms folds same-key roots, applies rebom's licence rule
// ([] is absent; beside an expression the survivor's list stands), and prints one bounded
// merge-boms: line per loss (architecture-4 T-1 to T-8).

const mergeBomsFixtures = "merge_boms"

type mergeRun struct {
	code           int
	stdout, stderr string
}

// runMergeBoms fills what a case leaves empty with the command's flag defaults (FLAT,
// PRESERVE_UNDER_NEW_ROOT) and a name and version. A case that needs an empty value calls
// runMergeBomsRaw, which passes the options through untouched.
func runMergeBoms(opts cmd.MergeBomsOptions) mergeRun {
	if opts.RootComponentMode == "" {
		opts.RootComponentMode = "PRESERVE_UNDER_NEW_ROOT"
	}
	if opts.Structure == "" {
		opts.Structure = "FLAT"
	}
	if opts.Name == "" {
		opts.Name = "merged"
	}
	if opts.Version == "" {
		opts.Version = "1.0.0"
	}
	return runMergeBomsRaw(opts)
}

func runMergeBomsRaw(opts cmd.MergeBomsOptions) mergeRun {
	var stdout, stderr bytes.Buffer
	code := cmd.RunMergeBoms(opts, &stdout, &stderr)
	return mergeRun{code, stdout.String(), stderr.String()}
}

type obj = map[string]any

// licence lists of the design's test table (architecture-4 section 4); "A" is no licences key.
var mergeLicenceLists = map[string]string{
	"E":  `[]`,
	"L":  `[{"license":{"id":"MIT"}}]`,
	"L2": `[{"license":{"id":"Apache-2.0"}},{"license":{"name":"Custom"}}]`,
	"LC": `[{"license":{"id":"MIT","acknowledgement":"concluded"}}]`,
	"LB": `[{"license":{"id":"MIT","bom-ref":"lic-1"}}]`,
	"X":  `[{"expression":"MIT OR Apache-2.0"}]`,
	"X2": `[{"expression":"BSD-3-Clause OR MIT"}]`,
	"N":  `[{"expression":"NOASSERTION"}]`,
	// results of a union, survivor first
	"L+L2":  `[{"license":{"id":"MIT"}},{"license":{"id":"Apache-2.0"}},{"license":{"name":"Custom"}}]`,
	"L2+L":  `[{"license":{"id":"Apache-2.0"}},{"license":{"name":"Custom"}},{"license":{"id":"MIT"}}]`,
	"L+LC":  `[{"license":{"id":"MIT"}},{"license":{"id":"MIT","acknowledgement":"concluded"}}]`,
	"LC+L":  `[{"license":{"id":"MIT","acknowledgement":"concluded"}},{"license":{"id":"MIT"}}]`,
	"L+LB":  `[{"license":{"id":"MIT"}},{"license":{"id":"MIT","bom-ref":"lic-1"}}]`,
	"L+all": `[{"license":{"id":"MIT"}},{"license":{"id":"Apache-2.0"}},{"license":{"id":"BSD-3-Clause"}},{"license":{"id":"0BSD"}}]`,
}

// withLicences returns c with the named licence list set ("A" leaves the key out).
func withLicences(t *testing.T, c obj, list string) obj {
	t.Helper()
	if list == "A" {
		return c
	}
	raw, ok := mergeLicenceLists[list]
	if !ok {
		t.Fatalf("unknown licence list %q", list)
	}
	c["licenses"] = json.RawMessage(raw)
	return c
}

func mergeBomDoc(t *testing.T, root obj, components ...obj) []byte {
	t.Helper()
	doc := obj{"bomFormat": "CycloneDX", "specVersion": "1.6", "version": 1}
	if root != nil {
		doc["metadata"] = obj{"component": root}
	}
	if components != nil {
		doc["components"] = components
	}
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// writeMergeInputs writes each document to a file under a temporary directory.
func writeMergeInputs(t *testing.T, docs ...[]byte) []string {
	t.Helper()
	dir := t.TempDir()
	paths := make([]string, len(docs))
	for i, d := range docs {
		paths[i] = filepath.Join(dir, fmt.Sprintf("input-%d.cdx.json", i))
		if err := os.WriteFile(paths[i], d, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return paths
}

func appRoot(name string) obj {
	return obj{"type": "application", "bom-ref": name, "name": name, "version": "1.0.0", "purl": "pkg:generic/" + name + "@1.0.0"}
}

func decodeMerged(t *testing.T, out string) obj {
	t.Helper()
	var doc obj
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, out)
	}
	return doc
}

func topComponents(t *testing.T, doc obj) []obj {
	t.Helper()
	raw, _ := doc["components"].([]any)
	out := make([]obj, 0, len(raw))
	for _, r := range raw {
		out = append(out, r.(obj))
	}
	return out
}

func nestedComponents(c obj) []obj {
	raw, _ := c["components"].([]any)
	out := make([]obj, 0, len(raw))
	for _, r := range raw {
		out = append(out, r.(obj))
	}
	return out
}

// componentsWith returns the nodes among cs whose field equals value.
func componentsWith(cs []obj, field, value string) []obj {
	var out []obj
	for _, c := range cs {
		if s, _ := c[field].(string); s == value {
			out = append(out, c)
		}
	}
	return out
}

func onlyComponentWith(t *testing.T, cs []obj, field, value string) obj {
	t.Helper()
	found := componentsWith(cs, field, value)
	if len(found) != 1 {
		t.Fatalf("%d components with %s %q, want 1: %v", len(found), field, value, cs)
	}
	return found[0]
}

// assertLicences compares a node's licences with the named list ("A": no key).
func assertLicences(t *testing.T, label string, c obj, list string) {
	t.Helper()
	got, has := c["licenses"]
	if list == "A" {
		if has {
			t.Errorf("%s: licenses %v, want no licenses key", label, got)
		}
		return
	}
	if !has {
		t.Errorf("%s: no licenses key, want %s", label, mergeLicenceLists[list])
		return
	}
	var want any
	if err := json.Unmarshal([]byte(mergeLicenceLists[list]), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		gotJSON, _ := json.Marshal(got)
		t.Errorf("%s: licenses %s, want %s", label, gotJSON, mergeLicenceLists[list])
	}
}

func fieldStrings(c obj, field, sub string) []string {
	raw, _ := c[field].([]any)
	var out []string
	for _, r := range raw {
		m := r.(obj)
		out = append(out, fmt.Sprint(m[sub]))
	}
	return out
}

func mergedRootDependsOn(t *testing.T, doc obj) []string {
	t.Helper()
	root := doc["metadata"].(obj)["component"].(obj)["bom-ref"].(string)
	deps, _ := doc["dependencies"].([]any)
	for _, d := range deps {
		m := d.(obj)
		if m["ref"] == root {
			var out []string
			for _, r := range m["dependsOn"].([]any) {
				out = append(out, r.(string))
			}
			return out
		}
	}
	t.Fatalf("no dependency entry for the merged root %q", root)
	return nil
}

func dropLine(key, lost, kept string) string {
	return fmt.Sprintf("merge-boms: component %q: licences dropped beside the kept list: %s (kept: %s)\n", key, lost, kept)
}

// T-1: the licence matrix, FLAT, every S and D in {A, E, L, L2, X, X2, N}, plus the key cells.
func TestMergeBomsLicenceMatrix(t *testing.T) {
	const key = "pkg:npm/one@1.0.0"
	kL := `[id:"MIT"]`
	kL2 := `[id:"Apache-2.0" name:"Custom"]`
	kX := `[expression:"MIT OR Apache-2.0"]`
	kX2 := `[expression:"BSD-3-Clause OR MIT"]`
	kN := `[expression:"NOASSERTION"]`
	type cell struct {
		s, d, want, lost, kept string // lost == "": no line
	}
	cells := []cell{
		{"A", "A", "A", "", ""}, {"A", "E", "A", "", ""}, {"A", "L", "L", "", ""}, {"A", "L2", "L2", "", ""},
		{"A", "X", "X", "", ""}, {"A", "X2", "X2", "", ""}, {"A", "N", "N", "", ""},

		{"E", "A", "E", "", ""}, {"E", "E", "E", "", ""}, {"E", "L", "L", "", ""}, {"E", "L2", "L2", "", ""},
		{"E", "X", "X", "", ""}, {"E", "X2", "X2", "", ""}, {"E", "N", "N", "", ""},

		{"L", "A", "L", "", ""}, {"L", "E", "L", "", ""}, {"L", "L", "L", "", ""}, {"L", "L2", "L+L2", "", ""},
		{"L", "X", "L", kX, kL}, {"L", "X2", "L", kX2, kL}, {"L", "N", "L", kN, kL},

		{"L2", "A", "L2", "", ""}, {"L2", "E", "L2", "", ""}, {"L2", "L", "L2+L", "", ""}, {"L2", "L2", "L2", "", ""},
		{"L2", "X", "L2", kX, kL2}, {"L2", "X2", "L2", kX2, kL2}, {"L2", "N", "L2", kN, kL2},

		{"X", "A", "X", "", ""}, {"X", "E", "X", "", ""}, {"X", "L", "X", kL, kX}, {"X", "L2", "X", kL2, kX},
		{"X", "X", "X", "", ""}, {"X", "X2", "X", kX2, kX}, {"X", "N", "X", kN, kX},

		{"X2", "A", "X2", "", ""}, {"X2", "E", "X2", "", ""}, {"X2", "L", "X2", kL, kX2}, {"X2", "L2", "X2", kL2, kX2},
		{"X2", "X", "X2", kX, kX2}, {"X2", "X2", "X2", "", ""}, {"X2", "N", "X2", kN, kX2},

		{"N", "A", "N", "", ""}, {"N", "E", "N", "", ""}, {"N", "L", "N", kL, kN}, {"N", "L2", "N", kL2, kN},
		{"N", "X", "N", kX, kN}, {"N", "X2", "N", kX2, kN}, {"N", "N", "N", "", ""},

		// the key cells: acknowledgement and licence bom-ref are part of the key
		{"L", "LC", "L+LC", "", ""},
		{"LC", "L", "LC+L", "", ""},
		{"L", "LB", "L+LB", "", ""},
		{"LC", "LC", "LC", "", ""},
		{"X", "LC", "X", `[id:"MIT",acknowledgement:"concluded"]`, kX},
	}
	if len(cells) != 54 {
		t.Fatalf("%d cells, want the 49 of the matrix plus 5 key cells", len(cells))
	}
	for _, c := range cells {
		t.Run(c.s+","+c.d, func(t *testing.T) {
			one := func(list string) obj {
				return withLicences(t, obj{"type": "library", "bom-ref": "one", "name": "one", "version": "1.0.0", "purl": key}, list)
			}
			paths := writeMergeInputs(t,
				mergeBomDoc(t, appRoot("r0"), one(c.s)),
				mergeBomDoc(t, appRoot("r1"), one(c.d)))
			run := runMergeBoms(cmd.MergeBomsOptions{InputFiles: paths})
			if run.code != 0 {
				t.Fatalf("exit %d, stderr %q", run.code, run.stderr)
			}
			node := onlyComponentWith(t, topComponents(t, decodeMerged(t, run.stdout)), "purl", key)
			assertLicences(t, "survivor", node, c.want)
			wantErr := ""
			if c.lost != "" {
				wantErr = dropLine(key, c.lost, c.kept)
			}
			if run.stderr != wantErr {
				t.Errorf("stderr %q, want %q", run.stderr, wantErr)
			}
		})
	}
}

// t2Inputs are T-2's three inputs; secondRef is the second root's bom-ref (T-3 changes it).
func t2Inputs(t *testing.T, secondRef string) []string {
	t.Helper()
	first := withLicences(t, obj{
		"type": "application", "bom-ref": "app", "name": "app", "version": "1.0.0", "purl": "pkg:generic/app@1.0.0",
		"supplier":    obj{"name": "S1"},
		"description": "d1",
		"hashes":      []obj{{"alg": "SHA-256", "content": strings.Repeat("1", 64)}},
		"components":  []obj{{"type": "library", "bom-ref": "nested-1", "name": "nested", "version": "1"}},
	}, "L")
	second := withLicences(t, obj{
		"type": "application", "bom-ref": secondRef, "name": "app", "version": "1.0.0", "purl": "pkg:generic/app@1.0.0",
		"supplier":    obj{"name": "S2"},
		"description": "d2",
		"hashes":      []obj{{"alg": "SHA-512", "content": strings.Repeat("2", 128)}},
	}, "X2")
	listing := withLicences(t, obj{
		"type": "library", "bom-ref": "app-listing", "name": "app-listing", "version": "1.0.0", "purl": "pkg:generic/app@1.0.0",
		"properties": []obj{{"name": "p", "value": "pv"}},
	}, "L2")
	third := obj{"type": "application", "bom-ref": "third", "name": "third", "version": "1.0.0", "purl": "pkg:generic/third@1.0.0"}
	return writeMergeInputs(t,
		mergeBomDoc(t, first),
		mergeBomDoc(t, second),
		mergeBomDoc(t, third, listing))
}

const t2FoldLine = `merge-boms: input 1 root "app" folds into root of input 0 (key "pkg:generic/app@1.0.0"); fields outside the fold (supplier, description, nested components) are the survivor's` + "\n"
const t2DropLine = `merge-boms: component "pkg:generic/app@1.0.0": licences dropped beside the kept list: [expression:"BSD-3-Clause OR MIT"] (kept: [id:"MIT"])` + "\n"

// T-2: a same-key root folds into the first; it no longer replaces it.
func TestMergeBomsRootFold(t *testing.T) {
	run := runMergeBoms(cmd.MergeBomsOptions{InputFiles: t2Inputs(t, "app")})
	if run.code != 0 {
		t.Fatalf("exit %d, stderr %q", run.code, run.stderr)
	}
	doc := decodeMerged(t, run.stdout)
	cs := topComponents(t, doc)
	node := onlyComponentWith(t, cs, "purl", "pkg:generic/app@1.0.0")
	if got := node["supplier"].(obj)["name"]; got != "S1" {
		t.Errorf("supplier %v, want S1 (the survivor's)", got)
	}
	if node["description"] != "d1" {
		t.Errorf("description %v, want d1", node["description"])
	}
	if node["bom-ref"] != "app" {
		t.Errorf("bom-ref %v, want app", node["bom-ref"])
	}
	if nested := nestedComponents(node); len(nested) != 1 || nested[0]["bom-ref"] != "nested-1" {
		t.Errorf("nested components %v, want the survivor's nested-1", nested)
	}
	assertLicences(t, "app", node, "L+L2")
	if got, want := fieldStrings(node, "hashes", "alg"), []string{"SHA-256", "SHA-512"}; !reflect.DeepEqual(got, want) {
		t.Errorf("hashes %v, want %v", got, want)
	}
	if got, want := fieldStrings(node, "properties", "name"), []string{"p"}; !reflect.DeepEqual(got, want) {
		t.Errorf("properties %v, want %v", got, want)
	}
	if len(cs) != 2 {
		t.Errorf("%d top-level components, want app and third", len(cs))
	}
	if want := t2FoldLine + t2DropLine; run.stderr != want {
		t.Errorf("stderr %q, want %q", run.stderr, want)
	}
	if got, want := mergedRootDependsOn(t, doc), []string{"app", "third"}; !reflect.DeepEqual(got, want) {
		t.Errorf("merged root dependsOn %v, want %v (each root bom-ref once)", got, want)
	}
}

// T-3: two roots of one purl with different bom-refs; the folded root's bom-ref is not
// re-pointed and stays listed under the merged root.
func TestMergeBomsRootFoldDifferentBomRefNotRepointed(t *testing.T) {
	paths := t2Inputs(t, "app-2")[:2]
	run := runMergeBoms(cmd.MergeBomsOptions{InputFiles: paths})
	if run.code != 0 {
		t.Fatalf("exit %d, stderr %q", run.code, run.stderr)
	}
	doc := decodeMerged(t, run.stdout)
	cs := topComponents(t, doc)
	if len(cs) != 1 || cs[0]["bom-ref"] != "app" {
		t.Fatalf("components %v, want one node with bom-ref app", cs)
	}
	if got, want := mergedRootDependsOn(t, doc), []string{"app", "app-2"}; !reflect.DeepEqual(got, want) {
		t.Errorf("merged root dependsOn %v, want %v", got, want)
	}
	wantFold := `merge-boms: input 1 root "app-2" folds into root of input 0 (key "pkg:generic/app@1.0.0"); fields outside the fold (supplier, description, nested components) are the survivor's` + "\n"
	if run.stderr != wantFold+t2DropLine {
		t.Errorf("stderr %q, want %q", run.stderr, wantFold+t2DropLine)
	}
	if strings.Contains(run.stderr, `"app"`) {
		t.Errorf("stderr names the survivor's bom-ref %q: %q", "app", run.stderr)
	}
}

// T-4 (a): HIERARCHICAL folds nothing.
func TestMergeBomsHierarchicalFoldsNothing(t *testing.T) {
	run := runMergeBoms(cmd.MergeBomsOptions{InputFiles: t2Inputs(t, "app"), Structure: "HIERARCHICAL"})
	if run.code != 0 || run.stderr != "" {
		t.Fatalf("exit %d, stderr %q; want 0 and nothing", run.code, run.stderr)
	}
	doc := decodeMerged(t, run.stdout)
	cs := topComponents(t, doc)
	if len(cs) != 3 {
		t.Fatalf("%d top-level entries, want 3", len(cs))
	}
	first, second, third := cs[0], cs[1], cs[2]
	if first["supplier"].(obj)["name"] != "S1" || first["description"] != "d1" {
		t.Errorf("first root %v, want S1 and d1", first)
	}
	assertLicences(t, "first root", first, "L")
	if got := fieldStrings(first, "hashes", "alg"); !reflect.DeepEqual(got, []string{"SHA-256"}) {
		t.Errorf("first root hashes %v, want SHA-256 only", got)
	}
	if nested := nestedComponents(first); len(nested) != 1 || nested[0]["bom-ref"] != "nested-1" {
		t.Errorf("first root nested %v, want nested-1", nested)
	}
	if second["supplier"].(obj)["name"] != "S2" || second["description"] != "d2" {
		t.Errorf("second root %v, want S2 and d2", second)
	}
	assertLicences(t, "second root", second, "X2")
	if got := fieldStrings(second, "hashes", "alg"); !reflect.DeepEqual(got, []string{"SHA-512"}) {
		t.Errorf("second root hashes %v, want SHA-512 only", got)
	}
	if third["bom-ref"] != "third" {
		t.Fatalf("third entry %v, want root third", third)
	}
	for _, f := range []string{"licenses", "hashes", "properties"} {
		if _, has := third[f]; has {
			t.Errorf("third root carries %s %v, want none", f, third[f])
		}
	}
	nested := nestedComponents(third)
	if len(nested) != 1 || nested[0]["bom-ref"] != "app-listing" {
		t.Fatalf("third root nested %v, want app-listing", nested)
	}
	assertLicences(t, "app-listing", nested[0], "L2")
	if got := fieldStrings(nested[0], "properties", "name"); !reflect.DeepEqual(got, []string{"p"}) {
		t.Errorf("app-listing properties %v, want p", got)
	}
	if got, want := mergedRootDependsOn(t, doc), []string{"app", "third"}; !reflect.DeepEqual(got, want) {
		t.Errorf("merged root dependsOn %v, want %v", got, want)
	}
}

func libComponent(t *testing.T, ref, list, alg, content string) obj {
	return withLicences(t, obj{
		"type": "library", "bom-ref": ref, "name": ref, "version": "1.0.0", "purl": "pkg:npm/lib@1.0.0",
		"hashes": []obj{{"alg": alg, "content": content}},
	}, list)
}

// T-4 (b): case (iv), same-key top-level components of two inputs or of one input, under
// HIERARCHICAL: each nested copy carries exactly its own fields.
func TestMergeBomsHierarchicalSharedLibraryNoLeak(t *testing.T) {
	h1, h2 := strings.Repeat("1", 64), strings.Repeat("2", 128)
	check := func(t *testing.T, c obj, ref, list, alg string) {
		t.Helper()
		if c["bom-ref"] != ref {
			t.Fatalf("nested %v, want %s", c, ref)
		}
		assertLicences(t, ref, c, list)
		if got := fieldStrings(c, "hashes", "alg"); !reflect.DeepEqual(got, []string{alg}) {
			t.Errorf("%s hashes %v, want %s only", ref, got, alg)
		}
	}
	t.Run("two inputs", func(t *testing.T) {
		paths := writeMergeInputs(t,
			mergeBomDoc(t, appRoot("r0"), libComponent(t, "lib", "L", "SHA-256", h1)),
			mergeBomDoc(t, appRoot("r1"), libComponent(t, "lib", "L2", "SHA-512", h2)))
		run := runMergeBoms(cmd.MergeBomsOptions{InputFiles: paths, Structure: "HIERARCHICAL"})
		if run.code != 0 || run.stderr != "" {
			t.Fatalf("exit %d, stderr %q; want 0 and nothing", run.code, run.stderr)
		}
		cs := topComponents(t, decodeMerged(t, run.stdout))
		if len(cs) != 2 {
			t.Fatalf("%d top-level entries, want 2", len(cs))
		}
		n0, n1 := nestedComponents(cs[0]), nestedComponents(cs[1])
		if len(n0) != 1 || len(n1) != 1 {
			t.Fatalf("nested %v / %v, want one each", n0, n1)
		}
		check(t, n0[0], "lib", "L", "SHA-256")
		check(t, n1[0], "lib", "L2", "SHA-512")
	})
	t.Run("one input", func(t *testing.T) {
		paths := writeMergeInputs(t, mergeBomDoc(t, appRoot("r0"),
			libComponent(t, "lib", "L", "SHA-256", h1),
			libComponent(t, "lib-dup", "L2", "SHA-512", h2)))
		run := runMergeBoms(cmd.MergeBomsOptions{InputFiles: paths, Structure: "HIERARCHICAL"})
		if run.code != 0 || run.stderr != "" {
			t.Fatalf("exit %d, stderr %q; want 0 and nothing", run.code, run.stderr)
		}
		cs := topComponents(t, decodeMerged(t, run.stdout))
		if len(cs) != 1 {
			t.Fatalf("%d top-level entries, want 1", len(cs))
		}
		n := nestedComponents(cs[0])
		if len(n) != 2 {
			t.Fatalf("nested %v, want lib and lib-dup", n)
		}
		check(t, n[0], "lib", "L", "SHA-256")
		check(t, n[1], "lib-dup", "L2", "SHA-512")
	})
}

var serialNumberPattern = regexp.MustCompile(`"serialNumber":"[^"]*"`)

func blankSerial(b []byte) []byte {
	return serialNumberPattern.ReplaceAll(b, []byte(`"serialNumber":""`))
}

// T-4 (c): disjoint keys, every component identified: HIERARCHICAL output is byte for byte
// what the unchanged code wrote. merge_boms/hierarchical.golden.json was recorded from the
// branch head before this change (rearm-cli 2026-10-sbom-score e0ee130) with
// `go run . bomutils merge-boms --structure HIERARCHICAL --name merged --version 1.0.0
// --input-files tests/merge_boms/hier-a.cdx.json,tests/merge_boms/hier-b.cdx.json
// --outfile tests/merge_boms/hierarchical.golden.json`.
func TestMergeBomsHierarchicalGolden(t *testing.T) {
	run := runMergeBoms(cmd.MergeBomsOptions{
		Structure:  "HIERARCHICAL",
		InputFiles: []string{filepath.Join(mergeBomsFixtures, "hier-a.cdx.json"), filepath.Join(mergeBomsFixtures, "hier-b.cdx.json")},
	})
	if run.code != 0 || run.stderr != "" {
		t.Fatalf("exit %d, stderr %q; want 0 and nothing", run.code, run.stderr)
	}
	golden, err := os.ReadFile(filepath.Join(mergeBomsFixtures, "hierarchical.golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := blankSerial([]byte(run.stdout)), blankSerial(golden); !bytes.Equal(got, want) {
		t.Errorf("output differs from the golden:\n got %s\nwant %s", got, want)
	}
}

// T-5: [] round-trips, and a [] root takes the folded root's expression.
func TestMergeBomsEmptyLicencesRoundTrip(t *testing.T) {
	paths := writeMergeInputs(t, mergeBomDoc(t, appRoot("r0"),
		withLicences(t, obj{"type": "library", "bom-ref": "one", "name": "one", "version": "1.0.0", "purl": "pkg:npm/one@1.0.0"}, "E")))
	run := runMergeBoms(cmd.MergeBomsOptions{InputFiles: paths})
	if run.code != 0 || run.stderr != "" {
		t.Fatalf("exit %d, stderr %q; want 0 and nothing", run.code, run.stderr)
	}
	if !strings.Contains(run.stdout, `"purl":"pkg:npm/one@1.0.0"`) || !strings.Contains(run.stdout, `"licenses":[]`) {
		t.Errorf("output %s, want the component with \"licenses\":[]", run.stdout)
	}

	root := func(list string) obj {
		return withLicences(t, obj{"type": "application", "bom-ref": "app", "name": "app", "version": "1.0.0", "purl": "pkg:generic/app@1.0.0"}, list)
	}
	paths = writeMergeInputs(t, mergeBomDoc(t, root("E")), mergeBomDoc(t, root("X2")))
	run = runMergeBoms(cmd.MergeBomsOptions{InputFiles: paths})
	if run.code != 0 {
		t.Fatalf("exit %d, stderr %q", run.code, run.stderr)
	}
	node := onlyComponentWith(t, topComponents(t, decodeMerged(t, run.stdout)), "purl", "pkg:generic/app@1.0.0")
	assertLicences(t, "survivor root", node, "X2")
	if run.stderr != t2FoldLine {
		t.Errorf("stderr %q, want the fold line alone %q", run.stderr, t2FoldLine)
	}
}

// T-6 (a): a clean merge prints nothing.
func TestMergeBomsCleanMergeSilent(t *testing.T) {
	run := runMergeBoms(cmd.MergeBomsOptions{
		InputFiles: []string{filepath.Join(mergeBomsFixtures, "hier-a.cdx.json"), filepath.Join(mergeBomsFixtures, "hier-b.cdx.json")},
	})
	if run.code != 0 || run.stderr != "" {
		t.Errorf("exit %d, stderr %q; want 0 and nothing", run.code, run.stderr)
	}
	if n := len(topComponents(t, decodeMerged(t, run.stdout))); n != 5 {
		t.Errorf("%d top-level components, want 5 (two roots, three components)", n)
	}
}

// T-6 (b): a bom-ref with a newline stays on one line.
func TestMergeBomsQuotedKeyOneLine(t *testing.T) {
	c := func(list string) obj {
		return withLicences(t, obj{"type": "library", "bom-ref": "a\nb", "name": "ab", "version": "1"}, list)
	}
	paths := writeMergeInputs(t, mergeBomDoc(t, appRoot("r0"), c("X")), mergeBomDoc(t, appRoot("r1"), c("L")))
	run := runMergeBoms(cmd.MergeBomsOptions{InputFiles: paths})
	if run.code != 0 {
		t.Fatalf("exit %d, stderr %q", run.code, run.stderr)
	}
	if strings.Count(run.stderr, "\n") != 1 || !strings.HasSuffix(run.stderr, "\n") {
		t.Errorf("stderr %q, want exactly one line", run.stderr)
	}
	if !strings.Contains(run.stderr, `component "a\nb":`) {
		t.Errorf("stderr %q, want the key quoted as Go %%q writes it", run.stderr)
	}
}

// T-6 (c): the hash and property mismatch lines.
func TestMergeBomsMismatchLines(t *testing.T) {
	h1, h2, h3 := strings.Repeat("a1", 32), strings.Repeat("b2", 32), strings.Repeat("c3", 64)
	survivor := obj{"type": "library", "bom-ref": "one", "name": "one", "version": "1.0.0", "purl": "pkg:npm/one@1.0.0",
		"hashes":     []obj{{"alg": "SHA-256", "content": h1}},
		"properties": []obj{{"name": "p", "value": "a"}}}
	dup := obj{"type": "library", "bom-ref": "one", "name": "one", "version": "1.0.0", "purl": "pkg:npm/one@1.0.0",
		"hashes":     []obj{{"alg": "SHA-256", "content": h2}, {"alg": "SHA-512", "content": h3}},
		"properties": []obj{{"name": "p", "value": "b"}}}
	paths := writeMergeInputs(t, mergeBomDoc(t, appRoot("r0"), survivor), mergeBomDoc(t, appRoot("r1"), dup))
	run := runMergeBoms(cmd.MergeBomsOptions{InputFiles: paths})
	if run.code != 0 {
		t.Fatalf("exit %d, stderr %q", run.code, run.stderr)
	}
	want := `merge-boms: component "pkg:npm/one@1.0.0": hash "SHA-256" dropped beside the kept value: "` + h2 + `" (kept: "` + h1 + `")` + "\n" +
		`merge-boms: component "pkg:npm/one@1.0.0": property "p" dropped beside the kept value: "b" (kept: "a")` + "\n"
	if run.stderr != want {
		t.Errorf("stderr %q, want %q", run.stderr, want)
	}
	if strings.Contains(run.stderr, "Warning:") || regexp.MustCompile(`\d{4}/\d\d/\d\d`).MatchString(run.stderr) {
		t.Errorf("stderr %q carries Warning: or a timestamp", run.stderr)
	}
	node := onlyComponentWith(t, topComponents(t, decodeMerged(t, run.stdout)), "purl", "pkg:npm/one@1.0.0")
	if got, want := fieldStrings(node, "hashes", "content"), []string{h1, h3}; !reflect.DeepEqual(got, want) {
		t.Errorf("hashes %v, want %v", got, want)
	}
	if got := fieldStrings(node, "properties", "value"); !reflect.DeepEqual(got, []string{"a"}) {
		t.Errorf("property values %v, want [a]", got)
	}
}

// T-6 (d): a keyless top-level component warns under FLAT only.
func TestMergeBomsKeylessComponentWarnsFlatOnly(t *testing.T) {
	paths := writeMergeInputs(t, mergeBomDoc(t, appRoot("r0"), obj{"type": "library", "name": "nokey", "version": "1"}))
	run := runMergeBoms(cmd.MergeBomsOptions{InputFiles: paths})
	want := "Warning: Dropping component 'nokey' (type: library) - missing both PackageURL and BOMRef\n" +
		"Warning: Dropped 1 component(s) without identifiers\n"
	if run.code != 0 || run.stderr != want {
		t.Errorf("FLAT: exit %d, stderr %q; want 0 and %q", run.code, run.stderr, want)
	}
	run = runMergeBoms(cmd.MergeBomsOptions{InputFiles: paths, Structure: "HIERARCHICAL"})
	if run.code != 0 || run.stderr != "" {
		t.Errorf("HIERARCHICAL: exit %d, stderr %q; want 0 and nothing", run.code, run.stderr)
	}
}

// T-6 (e): a keyless root is dropped under FLAT with a line, kept under HIERARCHICAL.
func TestMergeBomsKeylessRoot(t *testing.T) {
	child := obj{"type": "library", "bom-ref": "child", "name": "child", "version": "1", "purl": "pkg:npm/child@1"}
	for _, tc := range []struct{ name, shown string }{{"keyless", `"keyless"`}, {"", `"<unnamed>"`}} {
		keyless := obj{"type": "application", "version": "1"}
		if tc.name != "" {
			keyless["name"] = tc.name
		}
		paths := writeMergeInputs(t, mergeBomDoc(t, appRoot("r0")), mergeBomDoc(t, keyless, child))
		run := runMergeBoms(cmd.MergeBomsOptions{InputFiles: paths})
		if want := "merge-boms: input 1 root " + tc.shown + " has neither purl nor bom-ref and is dropped\n"; run.code != 0 || run.stderr != want {
			t.Errorf("FLAT %q: exit %d, stderr %q; want 0 and %q", tc.name, run.code, run.stderr, want)
		}
		cs := topComponents(t, decodeMerged(t, run.stdout))
		if tc.name != "" && len(componentsWith(cs, "name", tc.name)) != 0 {
			t.Errorf("FLAT: components %v still hold the keyless root", cs)
		}
		if len(cs) != 2 || len(componentsWith(cs, "bom-ref", "child")) != 1 {
			t.Errorf("FLAT: components %v, want r0 and child", cs)
		}

		run = runMergeBoms(cmd.MergeBomsOptions{InputFiles: paths, Structure: "HIERARCHICAL"})
		if run.code != 0 || run.stderr != "" {
			t.Errorf("HIERARCHICAL %q: exit %d, stderr %q; want 0 and nothing", tc.name, run.code, run.stderr)
		}
		cs = topComponents(t, decodeMerged(t, run.stdout))
		if len(cs) != 2 || cs[1]["name"] != tc.name || cs[1]["purl"] != nil {
			t.Fatalf("HIERARCHICAL: entries %v, want the keyless root second", cs)
		}
		if n := nestedComponents(cs[1]); len(n) != 1 || n[0]["bom-ref"] != "child" {
			t.Errorf("HIERARCHICAL: keyless root nests %v, want child", n)
		}
	}
}

// dupComponents builds n top-level components with one bom-ref, no purl, one name and versions
// 0 to n-1, the shape rebom's upload de-dup keeps; first and rest set the extra fields.
func dupComponents(n int, first, rest obj) []obj {
	out := make([]obj, n)
	for i := range out {
		c := obj{"type": "library", "bom-ref": "dup", "name": "dup", "version": fmt.Sprint(i)}
		extra := rest
		if i == 0 {
			extra = first
		}
		for k, v := range extra {
			c[k] = v
		}
		out[i] = c
	}
	return out
}

// T-6 (f): the kept licence list is capped at four keys.
func TestMergeBomsKeptLicenceCountCap(t *testing.T) {
	var ids []string
	for i := 1; i <= 40; i++ {
		ids = append(ids, fmt.Sprintf(`{"license":{"id":"L%d"}}`, i))
	}
	first := obj{"licenses": json.RawMessage("[" + strings.Join(ids, ",") + "]")}
	rest := obj{"licenses": json.RawMessage(mergeLicenceLists["X"])}
	paths := writeMergeInputs(t, mergeBomDoc(t, appRoot("r0"), dupComponents(200, first, rest)...))
	run := runMergeBoms(cmd.MergeBomsOptions{InputFiles: paths})
	if run.code != 0 {
		t.Fatalf("exit %d", run.code)
	}
	node := onlyComponentWith(t, topComponents(t, decodeMerged(t, run.stdout)), "bom-ref", "dup")
	if got := fieldStrings(node, "licenses", "license"); len(got) != 40 {
		t.Errorf("%d licences on dup, want the 40 ids", len(got))
	}
	line := dropLine("dup", `[expression:"MIT OR Apache-2.0"]`, `[id:"L1" id:"L2" id:"L3" id:"L4" +36 more]`)
	if want := strings.Repeat(line, 199); run.stderr != want {
		t.Errorf("stderr is not 199 copies of %q; got %d bytes, %d lines", line, len(run.stderr), strings.Count(run.stderr, "\n"))
	}
	if len(run.stderr) >= 199*256 {
		t.Errorf("stderr %d bytes, want under %d", len(run.stderr), 199*256)
	}
}

// T-6 (g): the kept string byte cap, the uncapped lost list and the rune boundary.
func TestMergeBomsKeptStringByteCap(t *testing.T) {
	const key = "pkg:npm/one@1.0.0"
	expr := func(e string) json.RawMessage {
		b, _ := json.Marshal([]obj{{"expression": e}})
		return b
	}
	run2 := func(t *testing.T, s, d json.RawMessage) string {
		t.Helper()
		c := func(l json.RawMessage) obj {
			return obj{"type": "library", "bom-ref": "one", "name": "one", "version": "1.0.0", "purl": key, "licenses": l}
		}
		paths := writeMergeInputs(t, mergeBomDoc(t, appRoot("r0"), c(s)), mergeBomDoc(t, appRoot("r1"), c(d)))
		run := runMergeBoms(cmd.MergeBomsOptions{InputFiles: paths})
		if run.code != 0 {
			t.Fatalf("exit %d, stderr %q", run.code, run.stderr)
		}
		return run.stderr
	}
	long := strings.Repeat("A", 300)
	L := json.RawMessage(mergeLicenceLists["L"])
	X := json.RawMessage(mergeLicenceLists["X"])

	if got, want := run2(t, expr(long), L), dropLine(key, `[id:"MIT"]`, `[expression:"`+strings.Repeat("A", 96)+`"(+204 bytes)]`); got != want {
		t.Errorf("kept cap: stderr %q, want %q", got, want)
	}
	if got, want := run2(t, X, expr(long)), dropLine(key, `[expression:"`+long+`"]`, `[expression:"MIT OR Apache-2.0"]`); got != want {
		t.Errorf("lost uncapped: stderr %q, want %q", got, want)
	}
	runes := "a" + strings.Repeat("\u20ac", 40) // 121 bytes; byte 96 falls inside a rune
	if got, want := run2(t, expr(runes), L), dropLine(key, `[id:"MIT"]`, `[expression:"a`+strings.Repeat("\u20ac", 31)+`"(+27 bytes)]`); got != want {
		t.Errorf("rune boundary: stderr %q, want %q", got, want)
	}
}

// T-6 (h): each drop line shows the kept list as it stood at that fold.
func TestMergeBomsKeptListGrowth(t *testing.T) {
	c := func(i int, licences string) obj {
		return obj{"type": "library", "bom-ref": "dup", "name": "dup", "version": fmt.Sprint(i), "licenses": json.RawMessage(licences)}
	}
	id := func(s string) string { return `[{"license":{"id":"` + s + `"}}]` }
	x2 := mergeLicenceLists["X2"]
	paths := writeMergeInputs(t, mergeBomDoc(t, appRoot("r0"),
		c(0, mergeLicenceLists["L"]), c(1, id("Apache-2.0")), c(2, x2), c(3, id("BSD-3-Clause")), c(4, x2), c(5, id("0BSD"))))
	run := runMergeBoms(cmd.MergeBomsOptions{InputFiles: paths})
	if run.code != 0 {
		t.Fatalf("exit %d", run.code)
	}
	want := dropLine("dup", `[expression:"BSD-3-Clause OR MIT"]`, `[id:"MIT" id:"Apache-2.0"]`) +
		dropLine("dup", `[expression:"BSD-3-Clause OR MIT"]`, `[id:"MIT" id:"Apache-2.0" id:"BSD-3-Clause"]`)
	if run.stderr != want {
		t.Errorf("stderr %q, want %q", run.stderr, want)
	}
	node := onlyComponentWith(t, topComponents(t, decodeMerged(t, run.stdout)), "bom-ref", "dup")
	assertLicences(t, "dup", node, "L+all")
}

// T-6 (i): the kept hash and property values are capped at 96 bytes.
func TestMergeBomsKeptValueCap(t *testing.T) {
	first := obj{"properties": []obj{{"name": "p", "value": strings.Repeat("A", 10000)}},
		"hashes": []obj{{"alg": "SHA-256", "content": strings.Repeat("a", 10000)}}}
	rest := obj{"properties": []obj{{"name": "p", "value": "v"}},
		"hashes": []obj{{"alg": "SHA-256", "content": "b"}}}
	paths := writeMergeInputs(t, mergeBomDoc(t, appRoot("r0"), dupComponents(50, first, rest)...))
	run := runMergeBoms(cmd.MergeBomsOptions{InputFiles: paths})
	if run.code != 0 {
		t.Fatalf("exit %d", run.code)
	}
	node := onlyComponentWith(t, topComponents(t, decodeMerged(t, run.stdout)), "bom-ref", "dup")
	if got := fieldStrings(node, "properties", "value"); len(got) != 1 || got[0] != strings.Repeat("A", 10000) {
		t.Errorf("dup properties: %d values, want the survivor's one", len(got))
	}
	if got := fieldStrings(node, "hashes", "content"); len(got) != 1 || got[0] != strings.Repeat("a", 10000) {
		t.Errorf("dup hashes: %d values, want the survivor's one", len(got))
	}
	hashLine := `merge-boms: component "dup": hash "SHA-256" dropped beside the kept value: "b" (kept: "` + strings.Repeat("a", 96) + `"(+9904 bytes))` + "\n"
	propLine := `merge-boms: component "dup": property "p" dropped beside the kept value: "v" (kept: "` + strings.Repeat("A", 96) + `"(+9904 bytes))` + "\n"
	if want := strings.Repeat(hashLine+propLine, 49); run.stderr != want {
		t.Errorf("stderr is not 49 hash and property line pairs; got %d bytes, %d lines", len(run.stderr), strings.Count(run.stderr, "\n"))
	}
	if len(run.stderr) >= 98*256 {
		t.Errorf("stderr %d bytes, want under %d", len(run.stderr), 98*256)
	}
}

// T-6 (j): the hash and property lines come once per entry of the duplicate, so they show
// the key through boundedQuote; the licence line comes at most once per fold of a node and
// keeps the key whole.
func TestMergeBomsMismatchLineKeyBounded(t *testing.T) {
	longRef := strings.Repeat("k", 5000)
	node := func(extra obj) obj {
		c := obj{"type": "library", "bom-ref": longRef, "name": "k", "version": "1"}
		for k, v := range extra {
			c[k] = v
		}
		return c
	}
	t.Run("property line bounds the key", func(t *testing.T) {
		props := make([]obj, 200)
		for i := range props {
			props[i] = obj{"name": "p", "value": fmt.Sprint("b", i)}
		}
		survivor := node(obj{"properties": []obj{{"name": "p", "value": "a"}}})
		dup := node(obj{"properties": props})
		paths := writeMergeInputs(t, mergeBomDoc(t, appRoot("r0"), survivor), mergeBomDoc(t, appRoot("r1"), dup))
		run := runMergeBoms(cmd.MergeBomsOptions{InputFiles: paths})
		if run.code != 0 {
			t.Fatalf("exit %d", run.code)
		}
		lines := strings.Split(strings.TrimSuffix(run.stderr, "\n"), "\n")
		if len(lines) != 200 {
			t.Fatalf("%d stderr lines, want 200", len(lines))
		}
		shownKey := `"` + strings.Repeat("k", 96) + `"(+4904 bytes)`
		if want := `merge-boms: component ` + shownKey + `: property "p" dropped beside the kept value: "b0" (kept: "a")`; lines[0] != want {
			t.Errorf("first line %q, want %q", lines[0], want)
		}
		if len(run.stderr) >= 200*256 {
			t.Errorf("stderr %d bytes, want under %d", len(run.stderr), 200*256)
		}
		merged := onlyComponentWith(t, topComponents(t, decodeMerged(t, run.stdout)), "bom-ref", longRef)
		if got := fieldStrings(merged, "properties", "name"); !reflect.DeepEqual(got, []string{"p"}) {
			t.Errorf("property names %v, want [p]", got)
		}
		if got := fieldStrings(merged, "properties", "value"); !reflect.DeepEqual(got, []string{"a"}) {
			t.Errorf("property values %v, want [a]", got)
		}
	})
	t.Run("hash line bounds the key", func(t *testing.T) {
		hashes := make([]obj, 200)
		for i := range hashes {
			hashes[i] = obj{"alg": "SHA-256", "content": fmt.Sprint("h", i)}
		}
		survivor := node(obj{"hashes": []obj{{"alg": "SHA-256", "content": "a"}}})
		dup := node(obj{"hashes": hashes})
		paths := writeMergeInputs(t, mergeBomDoc(t, appRoot("r0"), survivor), mergeBomDoc(t, appRoot("r1"), dup))
		run := runMergeBoms(cmd.MergeBomsOptions{InputFiles: paths})
		if run.code != 0 {
			t.Fatalf("exit %d", run.code)
		}
		lines := strings.Split(strings.TrimSuffix(run.stderr, "\n"), "\n")
		if len(lines) != 200 {
			t.Fatalf("%d stderr lines, want 200", len(lines))
		}
		shownKey := `"` + strings.Repeat("k", 96) + `"(+4904 bytes)`
		if want := `merge-boms: component ` + shownKey + `: hash "SHA-256" dropped beside the kept value: "h0" (kept: "a")`; lines[0] != want {
			t.Errorf("first line %q, want %q", lines[0], want)
		}
		if len(run.stderr) >= 200*256 {
			t.Errorf("stderr %d bytes, want under %d", len(run.stderr), 200*256)
		}
	})
	t.Run("licence line keeps the key whole", func(t *testing.T) {
		survivor := withLicences(t, node(nil), "X")
		dup := withLicences(t, node(nil), "L")
		paths := writeMergeInputs(t, mergeBomDoc(t, appRoot("r0"), survivor), mergeBomDoc(t, appRoot("r1"), dup))
		run := runMergeBoms(cmd.MergeBomsOptions{InputFiles: paths})
		if run.code != 0 {
			t.Fatalf("exit %d", run.code)
		}
		want := dropLine(longRef, `[id:"MIT"]`, `[expression:"MIT OR Apache-2.0"]`)
		if run.stderr != want {
			t.Errorf("stderr %d bytes %.200q..., want exactly the one line with the whole key (%d bytes)", len(run.stderr), run.stderr, len(want))
		}
		merged := onlyComponentWith(t, topComponents(t, decodeMerged(t, run.stdout)), "bom-ref", longRef)
		assertLicences(t, "survivor", merged, "X")
	})
}

// T-7: every error path returns 1 with its message, writes nothing to stdout and does not exit.
func TestMergeBomsExitPaths(t *testing.T) {
	good := writeMergeInputs(t, mergeBomDoc(t, appRoot("r0")))[0]
	bad := filepath.Join(t.TempDir(), "bad.cdx.json")
	if err := os.WriteFile(bad, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(t.TempDir(), "missing.cdx.json")
	full := cmd.MergeBomsOptions{Structure: "FLAT", Name: "merged", Version: "1.0.0", RootComponentMode: "PRESERVE_UNDER_NEW_ROOT", InputFiles: []string{good}}
	for _, tc := range []struct {
		name   string
		edit   func(o *cmd.MergeBomsOptions)
		prefix string
	}{
		{"no input files", func(o *cmd.MergeBomsOptions) { o.InputFiles = nil }, "Error: --input-files is required\n"},
		{"no name", func(o *cmd.MergeBomsOptions) { o.Name = "" }, "Error: --name is required\n"},
		{"no version", func(o *cmd.MergeBomsOptions) { o.Version = "" }, "Error: --version is required\n"},
		{"unreadable path", func(o *cmd.MergeBomsOptions) { o.InputFiles = []string{good, missing} }, "Error reading file " + missing + ": "},
		{"undecodable file", func(o *cmd.MergeBomsOptions) { o.InputFiles = []string{bad} }, "Error decoding file " + bad + ": "},
		{"unknown mode", func(o *cmd.MergeBomsOptions) { o.RootComponentMode = "SIDEWAYS" }, "Error: Unknown MergeRootComponentMode: SIDEWAYS\n"},
		{"empty mode", func(o *cmd.MergeBomsOptions) { o.RootComponentMode = "" }, "Error: Unknown MergeRootComponentMode: \n"},
	} {
		opts := full
		tc.edit(&opts)
		run := runMergeBomsRaw(opts)
		if run.code != 1 || run.stdout != "" || !strings.HasPrefix(run.stderr, tc.prefix) || strings.Count(run.stderr, "\n") != 1 {
			t.Errorf("%s: exit %d, stdout %q, stderr %q; want 1, nothing, one line starting %q", tc.name, run.code, run.stdout, run.stderr, tc.prefix)
		}
	}

	out := filepath.Join(t.TempDir(), "merged.json")
	opts := full
	opts.Outfile = out
	run := runMergeBomsRaw(opts)
	if run.code != 0 || run.stdout != "" || run.stderr != "" {
		t.Errorf("Outfile: exit %d, stdout %q, stderr %q; want 0 and nothing", run.code, run.stdout, run.stderr)
	}
	if b, err := os.ReadFile(out); err != nil || !bytes.Contains(b, []byte(`"bomFormat":"CycloneDX"`)) {
		t.Errorf("Outfile %s: %v %q, want the merged document", out, err, b)
	}
	opts.Outfile = "-"
	if run := runMergeBomsRaw(opts); run.code != 0 || !strings.Contains(run.stdout, `"bomFormat":"CycloneDX"`) {
		t.Errorf("Outfile -: exit %d, stdout %q; want the document on stdout", run.code, run.stdout)
	}

	// Structure "" and lower-case "hierarchical" run FLAT: T-4 (a)'s inputs fold.
	for _, structure := range []string{"", "hierarchical"} {
		opts := full
		opts.Structure = structure
		opts.InputFiles = t2Inputs(t, "app")
		run := runMergeBomsRaw(opts)
		if run.code != 0 || !strings.HasPrefix(run.stderr, t2FoldLine) {
			t.Errorf("Structure %q: exit %d, stderr %q; want the fold line", structure, run.code, run.stderr)
			continue
		}
		cs := topComponents(t, decodeMerged(t, run.stdout))
		if len(cs) != 2 || len(componentsWith(cs, "purl", "pkg:generic/app@1.0.0")) != 1 {
			t.Errorf("Structure %q: components %v, want one app node and third", structure, cs)
		}
	}
}

// T-8: two runs differ in serialNumber only.
func TestMergeBomsDeterministic(t *testing.T) {
	paths := t2Inputs(t, "app")
	a := runMergeBoms(cmd.MergeBomsOptions{InputFiles: paths})
	b := runMergeBoms(cmd.MergeBomsOptions{InputFiles: paths})
	if a.code != 0 || b.code != 0 {
		t.Fatalf("exit %d / %d", a.code, b.code)
	}
	if a.stdout == b.stdout {
		t.Errorf("two runs gave one serialNumber")
	}
	if !bytes.Equal(blankSerial([]byte(a.stdout)), blankSerial([]byte(b.stdout))) || a.stderr != b.stderr {
		t.Errorf("two runs differ beyond serialNumber:\n%s\n%s", a.stdout, b.stdout)
	}
}
