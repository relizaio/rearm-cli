package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	rearm "github.com/relizaio/rearm-client-go"
	"github.com/spf13/cobra"
)

// The harness of the import check tests (task SAFE-16, design SAFE-9 section 4): each case runs the
// command in a child process of the test binary, with LC_ALL=C, no REARM_ variable, no
// ACTIONS_ID_TOKEN_ variable and a fresh empty HOME, against an httptest server that records every
// request and answers per case. Stdout and stderr are captured separately.

// importChildEnv hands the child its spec; it names no flag.
const importChildEnv = "IMPORT_CHECK_TEST_CHILD"

type importChildSpec struct {
	URL       string   `json:"url"`
	Command   string   `json:"command"` // the command whose Run is called; empty runs the real Execute
	Args      []string `json:"args"`
	FakeClock bool     `json:"fakeClock"`
}

var importTestCommands = map[string]*cobra.Command{
	"addartifact":     addArtifactCmd,
	"addrelease":      addreleaseCmd,
	"addreleases":     addReleasesCmd,
	"addodeliverable": addODeliverableCmd,
	"getversion":      getVersionCmd,
}

// TestImportCheckChild is the child process of the import check tests; it does nothing otherwise.
func TestImportCheckChild(t *testing.T) {
	raw := os.Getenv(importChildEnv)
	if raw == "" {
		t.Skip("runs only as the child of an import check test")
	}
	var spec importChildSpec
	if err := json.Unmarshal([]byte(raw), &spec); err != nil {
		os.Exit(90)
	}
	c, err := rearm.New(spec.URL, "id", "secret", rearm.WithoutTokenExchange())
	if err != nil {
		os.Exit(91)
	}
	apiClient = c
	if spec.FakeClock {
		now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		importNow = func() time.Time { return now }
		importSleep = func(d time.Duration) { now = now.Add(d) }
	}
	if spec.Command == "" {
		rootCmd.SetArgs(spec.Args)
		Execute()
		os.Exit(0)
	}
	cmd := importTestCommands[spec.Command]
	if err := cmd.ParseFlags(spec.Args); err != nil {
		fmt.Fprintln(os.Stderr, "test child: ", err)
		os.Exit(92)
	}
	cmd.Run(cmd, cmd.Flags().Args())
	os.Exit(0)
}

// recordedRequest is one request the fake server received.
type recordedRequest struct {
	OperationName string
	Query         string
	Variables     json.RawMessage
	Files         []string // multipart file names, in part order
	FileContents  []string
}

// fakeServer records requests and answers each with respond(n, request) -> (status, body).
type fakeServer struct {
	mu       sync.Mutex
	requests []recordedRequest
	bodies   []string
	srv      *httptest.Server
}

func newFakeServer(t *testing.T, respond func(n int, r recordedRequest) (int, string)) *fakeServer {
	t.Helper()
	f := &fakeServer{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := recordedRequest{}
		var ops struct {
			OperationName string          `json:"operationName"`
			Query         string          `json:"query"`
			Variables     json.RawMessage `json:"variables"`
		}
		mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if mediaType == "multipart/form-data" {
			if err := r.ParseMultipartForm(32 << 20); err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
			_ = json.Unmarshal([]byte(r.FormValue("operations")), &ops)
			for i := 0; ; i++ {
				fh := r.MultipartForm.File[fmt.Sprint(i)]
				if len(fh) == 0 {
					break
				}
				rec.Files = append(rec.Files, fh[0].Filename)
				fr, _ := fh[0].Open()
				b, _ := io.ReadAll(fr)
				fr.Close()
				rec.FileContents = append(rec.FileContents, string(b))
			}
		} else {
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &ops)
		}
		rec.OperationName, rec.Query, rec.Variables = ops.OperationName, ops.Query, ops.Variables
		f.mu.Lock()
		n := len(f.requests)
		f.requests = append(f.requests, rec)
		f.mu.Unlock()
		status, body := respond(n, rec)
		f.mu.Lock()
		f.bodies = append(f.bodies, body)
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeServer) recorded() ([]recordedRequest, []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]recordedRequest(nil), f.requests...), append([]string(nil), f.bodies...)
}

