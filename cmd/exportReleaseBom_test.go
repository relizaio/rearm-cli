package cmd

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	rearm "github.com/relizaio/rearm-client-go"
	"github.com/spf13/cobra"
)

// rearm exportreleasebom and scorereleasebom (task SCORE-24, design 3.3 to 3.5, tests T-7 to T-12)
// against a stubbed GraphQL endpoint: the variables each flag sends, the checks made before any
// request, what reaches stdout, the credentials on the request, and the error lines.

type releaseBomRequest struct {
	OperationName string                 `json:"operationName"`
	Query         string                 `json:"query"`
	Variables     map[string]interface{} `json:"variables"`
	Authorization string                 `json:"-"`
}

type releaseBomServer struct {
	mu       sync.Mutex
	requests []releaseBomRequest
	// answer is the whole JSON body the GraphQL endpoint returns
	answer map[string]interface{}
}

func (s *releaseBomServer) calls() []releaseBomRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]releaseBomRequest(nil), s.requests...)
}

// releaseBomWorld starts the stub; the client is a key client without the token exchange unless
// the test builds its own.
func releaseBomWorld(t *testing.T, answer map[string]interface{}) (*releaseBomServer, *httptest.Server) {
	t.Helper()
	s := &releaseBomServer{answer: answer}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == rearm.TokenPath {
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "exchanged-token", "token_type": "Bearer", "expires_in": 3600})
			return
		}
		var req releaseBomRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		req.Authorization = r.Header.Get("Authorization")
		s.mu.Lock()
		s.requests = append(s.requests, req)
		s.mu.Unlock()
		_ = json.NewEncoder(w).Encode(s.answer)
	}))
	t.Cleanup(srv.Close)
	c, err := rearm.New(srv.URL, "id", "secret", rearm.WithoutTokenExchange())
	if err != nil {
		t.Fatal(err)
	}
	savedClient, savedDebug := apiClient, debug
	apiClient, debug = c, ""
	t.Cleanup(func() { apiClient, debug = savedClient, savedDebug })
	return s, srv
}

func exportAnswer(doc interface{}) map[string]interface{} {
	return map[string]interface{}{"data": map[string]interface{}{"releaseSbomExportProgrammatic": doc}}
}

func scoreAnswer(report interface{}) map[string]interface{} {
	return map[string]interface{}{"data": map[string]interface{}{"releaseSbomScoreProgrammatic": report}}
}

// runReleaseBomCmd runs a fresh command with args; it returns stdout, stderr and the exit code.
func runReleaseBomCmd(t *testing.T, newCmd func() *cobra.Command, args ...string) (string, string, int) {
	t.Helper()
	code := 0
	saved := releaseBomExit
	releaseBomExit = func(c int) { code = c }
	t.Cleanup(func() { releaseBomExit = saved })
	cmd := newCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("%v: %v", args, err)
	}
	return stdout.String(), stderr.String(), code
}

// jsonEqual compares as JSON, so a []string and a decoded []interface{} compare equal.
func jsonEqual(t *testing.T, want, got interface{}) bool {
	t.Helper()
	w, _ := json.Marshal(want)
	g, _ := json.Marshal(got)
	var wv, gv interface{}
	_ = json.Unmarshal(w, &wv)
	_ = json.Unmarshal(g, &gv)
	return reflect.DeepEqual(wv, gv)
}

const releaseBomDoc = `{"bomFormat":"CycloneDX","specVersion":"1.6","components":[{"name":"caf` + "\u00e9" + `"}]}`

