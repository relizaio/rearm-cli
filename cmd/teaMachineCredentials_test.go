package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/oolong-project/tea-client-go"
	"github.com/oolong-project/tea-client-go/tei"
)

// Credentials, the streams, the classification and the production path of the machine
// mode (task TPF-178, design section 4, rows T-49 to T-58).

// teaRevokeAfter makes f refuse the bearer after n routed answers.
func teaRevokeAfter(f *teaFixture, n int, paths ...string) {
	count := 0
	for _, p := range paths {
		f.mu.Lock()
		fn, isRoute := f.routes[p]
		h, isRaw := f.raw[p]
		f.mu.Unlock()
		gate := func() {
			f.mu.Lock()
			count++
			if count >= n {
				f.requireBearer = "revoked"
			}
			f.mu.Unlock()
		}
		switch {
		case isRoute:
			f.routeFunc(p, func(q *http.Request) teaAnswer { gate(); return fn(q) })
		case isRaw:
			f.handle(p, func(w http.ResponseWriter, q *http.Request) { gate(); h(w, q) })
		}
	}
}

func TestTeaMachineCredentialStaysOnItsHost(t *testing.T) { // T-49
	d := newTeaDL(t)
	d.r.env[teaEnvAuthRoot] = d.f.root
	d.r.env[teaEnvToken] = "tok-1"
	d.art(teaArtProd, 1, "BOM", "",
		d.serverBody(teaArtProd, 1, "a/server", "one"),
		d.externalBody("/ext", "a/external", "two"),
		d.server(teaArtProd, 1, "a/moved", func(w http.ResponseWriter, q *http.Request) {
			http.Redirect(w, q, d.cdn.origin+"/moved", http.StatusFound)
		}, teaSums("SHA-256", teaSumSHA256("three"))))
	d.cdn.handle("/moved", teaBody("three"))
	d.f.handle("/tea/ext/under-root", teaBody("four"))
	d.arts = append(d.arts, `{"uuid":"`+teaArt3+`","version":1,"type":"OTHER","createdDate":"2026-01-10T08:05:00Z","formats":[`+
		`{"mediaType":"a/under","url":"`+d.f.root+`/ext/under-root","checksums":`+teaSums("SHA-256", teaSumSHA256("four"))+`}]}`)
	d.r.want(d.run(), teaExitOK)
	for _, q := range d.f.requests() {
		under := q.path == "/tea/ext/under-root"
		switch {
		case under && q.authorization != "":
			t.Errorf("the external URL under the root carried %q", q.authorization)
		case !under && strings.HasPrefix(q.path, "/tea/") && q.authorization != "Bearer tok-1":
			t.Errorf("%s carried %q", q.path, q.authorization)
		}
	}
	teaNoAuthorization(t, d.cdn)
	if d.cdn.count("/moved") != 1 || d.cdn.count("/ext") != 1 {
		t.Errorf("the external host saw %+v", d.cdn.requests())
	}
}

func TestTeaMachineOneExchange(t *testing.T) { // T-50
	d := newTeaDL(t)
	teaTokenFixture(d.f, true)
	teaKeyEnv(d.r.teaRow, d.f)
	var formats []string
	for i := 1; i <= 5; i++ {
		formats = append(formats, d.serverBody(teaArtProd, 1, fmt.Sprintf("a/f%d", i), fmt.Sprint(i)))
	}
	d.art(teaArtProd, 1, "BOM", "", formats...)
	d.r.want(d.run(), teaExitOK)
	teaCheckOneToken(t, d.f)

	r := newTeaMRow(t)
	f := r.fixture("tea.example.com", true)
	teaTokenFixture(f, true)
	teaKeyEnv(r.teaRow, f)
	teaThreePages(f, f.api("/product/"+teaProduct+"/releases"), teaListRelease)
	r.want(r.exec(teaVerbReleases, "--api-root", f.root, "--product", teaProduct), teaExitOK)
	teaCheckOneToken(t, f)
}