type childResult struct {
	stdout, stderr string
	exit           int
}

func (r childResult) String() string {
	return fmt.Sprintf("exit %d\nstdout:\n%s\nstderr:\n%s", r.exit, r.stdout, r.stderr)
}

// stderrLines is stderr split into lines, without the final empty one.
func (r childResult) stderrLines() []string {
	s := strings.TrimSuffix(r.stderr, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

type childOpts struct {
	execute   bool     // run the real Execute with args (args then include the command name)
	fakeClock bool     // replace the --wait-for-sbom clock
	env       []string // added to the child's environment
}

// runImportChild runs one command in a child process against url.
func runImportChild(t *testing.T, url, command string, args []string, o childOpts) childResult {
	t.Helper()
	spec := importChildSpec{URL: url, Command: command, Args: args, FakeClock: o.fakeClock}
	if o.execute {
		spec.Command = ""
	}
	specJSON, _ := json.Marshal(spec)
	var env []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "REARM_") || strings.HasPrefix(kv, "HOME=") || strings.HasPrefix(kv, "ACTIONS_ID_TOKEN_") ||
			strings.HasPrefix(kv, "LC_ALL=") || strings.HasPrefix(kv, importChildEnv+"=") {
			continue
		}
		env = append(env, kv)
	}
	env = append(env, "HOME="+t.TempDir(), "LC_ALL=C", importChildEnv+"="+string(specJSON))
	env = append(env, o.env...)
	child := exec.Command(os.Args[0], "-test.run=^TestImportCheckChild$")
	child.Env = env
	var stdout, stderr strings.Builder
	child.Stdout, child.Stderr = &stdout, &stderr
	err := child.Run()
	res := childResult{stdout: stdout.String(), stderr: stderr.String()}
	if ee, ok := err.(*exec.ExitError); ok {
		res.exit = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("child did not run: %v", err)
	}
	if res.exit >= 90 {
		t.Fatalf("the test child failed to start: %s", res)
	}
	return res
}

// ---------------------------------------------------------------------------------------------
// Inputs