// T-7
func TestExportReleaseBomSendsTheDialogDefaults(t *testing.T) {
	s, _ := releaseBomWorld(t, exportAnswer(releaseBomDoc))
	if _, stderr, code := runReleaseBomCmd(t, newExportReleaseBomCmd, "--releaseid", "r-1"); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	calls := s.calls()
	if len(calls) != 1 || calls[0].OperationName != "ReleaseSbomExportProgrammatic" {
		t.Fatalf("one ReleaseSbomExportProgrammatic request, got %+v", calls)
	}
	want := map[string]interface{}{"release": "r-1", "tldOnly": true, "ignoreDev": false, "structure": "FLAT",
		"belongsTo": nil, "mediaType": "JSON", "excludeCoverageTypes": []string{"DEV", "TEST", "BUILD_TIME"},
		"includeSupportMetadata": false, "includeInternalMetadata": false, "excludeFileComponents": false}
	if !jsonEqual(t, want, calls[0].Variables) {
		t.Fatalf("the dialog's defaults, sent explicitly:\nwant %v\ngot  %v", want, calls[0].Variables)
	}
	if _, has := calls[0].Variables["belongsTo"]; !has {
		t.Fatal("belongsTo is sent as null, not left out")
	}
}

// flagCase is one flag set and the variables it changes from the command's defaults.
type flagCase struct {
	args    []string
	changes map[string]interface{}
}

// assertEachFlagChangesExactlyItsVariable runs the command once per case after baseArgs and checks
// that the request carries base with exactly the case's changes.
func assertEachFlagChangesExactlyItsVariable(t *testing.T, newCmd func() *cobra.Command, answer map[string]interface{},
	baseArgs []string, base map[string]interface{}, cases []flagCase) {
	t.Helper()
	for _, c := range cases {
		s, _ := releaseBomWorld(t, answer)
		if _, stderr, code := runReleaseBomCmd(t, newCmd, append(append([]string(nil), baseArgs...), c.args...)...); code != 0 {
			t.Fatalf("%v: exit %d: %s", c.args, code, stderr)
		}
		want := map[string]interface{}{}
		for k, v := range base {
			want[k] = v
		}
		for k, v := range c.changes {
			want[k] = v
		}
		if got := s.calls()[0].Variables; !jsonEqual(t, want, got) {
			t.Fatalf("%v:\nwant %v\ngot  %v", c.args, want, got)
		}
	}
}

// The selection and merge flags both commands share (design 3.3, 3.4).
var releaseBomSelectionFlagCases = []flagCase{
	{[]string{"--tldonly=false"}, map[string]interface{}{"tldOnly": false}},
	{[]string{"--ignoredev"}, map[string]interface{}{"ignoreDev": true}},
	{[]string{"--structure", "HIERARCHICAL"}, map[string]interface{}{"structure": "HIERARCHICAL"}},
	{[]string{"--structure", "hierarchical"}, map[string]interface{}{"structure": "HIERARCHICAL"}},
	{[]string{"--belongsto", "SCE"}, map[string]interface{}{"belongsTo": "SCE"}},
	{[]string{"--excludecoveragetypes", "none"}, map[string]interface{}{"excludeCoverageTypes": nil}},
	{[]string{"--excludecoveragetypes", "DEV", "--excludecoveragetypes", "TEST"}, map[string]interface{}{"excludeCoverageTypes": []string{"DEV", "TEST"}}},
	{[]string{"--excludecoveragetypes", "DEV,TEST"}, map[string]interface{}{"excludeCoverageTypes": []string{"DEV", "TEST"}}},
	{[]string{"--excludefilecomponents"}, map[string]interface{}{"excludeFileComponents": true}},
}

// T-8
func TestEachExportFlagChangesExactlyItsVariable(t *testing.T) {
	base := map[string]interface{}{"release": "r-1", "tldOnly": true, "ignoreDev": false, "structure": "FLAT",
		"belongsTo": nil, "mediaType": "JSON", "excludeCoverageTypes": []string{"DEV", "TEST", "BUILD_TIME"},
		"includeSupportMetadata": false, "includeInternalMetadata": false, "excludeFileComponents": false}
	cases := append(append([]flagCase(nil), releaseBomSelectionFlagCases...),
		flagCase{[]string{"--mediatype", "CSV"}, map[string]interface{}{"mediaType": "CSV"}},
		flagCase{[]string{"--includesupportmetadata"}, map[string]interface{}{"includeSupportMetadata": true}},
		flagCase{[]string{"--includeinternalmetadata"}, map[string]interface{}{"includeInternalMetadata": true}},
	)
	assertEachFlagChangesExactlyItsVariable(t, newExportReleaseBomCmd, exportAnswer(releaseBomDoc),
		[]string{"--releaseid", "r-1"}, base, cases)

	s, _ := releaseBomWorld(t, exportAnswer(releaseBomDoc))
	runReleaseBomCmd(t, newExportReleaseBomCmd, "--component", "my-product", "--version", "1.2.3")
	got := s.calls()[0].Variables
	if got["componentId"] != "my-product" || got["version"] != "1.2.3" {
		t.Fatalf("component and version sent, got %v", got)
	}
	if _, has := got["release"]; has {
		t.Fatalf("no release variable when named by component, got %v", got)
	}
}