// teaCheckOneToken: exactly one token request, with HTTP Basic, and the issued token on
// every other request under the root.
func teaCheckOneToken(t *testing.T, f *teaFixture) {
	t.Helper()
	tokens := 0
	for _, q := range f.requests() {
		switch {
		case q.path == f.api("/token"):
			tokens++
			if !strings.HasPrefix(q.authorization, "Basic ") {
				t.Errorf("token request carried %q", q.authorization)
			}
		case strings.HasPrefix(q.path, "/tea/") && q.authorization != "Bearer tok-2":
			t.Errorf("%s carried %q", q.path, q.authorization)
		}
	}
	if tokens != 1 {
		t.Errorf("%d token requests", tokens)
	}
}

func TestTeaMachineTokenNotRefreshed(t *testing.T) { // T-51
	r := newTeaMRow(t)
	f := r.fixture("tea.example.com", true)
	teaTokenFixture(f, true)
	teaKeyEnv(r.teaRow, f)
	teaComponentReleaseFixture(f, "")
	f.route(f.api("/componentRelease/"+teaCR1+"/cle"), teaJSON(`{"events":[]}`))
	teaRevokeAfter(f, 1, f.api("/componentRelease/"+teaCR1))
	r.wantFail(r.exec(teaVerbRelease, "--api-root", f.root, "--component-release", teaCR1), teaExitAuth, teaReasonAuth)
	if f.count(f.api("/token")) != 1 {
		t.Errorf("%d token requests", f.count(f.api("/token")))
	}

	d := newTeaDL(t)
	teaTokenFixture(d.f, true)
	teaKeyEnv(d.r.teaRow, d.f)
	d.art(teaArtProd, 1, "BOM", "",
		d.serverBody(teaArtProd, 1, "a/s1", "one"),
		d.externalBody("/e1", "a/e1", "two"),
		d.serverBody(teaArtProd, 1, "a/s2", "three"),
		d.externalBody("/e2", "a/e2", "four"))
	teaRevokeAfterDownload(d, 2)
	d.r.want(d.run(), teaExitPartial)
	doc := d.r.doc()
	d.wantRow(doc, 0, "VERIFIED", nil)
	d.wantRow(doc, 1, "VERIFIED", nil)
	d.wantRow(doc, 2, "FAILED", "AUTH")
	d.wantRow(doc, 3, "VERIFIED", nil)
	if d.f.count(d.f.api("/token")) != 1 {
		t.Errorf("%d token requests", d.f.count(d.f.api("/token")))
	}
}

// teaRevokeAfterDownload revokes the token after n answers of the TEA server's download
// endpoint and collection (counted together).
func teaRevokeAfterDownload(d *teaDL, n int) {
	path := d.downloadPath(teaArtProd, 1)
	d.f.mu.Lock()
	h := d.f.raw[path]
	d.f.mu.Unlock()
	count := 1 // the collection read
	d.f.handle(path, func(w http.ResponseWriter, q *http.Request) {
		d.f.mu.Lock()
		count++
		if count >= n {
			d.f.requireBearer = "revoked"
		}
		d.f.mu.Unlock()
		h(w, q)
	})
}