// artFile writes a small document into dir and returns its path.
func artFile(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(`{"name":"`+name+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// art is one artifact entry of an input list.
func art(path, typ, name string, extra ...any) map[string]any {
	a := map[string]any{"filePath": path, "type": typ, "displayIdentifier": name}
	for i := 0; i+1 < len(extra); i += 2 {
		a[extra[i].(string)] = extra[i+1]
	}
	return a
}

func jsonArg(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// addArtifactArgs are addartifact's target flags followed by more.
func addArtifactArgs(more ...string) []string {
	return append([]string{"--component", "comp", "--version", "1.0.0"}, more...)
}

// ---------------------------------------------------------------------------------------------
// Answers

// currentSummaryFields are the ImportSummary fields of the pinned schema (no
// statementsAwaitingReconcile).
var currentSummaryFields = []string{"statementsTotal", "proposalsCreated", "proposalsAutoAccepted", "proposalsAutoRejected",
	"attestationsDeferred", "statementsUnmatched", "statementsErrored", "statementsUnchanged", "errorMessages", "demotions", "overrideOutcome"}

// vexSummaryFields are the fields of a server that has only VexImportSummary.
var vexSummaryFields = []string{"statementsTotal", "proposalsCreated", "proposalsAutoAccepted", "proposalsAutoRejected",
	"attestationsDeferred", "statementsUnmatched", "statementsErrored", "errorMessages"}

func probeType(fields []string) any {
	if fields == nil {
		return nil
	}
	fs := make([]any, 0, len(fields))
	for _, f := range fields {
		fs = append(fs, map[string]any{"name": f})
	}
	return map[string]any{"fields": fs}
}

// probeTypes are the fields of the two summary types a fake server has (nil: the type is absent).
type probeTypes struct {
	ImportSummary, VexImportSummary []string
}

// badFaithBody is the answer of a server that keeps graphql-java's good-faith introspection limit
// to a request selecting __type or __Type.fields more than once.
const badFaithBody = `{"errors":[{"message":"This request is not asking for introspection in good faith - __Type.fields is present too often!","extensions":{"classification":"BadFaithIntrospection"}}]}`

// answerProbe answers one probe request as a server with the good-faith introspection limit:
// a query with more than one __type or fields is refused, otherwise the type named by the variable
// `name` is answered from p.
func answerProbe(t *testing.T, r recordedRequest, p probeTypes) (int, string) {
	t.Helper()
	if strings.Count(r.Query, "__type") > 1 || strings.Count(r.Query, "fields") > 1 {
		return 200, badFaithBody
	}
	var vars struct {
		Name string `json:"name"`
	}
	_ = json.Unmarshal(r.Variables, &vars)
	var fields []string
	switch vars.Name {
	case rearm.ImportSummaryTypeName:
		fields = p.ImportSummary
	case rearm.VexImportSummaryTypeName:
		fields = p.VexImportSummary
	default:
		t.Errorf("the probe asks for an unknown type %q: %s", vars.Name, r.Variables)
	}
	return 200, dataBody(map[string]any{"__type": probeType(fields)})
}

// probeName is the type a probe request asks for.
func probeName(t *testing.T, r recordedRequest) string {
	t.Helper()
	var vars struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(r.Variables, &vars); err != nil {
		t.Errorf("probe variables: %v", err)
	}
	return vars.Name
}

func dataBody(data map[string]any) string {
	b, _ := json.Marshal(map[string]any{"data": data})
	return string(b)
}

func errorsBody(data map[string]any, messages ...string) string {
	errs := make([]any, 0, len(messages))
	for _, m := range messages {
		errs = append(errs, map[string]any{"message": m})
	}
	b, _ := json.Marshal(map[string]any{"data": data, "errors": errs})
	return string(b)
}

// sum is an import summary; values are given as key, value pairs.
func sum(kv ...any) map[string]any {
	s := map[string]any{}
	for i := 0; i+1 < len(kv); i += 2 {
		s[kv[i].(string)] = kv[i+1]
	}
	return s
}

// fakeAnswer builds addArtifactProgrammatic's answer from the request: one detail per top-level
// artifact of the release part and of each deliverable group, carrying the tags as sent.
type fakeAnswer struct {
	field     string                 // summary field, importSummary by default
	summaries map[string]any         // "release/<j>" or "deliverable/<i>/<j>": the summary (absent: null)
	outbound  map[int]bool           // deliverable groups answered under variantDetails[0]
	mutate    func(a map[string]any) // last edits
}

func detailUuid(n int) string { return fmt.Sprintf("aaaaaaaa-0000-0000-0000-%012d", n) }

func (f fakeAnswer) build(t *testing.T, req recordedRequest) map[string]any {
	t.Helper()
	var vars struct {
		ArtifactInput struct {
			ReleaseArtifacts     []map[string]any `json:"releaseArtifacts"`
			DeliverableArtifacts []struct {
				Deliverable string           `json:"deliverable"`
				Artifacts   []map[string]any `json:"artifacts"`
			} `json:"deliverableArtifacts"`
		} `json:"artifactInput"`
	}
	if err := json.Unmarshal(req.Variables, &vars); err != nil {
		t.Errorf("variables: %v", err)
	}
	field := f.field
	if field == "" {
		field = rearm.ImportSummaryField
	}
	n := 0
	detail := func(a map[string]any, key string) map[string]any {
		n++
		d := map[string]any{"uuid": detailUuid(n), "type": a["type"], "displayIdentifier": a["displayIdentifier"], "tags": a["tags"]}
		d[field] = f.summaries[key]
		return d
	}
	var release []any
	for j, a := range vars.ArtifactInput.ReleaseArtifacts {
		release = append(release, detail(a, fmt.Sprintf("release/%d", j)))
	}
	answer := map[string]any{"uuid": "rel-1", "version": "1.0.0", "lifecycle": "ASSEMBLED", "artifacts": []any{}, "artifactDetails": release}
	if strings.Contains(req.Query, "inboundDeliverableDetails") {
		inbound, outbound := []any{}, []any{}
		for i, g := range vars.ArtifactInput.DeliverableArtifacts {
			var details []any
			for j, a := range g.Artifacts {
				details = append(details, detail(a, fmt.Sprintf("deliverable/%d/%d", i, j)))
			}
			d := map[string]any{"uuid": g.Deliverable, "artifacts": []any{}, "artifactDetails": details}
			if f.outbound[i] {
				outbound = append(outbound, d)
			} else {
				inbound = append(inbound, d)
			}
		}
		answer["inboundDeliverableDetails"] = inbound
		answer["variantDetails"] = []any{map[string]any{"outboundDeliverableDetails": outbound}}
	}
	if f.mutate != nil {
		f.mutate(answer)
	}
	return answer
}

// importServer answers each probe request from p and every upload with the fake answer.
func importServer(t *testing.T, p probeTypes, answer fakeAnswer) *fakeServer {
	return newFakeServer(t, func(n int, r recordedRequest) (int, string) {
		if r.OperationName == rearm.ImportSummaryProbeOperationName {
			return answerProbe(t, r, p)
		}
		return 200, dataBody(map[string]any{"addArtifactProgrammatic": answer.build(t, r)})
	})
}

// uploads are the recorded requests other than the probe.
func uploads(reqs []recordedRequest) []recordedRequest {
	var out []recordedRequest
	for _, r := range reqs {
		if r.OperationName != rearm.ImportSummaryProbeOperationName {
			out = append(out, r)
		}
	}
	return out
}

// lastUploadEnvelope is the stdout the CLI prints for the last upload answer the server sent.
func lastUploadEnvelope(t *testing.T, f *fakeServer) string {
	t.Helper()
	reqs, bodies := f.recorded()
	for i := len(reqs) - 1; i >= 0; i-- {
		if reqs[i].OperationName != rearm.ImportSummaryProbeOperationName {
			return bodies[i] + "\n"
		}
	}
	t.Fatal("no upload was sent")
	return ""
}

// releaseArtifactsOf decodes the release artifacts the request sent.
func releaseArtifactsOf(t *testing.T, r recordedRequest) []map[string]any {
	t.Helper()
	var vars struct {
		ArtifactInput struct {
			ReleaseArtifacts []map[string]any `json:"releaseArtifacts"`
		} `json:"artifactInput"`
	}
	if err := json.Unmarshal(r.Variables, &vars); err != nil {
		t.Fatal(err)
	}
	return vars.ArtifactInput.ReleaseArtifacts
}

// checkTagsOf is the values of the check tags of one sent artifact.
func checkTagsOf(a map[string]any) []string {
	var out []string
	tags, _ := a["tags"].([]any)
	for _, tg := range tags {
		m, _ := tg.(map[string]any)
		if m["key"] == importCheckTagKey {
			out = append(out, fmt.Sprint(m["value"]))
		}
	}
	return out
}

func wantLines(t *testing.T, res childResult, want []string) {
	t.Helper()
	got := res.stderrLines()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("stderr lines differ\nwant:\n%s\ngot:\n%s\n\n%s", strings.Join(want, "\n"), strings.Join(got, "\n"), res)
	}
}

func wantExit(t *testing.T, res childResult, code int) {
	t.Helper()
	if res.exit != code {
		t.Fatalf("want exit %d, got %s", code, res)
	}
}

func wantNoRequests(t *testing.T, f *fakeServer) {
	t.Helper()
	if reqs, _ := f.recorded(); len(reqs) != 0 {
		t.Fatalf("want no request, the server got %d (first %s)", len(reqs), reqs[0].OperationName)
	}
}