// T-8 for scorereleasebom (review item T-4 of run 1): the same flags as the export, each to its
// variable; the metadata stays null whatever else is set.
func TestEachScoreFlagChangesExactlyItsVariable(t *testing.T) {
	base := map[string]interface{}{"release": "r-1", "tldOnly": true, "ignoreDev": false, "structure": "FLAT",
		"belongsTo": nil, "excludeCoverageTypes": []string{"DEV", "TEST", "BUILD_TIME"},
		"includeSupportMetadata": nil, "includeInternalMetadata": nil, "excludeFileComponents": false,
		"profiles": []string{"cisa-2026"}}
	assertEachFlagChangesExactlyItsVariable(t, newScoreReleaseBomCmd, scoreAnswer(`{"reportVersion":1}`),
		[]string{"--releaseid", "r-1", "--profile", "cisa-2026"}, base, releaseBomSelectionFlagCases)
}

// T-9
func TestBadInputIsRefusedBeforeAnyRequest(t *testing.T) {
	cases := []struct {
		newCmd func() *cobra.Command
		args   []string
		names  string
	}{
		{newExportReleaseBomCmd, []string{"--releaseid", "r", "--structure", "TREE"}, "--structure"},
		{newExportReleaseBomCmd, []string{"--releaseid", "r", "--belongsto", "BUILD"}, "--belongsto"},
		{newExportReleaseBomCmd, []string{"--releaseid", "r", "--mediatype", "XML"}, "--mediatype"},
		{newExportReleaseBomCmd, []string{"--releaseid", "r", "--excludecoveragetypes", "DEV,PROD"}, "--excludecoveragetypes"},
		{newExportReleaseBomCmd, []string{"--releaseid", "r", "--excludecoveragetypes", "none,DEV"}, "--excludecoveragetypes"},
		{newExportReleaseBomCmd, []string{"--releaseid", "r", "--component", "c", "--version", "1"}, "--releaseid"},
		{newExportReleaseBomCmd, []string{}, "--releaseid"},
		{newExportReleaseBomCmd, []string{"--component", "c"}, "--version"},
		{newExportReleaseBomCmd, []string{"--version", "1"}, "--component"},
		{newExportReleaseBomCmd, []string{"--releaseid", "r", "--mediatype", "EXCEL"}, "--outfile"},
		{newExportReleaseBomCmd, []string{"--releaseid", "r", "--outfile", "-"}, "--outfile"},
		{newScoreReleaseBomCmd, []string{"--releaseid", "r"}, "--profile"},
		{newScoreReleaseBomCmd, []string{"--profile", "fda"}, "--releaseid"},
		{newScoreReleaseBomCmd, []string{"--releaseid", "r", "--profile", "fda", "--structure", "TREE"}, "--structure"},
	}
	for _, c := range cases {
		s, _ := releaseBomWorld(t, exportAnswer(releaseBomDoc))
		stdout, stderr, code := runReleaseBomCmd(t, c.newCmd, c.args...)
		if code != 1 || stdout != "" || !strings.HasPrefix(stderr, "Error: ") || !strings.Contains(stderr, c.names) {
			t.Fatalf("%v: want exit 1, nothing on stdout and an error naming %s; got %d %q %q", c.args, c.names, code, stdout, stderr)
		}
		if n := len(s.calls()); n != 0 {
			t.Fatalf("%v: refused before any request, got %d", c.args, n)
		}
	}
}