func TestTeaMachineNothingSecret(t *testing.T) { // T-52
	secrets := []string{"tok-SECRET-1", "sec-SECRET-2", "pw-SECRET-3", "sig-SECRET-4"}
	check := func(r *teaMRow, stdoutMayHave string) {
		t.Helper()
		for _, s := range secrets {
			if strings.Contains(r.errOut.String(), s) {
				t.Errorf("stderr carries %s: %q", s, r.errOut.String())
			}
			if s != stdoutMayHave && strings.Contains(r.out.String(), s) {
				t.Errorf("stdout carries %s", s)
			}
		}
	}
	// a bearer token; a failing external URL with a signed query (the URL is data on stdout)
	d := newTeaDL(t)
	d.r.debug = true
	d.r.env[teaEnvAuthRoot] = d.f.root
	d.r.env[teaEnvToken] = "tok-SECRET-1"
	d.cdn.route("/signed", teaTEAError(http.StatusForbidden, "DENIED"))
	d.art(teaArtProd, 1, "BOM", "", d.serverBody(teaArtProd, 1, "a/ok", "x"),
		`{"mediaType":"a/signed","url":"`+d.cdn.origin+`/signed?X-Sig=sig-SECRET-4","checksums":`+teaSums("SHA-256", teaSumSHA256("x"))+`}`)
	d.r.want(d.run(), teaExitPartial)
	check(d.r, "sig-SECRET-4")
	if !strings.Contains(d.r.errOut.String(), "TEA credential: bearer token for") {
		t.Errorf("no trace: %q", d.r.errOut.String())
	}
	// an API key, debug, a failing run
	r := newTeaMRow(t)
	r.debug = true
	f := r.fixture("tea.example.com", true)
	f.mu.Lock()
	f.tokenKeyID, f.tokenSecret, f.tokenIssued, f.requireBearer = "kid", "sec-SECRET-2", "tok-SECRET-1", "tok-SECRET-1"
	f.mu.Unlock()
	r.env[teaEnvAuthRoot] = f.root
	r.env[teaEnvKeyID] = "kid"
	r.env[teaEnvKey] = "sec-SECRET-2"
	r.wantFail(r.exec(teaVerbProduct, "--api-root", f.root, "--uuid", teaProduct), teaExitNotResolved, teaReasonNotFound)
	check(r, "")
	// a root with userinfo is refused without being echoed
	r = newTeaMRow(t)
	r.debug = true
	r.wantFail(r.exec(teaVerbProduct, "--api-root", "https://user:pw-SECRET-3@tea.example.com/tea", "--uuid", teaProduct), teaExitUsage, teaReasonUsage)
	check(r, "")
	r.env[teaEnvAuthRoot] = "https://user:pw-SECRET-3@tea.example.com/tea"
	r.env[teaEnvToken] = "tok-SECRET-1"
	r.wantFail(r.exec(teaVerbProduct, "--api-root", "https://tea.example.com/tea", "--uuid", teaProduct), teaExitUsage, teaReasonUsage)
	check(r, "")
}

func TestTeaMachineCredentialStdin(t *testing.T) { // T-53
	// (a) the first line of a pipe; the rest is not consumed
	r := newTeaMRow(t)
	f := r.fixture("tea.example.com", true)
	f.requireBearer = "tok"
	f.route(f.api("/product/"+teaProduct), teaJSON(`{"uuid":"`+teaProduct+`","name":"P","identifiers":[]}`))
	r.env[teaEnvAuthRoot] = f.root
	in := strings.NewReader("tok\nrest")
	r.in = in
	r.want(r.exec(teaVerbProduct, "--api-root", f.root, "--uuid", teaProduct, "--credential-stdin"), teaExitOK)
	if in.Len() != 4 {
		t.Errorf("%d bytes left, want 4", in.Len())
	}
	// (b) a terminal: refused, the no-echo reader never called (the row's reader fails the test)
	r = newTeaMRow(t)
	f = r.fixture("tea.example.com", true)
	r.env[teaEnvAuthRoot] = f.root
	r.stdin = teaStdinTerminal
	r.wantFail(r.exec(teaVerbProduct, "--api-root", f.root, "--uuid", teaProduct, "--credential-stdin"), teaExitUsage, teaReasonUsage)
	teaNoRequests(t, f)
	// (c) without the flag standard input is never read: every other row runs with a reader
	// that fails the test when read (newTeaMRow)
}

