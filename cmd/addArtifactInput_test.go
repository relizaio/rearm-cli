package cmd

import (
	"encoding/json"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	rearm "github.com/relizaio/rearm-client-go"
)

// addartifact's input: fields, unknown keys, deprecated aliases, nested and source code entry
// importable entries, digest scopes, --artifacts beside an advanced flag, check tags (task SAFE-16,
// design SAFE-9 3.4 and 3.5 item 4, test list 4.2).

const knownArtifactNames = "artifacts, bomFormat, digestRecords, displayIdentifier, downloadLinks, file, filePath, importMode, importScope, inventoryTypes, status, storedIn, stripBom, tags, type, userIssuerClassOverride, version, vexImportMode, vexScope"

func tagSet(v any, drop string) []string {
	t := reflect.TypeOf(v)
	var names []string
	for i := 0; i < t.NumField(); i++ {
		name := strings.Split(t.Field(i).Tag.Get("json"), ",")[0]
		if name != "" && name != drop {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// c1
func TestArtifactCarriesEveryArtifactInputField(t *testing.T) {
	cli := tagSet(Artifact{}, "filePath")
	client := tagSet(rearm.ArtifactInput{}, "fileSce")
	if !reflect.DeepEqual(cli, client) {
		t.Fatalf("the CLI's Artifact and the pinned rearm.ArtifactInput differ (add the field to cmd/artifactInput.go):\ncli:    %v\nclient: %v", cli, client)
	}
	for _, f := range []string{"importScope", "importMode", "status", "digestRecords"} {
		if !strings.Contains(" "+strings.Join(cli, " ")+" ", " "+f+" ") {
			t.Errorf("Artifact lacks %s", f)
		}
	}
	if got := strings.Join(artifactKnownKeys, ", "); got != knownArtifactNames {
		t.Errorf("known names: %s", got)
	}
}

// c2
func TestImportFieldsAreSent(t *testing.T) {
	dir := t.TempDir()
	f := importServer(t, probeBody(currentSummaryFields, nil), fakeAnswer{summaries: map[string]any{
		"release/0": sum("proposalsAutoAccepted", 1), "release/1": sum("proposalsAutoAccepted", 1)}})
	extra := []any{"importScope", "RELEASE", "importMode", "STAGE", "userIssuerClassOverride", "VENDOR", "status", "COMPLETE",
		"digestRecords", []any{map[string]any{"algo": "SHA_256", "digest": strings.Repeat("a", 64), "scope": "ORIGINAL_FILE"}}}
	input := []any{art(artFile(t, dir, "vex.json"), "VEX", "v", extra...), art(artFile(t, dir, "sc.json"), "SAFETY_CONTEXT", "s", extra...)}
	res := runImportChild(t, f.srv.URL, "addartifact", addArtifactArgs("--artifacts", jsonArg(t, input)), childOpts{})
	wantExit(t, res, 0)
	reqs, _ := f.recorded()
	sent := releaseArtifactsOf(t, uploads(reqs)[0])
	for i, a := range sent {
		for _, k := range []string{"importScope", "importMode", "userIssuerClassOverride", "status"} {
			if a[k] != extra[indexOf(extra, k)+1] {
				t.Errorf("artifact %d: %s = %v", i, k, a[k])
			}
		}
		recs, _ := a["digestRecords"].([]any)
		if len(recs) != 1 || recs[0].(map[string]any)["scope"] != "ORIGINAL_FILE" || recs[0].(map[string]any)["algo"] != "SHA_256" {
			t.Errorf("artifact %d: digestRecords %v", i, a["digestRecords"])
		}
	}
}

func indexOf(kv []any, key string) int {
	for i := 0; i < len(kv); i += 2 {
		if kv[i] == key {
			return i
		}
	}
	return -1
}

// c3
func TestUnknownKeysAreRefusedBeforeAnyRequest(t *testing.T) {
	dir := t.TempDir()
	bom := artFile(t, dir, "bom.json")
	groupNames := "artifacts, deliverable, variant"
	for _, c := range []struct {
		flag  string
		input any
		want  string
	}{
		{"--artifacts", []any{art(bom, "BOM", "b", "importScop", "X")},
			`Error: --artifacts[0]: unknown field "importScop"; this rearm CLI sends only: ` + knownArtifactNames},
		{"--releasearts", []any{art(bom, "BOM", "b", "artifacts", []any{art(bom, "BOM", "n", "frob", 1)})},
			`Error: --releasearts[0].artifacts[0]: unknown field "frob"; this rearm CLI sends only: ` + knownArtifactNames},
		{"--deliverablearts", []any{map[string]any{"deliverabl": "d", "artifacts": []any{art(bom, "BOM", "b")}}},
			`Error: --deliverablearts[0]: unknown field "deliverabl"; this rearm CLI sends only: ` + groupNames},
		{"--scearts", []any{map[string]any{"scee": "s", "artifacts": []any{art(bom, "BOM", "b")}}},
			`Error: --scearts[0]: unknown field "scee"; this rearm CLI sends only: artifacts, sce`},
		{"--artifacts", []any{art(bom, "BOM", "b", "a\u001b[31mb", 1)},
			`Error: --artifacts[0]: unknown field "a\x1b[31mb"; this rearm CLI sends only: ` + knownArtifactNames},
	} {
		f := newFakeServer(t, func(int, recordedRequest) (int, string) { return 200, "{}" })
		res := runImportChild(t, f.srv.URL, "addartifact", addArtifactArgs(c.flag, jsonArg(t, c.input)), childOpts{})
		wantExit(t, res, 1)
		wantLines(t, res, []string{c.want})
		wantNoRequests(t, f)
		if strings.ContainsRune(res.stderr, 0x1b) {
			t.Errorf("a raw ESC reached stderr: %q", res.stderr)
		}
	}
	// a long key is cut to 100 runes
	long := strings.Repeat("k", 150)
	_, err := decodeAddArtifactInput("--artifacts", `[{"`+long+`": 1}]`, "", "", false)
	if err == nil || !strings.Contains(err.Error(), `unknown field "`+strings.Repeat("k", 100)+`";`) {
		t.Errorf("long key: %v", err)
	}
}

// c4
func TestCaseVariantsOfKnownKeysAndCRLFAreAccepted(t *testing.T) {
	dir := t.TempDir()
	f := importServer(t, probeBody(currentSummaryFields, nil), fakeAnswer{summaries: map[string]any{"release/0": sum("proposalsAutoAccepted", 1)}})
	p, _ := json.Marshal(artFile(t, dir, "vex.json"))
	input := "[\r\n  {\"filePath\": " + string(p) + ",\r\n   \"type\": \"VEX\",\r\n   \"ImportScope\": \"RELEASE\",\r\n   \"DISPLAYIDENTIFIER\": \"v\"}\r\n]\r\n"
	res := runImportChild(t, f.srv.URL, "addartifact", addArtifactArgs("--artifacts", input), childOpts{})
	wantExit(t, res, 0)
	reqs, _ := f.recorded()
	sent := releaseArtifactsOf(t, uploads(reqs)[0])
	if sent[0]["importScope"] != "RELEASE" || sent[0]["displayIdentifier"] != "v" {
		t.Errorf("sent: %v", sent[0])
	}
}

// c5
func TestDeprecatedAliasesAreForwardedWithNotices(t *testing.T) {
	dir := t.TempDir()
	vex := artFile(t, dir, "vex.json")
	for _, c := range []struct {
		extra []any
		want  []string
	}{
		{[]any{"vexScope", "ORG"}, []string{`notice: --artifacts[0]: "vexScope" is deprecated; use "importScope"`}},
		{[]any{"importScope", "RELEASE", "vexScope", "ORG"}, []string{
			`notice: --artifacts[0]: "vexScope" is deprecated; use "importScope"`,
			`notice: --artifacts[0]: "importScope" (RELEASE) is used and "vexScope" (ORG) is ignored`}},
		{[]any{"vexImportMode", "STAGE", "importMode", "STAGE"}, []string{`notice: --artifacts[0]: "vexImportMode" is deprecated; use "importMode"`}},
	} {
		f := importServer(t, probeBody(currentSummaryFields, nil), fakeAnswer{summaries: map[string]any{"release/0": sum("proposalsAutoAccepted", 1)}})
		res := runImportChild(t, f.srv.URL, "addartifact", addArtifactArgs("--artifacts", jsonArg(t, []any{art(vex, "VEX", "v", c.extra...)})), childOpts{})
		wantExit(t, res, 0)
		lines := res.stderrLines()
		if len(lines) < len(c.want) || strings.Join(lines[:len(c.want)], "\n") != strings.Join(c.want, "\n") {
			t.Errorf("want notices %q first, got %s", c.want, res)
		}
		if strings.Count(res.stderr, "notice:") != len(c.want) {
			t.Errorf("extra notices: %s", res)
		}
		reqs, _ := f.recorded()
		sent := releaseArtifactsOf(t, uploads(reqs)[0])[0]
		for i := 0; i < len(c.extra); i += 2 {
			if sent[c.extra[i].(string)] != c.extra[i+1] {
				t.Errorf("%v not forwarded unchanged: %v", c.extra[i], sent)
			}
		}
	}
}

// c6
func TestANestedImportableEntryIsRefused(t *testing.T) {
	dir := t.TempDir()
	input := []any{art(artFile(t, dir, "bom.json"), "BOM", "b", "artifacts", []any{art(artFile(t, dir, "vex.json"), "VEX", "n")})}
	f := newFakeServer(t, func(int, recordedRequest) (int, string) { return 200, "{}" })
	res := runImportChild(t, f.srv.URL, "addartifact", addArtifactArgs("--artifacts", jsonArg(t, input)), childOpts{})
	wantExit(t, res, 1)
	wantLines(t, res, []string{"Error: --artifacts[0].artifacts[0]: a VEX or SAFETY_CONTEXT artifact nested under another artifact is stored but never imported; list it at the top level, or pass --allow-import-failure to upload it anyway"})
	wantNoRequests(t, f)

	f = importServer(t, probeBody(currentSummaryFields, nil), fakeAnswer{})
	res = runImportChild(t, f.srv.URL, "addartifact", addArtifactArgs("--artifacts", jsonArg(t, input), "--allow-import-failure"), childOpts{})
	wantExit(t, res, 0)
	wantLines(t, res, []string{
		`import VEX --artifacts[0].artifacts[0] "n": NOT IMPORTED: a nested artifact runs no import`,
		"import check: 1 of 1 VEX/SAFETY_CONTEXT artifacts imported nothing or have no known result; accepted by --allow-import-failure",
	})
	reqs, _ := f.recorded()
	if len(reqs) != 1 || reqs[0].OperationName != "AddArtifactProgrammatic" {
		t.Errorf("want one legacy upload and no probe, got %d requests", len(reqs))
	}
}

// c7 (addartifact part; the --digest part is in digestFlag_test.go)
func TestDigestScopesComputedByReARMAreRefused(t *testing.T) {
	dir := t.TempDir()
	bom := artFile(t, dir, "bom.json")
	rec := func(scope string) []any {
		return []any{map[string]any{"algo": "SHA_256", "digest": strings.Repeat("a", 64), "scope": scope}}
	}
	for scope, want := range map[string]string{
		"rearm":       "Error: --artifacts[0]: digest scope REARM is computed by ReARM and cannot be supplied",
		"AS_UPLOADED": "Error: --artifacts[0]: digest scope AS_UPLOADED is computed by ReARM and cannot be supplied",
	} {
		f := newFakeServer(t, func(int, recordedRequest) (int, string) { return 200, "{}" })
		res := runImportChild(t, f.srv.URL, "addartifact", addArtifactArgs("--artifacts", jsonArg(t, []any{art(bom, "BOM", "b", "digestRecords", rec(scope))})), childOpts{})
		wantExit(t, res, 1)
		wantLines(t, res, []string{want})
		wantNoRequests(t, f)
	}
	f := newFakeServer(t, func(int, recordedRequest) (int, string) {
		return 200, dataBody(map[string]any{"addArtifactProgrammatic": map[string]any{"uuid": "r"}})
	})
	res := runImportChild(t, f.srv.URL, "addartifact", addArtifactArgs("--artifacts", jsonArg(t, []any{art(bom, "BOM", "b", "digestRecords", rec("ORIGINAL_FILE"))})), childOpts{})
	wantExit(t, res, 0)
	reqs, _ := f.recorded()
	if recs, _ := releaseArtifactsOf(t, reqs[0])[0]["digestRecords"].([]any); len(recs) != 1 || recs[0].(map[string]any)["scope"] != "ORIGINAL_FILE" {
		t.Errorf("ORIGINAL_FILE is forwarded: %v", recs)
	}
}

// c8
func TestArtifactsBesideAnAdvancedFlagIsRefused(t *testing.T) {
	dir := t.TempDir()
	vex, bom := artFile(t, dir, "vex.json"), artFile(t, dir, "bom.json")
	const want = "Error: --artifacts cannot be combined with --releasearts, --deliverablearts or --scearts; put release artifacts in --releasearts"
	delGroup := jsonArg(t, []any{map[string]any{"deliverable": "d-1", "artifacts": []any{art(bom, "BOM", "b")}}})
	for _, args := range [][]string{
		{"--artifacts", jsonArg(t, []any{art(vex, "VEX", "v")}), "--deliverablearts", delGroup},
		{"--artifacts", jsonArg(t, []any{art(bom, "BOM", "b")}), "--releasearts", jsonArg(t, []any{art(bom, "BOM", "b")})},
		{"--artifacts", jsonArg(t, []any{art(vex, "VEX", "v")}), "--deliverablearts", delGroup, "--allow-import-failure"},
	} {
		f := newFakeServer(t, func(int, recordedRequest) (int, string) { return 200, "{}" })
		res := runImportChild(t, f.srv.URL, "addartifact", addArtifactArgs(args...), childOpts{})
		wantExit(t, res, 1)
		wantLines(t, res, []string{want})
		wantNoRequests(t, f)
	}
	f := importServer(t, probeBody(currentSummaryFields, nil), fakeAnswer{summaries: map[string]any{"release/0": sum("proposalsAutoAccepted", 1)}})
	res := runImportChild(t, f.srv.URL, "addartifact", addArtifactArgs("--artifacts", jsonArg(t, []any{art(vex, "VEX", "v")})), childOpts{})
	wantExit(t, res, 0)
	reqs, _ := f.recorded()
	if len(reqs) != 2 || reqs[0].OperationName != "ImportSummaryProbe" || reqs[1].OperationName != "AddArtifactProgrammaticWithImports" {
		t.Errorf("control: want the probe and the upload, got %+v", reqs)
	}
}

// c9
func TestASourceCodeEntryImportableEntryIsRefused(t *testing.T) {
	dir := t.TempDir()
	for _, typ := range []string{"VEX", "SAFETY_CONTEXT"} {
		input := jsonArg(t, []any{map[string]any{"sce": "s-1", "artifacts": []any{art(artFile(t, dir, "doc.json"), typ, "d")}}})
		f := newFakeServer(t, func(int, recordedRequest) (int, string) { return 200, "{}" })
		res := runImportChild(t, f.srv.URL, "addartifact", addArtifactArgs("--scearts", input), childOpts{})
		wantExit(t, res, 1)
		wantLines(t, res, []string{"Error: --scearts[0].artifacts[0]: the server does not report the import result of a VEX or SAFETY_CONTEXT artifact on a source code entry, so this CLI cannot check it; upload it with --releasearts (the import runs against the release either way), or pass --allow-import-failure to upload it unchecked"})
		wantNoRequests(t, f)
	}
	// with the flag: uploaded unchecked, although the fake answer carries a summary on the entry
	input := jsonArg(t, []any{map[string]any{"sce": "s-1", "artifacts": []any{art(artFile(t, dir, "vex.json"), "VEX", "d")}}})
	f := newFakeServer(t, func(n int, r recordedRequest) (int, string) {
		return 200, dataBody(map[string]any{"addArtifactProgrammatic": map[string]any{"uuid": "rel-1",
			"sourceCodeEntryDetails": map[string]any{"artifactDetails": []any{map[string]any{"uuid": detailUuid(1), "type": "VEX",
				"importSummary": sum("proposalsCreated", 3)}}}}})
	})
	res := runImportChild(t, f.srv.URL, "addartifact", addArtifactArgs("--scearts", input, "--allow-import-failure"), childOpts{})
	wantExit(t, res, 0)
	wantLines(t, res, []string{
		`import VEX --scearts[0].artifacts[0] "d": NOT REPORTED: the server does not report import results for source code entry uploads`,
		"import check: 1 of 1 VEX/SAFETY_CONTEXT artifacts imported nothing or have no known result; accepted by --allow-import-failure",
	})
	reqs, _ := f.recorded()
	if len(reqs) != 1 || reqs[0].OperationName != "AddArtifactProgrammatic" {
		t.Errorf("want one legacy upload and no probe, got %+v", reqs)
	}
}

// c10
func TestCheckTagsGoOnCheckedEntriesOnly(t *testing.T) {
	dir := t.TempDir()
	hex32 := regexp.MustCompile(`^[0-9a-f]{32}$`)
	release := jsonArg(t, []any{
		art(artFile(t, dir, "vex.json"), "VEX", "rv", "tags", []any{map[string]any{"key": "team", "value": "a"}}),
		art(artFile(t, dir, "bom.json"), "BOM", "rb", "artifacts", []any{art(artFile(t, dir, "nested.json"), "VEX", "nv")}),
	})
	deliverable := jsonArg(t, []any{map[string]any{"deliverable": "d-1", "artifacts": []any{art(artFile(t, dir, "sc.json"), "SAFETY_CONTEXT", "ds")}}})
	sce := jsonArg(t, []any{map[string]any{"sce": "s-1", "artifacts": []any{art(artFile(t, dir, "svex.json"), "VEX", "sv")}}})
	var previous []string
	for run := 0; run < 2; run++ {
		f := importServer(t, probeBody(currentSummaryFields, nil), fakeAnswer{})
		res := runImportChild(t, f.srv.URL, "addartifact", addArtifactArgs("--releasearts", release, "--deliverablearts", deliverable, "--scearts", sce, "--allow-import-failure"), childOpts{})
		wantExit(t, res, 0)
		reqs, _ := f.recorded()
		var vars struct {
			ArtifactInput struct {
				ReleaseArtifacts     []map[string]any `json:"releaseArtifacts"`
				DeliverableArtifacts []struct {
					Artifacts []map[string]any `json:"artifacts"`
				} `json:"deliverableArtifacts"`
				SceArtifacts []struct {
					Artifacts []map[string]any `json:"artifacts"`
				} `json:"sceArtifacts"`
			} `json:"artifactInput"`
		}
		if err := json.Unmarshal(uploads(reqs)[0].Variables, &vars); err != nil {
			t.Fatal(err)
		}
		in := vars.ArtifactInput
		rv := in.ReleaseArtifacts[0]
		tags, _ := rv["tags"].([]any)
		if len(tags) != 2 || tags[0].(map[string]any)["key"] != "team" || tags[1].(map[string]any)["key"] != importCheckTagKey {
			t.Fatalf("release VEX tags: %v", rv["tags"])
		}
		values := append(checkTagsOf(rv), checkTagsOf(in.DeliverableArtifacts[0].Artifacts[0])...)
		if len(values) != 2 || values[0] == values[1] {
			t.Fatalf("check values: %v", values)
		}
		for _, v := range values {
			if !hex32.MatchString(v) {
				t.Errorf("check value %q is not 32 lowercase hex characters", v)
			}
			for _, p := range previous {
				if v == p {
					t.Errorf("value %s repeats across runs", v)
				}
			}
		}
		bom := in.ReleaseArtifacts[1]
		nested := bom["artifacts"].([]any)[0].(map[string]any)
		for name, a := range map[string]map[string]any{"BOM": bom, "nested VEX": nested, "SCE VEX": in.SceArtifacts[0].Artifacts[0]} {
			if len(checkTagsOf(a)) != 0 {
				t.Errorf("the %s carries a check tag: %v", name, a["tags"])
			}
		}
		previous = values
	}
}