// T-10
func TestTheDocumentReachesStdoutOrTheFileByteForByte(t *testing.T) {
	releaseBomWorld(t, exportAnswer(releaseBomDoc))
	stdout, stderr, code := runReleaseBomCmd(t, newExportReleaseBomCmd, "--releaseid", "r-1")
	if code != 0 || stdout != releaseBomDoc || stderr != "" {
		t.Fatalf("stdout is the document and nothing else, got %d %q %q", code, stdout, stderr)
	}

	out := filepath.Join(t.TempDir(), "nested", "product.cdx.json")
	stdout, _, code = runReleaseBomCmd(t, newExportReleaseBomCmd, "--releaseid", "r-1", "--outfile", out)
	written, err := os.ReadFile(out)
	if code != 0 || err != nil || string(written) != releaseBomDoc || stdout != out+"\n" {
		t.Fatalf("the file holds the document and stdout its path, got %d %v %q %q", code, err, written, stdout)
	}
	if info, _ := os.Stat(out); info.Mode().Perm() != 0644 {
		t.Fatalf("written 0644, got %v", info.Mode().Perm())
	}

	csv := "name,version\ncaf\u00e9,1.0\n"
	releaseBomWorld(t, exportAnswer(csv))
	if stdout, _, _ = runReleaseBomCmd(t, newExportReleaseBomCmd, "--releaseid", "r-1", "--mediatype", "CSV"); stdout != csv {
		t.Fatalf("CSV is written as text, got %q", stdout)
	}

	workbook := []byte{'P', 'K', 3, 4, 0, 0xff, 0x10}
	releaseBomWorld(t, exportAnswer(base64.StdEncoding.EncodeToString(workbook)))
	xlsx := filepath.Join(t.TempDir(), "product.xlsx")
	if _, stderr, code = runReleaseBomCmd(t, newExportReleaseBomCmd, "--releaseid", "r-1", "--mediatype", "EXCEL", "--outfile", xlsx); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if got, _ := os.ReadFile(xlsx); !bytes.Equal(got, workbook) {
		t.Fatalf("EXCEL is decoded to the workbook bytes, got %v", got)
	}

	releaseBomWorld(t, exportAnswer(nil))
	stdout, stderr, code = runReleaseBomCmd(t, newExportReleaseBomCmd, "--releaseid", "r-1")
	if code != 1 || stdout != "" || !strings.Contains(stderr, "empty export") {
		t.Fatalf("a null export is an error, not null, got %d %q %q", code, stdout, stderr)
	}

	// debug lines go to stderr, so a pipe stays clean
	releaseBomWorld(t, exportAnswer(releaseBomDoc))
	debug = "true"
	stdout, stderr, _ = runReleaseBomCmd(t, newExportReleaseBomCmd, "--releaseid", "r-1")
	if stdout != releaseBomDoc || !strings.Contains(stderr, "Using ReARM at") {
		t.Fatalf("debug on stderr only, got %q %q", stdout, stderr)
	}
}

// T-11
func TestBothCommandsCarryTheSessionTokenOrTheExchangedKey(t *testing.T) {
	for _, run := range []struct {
		newCmd func() *cobra.Command
		args   []string
		answer map[string]interface{}
	}{
		{newExportReleaseBomCmd, []string{"--releaseid", "r-1"}, exportAnswer(releaseBomDoc)},
		{newScoreReleaseBomCmd, []string{"--releaseid", "r-1", "--profile", "cisa-2026"}, scoreAnswer(`{"reportVersion":1}`)},
	} {
		s, srv := releaseBomWorld(t, run.answer)
		session, err := rearm.NewWithSession(srv.URL, "refresh-token", rearm.SessionTokens{AccessToken: "session-access",
			AccessTokenExpiry: time.Now().Add(time.Hour), SessionExpiry: time.Now().Add(24 * time.Hour)}, nil)
		if err != nil {
			t.Fatal(err)
		}
		apiClient = session
		if _, stderr, code := runReleaseBomCmd(t, run.newCmd, run.args...); code != 0 {
			t.Fatalf("exit %d: %s", code, stderr)
		}
		key, err := rearm.New(srv.URL, "id", "secret")
		if err != nil {
			t.Fatal(err)
		}
		apiClient = key
		if _, stderr, code := runReleaseBomCmd(t, run.newCmd, run.args...); code != 0 {
			t.Fatalf("exit %d: %s", code, stderr)
		}
		calls := s.calls()
		if len(calls) != 2 || calls[0].Authorization != "Bearer session-access" || calls[1].Authorization != "Bearer exchanged-token" {
			t.Fatalf("%v: the session's bearer, then the exchanged key's, got %+v", run.args, calls)
		}
	}
}