func TestTeaMachineTrace(t *testing.T) { // T-54
	r := newTeaMRow(t)
	r.debug = true
	f := r.fixture("tea.example.com", true)
	f.route(f.api("/discovery"), teaJSON(teaDiscoveryJSON(f.root, teaPR1)))
	r.want(r.exec(teaVerbResolve, teaResolveArgs(f, teaPR1)...), teaExitOK)
	r.golden(`{"formatVersion":1,"kind":"TEA_RESOLUTION","tei":"tei://tea.example.com/uuid/` + teaPR1 + `","results":[{"productReleaseUuid":"` + teaPR1 +
		`","discoveredAt":{"rootUrl":"` + f.root + `","apiVersion":"1.0.0"},"servers":[{"rootUrl":"` + f.root + `","versions":["1.0.0"],"priority":null}]}]}`)
	teaCheckTraceLines(t, r.errOut.String(), false)

	r = newTeaMRow(t)
	r.debug = true
	f = r.fixture("tea.example.com", true)
	r.m.resolveTo("evil.example.com", "169.254.169.254")
	r.wantRefused(r.exec(teaVerbProduct, "--api-root", "https://evil.example.com:"+f.port+"/tea", "--uuid", teaProduct), "169.254.169.254", teaAddressCloudMetadata)
	teaCheckTraceLines(t, r.errOut.String(), true)
	if !strings.Contains(r.errOut.String(), "TEA address refused: 169.254.169.254 CLOUD_METADATA for evil.example.com\n") {
		t.Errorf("no refusal line: %q", r.errOut.String())
	}
}

func teaCheckTraceLines(t *testing.T, s string, failed bool) {
	t.Helper()
	lines := teaLines(s)
	if failed {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		t.Fatalf("no trace")
	}
	for _, l := range lines {
		ok := false
		for _, p := range []string{"TEA request [", "TEA response [", "TEA attempt failed [", "TEA backoff [", "TEA credential: ", "TEA address refused: "} {
			if strings.HasPrefix(l, p) {
				ok = true
			}
		}
		if !ok {
			t.Errorf("trace line %q", l)
		}
		teaWantASCIILine(t, "trace line", l+"\n")
	}
}