// T-12
func TestErrorsAreOneLineOnStderrAndARefusedScoreNamesItsReason(t *testing.T) {
	releaseBomWorld(t, map[string]interface{}{"errors": []interface{}{map[string]interface{}{"message": "FreeForm key not authorized for this resource"}},
		"data": map[string]interface{}{"releaseSbomExportProgrammatic": nil}})
	stdout, stderr, code := runReleaseBomCmd(t, newExportReleaseBomCmd, "--releaseid", "r-1")
	if code != 1 || stdout != "" || stderr != "Error: FreeForm key not authorized for this resource\n" {
		t.Fatalf("got %d %q %q", code, stdout, stderr)
	}

	releaseBomWorld(t, map[string]interface{}{"errors": []interface{}{map[string]interface{}{"message": "unknown profile nope",
		"extensions": map[string]interface{}{"code": "SBOM_SCORE_ERROR", "reason": "UNKNOWN_PROFILE", "classification": "BAD_REQUEST"}}},
		"data": map[string]interface{}{"releaseSbomScoreProgrammatic": nil}})
	stdout, stderr, code = runReleaseBomCmd(t, newScoreReleaseBomCmd, "--releaseid", "r-1", "--profile", "nope")
	if code != 1 || stdout != "" || stderr != "Error: score refused: UNKNOWN_PROFILE: unknown profile nope\n" {
		t.Fatalf("got %d %q %q", code, stdout, stderr)
	}
}

// T-12
func TestScoreReleaseBomSendsTheProfilesAndTheOrganizationDefaultMetadata(t *testing.T) {
	report := `{"reportVersion":1,"profiles":[]}`
	s, _ := releaseBomWorld(t, scoreAnswer(report))
	stdout, stderr, code := runReleaseBomCmd(t, newScoreReleaseBomCmd, "--component", "my-product", "--version", "1.2.3",
		"--profile", "cisa-2026", "--profile", "fda")
	if code != 0 || stdout != report || stderr != "" {
		t.Fatalf("the report verbatim on stdout, got %d %q %q", code, stdout, stderr)
	}
	calls := s.calls()
	if len(calls) != 1 || calls[0].OperationName != "ReleaseSbomScoreProgrammatic" {
		t.Fatalf("one ReleaseSbomScoreProgrammatic request, got %+v", calls)
	}
	want := map[string]interface{}{"componentId": "my-product", "version": "1.2.3", "tldOnly": true, "ignoreDev": false,
		"structure": "FLAT", "belongsTo": nil, "excludeCoverageTypes": []string{"DEV", "TEST", "BUILD_TIME"},
		"includeSupportMetadata": nil, "includeInternalMetadata": nil, "excludeFileComponents": false,
		"profiles": []string{"cisa-2026", "fda"}}
	if !jsonEqual(t, want, calls[0].Variables) {
		t.Fatalf("\nwant %v\ngot  %v", want, calls[0].Variables)
	}
	for _, k := range []string{"includeSupportMetadata", "includeInternalMetadata"} {
		if v, has := calls[0].Variables[k]; !has || v != nil {
			t.Fatalf("%s is sent as null (the organization's default), got %v", k, calls[0].Variables)
		}
	}
	if _, has := calls[0].Variables["mediaType"]; has {
		t.Fatal("the score takes no media type")
	}
}

func TestTheCommandsAreRegistered(t *testing.T) {
	for _, name := range []string{"exportreleasebom", "scorereleasebom"} {
		if c, _, err := rootCmd.Find([]string{name}); err != nil || c.Name() != name {
			t.Fatalf("%s is a top-level command, got %v %v", name, c, err)
		}
	}
}