func TestTeaMachineReasons(t *testing.T) { // T-55
	live := context.Background()
	deadline, cancel := context.WithTimeoutCause(context.Background(), 0, errTeaDeadline)
	defer cancel()
	<-deadline.Done()
	interrupted, stop := context.WithCancel(context.Background())
	stop()
	_, policyErr := tea.ParseAPIBase("https://tea.example.com/a/../b", teaAPIVersion)
	_, syntaxErr := tei.Parse("tei://")
	refused := &teaAddressRefusedError{Address: netip.MustParseAddr("127.0.0.1"), Class: teaAddressLoopback}
	connect := &teaConnectError{Phase: teaPhaseConnect, Host: "h", Err: errors.New("refused")}
	server := &tea.APIError{Status: 500}
	mixed := func(errs ...error) error {
		var as []tea.Attempt
		for _, e := range errs {
			as = append(as, tea.Attempt{Err: e})
		}
		return &tea.DiscoveryError{Stage: tea.StageAPIDiscovery, Attempts: as}
	}
	for _, c := range []struct {
		name   string
		ctx    context.Context
		err    error
		bound  teaFileBound
		reason teaReason
		code   teaExit
	}{
		{"usage", live, teaUsage("x"), 0, teaReasonUsage, teaExitUsage},
		{"TEI syntax", live, syntaxErr, 0, teaReasonUsage, teaExitUsage},
		{"usage before an ended deadline", deadline, teaUsage("x"), 0, teaReasonUsage, teaExitUsage},
		{"deadline", deadline, context.DeadlineExceeded, 0, teaReasonDeadline, teaExitLimit},
		{"deadline wins over a refusal", deadline, refused, 0, teaReasonDeadline, teaExitLimit},
		{"interrupt", interrupted, context.Canceled, 0, teaReasonInterrupted, teaExitFailed},
		{"refusal and 500 in discovery", live, mixed(server, refused), 0, teaReasonAddressRefused, teaExitAddressRefused},
		{"connect failure and 500 in discovery", live, mixed(server, connect), 0, teaReasonConnectFailed, teaExitConnect},
		{"refusal wins over connect failure", live, mixed(connect, refused), 0, teaReasonAddressRefused, teaExitAddressRefused},
		{"redirect limit", live, fmt.Errorf("x: %w", tea.ErrRedirectLimit), 0, teaReasonRedirectLimit, teaExitRefusedURL},
		{"URL policy", live, policyErr, 0, teaReasonURLRefused, teaExitRefusedURL},
		{"401", live, &tea.APIError{Status: 401}, 0, teaReasonAuth, teaExitAuth},
		{"token error", live, &tea.TokenError{Status: 400}, 0, teaReasonAuth, teaExitAuth},
		{"not resolved", live, &tea.NotResolvedError{Code: tea.ErrorCodeObjectUnknown}, 0, teaReasonNotFound, teaExitNotResolved},
		{"empty discovery", live, errTeaNoResult, 0, teaReasonNotFound, teaExitNotResolved},
		{"too large", live, tea.ErrResponseTooLarge, teaFileBoundNone, teaReasonDocumentBytes, teaExitLimit},
		{"too large, artifact bound", live, tea.ErrResponseTooLarge, teaFileBoundArtifact, teaReasonArtifactBytes, teaExitFailed},
		{"too large, total bound", live, tea.ErrResponseTooLarge, teaFileBoundTotal, teaReasonTotalBytes, teaExitFailed},
		{"too many results", live, mixed(tea.ErrTooManyResults), 0, teaReasonDiscoveryResults, teaExitLimit},
		{"time bound", live, fmt.Errorf("x: %w", tea.ErrTimeout), 0, teaReasonRequestTimeout, teaExitLimit},
		{"string bound", live, &teaLimitError{reason: teaReasonStringBytes}, 0, teaReasonStringBytes, teaExitLimit},
		{"output bound", live, &teaLimitError{reason: teaReasonOutputBytes}, 0, teaReasonOutputBytes, teaExitLimit},
		{"file count", live, &teaLimitError{reason: teaReasonFileCount}, 0, teaReasonFileCount, teaExitLimit},
		{"no usable checksum", live, tea.ErrNoUsableChecksum, 0, teaReasonNoUsableChecksum, teaExitFailed},
		{"checksum", live, &tea.ChecksumError{}, 0, teaReasonChecksumMismatch, teaExitFailed},
		{"406", live, tea.ErrNoAcceptableFormat, 0, teaReasonNoAcceptableFormat, teaExitFailed},
		{"5xx", live, server, 0, teaReasonServerError, teaExitFailed},
		{"unexpected", live, &tea.UnexpectedResponseError{}, 0, teaReasonInvalidResponse, teaExitFailed},
		{"content coding", live, tea.ErrUnsupportedContentEncoding, 0, teaReasonInvalidResponse, teaExitFailed},
		{"invalid argument", live, tea.ErrInvalidArgument, 0, teaReasonInvalidResponse, teaExitFailed},
		{"local file", live, &os.PathError{Op: "open", Path: "x", Err: os.ErrPermission}, 0, teaReasonLocalIO, teaExitFailed},
		{"local rename", live, &os.LinkError{Op: "rename", Old: "a", New: "b", Err: os.ErrPermission}, 0, teaReasonLocalIO, teaExitFailed},
		{"interfaces", live, errTeaInterfaces, 0, teaReasonFailed, teaExitFailed},
		{"other", live, errors.New("x"), 0, teaReasonFailed, teaExitFailed},
	} {
		got := teaMachineReason(c.ctx, c.err, c.bound)
		if got != c.reason || got.exit() != c.code {
			t.Errorf("%s: %s (exit %d), want %s (exit %d)", c.name, got, got.exit(), c.reason, c.code)
		}
	}
}

// TestTeaMachineChild is the entry of a child process: it runs Execute with the arguments
// of REARM_TEST_TEA_MACHINE_ARGS; without them it does nothing.
func TestTeaMachineChild(t *testing.T) {
	args := os.Getenv("REARM_TEST_TEA_MACHINE_ARGS")
	if args == "" {
		t.Skip("child process entry")
	}
	var argv []string
	if err := json.Unmarshal([]byte(args), &argv); err != nil {
		os.Exit(99)
	}
	rootCmd.SetArgs(argv)
	Execute()
	os.Exit(0)
}

// teaChild runs the CLI in a child process through Execute with home as its home
// directory and extra variables.
func teaChild(t *testing.T, home string, extra []string, args ...string) (int, string, string) {
	t.Helper()
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(kv), "REARM_") && !strings.HasPrefix(kv, "HOME=") {
			env = append(env, kv)
		}
	}
	argv, _ := json.Marshal(args)
	cmd := exec.Command(os.Args[0], "-test.run=^TestTeaMachineChild$")
	cmd.Env = append(append(env, "HOME="+home, "REARM_TEST_TEA_MACHINE_ARGS="+string(argv)), extra...)
	var out, errOut strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	code := 0
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code = exitErr.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return code, out.String(), errOut.String()
}

func TestTeaMachineUsageThroughExecute(t *testing.T) { // T-56
	for _, args := range [][]string{
		{"tea", "machine", "product", "--no-such-flag"},
		{"tea", "machine"},
		{"tea", "machine", "nosuchverb"},
		{"tea", "machine", "product", "--api-root", "https://tea.example.com/tea"},
	} {
		code, out, errOut := teaChild(t, t.TempDir(), nil, args...)
		if code != 2 || out != "" {
			t.Errorf("%v: exit %d stdout %q", args, code, out)
		}
		var e map[string]any
		if strings.Count(errOut, "\n") != 1 || json.Unmarshal([]byte(errOut), &e) != nil || e["reason"] != "USAGE" || e["kind"] != "TEA_ERROR" {
			t.Errorf("%v: stderr %q", args, errOut)
		}
	}
}

func TestTeaMachineFlagsOnlyFromTheCommandLine(t *testing.T) { // T-57
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, ".rearm.yaml"), []byte("allow-this-host: true\nusehttp: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	vars := []string{"REARM_ALLOW_THIS_HOST=true", "rearm_ALLOW_THIS_HOST=true", "REARM_USEHTTP=true", "rearm_USEHTTP=true"}
	// (a) a real listener on loopback: refused, no connection
	ln := newTeaCountingListener(t)
	code, out, errOut := teaChild(t, home, vars, "tea", "machine", "product", "--api-root", "https://127.0.0.1:"+ln.port()+"/tea", "--uuid", teaProduct)
	var e map[string]any
	if code != 8 || out != "" || json.Unmarshal([]byte(errOut), &e) != nil || e["addressClass"] != "LOOPBACK" {
		t.Errorf("(a) exit %d stdout %q stderr %q", code, out, errOut)
	}
	if ln.accepts.Load() != 0 {
		t.Errorf("(a) the listener accepted %d connections", ln.accepts.Load())
	}
	// (b) with the flags on the command line: a plain fixture on loopback answers
	row := newTeaRow(t)
	f := newTeaFixture(t, row.net, "127.0.0.1", false)
	f.route(f.api("/product/"+teaProduct), teaJSON(`{"uuid":"`+teaProduct+`","name":"P","identifiers":[]}`))
	code, out, errOut = teaChild(t, home, vars, "tea", "machine", "product", "--api-root", f.root, "--uuid", teaProduct, "--allow-this-host", "--usehttp")
	want := `{"formatVersion":1,"kind":"TEA_PRODUCT","server":{"rootUrl":"` + f.root + `","apiVersion":"1.0.0"},"product":{"uuid":"` + teaProduct + `","name":"P","identifiers":[]}}` + "\n"
	if code != 0 || out != want || errOut != "" {
		t.Errorf("(b) exit %d stdout %q stderr %q", code, out, errOut)
	}
}

func TestTeaMachineInteractiveUnrestricted(t *testing.T) { // T-58
	row := newTeaRow(t)
	f := newTeaFixture(t, row.net, "localhost", false)
	f.route(f.api("/discovery"), teaJSON(teaDiscoveryJSON(f.root, teaPR1)))
	code, out, errOut := teaChild(t, t.TempDir(), nil, "tea", "discovery", "--tei", "tei://localhost/uuid/"+teaPR1, "--usehttp", "--useport", f.port)
	if code != 0 || out != teaPR1+"\n" {
		t.Errorf("exit %d stdout %q stderr %q", code, out, errOut)
	}
}
